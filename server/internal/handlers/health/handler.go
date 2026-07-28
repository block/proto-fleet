package health

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

func NewHandler() func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		writeOK(w, r, "health")
	}
}

type Pinger interface {
	PingContext(ctx context.Context) error
}

// How long a ping result is served from cache. The endpoint is
// unauthenticated on the public listener, so per-request pings would let a
// request flood consume DB pool slots; the cache bounds DB work to one ping
// per interval no matter the request rate.
const readyCacheInterval = 2 * time.Second

// NewReadyHandler reports traffic readiness: 200 only while this process is
// active and the database answers a ping. /health stays a static liveness
// check.
func NewReadyHandler(db Pinger, active ActiveState) func(w http.ResponseWriter, r *http.Request) {
	var mu sync.Mutex
	var lastCheck time.Time
	var lastErr error
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if !active.Active() {
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}

		mu.Lock()
		if time.Since(lastCheck) >= readyCacheInterval {
			// Background, not the request context: the result is shared, so
			// one disconnecting client must not poison the cache.
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			lastErr = db.PingContext(ctx)
			cancel()
			lastCheck = time.Now()
		}
		err := lastErr
		mu.Unlock()

		if err != nil {
			slog.Error("Readiness check failed to ping database",
				"error", err,
				"handler", "health-ready",
				"path", r.URL.Path,
			)
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}

		writeOK(w, r, "health-ready")
	}
}

type ActiveState interface {
	Active() bool
}

// NewActiveHandler reports whether this process currently owns a healthy
// active runtime. It does not grant ownership.
func NewActiveHandler(state ActiveState) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if !state.Active() {
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		writeOK(w, r, "health-active")
	}
}

func writeOK(w http.ResponseWriter, r *http.Request, handler string) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte("ok")); err != nil {
		slog.Error("Failed to write health check response",
			"error", err,
			"handler", handler,
			"path", r.URL.Path,
		)
	}
}
