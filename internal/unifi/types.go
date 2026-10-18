package unifi

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"sigs.k8s.io/external-dns/endpoint"
)

// Upper bounds on the operator-tunable concurrency/retry knobs. The defaults
// (5 workers, 3 attempts) are far below these; the ceilings exist so a typo
// (UNIFI_APPLY_WORKERS=500) can't fan out enough concurrent connections to
// exhaust a consumer controller's connection table, and so the retry budget
// stays sane. Generous enough that no realistic configuration trips them.
const (
	maxApplyWorkers  = 100
	maxRetryAttempts = 10
	// minRetryInitialDelay floors UNIFI_RETRY_INITIAL_DELAY. backoff() computes
	// jitter as rand.Int64N(base/2); a sub-2ns initial delay makes base/2 == 0,
	// which panics rand.Int64N. 1ms is far below any sensible retry delay (the
	// default is 500ms) while keeping base/2 safely positive.
	minRetryInitialDelay = time.Millisecond
)

// Config represents the configuration for the UniFi API.
type Config struct {
	Host              string        `env:"UNIFI_HOST,notEmpty"`
	APIKey            string        `env:"UNIFI_API_KEY,notEmpty,unset"`
	Site              string        `env:"UNIFI_SITE"                envDefault:"default"`
	ConsoleID         string        `env:"UNIFI_CONSOLE_ID"          envDefault:""`
	SkipTLSVerify     bool          `env:"UNIFI_SKIP_TLS_VERIFY"     envDefault:"true"`
	CACertPath        string        `env:"UNIFI_CA_CERT"             envDefault:""`
	RetryAttempts     int           `env:"UNIFI_RETRY_ATTEMPTS"      envDefault:"3"`
	RetryInitialDelay time.Duration `env:"UNIFI_RETRY_INITIAL_DELAY" envDefault:"500ms"`
	RetryMaxDelay     time.Duration `env:"UNIFI_RETRY_MAX_DELAY"     envDefault:"10s"`
	ApplyWorkers      int           `env:"UNIFI_APPLY_WORKERS"       envDefault:"5"`
	// RequestTimeout bounds a single attempt against the controller, including
	// reading the response body. Zero (the default) leaves attempts bounded only
	// by the caller's context. ExternalDNS gives the whole webhook call a fixed
	// budget (--webhook-provider-read-timeout + --webhook-provider-write-timeout)
	// and exits when it expires, while an HTTP 5xx from the webhook is only
	// logged. A per-attempt timeout turns a stalled controller into a retryable
	// network error and, once retries are exhausted, a 5xx, as long as
	// attempts × timeout + backoff stays below that budget.
	RequestTimeout time.Duration `env:"UNIFI_REQUEST_TIMEOUT" envDefault:"0"`
}

// isCloud reports whether requests route through the Ubiquiti cloud connector
// (api.ui.com) rather than directly to a local controller. A console ID is the
// operator's explicit opt-in to the cloud path.
func (c Config) isCloud() bool { return c.ConsoleID != "" }

// baseURL returns the URL prefix that every Integration API path is appended
// to. In cloud mode the request is routed through api.ui.com's per-console
// connector proxy; otherwise it hits the controller's own reverse proxy. The
// Integration API surface past this prefix is identical for both.
func (c Config) baseURL() string {
	host := strings.TrimRight(c.Host, "/")
	if !c.isCloud() {
		return host
	}

	return host + "/v1/connector/consoles/" + c.ConsoleID
}

// cloudAPIHosts are the Ubiquiti cloud connector endpoints. Pointing UNIFI_HOST
// at one of these without a console ID can't work — the connector needs to know
// which console to proxy to.
var cloudAPIHosts = map[string]bool{
	"api.ui.com":     true,
	"api.dev.ui.com": true,
	"api.stg.ui.com": true,
}

// Validate checks that the UniFi configuration is internally consistent. It is
// called once at provider construction so misconfiguration fails startup
// rather than surfacing as confusing request-time errors.
func (c Config) Validate() error {
	u, err := url.Parse(c.Host)
	if err != nil {
		return fmt.Errorf("UNIFI_HOST %q is not a valid URL: %w", c.Host, err)
	}
	if u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("UNIFI_HOST %q must include a scheme and host (e.g. https://unifi.local)", c.Host)
	}
	if cloudAPIHosts[u.Host] && c.ConsoleID == "" {
		return fmt.Errorf("UNIFI_CONSOLE_ID is required when UNIFI_HOST points at the Ubiquiti cloud API (%q)", u.Host)
	}
	if c.RetryAttempts < 1 || c.RetryAttempts > maxRetryAttempts {
		return fmt.Errorf("UNIFI_RETRY_ATTEMPTS must be between 1 and %d, got %d", maxRetryAttempts, c.RetryAttempts)
	}
	if c.ApplyWorkers < 1 || c.ApplyWorkers > maxApplyWorkers {
		return fmt.Errorf("UNIFI_APPLY_WORKERS must be between 1 and %d, got %d", maxApplyWorkers, c.ApplyWorkers)
	}
	if c.RetryInitialDelay < minRetryInitialDelay {
		return fmt.Errorf("UNIFI_RETRY_INITIAL_DELAY must be >= %s, got %s", minRetryInitialDelay, c.RetryInitialDelay)
	}
	if c.RetryMaxDelay < c.RetryInitialDelay {
		return fmt.Errorf("UNIFI_RETRY_MAX_DELAY (%s) must be >= UNIFI_RETRY_INITIAL_DELAY (%s)",
			c.RetryMaxDelay, c.RetryInitialDelay)
	}
	if c.RequestTimeout < 0 {
		return fmt.Errorf("UNIFI_REQUEST_TIMEOUT must be >= 0, got %s", c.RequestTimeout)
	}

	return nil
}

// DNSRecord is the internal representation of a record used by the provider
// layer. Wire-level type-switching (per record type DTOs, SRV/MX field splits)
// happens in dto.go; the provider only ever sees this shape.
//
// For SRV records, Value carries "priority weight port serverDomain".
// For MX records, Value carries "priority mailServerDomain".
// For all other types, Value is the raw target.
type DNSRecord struct {
	ID         string
	Key        string
	RecordType string
	TTL        endpoint.TTL
	Value      string
}

// apiErrorResponse matches the Integration API error envelope.
//
//nolint:tagliatelle // UniFi API field names cannot be changed
type apiErrorResponse struct {
	StatusCode  int    `json:"statusCode"`
	StatusName  string `json:"statusName"`
	Code        string `json:"code"`
	Message     string `json:"message"`
	Timestamp   string `json:"timestamp"`
	RequestPath string `json:"requestPath"`
	RequestID   string `json:"requestId"`
}
