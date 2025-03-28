package api

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

type HTTPConfig struct {
	Address                    string `help:"Address to listen on" default:"127.0.0.1:8080" env:"LISTEN_ADDRESS"`
	ReadHeaderTimeoutInSeconds int64  `help:"Read header timeout in seconds" default:"3" env:"READ_HEADER_TIMEOUT_IN_SECONDS"`
	StaticAssetPath            string `help:"Static asset path" env:"STATIC_ASSET_PATH"`
}

func RunServer(config *HTTPConfig, requestHandlers []HandlerWithPath) error {

	slog.Info("starting fleet", slog.String("addr", config.Address))

	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.Dir(config.StaticAssetPath)))

	for _, requestHandler := range requestHandlers {
		mux.Handle(requestHandler.Path, requestHandler.Handler)
	}

	httpServer := http.Server{
		Addr:              config.Address,
		Handler:           h2c.NewHandler(mux, &http2.Server{}),
		ReadHeaderTimeout: time.Duration(config.ReadHeaderTimeoutInSeconds) * time.Second,
	}

	return fmt.Errorf("http server error: %w", httpServer.ListenAndServe())
}

type HandlerWithPath struct {
	Path    string
	Handler http.Handler
}

type Middleware func(next http.Handler) http.Handler
