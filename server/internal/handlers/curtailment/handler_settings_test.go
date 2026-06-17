package curtailment

import (
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	pb "github.com/block/proto-fleet/server/generated/grpc/curtailment/v1"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	domainCurtailment "github.com/block/proto-fleet/server/internal/domain/curtailment"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

func TestHandler_CurtailmentSettings(t *testing.T) {
	t.Parallel()

	store := newStartStubStore()
	h := NewHandler(domainCurtailment.NewService(store))
	ctx := sessionCtxWithPerms(42, authz.PermCurtailmentManage)

	getResp, err := h.GetCurtailmentSettings(ctx, connect.NewRequest(&pb.GetCurtailmentSettingsRequest{}))
	require.NoError(t, err)
	assert.Equal(t, uint32(600), getResp.Msg.GetSettings().GetPostEventCooldownSec())

	updateResp, err := h.UpdateCurtailmentSettings(
		ctx,
		connect.NewRequest(&pb.UpdateCurtailmentSettingsRequest{PostEventCooldownSec: proto.Uint32(0)}),
	)
	require.NoError(t, err)
	assert.Equal(t, uint32(0), updateResp.Msg.GetSettings().GetPostEventCooldownSec())

	getResp, err = h.GetCurtailmentSettings(ctx, connect.NewRequest(&pb.GetCurtailmentSettingsRequest{}))
	require.NoError(t, err)
	assert.Equal(t, uint32(0), getResp.Msg.GetSettings().GetPostEventCooldownSec())
}

func TestHandler_CurtailmentSettingsRequireManage(t *testing.T) {
	t.Parallel()

	store := newStartStubStore()
	h := NewHandler(domainCurtailment.NewService(store))
	_, err := h.GetCurtailmentSettings(
		sessionCtxWithPerms(42, authz.PermCurtailmentRead),
		connect.NewRequest(&pb.GetCurtailmentSettingsRequest{}),
	)
	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodePermissionDenied, fleetErr.GRPCCode)

	_, err = h.UpdateCurtailmentSettings(
		sessionCtxWithPerms(42, authz.PermCurtailmentRead),
		connect.NewRequest(&pb.UpdateCurtailmentSettingsRequest{PostEventCooldownSec: proto.Uint32(0)}),
	)
	require.Error(t, err)
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodePermissionDenied, fleetErr.GRPCCode)
	assert.Equal(t, int32(600), store.orgConfig.PostEventCooldownSec)
}

func TestHandler_UpdateCurtailmentSettingsRequiresCooldownPresence(t *testing.T) {
	t.Parallel()

	h := NewHandler(domainCurtailment.NewService(newStartStubStore()))
	_, err := h.UpdateCurtailmentSettings(
		sessionCtxWithPerms(42, authz.PermCurtailmentManage),
		connect.NewRequest(&pb.UpdateCurtailmentSettingsRequest{}),
	)
	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodeInvalidArgument, fleetErr.GRPCCode)
}
