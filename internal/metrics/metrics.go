package metrics

import (
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	namespace    = "externaldns_webhook"
	ProviderName = "unifi"

	histogramBucketCount = 8

	// Prometheus label names reused across metric definitions.
	labelProvider   = "provider"
	labelEndpoint   = "endpoint"
	labelRecordType = "record_type"
	labelOperation  = "operation"
	labelMethod     = "method"
)

// Metrics holds all Prometheus metrics for the webhook.
type Metrics struct {
	// HTTP metrics
	HTTPRequestsTotal         *prometheus.CounterVec
	HTTPRequestDuration       *prometheus.HistogramVec
	HTTPRequestsInFlight      *prometheus.GaugeVec
	HTTPResponseSizeBytes     *prometheus.HistogramVec
	HTTPValidationErrorsTotal *prometheus.CounterVec
	HTTPJSONErrorsTotal       *prometheus.CounterVec

	// Business metrics - DNS records
	RecordsTotal             *prometheus.GaugeVec
	ChangesTotal             *prometheus.CounterVec
	ChangesByTypeTotal       *prometheus.CounterVec
	CNAMEConflictsTotal      *prometheus.CounterVec
	IgnoredCNAMETargetsTotal *prometheus.CounterVec
	SRVParsingErrorsTotal    *prometheus.CounterVec
	BatchSize                *prometheus.HistogramVec

	// Endpoint operations
	AdjustEndpointsTotal *prometheus.CounterVec
	NegotiateTotal       *prometheus.CounterVec

	// UniFi API metrics
	UniFiAPIErrorsTotal    *prometheus.CounterVec
	UniFiAPIDuration       *prometheus.HistogramVec
	UniFiResponseSizeBytes *prometheus.HistogramVec
	UniFiRetriesTotal      *prometheus.CounterVec
	UniFiRateLimitsTotal   *prometheus.CounterVec
	PanicsTotal            *prometheus.CounterVec

	// Quality metrics
	ConsecutiveErrors    *prometheus.GaugeVec
	LastSuccessTimestamp *prometheus.GaugeVec

	// Info metric
	Info *prometheus.GaugeVec
}

var (
	initOnce sync.Once
	instance *Metrics
)

// New initializes the package-wide metrics instance against the default
// Prometheus registerer. Safe to call multiple times — only the first call
// performs initialization, and the supplied version is recorded then.
func New(version string) *Metrics {
	initOnce.Do(func() {
		instance = build(promauto.With(prometheus.DefaultRegisterer), version)
	})

	return instance
}

// Get returns the singleton metrics instance. If New did not run yet, Get
// initializes the instance with an "unknown" version.
func Get() *Metrics {
	return New("unknown")
}

// build constructs a Metrics instance against the supplied factory. Exposed
// for tests that need an isolated registry.
//
//nolint:funlen // Metric registration is declarative and splitting would reduce readability
func build(f promauto.Factory, version string) *Metrics {
	m := &Metrics{
		HTTPRequestsTotal: f.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "http_requests_total",
				Help:      "Total number of HTTP requests",
			},
			[]string{labelProvider, labelMethod, labelEndpoint, "status_code"},
		),
		HTTPRequestDuration: f.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: namespace,
				Name:      "http_request_duration_seconds",
				Help:      "HTTP request duration in seconds",
				Buckets:   prometheus.DefBuckets,
			},
			[]string{labelProvider, labelMethod, labelEndpoint},
		),
		HTTPRequestsInFlight: f.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "http_requests_in_flight",
				Help:      "Number of HTTP requests currently being processed",
			},
			[]string{labelProvider},
		),
		HTTPResponseSizeBytes: f.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: namespace,
				Name:      "http_response_size_bytes",
				Help:      "HTTP response size in bytes",
				Buckets:   prometheus.ExponentialBuckets(100, 10, histogramBucketCount),
			},
			[]string{labelProvider, labelMethod, labelEndpoint},
		),
		HTTPValidationErrorsTotal: f.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "http_validation_errors_total",
				Help:      "Total number of HTTP header validation errors",
			},
			[]string{labelProvider, "header_type"},
		),
		HTTPJSONErrorsTotal: f.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "http_json_errors_total",
				Help:      "Total number of JSON decoding errors",
			},
			[]string{labelProvider, labelEndpoint},
		),

		RecordsTotal: f.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "records",
				Help:      "Current number of DNS records by type",
			},
			[]string{labelProvider, labelRecordType},
		),
		ChangesTotal: f.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "changes_total",
				Help:      "Total number of DNS changes",
			},
			[]string{labelProvider, labelOperation},
		),
		ChangesByTypeTotal: f.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "changes_by_type_total",
				Help:      "Total number of DNS changes by record type",
			},
			[]string{labelProvider, labelOperation, labelRecordType},
		),
		CNAMEConflictsTotal: f.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "cname_conflicts_total",
				Help:      "Total number of CNAME conflicts detected",
			},
			[]string{labelProvider},
		),
		IgnoredCNAMETargetsTotal: f.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "ignored_cname_targets_total",
				Help:      "Total number of ignored CNAME targets (only first target is used)",
			},
			[]string{labelProvider},
		),
		SRVParsingErrorsTotal: f.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "srv_parsing_errors_total",
				Help:      "Total number of SRV record parsing errors",
			},
			[]string{labelProvider},
		),
		BatchSize: f.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: namespace,
				Name:      "batch_size",
				Help:      "Size of change batches",
				Buckets:   prometheus.ExponentialBuckets(1, 2, 10),
			},
			[]string{labelProvider, labelOperation},
		),

		AdjustEndpointsTotal: f.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "adjust_endpoints_total",
				Help:      "Total number of adjust endpoints calls",
			},
			[]string{labelProvider},
		),
		NegotiateTotal: f.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "negotiate_total",
				Help:      "Total number of negotiate calls",
			},
			[]string{labelProvider},
		),

		UniFiAPIErrorsTotal: f.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "unifi_api_errors_total",
				Help:      "Total number of UniFi API errors",
			},
			[]string{labelProvider, labelOperation},
		),
		UniFiAPIDuration: f.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: namespace,
				Name:      "unifi_api_duration_seconds",
				Help:      "UniFi API request duration in seconds",
				Buckets:   prometheus.DefBuckets,
			},
			[]string{labelProvider, labelOperation},
		),
		UniFiResponseSizeBytes: f.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: namespace,
				Name:      "unifi_response_size_bytes",
				Help:      "UniFi API response size in bytes",
				Buckets:   prometheus.ExponentialBuckets(100, 10, histogramBucketCount),
			},
			[]string{labelOperation},
		),
		UniFiRetriesTotal: f.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "unifi_api_retries_total",
				Help:      "Total number of UniFi API retries triggered by 5xx or 429 responses",
			},
			[]string{labelProvider, labelOperation, "status"},
		),
		UniFiRateLimitsTotal: f.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "unifi_api_rate_limits_total",
				Help:      "Total number of HTTP 429 rate-limit responses received from UniFi",
			},
			[]string{labelProvider, labelOperation},
		),
		PanicsTotal: f.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "http_handler_panics_total",
				Help:      "Total number of panics caught by the HTTP recovery middleware",
			},
			[]string{labelProvider, labelEndpoint},
		),

		ConsecutiveErrors: f.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "consecutive_errors",
				Help:      "Number of consecutive errors",
			},
			[]string{labelProvider},
		),
		LastSuccessTimestamp: f.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "last_success_timestamp",
				Help:      "Timestamp of last successful operation",
			},
			[]string{labelProvider},
		),
		Info: f.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "info",
				Help:      "Information about the webhook instance",
			},
			[]string{"version", labelProvider},
		),
	}

	m.Info.WithLabelValues(version, ProviderName).Set(1)

	return m
}

// RecordHTTPRequest records HTTP request metrics.
func (m *Metrics) RecordHTTPRequest(method, endpoint string, statusCode int, duration time.Duration, responseSize int) {
	m.HTTPRequestsTotal.WithLabelValues(ProviderName, method, endpoint, strconv.Itoa(statusCode)).Inc()
	m.HTTPRequestDuration.WithLabelValues(ProviderName, method, endpoint).Observe(duration.Seconds())
	if responseSize > 0 {
		m.HTTPResponseSizeBytes.WithLabelValues(ProviderName, method, endpoint).Observe(float64(responseSize))
	}
}

// RecordUniFiAPICall records UniFi API call metrics.
func (m *Metrics) RecordUniFiAPICall(operation string, duration time.Duration, responseSize int, err error) {
	m.UniFiAPIDuration.WithLabelValues(ProviderName, operation).Observe(duration.Seconds())
	if responseSize > 0 {
		m.UniFiResponseSizeBytes.WithLabelValues(operation).Observe(float64(responseSize))
	}
	if err != nil {
		m.UniFiAPIErrorsTotal.WithLabelValues(ProviderName, operation).Inc()
	}
}

// RecordOperation records the outcome of one top-level provider operation (a
// full Records or ApplyChanges). The consecutive-error and last-success gauges
// are tracked here — per operation — rather than in RecordUniFiAPICall: a
// single ApplyChanges fans out many concurrent UniFi API calls, and updating
// these gauges from each of them races. One worker's success could Set the
// count to 0 while its siblings are still failing, so "consecutive" lost all
// meaning and an alert on consecutive_errors could silently never fire during a
// failing batch. Per-operation accounting keeps it honest; per-call failures
// are still counted unambiguously by UniFiAPIErrorsTotal.
func (m *Metrics) RecordOperation(err error) {
	if err != nil {
		m.ConsecutiveErrors.WithLabelValues(ProviderName).Inc()

		return
	}
	m.ConsecutiveErrors.WithLabelValues(ProviderName).Set(0)
	m.LastSuccessTimestamp.WithLabelValues(ProviderName).Set(float64(time.Now().Unix()))
}

// UpdateRecordsByType updates the records count by type.
func (m *Metrics) UpdateRecordsByType(recordType string, count int) {
	m.RecordsTotal.WithLabelValues(ProviderName, recordType).Set(float64(count))
}

// RecordChange records a DNS change operation.
func (m *Metrics) RecordChange(operation, recordType string) {
	m.ChangesTotal.WithLabelValues(ProviderName, operation).Inc()
	m.ChangesByTypeTotal.WithLabelValues(ProviderName, operation, recordType).Inc()
}
