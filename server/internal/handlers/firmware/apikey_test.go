package firmware

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/activity"
	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/block/proto-fleet/server/internal/domain/apikey"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/handlers/interceptors"
	"github.com/block/proto-fleet/server/internal/infrastructure/files"
	"github.com/block/proto-fleet/server/internal/testutil"
)

type apiKeyEnv struct {
	queries               *sqlc.Queries
	database              *testutil.DatabaseService
	mux                   *http.ServeMux
	mgr                   *ChunkedUploadManager
	keySvc                *apikey.Service
	sessionSvc            *session.Service
	activitySvc           *activity.Service
	rawKey                string
	key                   *interfaces.ApiKey
	userID, orgID, roleID int64
}

func newAPIKeyEnv(t *testing.T) *apiKeyEnv {
	t.Helper()
	cfg, err := testutil.GetTestConfig()
	require.NoError(t, err)
	database := testutil.NewDatabaseService(t, cfg)
	admin := database.CreateSuperAdminUser()
	q := sqlc.New(database.DB)
	role, err := q.CreateCustomRole(t.Context(), sqlc.CreateCustomRoleParams{Name: "Firmware operator", OrganizationID: sql.NullInt64{Int64: admin.OrganizationID, Valid: true}})
	require.NoError(t, err)
	userID, err := q.CreateUser(t.Context(), sqlc.CreateUserParams{UserID: "firmware-operator", Username: "operator@example.com", PasswordHash: "unused", CreatedAt: time.Now()})
	require.NoError(t, err)
	require.NoError(t, q.CreateUserOrganization(t.Context(), sqlc.CreateUserOrganizationParams{UserID: userID, OrganizationID: admin.OrganizationID, RoleID: role.ID}))
	_, err = q.AssignRole(t.Context(), sqlc.AssignRoleParams{UserID: userID, OrganizationID: admin.OrganizationID, RoleID: role.ID, ScopeType: "org"})
	require.NoError(t, err)

	userStore := sqlstores.NewSQLUserStore(database.DB)
	sessionSvc := session.NewService(session.Config{CookieName: "fleet_session", Duration: time.Hour, IDBytes: 32}, sqlstores.NewSQLSessionStore(database.DB))
	activitySvc := activity.NewService(sqlstores.NewSQLActivityStore(database.DB))
	keySvc := apikey.NewService(sqlstores.NewSQLApiKeyStore(database.DB), activitySvc)
	auth := interceptors.NewAuthInterceptor(sessionSvc, userStore, userStore, keySvc, authz.NewPermissionResolver(database.DB), nil, nil, nil)
	t.Chdir(t.TempDir())
	fileSvc, err := files.NewService(files.Config{})
	require.NoError(t, err)
	mgr := NewChunkedUploadManager()
	mux := http.NewServeMux()
	mux.Handle("POST /api/v1/firmware/upload", NewUploadHandler(fileSvc, auth, activitySvc))
	mux.Handle("POST /api/v1/firmware/check", NewCheckHandler(fileSvc, auth))
	mux.Handle("GET /api/v1/firmware/config", NewConfigHandler(fileSvc, auth, files.Config{}))
	mux.Handle("GET /api/v1/firmware/files", NewListFilesHandler(fileSvc, auth))
	mux.Handle("PATCH /api/v1/firmware/files/{fileId}", NewUpdateMetadataHandler(fileSvc, auth, activitySvc))
	mux.Handle("DELETE /api/v1/firmware/files/{fileId}", NewDeleteFileHandler(fileSvc, auth))
	mux.Handle("DELETE /api/v1/firmware/files", NewDeleteAllFilesHandler(fileSvc, auth))
	mux.Handle("POST /api/v1/firmware/upload/chunked", NewInitiateHandler(mgr, fileSvc, auth))
	mux.Handle("PUT /api/v1/firmware/upload/chunked/{uploadId}", NewChunkHandler(mgr, auth))
	mux.Handle("POST /api/v1/firmware/upload/chunked/{uploadId}/complete", NewCompleteHandler(mgr, fileSvc, auth, activitySvc))
	env := &apiKeyEnv{queries: q, database: database, mux: mux, mgr: mgr, keySvc: keySvc, sessionSvc: sessionSvc, activitySvc: activitySvc, userID: userID, orgID: admin.OrganizationID, roleID: role.ID}
	env.setPermissions(t, authz.PermMinerFirmwareUpdate)
	env.rawKey, env.key, err = keySvc.Create(t.Context(), userID, admin.OrganizationID, "firmware-operator", "operator@example.com", "deployment key", nil)
	require.NoError(t, err)
	return env
}

func (e *apiKeyEnv) setPermissions(t *testing.T, permissions ...string) {
	t.Helper()
	require.NoError(t, e.queries.ClearRolePermissions(t.Context(), e.roleID))
	for _, key := range permissions {
		permission, err := e.queries.GetPermissionByKey(t.Context(), key)
		require.NoError(t, err)
		require.NoError(t, e.queries.AssignPermissionToRole(t.Context(), sqlc.AssignPermissionToRoleParams{RoleID: e.roleID, PermissionID: permission.ID}))
	}
}

func (e *apiKeyEnv) serve(req *http.Request, key string) *httptest.ResponseRecorder {
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	rr := httptest.NewRecorder()
	e.mux.ServeHTTP(rr, req)
	return rr
}

func (e *apiKeyEnv) request(method, path, body string) *httptest.ResponseRecorder {
	return e.serve(httptest.NewRequest(method, path, strings.NewReader(body)), e.rawKey)
}

func (e *apiKeyEnv) initiate(t *testing.T) string {
	t.Helper()
	rr := e.request(http.MethodPost, "/api/v1/firmware/upload/chunked", chunkedInitiateBody("chunked.swu", 4))
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	var resp initiateResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	return "/api/v1/firmware/upload/chunked/" + resp.UploadID
}

func chunkRequest(path string) *http.Request {
	req := httptest.NewRequest(http.MethodPut, path, strings.NewReader("data"))
	req.Header.Set("Content-Range", "bytes 0-3/4")
	return req
}

func TestAPIKeyFirmwareLifecycle(t *testing.T) {
	env := newAPIKeyEnv(t)
	// A firmware-only role can use the whole file workflow without fleet:read.
	for _, path := range []string{"/api/v1/firmware/config", "/api/v1/firmware/files"} {
		rr := env.request(http.MethodGet, path, "")
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	}
	content := []byte("direct firmware")
	rr := env.serve(createMultipartRequest(t, "direct.swu", content, nil), env.rawKey)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	var uploaded uploadResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &uploaded))
	rr = env.request(http.MethodPost, "/api/v1/firmware/check", firmwareCheckBody(sha256Hex(string(content))))
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	assert.Contains(t, rr.Body.String(), uploaded.FirmwareFileID)
	rr = env.request(http.MethodPatch, "/api/v1/firmware/files/"+uploaded.FirmwareFileID, `{"target_manufacturer":"Proto","target_model":"Rig","firmware_version":"v3.0.0"}`)
	require.Equal(t, http.StatusNoContent, rr.Code, rr.Body.String())
	entries, err := env.activitySvc.List(t.Context(), activitymodels.Filter{OrganizationID: env.orgID, EventTypes: []string{firmwareUploadedEventType, firmwareMetadataUpdatedEventType}, PageSize: 10})
	require.NoError(t, err)
	require.Len(t, entries, 2)
	for _, entry := range entries {
		require.NotNil(t, entry.UserID)
		assert.Equal(t, "firmware-operator", *entry.UserID)
		var metadata map[string]any
		require.NoError(t, json.Unmarshal(entry.Metadata, &metadata))
		assert.Equal(t, env.key.KeyID, metadata["api_key_id"])
		assert.Equal(t, env.key.Name, metadata["api_key_name"])
	}
	rr = env.request(http.MethodDelete, "/api/v1/firmware/files/"+uploaded.FirmwareFileID, "")
	require.Equal(t, http.StatusNoContent, rr.Code, rr.Body.String())
	path := env.initiate(t)
	rr = env.serve(chunkRequest(path), env.rawKey)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	rr = env.request(http.MethodPost, path+"/complete", "")
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	rr = env.request(http.MethodDelete, "/api/v1/firmware/files", "")
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	assert.JSONEq(t, `{"deleted_count":1}`, rr.Body.String())
}

func TestAPIKeyFirmwareAuthorizationAndUploadOwnership(t *testing.T) {
	env := newAPIKeyEnv(t)
	path := env.initiate(t)
	otherKey, _, err := env.keySvc.Create(t.Context(), env.userID, env.orgID, "firmware-operator", "operator@example.com", "another key", nil)
	require.NoError(t, err)
	sameOrgUserID, err := env.queries.CreateUser(t.Context(), sqlc.CreateUserParams{UserID: "another-operator", Username: "another@example.com", PasswordHash: "unused", CreatedAt: time.Now()})
	require.NoError(t, err)
	require.NoError(t, env.queries.CreateUserOrganization(t.Context(), sqlc.CreateUserOrganizationParams{UserID: sameOrgUserID, OrganizationID: env.orgID, RoleID: env.roleID}))
	_, err = env.queries.AssignRole(t.Context(), sqlc.AssignRoleParams{UserID: sameOrgUserID, OrganizationID: env.orgID, RoleID: env.roleID, ScopeType: "org"})
	require.NoError(t, err)
	sameOrgKey, _, err := env.keySvc.Create(t.Context(), sameOrgUserID, env.orgID, "another-operator", "another@example.com", "another user", nil)
	require.NoError(t, err)
	otherUser := env.database.CreateSuperAdminUser2()
	crossOrgKey, _, err := env.keySvc.Create(t.Context(), otherUser.DatabaseID, otherUser.OrganizationID, "other-user", otherUser.Username, "another organization", nil)
	require.NoError(t, err)
	sess, err := env.sessionSvc.Create(t.Context(), env.userID, env.orgID, "test", "127.0.0.1")
	require.NoError(t, err)

	for name, key := range map[string]string{"same user different key": otherKey, "different user same organization": sameOrgKey, "different organization": crossOrgKey, "same user session": ""} {
		t.Run(name, func(t *testing.T) {
			req := chunkRequest(path)
			if key == "" {
				req.AddCookie(env.sessionSvc.CreateCookie(sess.SessionID))
			}
			rr := env.serve(req, key)
			assertJSONErrorResponse(t, rr, http.StatusNotFound, "upload session not found")
			req = httptest.NewRequest(http.MethodPost, path+"/complete", nil)
			if key == "" {
				req.AddCookie(env.sessionSvc.CreateCookie(sess.SessionID))
			}
			rr = env.serve(req, key)
			assertJSONErrorResponse(t, rr, http.StatusNotFound, "upload session not found")
		})
	}
	require.Len(t, env.mgr.sessions, 1, "rejected completion must preserve the original upload")

	env.setPermissions(t, authz.PermFleetRead)
	rr := env.serve(chunkRequest(path), env.rawKey)
	assertJSONErrorResponse(t, rr, http.StatusForbidden, "permission denied")
	rr = env.request(http.MethodPost, path+"/complete", "")
	assertJSONErrorResponse(t, rr, http.StatusForbidden, "permission denied")
	require.Len(t, env.mgr.sessions, 1)
	env.setPermissions(t, authz.PermMinerFirmwareUpdate)
	rr = env.serve(chunkRequest(path), env.rawKey)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	rr = env.request(http.MethodPost, path+"/complete", "")
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())

	path = env.initiate(t)
	require.NoError(t, env.keySvc.Revoke(t.Context(), env.key.KeyID, env.orgID, "firmware-operator", "operator@example.com"))
	rr = env.serve(chunkRequest(path), env.rawKey)
	assertJSONErrorResponse(t, rr, http.StatusUnauthorized, "authentication required")
	rr = env.request(http.MethodPost, path+"/complete", "")
	assertJSONErrorResponse(t, rr, http.StatusUnauthorized, "authentication required")
	require.Len(t, env.mgr.sessions, 1)
}

func TestAPIKeyFirmwareRejectsInvalidCredentialsAndMissingPermissions(t *testing.T) {
	env := newAPIKeyEnv(t)
	revoked, record, err := env.keySvc.Create(t.Context(), env.userID, env.orgID, "firmware-operator", "operator@example.com", "revoked", nil)
	require.NoError(t, err)
	require.NoError(t, env.keySvc.Revoke(t.Context(), record.KeyID, env.orgID, "firmware-operator", "operator@example.com"))
	sess, err := env.sessionSvc.Create(t.Context(), env.userID, env.orgID, "test", "127.0.0.1")
	require.NoError(t, err)
	endpoints := []struct {
		method, path string
		mutation     bool
	}{
		{http.MethodGet, "/api/v1/firmware/config", false},
		{http.MethodGet, "/api/v1/firmware/files", false},
		{http.MethodPost, "/api/v1/firmware/check", false},
		{http.MethodPost, "/api/v1/firmware/upload", true},
		{http.MethodPost, "/api/v1/firmware/upload/chunked", true},
		{http.MethodPut, "/api/v1/firmware/upload/chunked/id", true},
		{http.MethodPost, "/api/v1/firmware/upload/chunked/id/complete", true},
		{http.MethodPatch, "/api/v1/firmware/files/id", true},
		{http.MethodDelete, "/api/v1/firmware/files/id", true},
		{http.MethodDelete, "/api/v1/firmware/files", true},
	}
	for _, endpoint := range endpoints {
		for _, credentials := range []struct {
			name, header string
			cookie       bool
		}{
			{"missing", "", false}, {"malformed", "Basic key", false}, {"invalid", "Bearer invalid", false}, {"revoked", "Bearer " + revoked, false}, {"ambiguous", "Bearer " + env.rawKey, true},
		} {
			t.Run(fmt.Sprintf("%s %s/%s", endpoint.method, endpoint.path, credentials.name), func(t *testing.T) {
				req := httptest.NewRequest(endpoint.method, endpoint.path, nil)
				req.Header.Set("Authorization", credentials.header)
				if credentials.cookie {
					req.AddCookie(env.sessionSvc.CreateCookie(sess.SessionID))
				}
				rr := env.serve(req, "")
				assertJSONErrorResponse(t, rr, http.StatusUnauthorized, "authentication required")
			})
		}
	}
	env.setPermissions(t, authz.PermFleetRead)
	for _, endpoint := range endpoints {
		if endpoint.mutation {
			rr := env.request(endpoint.method, endpoint.path, "")
			assertJSONErrorResponse(t, rr, http.StatusForbidden, "permission denied")
		}
	}
	env.setPermissions(t)
	for _, endpoint := range endpoints {
		rr := env.request(endpoint.method, endpoint.path, "")
		assertJSONErrorResponse(t, rr, http.StatusForbidden, "permission denied")
	}
	require.NoError(t, env.queries.SoftDeleteUserFromOrganization(t.Context(), sqlc.SoftDeleteUserFromOrganizationParams{UserID: env.userID, OrganizationID: env.orgID}))
	for _, endpoint := range endpoints {
		rr := env.request(endpoint.method, endpoint.path, "")
		assertJSONErrorResponse(t, rr, http.StatusUnauthorized, "authentication required")
	}
}
