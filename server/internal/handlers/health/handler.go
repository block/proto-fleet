package health

import (
	"context"
	"log/slog"
	"net/http"
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

// NewReadyHandler reports readiness: 200 when the database answers a ping,
// 503 otherwise. /health stays a static liveness check.
func NewReadyHandler(db Pinger) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		if err := db.PingContext(ctx); err != nil {
			slog.Error("Readiness check failed to ping database", "error", err)
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}

		writeOK(w, r, "health-ready")
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
