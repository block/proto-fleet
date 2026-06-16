package curtailment

import (
	"context"
	"testing"

	"connectrpc.com/authn"
	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/block/proto-fleet/server/generated/grpc/curtailment/v1"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	domainCurtailment "github.com/block/proto-fleet/server/internal/domain/curtailment"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
)

// withCurtailmentRead returns ctx wrapped with an org-scope
// EffectivePermissions carrying PermCurtailmentRead so RequirePermission
// in the handler clears without further setup.
func withCurtailmentRead(ctx context.Context) context.Context {
	eff := authz.NewEffectivePermissions([]authz.Assignment{{
		AssignmentID: 1,
		ScopeType:    authz.ScopeOrg,
		Permissions:  []string{authz.PermCurtailmentRead},
	}})
	return middleware.WithEffectivePermissions(ctx, eff)
}

func TestHandler_GetActiveCurtailment_ReturnsActiveEvent(t *testing.T) {
	t.Parallel()
	store := newStopStubStore()
	store.activeEvent = store.event
	h := NewHandler(domainCurtailment.NewService(store))
	ctx := authn.SetInfo(t.Context(), &session.Info{
		AuthMethod:     session.AuthMethodSession,
		OrganizationID: 42,
		UserID:         9,
		Role:           "OPERATOR",
	})

	resp, err := h.GetActiveCurtailment(withCurtailmentRead(ctx), connect.NewRequest(&pb.GetActiveCurtailmentRequest{}))
	require.NoError(t, err)
	require.NotNil(t, resp.Msg.Event)
	assert.Equal(t, store.event.EventUUID.String(), resp.Msg.Event.EventUuid)
	assert.Equal(t, pb.CurtailmentEventState_CURTAILMENT_EVENT_STATE_ACTIVE, resp.Msg.Event.State)
	require.Len(t, resp.Msg.Event.Targets, 2)
	assert.Equal(t, pb.CurtailmentTargetState_CURTAILMENT_TARGET_STATE_CONFIRMED, resp.Msg.Event.Targets[0].State)
	assert.Equal(t, pb.CurtailmentTargetDesiredState_CURTAILMENT_TARGET_DESIRED_STATE_CURTAILED, resp.Msg.Event.Targets[0].DesiredState)
	assert.Equal(t, int32(2), resp.Msg.Event.TargetRollup.Confirmed)
	assert.Equal(t, int32(2), resp.Msg.Event.TargetRollup.Total)
}

func TestHandler_GetActiveCurtailment_UsesSiteScopedEventPermission(t *testing.T) {
	t.Parallel()
	const (
		orgID  = int64(42)
		siteID = int64(7)
	)

	for _, tc := range []struct {
		name        string
		assignments []authz.Assignment
		wantCode    connect.Code
	}{
		{"org permission without site narrowing allows read", []authz.Assignment{testOrgAssignment(authz.PermCurtailmentRead)}, 0},
		{"matching site narrowing allows read", []authz.Assignment{testOrgAssignment(authz.PermCurtailmentRead), testSiteAssignment(siteID, authz.PermCurtailmentRead)}, 0},
		{"site-only permission denies read", []authz.Assignment{testSiteAssignment(siteID, authz.PermCurtailmentRead)}, connect.CodePermissionDenied},
		{"site narrowing without read denies read", []authz.Assignment{testOrgAssignment(authz.PermCurtailmentRead), testSiteAssignment(siteID)}, connect.CodePermissionDenied},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			store := newStopStubStore()
			store.event.OrgID = orgID
			store.event.ScopeType = models.ScopeTypeSite
			store.event.ScopeJSON = siteScopeJSON(t, siteID)
			store.activeEvent = store.event
			h := NewHandler(domainCurtailment.NewService(store))
			ctx := testSessionCtxWithAssignments(t, &session.Info{
				AuthMethod:     session.AuthMethodSession,
				OrganizationID: orgID,
				UserID:         9,
				Role:           "OPERATOR",
			}, tc.assignments...)

			resp, err := h.GetActiveCurtailment(ctx, connect.NewRequest(&pb.GetActiveCurtailmentRequest{}))

			if tc.wantCode == 0 {
				require.NoError(t, err)
				require.NotNil(t, resp.Msg.Event)
				assert.Equal(t, store.event.EventUUID.String(), resp.Msg.Event.EventUuid)
			} else {
				require.Error(t, err)
				var fleetErr fleeterror.FleetError
				require.ErrorAs(t, err, &fleetErr)
				assert.Equal(t, tc.wantCode, fleetErr.GRPCCode)
			}
		})
	}
}

func TestHandler_GetActiveCurtailment_ReturnsEmptyWhenNoActiveEvent(t *testing.T) {
	t.Parallel()
	store := newStopStubStore()
	h := NewHandler(domainCurtailment.NewService(store))
	ctx := authn.SetInfo(t.Context(), &session.Info{
		AuthMethod:     session.AuthMethodSession,
		OrganizationID: 42,
		UserID:         9,
		Role:           "OPERATOR",
	})

	resp, err := h.GetActiveCurtailment(withCurtailmentRead(ctx), connect.NewRequest(&pb.GetActiveCurtailmentRequest{}))
	require.NoError(t, err)
	assert.Nil(t, resp.Msg.Event)
}

func TestHandler_GetActiveCurtailment_RejectsMissingSession(t *testing.T) {
	t.Parallel()
	store := newStopStubStore()
	h := NewHandler(domainCurtailment.NewService(store))

	_, err := h.GetActiveCurtailment(t.Context(), connect.NewRequest(&pb.GetActiveCurtailmentRequest{}))
	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodeUnauthenticated, fleetErr.GRPCCode)
}

func TestHandler_GetActiveCurtailment_PropagatesStoreError(t *testing.T) {
	t.Parallel()
	store := newStopStubStore()
	store.getActiveErr = fleeterror.NewInternalError("db down")
	h := NewHandler(domainCurtailment.NewService(store))
	ctx := authn.SetInfo(t.Context(), &session.Info{
		AuthMethod:     session.AuthMethodSession,
		OrganizationID: 42,
		UserID:         9,
		Role:           "OPERATOR",
	})

	_, err := h.GetActiveCurtailment(withCurtailmentRead(ctx), connect.NewRequest(&pb.GetActiveCurtailmentRequest{}))
	require.Error(t, err)
	assert.ErrorContains(t, err, "db down")
}

func TestHandler_GetActiveCurtailment_MapsRestoringEvent(t *testing.T) {
	t.Parallel()
	store := newStopStubStore()
	restoring := *store.event
	restoring.State = models.EventStateRestoring
	store.activeEvent = &restoring
	h := NewHandler(domainCurtailment.NewService(store))
	ctx := authn.SetInfo(t.Context(), &session.Info{
		AuthMethod:     session.AuthMethodSession,
		OrganizationID: 42,
		UserID:         9,
		Role:           "OPERATOR",
	})

	resp, err := h.GetActiveCurtailment(withCurtailmentRead(ctx), connect.NewRequest(&pb.GetActiveCurtailmentRequest{}))
	require.NoError(t, err)
	require.NotNil(t, resp.Msg.Event)
	assert.Equal(t, pb.CurtailmentEventState_CURTAILMENT_EVENT_STATE_RESTORING, resp.Msg.Event.State)
}
