package translator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const (
	DefaultHelperSocket = "/run/proto-fleet-sv2-helper/runtime.sock"
	DefaultDockerSocket = "/var/run/docker.sock"
)

type RuntimeHelperConfig struct {
	ListenSocket string
	DockerSocket string
}

// ServeRuntimeHelper exposes only the fixed translator inspect/start/stop
// operations over a private Unix socket. The helper is the sole component
// allowed to mount the host Docker socket.
func ServeRuntimeHelper(ctx context.Context, config RuntimeHelperConfig) error {
	if !filepath.IsAbs(config.ListenSocket) {
		return fmt.Errorf("SV2 runtime helper listen socket must be absolute")
	}
	if !filepath.IsAbs(config.DockerSocket) {
		return fmt.Errorf("SV2 runtime helper Docker socket must be absolute")
	}
	if config.ListenSocket == config.DockerSocket {
		return fmt.Errorf("SV2 runtime helper sockets must be distinct")
	}
	if err := os.MkdirAll(filepath.Dir(config.ListenSocket), 0o700); err != nil {
		return fmt.Errorf("create SV2 runtime helper socket directory: %w", err)
	}
	if err := removeStaleSocket(config.ListenSocket); err != nil {
		return err
	}

	listener, err := net.Listen("unix", config.ListenSocket)
	if err != nil {
		return fmt.Errorf("listen on SV2 runtime helper socket: %w", err)
	}
	defer listener.Close()
	defer os.Remove(config.ListenSocket)
	if err := os.Chmod(config.ListenSocket, 0o600); err != nil {
		return fmt.Errorf("restrict SV2 runtime helper socket: %w", err)
	}

	server := &http.Server{
		Handler:           newRuntimeHelperHandler(newDockerRuntime(config.DockerSocket)),
		ReadHeaderTimeout: 2 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    8 * 1024,
	}
	stopShutdown := context.AfterFunc(ctx, func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			slog.Error("shut down SV2 runtime helper", "error", err)
		}
	})
	defer stopShutdown()

	if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve SV2 runtime helper: %w", err)
	}
	return nil
}

func removeStaleSocket(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect SV2 runtime helper socket: %w", err)
	}
	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("refusing to replace non-socket SV2 runtime helper path")
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove stale SV2 runtime helper socket: %w", err)
	}
	return nil
}

func newRuntimeHelperHandler(runtime containerRuntime) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.RawQuery != "" || request.Body == nil {
			http.Error(response, "invalid lifecycle helper request", http.StatusBadRequest)
			return
		}
		body, err := io.ReadAll(io.LimitReader(request.Body, 1))
		if err != nil || len(body) != 0 {
			http.Error(response, "lifecycle helper requests must have an empty body", http.StatusBadRequest)
			return
		}

		var state runtimeState
		switch request.URL.Path {
		case "/state":
			if request.Method != http.MethodGet {
				response.Header().Set("Allow", http.MethodGet)
				http.Error(response, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			state, err = runtime.State(request.Context())
		case "/start":
			if request.Method != http.MethodPost {
				response.Header().Set("Allow", http.MethodPost)
				http.Error(response, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			err = runtime.EnsureStarted(request.Context())
			if err == nil {
				state, err = runtime.State(request.Context())
			}
		case "/stop":
			if request.Method != http.MethodPost {
				response.Header().Set("Allow", http.MethodPost)
				http.Error(response, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			err = runtime.Stop(request.Context())
			if err == nil {
				state, err = runtime.State(request.Context())
			}
		default:
			http.NotFound(response, request)
			return
		}
		if err != nil {
			http.Error(response, err.Error(), http.StatusInternalServerError)
			return
		}
		response.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(response).Encode(state); err != nil {
			slog.Error("encode SV2 runtime helper response", "error", err)
		}
	})
}
