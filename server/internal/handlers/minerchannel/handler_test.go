package minerchannel

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/authn"
	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	pb "github.com/block/proto-fleet/server/generated/grpc/minerchannel/v1"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	minerChannelDomain "github.com/block/proto-fleet/server/internal/domain/minerchannel"
	"github.com/block/proto-fleet/server/internal/domain/minerchannel/models"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
)

func TestAdminRPCsRequireSuperAdminRole(t *testing.T) {
	t.Parallel()

	for _, role := range []string{"FIELD_TECH", "ADMIN"} {
		t.Run(role, func(t *testing.T) {
			t.Parallel()

			ctrl := gomock.NewController(t)
			store := mocks.NewMockMinerChannelStore(ctrl)
			handler := NewHandler(minerChannelDomain.NewService(store))
			ctx := minerChannelHandlerContext(role)

			_, err := handler.AdminReleaseMinerChannel(ctx, connect.NewRequest(&pb.AdminReleaseMinerChannelRequest{MinerChannelId: 42}))
			require.Error(t, err)
			assert.True(t, fleeterror.IsForbiddenError(err))

			_, err = handler.AdminReassign(ctx, connect.NewRequest(&pb.AdminReassignRequest{
				TargetMinerChannelId: 42,
				DeviceIdentifiers:    []string{"miner-1"},
			}))
			require.Error(t, err)
			assert.True(t, fleeterror.IsForbiddenError(err))
		})
	}
}

func TestAdminReleaseMinerChannel_AllowsSuperAdmin(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	store := mocks.NewMockMinerChannelStore(ctrl)
	handler := NewHandler(minerChannelDomain.NewService(store))

	otherOwnerID := int64(99)
	now := time.Now()
	active := &models.MinerChannel{
		ID:          42,
		OrgID:       7,
		Label:       "reservation",
		OwnerUserID: &otherOwnerID,
		State:       models.MinerChannelStateActive,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	released := *active
	released.State = models.MinerChannelStateReleased
	store.EXPECT().GetMinerChannel(gomock.Any(), int64(7), int64(42)).Return(active, nil)
	store.EXPECT().ReleaseMinerChannel(gomock.Any(), int64(7), int64(42)).Return(&released, nil)

	resp, err := handler.AdminReleaseMinerChannel(minerChannelHandlerContext("SUPER_ADMIN"), connect.NewRequest(&pb.AdminReleaseMinerChannelRequest{MinerChannelId: 42}))
	require.NoError(t, err)
	require.NotNil(t, resp.Msg.GetMinerChannel())
	assert.Equal(t, pb.MinerChannelState_MINER_CHANNEL_STATE_RELEASED, resp.Msg.GetMinerChannel().GetSummary().GetState())
}

func TestAdminReassign_AllowsSuperAdmin(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	store := mocks.NewMockMinerChannelStore(ctrl)
	handler := NewHandler(minerChannelDomain.NewService(store))

	otherOwnerID := int64(99)
	now := time.Now()
	target := &models.MinerChannel{
		ID:          42,
		OrgID:       7,
		Label:       "reservation",
		OwnerUserID: &otherOwnerID,
		State:       models.MinerChannelStateActive,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	moved := *target
	moved.Members = []models.MinerChannelMember{{
		MinerChannelID:   42,
		OrgID:            7,
		DeviceIdentifier: "miner-1",
		AddedAt:          now,
		Display: models.MinerChannelDeviceDisplay{
			Manufacturer: "Proto",
			Model:        "Rig",
		},
	}}
	moved.ExplicitMemberCount = 1

	store.EXPECT().GetMinerChannel(gomock.Any(), int64(7), int64(42)).Return(target, nil)
	store.EXPECT().
		ListMinerChannelDeviceOwnership(gomock.Any(), int64(7), []string{"miner-1"}).
		Return([]models.MinerChannelDeviceOwnership{{
			DeviceIdentifier: "miner-1",
			MinerChannelID:   99,
			OwnerUserID:      &otherOwnerID,
		}}, nil)
	store.EXPECT().
		MoveDevicesToMinerChannel(gomock.Any(), gomock.Cond(func(v any) bool {
			params, ok := v.(models.MembershipMutationParams)
			return ok && params.ActorRole == "SUPER_ADMIN" && params.MinerChannelID == 42
		})).
		Return(&moved, nil)

	resp, err := handler.AdminReassign(minerChannelHandlerContext("SUPER_ADMIN"), connect.NewRequest(&pb.AdminReassignRequest{
		TargetMinerChannelId: 42,
		DeviceIdentifiers:    []string{"miner-1"},
	}))
	require.NoError(t, err)
	require.NotNil(t, resp.Msg.GetMinerChannel())
	assert.Equal(t, int64(1), resp.Msg.GetMinerChannel().GetSummary().GetExplicitMemberCount())
}

func minerChannelHandlerContext(role string) context.Context {
	return minerChannelHandlerContextWithPermissions(role, authz.PermMinerChannelManage, authz.PermMinerChannelRead)
}

func minerChannelHandlerContextWithPermissions(role string, permissions ...string) context.Context {
	info := &session.Info{
		AuthMethod:     session.AuthMethodSession,
		SessionID:      "session-1",
		UserID:         1,
		OrganizationID: 7,
		ExternalUserID: "user-1",
		Username:       "operator",
		Role:           role,
	}
	ctx := authn.SetInfo(context.Background(), info)
	return middleware.WithEffectivePermissions(ctx, authz.NewEffectivePermissions([]authz.Assignment{{
		AssignmentID: 1,
		ScopeType:    authz.ScopeOrg,
		Permissions:  permissions,
	}}))
}
