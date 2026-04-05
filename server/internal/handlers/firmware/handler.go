package firmware

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"

	"connectrpc.com/authn"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/infrastructure/files"
)

type uploadResponse struct {
	FirmwareFileID string `json:"firmware_file_id"`
}

type checkRequest struct {
	SHA256 string `json:"sha256"`
}

type checkResponse struct {
	Exists         bool   `json:"exists"`
	FirmwareFileID string `json:"firmware_file_id,omitempty"`
}

type errorResponse struct {
	Error string `json:"error"`
}

type configResponse struct {
	AllowedExtensions []string `json:"allowed_extensions"`
	MaxFileSizeBytes  int64    `json:"max_file_size_bytes"`
	ChunkSizeBytes    int64    `json:"chunk_size_bytes"`
}

// NewConfigHandler returns an http.Handler that serves firmware upload configuration.
// Clients use this to get allowed extensions, max file size, and chunked upload settings,
// keeping validation rules in sync with the server.
func NewConfigHandler(filesService *files.Service, sessionService *session.Service, userStore interfaces.UserStore, cfg files.Config) http.Handler {
	return &configHandler{
		filesService:   filesService,
		sessionService: sessionService,
		userStore:      userStore,
		cfg:            cfg,
	}
}

type configHandler struct {
	filesService   *files.Service
	sessionService *session.Service
	userStore      interfaces.UserStore
	cfg            files.Config
}

func (h *configHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if _, err := authenticate(r, h.sessionService, h.userStore); err != nil {
		slog.Warn("firmware config authentication failed", "error", err)
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	chunkSize := h.cfg.ChunkSizeBytes
	if chunkSize <= 0 {
		chunkSize = 32 * 1024 * 1024
	}

	resp := configResponse{
		AllowedExtensions: files.AllowedFirmwareExtensions(),
		MaxFileSizeBytes:  h.filesService.MaxFirmwareFileSize(),
		ChunkSizeBytes:    chunkSize,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		slog.Error("failed to encode config response", "error", err)
	}
}

// NewUploadHandler returns an http.Handler that accepts multipart firmware file uploads.
// The handler validates the file, streams it to disk, and returns a firmware_file_id.
// The request body is capped at maxUploadBytes to reject oversized uploads early.
func NewUploadHandler(filesService *files.Service, sessionService *session.Service, userStore interfaces.UserStore, maxUploadBytes int64) http.Handler {
	return &uploadHandler{
		filesService:   filesService,
		sessionService: sessionService,
		userStore:      userStore,
		maxUploadBytes: maxUploadBytes,
	}
}

// NewCheckHandler returns an http.Handler for the pre-upload checksum check endpoint.
// Clients send a SHA-256 hex digest; the server returns whether a file with that
// checksum already exists, allowing the client to skip a redundant upload.
func NewCheckHandler(filesService *files.Service, sessionService *session.Service, userStore interfaces.UserStore) http.Handler {
	return &checkHandler{
		filesService:   filesService,
		sessionService: sessionService,
		userStore:      userStore,
	}
}

type checkHandler struct {
	filesService   *files.Service
	sessionService *session.Service
	userStore      interfaces.UserStore
}

func (h *checkHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if _, err := authenticate(r, h.sessionService, h.userStore); err != nil {
		slog.Warn("firmware check authentication failed", "error", err)
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	const maxCheckBodyBytes = 1024
	r.Body = http.MaxBytesReader(w, r.Body, maxCheckBodyBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "request body too large")
		return
	}

	var req checkRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if _, err := hex.DecodeString(req.SHA256); err != nil || len(req.SHA256) != 64 {
		writeError(w, http.StatusBadRequest, "sha256 must be a 64-character hex string")
		return
	}

	fileID, ok := h.filesService.FindFirmwareFileByChecksum(strings.ToLower(req.SHA256))

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if ok {
		if err := json.NewEncoder(w).Encode(checkResponse{Exists: true, FirmwareFileID: fileID}); err != nil {
			slog.Error("failed to encode check response", "error", err)
		}
	} else {
		if err := json.NewEncoder(w).Encode(checkResponse{Exists: false}); err != nil {
			slog.Error("failed to encode check response", "error", err)
		}
	}
}

type uploadHandler struct {
	filesService   *files.Service
	sessionService *session.Service
	userStore      interfaces.UserStore
	maxUploadBytes int64
}

func (h *uploadHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	ctx, err := authenticate(r, h.sessionService, h.userStore)
	if err != nil {
		slog.Warn("firmware upload authentication failed", "error", err)
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	info, err := session.GetInfo(ctx)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	slog.Info("firmware upload request", "user_id", info.UserID, "org_id", info.OrganizationID)

	// Pad the body limit to account for multipart boundaries and part headers.
	const multipartOverhead int64 = 1 * 1024 * 1024 // 1 MB
	r.Body = http.MaxBytesReader(w, r.Body, h.maxUploadBytes+multipartOverhead)

	filename, fileReader, err := extractMultipartFile(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	defer fileReader.Close()

	if err := h.filesService.ValidateFirmwareFilename(filename); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	fileID, err := h.filesService.SaveFirmwareFile(filename, fileReader)
	if err != nil {
		if isClientError(err) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		slog.Error("failed to save firmware file", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to save firmware file")
		return
	}

	slog.Info("firmware file uploaded successfully", "file_id", fileID, "filename", filename)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(uploadResponse{FirmwareFileID: fileID}); err != nil {
		slog.Error("failed to encode upload response", "error", err)
	}
}

// extractMultipartFile streams the multipart body to find the "file" part
// without buffering the entire body in memory or spilling to temp files.
func extractMultipartFile(r *http.Request) (filename string, reader io.ReadCloser, err error) {
	contentType := r.Header.Get("Content-Type")
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil || !strings.HasPrefix(mediaType, "multipart/") {
		return "", nil, fmt.Errorf("expected multipart/form-data content type")
	}

	boundary := params["boundary"]
	if boundary == "" {
		return "", nil, fmt.Errorf("missing multipart boundary")
	}

	mr := multipart.NewReader(r.Body, boundary)
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			return "", nil, fmt.Errorf("missing 'file' field in multipart form")
		}
		if err != nil {
			return "", nil, fmt.Errorf("failed to read multipart form: %w", err)
		}

		if part.FormName() == "file" {
			return part.FileName(), part, nil
		}
		part.Close()
	}
}

// authenticate extracts and validates the session cookie from the HTTP request,
// reusing the same session/cookie logic as the Connect-RPC AuthInterceptor.
func authenticate(r *http.Request, sessionService *session.Service, userStore interfaces.UserStore) (context.Context, error) {
	cookie, err := r.Cookie(sessionService.CookieName())
	if err != nil || cookie.Value == "" {
		return r.Context(), fleeterror.NewUnauthenticatedError("session cookie required")
	}

	sess, err := sessionService.Validate(r.Context(), cookie.Value)
	if err != nil {
		return r.Context(), err
	}

	user, err := userStore.GetUserByID(r.Context(), sess.UserID)
	if err != nil {
		return r.Context(), fleeterror.NewUnauthenticatedErrorf("user with id %d not found", sess.UserID)
	}

	info := &session.Info{
		SessionID:      sess.SessionID,
		UserID:         sess.UserID,
		OrganizationID: sess.OrganizationID,
		ExternalUserID: user.UserID,
		Username:       user.Username,
	}

	return authn.SetInfo(r.Context(), info), nil
}

// isClientError returns true for errors caused by bad client input,
// including fleeterror.InvalidArgument and http.MaxBytesError (body too large).
func isClientError(err error) bool {
	if fleeterror.IsInvalidArgumentError(err) {
		return true
	}
	var maxBytesErr *http.MaxBytesError
	if errors.As(err, &maxBytesErr) {
		return true
	}
	if strings.Contains(err.Error(), "http: request body too large") {
		return true
	}
	return false
}

type listFilesResponse struct {
	Files []files.FirmwareFileInfo `json:"files"`
}

type deleteAllFilesResponse struct {
	DeletedCount int    `json:"deleted_count"`
	Error        string `json:"error,omitempty"`
}

// NewListFilesHandler returns an http.Handler that lists all uploaded firmware files.
func NewListFilesHandler(filesService *files.Service, sessionService *session.Service, userStore interfaces.UserStore) http.Handler {
	return &listFilesHandler{
		filesService:   filesService,
		sessionService: sessionService,
		userStore:      userStore,
	}
}

type listFilesHandler struct {
	filesService   *files.Service
	sessionService *session.Service
	userStore      interfaces.UserStore
}

func (h *listFilesHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if _, err := authenticate(r, h.sessionService, h.userStore); err != nil {
		slog.Warn("firmware list authentication failed", "error", err)
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	fileList, err := h.filesService.ListFirmwareFiles()
	if err != nil {
		slog.Error("failed to list firmware files", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list firmware files")
		return
	}

	if fileList == nil {
		fileList = []files.FirmwareFileInfo{}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(listFilesResponse{Files: fileList}); err != nil {
		slog.Error("failed to encode list files response", "error", err)
	}
}

// NewDeleteFileHandler returns an http.Handler that deletes a single firmware file by ID.
func NewDeleteFileHandler(filesService *files.Service, sessionService *session.Service, userStore interfaces.UserStore) http.Handler {
	return &deleteFileHandler{
		filesService:   filesService,
		sessionService: sessionService,
		userStore:      userStore,
	}
}

type deleteFileHandler struct {
	filesService   *files.Service
	sessionService *session.Service
	userStore      interfaces.UserStore
}

func (h *deleteFileHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if _, err := authenticate(r, h.sessionService, h.userStore); err != nil {
		slog.Warn("firmware delete authentication failed", "error", err)
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	fileID := r.PathValue("fileId")
	if fileID == "" {
		writeError(w, http.StatusBadRequest, "file ID is required")
		return
	}

	if err := h.filesService.DeleteFirmwareFile(fileID); err != nil {
		if fleeterror.IsNotFoundError(err) {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		if fleeterror.IsInvalidArgumentError(err) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		slog.Error("failed to delete firmware file", "file_id", fileID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to delete firmware file")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// NewDeleteAllFilesHandler returns an http.Handler that deletes all firmware files.
func NewDeleteAllFilesHandler(filesService *files.Service, sessionService *session.Service, userStore interfaces.UserStore) http.Handler {
	return &deleteAllFilesHandler{
		filesService:   filesService,
		sessionService: sessionService,
		userStore:      userStore,
	}
}

type deleteAllFilesHandler struct {
	filesService   *files.Service
	sessionService *session.Service
	userStore      interfaces.UserStore
}

func (h *deleteAllFilesHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if _, err := authenticate(r, h.sessionService, h.userStore); err != nil {
		slog.Warn("firmware delete-all authentication failed", "error", err)
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	deleted, err := h.filesService.DeleteAllFirmwareFiles()
	if err != nil {
		slog.Error("failed to delete all firmware files", "error", err, "deleted_before_error", deleted)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		if encErr := json.NewEncoder(w).Encode(deleteAllFilesResponse{
			DeletedCount: deleted,
			Error:        "failed to delete all firmware files",
		}); encErr != nil {
			slog.Error("failed to encode delete-all error response", "error", encErr)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(deleteAllFilesResponse{DeletedCount: deleted}); err != nil {
		slog.Error("failed to encode delete-all response", "error", err)
	}
}

func writeError(w http.ResponseWriter, statusCode int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(errorResponse{Error: message}); err != nil {
		slog.Error("failed to encode error response", "error", err)
	}
}
