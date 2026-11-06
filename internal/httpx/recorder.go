// Package httpx holds small HTTP helpers shared across the webhook's
// middleware layers.
package httpx

import "net/http"

// ResponseRecorder wraps an http.ResponseWriter to capture the status code and
// the number of body bytes written, so middleware can record them after the
// handler returns. Status defaults to http.StatusOK, which the net/http server
// writes implicitly when a handler calls Write without an explicit WriteHeader.
type ResponseRecorder struct {
	http.ResponseWriter

	status  int
	written int
}

// NewResponseRecorder wraps w with the status pre-set to http.StatusOK.
func NewResponseRecorder(w http.ResponseWriter) *ResponseRecorder {
	return &ResponseRecorder{ResponseWriter: w, status: http.StatusOK}
}

func (r *ResponseRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// Write is a transparent passthrough that also counts the bytes written. It
// does not re-annotate the underlying Write error. A ResponseWriter wrapper
// must surface the original error unchanged.
func (r *ResponseRecorder) Write(b []byte) (int, error) {
	n, err := r.ResponseWriter.Write(b)
	r.written += n

	return n, err
}

// Status returns the captured status code.
func (r *ResponseRecorder) Status() int { return r.status }

// Written returns the number of body bytes written to the client.
func (r *ResponseRecorder) Written() int { return r.written }
