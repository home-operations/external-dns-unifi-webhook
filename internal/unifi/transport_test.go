package unifi

import (
	"context"
	"encoding/pem"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/home-operations/external-dns-unifi-webhook/internal/metrics"
	dto "github.com/prometheus/client_model/go"
)

func newRetryClient(t *testing.T, srv *httptest.Server, cfg *Config) *httpClient {
	t.Helper()
	_ = metrics.Get()

	if cfg == nil {
		cfg = &Config{}
	}
	cfg.Host = srv.URL
	cfg.Site = "default"
	cfg.APIKey = "test-key"
	cfg.SkipTLSVerify = true
	if cfg.RetryAttempts == 0 {
		cfg.RetryAttempts = 3
	}
	if cfg.RetryInitialDelay == 0 {
		cfg.RetryInitialDelay = 1 * time.Millisecond
	}
	if cfg.RetryMaxDelay == 0 {
		cfg.RetryMaxDelay = 10 * time.Millisecond
	}

	return testClient(srv, cfg)
}

func TestDoRequest_RetriesOn5xxThenSucceeds(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) <= 2 {
			w.WriteHeader(http.StatusBadGateway)

			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	c := newRetryClient(t, srv, nil)

	resp, err := c.doRequest(context.Background(), http.MethodGet, srv.URL+"/static-dns/", nil)
	if err != nil {
		t.Fatalf("doRequest: %v", err)
	}
	_ = resp.Body.Close()

	if got := calls.Load(); got != 3 {
		t.Errorf("expected 3 attempts (2 retries), got %d", got)
	}
}

func TestDoRequest_NoRetryOn4xx(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message":"bad"}`))
	}))
	defer srv.Close()

	c := newRetryClient(t, srv, nil)

	_, err := c.doRequest(context.Background(), http.MethodGet, srv.URL+"/static-dns/", nil)
	if err == nil {
		t.Fatal("expected error on 400")
	}
	if !isAPIError(err) {
		t.Errorf("expected APIError, got %T: %v", err, err)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("4xx should not retry: got %d calls, want 1", got)
	}
}

// TestDoRequest_PostNotRetriedOn5xx verifies a non-idempotent POST is NOT
// retried on a 5xx. The controller may have committed the create before
// returning the 5xx, so re-sending could duplicate the DNS record; external-dns
// recovers via its own reconcile loop instead. (GET/DELETE still retry — see
// TestDoRequest_RetriesOn5xxThenSucceeds.)
func TestDoRequest_PostNotRetriedOn5xx(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"message":"upstream"}`))
	}))
	defer srv.Close()

	c := newRetryClient(t, srv, nil)

	_, err := c.doRequest(context.Background(), http.MethodPost, srv.URL+"/dns/policies", []byte(`{}`))
	if err == nil {
		t.Fatal("expected an error on 502")
	}
	if !isAPIError(err) {
		t.Errorf("expected APIError, got %T: %v", err, err)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("POST must not retry on 5xx: got %d attempts, want 1", got)
	}
}

// TestDoRequest_PostRetriedOn429 verifies a POST IS still retried on 429: the
// server rejected the request before processing, so re-sending is safe (and the
// body is re-read each attempt).
func TestDoRequest_PostRetriedOn429(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)

			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := newRetryClient(t, srv, nil)

	resp, err := c.doRequest(context.Background(), http.MethodPost, srv.URL+"/dns/policies", []byte(`{}`))
	if err != nil {
		t.Fatalf("doRequest: %v", err)
	}
	_ = resp.Body.Close()

	if got := calls.Load(); got != 2 {
		t.Errorf("POST should retry on 429: got %d attempts, want 2", got)
	}
}

func TestDoRequest_HonorsRetryAfterOn429(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "1") // 1 second hint
			w.WriteHeader(http.StatusTooManyRequests)

			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	c := newRetryClient(t, srv, &Config{
		RetryAttempts:     3,
		RetryInitialDelay: 1 * time.Millisecond,
		RetryMaxDelay:     2 * time.Second, // big enough to let Retry-After through
	})

	start := time.Now()
	resp, err := c.doRequest(context.Background(), http.MethodGet, srv.URL+"/static-dns/", nil)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("doRequest: %v", err)
	}
	_ = resp.Body.Close()

	// We asked for 1s; allow a small clock-skew margin.
	if elapsed < 900*time.Millisecond {
		t.Errorf("retry returned too fast (%v); Retry-After was 1s", elapsed)
	}
}

func TestDoRequest_GivesUpAfterMaxAttempts(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"message":"down"}`))
	}))
	defer srv.Close()

	c := newRetryClient(t, srv, &Config{RetryAttempts: 2})

	_, err := c.doRequest(context.Background(), http.MethodGet, srv.URL+"/static-dns/", nil)
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	if !isAPIError(err) {
		t.Errorf("expected APIError surfaced from final attempt, got %T: %v", err, err)
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("expected exactly RetryAttempts=2 calls, got %d", got)
	}
}

func TestParseRetryAfter(t *testing.T) {
	tests := []struct {
		input string
		want  time.Duration
	}{
		{"", 0},
		{"3", 3 * time.Second},
		{"   5  ", 5 * time.Second},
		{"-1", 0},
		{"nope", 0},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseRetryAfter(tt.input)
			if got != tt.want {
				t.Errorf("parseRetryAfter(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestBackoff_ClampsToMax(t *testing.T) {
	c := &httpClient{cfg: &Config{
		RetryInitialDelay: 1 * time.Second,
		RetryMaxDelay:     2 * time.Second,
	}}

	// attempt=10 would be 1s << 10 = 1024s if unclamped.
	got := c.backoff(10, 0)
	if got > 2*time.Second {
		t.Errorf("backoff did not clamp: %v > 2s", got)
	}
}

// TestBackoff_NoPanicOnSubTwoNanoDelay is the #230 regression: a sub-2ns
// initial delay makes base/2 == 0, and rand.Int64N panics on a non-positive
// argument. backoff must guard the divisor and still return a sane wait.
func TestBackoff_NoPanicOnSubTwoNanoDelay(t *testing.T) {
	c := &httpClient{cfg: &Config{
		RetryInitialDelay: 1 * time.Nanosecond,
		RetryMaxDelay:     1 * time.Second,
	}}

	got := c.backoff(0, 0) // base = 1ns, base/2 = 0 → would panic pre-fix
	if got < 0 {
		t.Errorf("backoff returned negative wait %v", got)
	}
}

func TestWorstCaseRetryBudget(t *testing.T) {
	// The bound is (attempts-1) * RetryMaxDelay: each of the attempts-1 waits
	// can be driven to RetryMaxDelay by a 429 Retry-After hint, so the initial
	// delay does not enter into the worst case.
	tests := []struct {
		name string
		cfg  *Config
		want time.Duration
	}{
		{
			name: "single attempt has no backoff waits",
			cfg:  &Config{RetryAttempts: 1, RetryInitialDelay: 500 * time.Millisecond, RetryMaxDelay: 10 * time.Second},
			want: 0,
		},
		{
			name: "three attempts: two waits at the ceiling",
			cfg:  &Config{RetryAttempts: 3, RetryInitialDelay: 500 * time.Millisecond, RetryMaxDelay: 10 * time.Second},
			want: 20 * time.Second,
		},
		{
			name: "six attempts: five waits at the ceiling",
			cfg:  &Config{RetryAttempts: 6, RetryInitialDelay: 500 * time.Millisecond, RetryMaxDelay: 10 * time.Second},
			want: 50 * time.Second,
		},
		{
			name: "a tiny initial delay does not shrink the bound",
			cfg:  &Config{RetryAttempts: 3, RetryInitialDelay: 1 * time.Millisecond, RetryMaxDelay: 10 * time.Second},
			want: 20 * time.Second,
		},
		{
			name: "zero RetryMaxDelay falls back to the 10s default",
			cfg:  &Config{RetryAttempts: 3},
			want: 20 * time.Second,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := worstCaseRetryBudget(tt.cfg); got != tt.want {
				t.Errorf("worstCaseRetryBudget = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestSiteResolveTimeoutFor_CoversRetryBudget is the #223 regression: the
// startup probe timeout must scale with the operator's retry settings instead
// of being a fixed cap that cancels configured retries mid-backoff.
func TestSiteResolveTimeoutFor_CoversRetryBudget(t *testing.T) {
	// A retry budget the old fixed 15s site-resolution cap would have truncated.
	cfg := &Config{RetryAttempts: 6, RetryInitialDelay: 500 * time.Millisecond, RetryMaxDelay: 10 * time.Second}

	budget := worstCaseRetryBudget(cfg)
	if budget <= siteResolveBaseTimeout {
		t.Fatalf("test premise broken: budget %v should exceed the %v base headroom", budget, siteResolveBaseTimeout)
	}

	got := siteResolveTimeoutFor(cfg)
	if want := siteResolveBaseTimeout + budget; got != want {
		t.Errorf("siteResolveTimeoutFor = %v, want base %v + budget %v = %v", got, siteResolveBaseTimeout, budget, want)
	}
	if got <= budget {
		t.Errorf("startup timeout %v must leave request headroom beyond the retry budget %v", got, budget)
	}

	// With retries disabled the timeout collapses to just the base headroom.
	none := &Config{RetryAttempts: 1, RetryInitialDelay: 500 * time.Millisecond, RetryMaxDelay: 10 * time.Second}
	if got := siteResolveTimeoutFor(none); got != siteResolveBaseTimeout {
		t.Errorf("no-retry timeout = %v, want base %v", got, siteResolveBaseTimeout)
	}
}

func TestNewHTTPTransport_SkipTLSVerify(t *testing.T) {
	tr, err := newHTTPTransport(&Config{SkipTLSVerify: true})
	if err != nil {
		t.Fatalf("newHTTPTransport: %v", err)
	}
	if !tr.TLSClientConfig.InsecureSkipVerify {
		t.Error("expected InsecureSkipVerify=true")
	}
}

func TestNewHTTPTransport_CloudIgnoresSkipTLSVerify(t *testing.T) {
	// In cloud mode UNIFI_SKIP_TLS_VERIFY must not weaken verification — the
	// connector terminates at api.ui.com behind a public CA.
	tr, err := newHTTPTransport(&Config{SkipTLSVerify: true, ConsoleID: testConsoleID})
	if err != nil {
		t.Fatalf("newHTTPTransport: %v", err)
	}
	if tr.TLSClientConfig.InsecureSkipVerify {
		t.Error("cloud mode must not honour UNIFI_SKIP_TLS_VERIFY")
	}
}

func TestNewHTTPTransport_CACertFromFile(t *testing.T) {
	// httptest's TLS cert is a good stand-in CA for this test — we just need
	// some valid PEM the file loader can parse.
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
	pemFile := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(pemFile, pemBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	tr, err := newHTTPTransport(&Config{CACertPath: pemFile})
	if err != nil {
		t.Fatalf("newHTTPTransport: %v", err)
	}
	if tr.TLSClientConfig.InsecureSkipVerify {
		t.Error("CA cert should not coexist with InsecureSkipVerify")
	}
	if tr.TLSClientConfig.RootCAs == nil {
		t.Error("RootCAs not populated")
	}
}

func TestNewHTTPTransport_MissingCAFile(t *testing.T) {
	_, err := newHTTPTransport(&Config{CACertPath: "/nonexistent/ca.pem"})
	if err == nil {
		t.Error("expected error for missing CA file")
	}
}

// TestNewHTTPTransport_BoundsConnections verifies the transport caps per-host
// connections so a worker count can't translate into unbounded connections to
// the controller, and keeps idle connections warm up to the configured
// concurrency (instead of net/http's default of 2).
func TestNewHTTPTransport_BoundsConnections(t *testing.T) {
	tr, err := newHTTPTransport(&Config{SkipTLSVerify: true, ApplyWorkers: 7})
	if err != nil {
		t.Fatalf("newHTTPTransport: %v", err)
	}
	if tr.MaxConnsPerHost != maxApplyWorkers {
		t.Errorf("MaxConnsPerHost = %d, want %d (absolute ceiling)", tr.MaxConnsPerHost, maxApplyWorkers)
	}
	if tr.MaxIdleConnsPerHost != 7 {
		t.Errorf("MaxIdleConnsPerHost = %d, want 7 (the configured worker count)", tr.MaxIdleConnsPerHost)
	}
}

// TestDoRequest_NetworkErrorRetries verifies that connection-refused style
// errors are retried (not just HTTP error responses).
func TestDoRequest_NetworkErrorRetries(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[]`))
	}))
	// Close immediately — any request will hit a closed connection.
	srv.Close()

	c := newRetryClient(t, srv, &Config{RetryAttempts: 2})

	_, err := c.doRequest(context.Background(), http.MethodGet, srv.URL+"/static-dns/", nil)
	if err == nil {
		t.Fatal("expected error against closed server")
	}
	if _, ok := errors.AsType[*NetworkError](err); !ok {
		t.Errorf("expected NetworkError, got %T: %v", err, err)
	}
}

// TestDoRequest_RequestTimeoutRetriesThenFails: a controller that accepts the
// connection but never answers must trip the per-attempt timeout, be retried
// like any other network error, and finally surface as a NetworkError rather
// than hanging until the caller's context expires.
func TestDoRequest_RequestTimeoutRetriesThenFails(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		// Hold the request until the client gives up and closes the connection.
		<-r.Context().Done()
	}))
	defer srv.Close()

	c := newRetryClient(t, srv, &Config{RetryAttempts: 2, RequestTimeout: 250 * time.Millisecond})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := c.doRequest(ctx, http.MethodGet, srv.URL+"/static-dns/", nil)
	if err == nil {
		t.Fatal("expected error against a stalled server")
	}
	if _, ok := errors.AsType[*NetworkError](err); !ok {
		t.Errorf("expected NetworkError, got %T: %v", err, err)
	}
	if ne, ok := errors.AsType[net.Error](err); !ok || !ne.Timeout() {
		t.Errorf("expected a timeout error, got %T: %v", err, err)
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("expected 2 attempts, got %d", got)
	}
}

// TestDoRequest_FinalAttempt429IsCounted: every 429 the controller sends must
// increment the rate-limit counter — including the one on the final (or only)
// attempt, which previously exited through the give-up branch uncounted. With
// UNIFI_RETRY_ATTEMPTS=1, rate limits were never counted at all.
func TestDoRequest_FinalAttempt429IsCounted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	rateLimits := metrics.Get().UniFiRateLimitsTotal.WithLabelValues(metrics.ProviderName, "dns_policies")
	read := func() float64 {
		t.Helper()
		var dm dto.Metric
		if err := rateLimits.Write(&dm); err != nil {
			t.Fatalf("counter.Write: %v", err)
		}

		return dm.GetCounter().GetValue()
	}
	before := read()

	c := newRetryClient(t, srv, &Config{RetryAttempts: 1})
	_, err := c.doRequest(context.Background(), http.MethodGet,
		srv.URL+"/proxy/network/integration/v1/sites/x/dns/policies", nil)
	if err == nil {
		t.Fatal("expected an error from an all-429 upstream")
	}

	if got := read() - before; got != 1 {
		t.Errorf("rate-limit counter delta = %v, want 1 (the final attempt's 429 must be counted)", got)
	}
}
