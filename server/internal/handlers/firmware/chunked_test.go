package firmware

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/infrastructure/files"
)

func TestParseContentRange(t *testing.T) {
	tests := []struct {
		name      string
		header    string
		wantStart int64
		wantEnd   int64
		wantTotal int64
		wantErr   bool
	}{
		{"valid", "bytes 0-4/10", 0, 4, 10, false},
		{"valid second chunk", "bytes 5-9/10", 5, 9, 10, false},
		{"missing header", "", 0, 0, 0, true},
		{"wrong prefix", "octets 0-4/10", 0, 0, 0, true},
		{"missing slash", "bytes 0-4", 0, 0, 0, true},
		{"missing dash", "bytes 04/10", 0, 0, 0, true},
		{"end exceeds total", "bytes 0-10/10", 0, 0, 0, true},
		{"negative start", "bytes -1-4/10", 0, 0, 0, true},
		{"zero total", "bytes 0-0/0", 0, 0, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end, total, err := parseContentRange(tt.header)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantStart, start)
			assert.Equal(t, tt.wantEnd, end)
			assert.Equal(t, tt.wantTotal, total)
		})
	}
}

func TestChunkedUpload_FullLifecycle(t *testing.T) {
	env := newTestEnv(t)
	mgr := NewChunkedUploadManager()

	initHandler := &initiateHandler{mgr: mgr, filesService: env.fileSvc, sessionService: env.sessionSvc, userStore: env.userStoreMock}
	chunkH := &chunkHandler{mgr: mgr, sessionService: env.sessionSvc, userStore: env.userStoreMock}
	completeH := &completeHandler{mgr: mgr, filesService: env.fileSvc, sessionService: env.sessionSvc, userStore: env.userStoreMock}

	content := "abcdefghij" // 10 bytes, 2 chunks of 5

	// Initiate
	env.expectAuth()
	body := fmt.Sprintf(`{"filename":"firmware.swu","file_size":%d}`, len(content))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/firmware/upload/chunked", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(validSessionCookie(env.sessionID))
	rr := httptest.NewRecorder()

	initHandler.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)

	var initResp initiateResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &initResp))
	assert.NotEmpty(t, initResp.UploadID)
	uploadID := initResp.UploadID

	// Chunk 1
	env.expectAuth()
	req = httptest.NewRequest(http.MethodPut, "/api/v1/firmware/upload/chunked/"+uploadID, strings.NewReader(content[:5]))
	req.Header.Set("Content-Range", "bytes 0-4/10")
	req.AddCookie(validSessionCookie(env.sessionID))
	req.SetPathValue("uploadId", uploadID)
	rr = httptest.NewRecorder()

	chunkH.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)

	// Chunk 2
	env.expectAuth()
	req = httptest.NewRequest(http.MethodPut, "/api/v1/firmware/upload/chunked/"+uploadID, strings.NewReader(content[5:]))
	req.Header.Set("Content-Range", "bytes 5-9/10")
	req.AddCookie(validSessionCookie(env.sessionID))
	req.SetPathValue("uploadId", uploadID)
	rr = httptest.NewRecorder()

	chunkH.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)

	// Complete
	env.expectAuth()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/firmware/upload/chunked/"+uploadID+"/complete", nil)
	req.AddCookie(validSessionCookie(env.sessionID))
	req.SetPathValue("uploadId", uploadID)
	rr = httptest.NewRecorder()

	completeH.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), "firmware_file_id")
}

func TestChunkedUpload_InitiateRejectsInvalidExtension(t *testing.T) {
	env := newTestEnv(t)
	env.expectAuth()
	mgr := NewChunkedUploadManager()

	h := &initiateHandler{mgr: mgr, filesService: env.fileSvc, sessionService: env.sessionSvc, userStore: env.userStoreMock}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/firmware/upload/chunked", strings.NewReader(`{"filename":"bad.bin","file_size":100}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(validSessionCookie(env.sessionID))
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "unsupported firmware file type")
}

func TestChunkedUpload_InitiateRejectsOversizedFile(t *testing.T) {
	env := newTestEnv(t)
	env.expectAuth()
	env.fileSvc, _ = files.NewService(files.Config{MaxFirmwareFileSize: 100})
	mgr := NewChunkedUploadManager()

	h := &initiateHandler{mgr: mgr, filesService: env.fileSvc, sessionService: env.sessionSvc, userStore: env.userStoreMock}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/firmware/upload/chunked", strings.NewReader(`{"filename":"firmware.swu","file_size":200}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(validSessionCookie(env.sessionID))
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "exceeds maximum")
}

func TestChunkedUpload_InitiateRejectsAuth(t *testing.T) {
	env := newTestEnv(t)
	mgr := NewChunkedUploadManager()

	h := &initiateHandler{mgr: mgr, filesService: env.fileSvc, sessionService: env.sessionSvc, userStore: env.userStoreMock}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/firmware/upload/chunked", strings.NewReader(`{"filename":"firmware.swu","file_size":100}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusUnauthorized, rr.Code)
}

func TestChunkedUpload_ChunkRejectsUnknownSession(t *testing.T) {
	env := newTestEnv(t)
	env.expectAuth()
	mgr := NewChunkedUploadManager()

	h := &chunkHandler{mgr: mgr, sessionService: env.sessionSvc, userStore: env.userStoreMock}
	req := httptest.NewRequest(http.MethodPut, "/api/v1/firmware/upload/chunked/nonexistent", strings.NewReader("data"))
	req.Header.Set("Content-Range", "bytes 0-3/10")
	req.AddCookie(validSessionCookie(env.sessionID))
	req.SetPathValue("uploadId", "nonexistent")
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestChunkedUpload_ChunkRejectsOutOfOrder(t *testing.T) {
	env := newTestEnv(t)
	mgr := NewChunkedUploadManager()

	initHandler := &initiateHandler{mgr: mgr, filesService: env.fileSvc, sessionService: env.sessionSvc, userStore: env.userStoreMock}
	chunkH := &chunkHandler{mgr: mgr, sessionService: env.sessionSvc, userStore: env.userStoreMock}

	// Initiate
	env.expectAuth()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/firmware/upload/chunked", strings.NewReader(`{"filename":"firmware.swu","file_size":10}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(validSessionCookie(env.sessionID))
	rr := httptest.NewRecorder()
	initHandler.ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)

	var initResp initiateResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &initResp))

	// Send chunk starting at byte 5 when 0 is expected
	env.expectAuth()
	req = httptest.NewRequest(http.MethodPut, "/api/v1/firmware/upload/chunked/"+initResp.UploadID, strings.NewReader("hello"))
	req.Header.Set("Content-Range", "bytes 5-9/10")
	req.AddCookie(validSessionCookie(env.sessionID))
	req.SetPathValue("uploadId", initResp.UploadID)
	rr = httptest.NewRecorder()

	chunkH.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "expected chunk starting at byte 0")
}

func TestChunkedUpload_CompleteRejectsSizeMismatch(t *testing.T) {
	env := newTestEnv(t)
	mgr := NewChunkedUploadManager()

	initHandler := &initiateHandler{mgr: mgr, filesService: env.fileSvc, sessionService: env.sessionSvc, userStore: env.userStoreMock}
	chunkH := &chunkHandler{mgr: mgr, sessionService: env.sessionSvc, userStore: env.userStoreMock}
	completeH := &completeHandler{mgr: mgr, filesService: env.fileSvc, sessionService: env.sessionSvc, userStore: env.userStoreMock}

	// Initiate with file_size=10
	env.expectAuth()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/firmware/upload/chunked", strings.NewReader(`{"filename":"firmware.swu","file_size":10}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(validSessionCookie(env.sessionID))
	rr := httptest.NewRecorder()
	initHandler.ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)

	var initResp initiateResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &initResp))

	// Send only 5 bytes
	env.expectAuth()
	req = httptest.NewRequest(http.MethodPut, "/api/v1/firmware/upload/chunked/"+initResp.UploadID, strings.NewReader("hello"))
	req.Header.Set("Content-Range", "bytes 0-4/10")
	req.AddCookie(validSessionCookie(env.sessionID))
	req.SetPathValue("uploadId", initResp.UploadID)
	rr = httptest.NewRecorder()
	chunkH.ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)

	// Complete should fail because only 5 of 10 bytes received
	env.expectAuth()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/firmware/upload/chunked/"+initResp.UploadID+"/complete", nil)
	req.AddCookie(validSessionCookie(env.sessionID))
	req.SetPathValue("uploadId", initResp.UploadID)
	rr = httptest.NewRecorder()

	completeH.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "received 5 bytes but expected 10")
}

func TestChunkedUpload_DuplicateChunkRejected(t *testing.T) {
	env := newTestEnv(t)
	mgr := NewChunkedUploadManager()

	initHandler := &initiateHandler{mgr: mgr, filesService: env.fileSvc, sessionService: env.sessionSvc, userStore: env.userStoreMock}
	chunkH := &chunkHandler{mgr: mgr, sessionService: env.sessionSvc, userStore: env.userStoreMock}

	// Initiate
	env.expectAuth()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/firmware/upload/chunked", strings.NewReader(`{"filename":"firmware.swu","file_size":10}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(validSessionCookie(env.sessionID))
	rr := httptest.NewRecorder()
	initHandler.ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)

	var initResp initiateResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &initResp))

	// First chunk succeeds
	env.expectAuth()
	req = httptest.NewRequest(http.MethodPut, "/api/v1/firmware/upload/chunked/"+initResp.UploadID, strings.NewReader("hello"))
	req.Header.Set("Content-Range", "bytes 0-4/10")
	req.AddCookie(validSessionCookie(env.sessionID))
	req.SetPathValue("uploadId", initResp.UploadID)
	rr = httptest.NewRecorder()
	chunkH.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)

	// Duplicate chunk (same range) should be rejected because receivedBytes moved to 5
	env.expectAuth()
	req = httptest.NewRequest(http.MethodPut, "/api/v1/firmware/upload/chunked/"+initResp.UploadID, strings.NewReader("hello"))
	req.Header.Set("Content-Range", "bytes 0-4/10")
	req.AddCookie(validSessionCookie(env.sessionID))
	req.SetPathValue("uploadId", initResp.UploadID)
	rr = httptest.NewRecorder()
	chunkH.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "expected chunk starting at byte 5")
}

func TestChunkedUpload_CleanupSparesActiveSession(t *testing.T) {
	tmp := t.TempDir()
	mgr := NewChunkedUploadManager()

	tempPath := filepath.Join(tmp, "active-upload")
	require.NoError(t, os.WriteFile(tempPath, []byte("partial"), 0600))

	mgr.mu.Lock()
	mgr.sessions["active"] = &uploadSession{
		uploadID:     "active",
		tempFilePath: tempPath,
		createdAt:    time.Now().Add(-2 * time.Hour),
		lastActivity: time.Now(),
	}
	mgr.mu.Unlock()

	mgr.cleanupExpired(time.Hour)

	mgr.mu.Lock()
	_, exists := mgr.sessions["active"]
	mgr.mu.Unlock()

	assert.True(t, exists, "session with recent activity should survive cleanup")
	assert.FileExists(t, tempPath, "temp file for active session should not be deleted")
}

func TestChunkedUpload_CleanupRemovesExpiredSession(t *testing.T) {
	tmp := t.TempDir()
	mgr := NewChunkedUploadManager()

	tempPath := filepath.Join(tmp, "expired-upload")
	require.NoError(t, os.WriteFile(tempPath, []byte("stale"), 0600))

	mgr.mu.Lock()
	mgr.sessions["expired"] = &uploadSession{
		uploadID:     "expired",
		tempFilePath: tempPath,
		createdAt:    time.Now().Add(-3 * time.Hour),
		lastActivity: time.Now().Add(-2 * time.Hour),
	}
	mgr.mu.Unlock()

	mgr.cleanupExpired(time.Hour)

	mgr.mu.Lock()
	_, exists := mgr.sessions["expired"]
	mgr.mu.Unlock()

	assert.False(t, exists, "expired session should be removed by cleanup")
	_, err := os.Stat(tempPath)
	assert.True(t, os.IsNotExist(err), "temp file for expired session should be deleted")
}
