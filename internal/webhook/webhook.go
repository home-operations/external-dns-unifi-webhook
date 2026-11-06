package webhook

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/home-operations/external-dns-unifi-webhook/internal/metrics"

	"sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/plan"
	externaldnsprovider "sigs.k8s.io/external-dns/provider"
)

const (
	contentTypeHeader    = "Content-Type"
	contentTypePlaintext = "text/plain"
	acceptHeader         = "Accept"
	varyHeader           = "Vary"

	endpointTypeAccept      = "accept"
	endpointTypeContentType = "content-type"
)

var errMissingHeader = errors.New("missing required header")

// Webhook adapts the external-dns provider.Provider interface to the
// HTTP-based ExternalDNS webhook protocol.
type Webhook struct {
	provider externaldnsprovider.Provider
}

// New creates a new instance of the Webhook.
func New(provider externaldnsprovider.Provider) *Webhook {
	return &Webhook{provider: provider}
}

// Records handles GET /records.
func (p *Webhook) Records(w http.ResponseWriter, r *http.Request) {
	if err := p.checkAccept(w, r); err != nil {
		return
	}

	records, err := p.provider.Records(r.Context())
	if err != nil {
		requestLog(r).Error("getting records", "error", err)
		w.Header().Set(contentTypeHeader, contentTypePlaintext)
		w.WriteHeader(http.StatusInternalServerError)

		return
	}

	writeJSON(w, r, records, "records")
}

// ApplyChanges handles POST /records.
func (p *Webhook) ApplyChanges(w http.ResponseWriter, r *http.Request) {
	if err := p.checkContentType(w, r); err != nil {
		return
	}

	changes, ok := decodeBody[plan.Changes](w, r, "/records")
	if !ok {
		return
	}

	requestLog(r).Debug("executing plan changes",
		"create", len(changes.Create),
		"update_old", len(changes.UpdateOld),
		"update_new", len(changes.UpdateNew),
		"delete", len(changes.Delete),
	)

	if err := p.provider.ApplyChanges(r.Context(), &changes); err != nil {
		requestLog(r).Error("applying changes", "error", err)
		w.Header().Set(contentTypeHeader, contentTypePlaintext)
		w.WriteHeader(http.StatusInternalServerError)

		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// AdjustEndpoints handles POST /adjustendpoints.
func (p *Webhook) AdjustEndpoints(w http.ResponseWriter, r *http.Request) {
	metrics.Get().AdjustEndpointsTotal.WithLabelValues(metrics.ProviderName).Inc()

	if err := p.checkContentType(w, r); err != nil {
		return
	}
	if err := p.checkAccept(w, r); err != nil {
		return
	}

	endpoints, ok := decodeBody[[]*endpoint.Endpoint](w, r, "/adjustendpoints")
	if !ok {
		return
	}

	endpoints, err := p.provider.AdjustEndpoints(endpoints)
	if err != nil {
		requestLog(r).Error("adjusting endpoints", "error", err)
		w.Header().Set(contentTypeHeader, contentTypePlaintext)
		w.WriteHeader(http.StatusInternalServerError)

		return
	}

	writeJSON(w, r, endpoints, "endpoints")
}

// Negotiate handles GET / and returns the provider's domain filter.
func (p *Webhook) Negotiate(w http.ResponseWriter, r *http.Request) {
	m := metrics.Get()
	m.NegotiateTotal.WithLabelValues(metrics.ProviderName).Inc()

	if err := p.checkAccept(w, r); err != nil {
		return
	}

	w.Header().Set(contentTypeHeader, string(mediaTypeVersion1))
	if err := json.NewEncoder(w).Encode(p.provider.GetDomainFilter()); err != nil {
		// The encoder has already written (and implicitly flushed a 200) by the
		// time it can fail, so a WriteHeader(500) here is a no-op that only logs
		// a "superfluous WriteHeader" warning. Just record the cause, matching
		// writeJSON. See #228.
		requestLog(r).Error("encoding negotiate response", "error", err)
	}
}

// checkContentType validates the Content-Type header on incoming requests.
// On failure it writes an error response and returns the validation error.
func (p *Webhook) checkContentType(w http.ResponseWriter, r *http.Request) error {
	return validateMediaHeader(w, r, contentTypeHeader, endpointTypeContentType,
		"client must provide a content type",
		"client must provide a valid versioned media type in the content type")
}

// checkAccept validates the Accept header on incoming requests.
func (p *Webhook) checkAccept(w http.ResponseWriter, r *http.Request) error {
	return validateMediaHeader(w, r, acceptHeader, endpointTypeAccept,
		"client must provide an accept header",
		"client must provide a valid versioned media type in the accept header")
}

func validateMediaHeader(
	w http.ResponseWriter,
	r *http.Request,
	headerName, kind, missingMsg, unsupportedMsg string,
) error {
	m := metrics.Get()
	value := r.Header.Get(headerName)

	if value == "" {
		m.HTTPValidationErrorsTotal.WithLabelValues(metrics.ProviderName, kind).Inc()
		writePlainError(w, r, http.StatusNotAcceptable, missingMsg)

		return fmt.Errorf("%w: %s", errMissingHeader, headerName)
	}

	if _, err := checkAndGetMediaTypeHeaderValue(value); err != nil {
		m.HTTPValidationErrorsTotal.WithLabelValues(metrics.ProviderName, kind).Inc()
		writePlainError(w, r, http.StatusUnsupportedMediaType, fmt.Sprintf("%s: %v", unsupportedMsg, err))

		return err
	}

	return nil
}

// decodeBody decodes the JSON request body into T. On failure it writes the
// error response and returns ok=false so the caller can return. The response
// is 413 when the body exceeds the configured size limit. Otherwise it is 400,
// and the JSON error counts against the endpoint label of route. ApplyChanges
// and AdjustEndpoints share this decode handling.
func decodeBody[T any](w http.ResponseWriter, r *http.Request, route string) (T, bool) {
	var v T
	if err := json.NewDecoder(r.Body).Decode(&v); err != nil {
		if isBodyTooLarge(err) {
			writePlainError(w, r, http.StatusRequestEntityTooLarge, "request body too large")

			return v, false
		}
		metrics.Get().HTTPJSONErrorsTotal.WithLabelValues(metrics.ProviderName, route).Inc()
		writePlainError(w, r, http.StatusBadRequest, fmt.Sprintf("error decoding request body: %v", err))

		return v, false
	}

	return v, true
}

// writeJSON encodes v as the versioned-media-type response shared by the
// Records and AdjustEndpoints handlers. A failed encode is logged but not
// surfaced — the 200 status line is already on the wire by then. what labels
// the log message ("records", "endpoints").
func writeJSON(w http.ResponseWriter, r *http.Request, v any, what string) {
	w.Header().Set(contentTypeHeader, string(mediaTypeVersion1))
	w.Header().Set(varyHeader, contentTypeHeader)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		requestLog(r).Error("encoding "+what+" response", "error", err)
	}
}

// writePlainError writes a plaintext error response. A failed write is logged
// but does not propagate — at that point the client is gone anyway.
func writePlainError(w http.ResponseWriter, r *http.Request, status int, msg string) {
	w.Header().Set(contentTypeHeader, contentTypePlaintext)
	w.WriteHeader(status)
	if _, err := fmt.Fprint(w, msg); err != nil {
		requestLog(r).Error("writing error response", "error", err)
	}
}

func requestLog(r *http.Request) *slog.Logger {
	return slog.With("method", r.Method, "path", r.URL.Path)
}

// isBodyTooLarge reports whether err comes from a MaxBytesReader having been
// exceeded — used by the POST handlers to map decode failures to 413.
func isBodyTooLarge(err error) bool {
	_, ok := errors.AsType[*http.MaxBytesError](err)

	return ok
}
