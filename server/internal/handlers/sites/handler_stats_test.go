package sites

import (
	"context"
	"errors"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	pb "github.com/block/proto-fleet/server/generated/grpc/sites/v1"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	minerModels "github.com/block/proto-fleet/server/internal/domain/miner/models"
	"github.com/block/proto-fleet/server/internal/domain/sites"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
	modelsV2 "github.com/block/proto-fleet/server/internal/domain/telemetry/models/v2"
	"github.com/block/proto-fleet/server/internal/handlers/handlerstest"
)

// Hand-rolled fakes for the stats-only devicerollup interfaces — same
// shape used by the domain tests, kept local so handler tests don't
// import an internal-test type from another package.
type fakeDeviceQueryer struct {
	deviceIDs   []string
	stateCounts interfaces.MinerStateCounts
	collections map[int64]interfaces.MinerStateCounts
}

func (f *fakeDeviceQueryer) GetDeviceIdentifiersByOrgWithFilter(_ context.Context, _ int64, _ *interfaces.MinerFilter) ([]string, error) {
	return f.deviceIDs, nil
}
func (f *fakeDeviceQueryer) GetMinerStateCountsByDeviceIDs(_ context.Context, _ int64, _ []string) (interfaces.MinerStateCounts, error) {
	return f.stateCounts, nil
}
func (f *fakeDeviceQueryer) GetMinerStateCountsByCollections(_ context.Context, _ int64, _ []int64) (map[int64]interfaces.MinerStateCounts, error) {
	return f.collections, nil
}

type fakeTelemetryCollector struct {
	metrics map[minerModels.DeviceIdentifier]modelsV2.DeviceMetrics
}

func (f *fakeTelemetryCollector) GetLatestDeviceMetrics(_ context.Context, _ []minerModels.DeviceIdentifier) (map[minerModels.DeviceIdentifier]modelsV2.DeviceMetrics, error) {
	return f.metrics, nil
}

// statsHarness builds a service + handler with the stats deps wired so
// GetSiteStats can be exercised end-to-end against the auth middleware.
type statsHarness struct {
	handler       *Handler
	siteStore     *mocks.MockSiteStore
	buildingStore *mocks.MockBuildingStore
	deviceQueryer *fakeDeviceQueryer
	telemetry     *fakeTelemetryCollector
}

func newStatsHandler(t *testing.T) *statsHarness {
	t.Helper()
	ctrl := gomock.NewController(t)
	siteStore := mocks.NewMockSiteStore(ctrl)
	buildingStore := mocks.NewMockBuildingStore(ctrl)
	tx := mocks.NewMockTransactor(ctrl)
	tx.EXPECT().RunInTx(gomock.Any(), gomock.Any()).AnyTimes().DoAndReturn(
		func(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) },
	)
	deviceQueryer := &fakeDeviceQueryer{}
	telemetry := &fakeTelemetryCollector{}
	svc := sites.NewService(siteStore, buildingStore, deviceQueryer, telemetry, tx, nil)
	return &statsHarness{
		handler:       NewHandler(svc),
		siteStore:     siteStore,
		buildingStore: buildingStore,
		deviceQueryer: deviceQueryer,
		telemetry:     telemetry,
	}
}

func TestHandler_GetSiteStats_requiresSiteRead(t *testing.T) {
	t.Parallel()
	h := newStatsHandler(t)

	// Caller without site:read should be denied before service runs.
	ctx := handlerstest.CtxWithPermissions(t, 7) // no permissions
	_, err := h.handler.GetSiteStats(ctx, connect.NewRequest(&pb.GetSiteStatsRequest{SiteId: 1}))
	require.Error(t, err)
	var ce *connect.Error
	if errors.As(err, &ce) {
		assert.Equal(t, connect.CodePermissionDenied, ce.Code())
	}
}

func TestHandler_GetSiteStats_plumbsResponse(t *testing.T) {
	t.Parallel()
	h := newStatsHandler(t)

	h.siteStore.EXPECT().SiteBelongsToOrg(gomock.Any(), int64(7), int64(1)).Return(true, nil)
	h.buildingStore.EXPECT().ListBuildings(gomock.Any(), gomock.Any()).Return(nil, nil)
	h.deviceQueryer.deviceIDs = nil // empty short-circuit; telemetry never called

	ctx := handlerstest.CtxWithPermissions(t, 7, authz.PermSiteRead, authz.PermFleetRead)
	resp, err := h.handler.GetSiteStats(ctx, connect.NewRequest(&pb.GetSiteStatsRequest{SiteId: 1}))
	require.NoError(t, err)
	assert.Equal(t, int64(1), resp.Msg.GetSiteId())
	assert.Equal(t, int32(0), resp.Msg.GetBuildingCount())
	assert.Equal(t, int32(0), resp.Msg.GetDeviceCount())
	assert.Equal(t, int32(0), resp.Msg.GetReportingCount())
}

func TestHandler_GetSiteStats_propagatesNotFound(t *testing.T) {
	t.Parallel()
	h := newStatsHandler(t)
	h.siteStore.EXPECT().SiteBelongsToOrg(gomock.Any(), int64(7), int64(99)).Return(false, nil)

	ctx := handlerstest.CtxWithPermissions(t, 7, authz.PermSiteRead, authz.PermFleetRead)
	_, err := h.handler.GetSiteStats(ctx, connect.NewRequest(&pb.GetSiteStatsRequest{SiteId: 99}))
	require.Error(t, err)
	var fe fleeterror.FleetError
	if errors.As(err, &fe) {
		assert.Equal(t, connect.CodeNotFound, fe.GRPCCode)
	}
}
