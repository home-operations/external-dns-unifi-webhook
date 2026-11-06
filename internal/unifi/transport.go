package unifi

import (
	"bytes"
	"cmp"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/home-operations/external-dns-unifi-webhook/internal/metrics"
	extdnshttp "sigs.k8s.io/external-dns/pkg/http"
)

const errorBodyBufferSize = 512

// newHTTPTransport builds the http.Transport used by the UniFi client.
// CACertPath wins over SkipTLSVerify when both are set — operators with a
// proper internal CA should not also be running with verification off.
func newHTTPTransport(cfg *Config) (*http.Transport, error) {
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12}

	switch {
	case cfg.CACertPath != "":
		pem, err := os.ReadFile(cfg.CACertPath)
		if err != nil {
			return nil, fmt.Errorf("reading UNIFI_CA_CERT %q: %w", cfg.CACertPath, err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("UNIFI_CA_CERT %q: no certificates parsed", cfg.CACertPath)
		}
		tlsCfg.RootCAs = pool
		if cfg.SkipTLSVerify {
			slog.Warn("UNIFI_CA_CERT is set. The client ignores UNIFI_SKIP_TLS_VERIFY")
		}
	case cfg.SkipTLSVerify && cfg.isCloud():
		// The cloud connector terminates at api.ui.com, which presents a
		// publicly-trusted certificate. A skipped verification there exposes
		// the API key to a trivial MITM, so we never honor the setting.
		slog.Warn("The client ignores UNIFI_SKIP_TLS_VERIFY for the cloud connector. api.ui.com presents a publicly-trusted certificate")
	case cfg.SkipTLSVerify:
		// The other branches warn when they override the operator's setting.
		// This one warns because verification is off (the default, for
		// self-signed controllers). Operators must see that in the log and
		// know that UNIFI_CA_CERT keeps TLS verified.
		slog.Warn("TLS certificate verification is disabled (UNIFI_SKIP_TLS_VERIFY). Set UNIFI_CA_CERT to verify a self-signed controller")
		//nolint:gosec // Explicit opt-in via UNIFI_SKIP_TLS_VERIFY for self-signed controllers.
		tlsCfg.InsecureSkipVerify = true
	}

	// Bound the connection pool. ApplyChanges fans out up to ApplyWorkers
	// concurrent requests; without a per-host cap a misconfigured worker count
	// could open that many simultaneous connections to the controller and
	// exhaust its connection table. MaxConnsPerHost gives an absolute ceiling
	// (workers is validated <= maxApplyWorkers), and MaxIdleConnsPerHost keeps
	// connections warm for reuse up to the configured concurrency — the net/http
	// default of 2 would otherwise churn connections during a wide reconcile.
	return &http.Transport{
		TLSClientConfig:     tlsCfg,
		MaxConnsPerHost:     maxApplyWorkers,
		MaxIdleConnsPerHost: cmp.Or(cfg.ApplyWorkers, defaultApplyWorkers),
	}, nil
}

// doRequest issues an HTTP request and applies exponential backoff retries
// on 5xx and 429 responses (it honors Retry-After when present). body is the
// raw JSON payload (nil for GET/DELETE) — it is re-read on every retry
// attempt so the caller does not need a rewindable reader.
func (c *httpClient) doRequest(ctx context.Context, method, path string, body []byte) (*http.Response, error) {
	attempts := max(c.cfg.RetryAttempts, 1)

	// The final attempt always returns inside the loop (the give-up branch
	// covers attempt == attempts-1), so the loop itself needs no exit condition.
	for attempt := 0; ; attempt++ {
		if attempt > 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}

		resp, err := c.doOnce(ctx, method, path, body)
		if err == nil && resp.StatusCode < 400 {
			return resp, nil
		}

		// Count every 429 the controller sends, whether or not we retry it —
		// the metric promises "rate-limit responses received", and the final
		// attempt's 429 (or the only one, with UNIFI_RETRY_ATTEMPTS=1) is the
		// one that actually fails the operation.
		if resp != nil && resp.StatusCode == http.StatusTooManyRequests {
			metrics.Get().UniFiRateLimitsTotal.WithLabelValues(metrics.ProviderName, opLabel(path)).Inc()
		}

		retryable, wait := c.retryAfter(method, resp, err, attempt)
		if !retryable || attempt == attempts-1 {
			if err != nil {
				return nil, err
			}

			// errorResponse reads and closes resp.Body itself.
			return c.errorResponse(resp, method, path)
		}

		// We're going to retry — drain and close so the connection can be
		// reused while we wait. Network errors have no body to drain.
		if resp != nil {
			extdnshttp.DrainAndClose(resp.Body)
		}

		status := "network"
		if resp != nil {
			status = strconv.Itoa(resp.StatusCode)
		}
		metrics.Get().UniFiRetriesTotal.WithLabelValues(metrics.ProviderName, opLabel(path), status).Inc()
		slog.Debug("retrying upstream request", "attempt", attempt+1, "wait", wait, "status", status)

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
	}
}

// doOnce performs a single request attempt. The caller decides whether to
// retry based on the returned status code.
func (c *httpClient) doOnce(ctx context.Context, method, path string, body []byte) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, path, reader)
	if err != nil {
		return nil, fmt.Errorf("creating HTTP request: %w", err)
	}
	c.setHeaders(req)

	resp, err := c.httpc.Do(req)
	if err != nil {
		return nil, NewNetworkError(method, path, err)
	}

	return resp, nil
}

// retryAfter returns whether the client must retry a request and how long to
// wait.
//
// The client retries a 429 for any method. The server signals that it rejected
// the request before it processed it, so a second send is safe. The client
// retries 5xx responses and network errors only for idempotent methods
// (GET/DELETE). The controller can commit a POST that creates a DNS record
// before it returns a 5xx or before the connection drops. A blind second send
// of that POST can create a duplicate record. A create without a retry still
// converges, because external-dns retries the whole reconcile on its next
// sync. For a 429, the client honors the Retry-After header from the server,
// clamped to RetryMaxDelay. The other cases use exponential backoff with
// jitter.
func (c *httpClient) retryAfter(method string, resp *http.Response, err error, attempt int) (bool, time.Duration) {
	idempotent := method == http.MethodGet || method == http.MethodDelete

	// Network errors are retryable if they aren't context cancellations — but
	// only for idempotent methods (a POST may have landed before the break).
	if err != nil {
		if IsNetworkError(err) && idempotent {
			return true, c.backoff(attempt, 0)
		}

		return false, 0
	}

	if resp == nil {
		return false, 0
	}

	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		hint := parseRetryAfter(resp.Header.Get("Retry-After"))

		return true, c.backoff(attempt, hint)
	case resp.StatusCode >= 500 && resp.StatusCode < 600 && idempotent:
		// A 5xx on a non-idempotent POST is ambiguous (the write may have
		// committed), so don't auto-retry it.
		return true, c.backoff(attempt, 0)
	}

	return false, 0
}

// backoff returns initial * 2^attempt + jitter, clamped to max. If hint is
// non-zero (Retry-After from the server) it is used as the floor — we never
// retry sooner than the server asked us to.
func (c *httpClient) backoff(attempt int, hint time.Duration) time.Duration {
	initial := cmp.Or(c.cfg.RetryInitialDelay, 500*time.Millisecond)
	maxDelay := cmp.Or(c.cfg.RetryMaxDelay, 10*time.Second)

	base := initial << attempt
	if base <= 0 || base > maxDelay {
		base = maxDelay
	}
	// 0-50% jitter so concurrent workers don't all retry on the same tick.
	// Guard the divisor: rand.Int64N panics on a non-positive argument, which
	// base/2 would be for a sub-2ns base (config validation floors the initial
	// delay at 1ms, but backoff stays self-contained).
	var jitter time.Duration
	if half := int64(base) / 2; half > 0 {
		jitter = time.Duration(rand.Int64N(half))
	}
	wait := base + jitter

	if hint > 0 && hint > wait {
		wait = hint
	}
	if wait > maxDelay {
		wait = maxDelay
	}

	return wait
}

// worstCaseRetryBudget returns an upper bound on the total time doRequest can
// spend sleeping between attempts for the given config. doRequest sleeps once
// after each attempt except the last, so there are at most RetryAttempts-1
// waits. Every individual wait is capped at RetryMaxDelay by backoff(), and any
// wait can be driven all the way up to that cap by a server Retry-After hint on
// a 429 (backoff honors the hint as a floor, then clamps to RetryMaxDelay). So
// the tight, hint-safe upper bound is simply (RetryAttempts-1) * RetryMaxDelay —
// the exponential schedule only ever produces shorter waits, never longer. Used
// to size the startup probe timeout: a fixed cap that ignores this
// budget would cancel retries the operator explicitly asked for and crashloop
// the process.
func worstCaseRetryBudget(cfg *Config) time.Duration {
	attempts := max(cfg.RetryAttempts, 1)
	maxDelay := cmp.Or(cfg.RetryMaxDelay, 10*time.Second)

	return time.Duration(attempts-1) * maxDelay
}

// parseRetryAfter understands both numeric-seconds and HTTP-date forms of
// the Retry-After header. Unparseable values produce zero, which lets the
// caller fall through to ordinary backoff.
func parseRetryAfter(value string) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if secs, err := strconv.Atoi(value); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second
	}
	if when, err := http.ParseTime(value); err == nil {
		if d := time.Until(when); d > 0 {
			return d
		}
	}

	return 0
}

// opLabel reduces a UniFi URL path to a coarse operation tag suitable for a
// metric label. Real-record IDs would explode label cardinality, so we
// collapse the trailing segment.
func opLabel(path string) string {
	switch {
	case strings.Contains(path, "/static-dns"):
		return "static_dns"
	}

	return "other"
}

func (c *httpClient) errorResponse(resp *http.Response, method, path string) (*http.Response, error) {
	// DrainAndClose ensures the rest of the body (beyond errorBodyBufferSize)
	// is drained so the underlying connection can be returned to the pool.
	defer extdnshttp.DrainAndClose(resp.Body)
	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, errorBodyBufferSize))
	if err != nil {
		return nil, NewDataError("read", "error response body", err)
	}

	// The Network API returns JSON for most error cases, but some status codes
	// (e.g. 405 from the Tomcat layer) come back as HTML. Fall back to the raw
	// body so callers still get a useful APIError instead of a parse failure.
	var apiError apiErrorResponse
	message := strings.TrimSpace(string(bodyBytes))
	if err := json.Unmarshal(bodyBytes, &apiError); err == nil && apiError.Message != "" {
		message = formatAPIError(&apiError)
	}

	return nil, NewAPIError(method, path, resp.StatusCode, message)
}

// formatAPIError pulls the human-useful pieces (statusName + code + message)
// out of the new error envelope into one line. Timestamp / requestPath /
// requestId are dropped — they're only useful when correlating with server
// logs, and we already log status + URL ourselves.
func formatAPIError(e *apiErrorResponse) string {
	var prefix string
	switch {
	case e.StatusName != "" && e.Code != "":
		prefix = e.StatusName + " (" + e.Code + ")"
	case e.StatusName != "":
		prefix = e.StatusName
	case e.Code != "":
		prefix = e.Code
	}
	if prefix == "" {
		return e.Message
	}

	return prefix + ": " + e.Message
}

// setHeaders applies the X-API-KEY auth header and the standard JSON headers.
func (c *httpClient) setHeaders(req *http.Request) {
	req.Header.Set("X-API-KEY", c.cfg.APIKey)
	req.Header.Add("Accept", "application/json")
	req.Header.Add("Content-Type", "application/json; charset=utf-8")
}
