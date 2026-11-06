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

// httpClient is the DNS provider client. siteName is the site's
// internalReference (e.g. "default"), which is what the /static-dns paths key
// on; it is resolved from UNIFI_SITE at startup.
type httpClient struct {
	cfg      *Config
	httpc    *http.Client
	siteName string
}

// API paths. The leading %s is the base URL, which is either the local
// controller or the api.ui.com cloud connector prefix; see Config.baseURL.
//
// Records go through the Network application's own /v2/api/.../static-dns
// endpoints rather than the Integration API's /dns/policies: the Integration
// API rejects names whose labels start with an underscore (for example the
// RFC 9462 DDR name _dns.resolver.arpa) with "domain must be valid domain",
// while /static-dns accepts them and both are views over the same record
// store. Site lookup stays on the Integration API because it is documented
// and accepts either a name or a UUID.
const (
	pathSites           = "%s/proxy/network/integration/v1/sites"
	pathStaticDNS       = "%s/proxy/network/v2/api/site/%s/static-dns"
	pathStaticDNSRecord = "%s/proxy/network/v2/api/site/%s/static-dns/%s"
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

// siteResolveBaseTimeout is the headroom the startup site-resolution probe
// gets for the HTTP request(s) themselves. The effective timeout adds the
// worst-case retry backoff budget on top (see siteResolveTimeoutFor), so a
// broken controller still can't hang the process forever, but raising the
// retry knobs never starves the probe mid-backoff.
const siteResolveBaseTimeout = 15 * time.Second

// siteResolveTimeoutFor returns how long the startup probe can run for the
// given configuration: the fixed request headroom plus the worst-case time the
// retry loop can spend in backoff. The limit comes from the retry
// configuration rather than a fixed cap. An operator who raises
// UNIFI_RETRY_ATTEMPTS to ride out a slow or rate-limiting controller then
// does not trip a startup context deadline and crashloop on every restart.
func siteResolveTimeoutFor(cfg *Config) time.Duration {
	return siteResolveBaseTimeout + worstCaseRetryBudget(cfg)
}

// maxResponseBytes caps how much of a UniFi API response body we buffer into
// memory before decoding. The /static-dns list is a single unpaginated array
// of ~150-byte entries, so a 25 MiB ceiling sits far above any legitimate
// payload while still protecting the process from a buggy or hostile
// controller streaming an unbounded body into io.ReadAll. It is a var
// rather than a const so tests can lower it without generating multi-megabyte
// fixtures.
var maxResponseBytes int64 = 25 << 20 // 25 MiB

var uuidRegex = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// newUnifiClient constructs a UniFi API client and resolves the site name (or
// UUID) supplied via UNIFI_SITE to the site's internalReference at startup.
// The probe also validates credentials early so misconfiguration fails fast.
func newUnifiClient(config *Config) (*httpClient, error) {
	transport, err := newHTTPTransport(config)
	if err != nil {
		return nil, err
	}

	// Do NOT wrap with extdnshttp.NewInstrumentedClient here. Its
	// external_dns_http_request_duration_seconds metric is labelled by the
	// request path, and the UniFi API embeds the per-record ID in the path
	// (.../static-dns/{id}). Every record ever touched then becomes
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
	site, err := c.resolveSite(ctx, config.Site)
	if err != nil {
		return nil, fmt.Errorf("resolving UNIFI_SITE %q: %w", config.Site, err)
	}
	c.siteName = site.InternalReference
	slog.Info("resolved unifi site", "ref", config.Site, "id", site.ID, "name", site.InternalReference)

	return c, nil
}

// resolveSite looks up a user-supplied site name (or UUID) via GET /v1/sites
// with a filter. Accepts either the human-friendly internalReference (e.g.
// "default") or a raw UUID — both are common ways people identify a site in
// their environment.
func (c *httpClient) resolveSite(ctx context.Context, ref string) (siteEntry, error) {
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
		return siteEntry{}, err
	}

	switch len(page.Data) {
	case 0:
		return siteEntry{}, fmt.Errorf("no site matches %s=%q", field, ref)
	case 1:
		return page.Data[0], nil
	default:
		return siteEntry{}, fmt.Errorf("multiple sites match %s=%q (%d)", field, ref, len(page.Data))
	}
}

// getJSON issues a GET, reads the body fully, and decodes it into dest. The
// scope label is used in error messages ("DNS records", "sites", ...) so the
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
// controller. Disabled records and record types we don't manage are filtered
// out by the converter so we never report them as managed records.
func (c *httpClient) GetEndpoints(ctx context.Context) (records []DNSRecord, err error) {
	start := time.Now()
	var bodyRead int
	defer func() {
		metrics.Get().RecordUniFiAPICall("get_endpoints", time.Since(start), bodyRead, err)
	}()

	var entries []staticDNSRecord
	bodyRead, err = c.getJSON(ctx, formatURL(pathStaticDNS, c.cfg.baseURL(), c.siteName), "DNS records", &entries)
	if err != nil {
		return nil, fmt.Errorf("fetching DNS records from UniFi: %w", err)
	}

	records = make([]DNSRecord, 0, len(entries))
	for _, e := range entries {
		if r, ok := toDNSRecord(e); ok {
			records = append(records, r)
		}
	}

	slog.Debug("fetched records", "count", len(records))

	return records, nil
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
		slog.Warn("The provider ignores additional CNAME targets and uses only the first target",
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

// isSRVConversionError reports whether err originated in SRV target
// parsing. It guards the SRV-specific metric so unrelated DataErrors (marshal,
// MX parse, unsupported type) don't pollute the count.
func isSRVConversionError(err error) bool {
	return errors.Is(err, errSRVConversion)
}

// createOne POSTs a single DNS record and returns the number of response body
// bytes read (for metric accounting), the created record, and any error.
func (c *httpClient) createOne(ctx context.Context, r DNSRecord) (int, *DNSRecord, error) {
	rec, err := fromDNSRecord(r)
	if err != nil {
		return 0, nil, err
	}

	body, err := json.Marshal(rec)
	if err != nil {
		return 0, nil, NewDataError("marshal", "DNS record", err)
	}

	resp, err := c.doRequest(ctx, http.MethodPost, formatURL(pathStaticDNS, c.cfg.baseURL(), c.siteName), body)
	if err != nil {
		return 0, nil, fmt.Errorf("creating DNS record: %w", err)
	}
	defer extdnshttp.DrainAndClose(resp.Body)

	var created staticDNSRecord
	n, err := decodeJSON(resp, "created DNS record", &created)
	if err != nil {
		return n, nil, err
	}

	out, ok := toDNSRecord(created)
	if !ok {
		return n, nil, NewDataError("convert", "created DNS record",
			fmt.Errorf("unsupported response type %q", created.RecordType))
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

	u := formatURL(pathStaticDNSRecord, c.cfg.baseURL(), c.siteName, id)
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
