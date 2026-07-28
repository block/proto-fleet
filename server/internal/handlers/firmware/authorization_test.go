package firmware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/infrastructure/files"
)

func TestFirmwareMutationHandlers_RejectUserWithoutFirmwareUpdatePermission(t *testing.T) {
	tests := []struct {
		name    string
		method  string
		path    string
		handler func(*testEnv) http.Handler
	}{
		{
			name:   "direct upload",
			method: http.MethodPost,
			path:   "/api/v1/firmware/upload",
			handler: func(env *testEnv) http.Handler {
				return env.uploadHandler()
			},
		},
		{
			name:   "initiate chunked upload",
			method: http.MethodPost,
			path:   "/api/v1/firmware/upload/chunked",
			handler: func(env *testEnv) http.Handler {
				return &initiateHandler{
					mgr:                NewChunkedUploadManager(),
					filesService:       env.fileSvc,
					sessionService:     env.sessionSvc,
					userStore:          env.userStoreMock,
					permissionResolver: env.permissionResolver,
				}
			},
		},
		{
			name:   "upload chunk",
			method: http.MethodPut,
			path:   "/api/v1/firmware/upload/chunked/upload-id",
			handler: func(env *testEnv) http.Handler {
				return &chunkHandler{
					mgr:                NewChunkedUploadManager(),
					sessionService:     env.sessionSvc,
					userStore:          env.userStoreMock,
					permissionResolver: env.permissionResolver,
				}
			},
		},
		{
			name:   "complete chunked upload",
			method: http.MethodPost,
			path:   "/api/v1/firmware/upload/chunked/upload-id/complete",
			handler: func(env *testEnv) http.Handler {
				return &completeHandler{
					mgr:                NewChunkedUploadManager(),
					filesService:       env.fileSvc,
					sessionService:     env.sessionSvc,
					userStore:          env.userStoreMock,
					permissionResolver: env.permissionResolver,
				}
			},
		},
		{
			name:   "update metadata",
			method: http.MethodPatch,
			path:   "/api/v1/firmware/files/file-id",
			handler: func(env *testEnv) http.Handler {
				return env.updateMetadataHandler()
			},
		},
		{
			name:   "delete file",
			method: http.MethodDelete,
			path:   "/api/v1/firmware/files/file-id",
			handler: func(env *testEnv) http.Handler {
				return env.deleteFileHandler()
			},
		},
		{
			name:   "delete all files",
			method: http.MethodDelete,
			path:   "/api/v1/firmware/files",
			handler: func(env *testEnv) http.Handler {
				return env.deleteAllFilesHandler()
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newTestEnv(t)
			env.permissionResolver = staticPermissionResolver{
				permissions: []string{authz.PermFleetRead},
			}
			env.expectAuth()

			req := httptest.NewRequest(tt.method, tt.path, nil)
			req.AddCookie(validSessionCookie(env.sessionID))
			rr := httptest.NewRecorder()

			tt.handler(env).ServeHTTP(rr, req)

			assertJSONErrorResponse(t, rr, http.StatusForbidden, "permission denied")
		})
	}
}

func TestUpdateMetadataHandler_DenialPreservesStoredTarget(t *testing.T) {
	env := newTestEnv(t)
	fileID, err := env.fileSvc.SaveFirmwareFile(
		"firmware.swu",
		strings.NewReader("firmware payload"),
		testFirmwareMetadata(),
	)
	require.NoError(t, err)

	env.permissionResolver = staticPermissionResolver{
		permissions: []string{authz.PermFleetRead},
	}
	env.expectAuth()

	body := `{"target_manufacturer":"Bitmain","target_model":"S19","firmware_version":"v3.0.0"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/firmware/files/"+fileID, strings.NewReader(body))
	req.SetPathValue("fileId", fileID)
	req.AddCookie(validSessionCookie(env.sessionID))
	rr := httptest.NewRecorder()

	env.updateMetadataHandler().ServeHTTP(rr, req)

	require.Equal(t, http.StatusForbidden, rr.Code)
	metadata, err := env.fileSvc.GetFirmwareMetadata(fileID)
	require.NoError(t, err)
	assert.Equal(t, files.FirmwareMetadata{
		TargetManufacturer: "Proto",
		TargetModel:        "Rig",
		FirmwareVersion:    "v2.0.0",
	}, metadata)
}
