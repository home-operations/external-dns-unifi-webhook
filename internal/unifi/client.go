package unifi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/home-operations/external-dns-unifi-webhook/internal/metrics"
	externaldnsendpoint "sigs.k8s.io/external-dns/endpoint"
	extdnshttp "sigs.k8s.io/external-dns/pkg/http"
)

// httpClient is the DNS provider client.
type httpClient struct {
	cfg    *Config
	httpc  *http.Client
	siteID string
}

// API paths (Network Integration API). The leading %s is the base URL, which
// is either the local controller or the api.ui.com cloud connector prefix; see
// Config.baseURL. The Integration API surface is identical for both.
const (
	pathSites    = "%s/proxy/network/integration/v1/sites"
	pathPolicies = "%s/proxy/network/integration/v1/sites/%s/dns/policies"
	pathPolicy   = "%s/proxy/network/integration/v1/sites/%s/dns/policies/%s"
)

// recordType* are the external-dns / DNSRecord type strings. They map to the
// API's per-type discriminator values in dto.go.
const (
	recordTypeA     = "A"
	recordTypeAAAA  = "AAAA"
	recordTypeCNAME = "CNAME"
	recordTypeMX    = "MX"
	recordTypeSRV   = "SRV"
	recordTypeTXT   = "TXT"
)

// managedRecordTypes is every record type this provider emits (toDNSRecord
// produces only these; all other policy types are skipped). Records() seeds its
// per-type tally with all of them at zero so a type whose records were all
// deleted is reported as 0 this cycle instead of leaving the gauge pinned at
// its last value.
var managedRecordTypes = []string{
	recordTypeA, recordTypeAAAA, recordTypeCNAME,
	recordTypeMX, recordTypeSRV, recordTypeTXT,
}

// pageLimit is the API's documented maximum page size.
const pageLimit = 200

// siteResolveBaseTimeout is the headroom the startup site-resolution probe
// gets for the HTTP request(s) themselves. The effective timeout adds the
// worst-case retry backoff budget on top (see siteResolveTimeoutFor), so a
// broken controller still can't hang the process forever, but raising the
// retry knobs never starves the probe mid-backoff.
const siteResolveBaseTimeout = 15 * time.Second

// siteResolveTimeoutFor returns how long the startup probe may run for the
// given config: the fixed request headroom plus the worst-case time the retry
// loop can spend in backoff. Deriving it from the retry settings (rather than a
// fixed cap) is what stops an operator who raises UNIFI_RETRY_ATTEMPTS — to ride
// out a slow or rate-limiting controller — from instead tripping a startup
// context deadline and crashlooping on every restart.
func siteResolveTimeoutFor(cfg *Config) time.Duration {
	return siteResolveBaseTimeout + worstCaseRetryBudget(cfg)
}

// maxResponseBytes caps how much of a UniFi API response body we buffer into
// memory before decoding. Integration API responses are small JSON documents
// (a policy page is bounded by pageLimit), so a 25 MiB ceiling sits far above
// any legitimate payload while still protecting the process from a buggy or
// hostile controller streaming an unbounded body into io.ReadAll. It is a var
// rather than a const so tests can lower it without generating multi-megabyte
// fixtures.
var maxResponseBytes int64 = 25 << 20 // 25 MiB

var uuidRegex = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// newUnifiClient constructs a UniFi API client and resolves the site name (or
// UUID) supplied via UNIFI_SITE to the API's UUID at startup. The probe also
// validates credentials early so misconfiguration fails fast.
func newUnifiClient(config *Config) (*httpClient, error) {
	transport, err := newHTTPTransport(config)
	if err != nil {
		return nil, err
	}

	// Do NOT wrap with extdnshttp.NewInstrumentedClient here. Its
	// external_dns_http_request_duration_seconds metric is labelled by the
	// request path, and the UniFi Integration API embeds the per-record UUID in
	// the path (.../dns/policies/{uuid}). Every record ever touched then becomes
	// a permanent series, so the metric grows unbounded (tens of thousands of
	// series for a few dozen records) until the /metrics payload blows past a
	// scraper's max-scrape-size and scraping breaks entirely. UniFi API timing
	// is already covered, with bounded operation labels, by
	// externaldns_webhook_unifi_api_duration_seconds (see RecordUniFiAPICall).
	c := &httpClient{
		cfg:   config,
		httpc: &http.Client{Transport: transport, Timeout: config.RequestTimeout},
	}

	ctx, cancel := context.WithTimeout(context.Background(), siteResolveTimeoutFor(config))
	defer cancel()
	siteID, err := c.resolveSite(ctx, config.Site)
	if err != nil {
		return nil, fmt.Errorf("resolving UNIFI_SITE %q: %w", config.Site, err)
	}
	c.siteID = siteID
	slog.Info("resolved unifi site", "ref", config.Site, "id", siteID)

	return c, nil
}

// resolveSite turns a user-supplied site name (or UUID) into the API's site
// UUID via GET /v1/sites with a filter. Accepts either the human-friendly
// internalReference (e.g. "default") or a raw UUID — both are common ways
// people identify a site in their environment.
func (c *httpClient) resolveSite(ctx context.Context, ref string) (string, error) {
	field := "internalReference"
	if uuidRegex.MatchString(ref) {
		field = "id"
	}

	// The Integration API filter DSL wraps string values in single quotes and
	// escapes an embedded quote by doubling it. ref is operator-supplied
	// (UNIFI_SITE), so quote it correctly rather than trusting its contents.
	filter := fmt.Sprintf("%s.eq('%s')", field, strings.ReplaceAll(ref, "'", "''"))
	u := formatURL(pathSites, c.cfg.baseURL()) + "?filter=" + url.QueryEscape(filter)

	var page sitePage
	if _, err := c.getJSON(ctx, u, "sites", &page); err != nil {
		return "", err
	}

	switch len(page.Data) {
	case 0:
		return "", fmt.Errorf("no site matches %s=%q", field, ref)
	case 1:
		return page.Data[0].ID, nil
	default:
		return "", fmt.Errorf("multiple sites match %s=%q (%d)", field, ref, len(page.Data))
	}
}

// getJSON issues a GET, reads the body fully, and decodes it into dest. The
// scope label is used in error messages ("DNS policies", "sites", ...) so the
// caller doesn't have to repeat it across read/unmarshal sites. Returns the
// number of body bytes read for metric accounting.
func (c *httpClient) getJSON(ctx context.Context, reqURL, scope string, dest any) (int, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return 0, err
	}
	defer extdnshttp.DrainAndClose(resp.Body)

	return decodeJSON(resp, scope, dest)
}

// decodeJSON reads resp.Body fully and unmarshals it into dest, wrapping
// failures as DataErrors scoped by label. Returns the number of body bytes
// read for metric accounting. The caller owns closing resp.Body.
func decodeJSON(resp *http.Response, scope string, dest any) (int, error) {
	// Read one byte past the cap so a body sitting exactly at the limit still
	// decodes while anything larger is rejected before we try to unmarshal it.
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return 0, NewDataError("read", scope+" response body", err)
	}
	if int64(len(body)) > maxResponseBytes {
		return len(body), NewDataError("read", scope+" response body",
			fmt.Errorf("response exceeds %d-byte limit", maxResponseBytes))
	}
	if err := json.Unmarshal(body, dest); err != nil {
		return len(body), NewDataError("unmarshal", scope, err)
	}

	return len(body), nil
}

// GetEndpoints retrieves the full list of DNS records from the UniFi
// controller, walking the paginated DNS Policies endpoint. FORWARD_DOMAIN
// entries and unknown record types are filtered out by the converter so we
// never report them as managed records.
func (c *httpClient) GetEndpoints(ctx context.Context) (records []DNSRecord, err error) {
	start := time.Now()
	var offset, bodyRead int
	defer func() {
		metrics.Get().RecordUniFiAPICall("get_endpoints", time.Since(start), bodyRead, err)
	}()

	for {
		page, n, perr := c.fetchPolicyPage(ctx, offset, pageLimit)
		bodyRead += n
		if perr != nil {
			return nil, fmt.Errorf("fetching DNS records from UniFi: %w", perr)
		}

		for _, env := range page.Data {
			if r, ok := toDNSRecord(env); ok {
				records = append(records, r)
			}
		}

		offset += page.Count
		if page.Count == 0 || offset >= page.TotalCount {
			break
		}
	}

	slog.Debug("fetched records", "count", len(records))

	return records, nil
}

func (c *httpClient) fetchPolicyPage(ctx context.Context, offset, limit int) (dnsPolicyPage, int, error) {
	u := fmt.Sprintf("%s?offset=%d&limit=%d",
		formatURL(pathPolicies, c.cfg.baseURL(), c.siteID), offset, limit)

	var page dnsPolicyPage
	n, err := c.getJSON(ctx, u, "DNS policies", &page)

	return page, n, err
}

// CreateEndpoint creates a new DNS record in the UniFi controller.
func (c *httpClient) CreateEndpoint(ctx context.Context, endpoint *externaldnsendpoint.Endpoint) (created []*DNSRecord, err error) {
	m := metrics.Get()
	start := time.Now()
	var bodyRead int
	defer func() {
		m.RecordUniFiAPICall("create_endpoint", time.Since(start), bodyRead, err)
	}()

	if endpoint.RecordType == recordTypeCNAME && len(endpoint.Targets) > 1 {
		m.IgnoredCNAMETargetsTotal.WithLabelValues(metrics.ProviderName).Inc()
		slog.Warn("ignoring additional CNAME targets; only the first target will be used",
			"key", endpoint.DNSName, "ignored_targets", endpoint.Targets[1:])
		endpoint.Targets = endpoint.Targets[:1]
	}

	created = make([]*DNSRecord, 0, len(endpoint.Targets))
	for _, target := range endpoint.Targets {
		r := DNSRecord{
			Key:        endpoint.DNSName,
			RecordType: endpoint.RecordType,
			TTL:        endpoint.RecordTTL,
			Value:      target,
		}

		n, out, cerr := c.createOne(ctx, r)
		bodyRead += n
		if cerr != nil {
			if isSRVConversionError(cerr) {
				m.SRVParsingErrorsTotal.WithLabelValues(metrics.ProviderName).Inc()
			}

			return nil, cerr
		}

		created = append(created, out)
		slog.Debug("created record", "key", out.Key, "type", out.RecordType, "target", out.Value)
	}

	return created, nil
}

// isSRVConversionError reports whether err originated in SRV name or target
// parsing. It guards the SRV-specific metric so unrelated DataErrors (marshal,
// MX parse, unsupported type) don't pollute the count.
func isSRVConversionError(err error) bool {
	return errors.Is(err, errSRVConversion)
}

// createOne POSTs a single DNS record and returns the number of response body
// bytes read (for metric accounting), the created record, and any error.
func (c *httpClient) createOne(ctx context.Context, r DNSRecord) (int, *DNSRecord, error) {
	env, err := fromDNSRecord(r)
	if err != nil {
		return 0, nil, err
	}

	body, err := json.Marshal(env)
	if err != nil {
		return 0, nil, NewDataError("marshal", "DNS record", err)
	}

	resp, err := c.doRequest(ctx, http.MethodPost, formatURL(pathPolicies, c.cfg.baseURL(), c.siteID), body)
	if err != nil {
		return 0, nil, fmt.Errorf("creating DNS record: %w", err)
	}
	defer extdnshttp.DrainAndClose(resp.Body)

	var createdEnv dnsPolicyEnvelope
	n, err := decodeJSON(resp, "created DNS record", &createdEnv)
	if err != nil {
		return n, nil, err
	}

	out, ok := toDNSRecord(createdEnv)
	if !ok {
		return n, nil, NewDataError("convert", "created DNS record",
			fmt.Errorf("unsupported response type %q", createdEnv.Type))
	}

	return n, &out, nil
}

// DeleteRecord deletes a single DNS record by its ID. 404 responses are
// treated as success: the record is already gone, which is the desired
// outcome. Tolerating 404 lets callers use a snapshot-based index of records
// without worrying about staleness — concurrent deletes, manual cleanup in
// the UniFi UI, or a stale plan all converge harmlessly.
func (c *httpClient) DeleteRecord(ctx context.Context, id string) (err error) {
	start := time.Now()
	defer func() {
		metrics.Get().RecordUniFiAPICall("delete_record", time.Since(start), 0, err)
	}()

	u := formatURL(pathPolicy, c.cfg.baseURL(), c.siteID, id)
	resp, derr := c.doRequest(ctx, http.MethodDelete, u, nil)
	if derr != nil {
		if apiErr, ok := errors.AsType[*APIError](derr); ok && apiErr.StatusCode == http.StatusNotFound {
			slog.Debug("record already deleted", "id", id)

			return nil
		}

		return fmt.Errorf("deleting DNS record %s: %w", id, derr)
	}
	extdnshttp.DrainAndClose(resp.Body)
	slog.Debug("removed record", "id", id)

	return nil
}
