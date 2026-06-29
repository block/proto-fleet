package buildings

import (
	"context"
	"errors"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	pb "github.com/block/proto-fleet/server/generated/grpc/buildings/v1"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/buildings"
	"github.com/block/proto-fleet/server/internal/domain/buildings/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	minerModels "github.com/block/proto-fleet/server/internal/domain/miner/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
	modelsV2 "github.com/block/proto-fleet/server/internal/domain/telemetry/models/v2"
	"github.com/block/proto-fleet/server/internal/handlers/handlerstest"
)

type fakeStatsDeviceQueryer struct {
	deviceIDs   []string
	stateCounts interfaces.MinerStateCounts
	collections map[int64]interfaces.MinerStateCounts
}

func (f *fakeStatsDeviceQueryer) GetDeviceIdentifiersByOrgWithFilter(_ context.Context, _ int64, _ *interfaces.MinerFilter) ([]string, error) {
	return f.deviceIDs, nil
}
func (f *fakeStatsDeviceQueryer) GetMinerStateCountsByDeviceIDs(_ context.Context, _ int64, _ []string) (interfaces.MinerStateCounts, error) {
	return f.stateCounts, nil
}
func (f *fakeStatsDeviceQueryer) GetMinerStateCountsByCollections(_ context.Context, _ int64, _ []int64) (map[int64]interfaces.MinerStateCounts, error) {
	return f.collections, nil
}
func (f *fakeStatsDeviceQueryer) GetComponentErrorCounts(_ context.Context, _ int64, _ interfaces.ComponentErrorScope) ([]interfaces.ComponentErrorCount, error) {
	return nil, nil
}

type fakeStatsTelemetry struct {
	metrics map[minerModels.DeviceIdentifier]modelsV2.DeviceMetrics
}

func (f *fakeStatsTelemetry) GetLatestDeviceMetrics(_ context.Context, _ []minerModels.DeviceIdentifier) (map[minerModels.DeviceIdentifier]modelsV2.DeviceMetrics, error) {
	return f.metrics, nil
}

type statsHarness struct {
	handler       *Handler
	buildingStore *mocks.MockBuildingStore
	deviceQueryer *fakeStatsDeviceQueryer
}

func newStatsHandler(t *testing.T) *statsHarness {
	t.Helper()
	ctrl := gomock.NewController(t)
	buildingStore := mocks.NewMockBuildingStore(ctrl)
	siteStore := mocks.NewMockSiteStore(ctrl)
	collectionStore := mocks.NewMockCollectionStore(ctrl)
	tx := mocks.NewMockTransactor(ctrl)
	tx.EXPECT().RunInTx(gomock.Any(), gomock.Any()).AnyTimes().DoAndReturn(
		func(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) },
	)
	deviceQueryer := &fakeStatsDeviceQueryer{}
	telemetry := &fakeStatsTelemetry{}
	svc := buildings.NewService(buildingStore, siteStore, collectionStore, deviceQueryer, telemetry, tx, nil)
	return &statsHarness{handler: NewHandler(svc), buildingStore: buildingStore, deviceQueryer: deviceQueryer}
}

func intPtrStats(v int32) *int32 { return &v }

func TestHandler_GetBuildingStats_requiresSiteRead(t *testing.T) {
	t.Parallel()
	h := newStatsHandler(t)
	// GetBuilding runs before the authz checks so we can narrow them to
	// the building's site. The mock returns a building so the handler
	// reaches the perm check, which is what we're exercising.
	h.buildingStore.EXPECT().GetBuilding(gomock.Any(), int64(7), int64(1)).Return(&models.Building{}, nil)
	ctx := handlerstest.CtxWithPermissions(t, 7) // no perms
	_, err := h.handler.GetBuildingStats(ctx, connect.NewRequest(&pb.GetBuildingStatsRequest{BuildingId: 1}))
	require.Error(t, err)
	var ce *connect.Error
	if errors.As(err, &ce) {
		assert.Equal(t, connect.CodePermissionDenied, ce.Code())
	}
}

func TestHandler_GetBuildingStats_plumbsRackHealth(t *testing.T) {
	t.Parallel()
	h := newStatsHandler(t)

	h.buildingStore.EXPECT().ListBuildingRacks(gomock.Any(), gomock.Any(), int64(1), gomock.Any(), gomock.Any()).Return(
		[]models.BuildingRack{
			{RackID: 10, RackLabel: "R1", AisleIndex: intPtrStats(0), PositionInAisle: intPtrStats(0)},
		},
		"",
		nil,
	)
	// GetBuilding is called three times: once by the handler for
	// site-scoped authz narrowing, once by the service for the
	// layout-bounds clamp on rack_health, and once by the service's
	// post-read race re-check.
	h.buildingStore.EXPECT().GetBuilding(gomock.Any(), int64(7), int64(1)).Return(&models.Building{Aisles: 1, RacksPerAisle: 1}, nil).Times(3)
	h.deviceQueryer.collections = map[int64]interfaces.MinerStateCounts{
		10: {HashingCount: 3, BrokenCount: 1},
	}

	ctx := handlerstest.CtxWithPermissions(t, 7, authz.PermSiteRead, authz.PermFleetRead, authz.PermMinerRead)
	resp, err := h.handler.GetBuildingStats(ctx, connect.NewRequest(&pb.GetBuildingStatsRequest{BuildingId: 1}))
	require.NoError(t, err)
	assert.Equal(t, int64(1), resp.Msg.GetBuildingId())
	require.Len(t, resp.Msg.GetRackHealth(), 1)
	rh := resp.Msg.GetRackHealth()[0]
	assert.Equal(t, int64(10), rh.GetRackId())
	assert.Equal(t, "R1", rh.GetRackLabel())
	assert.Equal(t, int32(3), rh.GetHashingCount())
	assert.Equal(t, int32(1), rh.GetBrokenCount())
}

func TestHandler_GetBuildingStats_propagatesNotFound(t *testing.T) {
	t.Parallel()
	h := newStatsHandler(t)
	// GetBuilding runs first now so site-scoped authz can narrow against
	// the building's site. NotFound from this initial lookup propagates
	// out the same way the service-layer NotFound would.
	h.buildingStore.EXPECT().GetBuilding(gomock.Any(), int64(7), int64(99)).Return(nil, fleeterror.NewNotFoundError("building 99 not found"))

	ctx := handlerstest.CtxWithPermissions(t, 7, authz.PermSiteRead, authz.PermFleetRead, authz.PermMinerRead)
	_, err := h.handler.GetBuildingStats(ctx, connect.NewRequest(&pb.GetBuildingStatsRequest{BuildingId: 99}))
	require.Error(t, err)
	var fe fleeterror.FleetError
	if errors.As(err, &fe) {
		assert.Equal(t, connect.CodeNotFound, fe.GRPCCode)
	}
}
