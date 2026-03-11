package firmware

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
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

	"github.com/btc-mining/proto-fleet/server/internal/domain/session"
	sessionMocks "github.com/btc-mining/proto-fleet/server/internal/domain/session/mocks"
	"github.com/btc-mining/proto-fleet/server/internal/domain/stores/interfaces"
	storeMocks "github.com/btc-mining/proto-fleet/server/internal/domain/stores/interfaces/mocks"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/files"
)

const testMaxUploadBytes int64 = 10 * 1024 * 1024 // 10 MB for tests

type testEnv struct {
	ctrl             *gomock.Controller
	sessionStoreMock *sessionMocks.MockStore
	userStoreMock    *storeMocks.MockUserStore
	fileSvc          *files.Service
	sessionSvc       *session.Service
	sessionID        string
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
		Return(interfaces.User{ID: 1, Username: "testuser"}, nil)
}

func (e *testEnv) uploadHandler() *uploadHandler {
	return &uploadHandler{
		filesService:   e.fileSvc,
		sessionService: e.sessionSvc,
		userStore:      e.userStoreMock,
		maxUploadBytes: testMaxUploadBytes,
	}
}

func (e *testEnv) checkHandler() *checkHandler {
	return &checkHandler{
		filesService:   e.fileSvc,
		sessionService: e.sessionSvc,
		userStore:      e.userStoreMock,
	}
}

func createMultipartRequest(t *testing.T, filename string, content []byte, cookie *http.Cookie) *http.Request {
	t.Helper()

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("file", filename)
	require.NoError(t, err)
	_, err = part.Write(content)
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/firmware/upload", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if cookie != nil {
		req.AddCookie(cookie)
	}
	return req
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
	h.maxUploadBytes = 50

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
	req := createCheckRequest(t, `{"sha256":"`+nonHex+`"}`, validSessionCookie(env.sessionID))
	rr := httptest.NewRecorder()

	env.checkHandler().ServeHTTP(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "64-character hex string")
}

func TestCheckHandler_ReturnsFalseForUnknownChecksum(t *testing.T) {
	env := newTestEnv(t)
	env.expectAuth()
	checksum := strings.Repeat("a", 64)
	req := createCheckRequest(t, `{"sha256":"`+checksum+`"}`, validSessionCookie(env.sessionID))
	rr := httptest.NewRecorder()

	env.checkHandler().ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), `"exists":false`)
	assert.NotContains(t, rr.Body.String(), "firmware_file_id")
}

func TestCheckHandler_ReturnsTrueForKnownChecksum(t *testing.T) {
	env := newTestEnv(t)
	env.expectAuth()

	content := "firmware content for check test"
	fileID, err := env.fileSvc.SaveFirmwareFile("firmware.swu", strings.NewReader(content))
	require.NoError(t, err)

	checksum := sha256Hex(content)
	req := createCheckRequest(t, `{"sha256":"`+checksum+`"}`, validSessionCookie(env.sessionID))
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
	fileID, err := env.fileSvc.SaveFirmwareFile("firmware.swu", strings.NewReader(content))
	require.NoError(t, err)

	checksum := strings.ToUpper(sha256Hex(content))
	req := createCheckRequest(t, `{"sha256":"`+checksum+`"}`, validSessionCookie(env.sessionID))
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
