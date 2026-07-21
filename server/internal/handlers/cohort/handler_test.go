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
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/block/proto-fleet/server/generated/grpc/cohort/v1"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	domaincohort "github.com/block/proto-fleet/server/internal/domain/cohort"
	"github.com/block/proto-fleet/server/internal/domain/cohort/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
	telemetrymodels "github.com/block/proto-fleet/server/internal/domain/telemetry/models"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
)

func TestAdminRPCsRequireSuperAdminRole(t *testing.T) {
	t.Parallel()

	for _, role := range []string{"FIELD_TECH", "ADMIN"} {
		t.Run(role, func(t *testing.T) {
			t.Parallel()

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

func TestGetCohortFirmwareVersionHistory_UsesCallerOrganization(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	store := mocks.NewMockCohortStore(ctrl)
	handler := NewHandler(domaincohort.NewService(store))
	start := time.Date(2026, time.July, 14, 12, 0, 0, 0, time.UTC)
	end := start.Add(10 * time.Minute)
	store.EXPECT().GetCohort(gomock.Any(), int64(7), int64(42)).Return(&models.Cohort{
		ID: 42, OrgID: 7, Members: []models.CohortMember{{CohortID: 42, OrgID: 7, DeviceIdentifier: "miner-1"}},
	}, nil)
	store.EXPECT().ListCohortFirmwareVersionEvents(gomock.Any(), int64(7), int64(42), start, end).Return([]models.FirmwareVersionEvent{
		{DeviceIdentifier: "miner-1", FirmwareVersion: "1.2.3", ObservedAt: start.Add(-time.Minute)},
	}, nil)

	resp, err := handler.GetCohortFirmwareVersionHistory(
		cohortHandlerContextWithPermissions("USER", authz.PermCohortRead),
		connect.NewRequest(&pb.GetCohortFirmwareVersionHistoryRequest{
			CohortId: 42, StartTime: timestamppb.New(start), EndTime: timestamppb.New(end), Granularity: durationpb.New(10 * time.Minute),
		}),
	)
	require.NoError(t, err)
	assert.Equal(t, int32(1), resp.Msg.GetMemberCount())
	require.Len(t, resp.Msg.GetPoints(), 2)
	assert.Equal(t, "1.2.3", resp.Msg.GetPoints()[0].GetVersions()[0].GetFirmwareVersion())
}

func TestGetCohortFirmwareVersionHistory_RequiresReadPermission(t *testing.T) {
	t.Parallel()

	handler := NewHandler(domaincohort.NewService(mocks.NewMockCohortStore(gomock.NewController(t))))
	_, err := handler.GetCohortFirmwareVersionHistory(
		cohortHandlerContextWithPermissions("USER", authz.PermCohortManage),
		connect.NewRequest(&pb.GetCohortFirmwareVersionHistoryRequest{}),
	)
	require.Error(t, err)
	assert.True(t, fleeterror.IsForbiddenError(err))
}

func TestGetCohortFirmwareValidation_UsesCallerOrganization(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	store := mocks.NewMockCohortStore(ctrl)
	handler := NewHandler(domaincohort.NewService(store))
	store.EXPECT().GetCohort(gomock.Any(), int64(7), int64(42)).Return(&models.Cohort{
		ID: 42, OrgID: 7, State: models.CohortStateActive,
	}, nil)

	resp, err := handler.GetCohortFirmwareValidation(
		cohortHandlerContext("ADMIN"),
		connect.NewRequest(&pb.GetCohortFirmwareValidationRequest{
			CohortId:         42,
			Manufacturer:     "Proto",
			Model:            "Rig",
			ComparisonWindow: pb.CohortFirmwareValidationWindow_COHORT_FIRMWARE_VALIDATION_WINDOW_ONE_HOUR,
		}),
	)
	require.NoError(t, err)
	assert.Equal(t, pb.CohortFirmwareValidationState_COHORT_FIRMWARE_VALIDATION_STATE_NO_TARGET, resp.Msg.GetState())
}

func TestGetCohortFirmwareValidation_RequiresReadPermission(t *testing.T) {
	t.Parallel()

	handler := NewHandler(domaincohort.NewService(nil))
	_, err := handler.GetCohortFirmwareValidation(
		context.Background(),
		connect.NewRequest(&pb.GetCohortFirmwareValidationRequest{}),
	)
	require.Error(t, err)
}

type handlerComparisonTelemetryProvider struct {
	query telemetrymodels.DeviceOutcomeComparisonQuery
}

func (p *handlerComparisonTelemetryProvider) GetDeviceOutcomeAverages(_ context.Context, query telemetrymodels.DeviceOutcomeComparisonQuery) ([]telemetrymodels.DeviceOutcomeAverages, error) {
	p.query = query
	baseline, current := 3200.0, 3000.0
	return []telemetrymodels.DeviceOutcomeAverages{{
		DeviceID: telemetrymodels.DeviceIdentifier("miner-1"), BaselinePower: &baseline, ComparisonPower: &current,
	}}, nil
}

func TestGetCohortTelemetryComparisonUsesCallerOrganizationAndTranslatesResponse(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := mocks.NewMockCohortStore(ctrl)
	provider := &handlerComparisonTelemetryProvider{}
	handler := NewHandler(domaincohort.NewService(store, domaincohort.WithOutcomeTelemetryProvider(provider)))
	store.EXPECT().ListCohortTelemetryComparisonMemberships(gomock.Any(), int64(7), []int64{1}).Return(
		[]models.CohortTelemetryComparisonMembership{{
			CohortID: 1, Label: "Default", IsDefault: true, DeviceIdentifiers: []string{"miner-1"},
		}}, nil,
	)

	resp, err := handler.GetCohortTelemetryComparison(
		cohortHandlerContextWithPermissions("USER", authz.PermCohortRead),
		connect.NewRequest(&pb.GetCohortTelemetryComparisonRequest{
			CohortIds:        []int64{1},
			ComparisonWindow: pb.CohortTelemetryComparisonWindow_COHORT_TELEMETRY_COMPARISON_WINDOW_ONE_HOUR,
		}),
	)
	require.NoError(t, err)
	require.Len(t, resp.Msg.GetSeries(), 1)
	assert.Equal(t, "Default", resp.Msg.GetSeries()[0].GetLabel())
	assert.True(t, resp.Msg.GetSeries()[0].GetIsDefault())
	require.Len(t, resp.Msg.GetSeries()[0].GetDistributions(), 3)
	power := resp.Msg.GetSeries()[0].GetDistributions()[2]
	assert.Equal(t, pb.CohortTelemetryComparisonMetric_COHORT_TELEMETRY_COMPARISON_METRIC_POWER, power.GetMetric())
	assert.InDelta(t, 3200, power.GetBaselineMedian(), 0.001)
	assert.InDelta(t, 3000, power.GetComparisonMedian(), 0.001)
	assert.InDelta(t, -6.25, power.GetMedianPercentageChange(), 0.001)
	assert.Equal(t, int64(7), provider.query.OrganizationID)
}

func TestGetCohortTelemetryComparisonRequiresReadPermission(t *testing.T) {
	handler := NewHandler(domaincohort.NewService(nil))
	_, err := handler.GetCohortTelemetryComparison(
		context.Background(),
		connect.NewRequest(&pb.GetCohortTelemetryComparisonRequest{}),
	)
	require.Error(t, err)
}

func cohortHandlerContext(role string) context.Context {
	return cohortHandlerContextWithPermissions(role, authz.PermCohortManage, authz.PermCohortRead)
}

func cohortHandlerContextWithPermissions(role string, permissions ...string) context.Context {
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
