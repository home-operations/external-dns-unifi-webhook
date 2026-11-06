//go:build e2e

// Package e2e drives the actual external-dns-unifi-webhook binary against a
// httptest-served mock UniFi controller and asserts the full external-dns
// wire protocol works end-to-end. It is excluded from normal `go test ./...`
// by the `e2e` build tag; run with:
//
//	go test -tags e2e -timeout 60s ./test/e2e/...
package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const (
	mediaType   = "application/external.dns.webhook+json;version=1"
	startupWait = 10 * time.Second

	recordsPath = "/proxy/network/v2/api/site/default/static-dns"
)

type harness struct {
	t          *testing.T
	bin        string
	webhookURL string
	healthURL  string
	upstream   *httptest.Server
	upstreamHi atomic.Int32 // mock UniFi hit counter
	cmd        *exec.Cmd
	stdoutBuf  bytes.Buffer
	stderrBuf  bytes.Buffer
}

// newHarness compiles the webhook binary into a temp dir, starts a mock
// UniFi controller, then launches the binary pointed at the mock. The
// returned harness exposes the webhook + health URLs and tears everything
// down via t.Cleanup.
func newHarness(t *testing.T) *harness {
	t.Helper()

	h := &harness{t: t}
	h.bin = buildBinary(t)
	h.upstream = startMockUniFi(t, &h.upstreamHi)
	t.Cleanup(h.upstream.Close)

	webhookPort := pickFreePort(t)
	healthPort := pickFreePort(t)
	h.webhookURL = fmt.Sprintf("http://127.0.0.1:%d", webhookPort)
	h.healthURL = fmt.Sprintf("http://127.0.0.1:%d", healthPort)

	ctx, cancel := context.WithCancel(context.Background())
	h.cmd = exec.CommandContext(ctx, h.bin)
	h.cmd.Env = append(os.Environ(),
		"SERVER_HOST=127.0.0.1",
		fmt.Sprintf("SERVER_PORT=%d", webhookPort),
		fmt.Sprintf("HEALTH_SERVER_ADDR=127.0.0.1:%d", healthPort),
		"LOG_LEVEL=warn",
		"LOG_FORMAT=text",
		"UNIFI_HOST="+h.upstream.URL,
		"UNIFI_API_KEY=e2e-key",
		"UNIFI_SITE=default",
		"UNIFI_SKIP_TLS_VERIFY=true",
		"UNIFI_APPLY_WORKERS=2",
		"UNIFI_RETRY_INITIAL_DELAY=10ms",
	)
	h.cmd.Stdout = &h.stdoutBuf
	h.cmd.Stderr = &h.stderrBuf

	if err := h.cmd.Start(); err != nil {
		cancel()
		t.Fatalf("starting binary: %v", err)
	}

	t.Cleanup(func() {
		cancel()
		_ = h.cmd.Wait()
		if t.Failed() {
			t.Logf("binary stdout:\n%s", h.stdoutBuf.String())
			t.Logf("binary stderr:\n%s", h.stderrBuf.String())
		}
	})

	waitForReady(t, h.healthURL)

	return h
}

// buildBinary compiles cmd/external-dns-unifi-webhook into a tempdir and
// returns the path. We compile per-test-run to avoid relying on any prior
// `go build`.
func buildBinary(t *testing.T) string {
	t.Helper()

	bin := filepath.Join(t.TempDir(), "external-dns-unifi-webhook")
	cmd := exec.Command("go", "build", "-o", bin, "../../cmd/external-dns-unifi-webhook")
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("go build: %v", err)
	}

	return bin
}

func pickFreePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("pickFreePort: %v", err)
	}
	defer func() { _ = l.Close() }()

	return l.Addr().(*net.TCPAddr).Port
}

// startMockUniFi returns a httptest server that mimics a UniFi Network
// controller's /v2/api/site/{site}/static-dns endpoints. Hits are tracked so
// tests can assert the binary actually called upstream.
func startMockUniFi(t *testing.T, hits *atomic.Int32) *httptest.Server {
	t.Helper()

	var (
		mu     sync.Mutex
		store  = map[string]map[string]any{}
		nextID int
	)

	mux := http.NewServeMux()

	// List/create — at the collection URL.
	mux.HandleFunc(recordsPath, func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")

		switch r.Method {
		case http.MethodGet:
			mu.Lock()
			records := make([]map[string]any, 0, len(store))
			for _, v := range store {
				records = append(records, v)
			}
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(records)

		case http.MethodPost:
			var rec map[string]any
			if err := json.NewDecoder(r.Body).Decode(&rec); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)

				return
			}
			mu.Lock()
			nextID++
			id := fmt.Sprintf("%024d", nextID)
			rec["_id"] = id
			store[id] = rec
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(rec)

		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})

	// Delete — at the per-record URL.
	mux.HandleFunc(recordsPath+"/", func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")

		if r.Method != http.MethodDelete {
			w.WriteHeader(http.StatusMethodNotAllowed)

			return
		}

		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, recordsPath+"/"), "/")
		mu.Lock()
		_, ok := store[id]
		if ok {
			delete(store, id)
		}
		mu.Unlock()

		if !ok {
			w.WriteHeader(http.StatusNotFound)

			return
		}
		w.WriteHeader(http.StatusOK)
	})

	return httptest.NewServer(mux)
}

func waitForReady(t *testing.T, base string) {
	t.Helper()

	deadline := time.Now().Add(startupWait)
	for time.Now().Before(deadline) {
		resp, err := http.Get(base + "/healthz")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("health server never became ready at %s", base)
}

// ---- the tests --------------------------------------------------------

func TestE2E_NegotiateReturnsDomainFilter(t *testing.T) {
	h := newHarness(t)

	req, _ := http.NewRequest(http.MethodGet, h.webhookURL+"/", nil)
	req.Header.Set("Accept", mediaType)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Negotiate: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !json.Valid(body) {
		t.Errorf("negotiate body is not valid JSON: %s", body)
	}
	if ct := resp.Header.Get("Content-Type"); ct != mediaType {
		t.Errorf("Content-Type = %q, want %q", ct, mediaType)
	}
}

func TestE2E_RecordsRoundTrip(t *testing.T) {
	h := newHarness(t)

	// 1. Empty list to start.
	req, _ := http.NewRequest(http.MethodGet, h.webhookURL+"/records", nil)
	req.Header.Set("Accept", mediaType)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /records: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body=%s", resp.StatusCode, body)
	}

	// 2. Apply a small change set.
	changes := map[string]any{
		"Create": []map[string]any{
			{"dnsName": "alpha.example.com", "recordType": "A", "targets": []string{"192.0.2.1"}, "recordTTL": 300},
			{"dnsName": "beta.example.com", "recordType": "CNAME", "targets": []string{"alpha.example.com"}, "recordTTL": 300},
		},
	}
	payload, _ := json.Marshal(changes)

	req, _ = http.NewRequest(http.MethodPost, h.webhookURL+"/records", bytes.NewReader(payload))
	req.Header.Set("Content-Type", mediaType)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /records: %v", err)
	}
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("ApplyChanges status = %d", resp.StatusCode)
	}

	// 3. Confirm the records came back from a subsequent GET.
	req, _ = http.NewRequest(http.MethodGet, h.webhookURL+"/records", nil)
	req.Header.Set("Accept", mediaType)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /records (after apply): %v", err)
	}
	body, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()

	if !bytes.Contains(body, []byte("alpha.example.com")) || !bytes.Contains(body, []byte("beta.example.com")) {
		t.Errorf("expected both records in subsequent GET, body=%s", body)
	}
}

func TestE2E_RejectsMissingAcceptHeader(t *testing.T) {
	h := newHarness(t)

	req, _ := http.NewRequest(http.MethodGet, h.webhookURL+"/records", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /records: %v", err)
	}
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusNotAcceptable {
		t.Errorf("status = %d, want 406", resp.StatusCode)
	}
}

func TestE2E_BodyLimitReturns413(t *testing.T) {
	h := newHarness(t)

	// MaxBytesReader only trips when the body is actually being consumed —
	// a non-JSON blob fails the decoder before that happens. Build syntactically
	// valid JSON whose total size exceeds the 5 MiB default limit by stuffing
	// a giant string value into a Create entry's dnsName.
	huge := strings.Repeat("x", 6*1024*1024)
	body := []byte(`{"Create":[{"dnsName":"` + huge + `","recordType":"A","targets":["192.0.2.1"]}]}`)

	req, _ := http.NewRequest(http.MethodPost, h.webhookURL+"/records", bytes.NewReader(body))
	req.Header.Set("Content-Type", mediaType)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /records: %v", err)
	}
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413", resp.StatusCode)
	}
}

func TestE2E_ReadyzProbesUpstream(t *testing.T) {
	h := newHarness(t)

	resp, err := http.Get(h.healthURL + "/readyz")
	if err != nil {
		t.Fatalf("GET /readyz: %v", err)
	}
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200 when upstream is healthy", resp.StatusCode)
	}

	// At least one hit must have happened (the boot-time provider sanity
	// call or the /readyz probe itself).
	if h.upstreamHi.Load() == 0 {
		t.Error("expected at least one upstream call by the time /readyz returned")
	}
}
