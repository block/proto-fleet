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
	if err := prepareUpdaterSocketDirectory(socketPath); err != nil {
		return err
	}
	listener, err := listenUpdaterSocket(socketPath)
	if err != nil {
		return err
	}
	if err := secureUpdaterSocket(socketPath); err != nil {
		listener.Close()
		return err
	}
	if err := s.http.Serve(listener); err != nil {
		return fmt.Errorf("serve updater API: %w", err)
	}
	return nil
}

// prepareUpdaterSocketDirectory establishes the filesystem authentication
// boundary for the updater API. The socket is not network-reachable and its
// intended caller is the root fleet-api process, so a trusted directory chain
// plus a daemon-owned 0660 socket is sufficient without a second application
// authentication protocol.
func prepareUpdaterSocketDirectory(socketPath string) error {
	socketDir := filepath.Dir(socketPath)
	if err := os.MkdirAll(socketDir, 0o750); err != nil {
		return fmt.Errorf("create updater socket directory: %w", err)
	}
	if err := validateUpdaterSocketDirectoryChain(socketDir); err != nil {
		return fmt.Errorf("validate updater socket directory: %w", err)
	}
	return nil
}

// validateUpdaterSocketDirectoryChain checks both the configured and resolved
// paths. A protected symlink in an ancestor (for example, /tmp on macOS) is
// safe once its containing and target directory chains are both trusted, but
// the socket directory itself must never be a symlink.
func validateUpdaterSocketDirectoryChain(socketDir string) error {
	if err := walkUpdaterSocketDirectoryChain(socketDir, true); err != nil {
		return err
	}
	resolvedDir, err := filepath.EvalSymlinks(socketDir)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", socketDir, err)
	}
	if resolvedDir != socketDir {
		if err := walkUpdaterSocketDirectoryChain(resolvedDir, true); err != nil {
			return err
		}
	}
	return nil
}

func walkUpdaterSocketDirectoryChain(path string, rejectFirstSymlink bool) error {
	euid := uint32(os.Geteuid()) //nolint:gosec // Effective UIDs are non-negative on supported Unix hosts.
	current := filepath.Clean(path)
	first := true
	for {
		info, err := os.Lstat(current)
		if err != nil {
			return fmt.Errorf("inspect path component %s: %w", current, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			if first && rejectFirstSymlink {
				return fmt.Errorf("socket directory %s must not be a symlink", current)
			}
			// The containing directory is checked below and the symlink target
			// is checked by the resolved-path walk above.
		} else {
			if !info.IsDir() {
				return fmt.Errorf("path component %s is not a directory", current)
			}
			stat, ok := info.Sys().(*syscall.Stat_t)
			if !ok {
				return fmt.Errorf("path component %s has unsupported stat type %T", current, info.Sys())
			}
			if stat.Uid != 0 && stat.Uid != euid {
				return fmt.Errorf("path component %s owner uid %d must be root or daemon uid %d", current, stat.Uid, euid)
			}
			mode := info.Mode()
			if mode.Perm()&0o022 != 0 && (first || mode&os.ModeSticky == 0) {
				return fmt.Errorf("path component %s mode %#o must not be group- or world-writable", current, mode.Perm())
			}
		}

		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
		first = false
	}
	return nil
}

func secureUpdaterSocket(socketPath string) error {
	// A setgid socket directory can otherwise make a newly bound socket
	// inherit an unintended group. Set both IDs explicitly before chmod because
	// chown is permitted to clear mode bits on supported Unix hosts.
	if err := os.Chown(socketPath, os.Geteuid(), os.Getegid()); err != nil {
		return fmt.Errorf("set updater socket ownership: %w", err)
	}
	// #nosec G302 -- the root fleet-api caller needs read/write access through
	// the bind-mounted Unix socket; the validated parent directory is 0750 or
	// stricter and every writable ancestor is protected by the sticky bit.
	if err := os.Chmod(socketPath, 0o660); err != nil {
		return fmt.Errorf("set updater socket permissions: %w", err)
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
