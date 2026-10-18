package server

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// ProbeFunc is the signature the health server invokes to decide whether the
// process should report ready. Returning a non-nil error fails the probe.
type ProbeFunc func(ctx context.Context) error

// probeTimeout bounds a single upstream readiness probe. The probe runs
// detached from any caller's request context (see Check), so it needs its own
// ceiling — without one a hung upstream connection (the UniFi client sets no
// http.Client timeout unless UNIFI_REQUEST_TIMEOUT is configured, relying on
// ctx) would block the singleflight slot forever.
//
// Deliberately NOT sized to the UniFi client's worst-case retry budget (which
// Retry-After hints can stretch to tens of seconds): a controller deep in a
// rate-limit or outage episode should read as not-ready rather than keep the
// pod in rotation while a probe grinds through its full backoff schedule. The
// next cached probe re-checks once the TTL lapses.
const probeTimeout = 10 * time.Second

// cachedProbe wraps a ProbeFunc with a TTL-bounded result cache and
// singleflight deduplication. Multiple concurrent /readyz hits within the
// TTL share a single in-flight check; subsequent hits in that window return
// the memoised verdict without touching the upstream.
type cachedProbe struct {
	probe ProbeFunc
	ttl   time.Duration
	sf    singleflight.Group

	mu      sync.RWMutex
	lastAt  time.Time
	lastErr error
}

func newCachedProbe(probe ProbeFunc, ttl time.Duration) *cachedProbe {
	return &cachedProbe{probe: probe, ttl: ttl}
}

// Check returns the latest cached result if it is still within ttl, otherwise
// it runs the probe (deduped) and returns its result.
func (c *cachedProbe) Check(ctx context.Context) error {
	c.mu.RLock()
	fresh := !c.lastAt.IsZero() && time.Since(c.lastAt) < c.ttl
	cached := c.lastErr
	c.mu.RUnlock()
	if fresh {
		return cached
	}

	_, err, _ := c.sf.Do("probe", func() (any, error) {
		// Re-check inside the singleflight in case another caller refreshed
		// the cache while we were queued.
		c.mu.RLock()
		stillFresh := !c.lastAt.IsZero() && time.Since(c.lastAt) < c.ttl
		stillCached := c.lastErr
		c.mu.RUnlock()
		if stillFresh {
			return nil, stillCached
		}

		// Detach from the caller's request context before probing. singleflight
		// shares the leader's result with every queued caller, so if the leader's
		// context is cancelled (a kubelet probe timeout, a dropped /readyz
		// connection) the resulting context.Canceled would (a) fail all other
		// in-flight callers whose own contexts are still alive and (b) get
		// memoised as the readiness verdict for the whole TTL — knocking a healthy
		// pod out of Service rotation. WithoutCancel keeps any context values but
		// drops the cancellation; a fresh timeout still bounds the probe.
		pctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), probeTimeout)
		err := c.probe(pctx)
		cancel()

		if err != nil {
			// Log the full cause here: /readyz returns only a generic "not
			// ready" to its (possibly unauthenticated) caller, so the pod log is
			// where operators see which upstream failed and why. Logging at the
			// probe site rather than per request means one line per actual probe
			// run, not one per cached /readyz hit. See #225.
			slog.Warn("readiness probe failed", "error", err)
		}

		c.mu.Lock()
		c.lastAt = time.Now()
		c.lastErr = err
		c.mu.Unlock()

		return nil, err
	})

	return err
}

// noopProbe is used when the caller does not wire a real probe — keeps
// /readyz returning 200 for backwards compatibility in tests.
func noopProbe(_ context.Context) error { return nil }
