package collection

import (
	"context"
	"testing"

	pb "github.com/proto-at-block/proto-fleet/server/generated/grpc/collection/v1"
	commonpb "github.com/proto-at-block/proto-fleet/server/generated/grpc/common/v1"
	"github.com/proto-at-block/proto-fleet/server/internal/domain/fleeterror"
	minerModels "github.com/proto-at-block/proto-fleet/server/internal/domain/miner/models"
	"github.com/proto-at-block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/proto-at-block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
	modelsV2 "github.com/proto-at-block/proto-fleet/server/internal/domain/telemetry/models/v2"
	"github.com/proto-at-block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

const (
	testOrgID        = int64(1)
	testUserID       = int64(100)
	testCollectionID = int64(42)
)

// mockDeviceQueryer implements DeviceQueryer for tests.
type mockDeviceQueryer struct {
	devicesByFilter         map[int64][]string // collectionID -> device identifiers
	stateCountsByCollection map[int64]interfaces.MinerStateCounts
	err                     error
}

func (m *mockDeviceQueryer) GetDeviceIdentifiersByOrgWithFilter(_ context.Context, _ int64, filter *interfaces.MinerFilter) ([]string, error) {
	if m.err != nil {
		return nil, m.err
	}
	if filter != nil && len(filter.GroupIDs) == 1 {
		return m.devicesByFilter[filter.GroupIDs[0]], nil
	}
	return nil, nil
}

func (m *mockDeviceQueryer) GetMinerStateCountsByCollections(_ context.Context, _ int64, _ []int64) (map[int64]interfaces.MinerStateCounts, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.stateCountsByCollection, nil
}

func newTestService(t *testing.T) (*Service, *mocks.MockCollectionStore, *mocks.MockTransactor) {
	t.Helper()
	ctrl := gomock.NewController(t)

	mockStore := mocks.NewMockCollectionStore(ctrl)
	mockTransactor := mocks.NewMockTransactor(ctrl)

	// Wire up transactor to execute functions immediately
	mockTransactor.EXPECT().RunInTx(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, fn func(context.Context) error) error {
			return fn(ctx)
		},
	).AnyTimes()
	mockTransactor.EXPECT().RunInTxWithResult(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, fn func(context.Context) (any, error)) (any, error) {
			return fn(ctx)
		},
	).AnyTimes()

	noopResolver := func(_ context.Context, _ *commonpb.DeviceSelector, _ int64) ([]string, error) {
		return nil, nil
	}

	svc := NewService(mockStore, &mockDeviceQueryer{}, mockTransactor, noopResolver, nil)
	return svc, mockStore, mockTransactor
}

func testCtx(t *testing.T) context.Context {
	t.Helper()
	return testutil.MockAuthContextForTesting(t.Context(), testUserID, testOrgID)
}

func TestService_CreateCollection_RackRequiresRackInfo(t *testing.T) {
	svc, _, _ := newTestService(t)
	ctx := testCtx(t)

	// Act
	_, err := svc.CreateCollection(ctx, &pb.CreateCollectionRequest{
		Type:  pb.CollectionType_COLLECTION_TYPE_RACK,
		Label: "Rack without info",
	})

	// Assert
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
}

func TestService_CreateCollection_GroupDoesNotRequireRackInfo(t *testing.T) {
	svc, mockStore, _ := newTestService(t)
	ctx := testCtx(t)

	// Arrange
	mockStore.EXPECT().CreateCollection(gomock.Any(), testOrgID, pb.CollectionType_COLLECTION_TYPE_GROUP, "My Group", "desc").
		Return(&pb.DeviceCollection{Id: 1, Label: "My Group", Type: pb.CollectionType_COLLECTION_TYPE_GROUP}, nil)

	// Act
	resp, err := svc.CreateCollection(ctx, &pb.CreateCollectionRequest{
		Type:        pb.CollectionType_COLLECTION_TYPE_GROUP,
		Label:       "My Group",
		Description: "desc",
	})

	// Assert
	require.NoError(t, err)
	assert.Equal(t, "My Group", resp.Collection.Label)
}

func TestService_CreateCollection_RackCreatesExtension(t *testing.T) {
	svc, mockStore, _ := newTestService(t)
	ctx := testCtx(t)

	// Arrange
	rackInfo := &pb.RackInfo{Rows: 4, Columns: 8}
	mockStore.EXPECT().CreateCollection(gomock.Any(), testOrgID, pb.CollectionType_COLLECTION_TYPE_RACK, "Rack A", "").
		Return(&pb.DeviceCollection{Id: 10, Label: "Rack A", Type: pb.CollectionType_COLLECTION_TYPE_RACK}, nil)
	mockStore.EXPECT().CreateRackExtension(gomock.Any(), int64(10), "", int32(4), int32(8)).
		Return(nil)

	// Act
	resp, err := svc.CreateCollection(ctx, &pb.CreateCollectionRequest{
		Type:  pb.CollectionType_COLLECTION_TYPE_RACK,
		Label: "Rack A",
		TypeDetails: &pb.CreateCollectionRequest_RackInfo{
			RackInfo: rackInfo,
		},
	})

	// Assert
	require.NoError(t, err)
	assert.Equal(t, "Rack A", resp.Collection.Label)
	assert.Equal(t, rackInfo, resp.Collection.GetRackInfo())
}

func TestService_DeleteCollection_NotFoundWhenZeroRows(t *testing.T) {
	svc, mockStore, _ := newTestService(t)
	ctx := testCtx(t)

	// Arrange
	mockStore.EXPECT().SoftDeleteCollection(gomock.Any(), testOrgID, testCollectionID).
		Return(int64(0), nil)

	// Act
	_, err := svc.DeleteCollection(ctx, &pb.DeleteCollectionRequest{
		CollectionId: testCollectionID,
	})

	// Assert
	require.Error(t, err)
	assert.True(t, fleeterror.IsNotFoundError(err))
}

func TestService_AddDevicesToCollection_NotFoundWhenNotOwnedByOrg(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockStore := mocks.NewMockCollectionStore(ctrl)
	mockTransactor := mocks.NewMockTransactor(ctrl)
	mockTransactor.EXPECT().RunInTxWithResult(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, fn func(context.Context) (any, error)) (any, error) {
			return fn(ctx)
		},
	)
	ctx := testCtx(t)

	// Arrange - resolver returns device IDs, but collection doesn't belong to org
	resolver := func(_ context.Context, _ *commonpb.DeviceSelector, _ int64) ([]string, error) {
		return []string{"device-1"}, nil
	}
	svc := NewService(mockStore, &mockDeviceQueryer{}, mockTransactor, resolver, nil)

	mockStore.EXPECT().CollectionBelongsToOrg(gomock.Any(), testCollectionID, testOrgID).
		Return(false, nil)

	// Act
	_, err := svc.AddDevicesToCollection(ctx, &pb.AddDevicesToCollectionRequest{
		CollectionId: testCollectionID,
		DeviceSelector: &commonpb.DeviceSelector{
			SelectionType: &commonpb.DeviceSelector_DeviceList{
				DeviceList: &commonpb.DeviceIdentifierList{DeviceIdentifiers: []string{"device-1"}},
			},
		},
	})

	// Assert
	require.Error(t, err)
	assert.True(t, fleeterror.IsNotFoundError(err))
}

func TestService_SetRackSlotPosition_RequiresPosition(t *testing.T) {
	svc, _, _ := newTestService(t)
	ctx := testCtx(t)

	// Act
	_, err := svc.SetRackSlotPosition(ctx, &pb.SetRackSlotPositionRequest{
		CollectionId:     testCollectionID,
		DeviceIdentifier: "device-1",
		Position:         nil,
	})

	// Assert
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
}

func TestService_SetRackSlotPosition_RejectsGroupCollection(t *testing.T) {
	svc, mockStore, _ := newTestService(t)
	ctx := testCtx(t)

	// Arrange
	mockStore.EXPECT().GetCollectionType(gomock.Any(), testOrgID, testCollectionID).
		Return(pb.CollectionType_COLLECTION_TYPE_GROUP, nil)

	// Act
	_, err := svc.SetRackSlotPosition(ctx, &pb.SetRackSlotPositionRequest{
		CollectionId:     testCollectionID,
		DeviceIdentifier: "device-1",
		Position:         &pb.RackSlotPosition{Row: 0, Column: 0},
	})

	// Assert
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
}

func TestService_ClearRackSlotPosition_RejectsGroupCollection(t *testing.T) {
	svc, mockStore, _ := newTestService(t)
	ctx := testCtx(t)

	// Arrange
	mockStore.EXPECT().GetCollectionType(gomock.Any(), testOrgID, testCollectionID).
		Return(pb.CollectionType_COLLECTION_TYPE_GROUP, nil)

	// Act
	_, err := svc.ClearRackSlotPosition(ctx, &pb.ClearRackSlotPositionRequest{
		CollectionId:     testCollectionID,
		DeviceIdentifier: "device-1",
	})

	// Assert
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
}

func TestService_GetRackSlots_RejectsGroupCollection(t *testing.T) {
	svc, mockStore, _ := newTestService(t)
	ctx := testCtx(t)

	// Arrange
	mockStore.EXPECT().GetCollectionType(gomock.Any(), testOrgID, testCollectionID).
		Return(pb.CollectionType_COLLECTION_TYPE_GROUP, nil)

	// Act
	_, err := svc.GetRackSlots(ctx, &pb.GetRackSlotsRequest{
		CollectionId: testCollectionID,
	})

	// Assert
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
}

func TestService_AddDevicesToCollection_ResolverError(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockStore := mocks.NewMockCollectionStore(ctrl)
	mockTransactor := mocks.NewMockTransactor(ctrl)
	ctx := testCtx(t)

	// Arrange - resolver fails (e.g. device not owned by org)
	resolver := func(_ context.Context, _ *commonpb.DeviceSelector, _ int64) ([]string, error) {
		return nil, fleeterror.NewForbiddenError("access denied")
	}
	svc := NewService(mockStore, &mockDeviceQueryer{}, mockTransactor, resolver, nil)

	// Act
	_, err := svc.AddDevicesToCollection(ctx, &pb.AddDevicesToCollectionRequest{
		CollectionId: testCollectionID,
		DeviceSelector: &commonpb.DeviceSelector{
			SelectionType: &commonpb.DeviceSelector_DeviceList{
				DeviceList: &commonpb.DeviceIdentifierList{DeviceIdentifiers: []string{"device-1"}},
			},
		},
	})

	// Assert - error from resolver is propagated, store is never called
	require.Error(t, err)
	assert.Contains(t, err.Error(), "access denied")
}

func TestService_CreateCollection_WithDeviceSelectorAddsDevicesAtomically(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockStore := mocks.NewMockCollectionStore(ctrl)
	mockTransactor := mocks.NewMockTransactor(ctrl)
	ctx := testCtx(t)

	// Arrange - resolver returns device IDs
	deviceIDs := []string{"device-1", "device-2", "device-3"}
	resolver := func(_ context.Context, _ *commonpb.DeviceSelector, _ int64) ([]string, error) {
		return deviceIDs, nil
	}
	svc := NewService(mockStore, &mockDeviceQueryer{}, mockTransactor, resolver, nil)

	mockTransactor.EXPECT().RunInTxWithResult(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, fn func(context.Context) (any, error)) (any, error) {
			return fn(ctx)
		},
	)

	mockStore.EXPECT().CreateCollection(gomock.Any(), testOrgID, pb.CollectionType_COLLECTION_TYPE_GROUP, "Group with devices", "").
		Return(&pb.DeviceCollection{Id: 99, Label: "Group with devices", Type: pb.CollectionType_COLLECTION_TYPE_GROUP}, nil)

	mockStore.EXPECT().AddDevicesToCollection(gomock.Any(), testOrgID, int64(99), deviceIDs).
		Return(int64(3), nil)

	// Act
	resp, err := svc.CreateCollection(ctx, &pb.CreateCollectionRequest{
		Type:  pb.CollectionType_COLLECTION_TYPE_GROUP,
		Label: "Group with devices",
		DeviceSelector: &commonpb.DeviceSelector{
			SelectionType: &commonpb.DeviceSelector_DeviceList{
				DeviceList: &commonpb.DeviceIdentifierList{DeviceIdentifiers: deviceIDs},
			},
		},
	})

	// Assert
	require.NoError(t, err)
	assert.Equal(t, "Group with devices", resp.Collection.Label)
	assert.Equal(t, int32(3), resp.AddedCount)
	assert.Equal(t, int32(3), resp.Collection.DeviceCount)
}

func TestService_CreateCollection_WithDeviceSelectorResolverError(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockStore := mocks.NewMockCollectionStore(ctrl)
	mockTransactor := mocks.NewMockTransactor(ctrl)
	ctx := testCtx(t)

	// Arrange - resolver fails
	resolver := func(_ context.Context, _ *commonpb.DeviceSelector, _ int64) ([]string, error) {
		return nil, fleeterror.NewForbiddenError("device not owned by org")
	}
	svc := NewService(mockStore, &mockDeviceQueryer{}, mockTransactor, resolver, nil)

	// Act
	_, err := svc.CreateCollection(ctx, &pb.CreateCollectionRequest{
		Type:  pb.CollectionType_COLLECTION_TYPE_GROUP,
		Label: "Group with devices",
		DeviceSelector: &commonpb.DeviceSelector{
			SelectionType: &commonpb.DeviceSelector_DeviceList{
				DeviceList: &commonpb.DeviceIdentifierList{DeviceIdentifiers: []string{"device-1"}},
			},
		},
	})

	// Assert - error from resolver is propagated, collection is never created
	require.Error(t, err)
	assert.Contains(t, err.Error(), "device not owned by org")
}

func TestService_UpdateCollection_WithDeviceSelectorReplacesMembers(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockStore := mocks.NewMockCollectionStore(ctrl)
	mockTransactor := mocks.NewMockTransactor(ctrl)
	ctx := testCtx(t)

	deviceIDs := []string{"device-1", "device-2"}
	resolver := func(_ context.Context, _ *commonpb.DeviceSelector, _ int64) ([]string, error) {
		return deviceIDs, nil
	}
	svc := NewService(mockStore, &mockDeviceQueryer{}, mockTransactor, resolver, nil)

	mockTransactor.EXPECT().RunInTxWithResult(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, fn func(context.Context) (any, error)) (any, error) {
			return fn(ctx)
		},
	)

	newLabel := "Updated Group"
	mockStore.EXPECT().UpdateCollection(gomock.Any(), testOrgID, testCollectionID, &newLabel, (*string)(nil)).Return(nil)
	mockStore.EXPECT().RemoveAllDevicesFromCollection(gomock.Any(), testOrgID, testCollectionID).Return(int64(3), nil)
	mockStore.EXPECT().AddDevicesToCollection(gomock.Any(), testOrgID, testCollectionID, deviceIDs).Return(int64(2), nil)
	mockStore.EXPECT().GetCollection(gomock.Any(), testOrgID, testCollectionID).
		Return(&pb.DeviceCollection{Id: testCollectionID, Label: newLabel, DeviceCount: 2}, nil)

	resp, err := svc.UpdateCollection(ctx, &pb.UpdateCollectionRequest{
		CollectionId: testCollectionID,
		Label:        &newLabel,
		DeviceSelector: &commonpb.DeviceSelector{
			SelectionType: &commonpb.DeviceSelector_DeviceList{
				DeviceList: &commonpb.DeviceIdentifierList{DeviceIdentifiers: deviceIDs},
			},
		},
	})

	require.NoError(t, err)
	assert.Equal(t, newLabel, resp.Collection.Label)
	assert.Equal(t, int32(2), resp.Collection.DeviceCount)
}

func TestService_UpdateCollection_WithEmptyDeviceSelectorRemovesAllMembers(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockStore := mocks.NewMockCollectionStore(ctrl)
	mockTransactor := mocks.NewMockTransactor(ctrl)
	ctx := testCtx(t)

	resolver := func(_ context.Context, _ *commonpb.DeviceSelector, _ int64) ([]string, error) {
		return []string{}, nil
	}
	svc := NewService(mockStore, &mockDeviceQueryer{}, mockTransactor, resolver, nil)

	mockTransactor.EXPECT().RunInTxWithResult(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, fn func(context.Context) (any, error)) (any, error) {
			return fn(ctx)
		},
	)

	mockStore.EXPECT().UpdateCollection(gomock.Any(), testOrgID, testCollectionID, (*string)(nil), (*string)(nil)).Return(nil)
	mockStore.EXPECT().RemoveAllDevicesFromCollection(gomock.Any(), testOrgID, testCollectionID).Return(int64(5), nil)
	// AddDevicesToCollection should NOT be called since deviceIdentifiers is empty
	mockStore.EXPECT().GetCollection(gomock.Any(), testOrgID, testCollectionID).
		Return(&pb.DeviceCollection{Id: testCollectionID, Label: "My Group", DeviceCount: 0}, nil)

	resp, err := svc.UpdateCollection(ctx, &pb.UpdateCollectionRequest{
		CollectionId: testCollectionID,
		DeviceSelector: &commonpb.DeviceSelector{
			SelectionType: &commonpb.DeviceSelector_DeviceList{
				DeviceList: &commonpb.DeviceIdentifierList{DeviceIdentifiers: []string{}},
			},
		},
	})

	require.NoError(t, err)
	assert.Equal(t, int32(0), resp.Collection.DeviceCount)
}

func TestService_UpdateCollection_WithoutDeviceSelectorLeavesMembers(t *testing.T) {
	svc, mockStore, _ := newTestService(t)
	ctx := testCtx(t)

	newLabel := "Renamed Group"
	mockStore.EXPECT().UpdateCollection(gomock.Any(), testOrgID, testCollectionID, &newLabel, (*string)(nil)).Return(nil)
	mockStore.EXPECT().GetCollection(gomock.Any(), testOrgID, testCollectionID).
		Return(&pb.DeviceCollection{Id: testCollectionID, Label: newLabel, DeviceCount: 3}, nil)

	resp, err := svc.UpdateCollection(ctx, &pb.UpdateCollectionRequest{
		CollectionId: testCollectionID,
		Label:        &newLabel,
	})

	require.NoError(t, err)
	assert.Equal(t, newLabel, resp.Collection.Label)
	assert.Equal(t, int32(3), resp.Collection.DeviceCount)
}

// mockTelemetryCollector implements TelemetryCollector for tests.
type mockTelemetryCollector struct {
	metrics map[minerModels.DeviceIdentifier]modelsV2.DeviceMetrics
	err     error
}

func (m *mockTelemetryCollector) GetLatestDeviceMetrics(_ context.Context, _ []minerModels.DeviceIdentifier) (map[minerModels.DeviceIdentifier]modelsV2.DeviceMetrics, error) {
	return m.metrics, m.err
}

func newTestServiceWithTelemetry(t *testing.T, telemetry TelemetryCollector, deviceQ DeviceQueryer) (*Service, *mocks.MockCollectionStore) {
	t.Helper()
	ctrl := gomock.NewController(t)
	mockStore := mocks.NewMockCollectionStore(ctrl)
	mockTransactor := mocks.NewMockTransactor(ctrl)
	mockTransactor.EXPECT().RunInTx(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) },
	).AnyTimes()
	mockTransactor.EXPECT().RunInTxWithResult(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, fn func(context.Context) (any, error)) (any, error) { return fn(ctx) },
	).AnyTimes()
	noopResolver := func(_ context.Context, _ *commonpb.DeviceSelector, _ int64) ([]string, error) {
		return nil, nil
	}
	svc := NewService(mockStore, deviceQ, mockTransactor, noopResolver, telemetry)
	return svc, mockStore
}

func TestService_GetCollectionStats_EmptyRequest(t *testing.T) {
	svc, _ := newTestServiceWithTelemetry(t, nil, &mockDeviceQueryer{})
	ctx := testCtx(t)

	resp, err := svc.GetCollectionStats(ctx, &pb.GetCollectionStatsRequest{})
	require.NoError(t, err)
	assert.Empty(t, resp.Stats)
}

func TestService_GetCollectionStats_EmptyCollection(t *testing.T) {
	deviceQ := &mockDeviceQueryer{
		devicesByFilter:         map[int64][]string{testCollectionID: {}},
		stateCountsByCollection: map[int64]interfaces.MinerStateCounts{testCollectionID: {}},
	}
	svc, _ := newTestServiceWithTelemetry(t, &mockTelemetryCollector{
		metrics: map[minerModels.DeviceIdentifier]modelsV2.DeviceMetrics{},
	}, deviceQ)
	ctx := testCtx(t)

	resp, err := svc.GetCollectionStats(ctx, &pb.GetCollectionStatsRequest{
		CollectionIds: []int64{testCollectionID},
	})
	require.NoError(t, err)
	require.Len(t, resp.Stats, 1)

	cs := resp.Stats[0]
	assert.Equal(t, testCollectionID, cs.CollectionId)
	assert.Equal(t, int32(0), cs.DeviceCount)
	assert.Equal(t, int32(0), cs.ReportingCount)
	assert.Equal(t, float64(0), cs.TotalHashrateThs)
}

func TestService_GetCollectionStats_NilTelemetry(t *testing.T) {
	deviceQ := &mockDeviceQueryer{
		devicesByFilter: map[int64][]string{testCollectionID: {"dev-1", "dev-2"}},
		stateCountsByCollection: map[int64]interfaces.MinerStateCounts{
			testCollectionID: {HashingCount: 1, OfflineCount: 1},
		},
	}
	svc, _ := newTestServiceWithTelemetry(t, nil, deviceQ)
	ctx := testCtx(t)

	resp, err := svc.GetCollectionStats(ctx, &pb.GetCollectionStatsRequest{
		CollectionIds: []int64{testCollectionID},
	})
	require.NoError(t, err)
	require.Len(t, resp.Stats, 1)

	cs := resp.Stats[0]
	assert.Equal(t, int32(2), cs.DeviceCount)
	assert.Equal(t, int32(1), cs.HashingCount)
	assert.Equal(t, int32(1), cs.OfflineCount)
	// Telemetry fields should be zero since telemetry is nil
	assert.Equal(t, int32(0), cs.ReportingCount)
	assert.Equal(t, float64(0), cs.TotalHashrateThs)
}

func TestService_GetCollectionStats_MixedMetrics(t *testing.T) {
	collID := int64(10)
	deviceQ := &mockDeviceQueryer{
		devicesByFilter: map[int64][]string{collID: {"dev-1", "dev-2", "dev-3"}},
		stateCountsByCollection: map[int64]interfaces.MinerStateCounts{
			collID: {HashingCount: 2, BrokenCount: 0, OfflineCount: 1, SleepingCount: 0},
		},
	}
	telemetry := &mockTelemetryCollector{
		metrics: map[minerModels.DeviceIdentifier]modelsV2.DeviceMetrics{
			"dev-1": {
				HashrateHS:   &modelsV2.MetricValue{Value: 100e12}, // 100 TH/s
				PowerW:       &modelsV2.MetricValue{Value: 3000},   // 3 kW
				EfficiencyJH: &modelsV2.MetricValue{Value: 30e-12}, // 30 J/TH
				TempC:        &modelsV2.MetricValue{Value: 65},
			},
			"dev-2": {
				HashrateHS: &modelsV2.MetricValue{Value: 50e12}, // 50 TH/s
				TempC:      &modelsV2.MetricValue{Value: 72},
				// No power or efficiency for this device
			},
			// dev-3 has no telemetry at all (not in map)
		},
	}
	svc, _ := newTestServiceWithTelemetry(t, telemetry, deviceQ)
	ctx := testCtx(t)

	resp, err := svc.GetCollectionStats(ctx, &pb.GetCollectionStatsRequest{
		CollectionIds: []int64{collID},
	})
	require.NoError(t, err)
	require.Len(t, resp.Stats, 1)

	cs := resp.Stats[0]
	assert.Equal(t, int32(3), cs.DeviceCount)
	assert.Equal(t, int32(2), cs.ReportingCount)
	assert.Equal(t, int32(2), cs.HashingCount)
	assert.Equal(t, int32(1), cs.OfflineCount)

	// Hashrate: (100e12 + 50e12) / 1e12 = 150 TH/s
	assert.InDelta(t, 150.0, cs.TotalHashrateThs, 0.01)

	// Power: 3000 / 1000 = 3 kW (only dev-1)
	assert.InDelta(t, 3.0, cs.TotalPowerKw, 0.01)

	// Efficiency: 30e-12 * 1e12 = 30 J/TH (only dev-1)
	assert.InDelta(t, 30.0, cs.AvgEfficiencyJth, 0.01)

	// Temperature: min=65, max=72
	assert.InDelta(t, 65.0, cs.MinTemperatureC, 0.01)
	assert.InDelta(t, 72.0, cs.MaxTemperatureC, 0.01)
}

func TestService_GetCollectionStats_MultipleCollections(t *testing.T) {
	deviceQ := &mockDeviceQueryer{
		devicesByFilter: map[int64][]string{1: {"dev-1"}, 2: {"dev-2"}},
		stateCountsByCollection: map[int64]interfaces.MinerStateCounts{
			1: {HashingCount: 1},
			2: {HashingCount: 1},
		},
	}
	telemetry := &mockTelemetryCollector{
		metrics: map[minerModels.DeviceIdentifier]modelsV2.DeviceMetrics{
			"dev-1": {HashrateHS: &modelsV2.MetricValue{Value: 80e12}},
			"dev-2": {HashrateHS: &modelsV2.MetricValue{Value: 60e12}},
		},
	}
	svc, _ := newTestServiceWithTelemetry(t, telemetry, deviceQ)
	ctx := testCtx(t)

	resp, err := svc.GetCollectionStats(ctx, &pb.GetCollectionStatsRequest{CollectionIds: []int64{1, 2}})
	require.NoError(t, err)
	require.Len(t, resp.Stats, 2)

	assert.Equal(t, int64(1), resp.Stats[0].CollectionId)
	assert.InDelta(t, 80.0, resp.Stats[0].TotalHashrateThs, 0.01)
	assert.Equal(t, int64(2), resp.Stats[1].CollectionId)
	assert.InDelta(t, 60.0, resp.Stats[1].TotalHashrateThs, 0.01)
}

func TestService_CreateCollection_WithAllDevicesSelector(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockStore := mocks.NewMockCollectionStore(ctrl)
	mockTransactor := mocks.NewMockTransactor(ctrl)
	ctx := testCtx(t)

	// Arrange - resolver returns all devices for the org
	allDevices := []string{"device-1", "device-2", "device-3", "device-4", "device-5"}
	resolver := func(_ context.Context, selector *commonpb.DeviceSelector, _ int64) ([]string, error) {
		if selector.GetAllDevices() {
			return allDevices, nil
		}
		return nil, nil
	}
	svc := NewService(mockStore, &mockDeviceQueryer{}, mockTransactor, resolver, nil)

	mockTransactor.EXPECT().RunInTxWithResult(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, fn func(context.Context) (any, error)) (any, error) {
			return fn(ctx)
		},
	)

	mockStore.EXPECT().CreateCollection(gomock.Any(), testOrgID, pb.CollectionType_COLLECTION_TYPE_GROUP, "All devices group", "").
		Return(&pb.DeviceCollection{Id: 100, Label: "All devices group", Type: pb.CollectionType_COLLECTION_TYPE_GROUP}, nil)

	mockStore.EXPECT().AddDevicesToCollection(gomock.Any(), testOrgID, int64(100), allDevices).
		Return(int64(5), nil)

	// Act
	resp, err := svc.CreateCollection(ctx, &pb.CreateCollectionRequest{
		Type:  pb.CollectionType_COLLECTION_TYPE_GROUP,
		Label: "All devices group",
		DeviceSelector: &commonpb.DeviceSelector{
			SelectionType: &commonpb.DeviceSelector_AllDevices{AllDevices: true},
		},
	})

	// Assert
	require.NoError(t, err)
	assert.Equal(t, int32(5), resp.AddedCount)
	assert.Equal(t, int32(5), resp.Collection.DeviceCount)
}
