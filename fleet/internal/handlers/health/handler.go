package health

import (
	"log/slog"
	"net/http"
)

func NewHandler() func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte("ok")); err != nil {
			slog.Error("Failed to write health check response",
				"error", err,
				"handler", "health",
				"path", r.URL.Path,
			)
		}
	}
}
