package unifi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/home-operations/external-dns-unifi-webhook/internal/metrics"
	dto "github.com/prometheus/client_model/go"
	"sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/plan"
)

func init() {
	_ = metrics.Get()
}

// TestApplyChanges_DeletesShareSnapshot proves the indexing refactor: deleting
// N endpoints triggers exactly one GET (the snapshot) plus one DELETE per
// underlying record ID. The old list-then-match-per-call path would have
// produced N+1 GETs.
func TestApplyChanges_DeletesShareSnapshot(t *testing.T) {
	var (
		getCount    atomic.Int32
		deleteCount atomic.Int32
		seenIDs     sync.Map
	)

	existing := []staticDNSRecord{
		recA("id-a-1", "a.example.com", "192.0.2.1", 300),
		recA("id-a-2", "a.example.com", "192.0.2.2", 300), // same key+type, two IDs
		recA("id-b-1", "b.example.com", "192.0.2.3", 300),
		recA("id-c-1", "c.example.com", "192.0.2.4", 300),
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			getCount.Add(1)
			_ = json.NewEncoder(w).Encode(listOf(existing...))
		case http.MethodDelete:
			deleteCount.Add(1)
			// Capture which ID was hit so we can assert exact targeting.
			seenIDs.Store(r.URL.Path, struct{}{})
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	p := &UnifiProvider{client: newTestClient(srv), workers: 4}

	changes := &plan.Changes{
		Delete: []*endpoint.Endpoint{
			endpoint.NewEndpoint("a.example.com", "A", "192.0.2.1"),
			endpoint.NewEndpoint("b.example.com", "A", "192.0.2.3"),
			endpoint.NewEndpoint("c.example.com", "A", "192.0.2.4"),
		},
	}
	if err := p.ApplyChanges(context.Background(), changes); err != nil {
		t.Fatalf("ApplyChanges: %v", err)
	}

	if got := getCount.Load(); got != 1 {
		t.Errorf("GET count = %d, want exactly 1 (one snapshot, no per-delete refetch)", got)
	}
	// a.example.com has two IDs; b and c have one each → 4 deletes total.
	if got := deleteCount.Load(); got != 4 {
		t.Errorf("DELETE count = %d, want 4 (2 for a + 1 for b + 1 for c)", got)
	}
}

// TestApplyChanges_CNAMEConflictUsesSnapshot covers the conflict-cleanup
// branch: creating a CNAME at a name that already has one should delete the
// existing record IDs via the snapshot index, with no extra GET.
func TestApplyChanges_CNAMEConflictUsesSnapshot(t *testing.T) {
	var (
		getCount    atomic.Int32
		deleteCount atomic.Int32
		createCount atomic.Int32
	)

	existing := []staticDNSRecord{
		recCNAME("old-cname", "alias.example.com", "old.target.example.com", 300),
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			getCount.Add(1)
			_ = json.NewEncoder(w).Encode(listOf(existing...))
		case http.MethodPost:
			createCount.Add(1)
			body, _ := io.ReadAll(r.Body)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(body) // echo back as the created record
		case http.MethodDelete:
			deleteCount.Add(1)
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	p := &UnifiProvider{client: newTestClient(srv), workers: 1}

	changes := &plan.Changes{
		Create: []*endpoint.Endpoint{
			endpoint.NewEndpointWithTTL("alias.example.com", "CNAME", 300, "new.target.example.com"),
		},
	}
	if err := p.ApplyChanges(context.Background(), changes); err != nil {
		t.Fatalf("ApplyChanges: %v", err)
	}

	if got := getCount.Load(); got != 1 {
		t.Errorf("GET count = %d, want 1", got)
	}
	if got := deleteCount.Load(); got != 1 {
		t.Errorf("DELETE count = %d, want 1 (the conflicting CNAME)", got)
	}
	if got := createCount.Load(); got != 1 {
		t.Errorf("POST count = %d, want 1", got)
	}
}

// TestRecords_ResetsStalePerTypeGauge verifies the per-type records gauge is
// recomputed each call: once the last record of a type is deleted upstream, its
// gauge must drop to 0 rather than retain the previous count. Regression for
// the stale GaugeVec child (issue #218) — Records seeds every managed type at
// zero so a vanished type is explicitly Set(0).
func TestRecords_ResetsStalePerTypeGauge(t *testing.T) {
	var page atomic.Pointer[[]staticDNSRecord]
	first := listOf(
		recA("id-1", "a.example.com", "192.0.2.1", 300),
		recA("id-2", "b.example.com", "192.0.2.2", 300),
	)
	page.Store(&first)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(*page.Load())
	}))
	defer srv.Close()

	p := &UnifiProvider{client: newTestClient(srv)}
	gaugeA := metrics.Get().RecordsTotal.WithLabelValues(metrics.ProviderName, recordTypeA)
	read := func() float64 {
		t.Helper()
		var m dto.Metric
		if err := gaugeA.Write(&m); err != nil {
			t.Fatalf("gauge.Write: %v", err)
		}

		return m.GetGauge().GetValue()
	}

	if _, err := p.Records(context.Background()); err != nil {
		t.Fatalf("Records: %v", err)
	}
	if got := read(); got != 2 {
		t.Fatalf("records{type=A} after first fetch = %v, want 2", got)
	}

	// All A records deleted upstream.
	empty := listOf()
	page.Store(&empty)
	if _, err := p.Records(context.Background()); err != nil {
		t.Fatalf("Records (after deletion): %v", err)
	}
	if got := read(); got != 0 {
		t.Errorf("records{type=A} after all A records deleted = %v, want 0 (stale gauge not reset)", got)
	}
}

// TestRecords_DistinguishesCollidingKeys is the grouping half of the #222
// regression. Before the recordKey struct fix, records were grouped by the
// bare string r.Key+r.RecordType, which is ambiguous: an A record for
// "host.example.comAAA" and an AAAA record for "host.example.com" both flatten
// to "host.example.comAAAA" and were merged into a single endpoint (the second
// type silently dropped). They must come back as two distinct endpoints.
func TestRecords_DistinguishesCollidingKeys(t *testing.T) {
	existing := listOf(
		recA("id-a", "host.example.comAAA", "192.0.2.1", 300),
		recAAAA("id-aaaa", "host.example.com", "2001:db8::1", 300),
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(existing)
	}))
	defer srv.Close()

	p := &UnifiProvider{client: newTestClient(srv)}
	eps, err := p.Records(context.Background())
	if err != nil {
		t.Fatalf("Records: %v", err)
	}

	// Classify by type/target rather than DNSName so the assertion does not
	// depend on external-dns name normalisation.
	var aOK, aaaaOK bool
	for _, ep := range eps {
		switch ep.RecordType {
		case recordTypeA:
			aOK = len(ep.Targets) == 1 && ep.Targets[0] == "192.0.2.1"
		case recordTypeAAAA:
			aaaaOK = len(ep.Targets) == 1 && ep.Targets[0] == "2001:db8::1"
		}
	}
	if len(eps) != 2 || !aOK || !aaaaOK {
		t.Fatalf("want 2 endpoints (A→192.0.2.1, AAAA→2001:db8::1); got %d merged by key collision: %+v", len(eps), eps)
	}
}

// TestApplyChanges_DeleteDoesNotCollideAcrossTypes is the data-loss half of
// the #222 regression: deleting one endpoint must not take out an unrelated
// record that shared the old concatenated key. "host.example.comAAA"/A and
// "host.example.com"/AAAA both concatenated to "host.example.comAAAA", so
// deleting the A record used to delete the AAAA record's ID as collateral.
func TestApplyChanges_DeleteDoesNotCollideAcrossTypes(t *testing.T) {
	existing := []staticDNSRecord{
		recA("id-a", "host.example.comAAA", "192.0.2.1", 300),
		recAAAA("id-aaaa", "host.example.com", "2001:db8::1", 300),
	}

	var deleted sync.Map
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(listOf(existing...))
		case http.MethodDelete:
			deleted.Store(path.Base(r.URL.Path), struct{}{})
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	p := &UnifiProvider{client: newTestClient(srv), workers: 1}
	changes := &plan.Changes{
		Delete: []*endpoint.Endpoint{endpoint.NewEndpoint("host.example.comAAA", "A", "192.0.2.1")},
	}
	if err := p.ApplyChanges(context.Background(), changes); err != nil {
		t.Fatalf("ApplyChanges: %v", err)
	}

	if _, hit := deleted.Load("id-a"); !hit {
		t.Error("expected the targeted A record id-a to be deleted")
	}
	if _, hit := deleted.Load("id-aaaa"); hit {
		t.Error("AAAA record id-aaaa was deleted as collateral — Key+RecordType collision (#222)")
	}
}

// TestRecords_ExcludesDisabledRecords proves a record disabled in the UniFi UI
// is not reported as a managed endpoint (#221): external-dns must not believe a
// parked record is live.
func TestRecords_ExcludesDisabledRecords(t *testing.T) {
	disabled := recA("id-off", "off.example.com", "192.0.2.8", 300)
	disabled.Enabled = false
	existing := listOf(
		recA("id-on", "on.example.com", "192.0.2.1", 300),
		disabled,
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(existing)
	}))
	defer srv.Close()

	p := &UnifiProvider{client: newTestClient(srv)}
	eps, err := p.Records(context.Background())
	if err != nil {
		t.Fatalf("Records: %v", err)
	}

	if len(eps) != 1 {
		t.Fatalf("got %d endpoints, want 1 (the disabled record must be excluded): %+v", len(eps), eps)
	}
	if eps[0].DNSName != "on.example.com" {
		t.Errorf("endpoint = %q, want on.example.com (the enabled record)", eps[0].DNSName)
	}
}

// TestAdjustEndpoints_ClearsTTLForControllerManagedTypes is the #229
// regression: the UniFi API manages (and ignores a custom) TTL for TXT/MX/SRV,
// so a user-set TTL on those types must be stripped from the desired set or
// external-dns churns delete+create forever. A/AAAA/CNAME keep their TTL.
func TestAdjustEndpoints_ClearsTTLForControllerManagedTypes(t *testing.T) {
	p := &UnifiProvider{}
	in := []*endpoint.Endpoint{
		endpoint.NewEndpointWithTTL("a.example.com", recordTypeA, 300, "192.0.2.1"),
		endpoint.NewEndpointWithTTL("v6.example.com", recordTypeAAAA, 300, "2001:db8::1"),
		endpoint.NewEndpointWithTTL("alias.example.com", recordTypeCNAME, 300, "target.example.com"),
		endpoint.NewEndpointWithTTL("txt.example.com", recordTypeTXT, 300, "v=spf1 ~all"),
		endpoint.NewEndpointWithTTL("example.com", recordTypeMX, 300, "10 mail.example.com"),
		endpoint.NewEndpointWithTTL("_ldap._tcp.example.com", recordTypeSRV, 300, "10 20 389 ldap.example.com"),
	}

	out, err := p.AdjustEndpoints(in)
	if err != nil {
		t.Fatalf("AdjustEndpoints: %v", err)
	}

	byType := make(map[string]*endpoint.Endpoint, len(out))
	for _, ep := range out {
		byType[ep.RecordType] = ep
	}

	// A/AAAA/CNAME honour a custom TTL — it must survive.
	for _, ty := range []string{recordTypeA, recordTypeAAAA, recordTypeCNAME} {
		if got := byType[ty].RecordTTL; got != 300 {
			t.Errorf("%s TTL = %v, want 300 preserved", ty, got)
		}
	}
	// TXT/MX/SRV are controller-managed — the TTL must be cleared so it is no
	// longer "configured" (the trigger for external-dns TTL churn).
	for _, ty := range []string{recordTypeTXT, recordTypeMX, recordTypeSRV} {
		if byType[ty].RecordTTL.IsConfigured() {
			t.Errorf("%s TTL = %v, want cleared (controller-managed)", ty, byType[ty].RecordTTL)
		}
	}
}

// TestApplyChanges_CNAMEUpdateIsNotAConflict: a routine CNAME update
// (UpdateOld+UpdateNew for the same name) must delete the old record exactly
// once — in the delete phase — and must NOT take the conflict-cleanup branch.
// Before the deletedKeys guard, the create phase re-read the pre-delete
// snapshot, counted a spurious CNAME conflict, and issued a second DELETE that
// 404ed on every CNAME target change.
func TestApplyChanges_CNAMEUpdateIsNotAConflict(t *testing.T) {
	var deleteCount, notFoundServed atomic.Int32
	existing := []staticDNSRecord{
		recCNAME("old-cname", "alias.example.com", "old.target.example.com", 300),
	}
	var deletedOnce atomic.Bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(listOf(existing...))
		case http.MethodPost:
			body, _ := io.ReadAll(r.Body)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(body)
		case http.MethodDelete:
			deleteCount.Add(1)
			if !deletedOnce.CompareAndSwap(false, true) {
				// The record is already gone — a second DELETE is the bug.
				notFoundServed.Add(1)
				w.WriteHeader(http.StatusNotFound)

				return
			}
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	conflicts := metrics.Get().CNAMEConflictsTotal.WithLabelValues(metrics.ProviderName)
	readConflicts := func() float64 {
		t.Helper()
		var m dto.Metric
		if err := conflicts.Write(&m); err != nil {
			t.Fatalf("counter.Write: %v", err)
		}

		return m.GetCounter().GetValue()
	}
	before := readConflicts()

	p := &UnifiProvider{client: newTestClient(srv), workers: 1}
	changes := &plan.Changes{
		UpdateOld: []*endpoint.Endpoint{endpoint.NewEndpointWithTTL("alias.example.com", "CNAME", 300, "old.target.example.com")},
		UpdateNew: []*endpoint.Endpoint{endpoint.NewEndpointWithTTL("alias.example.com", "CNAME", 300, "new.target.example.com")},
	}
	if err := p.ApplyChanges(context.Background(), changes); err != nil {
		t.Fatalf("ApplyChanges: %v", err)
	}

	if got := deleteCount.Load(); got != 1 {
		t.Errorf("DELETE count = %d, want 1 (the update's own delete; no redundant conflict cleanup)", got)
	}
	if got := notFoundServed.Load(); got != 0 {
		t.Errorf("served %d 404s — the stale snapshot was re-deleted", got)
	}
	if got := readConflicts() - before; got != 0 {
		t.Errorf("CNAMEConflictsTotal delta = %v, want 0 (an update is not a conflict)", got)
	}
}
