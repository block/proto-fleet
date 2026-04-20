package firmware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/infrastructure/files"
)

type uploadSession struct {
	mu            sync.Mutex
	uploadID      string
	filename      string
	expectedSize  int64
	receivedBytes int64
	tempFilePath  string
	createdAt     time.Time
	lastActivity  time.Time
}

type initiateRequest struct {
	Filename string `json:"filename"`
	FileSize int64  `json:"file_size"`
}

type initiateResponse struct {
	UploadID string `json:"upload_id"`
}

// ChunkedUploadManager tracks in-progress chunked upload sessions.
type ChunkedUploadManager struct {
	mu       sync.Mutex
	sessions map[string]*uploadSession
}

// NewChunkedUploadManager creates a new manager for chunked upload sessions.
func NewChunkedUploadManager() *ChunkedUploadManager {
	return &ChunkedUploadManager{
		sessions: make(map[string]*uploadSession),
	}
}

// StartCleanup runs a background loop that removes abandoned upload sessions
// older than ttl. Stops when ctx is cancelled.
func (m *ChunkedUploadManager) StartCleanup(ctx context.Context, ttl time.Duration) {
	if ttl <= 0 {
		ttl = time.Hour
	}
	interval := ttl / 2
	if interval < time.Minute {
		interval = time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			m.cleanupExpired(ttl)
		case <-ctx.Done():
			return
		}
	}
}

func (m *ChunkedUploadManager) cleanupExpired(ttl time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	cutoff := time.Now().Add(-ttl)
	for id, sess := range m.sessions {
		if !sess.mu.TryLock() {
			continue // chunk write in progress, clearly not abandoned
		}
		expired := sess.lastActivity.Before(cutoff)
		if expired {
			slog.Info("cleaning up abandoned chunked upload session", "upload_id", id)
			os.Remove(sess.tempFilePath)
			delete(m.sessions, id)
		}
		sess.mu.Unlock()
	}
}

// NewInitiateHandler returns a handler for POST /api/v1/firmware/upload/chunked.
func NewInitiateHandler(
	mgr *ChunkedUploadManager,
	filesService *files.Service,
	sessionService *session.Service,
	userStore interfaces.UserStore,
) http.Handler {
	return &initiateHandler{mgr: mgr, filesService: filesService, sessionService: sessionService, userStore: userStore}
}

type initiateHandler struct {
	mgr            *ChunkedUploadManager
	filesService   *files.Service
	sessionService *session.Service
	userStore      interfaces.UserStore
}

func (h *initiateHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if _, err := authenticate(r, h.sessionService, h.userStore); err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	const maxBody = 4096
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "request body too large")
		return
	}

	var req initiateRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if err := h.filesService.ValidateFirmwareFilename(req.Filename); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if req.FileSize <= 0 {
		writeError(w, http.StatusBadRequest, "file_size must be greater than zero")
		return
	}
	if req.FileSize > h.filesService.MaxFirmwareFileSize() {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("file_size exceeds maximum allowed size (%d bytes)", h.filesService.MaxFirmwareFileSize()))
		return
	}

	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		slog.Error("failed to generate upload ID", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to initiate upload")
		return
	}
	uploadID := hex.EncodeToString(b)
	tempPath := filepath.Join(files.StagingDir(), uploadID)
	tempFile, err := os.OpenFile(tempPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		slog.Error("failed to create temp file for chunked upload", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to initiate upload")
		return
	}
	tempFile.Close()

	now := time.Now()
	h.mgr.mu.Lock()
	h.mgr.sessions[uploadID] = &uploadSession{
		uploadID:     uploadID,
		filename:     req.Filename,
		expectedSize: req.FileSize,
		tempFilePath: tempPath,
		createdAt:    now,
		lastActivity: now,
	}
	h.mgr.mu.Unlock()

	slog.Info("chunked upload initiated", "upload_id", uploadID, "filename", req.Filename, "file_size", req.FileSize)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(initiateResponse{UploadID: uploadID}); err != nil {
		slog.Error("failed to encode initiate response", "error", err)
	}
}

// NewChunkHandler returns a handler for PUT /api/v1/firmware/upload/chunked/{uploadId}.
func NewChunkHandler(
	mgr *ChunkedUploadManager,
	sessionService *session.Service,
	userStore interfaces.UserStore,
) http.Handler {
	return &chunkHandler{mgr: mgr, sessionService: sessionService, userStore: userStore}
}

type chunkHandler struct {
	mgr            *ChunkedUploadManager
	sessionService *session.Service
	userStore      interfaces.UserStore
}

func (h *chunkHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if _, err := authenticate(r, h.sessionService, h.userStore); err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	uploadID := r.PathValue("uploadId")
	if uploadID == "" {
		writeError(w, http.StatusBadRequest, "missing upload ID")
		return
	}

	h.mgr.mu.Lock()
	sess, ok := h.mgr.sessions[uploadID]
	h.mgr.mu.Unlock()

	if !ok {
		writeError(w, http.StatusNotFound, "upload session not found")
		return
	}

	sess.mu.Lock()
	defer sess.mu.Unlock()

	start, end, total, err := parseContentRange(r.Header.Get("Content-Range"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if total != sess.expectedSize {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Content-Range total (%d) does not match expected file size (%d)", total, sess.expectedSize))
		return
	}

	if start != sess.receivedBytes {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("expected chunk starting at byte %d, got %d", sess.receivedBytes, start))
		return
	}

	chunkSize := end - start + 1

	f, err := os.OpenFile(sess.tempFilePath, os.O_RDWR, 0600)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to open upload file")
		return
	}
	defer f.Close()

	written, err := io.Copy(io.NewOffsetWriter(f, start), io.LimitReader(r.Body, chunkSize))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to write chunk")
		return
	}

	if written != chunkSize {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("expected %d bytes but received %d", chunkSize, written))
		return
	}

	sess.receivedBytes += written
	sess.lastActivity = time.Now()

	w.WriteHeader(http.StatusOK)
}

// NewCompleteHandler returns a handler for POST /api/v1/firmware/upload/chunked/{uploadId}/complete.
func NewCompleteHandler(
	mgr *ChunkedUploadManager,
	filesService *files.Service,
	sessionService *session.Service,
	userStore interfaces.UserStore,
) http.Handler {
	return &completeHandler{mgr: mgr, filesService: filesService, sessionService: sessionService, userStore: userStore}
}

type completeHandler struct {
	mgr            *ChunkedUploadManager
	filesService   *files.Service
	sessionService *session.Service
	userStore      interfaces.UserStore
}

func (h *completeHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if _, err := authenticate(r, h.sessionService, h.userStore); err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	uploadID := r.PathValue("uploadId")
	if uploadID == "" {
		writeError(w, http.StatusBadRequest, "missing upload ID")
		return
	}

	h.mgr.mu.Lock()
	sess, ok := h.mgr.sessions[uploadID]
	if ok {
		delete(h.mgr.sessions, uploadID)
	}
	h.mgr.mu.Unlock()

	if !ok {
		writeError(w, http.StatusNotFound, "upload session not found")
		return
	}

	// Wait for any in-flight chunk write to finish. After removing the
	// session from the map no new chunk requests can find it, so once we
	// acquire sess.mu the receivedBytes value is final.
	sess.mu.Lock()
	receivedBytes := sess.receivedBytes
	sess.mu.Unlock()

	if receivedBytes != sess.expectedSize {
		os.Remove(sess.tempFilePath)
		writeError(w, http.StatusBadRequest, fmt.Sprintf("received %d bytes but expected %d", receivedBytes, sess.expectedSize))
		return
	}

	info, statErr := os.Stat(sess.tempFilePath)
	if statErr != nil || info.Size() != sess.expectedSize {
		os.Remove(sess.tempFilePath)
		writeError(w, http.StatusInternalServerError, "uploaded file size mismatch on disk")
		return
	}

	fileID, err := h.filesService.SaveFirmwareFileFromPath(sess.filename, sess.tempFilePath)
	if err != nil {
		os.Remove(sess.tempFilePath)
		if isClientError(err) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		slog.Error("failed to finalize chunked upload", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to finalize upload")
		return
	}

	slog.Info("chunked upload completed", "upload_id", uploadID, "firmware_file_id", fileID)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(uploadResponse{FirmwareFileID: fileID}); err != nil {
		slog.Error("failed to encode chunked upload response", "error", err)
	}
}

// parseContentRange parses a Content-Range header value of the form "bytes start-end/total".
func parseContentRange(header string) (start, end, total int64, err error) {
	if header == "" {
		return 0, 0, 0, fmt.Errorf("missing Content-Range header")
	}

	if !strings.HasPrefix(header, "bytes ") {
		return 0, 0, 0, fmt.Errorf("Content-Range must start with 'bytes '")
	}

	rest := strings.TrimPrefix(header, "bytes ")
	slashIdx := strings.Index(rest, "/")
	if slashIdx < 0 {
		return 0, 0, 0, fmt.Errorf("Content-Range missing '/' separator")
	}

	rangePart := rest[:slashIdx]
	totalStr := rest[slashIdx+1:]

	dashIdx := strings.Index(rangePart, "-")
	if dashIdx < 0 {
		return 0, 0, 0, fmt.Errorf("Content-Range missing '-' in range")
	}

	start, err = strconv.ParseInt(rangePart[:dashIdx], 10, 64)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("invalid Content-Range start: %w", err)
	}

	end, err = strconv.ParseInt(rangePart[dashIdx+1:], 10, 64)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("invalid Content-Range end: %w", err)
	}

	total, err = strconv.ParseInt(totalStr, 10, 64)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("invalid Content-Range total: %w", err)
	}

	if start < 0 || end < start || total <= 0 || end >= total {
		return 0, 0, 0, fmt.Errorf("invalid Content-Range values: start=%d end=%d total=%d", start, end, total)
	}

	return start, end, total, nil
}
