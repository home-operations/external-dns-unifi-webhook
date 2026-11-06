package unifi

import (
	"errors"
	"fmt"

	"sigs.k8s.io/external-dns/endpoint"
)

// errSRVConversion marks errors that originate in SRV target parsing.
// CreateEndpoint matches it with errors.Is to attribute the SRV-specific
// parsing metric, decoupled from the human-readable DataType strings.
var errSRVConversion = errors.New("SRV conversion")

// TTL ceilings (in seconds) the controller enforces per record type.
const (
	ttlMaxA     = 86400
	ttlMaxAAAA  = 86400
	ttlMaxCNAME = 604800
)

// staticDNSRecord is the wire shape of a /static-dns entry. Every record type
// shares the same flat object; the per-type extras (ttl, priority, weight,
// port) are pointers with omitempty because the controller rejects a field it
// does not expect for the given record_type (ttl on TXT/MX/SRV, priority on
// A, and so on).
//
//nolint:tagliatelle // UniFi API field names cannot be changed
type staticDNSRecord struct {
	ID         string `json:"_id,omitempty"`
	Enabled    bool   `json:"enabled"`
	Key        string `json:"key"`
	RecordType string `json:"record_type"`
	Value      string `json:"value"`

	TTL      *int `json:"ttl,omitempty"`
	Port     *int `json:"port,omitempty"`
	Priority *int `json:"priority,omitempty"`
	Weight   *int `json:"weight,omitempty"`
}

// apiPage is the paginated list envelope the Integration API returns for its
// collection endpoints. Data carries the per-endpoint entry type.
type apiPage[T any] struct {
	Offset     int `json:"offset"`
	Limit      int `json:"limit"`
	Count      int `json:"count"`
	TotalCount int `json:"totalCount"`
	Data       []T `json:"data"`
}

// sitePage is the envelope returned by GET /sites.
type sitePage = apiPage[siteEntry]

//nolint:tagliatelle // UniFi API field names cannot be changed
type siteEntry struct {
	ID                string `json:"id"`
	InternalReference string `json:"internalReference"`
	Name              string `json:"name"`
}

// toDNSRecord converts a wire record into the internal DNSRecord shape.
// ok=false means the record should be skipped: a disabled record (not serving
// DNS) or a record type we don't manage.
//
// Disabled records are skipped rather than reported because external-dns has
// no "disabled" concept; surfacing one would make external-dns believe a parked
// record is live. Treating it as not-present lets external-dns re-create the
// record (enabled) if it is still in the desired set, and otherwise leave it
// alone. See #221.
func toDNSRecord(rec staticDNSRecord) (DNSRecord, bool) {
	if !rec.Enabled {
		return DNSRecord{}, false
	}

	r := DNSRecord{
		ID:         rec.ID,
		Key:        rec.Key,
		RecordType: rec.RecordType,
	}
	if rec.TTL != nil {
		r.TTL = endpoint.TTL(*rec.TTL)
	}

	switch rec.RecordType {
	case recordTypeA, recordTypeAAAA, recordTypeCNAME, recordTypeTXT:
		r.Value = rec.Value
	case recordTypeMX:
		r.Value = formatMXValue(rec.Priority, rec.Value)
	case recordTypeSRV:
		r.Value = formatSRVValue(rec.Priority, rec.Weight, rec.Port, rec.Value)
	default:
		return DNSRecord{}, false
	}

	return r, true
}

// fromDNSRecord converts an internal DNSRecord into a wire record ready for
// POST /static-dns. TTL is clamped per-type to the controller's maxima.
func fromDNSRecord(r DNSRecord) (staticDNSRecord, error) {
	rec := staticDNSRecord{
		// Records external-dns asks us to write are always live; "disabled" is
		// a manual UI state we deliberately don't model (see toDNSRecord, #221).
		Enabled:    true,
		Key:        r.Key,
		RecordType: r.RecordType,
		Value:      r.Value,
	}

	switch r.RecordType {
	case recordTypeA:
		rec.TTL = new(clampTTL(int(r.TTL), ttlMaxA))
	case recordTypeAAAA:
		rec.TTL = new(clampTTL(int(r.TTL), ttlMaxAAAA))
	case recordTypeCNAME:
		rec.TTL = new(clampTTL(int(r.TTL), ttlMaxCNAME))
	// TXT/MX/SRV deliberately send no ttl: the controller manages their TTL
	// itself and rejects the field. AdjustEndpoints clears the TTL from the
	// desired set so external-dns doesn't churn over it. See #229.
	case recordTypeTXT:
	case recordTypeMX:
		prio, mail, err := parseMXValue(r.Value)
		if err != nil {
			return staticDNSRecord{}, err
		}
		rec.Priority = &prio
		rec.Value = mail
	case recordTypeSRV:
		prio, weight, port, server, err := parseSRVValue(r.Value)
		if err != nil {
			return staticDNSRecord{}, err
		}
		rec.Priority = &prio
		rec.Weight = &weight
		rec.Port = &port
		rec.Value = server
	default:
		return staticDNSRecord{}, NewDataError("convert", "DNS record",
			fmt.Errorf("unsupported record type %q", r.RecordType))
	}

	return rec, nil
}

// clampTTL clips ttl into [0, max]. A zero TTL means "use controller default"
// to external-dns, so we don't substitute anything for unset.
func clampTTL(ttl, maxSecs int) int {
	return min(max(ttl, 0), maxSecs)
}

// formatSRVValue renders the external-dns SRV target convention. Nil pointers
// (shouldn't happen for well-formed records) are rendered as 0 to keep the
// shape consistent.
func formatSRVValue(priority, weight, port *int, server string) string {
	return fmt.Sprintf("%d %d %d %s",
		derefInt(priority), derefInt(weight), derefInt(port), server)
}

// parseSRVValue reverses formatSRVValue.
func parseSRVValue(target string) (priority, weight, port int, server string, err error) {
	if _, err := fmt.Sscanf(target, "%d %d %d %s", &priority, &weight, &port, &server); err != nil {
		return 0, 0, 0, "", NewDataError("parse", "SRV record target",
			fmt.Errorf("%w: %w", errSRVConversion, err))
	}

	return priority, weight, port, server, nil
}

// formatMXValue renders the external-dns MX target convention: "priority host".
func formatMXValue(priority *int, mail string) string {
	return fmt.Sprintf("%d %s", derefInt(priority), mail)
}

// parseMXValue reverses formatMXValue.
func parseMXValue(target string) (priority int, mail string, err error) {
	if _, err := fmt.Sscanf(target, "%d %s", &priority, &mail); err != nil {
		return 0, "", NewDataError("parse", "MX record target", err)
	}

	return priority, mail, nil
}

func derefInt(p *int) int {
	if p == nil {
		return 0
	}

	return *p
}
