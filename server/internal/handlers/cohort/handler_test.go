package cohort

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/authn"
	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	pb "github.com/block/proto-fleet/server/generated/grpc/cohort/v1"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	domaincohort "github.com/block/proto-fleet/server/internal/domain/cohort"
	"github.com/block/proto-fleet/server/internal/domain/cohort/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
)

func TestAdminRPCsRequireSuperAdminRole(t *testing.T) {
	t.Parallel()

	for _, role := range []string{"FIELD_TECH", "ADMIN"} {
		t.Run(role, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			store := mocks.NewMockCohortStore(ctrl)
			handler := NewHandler(domaincohort.NewService(store))
			ctx := cohortHandlerContext(role)

			_, err := handler.AdminReleaseCohort(ctx, connect.NewRequest(&pb.AdminReleaseCohortRequest{CohortId: 42}))
			require.Error(t, err)
			assert.True(t, fleeterror.IsForbiddenError(err))

			_, err = handler.AdminReassign(ctx, connect.NewRequest(&pb.AdminReassignRequest{
				TargetCohortId:    42,
				DeviceIdentifiers: []string{"miner-1"},
			}))
			require.Error(t, err)
			assert.True(t, fleeterror.IsForbiddenError(err))
		})
	}
}

func TestAdminReleaseCohort_AllowsSuperAdmin(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	store := mocks.NewMockCohortStore(ctrl)
	handler := NewHandler(domaincohort.NewService(store))

	otherOwnerID := int64(99)
	now := time.Now()
	active := &models.Cohort{
		ID:          42,
		OrgID:       7,
		Label:       "reservation",
		OwnerUserID: &otherOwnerID,
		State:       models.CohortStateActive,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	released := *active
	released.State = models.CohortStateReleased
	store.EXPECT().GetCohort(gomock.Any(), int64(7), int64(42)).Return(active, nil)
	store.EXPECT().ReleaseCohort(gomock.Any(), int64(7), int64(42)).Return(&released, nil)

	resp, err := handler.AdminReleaseCohort(cohortHandlerContext("SUPER_ADMIN"), connect.NewRequest(&pb.AdminReleaseCohortRequest{CohortId: 42}))
	require.NoError(t, err)
	require.NotNil(t, resp.Msg.GetCohort())
	assert.Equal(t, pb.CohortState_COHORT_STATE_RELEASED, resp.Msg.GetCohort().GetSummary().GetState())
}

func TestAdminReassign_AllowsSuperAdmin(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	store := mocks.NewMockCohortStore(ctrl)
	handler := NewHandler(domaincohort.NewService(store))

	otherOwnerID := int64(99)
	now := time.Now()
	target := &models.Cohort{
		ID:          42,
		OrgID:       7,
		Label:       "reservation",
		OwnerUserID: &otherOwnerID,
		State:       models.CohortStateActive,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	moved := *target
	moved.Members = []models.CohortMember{{
		CohortID:         42,
		OrgID:            7,
		DeviceIdentifier: "miner-1",
		AddedAt:          now,
		Display: models.CohortDeviceDisplay{
			Manufacturer: "Proto",
			Model:        "Rig",
		},
	}}
	moved.ExplicitMemberCount = 1

	store.EXPECT().GetCohort(gomock.Any(), int64(7), int64(42)).Return(target, nil)
	store.EXPECT().
		ListCohortDeviceOwnership(gomock.Any(), int64(7), []string{"miner-1"}).
		Return([]models.CohortDeviceOwnership{{
			DeviceIdentifier: "miner-1",
			CohortID:         99,
			OwnerUserID:      &otherOwnerID,
		}}, nil)
	store.EXPECT().
		MoveDevicesToCohort(gomock.Any(), gomock.Cond(func(v any) bool {
			params, ok := v.(models.MembershipMutationParams)
			return ok && params.ActorRole == "SUPER_ADMIN" && params.CohortID == 42
		})).
		Return(&moved, nil)

	resp, err := handler.AdminReassign(cohortHandlerContext("SUPER_ADMIN"), connect.NewRequest(&pb.AdminReassignRequest{
		TargetCohortId:    42,
		DeviceIdentifiers: []string{"miner-1"},
	}))
	require.NoError(t, err)
	require.NotNil(t, resp.Msg.GetCohort())
	assert.Equal(t, int64(1), resp.Msg.GetCohort().GetSummary().GetExplicitMemberCount())
}

func cohortHandlerContext(role string) context.Context {
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
		Permissions:  []string{authz.PermCohortManage},
	}}))
}
