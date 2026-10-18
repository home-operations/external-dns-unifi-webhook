package unifi

import (
	"testing"
	"time"
)

// validUnifiConfig returns a Config that passes Validate; tests mutate one
// field at a time to exercise each rule.
func validUnifiConfig() Config {
	return Config{
		Host:              testHost,
		APIKey:            testKeyA,
		Site:              testSite,
		RetryAttempts:     3,
		RetryInitialDelay: 500 * time.Millisecond,
		RetryMaxDelay:     10 * time.Second,
		ApplyWorkers:      5,
	}
}

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr bool
	}{
		{"valid", func(*Config) {}, false},
		{"host missing scheme", func(c *Config) { c.Host = "unifi.local" }, true},
		{"host empty", func(c *Config) { c.Host = "" }, true},
		{"retry attempts zero", func(c *Config) { c.RetryAttempts = 0 }, true},
		{"retry attempts too high", func(c *Config) { c.RetryAttempts = maxRetryAttempts + 1 }, true},
		{"apply workers zero", func(c *Config) { c.ApplyWorkers = 0 }, true},
		{"apply workers too high", func(c *Config) { c.ApplyWorkers = maxApplyWorkers + 1 }, true},
		{"initial delay zero", func(c *Config) { c.RetryInitialDelay = 0 }, true},
		{"initial delay below floor", func(c *Config) { c.RetryInitialDelay = time.Nanosecond }, true},
		{"initial delay at floor", func(c *Config) {
			c.RetryInitialDelay = minRetryInitialDelay
			c.RetryMaxDelay = minRetryInitialDelay
		}, false},
		{"max delay below initial", func(c *Config) { c.RetryMaxDelay = 100 * time.Millisecond }, true},
		{"request timeout negative", func(c *Config) { c.RequestTimeout = -time.Second }, true},
		{"request timeout set", func(c *Config) { c.RequestTimeout = 4 * time.Second }, false},
		{"cloud host without console id", func(c *Config) { c.Host = "https://api.ui.com" }, true},
		{"cloud host with console id", func(c *Config) {
			c.Host = "https://api.ui.com"
			c.ConsoleID = testConsoleID
		}, false},
		{"console id with local host is fine", func(c *Config) { c.ConsoleID = testConsoleID }, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validUnifiConfig()
			tt.mutate(&cfg)
			err := cfg.Validate()
			if tt.wantErr != (err != nil) {
				t.Errorf("Validate() error = %v, wantErr = %v", err, tt.wantErr)
			}
		})
	}
}

func TestConfigBaseURL(t *testing.T) {
	tests := []struct {
		name      string
		host      string
		consoleID string
		want      string
	}{
		{
			name: "local controller",
			host: "https://unifi.local",
			want: "https://unifi.local",
		},
		{
			name: "local controller trailing slash trimmed",
			host: "https://unifi.local/",
			want: "https://unifi.local",
		},
		{
			name:      "cloud connector",
			host:      "https://api.ui.com",
			consoleID: testConsoleID,
			want:      "https://api.ui.com/v1/connector/consoles/" + testConsoleID,
		},
		{
			name:      "cloud connector trailing slash trimmed",
			host:      "https://api.ui.com/",
			consoleID: testConsoleID,
			want:      "https://api.ui.com/v1/connector/consoles/" + testConsoleID,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{Host: tt.host, ConsoleID: tt.consoleID}
			if got := cfg.baseURL(); got != tt.want {
				t.Errorf("baseURL() = %q, want %q", got, tt.want)
			}
			if got := cfg.isCloud(); got != (tt.consoleID != "") {
				t.Errorf("isCloud() = %v, want %v", got, tt.consoleID != "")
			}
		})
	}
}
