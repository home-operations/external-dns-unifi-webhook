package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/pprof"
	"time"

	"github.com/home-operations/external-dns-unifi-webhook/internal/config"
	"github.com/home-operations/external-dns-unifi-webhook/internal/metrics"
	"github.com/home-operations/external-dns-unifi-webhook/internal/webhook"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"golang.org/x/sync/errgroup"
)

const shutdownTimeout = 30 * time.Second

// Run starts the webhook and health HTTP servers and blocks until ctx is done
// or either server fails. probe is invoked by /readyz with a TTL-cached,
// singleflight-deduped wrapper — pass server.NoopProbe to skip the check.
// It returns the first error encountered (other than the expected context
// cancellation that triggers a graceful shutdown).
func Run(ctx context.Context, cfg *config.Config, whk *webhook.Webhook, probe ProbeFunc) error {
	if probe == nil {
		probe = noopProbe
	}

	// One cached readyz handler shared across both muxes so probes from either
	// port hit the same upstream-result cache.
	readyz := readyzHandler(probe, cfg.ReadinessCacheTTL)

	mainSrv := newServer(
		fmt.Sprintf("%s:%d", cfg.ServerHost, cfg.ServerPort),
		buildMainHandler(cfg, whk, readyz),
		cfg,
	)
	healthSrv := newServer(cfg.HealthServerAddr, buildHealthHandler(cfg, readyz), cfg)

	group, ctx := errgroup.WithContext(ctx)

	group.Go(func() error { return serve(ctx, mainSrv, "webhook") })
	group.Go(func() error { return serve(ctx, healthSrv, "health") })

	return group.Wait()
}

// NoopProbe is exported so callers without a real upstream check can still
// satisfy the Run signature.
var NoopProbe ProbeFunc = noopProbe

func buildMainHandler(cfg *config.Config, whk *webhook.Webhook, readyz http.HandlerFunc) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", whk.Negotiate)
	mux.HandleFunc("GET /records", whk.Records)
	mux.HandleFunc("POST /records", whk.ApplyChanges)
	mux.HandleFunc("POST /adjustendpoints", whk.AdjustEndpoints)

	// Health endpoints are mounted here too so Kubernetes probes can target
	// the webhook port. The external-dns Helm chart only exposes one
	// container port for the webhook sidecar, so probes pointed at the
	// health server's port would need extra chart wiring to work.
	mountHealth(mux, readyz)

	// Middleware order (outermost first): recovery+log -> metrics -> body limit -> mux.
	// recovery must be outermost so it catches panics in any inner layer, and
	// the body limit goes innermost so the wrapped reader is the one the
	// handler actually decodes.
	var h http.Handler = mux
	h = limitBody(cfg.ServerMaxBodyBytes)(h)
	h = metrics.HTTPMetricsMiddleware(h)
	h = recoveryAndAccessLog(h)

	return h
}

func buildHealthHandler(cfg *config.Config, readyz http.HandlerFunc) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", promhttp.Handler())
	mountHealth(mux, readyz)

	if cfg.PprofEnabled {
		slog.Warn("pprof endpoints are enabled on the health server. Unless you investigate a problem, disable them in production")
		mux.HandleFunc("GET /debug/pprof/", pprof.Index)
		mux.HandleFunc("GET /debug/pprof/cmdline", pprof.Cmdline)
		mux.HandleFunc("GET /debug/pprof/profile", pprof.Profile)
		mux.HandleFunc("GET /debug/pprof/symbol", pprof.Symbol)
		mux.HandleFunc("GET /debug/pprof/trace", pprof.Trace)
	}

	return recoveryAndAccessLog(mux)
}

// mountHealth registers the /healthz and /readyz endpoints on mux. Both the
// webhook and health muxes expose them so Kubernetes probes can target either
// port.
func mountHealth(mux *http.ServeMux, readyz http.HandlerFunc) {
	mux.HandleFunc("GET /healthz", okHandler)
	mux.HandleFunc("GET /readyz", readyz)
}

func okHandler(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte("OK")); err != nil {
		slog.Error("writing health response", "error", err)
	}
}

func readyzHandler(probe ProbeFunc, ttl time.Duration) http.HandlerFunc {
	cached := newCachedProbe(probe, ttl)

	return func(w http.ResponseWriter, r *http.Request) {
		if err := cached.Check(r.Context()); err != nil {
			// Return a generic body. The probe error chain embeds the UniFi
			// host (and, in cloud mode, the console ID) plus a slice of the
			// controller's error response; the health server listens on
			// 0.0.0.0 with no auth, so echoing that to the caller leaks
			// internal topology. The cause is logged server-side instead
			// (see cachedProbe.Check). See #225.
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("not ready\n"))

			return
		}
		okHandler(w, r)
	}
}

func newServer(addr string, handler http.Handler, cfg *config.Config) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadTimeout:       cfg.ServerReadTimeout,
		ReadHeaderTimeout: cfg.ServerReadHeaderTimeout,
		WriteTimeout:      cfg.ServerWriteTimeout,
		IdleTimeout:       cfg.ServerIdleTimeout,
		MaxHeaderBytes:    cfg.ServerMaxHeaderBytes,
	}
}

// serve runs srv.ListenAndServe and shuts it down when ctx is cancelled. A
// clean shutdown (or ErrServerClosed) returns nil; any other error propagates.
func serve(ctx context.Context, srv *http.Server, name string) error {
	errCh := make(chan error, 1)
	go func() {
		slog.Info("starting server", "name", name, "address", srv.Addr)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}

		return fmt.Errorf("%s server: %w", name, err)
	case <-ctx.Done():
		slog.Info("shutting down server", "name", name)
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()

		if err := srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("%s server shutdown: %w", name, err)
		}

		return nil
	}
}
