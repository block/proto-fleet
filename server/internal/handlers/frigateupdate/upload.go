package frigateupdate

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	domain "github.com/block/proto-fleet/server/internal/domain/frigateupdate"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/infrastructure/files"
	"github.com/block/proto-fleet/server/internal/runtimejobs"
)

const (
	maxInitiateBody   = 16 << 10
	maxChunkBytes     = 64 << 20
	maxUploadSessions = 8
	// maxUploadSessionsPerUser keeps one user from taking every slot.
	maxUploadSessionsPerUser = 2
	// idleSessionReclaim is how long a session may go without a chunk before a
	// new upload that hits a cap may reclaim it. A cancelled or failed upload
	// sends no further chunks, so it stops blocking new uploads after this.
	idleSessionReclaim = 5 * time.Minute
)

// RequestAuthenticator supplies the same identity and current permissions used
// by Connect RPCs. The production implementation is the shared AuthInterceptor.
type RequestAuthenticator interface {
	AuthenticateRequest(ctx context.Context, headers http.Header) (context.Context, error)
}

type errorResponse struct {
	Error string `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("failed to encode frigate package response", "error", err)
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorResponse{Error: message})
}

// writeServiceError maps a domain error to an HTTP response. Internal errors
// are logged and reported generically.
func writeServiceError(w http.ResponseWriter, operation string, err error) {
	switch {
	case fleeterror.IsAuthenticationError(err):
		writeError(w, http.StatusUnauthorized, "authentication required")
	case fleeterror.IsForbiddenError(err):
		writeError(w, http.StatusForbidden, "permission denied")
	case fleeterror.IsInvalidArgumentError(err):
		writeError(w, http.StatusBadRequest, err.Error())
	case fleeterror.IsNotFoundError(err):
		writeError(w, http.StatusNotFound, err.Error())
	case fleeterror.IsFailedPreconditionError(err):
		writeError(w, http.StatusConflict, err.Error())
	case fleeterror.IsUnavailableError(err):
		writeError(w, http.StatusServiceUnavailable, err.Error())
	default:
		slog.Error("frigate package request failed", "operation", operation, "error", err)
		writeError(w, http.StatusInternalServerError, "request failed")
	}
}

// uploadOwner prevents another user or API key from taking over a staged upload.
type uploadOwner struct {
	organizationID int64
	userID         int64
	authMethod     session.AuthMethod
	apiKeyID       int64
}

func ownerFromContext(ctx context.Context) (uploadOwner, bool) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return uploadOwner{}, false
	}
	return uploadOwner{organizationID: info.OrganizationID, userID: info.UserID, authMethod: info.AuthMethod, apiKeyID: info.APIKeyDatabaseID}, true
}

type uploadSession struct {
	mu        sync.Mutex
	owner     uploadOwner
	filename  string
	statement []byte
	signature []byte
	// publicKey is the optional release public key supplied with the upload.
	publicKey     []byte
	expectedSize  int64
	receivedBytes int64
	tempFilePath  string
	lastActivity  time.Time
}

// UploadManager tracks in-progress chunked package uploads.
type UploadManager struct {
	mu       sync.Mutex
	sessions map[string]*uploadSession
}

// NewUploadManager creates an empty manager.
func NewUploadManager() *UploadManager {
	return &UploadManager{sessions: make(map[string]*uploadSession)}
}

// OrphanSweeper removes package blobs that no package record owns.
type OrphanSweeper interface {
	SweepOrphanPackages(ctx context.Context) (int, error)
}

// StartCleanup removes abandoned upload sessions older than ttl, and package
// blobs without a record when orphans is not nil, until ctx ends.
func (m *UploadManager) StartCleanup(ctx context.Context, ttl time.Duration, orphans OrphanSweeper) {
	if ttl <= 0 {
		ttl = time.Hour
	}
	interval := max(ttl/2, time.Minute)
	reportProgress := runtimejobs.TrackProgress(ctx, interval)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			m.sweep(ctx, ttl, orphans)
			reportProgress()
		case <-ctx.Done():
			return
		}
	}
}

// sweep runs one cleanup pass.
func (m *UploadManager) sweep(ctx context.Context, ttl time.Duration, orphans OrphanSweeper) {
	m.cleanupExpired(ttl)
	if orphans == nil {
		return
	}
	if _, err := orphans.SweepOrphanPackages(ctx); err != nil && ctx.Err() == nil {
		slog.Warn("frigate package orphan sweep failed", "error", err)
	}
}

// countOwnedLocked counts the sessions one user has open. The caller holds m.mu.
func (m *UploadManager) countOwnedLocked(owner uploadOwner) int {
	n := 0
	for _, sess := range m.sessions {
		if sess.owner.organizationID == owner.organizationID && sess.owner.userID == owner.userID {
			n++
		}
	}
	return n
}

func (m *UploadManager) cleanupExpired(ttl time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.removeIdleLocked(time.Now().Add(-ttl))
}

// removeIdleLocked removes sessions with no activity since cutoff. The caller
// holds m.mu.
func (m *UploadManager) removeIdleLocked(cutoff time.Time) {
	for id, sess := range m.sessions {
		if !sess.mu.TryLock() {
			continue // A chunk write is in progress, so it is not abandoned.
		}
		if sess.lastActivity.Before(cutoff) {
			slog.Info("cleaning up abandoned frigate package upload", "upload_id", id)
			_ = os.Remove(sess.tempFilePath)
			delete(m.sessions, id)
		}
		sess.mu.Unlock()
	}
}

// Handlers bundles the REST handlers for chunked package upload.
type Handlers struct {
	Initiate http.Handler
	Chunk    http.Handler
	Complete http.Handler
}

// NewUploadHandlers wires the upload endpoints:
//
//	POST /api/v1/frigate-packages/upload/chunked
//	PUT  /api/v1/frigate-packages/upload/chunked/{uploadId}
//	POST /api/v1/frigate-packages/upload/chunked/{uploadId}/complete
func NewUploadHandlers(mgr *UploadManager, svc *domain.Service, store *files.FrigatePackageStore, auth domain.Authorizer, authenticator RequestAuthenticator) Handlers {
	h := &uploadHandler{mgr: mgr, svc: svc, store: store, auth: auth, authenticator: authenticator}
	return Handlers{Initiate: http.HandlerFunc(h.initiate), Chunk: http.HandlerFunc(h.chunk), Complete: http.HandlerFunc(h.complete)}
}

type uploadHandler struct {
	mgr           *UploadManager
	svc           *domain.Service
	store         *files.FrigatePackageStore
	auth          domain.Authorizer
	authenticator RequestAuthenticator
}

// authenticate resolves the caller and checks the permission, writing the
// failure response itself.
func (h *uploadHandler) authenticate(w http.ResponseWriter, r *http.Request, operation string) (context.Context, uploadOwner, bool) {
	if h.authenticator == nil {
		slog.Error("frigate package upload has no authenticator")
		writeError(w, http.StatusInternalServerError, "authorization failed")
		return r.Context(), uploadOwner{}, false
	}
	ctx, err := h.authenticator.AuthenticateRequest(r.Context(), r.Header)
	if err == nil {
		err = h.auth.AuthorizeAnySite(ctx)
	}
	if err != nil {
		writeServiceError(w, operation, err)
		return r.Context(), uploadOwner{}, false
	}
	owner, ok := ownerFromContext(ctx)
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return r.Context(), uploadOwner{}, false
	}
	return ctx, owner, true
}

type initiateRequest struct {
	Filename            string `json:"filename"`
	Size                int64  `json:"size"`
	ReleaseStatementB64 string `json:"release_statement_b64"`
	ReleaseSignatureB64 string `json:"release_signature_b64"`
	// ReleasePublicKeyB64 is the optional PEM release public key. Fleet uses it
	// to check the signature only when it has no installed release key.
	ReleasePublicKeyB64 string `json:"release_public_key_b64,omitempty"`
}

type initiateResponse struct {
	UploadID string `json:"upload_id"`
	// Kind is the package kind that the release statement names: "frigate"
	// or "proto_os".
	Kind         string `json:"kind"`
	Version      string `json:"version"`
	Architecture string `json:"architecture"`
	// Warning is set when Fleet accepts the upload without verifying the
	// release signature.
	Warning string `json:"warning,omitempty"`
}

func (h *uploadHandler) initiate(w http.ResponseWriter, r *http.Request) {
	ctx, owner, ok := h.authenticate(w, r, "initiate frigate package upload")
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxInitiateBody)
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
	statement, err := base64.StdEncoding.DecodeString(req.ReleaseStatementB64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "release_statement_b64 is not valid base64")
		return
	}
	signature, err := base64.StdEncoding.DecodeString(req.ReleaseSignatureB64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "release_signature_b64 is not valid base64")
		return
	}
	publicKey, err := base64.StdEncoding.DecodeString(req.ReleasePublicKeyB64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "release_public_key_b64 is not valid base64")
		return
	}
	// The release is checked before any bytes are accepted so a bad release
	// fails immediately instead of after a long upload.
	check, err := h.svc.ValidateUploadStatement(ctx, statement, signature, publicKey, req.Filename, req.Size)
	if err != nil {
		writeServiceError(w, "validate frigate release statement", err)
		return
	}

	idBytes := make([]byte, 16)
	if _, err := rand.Read(idBytes); err != nil {
		slog.Error("failed to generate upload ID", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to initiate upload")
		return
	}
	uploadID := hex.EncodeToString(idBytes)

	h.mgr.mu.Lock()
	if len(h.mgr.sessions) >= maxUploadSessions || h.mgr.countOwnedLocked(owner) >= maxUploadSessionsPerUser {
		h.mgr.removeIdleLocked(time.Now().Add(-idleSessionReclaim))
	}
	if len(h.mgr.sessions) >= maxUploadSessions {
		h.mgr.mu.Unlock()
		writeError(w, http.StatusTooManyRequests, "too many uploads are in progress")
		return
	}
	if h.mgr.countOwnedLocked(owner) >= maxUploadSessionsPerUser {
		h.mgr.mu.Unlock()
		writeError(w, http.StatusTooManyRequests, "you already have too many uploads in progress; finish or wait for them to expire")
		return
	}
	tempPath, err := h.store.NewStagingPath(uploadID)
	if err != nil {
		h.mgr.mu.Unlock()
		slog.Error("failed to create frigate package staging file", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to initiate upload")
		return
	}
	h.mgr.sessions[uploadID] = &uploadSession{
		owner: owner, filename: req.Filename, statement: statement, signature: signature, publicKey: publicKey,
		expectedSize: req.Size, tempFilePath: tempPath, lastActivity: time.Now(),
	}
	h.mgr.mu.Unlock()

	slog.Info("frigate package upload initiated", "upload_id", uploadID, "filename", req.Filename, "size", req.Size, "signature_check", string(check.Signature))
	writeJSON(w, http.StatusOK, initiateResponse{
		UploadID: uploadID, Kind: string(check.Kind), Version: check.Statement.Version, Architecture: check.Statement.Architecture, Warning: check.Warning,
	})
}

func (h *uploadHandler) session(w http.ResponseWriter, owner uploadOwner, uploadID string) (*uploadSession, bool) {
	h.mgr.mu.Lock()
	sess, ok := h.mgr.sessions[uploadID]
	h.mgr.mu.Unlock()
	if !ok || sess.owner != owner {
		writeError(w, http.StatusNotFound, "upload session not found")
		return nil, false
	}
	return sess, true
}

func (h *uploadHandler) chunk(w http.ResponseWriter, r *http.Request) {
	_, owner, ok := h.authenticate(w, r, "upload frigate package chunk")
	if !ok {
		return
	}
	uploadID := r.PathValue("uploadId")
	if uploadID == "" {
		writeError(w, http.StatusBadRequest, "missing upload ID")
		return
	}
	sess, ok := h.session(w, owner, uploadID)
	if !ok {
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
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Content-Range total (%d) does not match expected size (%d)", total, sess.expectedSize))
		return
	}
	if start != sess.receivedBytes {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("expected chunk starting at byte %d, got %d", sess.receivedBytes, start))
		return
	}
	chunkSize := end - start + 1
	if chunkSize > maxChunkBytes {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("chunks must be at most %d bytes", maxChunkBytes))
		return
	}

	f, err := os.OpenFile(sess.tempFilePath, os.O_RDWR, 0o600)
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

type packageResponse struct {
	ID                  string `json:"id"`
	Kind                string `json:"kind"`
	Filename            string `json:"filename"`
	Version             string `json:"version"`
	Architecture        string `json:"architecture"`
	SHA256              string `json:"sha256"`
	SizeBytes           int64  `json:"size_bytes"`
	Repository          string `json:"repository"`
	ReleaseKeyID        string `json:"release_key_id"`
	MinInstalledVersion string `json:"min_installed_version"`
	IssuedAt            string `json:"issued_at"`
	UploadedAt          string `json:"uploaded_at"`
}

type completeResponse struct {
	Package packageResponse `json:"package"`
	Reused  bool            `json:"reused"`
	// Warning is set when the stored package has no release signature that
	// Fleet verified. Package.ReleaseKeyID is empty in that case.
	Warning string `json:"warning,omitempty"`
}

func (h *uploadHandler) complete(w http.ResponseWriter, r *http.Request) {
	ctx, owner, ok := h.authenticate(w, r, "complete frigate package upload")
	if !ok {
		return
	}
	uploadID := r.PathValue("uploadId")
	if uploadID == "" {
		writeError(w, http.StatusBadRequest, "missing upload ID")
		return
	}
	h.mgr.mu.Lock()
	sess, found := h.mgr.sessions[uploadID]
	if found && sess.owner == owner {
		delete(h.mgr.sessions, uploadID)
	}
	h.mgr.mu.Unlock()
	if !found || sess.owner != owner {
		writeError(w, http.StatusNotFound, "upload session not found")
		return
	}

	// The session is out of the map, so no further chunk can arrive. Waiting for
	// the lock lets an in-flight chunk finish; ImportPackage removes the staged
	// file on every path, but the early returns below must do it themselves.
	sess.mu.Lock()
	received := sess.receivedBytes
	sess.mu.Unlock()
	if received != sess.expectedSize {
		_ = os.Remove(sess.tempFilePath)
		writeError(w, http.StatusBadRequest, fmt.Sprintf("received %d bytes but expected %d", received, sess.expectedSize))
		return
	}

	result, err := h.svc.ImportPackage(ctx, domain.ImportInput{
		StagedPath: sess.tempFilePath, Filename: sess.filename, Statement: sess.statement, Signature: sess.signature,
		PublicKey: sess.publicKey,
	})
	if err != nil {
		writeServiceError(w, "import frigate package", err)
		return
	}
	p := result.Package
	slog.Info("frigate package upload completed", "upload_id", uploadID, "package_id", p.ID, "reused", result.Reused)
	writeJSON(w, http.StatusOK, completeResponse{
		Package: packageResponse{
			ID: p.ID.String(), Kind: string(p.Kind), Filename: p.Filename, Version: p.Version, Architecture: p.Architecture, SHA256: p.SHA256,
			SizeBytes: p.SizeBytes, Repository: p.Repository, ReleaseKeyID: p.ReleaseKeyID,
			MinInstalledVersion: p.MinInstalledVersion, IssuedAt: p.IssuedAt.UTC().Format(time.RFC3339),
			UploadedAt: p.UploadedAt.UTC().Format(time.RFC3339),
		},
		Reused:  result.Reused,
		Warning: result.Warning,
	})
}

// parseContentRange parses a Content-Range header of the form "bytes start-end/total".
func parseContentRange(header string) (start, end, total int64, err error) {
	if header == "" {
		return 0, 0, 0, fmt.Errorf("missing Content-Range header")
	}
	rest, ok := strings.CutPrefix(header, "bytes ")
	if !ok {
		return 0, 0, 0, fmt.Errorf("Content-Range must start with 'bytes '")
	}
	rangePart, totalStr, ok := strings.Cut(rest, "/")
	if !ok {
		return 0, 0, 0, fmt.Errorf("Content-Range missing '/' separator")
	}
	startStr, endStr, ok := strings.Cut(rangePart, "-")
	if !ok {
		return 0, 0, 0, fmt.Errorf("Content-Range missing '-' in range")
	}
	if start, err = strconv.ParseInt(startStr, 10, 64); err != nil {
		return 0, 0, 0, fmt.Errorf("invalid Content-Range start: %w", err)
	}
	if end, err = strconv.ParseInt(endStr, 10, 64); err != nil {
		return 0, 0, 0, fmt.Errorf("invalid Content-Range end: %w", err)
	}
	if total, err = strconv.ParseInt(totalStr, 10, 64); err != nil {
		return 0, 0, 0, fmt.Errorf("invalid Content-Range total: %w", err)
	}
	if start < 0 || end < start || total <= 0 || end >= total {
		return 0, 0, 0, fmt.Errorf("invalid Content-Range values: start=%d end=%d total=%d", start, end, total)
	}
	return start, end, total, nil
}
