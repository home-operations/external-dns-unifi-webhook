package unifi

import (
	"testing"

	"sigs.k8s.io/external-dns/endpoint"
)

func TestToDNSRecord_PerType(t *testing.T) {
	tests := []struct {
		name      string
		rec       staticDNSRecord
		wantOK    bool
		wantKey   string
		wantValue string
		wantType  string
		wantTTL   endpoint.TTL
	}{
		{
			name:      "A record",
			rec:       recA("a1", "host.example.com", "192.0.2.1", 300),
			wantOK:    true,
			wantKey:   "host.example.com",
			wantValue: "192.0.2.1",
			wantType:  recordTypeA,
			wantTTL:   300,
		},
		{
			name:      "AAAA record",
			rec:       recAAAA("a2", "v6.example.com", "2001:db8::1", 60),
			wantOK:    true,
			wantKey:   "v6.example.com",
			wantValue: "2001:db8::1",
			wantType:  recordTypeAAAA,
			wantTTL:   60,
		},
		{
			name:      "CNAME record",
			rec:       recCNAME("c1", "alias.example.com", "target.example.com", 120),
			wantOK:    true,
			wantKey:   "alias.example.com",
			wantValue: "target.example.com",
			wantType:  recordTypeCNAME,
			wantTTL:   120,
		},
		{
			name:      "CNAME with a leading-underscore label",
			rec:       recCNAME("c2", "_dns.resolver.arpa", "unifi.example.com", 0),
			wantOK:    true,
			wantKey:   "_dns.resolver.arpa",
			wantValue: "unifi.example.com",
			wantType:  recordTypeCNAME,
		},
		{
			name:      "TXT has no TTL field on the wire",
			rec:       recTXT("t1", "txt.example.com", "v=spf1 ~all"),
			wantOK:    true,
			wantKey:   "txt.example.com",
			wantValue: "v=spf1 ~all",
			wantType:  recordTypeTXT,
		},
		{
			name: "MX renders as 'priority host'",
			rec: staticDNSRecord{
				RecordType: recordTypeMX, Enabled: true,
				Key: "example.com", Value: "mail.example.com",
				Priority: new(10),
			},
			wantOK:    true,
			wantKey:   "example.com",
			wantValue: "10 mail.example.com",
			wantType:  recordTypeMX,
		},
		{
			name:      "SRV renders as 'priority weight port target'",
			rec:       recSRV("s1", "_ldap._tcp.example.com", "ldap.example.com", 10, 20, 389),
			wantOK:    true,
			wantKey:   "_ldap._tcp.example.com",
			wantValue: "10 20 389 ldap.example.com",
			wantType:  recordTypeSRV,
		},
		{
			name:   "NS is dropped",
			rec:    staticDNSRecord{RecordType: "NS", Enabled: true, Key: "example.com", Value: "ns1.example.com"},
			wantOK: false,
		},
		{
			name:   "unknown type is dropped",
			rec:    staticDNSRecord{RecordType: "MYSTERY", Enabled: true, Key: "x"},
			wantOK: false,
		},
		{
			// A fully valid A record, but disabled in the UI: must not be
			// surfaced as a managed record (#221).
			name: "disabled record is dropped",
			rec: staticDNSRecord{
				RecordType: recordTypeA, Enabled: false,
				Key: "parked.example.com", Value: "192.0.2.9",
				TTL: new(300),
			},
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := toDNSRecord(tt.rec)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !tt.wantOK {
				return
			}
			if got.Key != tt.wantKey {
				t.Errorf("Key = %q, want %q", got.Key, tt.wantKey)
			}
			if got.Value != tt.wantValue {
				t.Errorf("Value = %q, want %q", got.Value, tt.wantValue)
			}
			if got.RecordType != tt.wantType {
				t.Errorf("RecordType = %q, want %q", got.RecordType, tt.wantType)
			}
			if got.TTL != tt.wantTTL {
				t.Errorf("TTL = %v, want %v", got.TTL, tt.wantTTL)
			}
		})
	}
}

func TestFromDNSRecord_PerType(t *testing.T) {
	tests := []fromDNSRecordCase{
		{
			name: "A record",
			in:   DNSRecord{Key: "host.example.com", RecordType: recordTypeA, Value: "192.0.2.1", TTL: 300},
			check: func(t *testing.T, rec staticDNSRecord) {
				t.Helper()
				if rec.RecordType != recordTypeA {
					t.Errorf("RecordType = %q", rec.RecordType)
				}
				if rec.Value != "192.0.2.1" {
					t.Errorf("Value = %q", rec.Value)
				}
				if rec.TTL == nil || *rec.TTL != 300 {
					t.Errorf("TTL = %v", rec.TTL)
				}
				if rec.Priority != nil || rec.Weight != nil || rec.Port != nil {
					t.Errorf("A record must not carry SRV/MX fields: %+v", rec)
				}
			},
		},
		{
			name: "A TTL clamped at 86400",
			in:   DNSRecord{Key: "x.example.com", RecordType: recordTypeA, Value: "192.0.2.1", TTL: 999999},
			check: func(t *testing.T, rec staticDNSRecord) {
				t.Helper()
				if rec.TTL == nil || *rec.TTL != ttlMaxA {
					t.Errorf("TTL = %v, want %d", rec.TTL, ttlMaxA)
				}
			},
		},
		{
			name: "CNAME TTL clamped at 604800",
			in:   DNSRecord{Key: "x.example.com", RecordType: recordTypeCNAME, Value: "y.example.com", TTL: 999999},
			check: func(t *testing.T, rec staticDNSRecord) {
				t.Helper()
				if rec.TTL == nil || *rec.TTL != ttlMaxCNAME {
					t.Errorf("TTL = %v, want %d", rec.TTL, ttlMaxCNAME)
				}
			},
		},
		{
			name: "CNAME keeps a leading-underscore name verbatim",
			in:   DNSRecord{Key: "_dns.resolver.arpa", RecordType: recordTypeCNAME, Value: "unifi.example.com"},
			check: func(t *testing.T, rec staticDNSRecord) {
				t.Helper()
				if rec.Key != "_dns.resolver.arpa" {
					t.Errorf("Key = %q", rec.Key)
				}
			},
		},
		{
			name: "TXT has no TTL field",
			in:   DNSRecord{Key: "x.example.com", RecordType: recordTypeTXT, Value: "hi", TTL: 60},
			check: func(t *testing.T, rec staticDNSRecord) {
				t.Helper()
				if rec.TTL != nil {
					t.Errorf("expected no TTL for TXT, got %d", *rec.TTL)
				}
				if rec.Value != "hi" {
					t.Errorf("Value = %q", rec.Value)
				}
			},
		},
		{
			name:    "unsupported record type errors",
			in:      DNSRecord{Key: "x", RecordType: "NS", Value: "ns1.example.com"},
			wantErr: true,
		},
	}

	runFromDNSRecordCases(t, tests)
}

// TestFromDNSRecord_MXSRV covers the two types whose external-dns target is
// split into separate wire fields.
func TestFromDNSRecord_MXSRV(t *testing.T) {
	tests := []fromDNSRecordCase{
		{
			name: "MX parses priority and host",
			in:   DNSRecord{Key: "example.com", RecordType: recordTypeMX, Value: "10 mail.example.com", TTL: 60},
			check: func(t *testing.T, rec staticDNSRecord) {
				t.Helper()
				if rec.Priority == nil || *rec.Priority != 10 {
					t.Errorf("Priority = %v", rec.Priority)
				}
				if rec.Value != "mail.example.com" {
					t.Errorf("Value = %q", rec.Value)
				}
				if rec.TTL != nil {
					t.Errorf("expected no TTL for MX, got %d", *rec.TTL)
				}
			},
		},
		{
			name:    "MX invalid value errors",
			in:      DNSRecord{Key: "example.com", RecordType: recordTypeMX, Value: "garbage"},
			wantErr: true,
		},
		{
			name: "SRV splits target into fields",
			in: DNSRecord{
				Key: "_ldap._tcp.example.com", RecordType: recordTypeSRV,
				Value: "10 20 389 ldap.example.com", TTL: 60,
			},
			check: func(t *testing.T, rec staticDNSRecord) {
				t.Helper()
				if rec.Key != "_ldap._tcp.example.com" {
					t.Errorf("Key = %q", rec.Key)
				}
				if rec.Priority == nil || *rec.Priority != 10 {
					t.Errorf("Priority = %v", rec.Priority)
				}
				if rec.Weight == nil || *rec.Weight != 20 {
					t.Errorf("Weight = %v", rec.Weight)
				}
				if rec.Port == nil || *rec.Port != 389 {
					t.Errorf("Port = %v", rec.Port)
				}
				if rec.Value != "ldap.example.com" {
					t.Errorf("Value = %q", rec.Value)
				}
				if rec.TTL != nil {
					t.Errorf("expected no TTL for SRV, got %d", *rec.TTL)
				}
			},
		},
		{
			name: "SRV malformed value errors",
			in: DNSRecord{
				Key: "_ldap._tcp.example.com", RecordType: recordTypeSRV,
				Value: "not a srv target",
			},
			wantErr: true,
		},
	}

	runFromDNSRecordCases(t, tests)
}

type fromDNSRecordCase struct {
	name    string
	in      DNSRecord
	check   func(*testing.T, staticDNSRecord)
	wantErr bool
}

func runFromDNSRecordCases(t *testing.T, tests []fromDNSRecordCase) {
	t.Helper()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec, err := fromDNSRecord(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}

				return
			}
			if err != nil {
				t.Fatalf("fromDNSRecord: %v", err)
			}
			if !rec.Enabled {
				t.Errorf("Enabled = false, want true")
			}
			if tt.check != nil {
				tt.check(t, rec)
			}
		})
	}
}

// TestSRVRoundTrip verifies that toDNSRecord(fromDNSRecord(r)) preserves the
// external-dns record shape.
func TestSRVRoundTrip(t *testing.T) {
	orig := DNSRecord{
		Key: "_ldap._tcp.example.com", RecordType: recordTypeSRV,
		Value: "10 20 389 ldap.example.com",
	}

	rec, err := fromDNSRecord(orig)
	if err != nil {
		t.Fatalf("fromDNSRecord: %v", err)
	}

	got, ok := toDNSRecord(rec)
	if !ok {
		t.Fatal("toDNSRecord dropped a record it converted from")
	}
	if got.Key != orig.Key {
		t.Errorf("Key roundtrip: got %q want %q", got.Key, orig.Key)
	}
	if got.Value != orig.Value {
		t.Errorf("Value roundtrip: got %q want %q", got.Value, orig.Value)
	}
}

func TestIsSRVConversionError(t *testing.T) {
	tests := []struct {
		name string
		in   DNSRecord
		want bool
	}{
		{
			name: "SRV value malformed",
			in:   DNSRecord{Key: "_x._tcp.example.com", RecordType: recordTypeSRV, Value: "bogus"},
			want: true,
		},
		{
			name: "MX value malformed",
			in:   DNSRecord{Key: "example.com", RecordType: recordTypeMX, Value: "bogus"},
			want: false,
		},
		{
			name: "unsupported record type",
			in:   DNSRecord{Key: "example.com", RecordType: "NS", Value: "ns1.example.com"},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := fromDNSRecord(tt.in)
			if err == nil {
				t.Fatal("expected error")
			}
			if got := isSRVConversionError(err); got != tt.want {
				t.Errorf("isSRVConversionError = %v, want %v (err=%v)", got, tt.want, err)
			}
		})
	}
}

func TestClampTTL(t *testing.T) {
	tests := []struct {
		ttl, max, want int
	}{
		{0, 86400, 0},
		{60, 86400, 60},
		{86400, 86400, 86400},
		{86401, 86400, 86400},
		{-5, 86400, 0},
	}
	for _, tt := range tests {
		if got := clampTTL(tt.ttl, tt.max); got != tt.want {
			t.Errorf("clampTTL(%d,%d)=%d, want %d", tt.ttl, tt.max, got, tt.want)
		}
	}
}
