package firmware

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	activityDomain "github.com/block/proto-fleet/server/internal/domain/activity"
	activityModels "github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/session"
	sessionMocks "github.com/block/proto-fleet/server/internal/domain/session/mocks"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	storeMocks "github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
	"github.com/block/proto-fleet/server/internal/infrastructure/files"
)

type testEnv struct {
	ctrl               *gomock.Controller
	sessionStoreMock   *sessionMocks.MockStore
	userStoreMock      *storeMocks.MockUserStore
	fileSvc            *files.Service
	sessionSvc         *session.Service
	sessionID          string
	permissionResolver effectivePermissionResolver
}

type staticPermissionResolver struct {
	permissions []string
	err         error
}

func assertJSONErrorResponse(t *testing.T, rr *httptest.ResponseRecorder, status int, message string) {
	t.Helper()
	require.Equal(t, status, rr.Code)
	assert.Equal(t, "application/json", rr.Header().Get("Content-Type"))

	var response errorResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &response))
	assert.Equal(t, message, response.Error)
}

func (r staticPermissionResolver) LoadEffective(
	_ context.Context,
	_,
	_ int64,
) (*authz.EffectivePermissions, error) {
	if r.err != nil {
		return nil, r.err
	}
	return authz.NewEffectivePermissions([]authz.Assignment{{
		AssignmentID: 1,
		ScopeType:    authz.ScopeOrg,
		Permissions:  r.permissions,
	}}), nil
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	tmp := t.TempDir()
	t.Chdir(tmp)

	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	fileSvc, err := files.NewService(files.Config{})
	require.NoError(t, err)

	sessionStore := sessionMocks.NewMockStore(ctrl)
	cfg := session.Config{
		CookieName: "fleet_session",
		Duration:   24 * time.Hour,
	}
	sessionSvc := session.NewService(cfg, sessionStore)

	userStore := storeMocks.NewMockUserStore(ctrl)

	return &testEnv{
		ctrl:             ctrl,
		sessionStoreMock: sessionStore,
		userStoreMock:    userStore,
		fileSvc:          fileSvc,
		sessionSvc:       sessionSvc,
		sessionID:        "test-session-id",
		permissionResolver: staticPermissionResolver{
			permissions: []string{authz.PermMinerFirmwareUpdate},
		},
	}
}

// expectAuth sets up expectations for a successful authentication flow.
func (e *testEnv) expectAuth() {
	testSession := &session.Session{
		SessionID:      e.sessionID,
		UserID:         1,
		OrganizationID: 1,
		ExpiresAt:      time.Now().Add(24 * time.Hour),
	}
	e.sessionStoreMock.EXPECT().
		GetSessionByID(gomock.Any(), e.sessionID).
		Return(testSession, nil)
	e.sessionStoreMock.EXPECT().
		UpdateSessionActivity(gomock.Any(), e.sessionID, gomock.Any(), gomock.Any()).
		Return(nil)
	e.userStoreMock.EXPECT().
		GetUserByID(gomock.Any(), int64(1)).
		Return(interfaces.User{ID: 1, UserID: "ext-user-id", Username: "testuser"}, nil)
}

func TestAuthenticate_PopulatesSessionInfo(t *testing.T) {
	env := newTestEnv(t)

	testSession := &session.Session{
		SessionID:      env.sessionID,
		UserID:         1,
		OrganizationID: 1,
		ExpiresAt:      time.Now().Add(24 * time.Hour),
	}
	env.sessionStoreMock.EXPECT().
		GetSessionByID(gomock.Any(), env.sessionID).
		Return(testSession, nil)
	env.sessionStoreMock.EXPECT().
		UpdateSessionActivity(gomock.Any(), env.sessionID, gomock.Any(), gomock.Any()).
		Return(nil)
	env.userStoreMock.EXPECT().
		GetUserByID(gomock.Any(), int64(1)).
		Return(interfaces.User{ID: 1, UserID: "ext-user-123", Username: "alice@fleet.io"}, nil)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	cookie := env.sessionSvc.CreateCookie(env.sessionID)
	req.AddCookie(cookie)

	ctx, err := authenticate(req, env.sessionSvc, env.userStoreMock)
	require.NoError(t, err)

	info, err := session.GetInfo(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(1), info.UserID)
	assert.Equal(t, int64(1), info.OrganizationID)
	assert.Equal(t, "ext-user-123", info.ExternalUserID)
	assert.Equal(t, "alice@fleet.io", info.Username)
}

func (e *testEnv) uploadHandler() *uploadHandler {
	return &uploadHandler{
		filesService:       e.fileSvc,
		sessionService:     e.sessionSvc,
		userStore:          e.userStoreMock,
		permissionResolver: e.permissionResolver,
	}
}

func (e *testEnv) checkHandler() *checkHandler {
	return &checkHandler{
		filesService:   e.fileSvc,
		sessionService: e.sessionSvc,
		userStore:      e.userStoreMock,
	}
}

func (e *testEnv) updateMetadataHandler() *updateMetadataHandler {
	return &updateMetadataHandler{
		filesService:       e.fileSvc,
		sessionService:     e.sessionSvc,
		userStore:          e.userStoreMock,
		permissionResolver: e.permissionResolver,
	}
}

func (e *testEnv) configHandler() *configHandler {
	return &configHandler{
		filesService:   e.fileSvc,
		sessionService: e.sessionSvc,
		userStore:      e.userStoreMock,
		cfg:            files.Config{ChunkSizeBytes: 32 * 1024 * 1024},
	}
}

func defaultFirmwareFields() map[string]string {
	return map[string]string{
		"target_manufacturer": "Proto",
		"target_model":        "Rig",
		"firmware_version":    "v2.0.0",
	}
}

// buildMultipartRequest assembles a firmware upload body from fields plus the
// file part. fileFirst puts the file ahead of the metadata fields, which the
// handler must accept just the same.
func buildMultipartRequest(
	t *testing.T,
	filename string,
	content []byte,
	cookie *http.Cookie,
	fields map[string]string,
	fileFirst bool,
) *http.Request {
	t.Helper()

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	writeFile := func() {
		part, err := writer.CreateFormFile("file", filename)
		require.NoError(t, err)
		_, err = part.Write(content)
		require.NoError(t, err)
	}
	writeFields := func() {
		for name, value := range fields {
			require.NoError(t, writer.WriteField(name, value))
		}
	}

	if fileFirst {
		writeFile()
		writeFields()
	} else {
		writeFields()
		writeFile()
	}
	require.NoError(t, writer.Close())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/firmware/upload", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if cookie != nil {
		req.AddCookie(cookie)
	}
	return req
}

func createMultipartRequest(t *testing.T, filename string, content []byte, cookie *http.Cookie) *http.Request {
	t.Helper()
	return buildMultipartRequest(t, filename, content, cookie, defaultFirmwareFields(), false)
}

func createFileFirstMultipartRequest(t *testing.T, filename string, content []byte, cookie *http.Cookie) *http.Request {
	t.Helper()
	return buildMultipartRequest(t, filename, content, cookie, defaultFirmwareFields(), true)
}

func createMultipartRequestWithFields(
	t *testing.T,
	filename string,
	content []byte,
	cookie *http.Cookie,
	fields map[string]string,
) *http.Request {
	t.Helper()
	return buildMultipartRequest(t, filename, content, cookie, fields, false)
}

func testFirmwareMetadata() files.FirmwareMetadata {
	return files.FirmwareMetadata{TargetManufacturer: "Proto", TargetModel: "Rig", FirmwareVersion: "v2.0.0"}
}

func firmwareCheckBody(checksum string) string {
	return fmt.Sprintf(`{"sha256":%q,"target_manufacturer":"Proto","target_model":"Rig","firmware_version":"v2.0.0"}`, checksum)
}

func createCheckRequest(t *testing.T, body string, cookie *http.Cookie) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/firmware/check", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	return req
}

func validSessionCookie(value string) *http.Cookie {
	return &http.Cookie{Name: "fleet_session", Value: value}
}

func sha256Hex(content string) string {
	sum := sha256.Sum256([]byte(content))
	buf := make([]byte, 64)
	hex.Encode(buf, sum[:])
	return string(buf)
}

// --- Upload handler tests ---

func TestUploadHandler_RejectsNonPostMethod(t *testing.T) {
	env := newTestEnv(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/firmware/upload", nil)
	rr := httptest.NewRecorder()

	env.uploadHandler().ServeHTTP(rr, req)

	assert.Equal(t, http.StatusMethodNotAllowed, rr.Code)
	assert.Contains(t, rr.Body.String(), "method not allowed")
}

func TestUploadHandler_RejectsNoCookie(t *testing.T) {
	env := newTestEnv(t)
	req := createMultipartRequest(t, "firmware.swu", []byte("data"), nil)
	rr := httptest.NewRecorder()

	env.uploadHandler().ServeHTTP(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
}

func TestUploadHandler_RejectsEmptyCookieValue(t *testing.T) {
	env := newTestEnv(t)
	req := createMultipartRequest(t, "firmware.swu", []byte("data"), &http.Cookie{Name: "fleet_session", Value: ""})
	rr := httptest.NewRecorder()

	env.uploadHandler().ServeHTTP(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
}

func TestUploadHandler_RejectsInvalidSession(t *testing.T) {
	env := newTestEnv(t)
	env.sessionStoreMock.EXPECT().
		GetSessionByID(gomock.Any(), "bad-session-id").
		Return(nil, fmt.Errorf("not found"))
	req := createMultipartRequest(t, "firmware.swu", []byte("data"), validSessionCookie("bad-session-id"))
	rr := httptest.NewRecorder()

	env.uploadHandler().ServeHTTP(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
}

func TestUploadHandler_RejectsInvalidExtension(t *testing.T) {
	env := newTestEnv(t)
	env.expectAuth()
	req := createMultipartRequest(t, "firmware.bin", []byte("data"), validSessionCookie(env.sessionID))
	rr := httptest.NewRecorder()

	env.uploadHandler().ServeHTTP(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "unsupported firmware file type")
}

func TestUploadHandler_RejectsMissingTargetMetadata(t *testing.T) {
	tests := []struct {
		name      string
		fields    map[string]string
		wantError string
	}{
		{name: "all metadata", wantError: "target_manufacturer"},
		{name: "manufacturer", fields: map[string]string{"target_model": "Rig", "firmware_version": "v2.0.0"}, wantError: "target_manufacturer"},
		{name: "model", fields: map[string]string{"target_manufacturer": "Proto", "firmware_version": "v2.0.0"}, wantError: "target_model"},
		{name: "version", fields: map[string]string{"target_manufacturer": "Proto", "target_model": "Rig"}, wantError: "firmware_version"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newTestEnv(t)
			env.expectAuth()
			req := createMultipartRequestWithFields(t, "firmware.swu", []byte("data"), validSessionCookie(env.sessionID), tt.fields)
			rr := httptest.NewRecorder()

			env.uploadHandler().ServeHTTP(rr, req)

			assert.Equal(t, http.StatusBadRequest, rr.Code)
			assert.Contains(t, rr.Body.String(), tt.wantError)
			staged, err := files.StagedFirmwareUploadCount()
			require.NoError(t, err)
			assert.Zero(t, staged)
		})
	}
}

func TestUploadHandler_RejectsMissingFileField(t *testing.T) {
	env := newTestEnv(t)
	env.expectAuth()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/firmware/upload", nil)
	req.Header.Set("Content-Type", "multipart/form-data; boundary=xxx")
	req.AddCookie(validSessionCookie(env.sessionID))
	rr := httptest.NewRecorder()

	env.uploadHandler().ServeHTTP(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestUploadHandler_SuccessfulUpload(t *testing.T) {
	env := newTestEnv(t)
	env.expectAuth()
	content := []byte("fake firmware content for testing")
	req := createMultipartRequest(t, "firmware-v2.0.swu", content, validSessionCookie(env.sessionID))
	rr := httptest.NewRecorder()

	env.uploadHandler().ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), "firmware_file_id")
	assert.Contains(t, rr.Header().Get("Content-Type"), "application/json")
}

func TestUploadHandler_AcceptsMetadataAfterFile(t *testing.T) {
	env := newTestEnv(t)
	env.expectAuth()
	req := createFileFirstMultipartRequest(t, "firmware-v2.0.swu", []byte("file-first firmware"), validSessionCookie(env.sessionID))
	rr := httptest.NewRecorder()

	env.uploadHandler().ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	stored, err := env.fileSvc.ListFirmwareFiles()
	require.NoError(t, err)
	require.Len(t, stored, 1)
	assert.Equal(t, "Proto", stored[0].TargetManufacturer)
	assert.Equal(t, "Rig", stored[0].TargetModel)
	assert.Equal(t, "v2.0.0", stored[0].FirmwareVersion)
	staged, err := files.StagedFirmwareUploadCount()
	require.NoError(t, err)
	assert.Zero(t, staged)
}

func TestUploadHandler_LogsNewFirmwareActivity(t *testing.T) {
	env := newTestEnv(t)
	env.expectAuth()
	activityStore := storeMocks.NewMockActivityStore(env.ctrl)
	activityStore.EXPECT().Insert(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, event *activityModels.Event) error {
			assert.Equal(t, activityModels.CategorySystem, event.Category)
			assert.Equal(t, firmwareUploadedEventType, event.Type)
			assert.Equal(t, "Uploaded firmware file: firmware-v2.0.swu", event.Description)
			assert.Equal(t, "testuser", *event.Username)
			assert.Equal(t, int64(1), *event.OrganizationID)
			assert.Equal(t, "Proto", event.Metadata["target_manufacturer"])
			assert.Equal(t, "Rig", event.Metadata["target_model"])
			assert.Equal(t, "v2.0.0", event.Metadata["firmware_version"])
			return nil
		},
	)

	h := env.uploadHandler()
	h.activitySvc = activityDomain.NewService(activityStore)
	req := createMultipartRequest(t, "firmware-v2.0.swu", []byte("activity firmware"), validSessionCookie(env.sessionID))
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestUploadHandler_SuccessfulUploadTarGz(t *testing.T) {
	env := newTestEnv(t)
	env.expectAuth()
	content := []byte("fake tar.gz firmware")
	req := createMultipartRequest(t, "upgrade.tar.gz", content, validSessionCookie(env.sessionID))
	rr := httptest.NewRecorder()

	env.uploadHandler().ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), "firmware_file_id")
}

func TestUploadHandler_RejectsOversizedBody(t *testing.T) {
	env := newTestEnv(t)
	env.expectAuth()
	env.fileSvc, _ = files.NewService(files.Config{MaxFirmwareFileSize: 50})

	h := env.uploadHandler()

	oversized := []byte(strings.Repeat("x", 200))
	req := createMultipartRequest(t, "firmware.swu", oversized, validSessionCookie(env.sessionID))
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	assert.NotEqual(t, http.StatusOK, rr.Code)
}

// --- Check handler tests ---

func TestCheckHandler_RejectsNonPostMethod(t *testing.T) {
	env := newTestEnv(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/firmware/check", nil)
	rr := httptest.NewRecorder()

	env.checkHandler().ServeHTTP(rr, req)

	assert.Equal(t, http.StatusMethodNotAllowed, rr.Code)
}

func TestCheckHandler_RejectsNoCookie(t *testing.T) {
	env := newTestEnv(t)
	req := createCheckRequest(t, `{"sha256":"abc"}`, nil)
	rr := httptest.NewRecorder()

	env.checkHandler().ServeHTTP(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
}

func TestCheckHandler_RejectsInvalidJSON(t *testing.T) {
	env := newTestEnv(t)
	env.expectAuth()
	req := createCheckRequest(t, `not json`, validSessionCookie(env.sessionID))
	rr := httptest.NewRecorder()

	env.checkHandler().ServeHTTP(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "invalid JSON")
}

func TestCheckHandler_RejectsInvalidChecksumLength(t *testing.T) {
	env := newTestEnv(t)
	env.expectAuth()
	req := createCheckRequest(t, `{"sha256":"tooshort"}`, validSessionCookie(env.sessionID))
	rr := httptest.NewRecorder()

	env.checkHandler().ServeHTTP(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "64-character hex string")
}

func TestCheckHandler_RejectsNonHexChecksum(t *testing.T) {
	env := newTestEnv(t)
	env.expectAuth()
	nonHex := strings.Repeat("zz", 32) // 64 chars but not valid hex
	req := createCheckRequest(t, firmwareCheckBody(nonHex), validSessionCookie(env.sessionID))
	rr := httptest.NewRecorder()

	env.checkHandler().ServeHTTP(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "64-character hex string")
}

func TestCheckHandler_ReturnsFalseForUnknownChecksum(t *testing.T) {
	env := newTestEnv(t)
	env.expectAuth()
	checksum := strings.Repeat("a", 64)
	req := createCheckRequest(t, firmwareCheckBody(checksum), validSessionCookie(env.sessionID))
	rr := httptest.NewRecorder()

	env.checkHandler().ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), `"exists":false`)
	assert.NotContains(t, rr.Body.String(), "firmware_file_id")
}

func TestCheckHandler_RequiresMatchingTargetMetadata(t *testing.T) {
	env := newTestEnv(t)
	env.expectAuth()

	content := "firmware content for target mismatch"
	fileID, err := env.fileSvc.SaveFirmwareFile("firmware.swu", strings.NewReader(content), testFirmwareMetadata())
	require.NoError(t, err)

	body := fmt.Sprintf(`{"sha256":%q,"target_manufacturer":"Proto","target_model":"S19","firmware_version":"v2.0.0"}`, sha256Hex(content))
	req := createCheckRequest(t, body, validSessionCookie(env.sessionID))
	rr := httptest.NewRecorder()

	env.checkHandler().ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), `"exists":false`)
	assert.NotContains(t, rr.Body.String(), fileID)
}

func TestCheckHandler_ReturnsTrueForKnownChecksum(t *testing.T) {
	env := newTestEnv(t)
	env.expectAuth()

	content := "firmware content for check test"
	fileID, err := env.fileSvc.SaveFirmwareFile("firmware.swu", strings.NewReader(content), testFirmwareMetadata())
	require.NoError(t, err)

	checksum := sha256Hex(content)
	req := createCheckRequest(t, firmwareCheckBody(checksum), validSessionCookie(env.sessionID))
	rr := httptest.NewRecorder()

	env.checkHandler().ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), `"exists":true`)
	assert.Contains(t, rr.Body.String(), fileID)
}

func TestCheckHandler_AcceptsUppercaseChecksum(t *testing.T) {
	env := newTestEnv(t)
	env.expectAuth()

	content := "firmware for uppercase check"
	fileID, err := env.fileSvc.SaveFirmwareFile("firmware.swu", strings.NewReader(content), testFirmwareMetadata())
	require.NoError(t, err)

	checksum := strings.ToUpper(sha256Hex(content))
	req := createCheckRequest(t, firmwareCheckBody(checksum), validSessionCookie(env.sessionID))
	rr := httptest.NewRecorder()

	env.checkHandler().ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), `"exists":true`)
	assert.Contains(t, rr.Body.String(), fileID)
}

func TestCheckHandler_RejectsOversizedBody(t *testing.T) {
	env := newTestEnv(t)
	env.expectAuth()
	checksum := strings.Repeat("a", 64)
	body := `{"sha256":"` + checksum + `"}` + strings.Repeat(" ", 2000)
	req := createCheckRequest(t, body, validSessionCookie(env.sessionID))
	rr := httptest.NewRecorder()

	env.checkHandler().ServeHTTP(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "request body too large")
}

func TestCheckHandler_RejectsTrailingGarbage(t *testing.T) {
	env := newTestEnv(t)
	env.expectAuth()
	checksum := strings.Repeat("a", 64)
	body := `{"sha256":"` + checksum + `"}{"injection":"attack"}`
	req := createCheckRequest(t, body, validSessionCookie(env.sessionID))
	rr := httptest.NewRecorder()

	env.checkHandler().ServeHTTP(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "invalid JSON body")
}

// --- Config handler tests ---

func TestConfigHandler_RejectsNoCookie(t *testing.T) {
	env := newTestEnv(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/firmware/config", nil)
	rr := httptest.NewRecorder()

	env.configHandler().ServeHTTP(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
}

func TestConfigHandler_ReturnsConfigOnSuccess(t *testing.T) {
	env := newTestEnv(t)
	env.expectAuth()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/firmware/config", nil)
	req.AddCookie(validSessionCookie(env.sessionID))
	rr := httptest.NewRecorder()

	env.configHandler().ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Header().Get("Content-Type"), "application/json")

	var resp configResponse
	err := json.Unmarshal(rr.Body.Bytes(), &resp)
	require.NoError(t, err)

	assert.Equal(t, []string{".swu", ".tar.gz", ".zip"}, resp.AllowedExtensions)
	assert.Equal(t, int64(500*1024*1024), resp.MaxFileSizeBytes)
	assert.Equal(t, int64(32*1024*1024), resp.ChunkSizeBytes)
}

func TestConfigHandler_DefaultsChunkSizeWhenZero(t *testing.T) {
	env := newTestEnv(t)
	env.expectAuth()

	h := env.configHandler()
	h.cfg.ChunkSizeBytes = 0

	req := httptest.NewRequest(http.MethodGet, "/api/v1/firmware/config", nil)
	req.AddCookie(validSessionCookie(env.sessionID))
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var resp configResponse
	err := json.Unmarshal(rr.Body.Bytes(), &resp)
	require.NoError(t, err)

	assert.Equal(t, int64(32*1024*1024), resp.ChunkSizeBytes)
}

// --- List files handler tests ---

func (e *testEnv) listFilesHandler() *listFilesHandler {
	return &listFilesHandler{
		filesService:   e.fileSvc,
		sessionService: e.sessionSvc,
		userStore:      e.userStoreMock,
	}
}

func (e *testEnv) deleteFileHandler() *deleteFileHandler {
	return &deleteFileHandler{
		filesService:       e.fileSvc,
		sessionService:     e.sessionSvc,
		userStore:          e.userStoreMock,
		permissionResolver: e.permissionResolver,
	}
}

func (e *testEnv) deleteAllFilesHandler() *deleteAllFilesHandler {
	return &deleteAllFilesHandler{
		filesService:       e.fileSvc,
		sessionService:     e.sessionSvc,
		userStore:          e.userStoreMock,
		permissionResolver: e.permissionResolver,
	}
}

func TestListFilesHandler_RejectsNoCookie(t *testing.T) {
	env := newTestEnv(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/firmware/files", nil)
	rr := httptest.NewRecorder()

	env.listFilesHandler().ServeHTTP(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
}

func TestListFilesHandler_ReturnsEmptyArray(t *testing.T) {
	env := newTestEnv(t)
	env.expectAuth()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/firmware/files", nil)
	req.AddCookie(validSessionCookie(env.sessionID))
	rr := httptest.NewRecorder()

	env.listFilesHandler().ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Header().Get("Content-Type"), "application/json")

	var resp listFilesResponse
	err := json.Unmarshal(rr.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.NotNil(t, resp.Files)
	assert.Empty(t, resp.Files)
}

func TestListFilesHandler_ReturnsSavedFiles(t *testing.T) {
	env := newTestEnv(t)
	env.expectAuth()

	_, err := env.fileSvc.SaveFirmwareFile("alpha.swu", strings.NewReader("alpha"), testFirmwareMetadata())
	require.NoError(t, err)
	_, err = env.fileSvc.SaveFirmwareFile("beta.tar.gz", strings.NewReader("beta content"), testFirmwareMetadata())
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/firmware/files", nil)
	req.AddCookie(validSessionCookie(env.sessionID))
	rr := httptest.NewRecorder()

	env.listFilesHandler().ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var resp listFilesResponse
	err = json.Unmarshal(rr.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Len(t, resp.Files, 2)
	for _, file := range resp.Files {
		assert.Equal(t, "Proto", file.TargetManufacturer)
		assert.Equal(t, "Rig", file.TargetModel)
	}
}

// --- Update metadata handler tests ---

func TestUpdateMetadataHandler_UpdatesStoredFirmware(t *testing.T) {
	env := newTestEnv(t)
	env.expectAuth()
	fileID, err := env.fileSvc.SaveFirmwareFile("firmware.swu", strings.NewReader("data"), testFirmwareMetadata())
	require.NoError(t, err)

	body := `{"target_manufacturer":"Bitmain","target_model":"S19","firmware_version":"v3.0.0"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/firmware/files/"+fileID, strings.NewReader(body))
	req.SetPathValue("fileId", fileID)
	req.AddCookie(validSessionCookie(env.sessionID))
	rr := httptest.NewRecorder()

	env.updateMetadataHandler().ServeHTTP(rr, req)

	assert.Equal(t, http.StatusNoContent, rr.Code)
	metadata, err := env.fileSvc.GetFirmwareMetadata(fileID)
	require.NoError(t, err)
	assert.Equal(t, files.FirmwareMetadata{
		TargetManufacturer: "Bitmain",
		TargetModel:        "S19",
		FirmwareVersion:    "v3.0.0",
	}, metadata)
}

func TestUpdateMetadataHandler_RejectsIncompleteMetadata(t *testing.T) {
	env := newTestEnv(t)
	env.expectAuth()
	fileID, err := env.fileSvc.SaveFirmwareFile("firmware.swu", strings.NewReader("data"), testFirmwareMetadata())
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/firmware/files/"+fileID, strings.NewReader(`{"target_manufacturer":"Proto"}`))
	req.SetPathValue("fileId", fileID)
	req.AddCookie(validSessionCookie(env.sessionID))
	rr := httptest.NewRecorder()

	env.updateMetadataHandler().ServeHTTP(rr, req)

	assertJSONErrorResponse(t, rr, http.StatusBadRequest, "FleetError: invalid_argument (Common: 0) target_model is required")
}

func TestUpdateMetadataHandler_Returns404ForMissingFile(t *testing.T) {
	env := newTestEnv(t)
	env.expectAuth()
	fileID := "00000000-0000-0000-0000-000000000000"
	body := `{"target_manufacturer":"Proto","target_model":"Rig","firmware_version":"v2.0.0"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/firmware/files/"+fileID, strings.NewReader(body))
	req.SetPathValue("fileId", fileID)
	req.AddCookie(validSessionCookie(env.sessionID))
	rr := httptest.NewRecorder()

	env.updateMetadataHandler().ServeHTTP(rr, req)

	assertJSONErrorResponse(t, rr, http.StatusNotFound, "FleetError: not_found (Common: 0) firmware file not found: "+fileID)
}

// --- Delete file handler tests ---

func TestDeleteFileHandler_RejectsNoCookie(t *testing.T) {
	env := newTestEnv(t)
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/firmware/files/some-id", nil)
	rr := httptest.NewRecorder()

	env.deleteFileHandler().ServeHTTP(rr, req)

	assertJSONErrorResponse(t, rr, http.StatusUnauthorized, "authentication required")
}

func TestDeleteFileHandler_DeletesExistingFile(t *testing.T) {
	env := newTestEnv(t)
	env.expectAuth()

	fileID, err := env.fileSvc.SaveFirmwareFile("firmware.swu", strings.NewReader("data"), testFirmwareMetadata())
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/firmware/files/"+fileID, nil)
	req.SetPathValue("fileId", fileID)
	req.AddCookie(validSessionCookie(env.sessionID))
	rr := httptest.NewRecorder()

	env.deleteFileHandler().ServeHTTP(rr, req)

	assert.Equal(t, http.StatusNoContent, rr.Code)

	_, err = env.fileSvc.GetFirmwareFilePath(fileID)
	assert.Error(t, err, "file should be deleted")
}

func TestDeleteFileHandler_Returns400ForInvalidID(t *testing.T) {
	env := newTestEnv(t)
	env.expectAuth()

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/firmware/files/not-a-uuid", nil)
	req.SetPathValue("fileId", "not-a-uuid")
	req.AddCookie(validSessionCookie(env.sessionID))
	rr := httptest.NewRecorder()

	env.deleteFileHandler().ServeHTTP(rr, req)

	assertJSONErrorResponse(t, rr, http.StatusBadRequest, "FleetError: invalid_argument (Common: 0) invalid firmware file ID: not-a-uuid")
}

func TestDeleteFileHandler_Returns404ForMissingFile(t *testing.T) {
	env := newTestEnv(t)
	env.expectAuth()

	missingID := "00000000-0000-0000-0000-000000000000"
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/firmware/files/"+missingID, nil)
	req.SetPathValue("fileId", missingID)
	req.AddCookie(validSessionCookie(env.sessionID))
	rr := httptest.NewRecorder()

	env.deleteFileHandler().ServeHTTP(rr, req)

	assertJSONErrorResponse(t, rr, http.StatusNotFound, "FleetError: not_found (Common: 0) firmware file not found: "+missingID)
}

// --- Delete all files handler tests ---

func TestDeleteAllFilesHandler_RejectsNoCookie(t *testing.T) {
	env := newTestEnv(t)
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/firmware/files", nil)
	rr := httptest.NewRecorder()

	env.deleteAllFilesHandler().ServeHTTP(rr, req)

	assertJSONErrorResponse(t, rr, http.StatusUnauthorized, "authentication required")
}

func TestDeleteAllFilesHandler_DeletesAllFiles(t *testing.T) {
	env := newTestEnv(t)
	env.expectAuth()

	_, err := env.fileSvc.SaveFirmwareFile("one.swu", strings.NewReader("one"), testFirmwareMetadata())
	require.NoError(t, err)
	_, err = env.fileSvc.SaveFirmwareFile("two.swu", strings.NewReader("two"), testFirmwareMetadata())
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/firmware/files", nil)
	req.AddCookie(validSessionCookie(env.sessionID))
	rr := httptest.NewRecorder()

	env.deleteAllFilesHandler().ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var resp deleteAllFilesResponse
	err = json.Unmarshal(rr.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, 2, resp.DeletedCount)
}

func TestDeleteAllFilesHandler_EmptyReturnsZero(t *testing.T) {
	env := newTestEnv(t)
	env.expectAuth()

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/firmware/files", nil)
	req.AddCookie(validSessionCookie(env.sessionID))
	rr := httptest.NewRecorder()

	env.deleteAllFilesHandler().ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var resp deleteAllFilesResponse
	err := json.Unmarshal(rr.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, 0, resp.DeletedCount)
}
