package unifi

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/home-operations/external-dns-unifi-webhook/internal/metrics"
	"golang.org/x/sync/errgroup"
	"sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/plan"
	"sigs.k8s.io/external-dns/provider"
)

const defaultApplyWorkers = 5

// recordKey identifies a record set by its DNS name and type. It is used as a
// map key in place of string concatenation: concatenating r.Key+r.RecordType
// is ambiguous because the type strings are not self-delimiting, so an A record
// for the name "x.AAA" and an AAAA record for the name "x." both flatten to the
// same "x.AAAA" string. That collision would group two unrelated records
// together and, on the delete path, resolve one endpoint to the other's record
// IDs — silently deleting a bystander. A struct key compares field-wise and
// cannot collide.
type recordKey struct {
	name       string
	recordType string
}

// UnifiProvider type for interfacing with UniFi.
//
//nolint:revive // UnifiProvider is the correct name for this provider, renaming would be a breaking change
type UnifiProvider struct {
	provider.BaseProvider

	client  *httpClient
	workers int
}

// NewUnifiProvider initializes a new DNSProvider. The --domain-filter CLI flag
// is applied at the controller level via the registry; UniFi has no zones to
// further constrain against, so we inherit BaseProvider.GetDomainFilter
// (returns an empty filter — matches everything). See the GetDomainFilter
// contract in sigs.k8s.io/external-dns/docs/contributing/sources-and-providers.md.
//
//nolint:ireturn // Must return provider.Provider interface as required by external-dns API
func NewUnifiProvider(config *Config) (provider.Provider, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid unifi configuration: %w", err)
	}

	c, err := newUnifiClient(config)
	if err != nil {
		return nil, fmt.Errorf("creating unifi client: %w", err)
	}

	return &UnifiProvider{
		client:  c,
		workers: cmp.Or(config.ApplyWorkers, defaultApplyWorkers),
	}, nil
}

// recordTypesWithoutCustomTTL are the record types whose TTL the UniFi
// controller manages itself: it rejects a ttl sent for them, and only
// A/AAAA/CNAME honour a custom value. fromDNSRecord already omits the TTL
// on writes for these; AdjustEndpoints clears it from the desired set so it
// can't drive perpetual reconcile churn. See #229.
var recordTypesWithoutCustomTTL = map[string]struct{}{
	recordTypeTXT: {},
	recordTypeMX:  {},
	recordTypeSRV: {},
}

// AdjustEndpoints canonicalises the desired endpoints to what the UniFi
// controller can actually represent. The controller manages the TTL for
// TXT, MX, and SRV records itself and rejects any value sent for them. If a
// user-set TTL is left on such an endpoint, external-dns compares it against
// the controller-managed TTL on every reconcile, never reaches equality, and
// churns delete+create forever. Clearing the TTL here makes the desired state
// match what the controller will store, so the record converges. A/AAAA/CNAME
// (which do honour a custom TTL) are left untouched. See #229.
func (p *UnifiProvider) AdjustEndpoints(endpoints []*endpoint.Endpoint) ([]*endpoint.Endpoint, error) {
	for _, ep := range endpoints {
		if _, managed := recordTypesWithoutCustomTTL[ep.RecordType]; managed {
			ep.RecordTTL = 0
		}
	}

	return endpoints, nil
}

// Records returns the list of records in the DNS provider.
func (p *UnifiProvider) Records(ctx context.Context) (_ []*endpoint.Endpoint, err error) {
	m := metrics.Get()
	defer func() { m.RecordOperation(err) }()

	records, err := p.client.GetEndpoints(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetching DNS records: %w", err)
	}

	// Group supported records by key+type and tally per-type counts in one pass.
	// Seed every managed type at zero so the per-type gauge is fully recomputed
	// each cycle — a type whose records were all deleted is set to 0 rather than
	// retaining its previous value.
	groups := make(map[recordKey][]DNSRecord)
	counts := make(map[string]int, len(managedRecordTypes))
	for _, rt := range managedRecordTypes {
		counts[rt] = 0
	}
	for _, r := range records {
		if !provider.SupportedRecordType(r.RecordType) {
			continue
		}
		key := recordKey{r.Key, r.RecordType}
		groups[key] = append(groups[key], r)
		counts[r.RecordType]++
	}

	for recordType, count := range counts {
		m.UpdateRecordsByType(recordType, count)
	}

	endpoints := make([]*endpoint.Endpoint, 0, len(groups))
	for _, group := range groups {
		targets := make([]string, len(group))
		for i, r := range group {
			targets[i] = r.Value
		}

		if ep := endpoint.NewEndpointWithTTL(
			group[0].Key, group[0].RecordType, group[0].TTL, targets...,
		); ep != nil {
			endpoints = append(endpoints, ep)
		}
	}

	return endpoints, nil
}

// ApplyChanges applies a given set of changes in the DNS provider.
//
// The full record set is fetched once at the top and indexed by name+type
// so per-endpoint deletes and CNAME-conflict cleanup can resolve target
// record IDs locally instead of re-fetching the list per call (which used to
// produce 1+N paginated round trips per reconcile). DeleteRecord tolerates
// 404 so the snapshot index is safe even if a record disappears between
// snapshot and delete.
func (p *UnifiProvider) ApplyChanges(ctx context.Context, changes *plan.Changes) (err error) {
	m := metrics.Get()
	defer func() { m.RecordOperation(err) }()

	rawRecords, err := p.client.GetEndpoints(ctx)
	if err != nil {
		return fmt.Errorf("fetching existing records before applying changes: %w", err)
	}

	m.BatchSize.WithLabelValues(metrics.ProviderName, "create").Observe(float64(len(changes.Create)))
	m.BatchSize.WithLabelValues(metrics.ProviderName, "update").Observe(float64(len(changes.UpdateNew)))
	m.BatchSize.WithLabelValues(metrics.ProviderName, "delete").Observe(float64(len(changes.Delete)))

	byKeyType := indexRecordIDs(rawRecords)

	// Deletes (UpdateOld + Delete) and creates (Create + UpdateNew) each run
	// under their own errgroup with SetLimit so we get bounded concurrency
	// without a hand-rolled semaphore. external-dns already sequences delete
	// before create at the plan level, so we keep the two phases ordered but
	// parallelise within each phase.
	deleteEPs := slices.Concat(changes.UpdateOld, changes.Delete)
	if err := runBounded(ctx, p.workers, deleteEPs, func(ctx context.Context, ep *endpoint.Endpoint) error {
		if err := p.deleteByIDs(ctx, byKeyType[recordKey{ep.DNSName, ep.RecordType}]); err != nil {
			return fmt.Errorf("deleting endpoint %s (%s): %w", ep.DNSName, ep.RecordType, err)
		}
		m.RecordChange("delete", ep.RecordType)

		return nil
	}); err != nil {
		return err
	}

	// Keys the delete phase just processed. The snapshot index still lists their
	// (now removed) record IDs, so the CNAME-conflict cleanup below must skip
	// them: a routine CNAME *update* (UpdateOld+UpdateNew for the same name) is
	// not a conflict, and re-deleting the stale IDs would only burn an API round
	// trip per record (the 404s are tolerated) and false-positive the conflict
	// metric on every CNAME target change.
	deletedKeys := make(map[recordKey]struct{}, len(deleteEPs))
	for _, ep := range deleteEPs {
		deletedKeys[recordKey{ep.DNSName, ep.RecordType}] = struct{}{}
	}

	createEPs := slices.Concat(changes.Create, changes.UpdateNew)
	if err := runBounded(ctx, p.workers, createEPs, func(ctx context.Context, ep *endpoint.Endpoint) error {
		if ep.RecordType == recordTypeCNAME {
			key := recordKey{ep.DNSName, recordTypeCNAME}
			if _, handled := deletedKeys[key]; !handled {
				if ids := byKeyType[key]; len(ids) > 0 {
					m.CNAMEConflictsTotal.WithLabelValues(metrics.ProviderName).Inc()
					if err := p.deleteByIDs(ctx, ids); err != nil {
						return fmt.Errorf("deleting conflicting CNAME %s: %w", ep.DNSName, err)
					}
				}
			}
		}
		if _, err := p.client.CreateEndpoint(ctx, ep); err != nil {
			return fmt.Errorf("creating endpoint %s (%s): %w", ep.DNSName, ep.RecordType, err)
		}
		m.RecordChange("create", ep.RecordType)

		return nil
	}); err != nil {
		return err
	}

	return nil
}

// indexRecordIDs groups record IDs by name+type so the delete path can
// resolve "all IDs for endpoint X" in O(1) without re-listing.
func indexRecordIDs(records []DNSRecord) map[recordKey][]string {
	out := make(map[recordKey][]string, len(records))
	for _, r := range records {
		key := recordKey{r.Key, r.RecordType}
		out[key] = append(out[key], r.ID)
	}

	return out
}

// deleteByIDs deletes every record in ids, tolerating per-record failures and
// joining them into a single error if any fail. An empty slice is a no-op.
func (p *UnifiProvider) deleteByIDs(ctx context.Context, ids []string) error {
	var errs []error
	for _, id := range ids {
		if err := p.client.DeleteRecord(ctx, id); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

// runBounded executes fn(ctx, item) for every entry in items with at most
// limit in flight at any time. The first non-nil error cancels the context and
// bubbles up; remaining workers see ctx.Err() and short-circuit. An empty slice
// is a no-op.
func runBounded[T any](
	ctx context.Context,
	limit int,
	items []T,
	fn func(context.Context, T) error,
) error {
	if len(items) == 0 {
		return nil
	}

	group, gctx := errgroup.WithContext(ctx)
	group.SetLimit(limit)

	for _, item := range items {
		group.Go(func() error { return fn(gctx, item) })
	}

	return group.Wait()
}
