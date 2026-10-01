package rollout

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"connectrpc.com/validate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	"github.com/block/proto-fleet/server/generated/grpc/rollout/v1/rolloutv1connect"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/handlers/interceptors"
)

type restAdmissionGate struct {
	active bool
	calls  int
}

func (g *restAdmissionGate) Admit(ctx context.Context) (context.Context, func(), error) {
	g.calls++
	if !g.active {
		return nil, nil, errors.New("not active")
	}
	return ctx, func() {}, nil
}

func TestRESTPreservesHAAdmissionForReadsAndMutations(t *testing.T) {
	for _, tc := range []struct{ name, method, path, body string }{
		{"read", http.MethodGet, "/api/v1/rollouts/9", ""},
		{"mutation", http.MethodPost, "/api/v1/rollouts/9/advance", `{"count":1,"expectedRevision":"4"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gate := &restAdmissionGate{}
			svc := newFakeService()
			_, rpc := rolloutv1connect.NewRolloutServiceHandler(NewHandler(svc), connect.WithInterceptors(
				interceptors.NewErrorMappingInterceptor(),
				interceptors.NewActiveInterceptor(gate),
				validate.NewInterceptor(),
			))
			mux := http.NewServeMux()
			RegisterRESTRoutes(mux, rpc)
			request := func() *httptest.ResponseRecorder {
				req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
				req.Header.Set("Content-Type", "application/json")
				req = req.WithContext(ctxWithPermissions(t, authz.PermMinerFirmwareUpdate))
				rr := httptest.NewRecorder()
				mux.ServeHTTP(rr, req)
				return rr
			}

			rr := request()
			require.Equal(t, http.StatusServiceUnavailable, rr.Code, rr.Body.String())
			assert.Equal(t, 1, gate.calls)
			assert.Zero(t, svc.lastID, "HA rejection must not invoke the domain service")
			var failure struct {
				Code    string `json:"code"`
				Details []struct {
					Type  string          `json:"type"`
					Debug json.RawMessage `json:"debug"`
				} `json:"details"`
			}
			require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &failure))
			assert.Equal(t, "unavailable", failure.Code)
			require.Len(t, failure.Details, 1)
			assert.Equal(t, "common.v1.FleetErrorDetails", failure.Details[0].Type)
			var detail commonpb.FleetErrorDetails
			require.NoError(t, protojson.Unmarshal(failure.Details[0].Debug, &detail))
			assert.Equal(t, commonpb.FleetErrorCode_FLEET_ERROR_CODE_NOT_ACTIVE, detail.GetCommon())

			// The same valid request reaches the domain once this node is admitted.
			gate.active = true
			rr = request()
			require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
			assert.Equal(t, 2, gate.calls)
			assert.Equal(t, int64(9), svc.lastID)
		})
	}
}
