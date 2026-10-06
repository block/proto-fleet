package rollout

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"connectrpc.com/validate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"

	pb "github.com/block/proto-fleet/server/generated/grpc/rollout/v1"
	"github.com/block/proto-fleet/server/generated/grpc/rollout/v1/rolloutv1connect"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/rollout"
	"github.com/block/proto-fleet/server/internal/handlers/interceptors"
	"github.com/block/proto-fleet/server/internal/testutil"
)

func TestRESTQueryAndPathPreserveIDsAndFilters(t *testing.T) {
	mux := http.NewServeMux()
	RegisterRESTRoutes(mux, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, rolloutRPCPrefix+"ListRolloutEvents", r.URL.Path)
		assert.Empty(t, r.URL.RawQuery)
		assert.Equal(t, "Bearer example-key", r.Header.Get("Authorization"))
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		var request pb.ListRolloutEventsRequest
		require.NoError(t, protojson.Unmarshal(body, &request))
		assert.Equal(t, int64(9007199254740993), request.RolloutId)
		assert.Equal(t, int32(20), request.PageSize)
		assert.Equal(t, "opaque/+cursor=", request.Cursor)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"events":[],"cursor":"next"}`))
	}))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/rollouts/9007199254740993/events?pageSize=20&cursor=opaque%2F%2Bcursor%3D", nil)
	req.Header.Set("Authorization", "Bearer example-key")
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, req)
	assert.Equal(t, http.StatusOK, response.Code)
	assert.JSONEq(t, `{"events":[],"cursor":"next"}`, response.Body.String())
}

func TestRESTRejectsAmbiguousOrInvalidRequestsBeforeDispatch(t *testing.T) {
	for _, tc := range []struct{ name, method, path, body string }{
		{"conflicting body ID", "POST", "/api/v1/rollouts/7/advance", `{"rolloutId":"8","count":2}`},
		{"conflicting query ID", "GET", "/api/v1/rollouts/7?rolloutId=8", ""},
		{"negative ID", "GET", "/api/v1/rollouts/-1", ""},
		{"overflow ID", "GET", "/api/v1/rollouts/9223372036854775808", ""},
		{"unknown field", "POST", "/api/v1/rollouts/7/advance", `{"count":2,"unrecognized":1}`},
		{"duplicate field", "POST", "/api/v1/rollouts/7/advance", `{"count":2,"count":3}`},
		{"conflicting oneof", "POST", "/api/v1/rollouts/7/advance", `{"count":2,"devices":{"deviceIdentifiers":["miner-a"]}}`},
		{"repeated query", "GET", "/api/v1/rollouts?pageSize=2&pageSize=3", ""},
		{"aliased duplicate query", "GET", "/api/v1/rollouts?pageSize=2&page_size=3", ""},
		{"unknown query", "GET", "/api/v1/rollouts?unexpected=1", ""},
		{"invalid query escape", "GET", "/api/v1/rollouts?cursor=%xx", ""},
		{"mutation query ignored", "POST", "/api/v1/rollouts/7/pause?expectedRevision=1", "{}"},
		{"GET body ignored", "GET", "/api/v1/rollouts", `{"pageSize":1}`},
		{"trailing JSON", "POST", "/api/v1/rollouts/7/pause", "{}{}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mux := http.NewServeMux()
			RegisterRESTRoutes(mux, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("invalid request reached RPC") }))
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, req)
			assert.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
			var body map[string]any
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
			assert.Equal(t, "invalid_argument", body["code"])
		})
	}
}

func TestRESTMutationsRequireJSONEvenWithoutBody(t *testing.T) {
	for _, tc := range []struct{ name, contentType string }{
		{"missing", ""},
		{"form", "application/x-www-form-urlencoded"},
		{"text", "text/plain"},
		{"multipart", "multipart/form-data; boundary=fleet"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mux := http.NewServeMux()
			RegisterRESTRoutes(mux, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("non-JSON mutation reached RPC") }))
			req := httptest.NewRequest(http.MethodPost, "/api/v1/rollouts/7/rollback", nil)
			if tc.contentType != "" {
				req.Header.Set("Content-Type", tc.contentType)
			}
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, req)
			assert.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
			var body map[string]any
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
			assert.Equal(t, "invalid_argument", body["code"])
		})
	}

	mux := http.NewServeMux()
	RegisterRESTRoutes(mux, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		var request pb.PauseRolloutRequest
		require.NoError(t, protojson.Unmarshal(body, &request))
		assert.Equal(t, int64(7), request.RolloutId)
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/rollouts/7/pause", nil)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, req)
	assert.Equal(t, http.StatusOK, response.Code, response.Body.String())
}

func TestRESTPollingAcceptsEnumTimestampAndProtoFieldNames(t *testing.T) {
	mux := http.NewServeMux()
	RegisterRESTRoutes(mux, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		var request pb.ListRolloutsRequest
		require.NoError(t, protojson.Unmarshal(body, &request))
		assert.Equal(t, int64(42), request.ChannelId)
		assert.Equal(t, pb.RolloutStatus_ROLLOUT_STATUS_ACTIVE, request.Status)
		assert.Equal(t, int32(5), request.PageSize)
		assert.Equal(t, int64(1790676000), request.UpdatedAfter.Seconds)
		w.WriteHeader(http.StatusOK)
	}))
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/rollouts?channel_id=42&status=ROLLOUT_STATUS_ACTIVE&page_size=5&updated_after=2026-09-29T10%3A00%3A00Z", nil))
	assert.Equal(t, http.StatusOK, response.Code, response.Body.String())
}

func TestRESTDelegatedActionsUseAPIKeyAuthorizationAndRevisionErrors(t *testing.T) {
	config, err := testutil.GetTestConfig()
	require.NoError(t, err)
	database := testutil.NewDatabaseService(t, config)
	provider := testutil.NewServiceProvider(t, database.DB, config)
	owner := database.CreateSuperAdminUser()
	user, err := provider.UserStore.GetUserByID(t.Context(), owner.DatabaseID)
	require.NoError(t, err)
	key, _, err := provider.ApiKeyService.Create(t.Context(), owner.DatabaseID, owner.OrganizationID, user.UserID, owner.Username, "External controller", nil)
	require.NoError(t, err)
	auth := interceptors.NewAuthInterceptor(provider.SessionService, provider.UserStore, provider.UserStore, provider.ApiKeyService, provider.PermissionResolver, nil, nil, nil)
	svc := newFakeService()
	_, rpc := rolloutv1connect.NewRolloutServiceHandler(NewHandler(svc), connect.WithInterceptors(interceptors.NewErrorMappingInterceptor(), auth, validate.NewInterceptor()))
	mux := http.NewServeMux()
	RegisterRESTRoutes(mux, rpc)
	request := func(token, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/rollouts/9/advance", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	response := request("", `{"count":1}`)
	assert.Equal(t, http.StatusUnauthorized, response.Code, response.Body.String())
	response = request(key, `{"count":0}`)
	assert.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
	response = request(key, `{"count":1,"expectedRevision":"4","note":"Canary passed"}`)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	assert.Equal(t, owner.OrganizationID, svc.lastOrgID)
	assert.Equal(t, int64(9), svc.lastID)
	assert.Equal(t, int64(4), svc.lastMutation.ExpectedRevision)
	assert.Equal(t, "Canary passed", svc.lastMutation.Note)
	assert.Equal(t, rollout.ActorTypeAPIKey, svc.lastMutation.Actor.Type)
	assert.Equal(t, "External controller", svc.lastMutation.Actor.Name)

	svc.err = fleeterror.NewFailedPreconditionErrorf("stale revision: %w", &rollout.ErrorInfo{
		Reason: rollout.ReasonStaleRevision, CurrentRevision: 8,
	})
	response = request(key, `{"count":1,"expectedRevision":"4"}`)
	require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
	var failure struct {
		Code    string `json:"code"`
		Details []struct {
			Type  string          `json:"type"`
			Debug json.RawMessage `json:"debug"`
		} `json:"details"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &failure))
	assert.Equal(t, "failed_precondition", failure.Code)
	var found bool
	for _, detail := range failure.Details {
		if detail.Type == "rollout.v1.RolloutErrorInfo" {
			var info pb.RolloutErrorInfo
			require.NoError(t, protojson.Unmarshal(detail.Debug, &info))
			assert.Equal(t, pb.RolloutErrorReason_ROLLOUT_ERROR_REASON_STALE_REVISION, info.Reason)
			assert.Equal(t, int64(8), info.CurrentRevision)
			found = true
		}
	}
	assert.True(t, found, "REST errors must preserve structured rollout details")
}
