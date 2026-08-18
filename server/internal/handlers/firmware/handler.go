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
	activityDomain "github.com/block/proto-fleet/server/internal/domain/activity"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/infrastructure/files"
)

type uploadResponse struct {
	FirmwareFileID string `json:"firmware_file_id"`
	Reused         bool   `json:"reused,omitempty"`
}

type extractedMultipartUpload struct {
	filename string
	staged   *files.StagedFirmwareUpload
	metadata files.FirmwareMetadata
	force    bool
}

type checkRequest struct {
	SHA256 string `json:"sha256"`
	files.FirmwareMetadata
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
// The request body is capped at the files service's firmware size limit to reject
// oversized uploads early.
func NewUploadHandler(
	filesService *files.Service,
	sessionService *session.Service,
	userStore interfaces.UserStore,
	activitySvc *activityDomain.Service,
	permissionResolver effectivePermissionResolver,
) http.Handler {
	return &uploadHandler{
		filesService:       filesService,
		sessionService:     sessionService,
		userStore:          userStore,
		activitySvc:        activitySvc,
		permissionResolver: permissionResolver,
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

	if err := files.ValidateFirmwareUploadMetadata(req.FirmwareMetadata); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	fileID, ok := h.filesService.FindFirmwareFileByChecksum(strings.ToLower(req.SHA256), req.FirmwareMetadata)

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
	filesService       *files.Service
	sessionService     *session.Service
	userStore          interfaces.UserStore
	activitySvc        *activityDomain.Service
	permissionResolver effectivePermissionResolver
}

func (h *uploadHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	ctx, ok := requireMutationPermission(
		w,
		r,
		h.sessionService,
		h.userStore,
		h.permissionResolver,
		"upload",
	)
	if !ok {
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
	r.Body = http.MaxBytesReader(w, r.Body, h.filesService.MaxFirmwareFileSize()+multipartOverhead)

	upload, err := extractMultipartFile(r, h.filesService)
	if err != nil {
		if isClientError(err) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		slog.Error("failed to stage firmware upload", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to stage firmware upload")
		return
	}
	defer upload.staged.Discard()

	saveResult, err := h.filesService.SaveFirmwareUploadFromPath(
		upload.filename,
		upload.staged.Path,
		upload.metadata,
		upload.force,
		upload.staged.Checksum,
	)
	if err != nil {
		if isClientError(err) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		slog.Error("failed to save firmware file", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to save firmware file")
		return
	}

	slog.Info("firmware file uploaded successfully", "file_id", saveResult.FirmwareFileID, "filename", upload.filename, "reused", saveResult.Reused)
	logFirmwareUploadActivity(ctx, h.activitySvc, upload.filename, saveResult)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(uploadResponse{FirmwareFileID: saveResult.FirmwareFileID, Reused: saveResult.Reused}); err != nil {
		slog.Error("failed to encode upload response", "error", err)
	}
}

// extractMultipartFile stages the file while reading the complete multipart
// body, so metadata fields are accepted regardless of whether they appear
// before or after the file part.
func extractMultipartFile(r *http.Request, filesService *files.Service) (extractedMultipartUpload, error) {
	var upload extractedMultipartUpload
	contentType := r.Header.Get("Content-Type")
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil || !strings.HasPrefix(mediaType, "multipart/") {
		return extractedMultipartUpload{}, fleeterror.NewInvalidArgumentError("expected multipart/form-data content type")
	}

	boundary := params["boundary"]
	if boundary == "" {
		return extractedMultipartUpload{}, fleeterror.NewInvalidArgumentError("missing multipart boundary")
	}

	completed := false
	defer func() {
		if !completed {
			upload.staged.Discard()
		}
	}()

	mr := multipart.NewReader(r.Body, boundary)
	metadataFields := map[string]*string{
		"target_manufacturer": &upload.metadata.TargetManufacturer,
		"target_model":        &upload.metadata.TargetModel,
		"firmware_version":    &upload.metadata.FirmwareVersion,
	}
	metadataFieldsRead := make(map[string]bool, len(metadataFields))
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			var maxBytesErr *http.MaxBytesError
			if errors.As(err, &maxBytesErr) {
				return extractedMultipartUpload{}, fmt.Errorf("failed to read multipart form: %w", err)
			}
			return extractedMultipartUpload{}, fleeterror.NewInvalidArgumentErrorf("failed to read multipart form: %v", err)
		}

		name := part.FormName()
		switch {
		case name == "file":
			if upload.staged != nil {
				_ = part.Close()
				return extractedMultipartUpload{}, fleeterror.NewInvalidArgumentError("multiple 'file' fields are not supported")
			}
			upload.filename = part.FileName()
			if validationErr := filesService.ValidateFirmwareFilename(upload.filename); validationErr != nil {
				_ = part.Close()
				return extractedMultipartUpload{}, validationErr
			}
			if len(metadataFieldsRead) == len(metadataFields) {
				if validationErr := files.ValidateFirmwareUploadMetadata(upload.metadata); validationErr != nil {
					_ = part.Close()
					return extractedMultipartUpload{}, validationErr
				}
			}
			staged, stageErr := filesService.StageFirmwareUpload(part)
			_ = part.Close()
			if stageErr != nil {
				return extractedMultipartUpload{}, stageErr
			}
			upload.staged = staged
		case metadataFields[name] != nil:
			value, readErr := readPartValue(part, 1024, name)
			if readErr != nil {
				return extractedMultipartUpload{}, readErr
			}
			*metadataFields[name] = value
			metadataFieldsRead[name] = true
		case name == "force":
			value, readErr := readPartValue(part, 16, name)
			if readErr != nil {
				return extractedMultipartUpload{}, readErr
			}
			value = strings.TrimSpace(value)
			upload.force = strings.EqualFold(value, "true") || value == "1"
		default:
			part.Close()
		}
	}
	if upload.staged == nil {
		return extractedMultipartUpload{}, fleeterror.NewInvalidArgumentError("missing 'file' field in multipart form")
	}
	completed = true
	return upload, nil
}

// readPartValue reads a small text form field up to limit bytes and closes the part.
func readPartValue(part *multipart.Part, limit int64, name string) (string, error) {
	value, err := io.ReadAll(io.LimitReader(part, limit))
	part.Close()
	if err != nil {
		return "", fleeterror.NewInvalidArgumentErrorf("failed to read %s: %v", name, err)
	}
	return string(value), nil
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

type firmwareReferenceChecker interface {
	FirmwareArtifactReferenced(ctx context.Context, firmwareFileID string) (bool, error)
	AnyFirmwareArtifactReferenced(ctx context.Context) (bool, error)
}

var errFirmwareReferenceValidation = errors.New("firmware reference validation failed")

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

// NewUpdateMetadataHandler returns an http.Handler that updates a stored
// firmware file's deployment metadata.
func NewUpdateMetadataHandler(
	filesService *files.Service,
	sessionService *session.Service,
	userStore interfaces.UserStore,
	activitySvc *activityDomain.Service,
	permissionResolver effectivePermissionResolver,
	referenceChecker firmwareReferenceChecker,
) http.Handler {
	return &updateMetadataHandler{
		filesService:       filesService,
		sessionService:     sessionService,
		userStore:          userStore,
		activitySvc:        activitySvc,
		permissionResolver: permissionResolver,
		referenceChecker:   referenceChecker,
	}
}

type updateMetadataHandler struct {
	filesService       *files.Service
	sessionService     *session.Service
	userStore          interfaces.UserStore
	activitySvc        *activityDomain.Service
	permissionResolver effectivePermissionResolver
	referenceChecker   firmwareReferenceChecker
}

func (h *updateMetadataHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx, ok := requireMutationPermission(
		w,
		r,
		h.sessionService,
		h.userStore,
		h.permissionResolver,
		"update metadata",
	)
	if !ok {
		return
	}

	fileID := r.PathValue("fileId")
	if fileID == "" {
		writeError(w, http.StatusBadRequest, "file ID is required")
		return
	}

	const maxBodyBytes = 4096
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var metadata files.FirmwareMetadata
	if err := decoder.Decode(&metadata); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	result, err := h.filesService.UpdateFirmwareMetadataGuarded(fileID, metadata, func() error {
		return validateFirmwareArtifactMutation(ctx, h.referenceChecker, fileID)
	})
	if err != nil {
		switch {
		case fleeterror.IsNotFoundError(err):
			writeError(w, http.StatusNotFound, err.Error())
		case fleeterror.IsInvalidArgumentError(err):
			writeError(w, http.StatusBadRequest, err.Error())
		case fleeterror.IsFailedPreconditionError(err):
			writeError(w, http.StatusConflict, "firmware file is referenced by an immutable release set")
		case errors.Is(err, errFirmwareReferenceValidation):
			writeError(w, http.StatusInternalServerError, "failed to validate firmware references")
		default:
			slog.Error("failed to update firmware metadata", "file_id", fileID, "error", err)
			writeError(w, http.StatusInternalServerError, "failed to update firmware metadata")
		}
		return
	}

	logFirmwareMetadataUpdatedActivity(ctx, h.activitySvc, fileID, result.Previous, result.Current)

	w.WriteHeader(http.StatusNoContent)
}

// NewDeleteFileHandler returns an http.Handler that deletes a single firmware file by ID.
func NewDeleteFileHandler(
	filesService *files.Service,
	sessionService *session.Service,
	userStore interfaces.UserStore,
	permissionResolver effectivePermissionResolver,
	referenceChecker firmwareReferenceChecker,
) http.Handler {
	return &deleteFileHandler{
		filesService:       filesService,
		sessionService:     sessionService,
		userStore:          userStore,
		permissionResolver: permissionResolver,
		referenceChecker:   referenceChecker,
	}
}

type deleteFileHandler struct {
	filesService       *files.Service
	sessionService     *session.Service
	userStore          interfaces.UserStore
	permissionResolver effectivePermissionResolver
	referenceChecker   firmwareReferenceChecker
}

func (h *deleteFileHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx, ok := requireMutationPermission(
		w,
		r,
		h.sessionService,
		h.userStore,
		h.permissionResolver,
		"delete file",
	)
	if !ok {
		return
	}

	fileID := r.PathValue("fileId")
	if fileID == "" {
		writeError(w, http.StatusBadRequest, "file ID is required")
		return
	}
	if err := h.filesService.DeleteFirmwareFileGuarded(fileID, func() error {
		return validateFirmwareArtifactMutation(ctx, h.referenceChecker, fileID)
	}); err != nil {
		if fleeterror.IsNotFoundError(err) {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		if fleeterror.IsInvalidArgumentError(err) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if fleeterror.IsFailedPreconditionError(err) {
			writeError(w, http.StatusConflict, "firmware file is referenced by an immutable release set")
			return
		}
		if errors.Is(err, errFirmwareReferenceValidation) {
			writeError(w, http.StatusInternalServerError, "failed to validate firmware references")
			return
		}
		slog.Error("failed to delete firmware file", "file_id", fileID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to delete firmware file")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// NewDeleteAllFilesHandler returns an http.Handler that deletes all firmware files.
func NewDeleteAllFilesHandler(
	filesService *files.Service,
	sessionService *session.Service,
	userStore interfaces.UserStore,
	permissionResolver effectivePermissionResolver,
	referenceChecker firmwareReferenceChecker,
) http.Handler {
	return &deleteAllFilesHandler{
		filesService:       filesService,
		sessionService:     sessionService,
		userStore:          userStore,
		permissionResolver: permissionResolver,
		referenceChecker:   referenceChecker,
	}
}

type deleteAllFilesHandler struct {
	filesService       *files.Service
	sessionService     *session.Service
	userStore          interfaces.UserStore
	permissionResolver effectivePermissionResolver
	referenceChecker   firmwareReferenceChecker
}

func (h *deleteAllFilesHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx, ok := requireMutationPermission(
		w,
		r,
		h.sessionService,
		h.userStore,
		h.permissionResolver,
		"delete all files",
	)
	if !ok {
		return
	}
	deleted, err := h.filesService.DeleteAllFirmwareFilesGuarded(func() error {
		return validateAnyFirmwareArtifactMutation(ctx, h.referenceChecker)
	})
	if err != nil {
		if fleeterror.IsFailedPreconditionError(err) {
			writeError(w, http.StatusConflict, "one or more firmware files are referenced by immutable release sets")
			return
		}
		if errors.Is(err, errFirmwareReferenceValidation) {
			writeError(w, http.StatusInternalServerError, "failed to validate firmware references")
			return
		}
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

func validateFirmwareArtifactMutation(
	ctx context.Context,
	checker firmwareReferenceChecker,
	fileID string,
) error {
	if checker == nil {
		return fmt.Errorf("%w: unavailable", errFirmwareReferenceValidation)
	}
	referenced, err := checker.FirmwareArtifactReferenced(ctx, fileID)
	if err != nil {
		return fmt.Errorf("%w: %v", errFirmwareReferenceValidation, err)
	}
	if referenced {
		return fleeterror.NewFailedPreconditionError("firmware file is referenced by an immutable release set")
	}
	return nil
}

func validateAnyFirmwareArtifactMutation(ctx context.Context, checker firmwareReferenceChecker) error {
	if checker == nil {
		return fmt.Errorf("%w: unavailable", errFirmwareReferenceValidation)
	}
	referenced, err := checker.AnyFirmwareArtifactReferenced(ctx)
	if err != nil {
		return fmt.Errorf("%w: %v", errFirmwareReferenceValidation, err)
	}
	if referenced {
		return fleeterror.NewFailedPreconditionError("one or more firmware files are referenced by immutable release sets")
	}
	return nil
}

func writeError(w http.ResponseWriter, statusCode int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(errorResponse{Error: message}); err != nil {
		slog.Error("failed to encode error response", "error", err)
	}
}
