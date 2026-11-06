package unifi

import (
	"testing"
)

func TestFormatURL(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		params   []string
		expected string
	}{
		{
			name:     "login path",
			path:     "%s/api/auth/login",
			params:   []string{testHost},
			expected: testURLLoginInternal,
		},
		{
			name:     "static-dns path with site name",
			path:     pathStaticDNS,
			params:   []string{testHost, testSite},
			expected: "https://unifi.local/proxy/network/v2/api/site/default/static-dns",
		},
		{
			name:     "static-dns path with site name and record ID",
			path:     pathStaticDNSRecord,
			params:   []string{testHost, testSite, "abc123"},
			expected: "https://unifi.local/proxy/network/v2/api/site/default/static-dns/abc123",
		},
		{
			name:     "no placeholders - appends params",
			path:     "/api/login",
			params:   []string{testURLExample},
			expected: "/api/loginhttps://example.com",
		},
		{
			name:     "empty params",
			path:     testPathHostParam,
			params:   []string{},
			expected: testPathAPISlash,
		},
		{
			name:     "empty string params",
			path:     "%s/api/%s/data",
			params:   []string{testURLExample, ""},
			expected: "https://example.com/api//data",
		},
		{
			name:     "more placeholders than params",
			path:     "%s/api/%s/site/%s",
			params:   []string{testURLExample},
			expected: "https://example.com/api//site/",
		},
		{
			name:     "URL with port",
			path:     "%s/api/login",
			params:   []string{"https://unifi.local:8443"},
			expected: "https://unifi.local:8443/api/login",
		},
		{
			name:     "URL with query params",
			path:     "%s/api/site/%s?filter=active",
			params:   []string{testHost, testSite},
			expected: "https://unifi.local/api/site/default?filter=active",
		},
		{
			name:     "special characters in params",
			path:     testPathHostParam,
			params:   []string{testURLExample, "special-chars_123"},
			expected: "https://example.com/api/special-chars_123",
		},
		{
			name:     "unicode in params",
			path:     testPathHostParam,
			params:   []string{"https://пример.рф", "сайт"},
			expected: "https://пример.рф/api/сайт",
		},
		{
			name:     "empty path - appends params",
			path:     "",
			params:   []string{testURLExample},
			expected: testURLExample,
		},
		{
			name:     "single placeholder",
			path:     testPathSingle,
			params:   []string{testURLExample},
			expected: testURLExample,
		},
		{
			name:     "consecutive placeholders",
			path:     testPathTriple,
			params:   []string{"a", "b", "c"},
			expected: "abc",
		},
		{
			name:     "path with trailing slash",
			path:     "%s/api/%s/",
			params:   []string{testURLExample, "v1"},
			expected: "https://example.com/api/v1/",
		},
		{
			name:     "path with multiple slashes",
			path:     "%s//api//%s",
			params:   []string{testURLExample, "endpoint"},
			expected: "https://example.com//api//endpoint",
		},
		{
			name:     "nil params equivalent",
			path:     testPathHostParam,
			params:   nil,
			expected: testPathAPISlash,
		},
		{
			name:     "IPv4 address",
			path:     "%s/api/login",
			params:   []string{"https://192.168.1.1"},
			expected: "https://192.168.1.1/api/login",
		},
		{
			name:     "IPv6 address",
			path:     "%s/api/login",
			params:   []string{"https://[2001:db8::1]"},
			expected: "https://[2001:db8::1]/api/login",
		},
		{
			name:     "path injection attempt",
			path:     testPathHostParam,
			params:   []string{testURLExample, "../../../etc/passwd"},
			expected: "https://example.com/api/../../../etc/passwd",
		},
		{
			name:     "URL with fragment",
			path:     "%s/api/%s#section",
			params:   []string{testURLExample, "resource"},
			expected: "https://example.com/api/resource#section",
		},
		{
			name:     "URL with credentials",
			path:     testPathHost,
			params:   []string{testURLCreds},
			expected: testURLCreds + "/api",
		},
		{
			name:     "long record ID",
			path:     "%s/api/site/%s/static-dns/%s",
			params:   []string{testURLExample, testSite, "abcdef0123456789abcdef0123456789abcdef01"},
			expected: "https://example.com/api/site/default/static-dns/abcdef0123456789abcdef0123456789abcdef01",
		},
		{
			name:     "whitespace in params",
			path:     testPathHostParam,
			params:   []string{testURLExample, "my site"},
			expected: "https://example.com/api/my site",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatURL(tt.path, tt.params...)
			if result != tt.expected {
				t.Errorf("formatURL(%q, %v) = %q, want %q", tt.path, tt.params, result, tt.expected)
			}
		})
	}
}

// TestFormatURLRealWorldUsage tests actual usage patterns from the codebase.
func TestFormatURLRealWorldUsage(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		params   []string
		expected string
	}{
		{
			name:     "list records",
			path:     pathStaticDNS,
			params:   []string{testHost, testSite},
			expected: "https://unifi.local/proxy/network/v2/api/site/default/static-dns",
		},
		{
			name:     "delete specific record",
			path:     pathStaticDNSRecord,
			params:   []string{testHost, testSite, testRecordID},
			expected: "https://unifi.local/proxy/network/v2/api/site/default/static-dns/" + testRecordID,
		},
		{
			name:     "controller with port",
			path:     pathStaticDNS,
			params:   []string{testHostPort, testSite},
			expected: "https://192.168.1.1:8443/proxy/network/v2/api/site/default/static-dns",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatURL(tt.path, tt.params...)
			if result != tt.expected {
				t.Errorf("formatURL() = %q, want %q", result, tt.expected)
			}
		})
	}
}

// TestFormatURLEdgeCases tests boundary conditions.
func TestFormatURLEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		params   []string
		expected string
	}{
		{
			name:     "extremely long URL",
			path:     testPathHostParam,
			params:   []string{testURLExample, string(make([]byte, 10000))},
			expected: "https://example.com/api/" + string(make([]byte, 10000)),
		},
		{
			name:     "many placeholders",
			path:     "%s/%s/%s/%s/%s/%s/%s/%s/%s/%s",
			params:   []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"},
			expected: "a/b/c/d/e/f/g/h/i/j",
		},
		{
			name:     "only placeholders",
			path:     testPathTriple,
			params:   []string{"", "", ""},
			expected: "",
		},
		{
			name:     "escaped percent (not placeholder) - treats %% as single % and s as literal",
			path:     "%%s/api",
			params:   []string{"value"},
			expected: "%value/api",
		},
		{
			name:     "mixed content",
			path:     "prefix%smiddle%ssuffix",
			params:   []string{"AAA", "BBB"},
			expected: "prefixAAAmiddleBBBsuffix",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatURL(tt.path, tt.params...)
			if result != tt.expected {
				t.Errorf("formatURL() = %q, want %q", result, tt.expected)
			}
		})
	}
}

// TestFormatURLExcessParams verifies that extra params beyond the placeholder
// count are appended to the final segment instead of panicking.
func TestFormatURLExcessParams(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		params   []string
		expected string
	}{
		{
			name:     "one extra param appended to tail",
			path:     "%s/api",
			params:   []string{testURLExample, "extra"},
			expected: testURLExample + "/apiextra",
		},
		{
			name:     "three extra params all appended to tail",
			path:     "%s",
			params:   []string{"a", "b", "c", "d"},
			expected: "abcd",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatURL(tt.path, tt.params...)
			if result != tt.expected {
				t.Errorf("formatURL(%q, %v) = %q, want %q", tt.path, tt.params, result, tt.expected)
			}
		})
	}
}
