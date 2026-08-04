package updater

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/block/proto-fleet/server/internal/updaterapi"
)

type Server struct {
	manager *Manager
	http    *http.Server
}

func NewServer(manager *Manager) *Server {
	server := &Server{manager: manager}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/status", server.handleStatus)
	mux.HandleFunc("/v1/upgrade", server.handleUpgrade)
	server.http = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 3 * time.Second,
	}
	return server
}

func (s *Server) Serve(socketPath string) error {
	if !filepath.IsAbs(socketPath) {
		return fmt.Errorf("socket path must be absolute")
	}
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o750); err != nil {
		return fmt.Errorf("create updater socket directory: %w", err)
	}
	listener, err := listenUpdaterSocket(socketPath)
	if err != nil {
		return err
	}
	// #nosec G302 -- root fleet-api needs read/write access through the
	// bind-mounted Unix socket; the parent directory remains root-only.
	if err := os.Chmod(socketPath, 0o660); err != nil {
		listener.Close()
		return fmt.Errorf("set updater socket permissions: %w", err)
	}
	if err := s.http.Serve(listener); err != nil {
		return fmt.Errorf("serve updater API: %w", err)
	}
	return nil
}

func listenUpdaterSocket(socketPath string) (*net.UnixListener, error) {
	address := &net.UnixAddr{Name: socketPath, Net: "unix"}
	listener, err := net.ListenUnix("unix", address)
	if err == nil {
		listener.SetUnlinkOnClose(true)
		return listener, nil
	}
	if !errors.Is(err, syscall.EADDRINUSE) {
		return nil, fmt.Errorf("listen on updater socket: %w", err)
	}
	info, statErr := os.Lstat(socketPath)
	if statErr != nil {
		return nil, fmt.Errorf("inspect occupied updater socket path: %w", statErr)
	}
	if info.Mode()&os.ModeSocket == 0 {
		return nil, fmt.Errorf("refusing to replace non-socket path at updater socket location")
	}
	connection, dialErr := net.DialTimeout("unix", socketPath, 250*time.Millisecond)
	if dialErr == nil {
		_ = connection.Close()
		return nil, fmt.Errorf("another updater is already listening on the configured socket")
	}
	if !errors.Is(dialErr, syscall.ECONNREFUSED) {
		return nil, fmt.Errorf("refusing to replace updater socket with ambiguous owner: %w", dialErr)
	}
	if err := os.Remove(socketPath); err != nil {
		return nil, fmt.Errorf("remove stale updater socket: %w", err)
	}
	listener, err = net.ListenUnix("unix", address)
	if err != nil {
		return nil, fmt.Errorf("listen on updater socket after stale cleanup: %w", err)
	}
	listener.SetUnlinkOnClose(true)
	return listener, nil
}

func (s *Server) Shutdown(ctx context.Context) error {
	if err := s.http.Shutdown(ctx); err != nil {
		closeErr := s.http.Close()
		return errors.Join(
			fmt.Errorf("shut down updater API: %w", err),
			closeErr,
		)
	}
	return nil
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeJSON(w, http.StatusMethodNotAllowed, updaterapi.ErrorResponse{Error: "method not allowed"})
		return
	}
	writeJSON(w, http.StatusOK, s.manager.Status())
}

func (s *Server) handleUpgrade(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeJSON(w, http.StatusMethodNotAllowed, updaterapi.ErrorResponse{Error: "method not allowed"})
		return
	}
	defer r.Body.Close()
	var request updaterapi.TriggerRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, updaterapi.ErrorResponse{Error: "invalid request body"})
		return
	}
	operation, err := s.manager.Trigger(request.TargetVersion)
	if err != nil {
		status := http.StatusBadRequest
		if current := s.manager.Status().Operation; current != nil && !current.Phase.Terminal() {
			status = http.StatusConflict
		}
		writeJSON(w, status, updaterapi.ErrorResponse{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusAccepted, updaterapi.TriggerResponse{Operation: operation})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("encode updater response: %v", err)
	}
}
