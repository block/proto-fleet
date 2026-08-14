package updater

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/block/proto-fleet/server/internal/updaterapi"
)

type Server struct {
	manager   *Manager
	http      *http.Server
	ready     chan struct{}
	readyOnce sync.Once
}

func NewServer(manager *Manager) *Server {
	server := &Server{manager: manager, ready: make(chan struct{})}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/status", server.handleStatus)
	mux.HandleFunc("/v1/upgrade", server.handleUpgrade)
	mux.HandleFunc("/v1/acknowledge", server.handleAcknowledge)
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
	canonicalSocketPath, err := prepareUpdaterSocketDirectory(socketPath)
	if err != nil {
		return err
	}
	listener, err := listenUpdaterSocket(canonicalSocketPath)
	if err != nil {
		return err
	}
	if err := secureUpdaterSocket(canonicalSocketPath); err != nil {
		listener.Close()
		return err
	}
	s.readyOnce.Do(func() { close(s.ready) })
	if err := s.http.Serve(listener); err != nil {
		return fmt.Errorf("serve updater API: %w", err)
	}
	return nil
}

// Ready closes only after the Unix socket is bound and its ownership and mode
// establish the local authentication boundary. Callers can use it as the
// startup-commit point for a one-shot updater handoff.
func (s *Server) Ready() <-chan struct{} {
	return s.ready
}

// prepareUpdaterSocketDirectory establishes the filesystem authentication
// boundary for the updater API. The socket is not network-reachable and its
// intended caller is the root fleet-api process, so a trusted directory chain
// plus a daemon-owned 0660 socket is sufficient without a second application
// authentication protocol. It returns a canonical path so binding and
// permission changes never resolve configured ancestor symlinks again.
func prepareUpdaterSocketDirectory(socketPath string) (string, error) {
	socketPath = filepath.Clean(socketPath)
	socketDir := filepath.Dir(socketPath)
	if err := os.MkdirAll(socketDir, 0o750); err != nil {
		return "", fmt.Errorf("create updater socket directory: %w", err)
	}
	canonicalSocketDir, err := validateUpdaterSocketDirectoryChain(socketDir)
	if err != nil {
		return "", fmt.Errorf("validate updater socket directory: %w", err)
	}
	return filepath.Join(canonicalSocketDir, filepath.Base(socketPath)), nil
}

// validateUpdaterSocketDirectoryChain checks both the configured and resolved
// paths. A protected symlink in an ancestor (for example, /tmp on macOS) is
// safe once its containing and target directory chains are both trusted, but
// the socket directory itself must never be a symlink.
func validateUpdaterSocketDirectoryChain(socketDir string) (string, error) {
	if err := walkUpdaterSocketDirectoryChain(socketDir); err != nil {
		return "", err
	}
	resolvedDir, err := filepath.EvalSymlinks(socketDir)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", socketDir, err)
	}
	if resolvedDir != socketDir {
		if err := walkUpdaterSocketDirectoryChain(resolvedDir); err != nil {
			return "", err
		}
	}

	configuredInfo, err := os.Lstat(socketDir)
	if err != nil {
		return "", fmt.Errorf("inspect configured socket directory: %w", err)
	}
	canonicalInfo, err := os.Lstat(resolvedDir)
	if err != nil {
		return "", fmt.Errorf("inspect canonical socket directory: %w", err)
	}
	if !os.SameFile(configuredInfo, canonicalInfo) {
		return "", fmt.Errorf("updater socket directory changed while it was validated")
	}
	return resolvedDir, nil
}

func walkUpdaterSocketDirectoryChain(path string) error {
	euid := uint32(os.Geteuid()) //nolint:gosec // Effective UIDs are non-negative on supported Unix hosts.
	current := filepath.Clean(path)
	first := true
	for {
		info, err := os.Lstat(current)
		if err != nil {
			return fmt.Errorf("inspect path component %s: %w", current, err)
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			return fmt.Errorf("path component %s has unsupported stat type %T", current, info.Sys())
		}
		if err := validateUpdaterSocketPathComponent(current, info.Mode(), stat.Uid, euid, first); err != nil {
			return err
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

func validateUpdaterSocketPathComponent(path string, mode os.FileMode, ownerUID, daemonUID uint32, isSocketDirectory bool) error {
	if ownerUID != 0 && ownerUID != daemonUID {
		return fmt.Errorf("path component %s owner uid %d must be root or daemon uid %d", path, ownerUID, daemonUID)
	}
	if mode&os.ModeSymlink != 0 {
		if isSocketDirectory {
			return fmt.Errorf("socket directory %s must not be a symlink", path)
		}
		// The containing directory is checked by this walk and the symlink
		// target is checked by the resolved-path walk above.
		return nil
	}
	if !mode.IsDir() {
		return fmt.Errorf("path component %s is not a directory", path)
	}
	if mode.Perm()&0o022 != 0 && (isSocketDirectory || mode&os.ModeSticky == 0) {
		return fmt.Errorf("path component %s mode %#o must not be group- or world-writable", path, mode.Perm())
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
	if err := decodeSingleJSONValue(http.MaxBytesReader(w, r.Body, 4096), &request); err != nil {
		writeJSON(w, http.StatusBadRequest, updaterapi.ErrorResponse{Error: "invalid request body"})
		return
	}
	var operation updaterapi.Operation
	var err error
	if request.Complete {
		operation, err = s.manager.TriggerCompleteWithID(request.TargetVersion, request.OperationID)
	} else {
		operation, err = s.manager.TriggerWithID(request.TargetVersion, request.OperationID)
	}
	if err != nil {
		status := triggerErrorHTTPStatus(err)
		message := err.Error()
		if status == http.StatusInternalServerError {
			// Manager errors can contain privileged host paths and filesystem
			// details. Keep those in daemon logs, not the Fleet API response.
			log.Printf("trigger updater operation: %v", err)
			message = "host updater failed to start upgrade"
		}
		writeJSON(w, status, updaterapi.ErrorResponse{Error: message})
		return
	}
	writeJSON(w, http.StatusAccepted, updaterapi.TriggerResponse{Operation: operation})
}

func (s *Server) handleAcknowledge(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeJSON(w, http.StatusMethodNotAllowed, updaterapi.ErrorResponse{Error: "method not allowed"})
		return
	}
	defer r.Body.Close()
	var request updaterapi.AcknowledgeRequest
	if err := decodeSingleJSONValue(http.MaxBytesReader(w, r.Body, 4096), &request); err != nil {
		writeJSON(w, http.StatusBadRequest, updaterapi.ErrorResponse{Error: "invalid request body"})
		return
	}
	if request.OperationID == "" {
		// An empty ID would fall through to a 404 "no matching operation",
		// misreporting a malformed request as a missing resource.
		writeJSON(w, http.StatusBadRequest, updaterapi.ErrorResponse{Error: "operation_id is required"})
		return
	}
	operation, err := s.manager.Acknowledge(request.OperationID)
	if err != nil {
		status := acknowledgeErrorHTTPStatus(err)
		message := err.Error()
		if status == http.StatusInternalServerError {
			// See handleUpgrade: keep privileged host detail in daemon logs.
			log.Printf("acknowledge updater operation: %v", err)
			message = "host updater failed to record the acknowledgement"
		}
		writeJSON(w, status, updaterapi.ErrorResponse{Error: message})
		return
	}
	writeJSON(w, http.StatusOK, updaterapi.AcknowledgeResponse{Operation: operation})
}

func acknowledgeErrorHTTPStatus(err error) int {
	switch {
	case errors.Is(err, errAcknowledgeUnknown):
		return http.StatusNotFound
	case errors.Is(err, errAcknowledgeActive):
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}

func decodeSingleJSONValue(body io.Reader, output any) error {
	decoder := json.NewDecoder(body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(output); err != nil {
		return fmt.Errorf("decode request body: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("request body must contain exactly one JSON value")
		}
		return fmt.Errorf("decode trailing request body: %w", err)
	}
	return nil
}

func triggerErrorHTTPStatus(err error) int {
	switch {
	case errors.Is(err, errTriggerInvalid):
		return http.StatusBadRequest
	case errors.Is(err, errTriggerPrecondition):
		return http.StatusPreconditionFailed
	case errors.Is(err, errTriggerClosing):
		return http.StatusServiceUnavailable
	case errors.Is(err, errTriggerBusy):
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("encode updater response: %v", err)
	}
}
