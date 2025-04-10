package api

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"connectrpc.com/authn"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

type HTTPConfig struct {
	Address           string        `help:"Address to listen on" default:"127.0.0.1:8080" env:"LISTEN_ADDRESS"`
	ReadHeaderTimeout time.Duration `help:"Read header timeout" default:"3s" env:"READ_HEADER_TIMEOUT"`
	StaticAssetPath   string        `help:"Static asset path" env:"STATIC_ASSET_PATH"`
}

func RunServer(config *HTTPConfig, requestHandlers []HandlerWithPath, authMiddleware *authn.Middleware) error {

	slog.Info("starting fleet", slog.String("addr", config.Address))

	slog.Info("serving static files from dir", slog.String("path", config.StaticAssetPath))

	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.Dir(config.StaticAssetPath)))

	mux.HandleFunc("/health", HealthHandler)

	var handler http.Handler = mux
	if authMiddleware != nil {
		handler = authMiddleware.Wrap(handler)
	}
	handler = h2c.NewHandler(handler, &http2.Server{})

	for _, requestHandler := range requestHandlers {
		mux.Handle(requestHandler.Path, requestHandler.Handler)
	}

	httpServer := http.Server{
		Addr:              config.Address,
		Handler:           handler,
		ReadHeaderTimeout: config.ReadHeaderTimeout,
	}

	return fmt.Errorf("http server error: %w", httpServer.ListenAndServe())
}

func HealthHandler(w http.ResponseWriter, r *http.Request) {
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

type HandlerWithPath struct {
	Path    string
	Handler http.Handler
}

type Middleware func(next http.Handler) http.Handler
