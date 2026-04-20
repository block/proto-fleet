package collection

import (
	"context"
	"testing"

	pb "github.com/block/proto-fleet/server/generated/grpc/collection/v1"
	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	"github.com/block/proto-fleet/server/internal/domain/activity"
	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	minerModels "github.com/block/proto-fleet/server/internal/domain/miner/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
	modelsV2 "github.com/block/proto-fleet/server/internal/domain/telemetry/models/v2"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

const (
	testOrgID        = int64(1)
	testUserID       = int64(100)
	testCollectionID = int64(42)
)

func newStubActivityService(ctrl *gomock.Controller) *activity.Service {
	mockActivityStore := mocks.NewMockActivityStore(ctrl)
	mockActivityStore.EXPECT().Insert(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	return activity.NewService(mockActivityStore)
}

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
	if filter != nil && len(filter.RackIDs) == 1 {
		return m.devicesByFilter[filter.RackIDs[0]], nil
	}
	return nil, nil
}

func (m *mockDeviceQueryer) GetMinerStateCountsByCollections(_ context.Context, _ int64, _ []int64) (map[int64]interfaces.MinerStateCounts, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.stateCountsByCollection, nil
}

func (m *mockDeviceQueryer) GetComponentErrorCountsByCollections(_ context.Context, _ int64, _ []int64) ([]interfaces.ComponentErrorCount, error) {
	if m.err != nil {
		return nil, m.err
	}
	return nil, nil
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

	svc := NewService(mockStore, &mockDeviceQueryer{}, mockTransactor, noopResolver, nil, newStubActivityService(ctrl))
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
	loc := "Building A"
	rackInfo := &pb.RackInfo{Rows: 4, Columns: 8, Zone: loc, OrderIndex: pb.RackOrderIndex_RACK_ORDER_INDEX_BOTTOM_LEFT, CoolingType: pb.RackCoolingType_RACK_COOLING_TYPE_AIR}
	mockStore.EXPECT().CreateCollection(gomock.Any(), testOrgID, pb.CollectionType_COLLECTION_TYPE_RACK, "Rack A", "").
		Return(&pb.DeviceCollection{Id: 10, Label: "Rack A", Type: pb.CollectionType_COLLECTION_TYPE_RACK}, nil)
	mockStore.EXPECT().CreateRackExtension(gomock.Any(), int64(10), "Building A", int32(4), int32(8), int32(pb.RackOrderIndex_RACK_ORDER_INDEX_BOTTOM_LEFT), int32(pb.RackCoolingType_RACK_COOLING_TYPE_AIR), testOrgID).
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

func TestService_GetCollection_RackPopulatesTypeDetails(t *testing.T) {
	svc, mockStore, _ := newTestService(t)
	ctx := testCtx(t)

	loc := "Building A"
	mockStore.EXPECT().GetCollection(gomock.Any(), testOrgID, testCollectionID).
		Return(&pb.DeviceCollection{Id: testCollectionID, Label: "Rack A", Type: pb.CollectionType_COLLECTION_TYPE_RACK}, nil)
	mockStore.EXPECT().GetRackInfo(gomock.Any(), testCollectionID, testOrgID).
		Return(&pb.RackInfo{Rows: 4, Columns: 8, Zone: loc, OrderIndex: pb.RackOrderIndex_RACK_ORDER_INDEX_BOTTOM_LEFT, CoolingType: pb.RackCoolingType_RACK_COOLING_TYPE_AIR}, nil)

	resp, err := svc.GetCollection(ctx, &pb.GetCollectionRequest{CollectionId: testCollectionID})

	require.NoError(t, err)
	assert.NotNil(t, resp.Collection.GetRackInfo())
	assert.Equal(t, int32(4), resp.Collection.GetRackInfo().Rows)
	assert.Equal(t, int32(8), resp.Collection.GetRackInfo().Columns)
	assert.Equal(t, "Building A", resp.Collection.GetRackInfo().GetZone())
}

func TestService_GetCollection_GroupDoesNotFetchRackInfo(t *testing.T) {
	svc, mockStore, _ := newTestService(t)
	ctx := testCtx(t)

	mockStore.EXPECT().GetCollection(gomock.Any(), testOrgID, testCollectionID).
		Return(&pb.DeviceCollection{Id: testCollectionID, Label: "My Group", Type: pb.CollectionType_COLLECTION_TYPE_GROUP}, nil)
	// GetRackInfo should NOT be called for group collections

	resp, err := svc.GetCollection(ctx, &pb.GetCollectionRequest{CollectionId: testCollectionID})

	require.NoError(t, err)
	assert.Nil(t, resp.Collection.GetRackInfo())
}

func TestService_CreateCollection_RackRejectsUnspecifiedOrderIndex(t *testing.T) {
	svc, _, _ := newTestService(t)
	ctx := testCtx(t)

	loc := "Building A"
	_, err := svc.CreateCollection(ctx, &pb.CreateCollectionRequest{
		Type:  pb.CollectionType_COLLECTION_TYPE_RACK,
		Label: "Rack A",
		TypeDetails: &pb.CreateCollectionRequest_RackInfo{
			RackInfo: &pb.RackInfo{Rows: 4, Columns: 8, Zone: loc, OrderIndex: pb.RackOrderIndex_RACK_ORDER_INDEX_UNSPECIFIED, CoolingType: pb.RackCoolingType_RACK_COOLING_TYPE_AIR},
		},
	})

	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
	assert.Contains(t, err.Error(), "order_index")
}

func TestService_CreateCollection_RackRejectsUnspecifiedCoolingType(t *testing.T) {
	svc, _, _ := newTestService(t)
	ctx := testCtx(t)

	loc := "Building A"
	_, err := svc.CreateCollection(ctx, &pb.CreateCollectionRequest{
		Type:  pb.CollectionType_COLLECTION_TYPE_RACK,
		Label: "Rack A",
		TypeDetails: &pb.CreateCollectionRequest_RackInfo{
			RackInfo: &pb.RackInfo{Rows: 4, Columns: 8, Zone: loc, OrderIndex: pb.RackOrderIndex_RACK_ORDER_INDEX_BOTTOM_LEFT, CoolingType: pb.RackCoolingType_RACK_COOLING_TYPE_UNSPECIFIED},
		},
	})

	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
	assert.Contains(t, err.Error(), "cooling_type")
}

func TestService_DeleteCollection_NotFoundWhenZeroRows(t *testing.T) {
	svc, mockStore, _ := newTestService(t)
	ctx := testCtx(t)

	// Arrange
	mockStore.EXPECT().GetCollection(gomock.Any(), testOrgID, testCollectionID).
		Return(&pb.DeviceCollection{Id: testCollectionID, Label: "gone", Type: pb.CollectionType_COLLECTION_TYPE_GROUP}, nil)
	mockStore.EXPECT().RemoveAllDevicesFromCollection(gomock.Any(), testOrgID, testCollectionID).
		Return(int64(0), nil)
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
	svc := NewService(mockStore, &mockDeviceQueryer{}, mockTransactor, resolver, nil, newStubActivityService(ctrl))

	mockStore.EXPECT().GetCollection(gomock.Any(), testOrgID, testCollectionID).
		Return(nil, fleeterror.NewNotFoundErrorf("collection not found"))

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
	mockStore.EXPECT().GetCollection(gomock.Any(), testOrgID, testCollectionID).
		Return(&pb.DeviceCollection{Id: testCollectionID, Type: pb.CollectionType_COLLECTION_TYPE_GROUP, Label: "test"}, nil)

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
	mockStore.EXPECT().GetCollection(gomock.Any(), testOrgID, testCollectionID).
		Return(&pb.DeviceCollection{Id: testCollectionID, Type: pb.CollectionType_COLLECTION_TYPE_GROUP, Label: "test"}, nil)

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
	svc := NewService(mockStore, &mockDeviceQueryer{}, mockTransactor, resolver, nil, newStubActivityService(ctrl))

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
	svc := NewService(mockStore, &mockDeviceQueryer{}, mockTransactor, resolver, nil, newStubActivityService(ctrl))

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
	svc := NewService(mockStore, &mockDeviceQueryer{}, mockTransactor, resolver, nil, newStubActivityService(ctrl))

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
	svc := NewService(mockStore, &mockDeviceQueryer{}, mockTransactor, resolver, nil, newStubActivityService(ctrl))

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
	svc := NewService(mockStore, &mockDeviceQueryer{}, mockTransactor, resolver, nil, newStubActivityService(ctrl))

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
	svc := NewService(mockStore, deviceQ, mockTransactor, noopResolver, telemetry, newStubActivityService(ctrl))
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
	svc, mockStore := newTestServiceWithTelemetry(t, &mockTelemetryCollector{
		metrics: map[minerModels.DeviceIdentifier]modelsV2.DeviceMetrics{},
	}, deviceQ)
	ctx := testCtx(t)

	mockStore.EXPECT().GetCollectionTypes(gomock.Any(), testOrgID, []int64{testCollectionID}).
		Return(map[int64]pb.CollectionType{testCollectionID: pb.CollectionType_COLLECTION_TYPE_GROUP}, nil)

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
	svc, mockStore := newTestServiceWithTelemetry(t, nil, deviceQ)
	ctx := testCtx(t)

	mockStore.EXPECT().GetCollectionTypes(gomock.Any(), testOrgID, []int64{testCollectionID}).
		Return(map[int64]pb.CollectionType{testCollectionID: pb.CollectionType_COLLECTION_TYPE_GROUP}, nil)

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
	svc, mockStore := newTestServiceWithTelemetry(t, telemetry, deviceQ)
	ctx := testCtx(t)

	mockStore.EXPECT().GetCollectionTypes(gomock.Any(), testOrgID, []int64{collID}).
		Return(map[int64]pb.CollectionType{collID: pb.CollectionType_COLLECTION_TYPE_GROUP}, nil)

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
	svc, mockStore := newTestServiceWithTelemetry(t, telemetry, deviceQ)
	ctx := testCtx(t)

	mockStore.EXPECT().GetCollectionTypes(gomock.Any(), testOrgID, []int64{1, 2}).
		Return(map[int64]pb.CollectionType{
			1: pb.CollectionType_COLLECTION_TYPE_GROUP,
			2: pb.CollectionType_COLLECTION_TYPE_GROUP,
		}, nil)

	resp, err := svc.GetCollectionStats(ctx, &pb.GetCollectionStatsRequest{CollectionIds: []int64{1, 2}})
	require.NoError(t, err)
	require.Len(t, resp.Stats, 2)

	assert.Equal(t, int64(1), resp.Stats[0].CollectionId)
	assert.InDelta(t, 80.0, resp.Stats[0].TotalHashrateThs, 0.01)
	assert.Equal(t, int64(2), resp.Stats[1].CollectionId)
	assert.InDelta(t, 60.0, resp.Stats[1].TotalHashrateThs, 0.01)
}

func TestService_GetCollectionStats_RackSlotStatuses(t *testing.T) {
	rackID := int64(10)
	deviceQ := &mockDeviceQueryer{
		devicesByFilter:         map[int64][]string{rackID: {"dev-1"}},
		stateCountsByCollection: map[int64]interfaces.MinerStateCounts{rackID: {HashingCount: 1}},
	}
	telemetry := &mockTelemetryCollector{
		metrics: map[minerModels.DeviceIdentifier]modelsV2.DeviceMetrics{
			"dev-1": {HashrateHS: &modelsV2.MetricValue{Value: 50e12}},
		},
	}
	svc, mockStore := newTestServiceWithTelemetry(t, telemetry, deviceQ)
	ctx := testCtx(t)

	mockStore.EXPECT().GetCollectionTypes(gomock.Any(), testOrgID, []int64{rackID}).
		Return(map[int64]pb.CollectionType{rackID: pb.CollectionType_COLLECTION_TYPE_RACK}, nil)

	expectedSlots := []*pb.RackSlotStatus{
		{Row: 0, Column: 0, Status: pb.SlotDeviceStatus_SLOT_DEVICE_STATUS_HEALTHY},
		{Row: 0, Column: 1, Status: pb.SlotDeviceStatus_SLOT_DEVICE_STATUS_EMPTY},
		{Row: 1, Column: 0, Status: pb.SlotDeviceStatus_SLOT_DEVICE_STATUS_OFFLINE},
		{Row: 1, Column: 1, Status: pb.SlotDeviceStatus_SLOT_DEVICE_STATUS_EMPTY},
	}
	mockStore.EXPECT().GetRackSlotStatuses(gomock.Any(), testOrgID, []int64{rackID}).
		Return(map[int64][]*pb.RackSlotStatus{rackID: expectedSlots}, nil)

	resp, err := svc.GetCollectionStats(ctx, &pb.GetCollectionStatsRequest{CollectionIds: []int64{rackID}})
	require.NoError(t, err)
	require.Len(t, resp.Stats, 1)

	cs := resp.Stats[0]
	assert.Equal(t, rackID, cs.CollectionId)
	assert.Equal(t, int32(1), cs.HashingCount)
	require.Len(t, cs.SlotStatuses, 4)
	assert.Equal(t, pb.SlotDeviceStatus_SLOT_DEVICE_STATUS_HEALTHY, cs.SlotStatuses[0].Status)
	assert.Equal(t, pb.SlotDeviceStatus_SLOT_DEVICE_STATUS_EMPTY, cs.SlotStatuses[1].Status)
	assert.Equal(t, pb.SlotDeviceStatus_SLOT_DEVICE_STATUS_OFFLINE, cs.SlotStatuses[2].Status)
	assert.Equal(t, pb.SlotDeviceStatus_SLOT_DEVICE_STATUS_EMPTY, cs.SlotStatuses[3].Status)
}

func TestService_GetCollectionStats_GroupHasNoSlotStatuses(t *testing.T) {
	groupID := int64(20)
	deviceQ := &mockDeviceQueryer{
		devicesByFilter:         map[int64][]string{groupID: {"dev-1"}},
		stateCountsByCollection: map[int64]interfaces.MinerStateCounts{groupID: {HashingCount: 1}},
	}
	svc, mockStore := newTestServiceWithTelemetry(t, nil, deviceQ)
	ctx := testCtx(t)

	mockStore.EXPECT().GetCollectionTypes(gomock.Any(), testOrgID, []int64{groupID}).
		Return(map[int64]pb.CollectionType{groupID: pb.CollectionType_COLLECTION_TYPE_GROUP}, nil)

	resp, err := svc.GetCollectionStats(ctx, &pb.GetCollectionStatsRequest{CollectionIds: []int64{groupID}})
	require.NoError(t, err)
	require.Len(t, resp.Stats, 1)

	cs := resp.Stats[0]
	assert.Empty(t, cs.SlotStatuses)
}

func TestService_GetCollectionStats_RackSlotStatusesError(t *testing.T) {
	rackID := int64(10)
	deviceQ := &mockDeviceQueryer{
		devicesByFilter:         map[int64][]string{rackID: {"dev-1"}},
		stateCountsByCollection: map[int64]interfaces.MinerStateCounts{rackID: {HashingCount: 1}},
	}
	svc, mockStore := newTestServiceWithTelemetry(t, nil, deviceQ)
	ctx := testCtx(t)

	mockStore.EXPECT().GetCollectionTypes(gomock.Any(), testOrgID, []int64{rackID}).
		Return(map[int64]pb.CollectionType{rackID: pb.CollectionType_COLLECTION_TYPE_RACK}, nil)

	mockStore.EXPECT().GetRackSlotStatuses(gomock.Any(), testOrgID, []int64{rackID}).
		Return(nil, fleeterror.NewInternalError("database connection lost"))

	_, err := svc.GetCollectionStats(ctx, &pb.GetCollectionStatsRequest{CollectionIds: []int64{rackID}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "database connection lost")
}

func TestService_GetCollectionStats_MixedRackAndGroup(t *testing.T) {
	rackID := int64(10)
	groupID := int64(20)
	deviceQ := &mockDeviceQueryer{
		devicesByFilter: map[int64][]string{
			rackID:  {"dev-1"},
			groupID: {"dev-2"},
		},
		stateCountsByCollection: map[int64]interfaces.MinerStateCounts{
			rackID:  {HashingCount: 1},
			groupID: {HashingCount: 1},
		},
	}
	svc, mockStore := newTestServiceWithTelemetry(t, nil, deviceQ)
	ctx := testCtx(t)

	mockStore.EXPECT().GetCollectionTypes(gomock.Any(), testOrgID, []int64{rackID, groupID}).
		Return(map[int64]pb.CollectionType{
			rackID:  pb.CollectionType_COLLECTION_TYPE_RACK,
			groupID: pb.CollectionType_COLLECTION_TYPE_GROUP,
		}, nil)

	rackSlots := []*pb.RackSlotStatus{
		{Row: 0, Column: 0, Status: pb.SlotDeviceStatus_SLOT_DEVICE_STATUS_HEALTHY},
	}
	// Only rack IDs should be passed to GetRackSlotStatuses
	mockStore.EXPECT().GetRackSlotStatuses(gomock.Any(), testOrgID, []int64{rackID}).
		Return(map[int64][]*pb.RackSlotStatus{rackID: rackSlots}, nil)

	resp, err := svc.GetCollectionStats(ctx, &pb.GetCollectionStatsRequest{CollectionIds: []int64{rackID, groupID}})
	require.NoError(t, err)
	require.Len(t, resp.Stats, 2)

	// Rack should have slot statuses
	rackStats := resp.Stats[0]
	assert.Equal(t, rackID, rackStats.CollectionId)
	require.Len(t, rackStats.SlotStatuses, 1)
	assert.Equal(t, pb.SlotDeviceStatus_SLOT_DEVICE_STATUS_HEALTHY, rackStats.SlotStatuses[0].Status)

	// Group should have no slot statuses
	groupStats := resp.Stats[1]
	assert.Equal(t, groupID, groupStats.CollectionId)
	assert.Empty(t, groupStats.SlotStatuses)
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
	svc := NewService(mockStore, &mockDeviceQueryer{}, mockTransactor, resolver, nil, newStubActivityService(ctrl))

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

// --- SaveRack tests ---

var testRackInfo = &pb.RackInfo{
	Rows:        4,
	Columns:     8,
	Zone:        "Building A",
	OrderIndex:  pb.RackOrderIndex_RACK_ORDER_INDEX_BOTTOM_LEFT,
	CoolingType: pb.RackCoolingType_RACK_COOLING_TYPE_AIR,
}

func newTestServiceWithResolver(t *testing.T, resolver DeviceIdentifierResolver) (*Service, *mocks.MockCollectionStore, *mocks.MockTransactor) {
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
	svc := NewService(mockStore, &mockDeviceQueryer{}, mockTransactor, resolver, nil, newStubActivityService(ctrl))
	return svc, mockStore, mockTransactor
}

func TestService_SaveRack_CreateNewRack(t *testing.T) {
	deviceIDs := []string{"device-1", "device-2"}
	resolver := func(_ context.Context, _ *commonpb.DeviceSelector, _ int64) ([]string, error) {
		return deviceIDs, nil
	}
	svc, mockStore, _ := newTestServiceWithResolver(t, resolver)
	ctx := testCtx(t)

	// Arrange
	mockStore.EXPECT().CreateCollection(gomock.Any(), testOrgID, pb.CollectionType_COLLECTION_TYPE_RACK, "Rack A", "").
		Return(&pb.DeviceCollection{Id: 10, Label: "Rack A", Type: pb.CollectionType_COLLECTION_TYPE_RACK}, nil)
	mockStore.EXPECT().CreateRackExtension(gomock.Any(), int64(10), "Building A", int32(4), int32(8), int32(pb.RackOrderIndex_RACK_ORDER_INDEX_BOTTOM_LEFT), int32(pb.RackCoolingType_RACK_COOLING_TYPE_AIR), testOrgID).
		Return(nil)
	mockStore.EXPECT().RemoveAllDevicesFromCollection(gomock.Any(), testOrgID, int64(10)).Return(int64(0), nil)
	mockStore.EXPECT().AddDevicesToCollection(gomock.Any(), testOrgID, int64(10), deviceIDs).Return(int64(2), nil)
	mockStore.EXPECT().GetRackSlots(gomock.Any(), int64(10), testOrgID).Return(nil, nil)
	mockStore.EXPECT().SetRackSlotPosition(gomock.Any(), int64(10), "device-1", int32(0), int32(0), testOrgID).Return(nil)
	mockStore.EXPECT().SetRackSlotPosition(gomock.Any(), int64(10), "device-2", int32(0), int32(1), testOrgID).Return(nil)
	mockStore.EXPECT().GetCollection(gomock.Any(), testOrgID, int64(10)).
		Return(&pb.DeviceCollection{Id: 10, Label: "Rack A", Type: pb.CollectionType_COLLECTION_TYPE_RACK, DeviceCount: 2}, nil)

	// Act
	resp, err := svc.SaveRack(ctx, &pb.SaveRackRequest{
		Label:    "Rack A",
		RackInfo: testRackInfo,
		DeviceSelector: &commonpb.DeviceSelector{
			SelectionType: &commonpb.DeviceSelector_DeviceList{
				DeviceList: &commonpb.DeviceIdentifierList{DeviceIdentifiers: deviceIDs},
			},
		},
		SlotAssignments: []*pb.RackSlot{
			{DeviceIdentifier: "device-1", Position: &pb.RackSlotPosition{Row: 0, Column: 0}},
			{DeviceIdentifier: "device-2", Position: &pb.RackSlotPosition{Row: 0, Column: 1}},
		},
	})

	// Assert
	require.NoError(t, err)
	assert.Equal(t, "Rack A", resp.Collection.Label)
	assert.Equal(t, int32(2), resp.AssignedCount)
	assert.Equal(t, int32(2), resp.Collection.DeviceCount)
	assert.NotNil(t, resp.Collection.GetRackInfo())
}

func TestService_SaveRack_UpdateExistingRack(t *testing.T) {
	deviceIDs := []string{"device-3"}
	resolver := func(_ context.Context, _ *commonpb.DeviceSelector, _ int64) ([]string, error) {
		return deviceIDs, nil
	}
	svc, mockStore, _ := newTestServiceWithResolver(t, resolver)
	ctx := testCtx(t)

	collectionID := int64(42)

	// Arrange
	mockStore.EXPECT().CollectionBelongsToOrg(gomock.Any(), collectionID, testOrgID).Return(true, nil)
	mockStore.EXPECT().GetCollectionType(gomock.Any(), testOrgID, collectionID).Return(pb.CollectionType_COLLECTION_TYPE_RACK, nil)
	mockStore.EXPECT().UpdateCollection(gomock.Any(), testOrgID, collectionID, gomock.Any(), (*string)(nil)).Return(nil)
	mockStore.EXPECT().UpdateRackInfo(gomock.Any(), collectionID, "Building A", int32(4), int32(8), int32(pb.RackOrderIndex_RACK_ORDER_INDEX_BOTTOM_LEFT), int32(pb.RackCoolingType_RACK_COOLING_TYPE_AIR), testOrgID).Return(nil)
	mockStore.EXPECT().RemoveAllDevicesFromCollection(gomock.Any(), testOrgID, collectionID).Return(int64(2), nil)
	mockStore.EXPECT().AddDevicesToCollection(gomock.Any(), testOrgID, collectionID, deviceIDs).Return(int64(1), nil)
	mockStore.EXPECT().GetRackSlots(gomock.Any(), collectionID, testOrgID).Return([]*pb.RackSlot{
		{DeviceIdentifier: "old-device", Position: &pb.RackSlotPosition{Row: 0, Column: 0}},
	}, nil)
	mockStore.EXPECT().ClearRackSlotPosition(gomock.Any(), collectionID, "old-device", testOrgID).Return(nil)
	mockStore.EXPECT().SetRackSlotPosition(gomock.Any(), collectionID, "device-3", int32(1), int32(2), testOrgID).Return(nil)
	mockStore.EXPECT().GetCollection(gomock.Any(), testOrgID, collectionID).
		Return(&pb.DeviceCollection{Id: collectionID, Label: "Updated Rack", Type: pb.CollectionType_COLLECTION_TYPE_RACK, DeviceCount: 1}, nil)

	// Act
	resp, err := svc.SaveRack(ctx, &pb.SaveRackRequest{
		CollectionId: &collectionID,
		Label:        "Updated Rack",
		RackInfo:     testRackInfo,
		DeviceSelector: &commonpb.DeviceSelector{
			SelectionType: &commonpb.DeviceSelector_DeviceList{
				DeviceList: &commonpb.DeviceIdentifierList{DeviceIdentifiers: deviceIDs},
			},
		},
		SlotAssignments: []*pb.RackSlot{
			{DeviceIdentifier: "device-3", Position: &pb.RackSlotPosition{Row: 1, Column: 2}},
		},
	})

	// Assert
	require.NoError(t, err)
	assert.Equal(t, "Updated Rack", resp.Collection.Label)
	assert.Equal(t, int32(1), resp.AssignedCount)
}

func TestService_SaveRack_ValidationErrors(t *testing.T) {
	svc, _, _ := newTestService(t)
	ctx := testCtx(t)

	tests := []struct {
		name string
		req  *pb.SaveRackRequest
		want string
	}{
		{
			name: "missing rack_info",
			req:  &pb.SaveRackRequest{Label: "Rack"},
			want: "rack_info is required",
		},
		{
			name: "missing zone",
			req: &pb.SaveRackRequest{
				Label:    "Rack",
				RackInfo: &pb.RackInfo{Rows: 4, Columns: 8, OrderIndex: pb.RackOrderIndex_RACK_ORDER_INDEX_BOTTOM_LEFT, CoolingType: pb.RackCoolingType_RACK_COOLING_TYPE_AIR},
			},
			want: "zone is required",
		},
		{
			name: "rows too large",
			req: &pb.SaveRackRequest{
				Label:    "Rack",
				RackInfo: &pb.RackInfo{Rows: 13, Columns: 8, Zone: "A", OrderIndex: pb.RackOrderIndex_RACK_ORDER_INDEX_BOTTOM_LEFT, CoolingType: pb.RackCoolingType_RACK_COOLING_TYPE_AIR},
			},
			want: "rows must be between 1 and 12",
		},
		{
			name: "columns too large",
			req: &pb.SaveRackRequest{
				Label:    "Rack",
				RackInfo: &pb.RackInfo{Rows: 4, Columns: 13, Zone: "A", OrderIndex: pb.RackOrderIndex_RACK_ORDER_INDEX_BOTTOM_LEFT, CoolingType: pb.RackCoolingType_RACK_COOLING_TYPE_AIR},
			},
			want: "columns must be between 1 and 12",
		},
		{
			name: "unspecified order_index",
			req: &pb.SaveRackRequest{
				Label:    "Rack",
				RackInfo: &pb.RackInfo{Rows: 4, Columns: 8, Zone: "A", OrderIndex: pb.RackOrderIndex_RACK_ORDER_INDEX_UNSPECIFIED, CoolingType: pb.RackCoolingType_RACK_COOLING_TYPE_AIR},
			},
			want: "order_index is required",
		},
		{
			name: "unspecified cooling_type",
			req: &pb.SaveRackRequest{
				Label:    "Rack",
				RackInfo: &pb.RackInfo{Rows: 4, Columns: 8, Zone: "A", OrderIndex: pb.RackOrderIndex_RACK_ORDER_INDEX_BOTTOM_LEFT, CoolingType: pb.RackCoolingType_RACK_COOLING_TYPE_UNSPECIFIED},
			},
			want: "cooling_type is required",
		},
		{
			name: "slot row out of bounds",
			req: &pb.SaveRackRequest{
				Label:    "Rack",
				RackInfo: testRackInfo,
				DeviceSelector: &commonpb.DeviceSelector{
					SelectionType: &commonpb.DeviceSelector_DeviceList{
						DeviceList: &commonpb.DeviceIdentifierList{DeviceIdentifiers: []string{"device-1"}},
					},
				},
				SlotAssignments: []*pb.RackSlot{
					{DeviceIdentifier: "device-1", Position: &pb.RackSlotPosition{Row: 4, Column: 0}},
				},
			},
			want: "slot row 4 is out of bounds",
		},
		{
			name: "slot column out of bounds",
			req: &pb.SaveRackRequest{
				Label:    "Rack",
				RackInfo: testRackInfo,
				DeviceSelector: &commonpb.DeviceSelector{
					SelectionType: &commonpb.DeviceSelector_DeviceList{
						DeviceList: &commonpb.DeviceIdentifierList{DeviceIdentifiers: []string{"device-1"}},
					},
				},
				SlotAssignments: []*pb.RackSlot{
					{DeviceIdentifier: "device-1", Position: &pb.RackSlotPosition{Row: 0, Column: 8}},
				},
			},
			want: "slot column 8 is out of bounds",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.SaveRack(ctx, tt.req)
			require.Error(t, err)
			assert.True(t, fleeterror.IsInvalidArgumentError(err))
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestService_SaveRack_SlotAssignmentReferencesUnknownDevice(t *testing.T) {
	resolver := func(_ context.Context, _ *commonpb.DeviceSelector, _ int64) ([]string, error) {
		return []string{"device-1"}, nil
	}
	svc, _, _ := newTestServiceWithResolver(t, resolver)
	ctx := testCtx(t)

	_, err := svc.SaveRack(ctx, &pb.SaveRackRequest{
		Label:    "Rack",
		RackInfo: testRackInfo,
		DeviceSelector: &commonpb.DeviceSelector{
			SelectionType: &commonpb.DeviceSelector_DeviceList{
				DeviceList: &commonpb.DeviceIdentifierList{DeviceIdentifiers: []string{"device-1"}},
			},
		},
		SlotAssignments: []*pb.RackSlot{
			{DeviceIdentifier: "device-999", Position: &pb.RackSlotPosition{Row: 0, Column: 0}},
		},
	})

	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
	assert.Contains(t, err.Error(), "device-999")
}

func TestService_SaveRack_UpdateNotFound(t *testing.T) {
	resolver := func(_ context.Context, _ *commonpb.DeviceSelector, _ int64) ([]string, error) {
		return []string{}, nil
	}
	svc, mockStore, _ := newTestServiceWithResolver(t, resolver)
	ctx := testCtx(t)

	collectionID := int64(999)
	mockStore.EXPECT().CollectionBelongsToOrg(gomock.Any(), collectionID, testOrgID).Return(false, nil)

	_, err := svc.SaveRack(ctx, &pb.SaveRackRequest{
		CollectionId: &collectionID,
		Label:        "Rack",
		RackInfo:     testRackInfo,
		DeviceSelector: &commonpb.DeviceSelector{
			SelectionType: &commonpb.DeviceSelector_DeviceList{
				DeviceList: &commonpb.DeviceIdentifierList{DeviceIdentifiers: []string{}},
			},
		},
	})

	require.Error(t, err)
	assert.True(t, fleeterror.IsNotFoundError(err))
}

func TestService_SaveRack_RejectsGroupCollection(t *testing.T) {
	resolver := func(_ context.Context, _ *commonpb.DeviceSelector, _ int64) ([]string, error) {
		return []string{}, nil
	}
	svc, mockStore, _ := newTestServiceWithResolver(t, resolver)
	ctx := testCtx(t)

	collectionID := int64(42)
	mockStore.EXPECT().CollectionBelongsToOrg(gomock.Any(), collectionID, testOrgID).Return(true, nil)
	mockStore.EXPECT().GetCollectionType(gomock.Any(), testOrgID, collectionID).
		Return(pb.CollectionType_COLLECTION_TYPE_GROUP, nil)

	_, err := svc.SaveRack(ctx, &pb.SaveRackRequest{
		CollectionId: &collectionID,
		Label:        "Not a rack",
		RackInfo:     testRackInfo,
		DeviceSelector: &commonpb.DeviceSelector{
			SelectionType: &commonpb.DeviceSelector_DeviceList{
				DeviceList: &commonpb.DeviceIdentifierList{DeviceIdentifiers: []string{}},
			},
		},
	})

	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
	assert.Contains(t, err.Error(), "not a rack")
}

func TestService_SaveRack_StoreErrorRollsBack(t *testing.T) {
	deviceIDs := []string{"device-1"}
	resolver := func(_ context.Context, _ *commonpb.DeviceSelector, _ int64) ([]string, error) {
		return deviceIDs, nil
	}
	svc, mockStore, _ := newTestServiceWithResolver(t, resolver)
	ctx := testCtx(t)

	// Arrange - create succeeds but AddDevices fails
	mockStore.EXPECT().CreateCollection(gomock.Any(), testOrgID, pb.CollectionType_COLLECTION_TYPE_RACK, "Rack", "").
		Return(&pb.DeviceCollection{Id: 10, Type: pb.CollectionType_COLLECTION_TYPE_RACK}, nil)
	mockStore.EXPECT().CreateRackExtension(gomock.Any(), int64(10), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), testOrgID).
		Return(nil)
	mockStore.EXPECT().RemoveAllDevicesFromCollection(gomock.Any(), testOrgID, int64(10)).Return(int64(0), nil)
	mockStore.EXPECT().AddDevicesToCollection(gomock.Any(), testOrgID, int64(10), deviceIDs).
		Return(int64(0), fleeterror.NewInternalError("database error"))

	// Act
	_, err := svc.SaveRack(ctx, &pb.SaveRackRequest{
		Label:    "Rack",
		RackInfo: testRackInfo,
		DeviceSelector: &commonpb.DeviceSelector{
			SelectionType: &commonpb.DeviceSelector_DeviceList{
				DeviceList: &commonpb.DeviceIdentifierList{DeviceIdentifiers: deviceIDs},
			},
		},
		SlotAssignments: []*pb.RackSlot{
			{DeviceIdentifier: "device-1", Position: &pb.RackSlotPosition{Row: 0, Column: 0}},
		},
	})

	// Assert - error is returned, transaction would roll back
	require.Error(t, err)
	assert.Contains(t, err.Error(), "database error")
}

func TestService_SaveRack_CreateWithNoDevices(t *testing.T) {
	// Resolver should NOT be called for empty device lists — use a failing resolver to verify.
	resolver := func(_ context.Context, _ *commonpb.DeviceSelector, _ int64) ([]string, error) {
		t.Fatal("resolver should not be called for empty device list")
		return nil, nil
	}
	svc, mockStore, _ := newTestServiceWithResolver(t, resolver)
	ctx := testCtx(t)

	mockStore.EXPECT().CreateCollection(gomock.Any(), testOrgID, pb.CollectionType_COLLECTION_TYPE_RACK, "Empty Rack", "").
		Return(&pb.DeviceCollection{Id: 20, Label: "Empty Rack", Type: pb.CollectionType_COLLECTION_TYPE_RACK}, nil)
	mockStore.EXPECT().CreateRackExtension(gomock.Any(), int64(20), "Building A", int32(4), int32(8), int32(pb.RackOrderIndex_RACK_ORDER_INDEX_BOTTOM_LEFT), int32(pb.RackCoolingType_RACK_COOLING_TYPE_AIR), testOrgID).
		Return(nil)
	mockStore.EXPECT().RemoveAllDevicesFromCollection(gomock.Any(), testOrgID, int64(20)).Return(int64(0), nil)
	// AddDevicesToCollection should NOT be called since deviceIdentifiers is empty
	mockStore.EXPECT().GetRackSlots(gomock.Any(), int64(20), testOrgID).Return(nil, nil)
	mockStore.EXPECT().GetCollection(gomock.Any(), testOrgID, int64(20)).
		Return(&pb.DeviceCollection{Id: 20, Label: "Empty Rack", Type: pb.CollectionType_COLLECTION_TYPE_RACK, DeviceCount: 0}, nil)

	resp, err := svc.SaveRack(ctx, &pb.SaveRackRequest{
		Label:    "Empty Rack",
		RackInfo: testRackInfo,
		DeviceSelector: &commonpb.DeviceSelector{
			SelectionType: &commonpb.DeviceSelector_DeviceList{
				DeviceList: &commonpb.DeviceIdentifierList{DeviceIdentifiers: []string{}},
			},
		},
	})

	require.NoError(t, err)
	assert.Equal(t, "Empty Rack", resp.Collection.Label)
	assert.Equal(t, int32(0), resp.AssignedCount)
	assert.Equal(t, int32(0), resp.Collection.DeviceCount)
}

func TestService_SaveRack_UpdateRemoveAllMiners(t *testing.T) {
	// Removing all miners from an existing rack should work without resolver error.
	resolver := func(_ context.Context, _ *commonpb.DeviceSelector, _ int64) ([]string, error) {
		t.Fatal("resolver should not be called for empty device list")
		return nil, nil
	}
	svc, mockStore, _ := newTestServiceWithResolver(t, resolver)
	ctx := testCtx(t)

	collectionID := int64(42)

	mockStore.EXPECT().CollectionBelongsToOrg(gomock.Any(), collectionID, testOrgID).Return(true, nil)
	mockStore.EXPECT().GetCollectionType(gomock.Any(), testOrgID, collectionID).Return(pb.CollectionType_COLLECTION_TYPE_RACK, nil)
	mockStore.EXPECT().UpdateCollection(gomock.Any(), testOrgID, collectionID, gomock.Any(), (*string)(nil)).Return(nil)
	mockStore.EXPECT().UpdateRackInfo(gomock.Any(), collectionID, "Building A", int32(4), int32(8), int32(pb.RackOrderIndex_RACK_ORDER_INDEX_BOTTOM_LEFT), int32(pb.RackCoolingType_RACK_COOLING_TYPE_AIR), testOrgID).Return(nil)
	mockStore.EXPECT().RemoveAllDevicesFromCollection(gomock.Any(), testOrgID, collectionID).Return(int64(5), nil)
	mockStore.EXPECT().GetRackSlots(gomock.Any(), collectionID, testOrgID).Return(nil, nil)
	mockStore.EXPECT().GetCollection(gomock.Any(), testOrgID, collectionID).
		Return(&pb.DeviceCollection{Id: collectionID, Label: "Cleared Rack", Type: pb.CollectionType_COLLECTION_TYPE_RACK, DeviceCount: 0}, nil)

	resp, err := svc.SaveRack(ctx, &pb.SaveRackRequest{
		CollectionId: &collectionID,
		Label:        "Cleared Rack",
		RackInfo:     testRackInfo,
		DeviceSelector: &commonpb.DeviceSelector{
			SelectionType: &commonpb.DeviceSelector_DeviceList{
				DeviceList: &commonpb.DeviceIdentifierList{DeviceIdentifiers: []string{}},
			},
		},
	})

	require.NoError(t, err)
	assert.Equal(t, "Cleared Rack", resp.Collection.Label)
	assert.Equal(t, int32(0), resp.AssignedCount)
	assert.Equal(t, int32(0), resp.Collection.DeviceCount)
}

func newTestServiceWithActivityAssertions(t *testing.T) (*Service, *mocks.MockCollectionStore, *mocks.MockActivityStore) {
	t.Helper()
	ctrl := gomock.NewController(t)

	mockStore := mocks.NewMockCollectionStore(ctrl)
	mockTransactor := mocks.NewMockTransactor(ctrl)
	mockActivityStore := mocks.NewMockActivityStore(ctrl)

	mockTransactor.EXPECT().RunInTx(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) },
	).AnyTimes()
	mockTransactor.EXPECT().RunInTxWithResult(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, fn func(context.Context) (any, error)) (any, error) { return fn(ctx) },
	).AnyTimes()

	noopResolver := func(_ context.Context, _ *commonpb.DeviceSelector, _ int64) ([]string, error) {
		return nil, nil
	}

	activitySvc := activity.NewService(mockActivityStore)
	svc := NewService(mockStore, &mockDeviceQueryer{}, mockTransactor, noopResolver, nil, activitySvc)
	return svc, mockStore, mockActivityStore
}

func TestActivityLogging_CreateGroupLogsGroupScopeType(t *testing.T) {
	svc, mockStore, mockActivityStore := newTestServiceWithActivityAssertions(t)
	ctx := testCtx(t)

	mockStore.EXPECT().CreateCollection(gomock.Any(), testOrgID, pb.CollectionType_COLLECTION_TYPE_GROUP, "My Group", "").
		Return(&pb.DeviceCollection{Id: 1, Label: "My Group", Type: pb.CollectionType_COLLECTION_TYPE_GROUP}, nil)

	mockActivityStore.EXPECT().Insert(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, event *activitymodels.Event) error {
			assert.Equal(t, activitymodels.CategoryCollection, event.Category)
			assert.Equal(t, "create_collection", event.Type)
			require.NotNil(t, event.ScopeType)
			assert.Equal(t, "group", *event.ScopeType)
			assert.Contains(t, event.Description, "Create group:")
			return nil
		})

	_, err := svc.CreateCollection(ctx, &pb.CreateCollectionRequest{
		Type:  pb.CollectionType_COLLECTION_TYPE_GROUP,
		Label: "My Group",
	})
	require.NoError(t, err)
}

func TestActivityLogging_CreateRackLogsRackScopeType(t *testing.T) {
	svc, mockStore, mockActivityStore := newTestServiceWithActivityAssertions(t)
	ctx := testCtx(t)

	mockStore.EXPECT().CreateCollection(gomock.Any(), testOrgID, pb.CollectionType_COLLECTION_TYPE_RACK, "Rack A", "").
		Return(&pb.DeviceCollection{Id: 10, Label: "Rack A", Type: pb.CollectionType_COLLECTION_TYPE_RACK}, nil)
	mockStore.EXPECT().CreateRackExtension(gomock.Any(), int64(10), "Building A", int32(4), int32(8),
		int32(pb.RackOrderIndex_RACK_ORDER_INDEX_BOTTOM_LEFT), int32(pb.RackCoolingType_RACK_COOLING_TYPE_AIR), testOrgID).
		Return(nil)

	mockActivityStore.EXPECT().Insert(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, event *activitymodels.Event) error {
			assert.Equal(t, "create_collection", event.Type)
			require.NotNil(t, event.ScopeType)
			assert.Equal(t, "rack", *event.ScopeType)
			assert.Contains(t, event.Description, "Create rack:")
			return nil
		})

	_, err := svc.CreateCollection(ctx, &pb.CreateCollectionRequest{
		Type:  pb.CollectionType_COLLECTION_TYPE_RACK,
		Label: "Rack A",
		TypeDetails: &pb.CreateCollectionRequest_RackInfo{
			RackInfo: testRackInfo,
		},
	})
	require.NoError(t, err)
}

func TestActivityLogging_DeleteCollectionLogsEvent(t *testing.T) {
	svc, mockStore, mockActivityStore := newTestServiceWithActivityAssertions(t)
	ctx := testCtx(t)

	mockStore.EXPECT().GetCollection(gomock.Any(), testOrgID, testCollectionID).
		Return(&pb.DeviceCollection{Id: testCollectionID, Label: "Doomed Group", Type: pb.CollectionType_COLLECTION_TYPE_GROUP}, nil)
	mockStore.EXPECT().RemoveAllDevicesFromCollection(gomock.Any(), testOrgID, testCollectionID).
		Return(int64(3), nil)
	mockStore.EXPECT().SoftDeleteCollection(gomock.Any(), testOrgID, testCollectionID).
		Return(int64(1), nil)

	mockActivityStore.EXPECT().Insert(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, event *activitymodels.Event) error {
			assert.Equal(t, activitymodels.CategoryCollection, event.Category)
			assert.Equal(t, "delete_collection", event.Type)
			require.NotNil(t, event.ScopeType)
			assert.Equal(t, "group", *event.ScopeType)
			require.NotNil(t, event.ScopeLabel)
			assert.Equal(t, "Doomed Group", *event.ScopeLabel)
			return nil
		})

	_, err := svc.DeleteCollection(ctx, &pb.DeleteCollectionRequest{CollectionId: testCollectionID})
	require.NoError(t, err)
}
