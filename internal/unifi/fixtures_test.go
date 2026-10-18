package unifi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// isAPIError and isDataError are test-only predicates over the package's
// typed errors. Production code uses errors.As at the single site that needs
// it (DeleteRecord's 404 check); the tests assert classification more broadly.
func isAPIError(err error) bool {
	_, ok := errors.AsType[*APIError](err)

	return ok
}

func isDataError(err error) bool {
	_, ok := errors.AsType[*DataError](err)

	return ok
}

// fixture reads a captured response from internal/unifi/testdata. Files were
// originally captured from a real UniFi Network controller and then rewritten
// into the v10.3.58 Integration API page envelope shape.
func fixture(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join("testdata", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", path, err)
	}

	return data
}

// testClient builds an httpClient pointed at srv with siteID pre-resolved, so
// tests skip the startup site-resolution round trip.
func testClient(srv *httptest.Server, cfg *Config) *httpClient {
	httpc := srv.Client()
	httpc.Timeout = cfg.RequestTimeout

	return &httpClient{cfg: cfg, httpc: httpc, siteID: testSiteUUID}
}

func fixtureClient(t *testing.T, handler http.HandlerFunc) (*httpClient, func()) {
	t.Helper()
	srv := httptest.NewServer(handler)

	return newTestClient(srv), srv.Close
}

// TestGetEndpoints_RealFixture replays a captured response from a real UniFi
// controller and asserts that the client parses every entry without loss.
// FORWARD_DOMAIN entries are filtered out by the converter.
func TestGetEndpoints_RealFixture(t *testing.T) {
	body := fixture(t, "records_list.json")

	c, cleanup := fixtureClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	})
	defer cleanup()

	records, err := c.GetEndpoints(context.Background())
	if err != nil {
		t.Fatalf("GetEndpoints: %v", err)
	}

	if got, want := len(records), 82; got != want {
		t.Errorf("record count = %d, want %d", got, want)
	}

	counts := map[string]int{}
	for _, r := range records {
		counts[r.RecordType]++
	}
	for kind, want := range map[string]int{"A": 6, "CNAME": 36, "TXT": 40} {
		if counts[kind] != want {
			t.Errorf("%s count = %d, want %d", kind, counts[kind], want)
		}
	}

	// Spot-check a known A record from the fixture.
	found := false
	for _, r := range records {
		if r.Key == "unifi.example.com" && r.RecordType == recordTypeA {
			found = true
			if r.Value != "192.0.2.1" {
				t.Errorf("unifi.example.com value = %q, want 192.0.2.1", r.Value)
			}
		}
	}
	if !found {
		t.Errorf("expected to find an A record for unifi.example.com")
	}
}

// TestProviderRecords_RealFixture exercises the full provider.Records path —
// fixture → httpClient → UnifiProvider grouping → external-dns endpoints —
// to lock in the end-to-end transformation.
func TestProviderRecords_RealFixture(t *testing.T) {
	body := fixture(t, "records_list.json")

	c, cleanup := fixtureClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	})
	defer cleanup()

	p := &UnifiProvider{client: c}

	endpoints, err := p.Records(context.Background())
	if err != nil {
		t.Fatalf("Records: %v", err)
	}

	// Each fixture row maps to exactly one endpoint (no multi-target keys in
	// the real data), so the count should match the converted-record count.
	if got, want := len(endpoints), 82; got != want {
		t.Errorf("endpoint count = %d, want %d", got, want)
	}

	// Verify TXT records keep their external-dns heritage payload intact —
	// the wire format quotes the value, which Go decodes to a literal quoted
	// string. The provider must not strip those quotes.
	var sawHeritage bool
	for _, ep := range endpoints {
		if ep.RecordType == "TXT" && strings.Contains(ep.Targets[0], "heritage=external-dns") {
			sawHeritage = true

			break
		}
	}
	if !sawHeritage {
		t.Errorf("no TXT endpoint carried external-dns heritage payload")
	}
}

// TestSitesFixture verifies resolveSite parses a real-shaped sites response.
func TestSitesFixture(t *testing.T) {
	body := fixture(t, "sites_list.json")

	c, cleanup := fixtureClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	})
	// Clear pre-set siteID so we exercise the resolver path.
	c.siteID = ""
	defer cleanup()

	id, err := c.resolveSite(context.Background(), "default")
	if err != nil {
		t.Fatalf("resolveSite: %v", err)
	}
	if id != testSiteUUID {
		t.Errorf("siteID = %q, want %q", id, testSiteUUID)
	}
}

// TestHandleErrorResponse_HTMLBody verifies that non-JSON error bodies (such
// as the HTML 405 page UniFi returns when the path doesn't accept the verb)
// surface as an APIError carrying the raw body, not a DataError("unmarshal").
func TestHandleErrorResponse_HTMLBody(t *testing.T) {
	body := fixture(t, "records_not_found.html")

	c, cleanup := fixtureClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusMethodNotAllowed)
		_, _ = w.Write(body)
	})
	defer cleanup()

	_, err := c.GetEndpoints(context.Background())
	if err == nil {
		t.Fatal("GetEndpoints succeeded against a 405 response")
	}

	if !isAPIError(err) {
		t.Errorf("expected *APIError, got %T: %v", err, err)
	}
	if isDataError(err) {
		t.Errorf("expected APIError, got DataError — non-JSON body was misclassified: %v", err)
	}
}
