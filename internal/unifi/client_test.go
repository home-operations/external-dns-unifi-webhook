package unifi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/home-operations/external-dns-unifi-webhook/internal/metrics"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"sigs.k8s.io/external-dns/endpoint"
)

func init() {
	// Force lazy initialization of the metrics singleton so handler paths can
	// rely on metrics.Get() returning a non-nil instance.
	_ = metrics.Get()
}

const (
	testDomain = "test.example.com"
)

// newTestClient builds an httpClient pointed at the given test server. Real
// client construction goes through newUnifiClient's startup probe; tests skip
// that by populating siteName directly.
func newTestClient(srv *httptest.Server) *httpClient {
	return testClient(srv, &Config{
		Host:          srv.URL,
		Site:          testSite,
		APIKey:        testKeyA,
		SkipTLSVerify: true,
	})
}

// TestCloudConnectorPath verifies that with a console ID set, every request is
// routed through the api.ui.com connector prefix while reusing the same
// endpoints — the whole point of the cloud support is that only the base URL
// changes.
func TestCloudConnectorPath(t *testing.T) {
	wantPrefix := "/v1/connector/consoles/" + testConsoleID + "/proxy/network"

	var gotPaths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPaths = append(gotPaths, r.URL.Path)
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(listOf(recA(testDNSID1, testDomain, testIPv4A, 300)))
		case http.MethodDelete:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	c := testClient(srv, &Config{
		Host:          srv.URL,
		Site:          testSite,
		APIKey:        testKeyA,
		ConsoleID:     testConsoleID,
		SkipTLSVerify: true,
	})

	if _, err := c.GetEndpoints(context.Background()); err != nil {
		t.Fatalf("GetEndpoints: %v", err)
	}
	if err := c.DeleteRecord(context.Background(), testDNSID1); err != nil {
		t.Fatalf("DeleteRecord: %v", err)
	}

	if len(gotPaths) == 0 {
		t.Fatal("server received no requests")
	}
	for _, p := range gotPaths {
		if !strings.HasPrefix(p, wantPrefix) {
			t.Errorf("request path %q does not start with cloud connector prefix %q", p, wantPrefix)
		}
	}
	// Sanity-check the suffix is the same API surface as local.
	if want := wantPrefix + "/v2/api/site/" + testSite + "/static-dns/" + testDNSID1; gotPaths[len(gotPaths)-1] != want {
		t.Errorf("delete path = %q, want %q", gotPaths[len(gotPaths)-1], want)
	}
}

// record helpers keep test data terse and consistent.
func recA(id, key, ip string, ttl int) staticDNSRecord {
	return staticDNSRecord{
		ID: id, RecordType: recordTypeA, Enabled: true,
		Key: key, Value: ip, TTL: &ttl,
	}
}

func recAAAA(id, key, ip string, ttl int) staticDNSRecord {
	return staticDNSRecord{
		ID: id, RecordType: recordTypeAAAA, Enabled: true,
		Key: key, Value: ip, TTL: &ttl,
	}
}

func recCNAME(id, key, target string, ttl int) staticDNSRecord {
	return staticDNSRecord{
		ID: id, RecordType: recordTypeCNAME, Enabled: true,
		Key: key, Value: target, TTL: &ttl,
	}
}

func recTXT(id, key, text string) staticDNSRecord {
	return staticDNSRecord{
		ID: id, RecordType: recordTypeTXT, Enabled: true,
		Key: key, Value: text,
	}
}

func recSRV(id, key, server string, priority, weight, port int) staticDNSRecord {
	return staticDNSRecord{
		ID: id, RecordType: recordTypeSRV, Enabled: true,
		Key: key, Value: server,
		Priority: &priority,
		Weight:   &weight,
		Port:     &port,
	}
}

// listOf is the /static-dns list response: a bare JSON array.
func listOf(recs ...staticDNSRecord) []staticDNSRecord {
	return append([]staticDNSRecord{}, recs...)
}

// TestGetEndpoints exercises the list path against a faked server.
func TestGetEndpoints(t *testing.T) {
	srvPriority, srvWeight, srvPort := 10, 20, 8080
	srvRec := recSRV("srv1", testSRVName, testTarget, srvPriority, srvWeight, srvPort)

	tests := []struct {
		name           string
		responseBody   any
		responseStatus int
		expectedLen    int
		expectedErr    bool
		validateResult func(*testing.T, []DNSRecord)
	}{
		{
			name: "successful fetch with A records",
			responseBody: listOf(
				recA(testDNSID1, testDomain, testIPv4A, 300),
				recA(testDNSID2, "test2.example.com", testIPv4B, 600),
			),
			responseStatus: http.StatusOK,
			expectedLen:    2,
			validateResult: func(t *testing.T, records []DNSRecord) {
				t.Helper()
				if records[0].Key != testDomain {
					t.Errorf("First record Key = %q, want %q", records[0].Key, testDomain)
				}
				if records[0].RecordType != recordTypeA {
					t.Errorf("First record RecordType = %q, want A", records[0].RecordType)
				}
			},
		},
		{
			name:           "SRV record reassembled into external-dns shape",
			responseBody:   listOf(srvRec),
			responseStatus: http.StatusOK,
			expectedLen:    1,
			validateResult: func(t *testing.T, records []DNSRecord) {
				t.Helper()
				if records[0].Key != testSRVName {
					t.Errorf("SRV Key = %q, want %q", records[0].Key, testSRVName)
				}
				expected := "10 20 8080 target.example.com"
				if records[0].Value != expected {
					t.Errorf("SRV Value = %q, want %q", records[0].Value, expected)
				}
			},
		},
		{
			name: "unmanaged record type is filtered out",
			responseBody: listOf(
				recA(testDNSID1, testDomain, testIPv4A, 300),
				staticDNSRecord{
					ID: "ns1", RecordType: "NS", Enabled: true,
					Key: "example.com", Value: "ns1.example.com",
				},
			),
			responseStatus: http.StatusOK,
			expectedLen:    1,
		},
		{
			name:           "empty response",
			responseBody:   listOf(),
			responseStatus: http.StatusOK,
			expectedLen:    0,
		},
		{
			name:           "invalid JSON response",
			responseBody:   `{"invalid": json}`,
			responseStatus: http.StatusOK,
			expectedLen:    0,
			expectedErr:    true,
		},
		{
			name: "HTTP 500 error",
			responseBody: apiErrorResponse{
				StatusCode: 500, StatusName: "INTERNAL_SERVER_ERROR",
				Code: testErrCode, Message: testMsgInternalServer,
			},
			responseStatus: http.StatusInternalServerError,
			expectedLen:    0,
			expectedErr:    true,
		},
		{
			name: "mixed record types",
			responseBody: listOf(
				recA("a1", "a.example.com", "1.2.3.4", 300),
				recCNAME(testCNAMEID, "cname.example.com", testTarget, 300),
				recTXT("txt1", "txt.example.com", "v=spf1 include:example.com ~all"),
			),
			responseStatus: http.StatusOK,
			expectedLen:    3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.responseStatus)
				if strBody, ok := tt.responseBody.(string); ok {
					_, _ = w.Write([]byte(strBody))

					return
				}
				_ = json.NewEncoder(w).Encode(tt.responseBody)
			}))
			defer server.Close()

			client := newTestClient(server)

			records, err := client.GetEndpoints(context.Background())

			if tt.expectedErr && err == nil {
				t.Error("GetEndpoints() expected error, got nil")
			}
			if !tt.expectedErr && err != nil {
				t.Errorf("GetEndpoints() unexpected error: %v", err)
			}

			if len(records) != tt.expectedLen {
				t.Errorf("GetEndpoints() returned %d records, want %d", len(records), tt.expectedLen)
			}

			if tt.validateResult != nil && len(records) > 0 {
				tt.validateResult(t, records)
			}
		})
	}
}

// validateARequest checks an A-record POST body has the expected shape.
func validateARequest(t *testing.T, body []byte) {
	t.Helper()
	var rec staticDNSRecord
	if err := json.Unmarshal(body, &rec); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if rec.RecordType != recordTypeA {
		t.Errorf("RecordType = %q, want %q", rec.RecordType, recordTypeA)
	}
	if rec.Key != testDomain {
		t.Errorf("Key = %q, want %q", rec.Key, testDomain)
	}
	if rec.Value != testIPv4A {
		t.Errorf("Value = %q, want %q", rec.Value, testIPv4A)
	}
	if rec.TTL == nil || *rec.TTL != 300 {
		t.Errorf("TTL = %v, want 300", rec.TTL)
	}
}

// validateSRVRequest checks an SRV-record POST body keeps the full name and
// splits the target into its fields.
func validateSRVRequest(t *testing.T, body []byte) {
	t.Helper()
	var rec staticDNSRecord
	if err := json.Unmarshal(body, &rec); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if rec.Key != testSRVName {
		t.Errorf("Key = %q, want %q", rec.Key, testSRVName)
	}
	if rec.Priority == nil || *rec.Priority != 10 {
		t.Errorf("Priority = %v, want 10", rec.Priority)
	}
	if rec.Weight == nil || *rec.Weight != 20 {
		t.Errorf("Weight = %v, want 20", rec.Weight)
	}
	if rec.Port == nil || *rec.Port != 8080 {
		t.Errorf("Port = %v, want 8080", rec.Port)
	}
	if rec.Value != testTarget {
		t.Errorf("Value = %q, want %q", rec.Value, testTarget)
	}
}

// validateTTLClamped checks A-record TTL gets clamped at the maximum.
func validateTTLClamped(t *testing.T, body []byte) {
	t.Helper()
	var rec staticDNSRecord
	if err := json.Unmarshal(body, &rec); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if rec.TTL == nil || *rec.TTL != ttlMaxA {
		t.Errorf("TTL = %v, want %d", rec.TTL, ttlMaxA)
	}
}

// TestCreateEndpoint tests the per-type record construction on the create
// path, plus error handling for invalid SRV/MX targets.
func TestCreateEndpoint(t *testing.T) {
	tests := []struct {
		name           string
		endpoint       *endpoint.Endpoint
		responseRec    staticDNSRecord
		responseStatus int
		expectedErr    bool
		validateReq    func(*testing.T, []byte)
	}{
		{
			name:           "create A record",
			endpoint:       endpoint.NewEndpointWithTTL(testDomain, "A", 300, testIPv4A),
			responseRec:    recA("new-record-1", testDomain, testIPv4A, 300),
			responseStatus: http.StatusOK,
			validateReq:    validateARequest,
		},
		{
			name:           "create CNAME record",
			endpoint:       endpoint.NewEndpointWithTTL(testAlias, "CNAME", 600, testTarget),
			responseRec:    recCNAME("new-cname-1", testAlias, testTarget, 600),
			responseStatus: http.StatusOK,
		},
		{
			name:           "create SRV record splits value into fields",
			endpoint:       endpoint.NewEndpointWithTTL(testSRVName, "SRV", 300, "10 20 8080 target.example.com"),
			responseRec:    recSRV("new-srv-1", testSRVName, testTarget, 10, 20, 8080),
			responseStatus: http.StatusOK,
			validateReq:    validateSRVRequest,
		},
		{
			name:           "TTL clamped to A-record maximum",
			endpoint:       endpoint.NewEndpointWithTTL(testDomain, "A", 999999, testIPv4A),
			responseRec:    recA("a-clamped", testDomain, testIPv4A, ttlMaxA),
			responseStatus: http.StatusOK,
			validateReq:    validateTTLClamped,
		},
		{
			name: "create multiple CNAME targets - uses only first",
			endpoint: endpoint.NewEndpointWithTTL(
				testMultiName, "CNAME", 300,
				"target1.example.com", "target2.example.com",
			),
			responseRec:    recCNAME("new-record", testMultiName, "target1.example.com", 300),
			responseStatus: http.StatusOK,
		},
		{
			name:           "HTTP 400 error",
			endpoint:       endpoint.NewEndpointWithTTL(testDomain, "A", 300, testIPv4A),
			responseRec:    staticDNSRecord{},
			responseStatus: http.StatusBadRequest,
			expectedErr:    true,
		},
		{
			name:           "invalid SRV format",
			endpoint:       endpoint.NewEndpointWithTTL(testSRVName, "SRV", 300, "invalid srv format"),
			responseRec:    staticDNSRecord{},
			responseStatus: http.StatusOK,
			expectedErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedBody []byte
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Body != nil {
					capturedBody, _ = io.ReadAll(r.Body)
				}
				w.WriteHeader(tt.responseStatus)
				if tt.responseStatus >= 400 {
					_ = json.NewEncoder(w).Encode(apiErrorResponse{
						StatusCode: tt.responseStatus, StatusName: "BAD_REQUEST",
						Code: "validation.failed", Message: "Invalid record",
					})

					return
				}
				_ = json.NewEncoder(w).Encode(tt.responseRec)
			}))
			defer server.Close()

			records, err := newTestClient(server).CreateEndpoint(context.Background(), tt.endpoint)

			if tt.expectedErr && err == nil {
				t.Error("CreateEndpoint() expected error, got nil")
			}
			if !tt.expectedErr && err != nil {
				t.Errorf("CreateEndpoint() unexpected error: %v", err)
			}
			if !tt.expectedErr && len(records) == 0 {
				t.Error("CreateEndpoint() returned no records")
			}
			if tt.validateReq != nil && len(capturedBody) > 0 {
				tt.validateReq(t, capturedBody)
			}
		})
	}
}

// TestDeleteRecord covers the single-record DELETE path, including the
// idempotent treatment of 404 responses.
func TestDeleteRecord(t *testing.T) {
	tests := []struct {
		name        string
		recordID    string
		respStatus  int
		expectedErr bool
	}{
		{
			name:       "delete success",
			recordID:   testDNSID1,
			respStatus: http.StatusOK,
		},
		{
			name:       "404 is treated as success (already deleted)",
			recordID:   "missing",
			respStatus: http.StatusNotFound,
		},
		{
			name:        "500 surfaces as error",
			recordID:    testDNSID1,
			respStatus:  http.StatusInternalServerError,
			expectedErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var (
				gotMethod, gotPath string
				deleteCount        int
			)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotMethod = r.Method
				gotPath = r.URL.Path
				deleteCount++
				w.WriteHeader(tt.respStatus)
				if tt.respStatus >= 400 {
					_ = json.NewEncoder(w).Encode(apiErrorResponse{
						StatusCode: tt.respStatus, StatusName: "ERROR",
						Code: testErrCode, Message: "Delete failed",
					})
				}
			}))
			defer server.Close()

			err := newTestClient(server).DeleteRecord(context.Background(), tt.recordID)

			if tt.expectedErr && err == nil {
				t.Error("DeleteRecord() expected error, got nil")
			}
			if !tt.expectedErr && err != nil {
				t.Errorf("DeleteRecord() unexpected error: %v", err)
			}
			if deleteCount != 1 {
				t.Errorf("DeleteRecord() made %d DELETE calls, want 1", deleteCount)
			}
			if gotMethod != http.MethodDelete {
				t.Errorf("method = %q, want DELETE", gotMethod)
			}
			if want := "/v2/api/site/" + testSite + "/static-dns/" + tt.recordID; !strings.HasSuffix(gotPath, want) {
				t.Errorf("path = %q, expected suffix %s", gotPath, want)
			}
		})
	}
}

// TestCreateEndpoint_V2ErrorEnvelope verifies the /static-dns error shape
// ({code, errorCode, message, details}) surfaces as an APIError that keeps the
// controller's code and message, rather than the raw body.
func TestCreateEndpoint_V2ErrorEnvelope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(headerContentType, contentTypeJSON)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"code":"api.err.InvalidPayload","details":{"key":"bad"},"errorCode":400,"message":"Invalid payload"}`))
	}))
	defer srv.Close()

	ep := endpoint.NewEndpointWithTTL(testDomain, recordTypeA, 300, testIPv4A)
	_, err := newTestClient(srv).CreateEndpoint(context.Background(), ep)
	if err == nil {
		t.Fatal("expected an error from a 400 response")
	}

	apiErr, ok := errors.AsType[*APIError](err)
	if !ok {
		t.Fatalf("expected *APIError, got %T: %v", err, err)
	}
	if apiErr.StatusCode != http.StatusBadRequest {
		t.Errorf("StatusCode = %d, want 400", apiErr.StatusCode)
	}
	if want := "api.err.InvalidPayload: Invalid payload"; apiErr.Message != want {
		t.Errorf("Message = %q, want %q", apiErr.Message, want)
	}
}

// TestNewUnifiClient_ProbesSite exercises the real constructor: UNIFI_SITE
// goes into the /static-dns path as-is, the constructor lists that path once
// as a startup probe, and a probe failure fails construction. Both local and
// cloud mode are covered.
func TestNewUnifiClient_ProbesSite(t *testing.T) {
	const siteName = "branch"

	for _, mode := range []struct {
		name      string
		consoleID string
		prefix    string
	}{
		{name: "local"},
		{name: "cloud", consoleID: testConsoleID, prefix: "/v1/connector/consoles/" + testConsoleID},
	} {
		t.Run(mode.name, func(t *testing.T) {
			var gotPaths []string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPaths = append(gotPaths, r.Method+" "+r.URL.Path)
				w.Header().Set(headerContentType, contentTypeJSON)
				switch r.Method {
				case http.MethodGet:
					_ = json.NewEncoder(w).Encode(listOf(recA(testDNSID1, testDomain, testIPv4A, 300)))
				case http.MethodPost:
					_ = json.NewEncoder(w).Encode(recA(testDNSID1, testDomain, testIPv4A, 300))
				case http.MethodDelete:
					w.WriteHeader(http.StatusOK)
				}
			}))
			defer srv.Close()

			c, err := newUnifiClient(&Config{
				Host: srv.URL, Site: siteName, APIKey: testKeyA, ConsoleID: mode.consoleID,
				RetryAttempts: 1, RetryInitialDelay: time.Millisecond, RetryMaxDelay: time.Millisecond,
			})
			if err != nil {
				t.Fatalf("newUnifiClient: %v", err)
			}
			if c.siteName != siteName {
				t.Fatalf("siteName = %q, want %q", c.siteName, siteName)
			}

			ctx := context.Background()
			ep := endpoint.NewEndpointWithTTL(testDomain, recordTypeA, 300, testIPv4A)
			if _, err := c.CreateEndpoint(ctx, ep); err != nil {
				t.Fatalf("CreateEndpoint: %v", err)
			}
			if err := c.DeleteRecord(ctx, testDNSID1); err != nil {
				t.Fatalf("DeleteRecord: %v", err)
			}

			records := mode.prefix + "/proxy/network/v2/api/site/" + siteName + "/static-dns"
			want := []string{
				"GET " + records,
				"POST " + records,
				"DELETE " + records + "/" + testDNSID1,
			}
			if !slices.Equal(gotPaths, want) {
				t.Errorf("request paths = %q, want %q", gotPaths, want)
			}
		})
	}

	t.Run("site name is escaped as one path segment", func(t *testing.T) {
		var gotPath string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.EscapedPath()
			w.Header().Set(headerContentType, contentTypeJSON)
			_ = json.NewEncoder(w).Encode(listOf())
		}))
		defer srv.Close()

		if _, err := newUnifiClient(&Config{
			Host: srv.URL, Site: "br anch/x?y#z", APIKey: testKeyA,
			RetryAttempts: 1, RetryInitialDelay: time.Millisecond, RetryMaxDelay: time.Millisecond,
		}); err != nil {
			t.Fatalf("newUnifiClient: %v", err)
		}
		if want := "/proxy/network/v2/api/site/br%20anch%2Fx%3Fy%23z/static-dns"; gotPath != want {
			t.Errorf("probe path = %q, want %q", gotPath, want)
		}
	})

	t.Run("probe failure fails construction", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set(headerContentType, contentTypeJSON)
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"code":"api.err.LoginRequired","errorCode":401,"message":"Login required"}`))
		}))
		defer srv.Close()

		_, err := newUnifiClient(&Config{
			Host: srv.URL, Site: testSite, APIKey: testKeyA,
			RetryAttempts: 1, RetryInitialDelay: time.Millisecond, RetryMaxDelay: time.Millisecond,
		})
		if err == nil {
			t.Fatal("expected construction to fail on a 401 probe")
		}
		if !strings.Contains(err.Error(), "probing UNIFI_SITE") || !strings.Contains(err.Error(), "Login required") {
			t.Errorf("error = %q, want the probe context and the controller message", err)
		}
	})
}

// TestSetHeaders verifies the API-key auth header and standard JSON headers.
func TestSetHeaders(t *testing.T) {
	client := &httpClient{cfg: &Config{APIKey: testKeyB}}
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://example.com", http.NoBody)
	client.setHeaders(req)

	expected := map[string]string{
		"X-API-KEY":       "test-api-key",
		headerAccept:      contentTypeJSON,
		headerContentType: contentTypeJSONUTF8,
	}
	for key, want := range expected {
		if got := req.Header.Get(key); got != want {
			t.Errorf("Header %s = %q, want %q", key, got, want)
		}
	}
}

// TestDecodeJSON_RejectsOversizedBody proves decodeJSON refuses to buffer a
// response larger than maxResponseBytes instead of letting io.ReadAll grow
// unbounded. These two tests are intentionally serial (no t.Parallel) because
// they mutate the package-level cap; Go resumes parallel tests only after the
// serial group finishes, so the deferred restore closes the window first.
func TestDecodeJSON_RejectsOversizedBody(t *testing.T) {
	defer func(orig int64) { maxResponseBytes = orig }(maxResponseBytes)
	maxResponseBytes = 16

	// Valid JSON, but well past the 16-byte cap. Decoding into a generic sink
	// means the only thing that can fail here is the size guard — the old
	// unbounded io.ReadAll would have buffered it all and returned nil.
	oversized := `{"data":[` + strings.Repeat(`"x",`, 100) + `"x"]}`
	resp := &http.Response{Body: io.NopCloser(strings.NewReader(oversized))}

	var dest any
	n, err := decodeJSON(resp, "DNS records", &dest)
	if err == nil {
		t.Fatal("expected error for oversized body, got nil")
	}

	var dataErr *DataError
	if !errors.As(err, &dataErr) {
		t.Fatalf("expected *DataError, got %T: %v", err, err)
	}
	if !strings.Contains(dataErr.Error(), "limit") {
		t.Errorf("error %q should mention the byte limit", dataErr.Error())
	}
	// Rejection must happen before unmarshalling, so dest stays nil.
	if dest != nil {
		t.Errorf("oversized body must not be decoded: got %v", dest)
	}
	// The over-limit read is reported (cap+1), never silently zeroed.
	if int64(n) <= maxResponseBytes {
		t.Errorf("byte count = %d, want > cap %d", n, maxResponseBytes)
	}
}

// TestDecodeJSON_AcceptsBodyAtLimit guards the boundary: a body sitting exactly
// at maxResponseBytes still decodes (the +1 LimitReader headroom is what lets
// it through), so a tight-but-legitimate payload is never rejected.
func TestDecodeJSON_AcceptsBodyAtLimit(t *testing.T) {
	defer func(orig int64) { maxResponseBytes = orig }(maxResponseBytes)

	body := `[]`
	maxResponseBytes = int64(len(body)) // exactly at the cap

	resp := &http.Response{Body: io.NopCloser(strings.NewReader(body))}
	var dest []staticDNSRecord
	if _, err := decodeJSON(resp, "DNS records", &dest); err != nil {
		t.Fatalf("body exactly at the limit should decode, got: %v", err)
	}
}

// TestCreateEndpoint_RecordsResponseSize is the #226 regression: the create
// path must report the decoded response-body size to the UniFi response-size
// histogram. It previously hardcoded 0, so the create_endpoint operation never
// contributed a sample.
func TestCreateEndpoint_RecordsResponseSize(t *testing.T) {
	// Echo the posted record back as the created record so decodeJSON reads a
	// non-empty, well-formed body.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	c := newTestClient(srv)
	// HistogramVec.WithLabelValues returns an Observer; the concrete child also
	// implements prometheus.Metric, which is what exposes Write for readback.
	hist, ok := metrics.Get().UniFiResponseSizeBytes.WithLabelValues("create_endpoint").(prometheus.Metric)
	if !ok {
		t.Fatal("response-size histogram child is not a prometheus.Metric")
	}
	read := func() (uint64, float64) {
		t.Helper()
		var dm dto.Metric
		if err := hist.Write(&dm); err != nil {
			t.Fatalf("histogram.Write: %v", err)
		}

		return dm.GetHistogram().GetSampleCount(), dm.GetHistogram().GetSampleSum()
	}

	beforeCount, beforeSum := read()
	ep := endpoint.NewEndpointWithTTL("a.example.com", recordTypeA, 300, "192.0.2.1")
	if _, err := c.CreateEndpoint(context.Background(), ep); err != nil {
		t.Fatalf("CreateEndpoint: %v", err)
	}
	afterCount, afterSum := read()

	if afterCount != beforeCount+1 {
		t.Errorf("response-size sample count delta = %d, want 1 (create path observed nothing)", afterCount-beforeCount)
	}
	if afterSum <= beforeSum {
		t.Errorf("response-size sum did not grow (%v -> %v); create path still reports 0 bytes", beforeSum, afterSum)
	}
}
