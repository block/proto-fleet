package curtailment

import (
	"testing"

	"connectrpc.com/authn"
	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/block/proto-fleet/server/generated/grpc/curtailment/v1"
	domainCurtailment "github.com/block/proto-fleet/server/internal/domain/curtailment"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/session"
)

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

	resp, err := h.GetActiveCurtailment(ctx, connect.NewRequest(&pb.GetActiveCurtailmentRequest{}))
	require.NoError(t, err)
	require.NotNil(t, resp.Msg.Event)
	assert.Equal(t, store.event.EventUUID.String(), resp.Msg.Event.EventUuid)
	assert.Equal(t, pb.CurtailmentEventState_CURTAILMENT_EVENT_STATE_ACTIVE, resp.Msg.Event.State)
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

	resp, err := h.GetActiveCurtailment(ctx, connect.NewRequest(&pb.GetActiveCurtailmentRequest{}))
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

	_, err := h.GetActiveCurtailment(ctx, connect.NewRequest(&pb.GetActiveCurtailmentRequest{}))
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

	resp, err := h.GetActiveCurtailment(ctx, connect.NewRequest(&pb.GetActiveCurtailmentRequest{}))
	require.NoError(t, err)
	require.NotNil(t, resp.Msg.Event)
	assert.Equal(t, pb.CurtailmentEventState_CURTAILMENT_EVENT_STATE_RESTORING, resp.Msg.Event.State)
}
