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

func int64Ptr(v int64) *int64 { return &v }

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

func (m *mockDeviceQueryer) GetComponentErrorCounts(_ context.Context, _ int64, _ interfaces.ComponentErrorScope) ([]interfaces.ComponentErrorCount, error) {
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

	svc := NewService(mockStore, &mockDeviceQueryer{}, nil, nil, mockTransactor, noopResolver, nil, newStubActivityService(ctrl))
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
	mockStore.EXPECT().CreateRackExtension(gomock.Any(), gomock.Any()).
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

// TestService_DeleteCollection_LocksRackBeforeCascade guards against a
// race where a concurrent AddDevicesToCollection slips between the
// delete path's unassign and soft-delete steps. The lock must fire
// BEFORE UnassignDeviceSitesByRack so callers that share the rack lock
// (AddDevicesToCollection, SaveRack) serialize against deletion.
func TestService_DeleteCollection_LocksRackBeforeCascade(t *testing.T) {
	svc, mockStore, _ := newTestService(t)
	ctx := testCtx(t)

	mockStore.EXPECT().GetCollection(gomock.Any(), testOrgID, testCollectionID).
		Return(&pb.DeviceCollection{Id: testCollectionID, Label: "rack-1", Type: pb.CollectionType_COLLECTION_TYPE_RACK}, nil)
	mockStore.EXPECT().GetCollectionType(gomock.Any(), testOrgID, testCollectionID).
		Return(pb.CollectionType_COLLECTION_TYPE_RACK, nil)

	gomock.InOrder(
		mockStore.EXPECT().LockRackPlacementForWrite(gomock.Any(), testCollectionID, testOrgID).
			Return(interfaces.RackPlacement{}, nil),
		mockStore.EXPECT().UnassignDeviceSitesByRack(gomock.Any(), testCollectionID, testOrgID).
			Return(int64(0), nil),
		mockStore.EXPECT().UnassignDeviceBuildingsByRack(gomock.Any(), testCollectionID, testOrgID).
			Return(int64(0), nil),
		// Placement clear is rack-scoped and now lives inside the
		// rack branch, so it lands BEFORE the generic
		// RemoveAllDevicesFromCollection step.
		mockStore.EXPECT().ClearRackPlacementForSoftDelete(gomock.Any(), testOrgID, testCollectionID).
			Return(nil),
		mockStore.EXPECT().RemoveAllDevicesFromCollection(gomock.Any(), testOrgID, testCollectionID).
			Return(int64(0), nil),
		mockStore.EXPECT().SoftDeleteCollection(gomock.Any(), testOrgID, testCollectionID).
			Return(int64(1), nil),
	)

	_, err := svc.DeleteCollection(ctx, &pb.DeleteCollectionRequest{CollectionId: testCollectionID})
	require.NoError(t, err)
}

func TestService_DeleteCollection_NotFoundWhenZeroRows(t *testing.T) {
	svc, mockStore, _ := newTestService(t)
	ctx := testCtx(t)

	// Arrange
	mockStore.EXPECT().GetCollection(gomock.Any(), testOrgID, testCollectionID).
		Return(&pb.DeviceCollection{Id: testCollectionID, Label: "gone", Type: pb.CollectionType_COLLECTION_TYPE_GROUP}, nil)
	// In-tx type re-read decides whether the site cascade runs; group
	// targets skip cascade.
	mockStore.EXPECT().GetCollectionType(gomock.Any(), testOrgID, testCollectionID).
		Return(pb.CollectionType_COLLECTION_TYPE_GROUP, nil)
	mockStore.EXPECT().RemoveAllDevicesFromCollection(gomock.Any(), testOrgID, testCollectionID).
		Return(int64(0), nil)
	// ClearRackPlacementForSoftDelete is rack-scoped — this group
	// delete must not invoke it.
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

func TestService_AddDevicesToGroup_NotFoundWhenNotOwnedByOrg(t *testing.T) {
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
	svc := NewService(mockStore, &mockDeviceQueryer{}, nil, nil, mockTransactor, resolver, nil, newStubActivityService(ctrl))

	mockStore.EXPECT().GetCollection(gomock.Any(), testOrgID, testCollectionID).
		Return(nil, fleeterror.NewNotFoundErrorf("collection not found"))

	// Act
	_, err := svc.AddDevicesToGroup(ctx, AddDevicesToGroupParams{
		TargetGroupID: testCollectionID,
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

func TestService_AddDevicesToGroup_ResolverError(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockStore := mocks.NewMockCollectionStore(ctrl)
	mockTransactor := mocks.NewMockTransactor(ctrl)
	ctx := testCtx(t)

	// Arrange - resolver fails (e.g. device not owned by org)
	resolver := func(_ context.Context, _ *commonpb.DeviceSelector, _ int64) ([]string, error) {
		return nil, fleeterror.NewForbiddenError("access denied")
	}
	svc := NewService(mockStore, &mockDeviceQueryer{}, nil, nil, mockTransactor, resolver, nil, newStubActivityService(ctrl))

	// Act
	_, err := svc.AddDevicesToGroup(ctx, AddDevicesToGroupParams{
		TargetGroupID: testCollectionID,
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
	svc := NewService(mockStore, &mockDeviceQueryer{}, nil, nil, mockTransactor, resolver, nil, newStubActivityService(ctrl))

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
	svc := NewService(mockStore, &mockDeviceQueryer{}, nil, nil, mockTransactor, resolver, nil, newStubActivityService(ctrl))

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
	svc := NewService(mockStore, &mockDeviceQueryer{}, nil, nil, mockTransactor, resolver, nil, newStubActivityService(ctrl))

	mockTransactor.EXPECT().RunInTxWithResult(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, fn func(context.Context) (any, error)) (any, error) {
			return fn(ctx)
		},
	)

	newLabel := "Updated Group"
	mockStore.EXPECT().UpdateCollection(gomock.Any(), testOrgID, testCollectionID, &newLabel, (*string)(nil)).Return(nil)
	// Group target: no rack lock + no cascade (groups are cross-site).
	mockStore.EXPECT().GetCollectionType(gomock.Any(), testOrgID, testCollectionID).Return(pb.CollectionType_COLLECTION_TYPE_GROUP, nil)
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
	svc := NewService(mockStore, &mockDeviceQueryer{}, nil, nil, mockTransactor, resolver, nil, newStubActivityService(ctrl))

	mockTransactor.EXPECT().RunInTxWithResult(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, fn func(context.Context) (any, error)) (any, error) {
			return fn(ctx)
		},
	)

	mockStore.EXPECT().UpdateCollection(gomock.Any(), testOrgID, testCollectionID, (*string)(nil), (*string)(nil)).Return(nil)
	mockStore.EXPECT().GetCollectionType(gomock.Any(), testOrgID, testCollectionID).Return(pb.CollectionType_COLLECTION_TYPE_GROUP, nil)
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
	svc := NewService(mockStore, deviceQ, nil, nil, mockTransactor, noopResolver, telemetry, newStubActivityService(ctrl))
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
	svc := NewService(mockStore, &mockDeviceQueryer{}, nil, nil, mockTransactor, resolver, nil, newStubActivityService(ctrl))

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
	svc := NewService(mockStore, &mockDeviceQueryer{}, nil, nil, mockTransactor, resolver, nil, newStubActivityService(ctrl))
	return svc, mockStore, mockTransactor
}

// newTestServiceWithSites wires a MockSiteStore so cascade flows that
// require site/building locking can be exercised.
func newTestServiceWithSites(t *testing.T, resolver DeviceIdentifierResolver) (*Service, *mocks.MockCollectionStore, *mocks.MockSiteStore) {
	t.Helper()
	ctrl := gomock.NewController(t)
	mockStore := mocks.NewMockCollectionStore(ctrl)
	mockSiteStore := mocks.NewMockSiteStore(ctrl)
	mockTransactor := mocks.NewMockTransactor(ctrl)
	mockTransactor.EXPECT().RunInTx(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) },
	).AnyTimes()
	mockTransactor.EXPECT().RunInTxWithResult(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, fn func(context.Context) (any, error)) (any, error) { return fn(ctx) },
	).AnyTimes()
	// Multi-device group writers resolve their activity-log site scope (#538);
	// default to an empty (org-scoped) result so callers that don't assert on
	// scope stay simple. Tests that exercise scope use explicit expectations.
	mockSiteStore.EXPECT().GetDistinctDeviceSiteIDs(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil, nil).AnyTimes()
	svc := NewService(mockStore, &mockDeviceQueryer{}, mockSiteStore, nil, mockTransactor, resolver, nil, newStubActivityService(ctrl))
	return svc, mockStore, mockSiteStore
}

// newTestServiceWithSitesRecordingActivity wires a MockSiteStore PLUS a
// recording activity service so cascade tests can assert the activity-log
// event shape (Type, SiteID, Metadata keys). Returns the captured events
// slice — callers assert against its final state after the RPC returns.
func newTestServiceWithSitesRecordingActivity(t *testing.T, resolver DeviceIdentifierResolver) (*Service, *mocks.MockCollectionStore, *mocks.MockSiteStore, *[]activitymodels.Event) {
	t.Helper()
	ctrl := gomock.NewController(t)
	mockStore := mocks.NewMockCollectionStore(ctrl)
	mockSiteStore := mocks.NewMockSiteStore(ctrl)
	mockTransactor := mocks.NewMockTransactor(ctrl)
	mockActivityStore := mocks.NewMockActivityStore(ctrl)

	captured := []activitymodels.Event{}
	mockActivityStore.EXPECT().Insert(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, event *activitymodels.Event) error {
			captured = append(captured, *event)
			return nil
		}).AnyTimes()

	mockTransactor.EXPECT().RunInTx(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) },
	).AnyTimes()
	mockTransactor.EXPECT().RunInTxWithResult(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, fn func(context.Context) (any, error)) (any, error) { return fn(ctx) },
	).AnyTimes()
	svc := NewService(mockStore, &mockDeviceQueryer{}, mockSiteStore, nil, mockTransactor, resolver, nil, activity.NewService(mockActivityStore))
	return svc, mockStore, mockSiteStore, &captured
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
	mockStore.EXPECT().CreateRackExtension(gomock.Any(), gomock.Any()).
		Return(nil)
	mockStore.EXPECT().RemoveAllDevicesFromCollection(gomock.Any(), testOrgID, int64(10)).Return(int64(0), nil)
	mockStore.EXPECT().LockRacksForReparent(gomock.Any(), testOrgID, deviceIDs, int64(0)).Return(nil, nil)
	mockStore.EXPECT().RemoveDevicesFromAnyRack(gomock.Any(), testOrgID, deviceIDs, int64(10)).Return(int64(0), nil)
	mockStore.EXPECT().AddDevicesToCollection(gomock.Any(), testOrgID, int64(10), deviceIDs).Return(int64(2), nil)
	// A rack ALWAYS dictates member placement now — a site-less rack
	// cascades nil/nil, stripping any member's direct site/building to
	// keep the membership tree consistent (IS DISTINCT FROM no-ops
	// already-matching members).
	mockStore.EXPECT().GetDeviceSiteIDsByMembership(gomock.Any(), int64(10), testOrgID).Return(nil, nil)
	mockStore.EXPECT().CascadeRackDeviceSites(gomock.Any(), int64(10), testOrgID, gomock.Nil()).Return(int64(0), nil)
	mockStore.EXPECT().CascadeRackDeviceBuildings(gomock.Any(), int64(10), testOrgID, gomock.Nil()).Return(int64(0), nil)
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
	mockStore.EXPECT().LockRackPlacementForWrite(gomock.Any(), collectionID, testOrgID).
		Return(interfaces.RackPlacement{}, nil)
	mockStore.EXPECT().UpdateCollection(gomock.Any(), testOrgID, collectionID, gomock.Any(), (*string)(nil)).Return(nil)
	mockStore.EXPECT().UpdateRackInfo(gomock.Any(), collectionID, "Building A", int32(4), int32(8), int32(pb.RackOrderIndex_RACK_ORDER_INDEX_BOTTOM_LEFT), int32(pb.RackCoolingType_RACK_COOLING_TYPE_AIR), testOrgID).Return(nil)
	mockStore.EXPECT().UpdateRackPlacement(gomock.Any(), collectionID, testOrgID, gomock.Nil(), gomock.Nil(), "Building A").Return(nil)
	mockStore.EXPECT().RemoveAllDevicesFromCollection(gomock.Any(), testOrgID, collectionID).Return(int64(2), nil)
	mockStore.EXPECT().LockRacksForReparent(gomock.Any(), testOrgID, deviceIDs, collectionID).Return(nil, nil)
	mockStore.EXPECT().RemoveDevicesFromAnyRack(gomock.Any(), testOrgID, deviceIDs, collectionID).Return(int64(0), nil)
	mockStore.EXPECT().AddDevicesToCollection(gomock.Any(), testOrgID, collectionID, deviceIDs).Return(int64(1), nil)
	// Site-less rack now cascades nil/nil unconditionally — members can't
	// keep a direct site/building the rack lacks.
	mockStore.EXPECT().GetDeviceSiteIDsByMembership(gomock.Any(), collectionID, testOrgID).Return(nil, nil)
	mockStore.EXPECT().CascadeRackDeviceSites(gomock.Any(), collectionID, testOrgID, gomock.Nil()).Return(int64(0), nil)
	mockStore.EXPECT().CascadeRackDeviceBuildings(gomock.Any(), collectionID, testOrgID, gomock.Nil()).Return(int64(0), nil)
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
			// Zone is now only required when the rack belongs to a building;
			// direct-under-site racks (building_id nil) may have empty zone.
			name: "missing zone with building set",
			req: &pb.SaveRackRequest{
				Label:    "Rack",
				RackInfo: &pb.RackInfo{Rows: 4, Columns: 8, OrderIndex: pb.RackOrderIndex_RACK_ORDER_INDEX_BOTTOM_LEFT, CoolingType: pb.RackCoolingType_RACK_COOLING_TYPE_AIR, BuildingId: int64Ptr(7)},
			},
			want: "zone is required when the rack belongs to a building",
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
	mockStore.EXPECT().CreateRackExtension(gomock.Any(), gomock.Any()).
		Return(nil)
	mockStore.EXPECT().RemoveAllDevicesFromCollection(gomock.Any(), testOrgID, int64(10)).Return(int64(0), nil)
	mockStore.EXPECT().LockRacksForReparent(gomock.Any(), testOrgID, deviceIDs, int64(0)).Return(nil, nil)
	mockStore.EXPECT().RemoveDevicesFromAnyRack(gomock.Any(), testOrgID, deviceIDs, int64(10)).Return(int64(0), nil)
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
	mockStore.EXPECT().CreateRackExtension(gomock.Any(), gomock.Any()).
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
	mockStore.EXPECT().LockRackPlacementForWrite(gomock.Any(), collectionID, testOrgID).
		Return(interfaces.RackPlacement{}, nil)
	mockStore.EXPECT().UpdateCollection(gomock.Any(), testOrgID, collectionID, gomock.Any(), (*string)(nil)).Return(nil)
	mockStore.EXPECT().UpdateRackInfo(gomock.Any(), collectionID, "Building A", int32(4), int32(8), int32(pb.RackOrderIndex_RACK_ORDER_INDEX_BOTTOM_LEFT), int32(pb.RackCoolingType_RACK_COOLING_TYPE_AIR), testOrgID).Return(nil)
	mockStore.EXPECT().UpdateRackPlacement(gomock.Any(), collectionID, testOrgID, gomock.Nil(), gomock.Nil(), "Building A").Return(nil)
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
	svc := NewService(mockStore, &mockDeviceQueryer{}, nil, nil, mockTransactor, noopResolver, nil, activitySvc)
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
	mockStore.EXPECT().CreateRackExtension(gomock.Any(), gomock.Any()).
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
	mockStore.EXPECT().GetCollectionType(gomock.Any(), testOrgID, testCollectionID).
		Return(pb.CollectionType_COLLECTION_TYPE_GROUP, nil)
	mockStore.EXPECT().RemoveAllDevicesFromCollection(gomock.Any(), testOrgID, testCollectionID).
		Return(int64(3), nil)
	// ClearRackPlacementForSoftDelete is rack-scoped — this group
	// delete must not invoke it.
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

// TestService_SaveRack_MoveBetweenBuildingsCascadesSite asserts the rack
// edit/move cascade: when a rack moves to a building whose site differs
// from the current placement, the rack's site_id is rewritten and every
// descendant device.site_id is cascaded to match (plan §"Rack edit / move"
// + issue #220).
func TestService_SaveRack_MoveBetweenBuildingsCascadesSite(t *testing.T) {
	deviceIDs := []string{"device-1"}
	resolver := func(_ context.Context, _ *commonpb.DeviceSelector, _ int64) ([]string, error) {
		return deviceIDs, nil
	}
	svc, mockStore, mockSiteStore, captured := newTestServiceWithSitesRecordingActivity(t, resolver)
	ctx := testCtx(t)

	collectionID := int64(42)
	priorSite := int64Ptr(7)
	priorBuilding := int64Ptr(70)
	newBuilding := int64(80)
	newSiteID := int64(8)

	// Canonical lock order: collection ownership → type → resolve placement
	// (site → building) → rack lock. SaveRack now follows
	// site → building → rack → devices to match SiteService writers.
	mockStore.EXPECT().CollectionBelongsToOrg(gomock.Any(), collectionID, testOrgID).Return(true, nil)
	mockStore.EXPECT().GetCollectionType(gomock.Any(), testOrgID, collectionID).Return(pb.CollectionType_COLLECTION_TYPE_RACK, nil)
	// resolveAndLockRackPlacement peeks building→site, locks site, locks
	// building, then re-reads building→site under the lock to detect
	// concurrent AssignBuildingsToSite. Both reads return the same value
	// here, so the tx proceeds without abort/retry.
	mockStore.EXPECT().GetBuildingSite(gomock.Any(), testOrgID, newBuilding).Return(&newSiteID, nil).Times(2)
	mockSiteStore.EXPECT().LockSiteForWrite(gomock.Any(), testOrgID, newSiteID).Return(nil)
	mockSiteStore.EXPECT().LockBuildingForWrite(gomock.Any(), testOrgID, newBuilding).Return(nil)
	mockStore.EXPECT().LockRackPlacementForWrite(gomock.Any(), collectionID, testOrgID).
		Return(interfaces.RackPlacement{SiteID: priorSite, BuildingID: priorBuilding, Zone: "Old Zone"}, nil)

	mockStore.EXPECT().UpdateCollection(gomock.Any(), testOrgID, collectionID, gomock.Any(), (*string)(nil)).Return(nil)
	// Zone is cleared by the cascade because the rack crossed a building
	// boundary; both UpdateRackInfo and UpdateRackPlacement write "".
	mockStore.EXPECT().UpdateRackInfo(gomock.Any(), collectionID, "", int32(4), int32(8), int32(pb.RackOrderIndex_RACK_ORDER_INDEX_BOTTOM_LEFT), int32(pb.RackCoolingType_RACK_COOLING_TYPE_AIR), testOrgID).Return(nil)
	mockStore.EXPECT().UpdateRackPlacement(gomock.Any(), collectionID, testOrgID, gomock.Eq(&newSiteID), gomock.Eq(&newBuilding), "").Return(nil)

	mockStore.EXPECT().RemoveAllDevicesFromCollection(gomock.Any(), testOrgID, collectionID).Return(int64(1), nil)
	mockStore.EXPECT().LockRacksForReparent(gomock.Any(), testOrgID, deviceIDs, collectionID).Return(nil, nil)
	mockStore.EXPECT().RemoveDevicesFromAnyRack(gomock.Any(), testOrgID, deviceIDs, collectionID).Return(int64(0), nil)
	mockStore.EXPECT().AddDevicesToCollection(gomock.Any(), testOrgID, collectionID, deviceIDs).Return(int64(1), nil)
	// Single cascade after membership replace: captures per-device
	// priors on the FINAL member set, then rewrites differing devices.
	// device-1 was at priorSite (7); rack now stamped with newSiteID (8).
	mockStore.EXPECT().GetDeviceSiteIDsByMembership(gomock.Any(), collectionID, testOrgID).
		Return(map[string]*int64{"device-1": priorSite}, nil)
	mockStore.EXPECT().CascadeRackDeviceSites(gomock.Any(), collectionID, testOrgID, gomock.Eq(&newSiteID)).Return(int64(1), nil)
	// Building peer of the site cascade — fires whenever the rack has a
	// stamped building, mirroring the site cascade above for
	// device.building_id.
	mockStore.EXPECT().CascadeRackDeviceBuildings(gomock.Any(), collectionID, testOrgID, gomock.Eq(&newBuilding)).Return(int64(1), nil)
	mockStore.EXPECT().GetRackSlots(gomock.Any(), collectionID, testOrgID).Return(nil, nil)
	mockStore.EXPECT().GetCollection(gomock.Any(), testOrgID, collectionID).
		Return(&pb.DeviceCollection{Id: collectionID, Label: "Rack A", Type: pb.CollectionType_COLLECTION_TYPE_RACK, DeviceCount: 1}, nil)

	rackInfo := &pb.RackInfo{
		Rows:        4,
		Columns:     8,
		Zone:        "Old Zone",
		OrderIndex:  pb.RackOrderIndex_RACK_ORDER_INDEX_BOTTOM_LEFT,
		CoolingType: pb.RackCoolingType_RACK_COOLING_TYPE_AIR,
		BuildingId:  &newBuilding,
	}
	resp, err := svc.SaveRack(ctx, &pb.SaveRackRequest{
		CollectionId: &collectionID,
		Label:        "Rack A",
		RackInfo:     rackInfo,
		DeviceSelector: &commonpb.DeviceSelector{
			SelectionType: &commonpb.DeviceSelector_DeviceList{
				DeviceList: &commonpb.DeviceIdentifierList{DeviceIdentifiers: deviceIDs},
			},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotNil(t, resp.Collection.GetRackInfo())
	assert.Equal(t, "", resp.Collection.GetRackInfo().Zone, "zone should be cleared when crossing buildings")
	require.NotNil(t, resp.Collection.GetRackInfo().SiteId)
	assert.Equal(t, newSiteID, *resp.Collection.GetRackInfo().SiteId)
	require.NotNil(t, resp.Collection.GetRackInfo().BuildingId)
	assert.Equal(t, newBuilding, *resp.Collection.GetRackInfo().BuildingId)

	// Assert cascade metadata on the activity-log event so the audit
	// trail reflects the implicit device-site reassignment.
	require.Len(t, *captured, 1, "expected exactly one save_rack activity event")
	event := (*captured)[0]
	assert.Equal(t, "save_rack", event.Type)
	require.NotNil(t, event.SiteID, "activity event must carry final site_id")
	assert.Equal(t, newSiteID, *event.SiteID)
	require.NotNil(t, event.Metadata, "cascade events must populate metadata")
	assert.Equal(t, true, event.Metadata["site_cascade"])
	assert.Equal(t, int64(1), event.Metadata["site_reassigned_count"])
	changes, ok := event.Metadata["device_site_changes"].([]map[string]any)
	require.True(t, ok, "device_site_changes must be present")
	require.Len(t, changes, 1)
	assert.Equal(t, "device-1", changes[0]["device_identifier"])
	assert.Equal(t, int64(7), changes[0]["prior_site_id"])
	assert.Equal(t, newSiteID, changes[0]["target_site_id"])

	// SiteReassignedCount mirrors the cascade row count on the response.
	assert.Equal(t, int32(1), resp.SiteReassignedCount)
}

// TestService_SaveRack_locksBuildingBeforeRacks pins #555's lock-order
// invariant: on the placement-update path SaveRack must lock the target
// site/building FIRST, then acquire every source + target rack via
// LockRacksForReparent, then the per-target LockRackPlacementForWrite.
// Reversing this (locking racks before the building, as PR #551 did) risks
// deadlock against AssignRacksToBuilding, which takes building → rack. The
// rack pre-pass must still land BEFORE LockRackPlacementForWrite so the
// original rack-vs-rack deadlock fix is preserved.
func TestService_SaveRack_locksBuildingBeforeRacks(t *testing.T) {
	deviceIDs := []string{"device-1"}
	resolver := func(_ context.Context, _ *commonpb.DeviceSelector, _ int64) ([]string, error) {
		return deviceIDs, nil
	}
	svc, mockStore, mockSiteStore := newTestServiceWithSites(t, resolver)
	ctx := testCtx(t)

	collectionID := int64(42)
	newBuilding := int64(80)
	newSiteID := int64(8)

	// Unordered scaffolding around the lock sequence.
	mockStore.EXPECT().CollectionBelongsToOrg(gomock.Any(), collectionID, testOrgID).Return(true, nil)
	mockStore.EXPECT().GetCollectionType(gomock.Any(), testOrgID, collectionID).Return(pb.CollectionType_COLLECTION_TYPE_RACK, nil)
	// Call count left loose: this test pins the lock ordering, not how many
	// times placement resolution reads building->site.
	mockStore.EXPECT().GetBuildingSite(gomock.Any(), testOrgID, newBuilding).Return(&newSiteID, nil).AnyTimes()
	mockStore.EXPECT().UpdateCollection(gomock.Any(), testOrgID, collectionID, gomock.Any(), (*string)(nil)).Return(nil)
	mockStore.EXPECT().UpdateRackInfo(gomock.Any(), collectionID, gomock.Any(), int32(4), int32(8), gomock.Any(), gomock.Any(), testOrgID).Return(nil)
	mockStore.EXPECT().UpdateRackPlacement(gomock.Any(), collectionID, testOrgID, gomock.Eq(&newSiteID), gomock.Eq(&newBuilding), gomock.Any()).Return(nil)
	mockStore.EXPECT().RemoveAllDevicesFromCollection(gomock.Any(), testOrgID, collectionID).Return(int64(0), nil)
	mockStore.EXPECT().RemoveDevicesFromAnyRack(gomock.Any(), testOrgID, deviceIDs, collectionID).Return(int64(0), nil)
	mockStore.EXPECT().AddDevicesToCollection(gomock.Any(), testOrgID, collectionID, deviceIDs).Return(int64(1), nil)
	mockStore.EXPECT().GetDeviceSiteIDsByMembership(gomock.Any(), collectionID, testOrgID).Return(map[string]*int64{"device-1": &newSiteID}, nil)
	mockStore.EXPECT().CascadeRackDeviceSites(gomock.Any(), collectionID, testOrgID, gomock.Any()).Return(int64(0), nil)
	mockStore.EXPECT().CascadeRackDeviceBuildings(gomock.Any(), collectionID, testOrgID, gomock.Any()).Return(int64(0), nil)
	mockStore.EXPECT().GetRackSlots(gomock.Any(), collectionID, testOrgID).Return(nil, nil)
	mockStore.EXPECT().GetCollection(gomock.Any(), testOrgID, collectionID).
		Return(&pb.DeviceCollection{Id: collectionID, Label: "Rack A", Type: pb.CollectionType_COLLECTION_TYPE_RACK, DeviceCount: 1}, nil)

	// The invariant: site/building locks precede the rack pre-pass, which
	// precedes the target placement-row lock.
	gomock.InOrder(
		mockSiteStore.EXPECT().LockSiteForWrite(gomock.Any(), testOrgID, newSiteID).Return(nil),
		mockSiteStore.EXPECT().LockBuildingForWrite(gomock.Any(), testOrgID, newBuilding).Return(nil),
		mockStore.EXPECT().LockRacksForReparent(gomock.Any(), testOrgID, deviceIDs, collectionID).Return(nil, nil),
		mockStore.EXPECT().LockRackPlacementForWrite(gomock.Any(), collectionID, testOrgID).
			Return(interfaces.RackPlacement{SiteID: &newSiteID, BuildingID: &newBuilding}, nil),
	)

	rackInfo := &pb.RackInfo{
		Rows:        4,
		Columns:     8,
		Zone:        "Zone A",
		OrderIndex:  pb.RackOrderIndex_RACK_ORDER_INDEX_BOTTOM_LEFT,
		CoolingType: pb.RackCoolingType_RACK_COOLING_TYPE_AIR,
		BuildingId:  &newBuilding,
	}
	_, err := svc.SaveRack(ctx, &pb.SaveRackRequest{
		CollectionId: &collectionID,
		Label:        "Rack A",
		RackInfo:     rackInfo,
		DeviceSelector: &commonpb.DeviceSelector{
			SelectionType: &commonpb.DeviceSelector_DeviceList{
				DeviceList: &commonpb.DeviceIdentifierList{DeviceIdentifiers: deviceIDs},
			},
		},
	})
	require.NoError(t, err)
}

// TestService_SaveRack_MoveToDirectUnderSite covers the variant where a
// rack is moved out of any building and attached directly to a site.
// Zone is still cleared (building boundary crossed) and the site lock
// is taken without a building lock.
func TestService_SaveRack_MoveToDirectUnderSite(t *testing.T) {
	resolver := func(_ context.Context, _ *commonpb.DeviceSelector, _ int64) ([]string, error) {
		return nil, nil
	}
	svc, mockStore, mockSiteStore := newTestServiceWithSites(t, resolver)
	ctx := testCtx(t)

	collectionID := int64(42)
	priorSite := int64Ptr(7)
	priorBuilding := int64Ptr(70)
	newSite := int64(7) // Same site, just direct attach (no building).

	mockStore.EXPECT().CollectionBelongsToOrg(gomock.Any(), collectionID, testOrgID).Return(true, nil)
	mockStore.EXPECT().GetCollectionType(gomock.Any(), testOrgID, collectionID).Return(pb.CollectionType_COLLECTION_TYPE_RACK, nil)
	mockStore.EXPECT().LockRackPlacementForWrite(gomock.Any(), collectionID, testOrgID).
		Return(interfaces.RackPlacement{SiteID: priorSite, BuildingID: priorBuilding, Zone: "Some Zone"}, nil)
	mockSiteStore.EXPECT().LockSiteForWrite(gomock.Any(), testOrgID, newSite).Return(nil)

	mockStore.EXPECT().UpdateCollection(gomock.Any(), testOrgID, collectionID, gomock.Any(), (*string)(nil)).Return(nil)
	mockStore.EXPECT().UpdateRackInfo(gomock.Any(), collectionID, "", int32(4), int32(8), int32(pb.RackOrderIndex_RACK_ORDER_INDEX_BOTTOM_LEFT), int32(pb.RackCoolingType_RACK_COOLING_TYPE_AIR), testOrgID).Return(nil)
	mockStore.EXPECT().UpdateRackPlacement(gomock.Any(), collectionID, testOrgID, gomock.Eq(&newSite), gomock.Nil(), "").Return(nil)
	// Site identical (7 -> 7) so no cascade.
	mockStore.EXPECT().RemoveAllDevicesFromCollection(gomock.Any(), testOrgID, collectionID).Return(int64(0), nil)
	mockStore.EXPECT().GetRackSlots(gomock.Any(), collectionID, testOrgID).Return(nil, nil)
	mockStore.EXPECT().GetCollection(gomock.Any(), testOrgID, collectionID).
		Return(&pb.DeviceCollection{Id: collectionID, Label: "Rack A", Type: pb.CollectionType_COLLECTION_TYPE_RACK}, nil)

	rackInfo := &pb.RackInfo{
		Rows:        4,
		Columns:     8,
		Zone:        "Some Zone",
		OrderIndex:  pb.RackOrderIndex_RACK_ORDER_INDEX_BOTTOM_LEFT,
		CoolingType: pb.RackCoolingType_RACK_COOLING_TYPE_AIR,
		SiteId:      &newSite,
	}
	resp, err := svc.SaveRack(ctx, &pb.SaveRackRequest{
		CollectionId: &collectionID,
		Label:        "Rack A",
		RackInfo:     rackInfo,
		DeviceSelector: &commonpb.DeviceSelector{
			SelectionType: &commonpb.DeviceSelector_DeviceList{
				DeviceList: &commonpb.DeviceIdentifierList{DeviceIdentifiers: []string{}},
			},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, resp.Collection.GetRackInfo())
	assert.Equal(t, "", resp.Collection.GetRackInfo().Zone)
	require.NotNil(t, resp.Collection.GetRackInfo().SiteId)
	assert.Equal(t, newSite, *resp.Collection.GetRackInfo().SiteId)
	assert.Nil(t, resp.Collection.GetRackInfo().BuildingId)
}

// TestService_SaveRack_OmittedPlacementPreservesCurrent covers the
// "legacy save" path: a client that doesn't send site_id / building_id
// (e.g., today's rack-edit modal saving a slot reassignment) must NOT
// have its rack's site silently wiped. We verify the rack lock fires,
// site/building locks are SKIPPED, and UpdateRackPlacement writes the
// existing site_id back idempotently.
func TestService_SaveRack_OmittedPlacementPreservesCurrent(t *testing.T) {
	resolver := func(_ context.Context, _ *commonpb.DeviceSelector, _ int64) ([]string, error) {
		return nil, nil
	}
	svc, mockStore, mockSiteStore := newTestServiceWithSites(t, resolver)
	ctx := testCtx(t)

	collectionID := int64(42)
	existingSite := int64(7)
	existingBuilding := int64(70)

	mockStore.EXPECT().CollectionBelongsToOrg(gomock.Any(), collectionID, testOrgID).Return(true, nil)
	mockStore.EXPECT().GetCollectionType(gomock.Any(), testOrgID, collectionID).Return(pb.CollectionType_COLLECTION_TYPE_RACK, nil)
	// Preserve branch: ONLY the rack lock fires. Site/building locks
	// are NOT expected — the test will fail loudly via the mock
	// controller if SaveRack reaches into mockSiteStore.
	mockStore.EXPECT().LockRackPlacementForWrite(gomock.Any(), collectionID, testOrgID).
		Return(interfaces.RackPlacement{SiteID: &existingSite, BuildingID: &existingBuilding, Zone: "Old Zone"}, nil)

	mockStore.EXPECT().UpdateCollection(gomock.Any(), testOrgID, collectionID, gomock.Any(), (*string)(nil)).Return(nil)
	mockStore.EXPECT().UpdateRackInfo(gomock.Any(), collectionID, "Old Zone", int32(4), int32(8), int32(pb.RackOrderIndex_RACK_ORDER_INDEX_BOTTOM_LEFT), int32(pb.RackCoolingType_RACK_COOLING_TYPE_AIR), testOrgID).Return(nil)
	// UpdateRackPlacement writes the current values back (idempotent).
	mockStore.EXPECT().UpdateRackPlacement(gomock.Any(), collectionID, testOrgID, gomock.Eq(&existingSite), gomock.Eq(&existingBuilding), "Old Zone").Return(nil)

	mockStore.EXPECT().RemoveAllDevicesFromCollection(gomock.Any(), testOrgID, collectionID).Return(int64(0), nil)
	mockStore.EXPECT().GetRackSlots(gomock.Any(), collectionID, testOrgID).Return(nil, nil)
	mockStore.EXPECT().GetCollection(gomock.Any(), testOrgID, collectionID).
		Return(&pb.DeviceCollection{Id: collectionID, Label: "Rack", Type: pb.CollectionType_COLLECTION_TYPE_RACK}, nil)

	// Legacy RackInfo: no SiteId, no BuildingId, just layout fields.
	rackInfo := &pb.RackInfo{
		Rows:        4,
		Columns:     8,
		Zone:        "Old Zone",
		OrderIndex:  pb.RackOrderIndex_RACK_ORDER_INDEX_BOTTOM_LEFT,
		CoolingType: pb.RackCoolingType_RACK_COOLING_TYPE_AIR,
	}
	resp, err := svc.SaveRack(ctx, &pb.SaveRackRequest{
		CollectionId: &collectionID,
		Label:        "Rack",
		RackInfo:     rackInfo,
		DeviceSelector: &commonpb.DeviceSelector{
			SelectionType: &commonpb.DeviceSelector_DeviceList{
				DeviceList: &commonpb.DeviceIdentifierList{DeviceIdentifiers: []string{}},
			},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, resp.Collection.GetRackInfo())
	require.NotNil(t, resp.Collection.GetRackInfo().SiteId)
	assert.Equal(t, existingSite, *resp.Collection.GetRackInfo().SiteId, "site_id preserved across legacy save")
	require.NotNil(t, resp.Collection.GetRackInfo().BuildingId)
	assert.Equal(t, existingBuilding, *resp.Collection.GetRackInfo().BuildingId, "building_id preserved across legacy save")
	assert.Equal(t, "Old Zone", resp.Collection.GetRackInfo().Zone)

	// Suppress unused warning — site store assertion is via no-call.
	_ = mockSiteStore
}

// TestService_SaveRack_OmittedPlacementPreservesZone guards the
// building-zone invariant: when a legacy client sends an omitted
// placement with an empty zone for a rack that's currently in a
// building, the persisted zone must come from the current placement,
// not the empty request field.
func TestService_SaveRack_OmittedPlacementPreservesZone(t *testing.T) {
	resolver := func(_ context.Context, _ *commonpb.DeviceSelector, _ int64) ([]string, error) {
		return nil, nil
	}
	svc, mockStore, mockSiteStore := newTestServiceWithSites(t, resolver)
	ctx := testCtx(t)

	collectionID := int64(43)
	existingSite := int64(7)
	existingBuilding := int64(70)

	mockStore.EXPECT().CollectionBelongsToOrg(gomock.Any(), collectionID, testOrgID).Return(true, nil)
	mockStore.EXPECT().GetCollectionType(gomock.Any(), testOrgID, collectionID).Return(pb.CollectionType_COLLECTION_TYPE_RACK, nil)
	mockStore.EXPECT().LockRackPlacementForWrite(gomock.Any(), collectionID, testOrgID).
		Return(interfaces.RackPlacement{SiteID: &existingSite, BuildingID: &existingBuilding, Zone: "Floor 1"}, nil)

	mockStore.EXPECT().UpdateCollection(gomock.Any(), testOrgID, collectionID, gomock.Any(), (*string)(nil)).Return(nil)
	mockStore.EXPECT().UpdateRackInfo(gomock.Any(), collectionID, "Floor 1", int32(4), int32(8), int32(pb.RackOrderIndex_RACK_ORDER_INDEX_BOTTOM_LEFT), int32(pb.RackCoolingType_RACK_COOLING_TYPE_AIR), testOrgID).Return(nil)
	mockStore.EXPECT().UpdateRackPlacement(gomock.Any(), collectionID, testOrgID, gomock.Eq(&existingSite), gomock.Eq(&existingBuilding), "Floor 1").Return(nil)

	mockStore.EXPECT().RemoveAllDevicesFromCollection(gomock.Any(), testOrgID, collectionID).Return(int64(0), nil)
	mockStore.EXPECT().GetRackSlots(gomock.Any(), collectionID, testOrgID).Return(nil, nil)
	mockStore.EXPECT().GetCollection(gomock.Any(), testOrgID, collectionID).
		Return(&pb.DeviceCollection{Id: collectionID, Label: "Rack", Type: pb.CollectionType_COLLECTION_TYPE_RACK}, nil)

	// Legacy update: no SiteId, no BuildingId, AND empty zone.
	resp, err := svc.SaveRack(ctx, &pb.SaveRackRequest{
		CollectionId: &collectionID,
		Label:        "Rack",
		RackInfo: &pb.RackInfo{
			Rows:        4,
			Columns:     8,
			Zone:        "",
			OrderIndex:  pb.RackOrderIndex_RACK_ORDER_INDEX_BOTTOM_LEFT,
			CoolingType: pb.RackCoolingType_RACK_COOLING_TYPE_AIR,
		},
		DeviceSelector: &commonpb.DeviceSelector{
			SelectionType: &commonpb.DeviceSelector_DeviceList{
				DeviceList: &commonpb.DeviceIdentifierList{DeviceIdentifiers: []string{}},
			},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "Floor 1", resp.Collection.GetRackInfo().Zone, "zone preserved when placement omitted on a building-bound rack")

	_ = mockSiteStore
}

// TestService_AddDevicesToGroup_HappyPath covers the group add flow:
// groups are org-scoped (cross-site allowed) so there is no rack lock,
// no LockSiteForWrite, and no cascade — just the membership insert
// plus an activity event.
func TestService_AddDevicesToGroup_HappyPath(t *testing.T) {
	deviceIDs := []string{"device-1"}
	resolver := func(_ context.Context, _ *commonpb.DeviceSelector, _ int64) ([]string, error) {
		return deviceIDs, nil
	}
	svc, mockStore, _ := newTestServiceWithSites(t, resolver)
	ctx := testCtx(t)

	collectionID := int64(43)
	mockStore.EXPECT().GetCollection(gomock.Any(), testOrgID, collectionID).
		Return(&pb.DeviceCollection{Id: collectionID, Label: "G1", Type: pb.CollectionType_COLLECTION_TYPE_GROUP}, nil)
	mockStore.EXPECT().AddDevicesToCollectionReturningAdded(gomock.Any(), testOrgID, collectionID, deviceIDs).Return(deviceIDs, nil)

	resp, err := svc.AddDevicesToGroup(ctx, AddDevicesToGroupParams{
		TargetGroupID: collectionID,
		DeviceSelector: &commonpb.DeviceSelector{
			SelectionType: &commonpb.DeviceSelector_DeviceList{
				DeviceList: &commonpb.DeviceIdentifierList{DeviceIdentifiers: deviceIDs},
			},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, int64(1), resp.AddedCount)
}

// TestService_AddDevicesToGroup_RejectsRackTarget covers the type
// guard: rack targets must go through AssignDevicesToRack to get the
// atomic prior-rack removal + site cascade. The group endpoint rejects
// rack targets with InvalidArgument before any store mutation.
func TestService_AddDevicesToGroup_RejectsRackTarget(t *testing.T) {
	deviceIDs := []string{"device-1"}
	resolver := func(_ context.Context, _ *commonpb.DeviceSelector, _ int64) ([]string, error) {
		return deviceIDs, nil
	}
	svc, mockStore, _ := newTestServiceWithSites(t, resolver)
	ctx := testCtx(t)

	collectionID := int64(44)
	mockStore.EXPECT().GetCollection(gomock.Any(), testOrgID, collectionID).
		Return(&pb.DeviceCollection{Id: collectionID, Label: "Rack-X", Type: pb.CollectionType_COLLECTION_TYPE_RACK}, nil)

	_, err := svc.AddDevicesToGroup(ctx, AddDevicesToGroupParams{
		TargetGroupID: collectionID,
		DeviceSelector: &commonpb.DeviceSelector{
			SelectionType: &commonpb.DeviceSelector_DeviceList{
				DeviceList: &commonpb.DeviceIdentifierList{DeviceIdentifiers: deviceIDs},
			},
		},
	})
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
}

// TestService_RemoveDevicesFromGroup_HappyPath covers the inverse: a
// straight membership delete with no cascade, on a group target.
func TestService_RemoveDevicesFromGroup_HappyPath(t *testing.T) {
	deviceIDs := []string{"device-1"}
	resolver := func(_ context.Context, _ *commonpb.DeviceSelector, _ int64) ([]string, error) {
		return deviceIDs, nil
	}
	svc, mockStore, _ := newTestServiceWithSites(t, resolver)
	ctx := testCtx(t)

	collectionID := int64(45)
	mockStore.EXPECT().GetCollection(gomock.Any(), testOrgID, collectionID).
		Return(&pb.DeviceCollection{Id: collectionID, Label: "G2", Type: pb.CollectionType_COLLECTION_TYPE_GROUP}, nil)
	mockStore.EXPECT().RemoveDevicesFromCollectionReturningRemoved(gomock.Any(), testOrgID, collectionID, deviceIDs).Return(deviceIDs, nil)

	resp, err := svc.RemoveDevicesFromGroup(ctx, RemoveDevicesFromGroupParams{
		TargetGroupID: collectionID,
		DeviceSelector: &commonpb.DeviceSelector{
			SelectionType: &commonpb.DeviceSelector_DeviceList{
				DeviceList: &commonpb.DeviceIdentifierList{DeviceIdentifiers: deviceIDs},
			},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, int64(1), resp.RemovedCount)
}

// TestService_RemoveDevicesFromGroup_RejectsRackTarget mirrors the add
// path: rack membership is cleared via AssignDevicesToRack with
// target_rack_id unset, not via the group endpoint.
func TestService_RemoveDevicesFromGroup_RejectsRackTarget(t *testing.T) {
	deviceIDs := []string{"device-1"}
	resolver := func(_ context.Context, _ *commonpb.DeviceSelector, _ int64) ([]string, error) {
		return deviceIDs, nil
	}
	svc, mockStore, _ := newTestServiceWithSites(t, resolver)
	ctx := testCtx(t)

	collectionID := int64(46)
	mockStore.EXPECT().GetCollection(gomock.Any(), testOrgID, collectionID).
		Return(&pb.DeviceCollection{Id: collectionID, Label: "Rack-Y", Type: pb.CollectionType_COLLECTION_TYPE_RACK}, nil)

	_, err := svc.RemoveDevicesFromGroup(ctx, RemoveDevicesFromGroupParams{
		TargetGroupID: collectionID,
		DeviceSelector: &commonpb.DeviceSelector{
			SelectionType: &commonpb.DeviceSelector_DeviceList{
				DeviceList: &commonpb.DeviceIdentifierList{DeviceIdentifiers: deviceIDs},
			},
		},
	})
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
}

// TestService_AddDevicesToGroup_StampsSingleSite pins #538: a group
// add whose touched devices all sit in one site stamps that site_id
// (multi_site false) so the event surfaces under /{site}/activity.
func TestService_AddDevicesToGroup_StampsSingleSite(t *testing.T) {
	deviceIDs := []string{"device-1", "device-2"}
	resolver := func(_ context.Context, _ *commonpb.DeviceSelector, _ int64) ([]string, error) {
		return deviceIDs, nil
	}
	svc, mockStore, mockSiteStore, captured := newTestServiceWithSitesRecordingActivity(t, resolver)
	ctx := testCtx(t)

	collectionID := int64(50)
	siteA := int64(7)
	mockStore.EXPECT().GetCollection(gomock.Any(), testOrgID, collectionID).
		Return(&pb.DeviceCollection{Id: collectionID, Label: "G1", Type: pb.CollectionType_COLLECTION_TYPE_GROUP}, nil)
	mockStore.EXPECT().AddDevicesToCollectionReturningAdded(gomock.Any(), testOrgID, collectionID, deviceIDs).Return(deviceIDs, nil)
	mockSiteStore.EXPECT().GetDistinctDeviceSiteIDs(gomock.Any(), testOrgID, deviceIDs).
		Return([]*int64{&siteA}, nil)

	_, err := svc.AddDevicesToGroup(ctx, AddDevicesToGroupParams{
		TargetGroupID: collectionID,
		DeviceSelector: &commonpb.DeviceSelector{
			SelectionType: &commonpb.DeviceSelector_DeviceList{
				DeviceList: &commonpb.DeviceIdentifierList{DeviceIdentifiers: deviceIDs},
			},
		},
	})
	require.NoError(t, err)

	require.Len(t, *captured, 1)
	event := (*captured)[0]
	assert.Equal(t, "add_devices", event.Type)
	require.NotNil(t, event.SiteID, "single-site group add must stamp site_id")
	assert.Equal(t, siteA, *event.SiteID)
	assert.False(t, event.MultiSite)
}

// TestService_AddDevicesToGroup_MarksMultiSite pins the cross-site half
// of #538: a group add spanning sites carries no single site_id and is
// marked multi_site so it stays out of the /unassigned bucket (all-sites
// feed only).
func TestService_AddDevicesToGroup_MarksMultiSite(t *testing.T) {
	deviceIDs := []string{"device-1", "device-2"}
	resolver := func(_ context.Context, _ *commonpb.DeviceSelector, _ int64) ([]string, error) {
		return deviceIDs, nil
	}
	svc, mockStore, mockSiteStore, captured := newTestServiceWithSitesRecordingActivity(t, resolver)
	ctx := testCtx(t)

	collectionID := int64(51)
	siteA := int64(7)
	siteB := int64(8)
	mockStore.EXPECT().GetCollection(gomock.Any(), testOrgID, collectionID).
		Return(&pb.DeviceCollection{Id: collectionID, Label: "G2", Type: pb.CollectionType_COLLECTION_TYPE_GROUP}, nil)
	mockStore.EXPECT().AddDevicesToCollectionReturningAdded(gomock.Any(), testOrgID, collectionID, deviceIDs).Return(deviceIDs, nil)
	mockSiteStore.EXPECT().GetDistinctDeviceSiteIDs(gomock.Any(), testOrgID, deviceIDs).
		Return([]*int64{&siteA, &siteB}, nil)

	_, err := svc.AddDevicesToGroup(ctx, AddDevicesToGroupParams{
		TargetGroupID: collectionID,
		DeviceSelector: &commonpb.DeviceSelector{
			SelectionType: &commonpb.DeviceSelector_DeviceList{
				DeviceList: &commonpb.DeviceIdentifierList{DeviceIdentifiers: deviceIDs},
			},
		},
	})
	require.NoError(t, err)

	require.Len(t, *captured, 1)
	event := (*captured)[0]
	assert.Equal(t, "add_devices", event.Type)
	assert.Nil(t, event.SiteID, "cross-site group add must not stamp a single site_id")
	assert.True(t, event.MultiSite)
	assert.ElementsMatch(t, []int64{siteA, siteB}, event.MemberSiteIDs,
		"cross-site group add records membership for each touched site")
	assert.False(t, event.TouchesUnassigned)
}

// TestService_AddDevicesToGroup_ScopesToChangedMembersOnly pins the Codex P2
// follow-up: when the request includes a no-op identifier in another site
// (already a member), the activity scope is resolved from the devices whose
// membership actually changed, not the full requested set — so the event does
// NOT get pulled into the no-op device's site (#538).
func TestService_AddDevicesToGroup_ScopesToChangedMembersOnly(t *testing.T) {
	requested := []string{"device-A-new", "device-B-existing"}
	changed := []string{"device-A-new"} // device-B-existing was already a member (ON CONFLICT DO NOTHING)
	resolver := func(_ context.Context, _ *commonpb.DeviceSelector, _ int64) ([]string, error) {
		return requested, nil
	}
	svc, mockStore, mockSiteStore, captured := newTestServiceWithSitesRecordingActivity(t, resolver)
	ctx := testCtx(t)

	collectionID := int64(52)
	siteA := int64(7)
	mockStore.EXPECT().GetCollection(gomock.Any(), testOrgID, collectionID).
		Return(&pb.DeviceCollection{Id: collectionID, Label: "G3", Type: pb.CollectionType_COLLECTION_TYPE_GROUP}, nil)
	mockStore.EXPECT().AddDevicesToCollectionReturningAdded(gomock.Any(), testOrgID, collectionID, requested).
		Return(changed, nil)
	// Scope must be resolved from the CHANGED set, not the requested set: the
	// already-member site-B device must not appear here.
	mockSiteStore.EXPECT().GetDistinctDeviceSiteIDs(gomock.Any(), testOrgID, changed).
		Return([]*int64{&siteA}, nil)

	resp, err := svc.AddDevicesToGroup(ctx, AddDevicesToGroupParams{
		TargetGroupID: collectionID,
		DeviceSelector: &commonpb.DeviceSelector{
			SelectionType: &commonpb.DeviceSelector_DeviceList{
				DeviceList: &commonpb.DeviceIdentifierList{DeviceIdentifiers: requested},
			},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, int64(1), resp.AddedCount, "count reflects only the newly-added device")

	require.Len(t, *captured, 1)
	event := (*captured)[0]
	require.NotNil(t, event.SiteID, "scope follows the one changed device's site")
	assert.Equal(t, siteA, *event.SiteID)
	assert.False(t, event.MultiSite, "the no-op site-B device must not make this multi-site")
}

// TestService_AssignDevicesToRack_atomicReassign covers the issue
// #420 atomic-rack-reassign happy path: the prior rack membership is
// cleared and the new membership written inside one transaction, with
// the cascade firing when the target rack has a stamped site.
func TestService_AssignDevicesToRack_atomicReassign(t *testing.T) {
	svc, mockStore, _ := newTestServiceWithSites(t, nil)
	ctx := testCtx(t)

	targetRackID := int64(42)
	rackSite := int64(7)
	deviceIDs := []string{"d1", "d2"}

	gomock.InOrder(
		mockStore.EXPECT().LockRacksForReparent(gomock.Any(), testOrgID, deviceIDs, targetRackID).
			Return([]int64{11, 23}, nil),
		mockStore.EXPECT().LockRackPlacementForWrite(gomock.Any(), targetRackID, testOrgID).
			Return(interfaces.RackPlacement{SiteID: &rackSite}, nil),
		mockStore.EXPECT().GetCollection(gomock.Any(), testOrgID, targetRackID).
			Return(&pb.DeviceCollection{Id: targetRackID, Label: "Rack-B", Type: pb.CollectionType_COLLECTION_TYPE_RACK}, nil),
		mockStore.EXPECT().RemoveDevicesFromAnyRack(gomock.Any(), testOrgID, deviceIDs, targetRackID).Return(int64(2), nil),
		mockStore.EXPECT().AddDevicesToCollection(gomock.Any(), testOrgID, targetRackID, deviceIDs).Return(int64(2), nil),
		mockStore.EXPECT().GetDeviceSiteIDsByMembership(gomock.Any(), targetRackID, testOrgID).
			Return(map[string]*int64{"d1": int64Ptr(99), "d2": &rackSite}, nil),
		mockStore.EXPECT().CascadeAddedDeviceSites(gomock.Any(), testOrgID, targetRackID, deviceIDs).Return(int64(1), nil),
		mockStore.EXPECT().CascadeAddedDeviceBuildings(gomock.Any(), testOrgID, targetRackID, deviceIDs).Return(int64(0), nil),
	)

	out, err := svc.AssignDevicesToRack(ctx, AssignDevicesToRackParams{
		OrgID:             testOrgID,
		TargetRackID:      &targetRackID,
		DeviceIdentifiers: deviceIDs,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(2), out.AssignedCount)
	assert.Equal(t, int64(2), out.RemovedCount)
	assert.Equal(t, int64(1), out.SiteReassignedCount)
}

// TestService_AssignDevicesToRack_unassignClearsWithoutAdd covers the
// target_rack_id-unset branch: removes prior rack membership without
// touching site/building. No GetCollection/Lock/AddDevices call.
func TestService_AssignDevicesToRack_unassignClearsWithoutAdd(t *testing.T) {
	svc, mockStore, _ := newTestServiceWithSites(t, nil)
	ctx := testCtx(t)

	deviceIDs := []string{"d1"}
	gomock.InOrder(
		mockStore.EXPECT().LockRacksForReparent(gomock.Any(), testOrgID, deviceIDs, int64(0)).
			Return([]int64{17}, nil),
		mockStore.EXPECT().RemoveDevicesFromAnyRack(gomock.Any(), testOrgID, deviceIDs, int64(0)).Return(int64(1), nil),
	)

	out, err := svc.AssignDevicesToRack(ctx, AssignDevicesToRackParams{
		OrgID:             testOrgID,
		TargetRackID:      nil,
		DeviceIdentifiers: deviceIDs,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(0), out.AssignedCount)
	assert.Equal(t, int64(1), out.RemovedCount)
	assert.Equal(t, int64(0), out.SiteReassignedCount)
}

// TestService_AssignDevicesToRack_targetMustBeRack rejects when the
// target collection exists but isn't a rack.
func TestService_AssignDevicesToRack_targetMustBeRack(t *testing.T) {
	svc, mockStore, _ := newTestServiceWithSites(t, nil)
	ctx := testCtx(t)

	targetID := int64(99)
	mockStore.EXPECT().LockRacksForReparent(gomock.Any(), testOrgID, []string{"d1"}, targetID).
		Return(nil, nil)
	mockStore.EXPECT().LockRackPlacementForWrite(gomock.Any(), targetID, testOrgID).
		Return(interfaces.RackPlacement{}, nil)
	mockStore.EXPECT().GetCollection(gomock.Any(), testOrgID, targetID).
		Return(&pb.DeviceCollection{Id: targetID, Label: "G1", Type: pb.CollectionType_COLLECTION_TYPE_GROUP}, nil)

	_, err := svc.AssignDevicesToRack(ctx, AssignDevicesToRackParams{
		OrgID:             testOrgID,
		TargetRackID:      &targetID,
		DeviceIdentifiers: []string{"d1"},
	})
	require.Error(t, err)
}

// TestService_AssignDevicesToRack_acquiresSourceRackLocksBeforeWrites
// pins F9's lock-order invariant: LockRacksForReparent must run
// BEFORE LockRackPlacementForWrite and BEFORE RemoveDevicesFromAnyRack.
// Reversing this risks deadlock against a concurrent call that takes
// the locks in the opposite order, and skipping it altogether is what
// lets concurrent overlapping reparent calls race the
// device_set_membership unique constraint.
func TestService_AssignDevicesToRack_acquiresSourceRackLocksBeforeWrites(t *testing.T) {
	svc, mockStore, _ := newTestServiceWithSites(t, nil)
	ctx := testCtx(t)

	targetRackID := int64(42)
	deviceIDs := []string{"d1", "d2"}

	gomock.InOrder(
		mockStore.EXPECT().LockRacksForReparent(gomock.Any(), testOrgID, deviceIDs, targetRackID).
			Return([]int64{11, 23}, nil),
		mockStore.EXPECT().LockRackPlacementForWrite(gomock.Any(), targetRackID, testOrgID).
			Return(interfaces.RackPlacement{}, nil),
		mockStore.EXPECT().GetCollection(gomock.Any(), testOrgID, targetRackID).
			Return(&pb.DeviceCollection{Id: targetRackID, Label: "Rack-B", Type: pb.CollectionType_COLLECTION_TYPE_RACK}, nil),
		mockStore.EXPECT().FindDevicesWithSiteOrBuilding(gomock.Any(), testOrgID, deviceIDs).Return(nil, nil),
		mockStore.EXPECT().RemoveDevicesFromAnyRack(gomock.Any(), testOrgID, deviceIDs, targetRackID).Return(int64(2), nil),
		mockStore.EXPECT().AddDevicesToCollection(gomock.Any(), testOrgID, targetRackID, deviceIDs).Return(int64(2), nil),
		mockStore.EXPECT().CascadeAddedDeviceBuildings(gomock.Any(), testOrgID, targetRackID, deviceIDs).Return(int64(0), nil),
		mockStore.EXPECT().ClearDeviceSitesAndBuildings(gomock.Any(), testOrgID, deviceIDs).Return(int64(0), nil),
	)

	_, err := svc.AssignDevicesToRack(ctx, AssignDevicesToRackParams{
		OrgID:             testOrgID,
		TargetRackID:      &targetRackID,
		DeviceIdentifiers: deviceIDs,
	})
	require.NoError(t, err)
}

// TestService_AssignDevicesToRack_unassignPathLocksSourceRacks pins
// that the rack-lock pre-pass ALSO fires on the clear-rack path.
// targetRackID is 0 so the UNION arm contributes no target row and
// only the source racks holding any of the requested devices are
// locked.
func TestService_AssignDevicesToRack_unassignPathLocksSourceRacks(t *testing.T) {
	svc, mockStore, _ := newTestServiceWithSites(t, nil)
	ctx := testCtx(t)

	deviceIDs := []string{"d1", "d2"}

	gomock.InOrder(
		mockStore.EXPECT().LockRacksForReparent(gomock.Any(), testOrgID, deviceIDs, int64(0)).
			Return([]int64{5, 12}, nil),
		mockStore.EXPECT().RemoveDevicesFromAnyRack(gomock.Any(), testOrgID, deviceIDs, int64(0)).Return(int64(2), nil),
	)

	_, err := svc.AssignDevicesToRack(ctx, AssignDevicesToRackParams{
		OrgID:             testOrgID,
		TargetRackID:      nil,
		DeviceIdentifiers: deviceIDs,
	})
	require.NoError(t, err)
}

// TestService_AssignDevicesToRack_locksIncludeTargetRack pins the
// deadlock-prevention contract: when a target rack is supplied, the
// pre-pass lock call MUST receive the target rack id (not 0) so the
// SQL UNION includes the target in the same globally sorted FOR
// UPDATE acquisition as the source racks. Two concurrent reparents
// moving devices in opposite directions between rack A and rack B
// would otherwise lock {sourceA} then {B} in one tx and {sourceB}
// then {A} in the other, producing the classic A→B / B→A deadlock.
func TestService_AssignDevicesToRack_locksIncludeTargetRack(t *testing.T) {
	svc, mockStore, _ := newTestServiceWithSites(t, nil)
	ctx := testCtx(t)

	targetRackID := int64(77)
	deviceIDs := []string{"d1"}

	// Assert by argument: LockRacksForReparent receives targetRackID,
	// not 0. The mock matcher rejects any other value.
	mockStore.EXPECT().LockRacksForReparent(gomock.Any(), testOrgID, deviceIDs, targetRackID).
		Return([]int64{77}, nil)
	mockStore.EXPECT().LockRackPlacementForWrite(gomock.Any(), targetRackID, testOrgID).
		Return(interfaces.RackPlacement{}, nil)
	mockStore.EXPECT().GetCollection(gomock.Any(), testOrgID, targetRackID).
		Return(&pb.DeviceCollection{Id: targetRackID, Label: "Rack-Target", Type: pb.CollectionType_COLLECTION_TYPE_RACK}, nil)
	// Site-less target rack → site-consistency pre-check + post-add strip.
	mockStore.EXPECT().FindDevicesWithSiteOrBuilding(gomock.Any(), testOrgID, deviceIDs).Return(nil, nil)
	mockStore.EXPECT().RemoveDevicesFromAnyRack(gomock.Any(), testOrgID, deviceIDs, targetRackID).Return(int64(0), nil)
	mockStore.EXPECT().AddDevicesToCollection(gomock.Any(), testOrgID, targetRackID, deviceIDs).Return(int64(1), nil)
	mockStore.EXPECT().CascadeAddedDeviceBuildings(gomock.Any(), testOrgID, targetRackID, deviceIDs).Return(int64(0), nil)
	mockStore.EXPECT().ClearDeviceSitesAndBuildings(gomock.Any(), testOrgID, deviceIDs).Return(int64(0), nil)

	_, err := svc.AssignDevicesToRack(ctx, AssignDevicesToRackParams{
		OrgID:             testOrgID,
		TargetRackID:      &targetRackID,
		DeviceIdentifiers: deviceIDs,
	})
	require.NoError(t, err)
}

// TestService_AssignDevicesToRack_siteLessRackConflictNoForce pins the
// device→site consistency guard for the add-to-rack path: adding a miner
// that has a site into a site-less rack would strip its site, so without
// force the batch returns the conflict and writes NOTHING.
func TestService_AssignDevicesToRack_siteLessRackConflictNoForce(t *testing.T) {
	svc, mockStore, _ := newTestServiceWithSites(t, nil)
	ctx := testCtx(t)

	targetRackID := int64(77)
	deviceIDs := []string{"d1"}

	mockStore.EXPECT().LockRacksForReparent(gomock.Any(), testOrgID, deviceIDs, targetRackID).Return([]int64{77}, nil)
	mockStore.EXPECT().LockRackPlacementForWrite(gomock.Any(), targetRackID, testOrgID).Return(interfaces.RackPlacement{}, nil)
	mockStore.EXPECT().GetCollection(gomock.Any(), testOrgID, targetRackID).
		Return(&pb.DeviceCollection{Id: targetRackID, Label: "Rack-Target", Type: pb.CollectionType_COLLECTION_TYPE_RACK}, nil)
	// d1 currently has a site → would be stripped. No force → reject,
	// no RemoveDevicesFromAnyRack / AddDevicesToCollection.
	mockStore.EXPECT().FindDevicesWithSiteOrBuilding(gomock.Any(), testOrgID, deviceIDs).Return([]string{"d1"}, nil)

	res, err := svc.AssignDevicesToRack(ctx, AssignDevicesToRackParams{
		OrgID:             testOrgID,
		TargetRackID:      &targetRackID,
		DeviceIdentifiers: deviceIDs,
	})
	require.NoError(t, err)
	require.Len(t, res.Conflicts, 1)
	require.Equal(t, "d1", res.Conflicts[0].DeviceIdentifier)
	require.Equal(t, RackConflictReasonDeviceLosesSite, res.Conflicts[0].Reason)
	require.Zero(t, res.AssignedCount)
}

// TestService_AssignDevicesToRack_siteLessRackForceStrips pins the force
// path: the miner joins the site-less rack and its site/building are
// stripped to match, keeping device→site consistency.
func TestService_AssignDevicesToRack_siteLessRackForceStrips(t *testing.T) {
	svc, mockStore, _ := newTestServiceWithSites(t, nil)
	ctx := testCtx(t)

	targetRackID := int64(77)
	deviceIDs := []string{"d1"}

	gomock.InOrder(
		mockStore.EXPECT().LockRacksForReparent(gomock.Any(), testOrgID, deviceIDs, targetRackID).Return([]int64{77}, nil),
		mockStore.EXPECT().LockRackPlacementForWrite(gomock.Any(), targetRackID, testOrgID).Return(interfaces.RackPlacement{}, nil),
		mockStore.EXPECT().GetCollection(gomock.Any(), testOrgID, targetRackID).
			Return(&pb.DeviceCollection{Id: targetRackID, Label: "Rack-Target", Type: pb.CollectionType_COLLECTION_TYPE_RACK}, nil),
		mockStore.EXPECT().FindDevicesWithSiteOrBuilding(gomock.Any(), testOrgID, deviceIDs).Return([]string{"d1"}, nil),
		mockStore.EXPECT().RemoveDevicesFromAnyRack(gomock.Any(), testOrgID, deviceIDs, targetRackID).Return(int64(0), nil),
		mockStore.EXPECT().AddDevicesToCollection(gomock.Any(), testOrgID, targetRackID, deviceIDs).Return(int64(1), nil),
		mockStore.EXPECT().CascadeAddedDeviceBuildings(gomock.Any(), testOrgID, targetRackID, deviceIDs).Return(int64(0), nil),
		mockStore.EXPECT().ClearDeviceSitesAndBuildings(gomock.Any(), testOrgID, deviceIDs).Return(int64(1), nil),
	)

	res, err := svc.AssignDevicesToRack(ctx, AssignDevicesToRackParams{
		OrgID:                     testOrgID,
		TargetRackID:              &targetRackID,
		DeviceIdentifiers:         deviceIDs,
		ForceClearConflictingSite: true,
	})
	require.NoError(t, err)
	require.Empty(t, res.Conflicts)
	require.Equal(t, int64(1), res.AssignedCount)
	require.Equal(t, int64(1), res.SiteReassignedCount)
}

// TestService_AssignDevicesToRack_emptyDevicesRejected guards the
// empty-input edge so callers get InvalidArgument instead of a
// silent 0-row response.
func TestService_AssignDevicesToRack_emptyDevicesRejected(t *testing.T) {
	svc, _, _ := newTestServiceWithSites(t, nil)
	ctx := testCtx(t)

	_, err := svc.AssignDevicesToRack(ctx, AssignDevicesToRackParams{
		OrgID:             testOrgID,
		DeviceIdentifiers: nil,
	})
	require.Error(t, err)
}

// TestService_AssignDevicesToRack_crossSiteEmitsCascadeMetadata asserts
// that when the target rack lives at a different site than (some of) the
// devices, the activity event carries SiteID + cascade Metadata mirroring
// the CreateCollection cascade-audit shape — preserving the audit trail
// for slot-search rack moves and any flow where the reparent crosses
// sites.
func TestService_AssignDevicesToRack_crossSiteEmitsCascadeMetadata(t *testing.T) {
	svc, mockStore, _, captured := newTestServiceWithSitesRecordingActivity(t, nil)
	ctx := testCtx(t)

	targetRackID := int64(42)
	rackSite := int64(8)
	priorSite := int64(7)
	deviceIDs := []string{"d1"}

	gomock.InOrder(
		mockStore.EXPECT().LockRacksForReparent(gomock.Any(), testOrgID, deviceIDs, targetRackID).
			Return([]int64{targetRackID}, nil),
		mockStore.EXPECT().LockRackPlacementForWrite(gomock.Any(), targetRackID, testOrgID).
			Return(interfaces.RackPlacement{SiteID: &rackSite}, nil),
		mockStore.EXPECT().GetCollection(gomock.Any(), testOrgID, targetRackID).
			Return(&pb.DeviceCollection{Id: targetRackID, Label: "Rack-B", Type: pb.CollectionType_COLLECTION_TYPE_RACK}, nil),
		mockStore.EXPECT().RemoveDevicesFromAnyRack(gomock.Any(), testOrgID, deviceIDs, targetRackID).Return(int64(1), nil),
		mockStore.EXPECT().AddDevicesToCollection(gomock.Any(), testOrgID, targetRackID, deviceIDs).Return(int64(1), nil),
		mockStore.EXPECT().GetDeviceSiteIDsByMembership(gomock.Any(), targetRackID, testOrgID).
			Return(map[string]*int64{"d1": &priorSite}, nil),
		mockStore.EXPECT().CascadeAddedDeviceSites(gomock.Any(), testOrgID, targetRackID, deviceIDs).Return(int64(1), nil),
		mockStore.EXPECT().CascadeAddedDeviceBuildings(gomock.Any(), testOrgID, targetRackID, deviceIDs).Return(int64(0), nil),
	)

	_, err := svc.AssignDevicesToRack(ctx, AssignDevicesToRackParams{
		OrgID:             testOrgID,
		TargetRackID:      &targetRackID,
		DeviceIdentifiers: deviceIDs,
	})
	require.NoError(t, err)

	require.Len(t, *captured, 1, "expected exactly one assign_devices_to_rack event")
	event := (*captured)[0]
	assert.Equal(t, "assign_devices_to_rack", event.Type)
	require.NotNil(t, event.SiteID, "activity event must carry final site_id")
	assert.Equal(t, rackSite, *event.SiteID)
	require.NotNil(t, event.Metadata, "cross-site reassignments must populate metadata")
	assert.Equal(t, true, event.Metadata["site_cascade"])
	assert.Equal(t, int64(1), event.Metadata["site_reassigned_count"])
	assert.Equal(t, 1, event.Metadata["total_affected"])
	_, truncated := event.Metadata["truncated"]
	assert.False(t, truncated, "single-device change should not be truncated")
	changes, ok := event.Metadata["device_site_changes"].([]map[string]any)
	require.True(t, ok, "device_site_changes must be present and typed")
	require.Len(t, changes, 1)
	assert.Equal(t, "d1", changes[0]["device_identifier"])
	assert.Equal(t, priorSite, changes[0]["prior_site_id"])
	assert.Equal(t, rackSite, changes[0]["target_site_id"])
}

// TestService_AssignDevicesToRack_sameSiteSkipsCascadeMetadata asserts
// the no-op cascade case: when every device already sits at the target
// rack's site, the event records SiteID but omits cascade-specific
// metadata keys so audit consumers can distinguish "implicit move"
// from "membership-only change".
func TestService_AssignDevicesToRack_sameSiteSkipsCascadeMetadata(t *testing.T) {
	svc, mockStore, _, captured := newTestServiceWithSitesRecordingActivity(t, nil)
	ctx := testCtx(t)

	targetRackID := int64(42)
	rackSite := int64(8)
	deviceIDs := []string{"d1"}

	gomock.InOrder(
		mockStore.EXPECT().LockRacksForReparent(gomock.Any(), testOrgID, deviceIDs, targetRackID).
			Return([]int64{targetRackID}, nil),
		mockStore.EXPECT().LockRackPlacementForWrite(gomock.Any(), targetRackID, testOrgID).
			Return(interfaces.RackPlacement{SiteID: &rackSite}, nil),
		mockStore.EXPECT().GetCollection(gomock.Any(), testOrgID, targetRackID).
			Return(&pb.DeviceCollection{Id: targetRackID, Label: "Rack-B", Type: pb.CollectionType_COLLECTION_TYPE_RACK}, nil),
		mockStore.EXPECT().RemoveDevicesFromAnyRack(gomock.Any(), testOrgID, deviceIDs, targetRackID).Return(int64(0), nil),
		mockStore.EXPECT().AddDevicesToCollection(gomock.Any(), testOrgID, targetRackID, deviceIDs).Return(int64(1), nil),
		mockStore.EXPECT().GetDeviceSiteIDsByMembership(gomock.Any(), targetRackID, testOrgID).
			Return(map[string]*int64{"d1": &rackSite}, nil),
		mockStore.EXPECT().CascadeAddedDeviceSites(gomock.Any(), testOrgID, targetRackID, deviceIDs).Return(int64(0), nil),
		mockStore.EXPECT().CascadeAddedDeviceBuildings(gomock.Any(), testOrgID, targetRackID, deviceIDs).Return(int64(0), nil),
	)

	_, err := svc.AssignDevicesToRack(ctx, AssignDevicesToRackParams{
		OrgID:             testOrgID,
		TargetRackID:      &targetRackID,
		DeviceIdentifiers: deviceIDs,
	})
	require.NoError(t, err)

	require.Len(t, *captured, 1)
	event := (*captured)[0]
	assert.Equal(t, "assign_devices_to_rack", event.Type)
	require.NotNil(t, event.SiteID)
	assert.Equal(t, rackSite, *event.SiteID)
	assert.Nil(t, event.Metadata, "no cascade should mean no cascade metadata")
}

// TestService_AssignDevicesToRack_siteLessBuildingClearsDeviceSite pins
// the cross-scope-consistency fix: when the target rack sits in an
// unassigned building (building_id set, site_id NULL), the site cascade
// must still fire — cascading device.site_id to NULL — so a device
// moved in from another site doesn't end up with a building_id that
// disagrees with a stale site_id. Without the fix the site cascade was
// gated on targetSiteID != nil and skipped, leaving device.site_id
// pointing at the old site while building_id pointed at a site-less
// building.
func TestService_AssignDevicesToRack_siteLessBuildingClearsDeviceSite(t *testing.T) {
	svc, mockStore, _ := newTestServiceWithSites(t, nil)
	ctx := testCtx(t)

	targetRackID := int64(42)
	rackBuilding := int64(70)
	priorSite := int64(8)
	deviceIDs := []string{"d1"}

	gomock.InOrder(
		mockStore.EXPECT().LockRacksForReparent(gomock.Any(), testOrgID, deviceIDs, targetRackID).
			Return([]int64{targetRackID}, nil),
		// Rack is in a building but has no site (unassigned building).
		mockStore.EXPECT().LockRackPlacementForWrite(gomock.Any(), targetRackID, testOrgID).
			Return(interfaces.RackPlacement{SiteID: nil, BuildingID: &rackBuilding}, nil),
		mockStore.EXPECT().GetCollection(gomock.Any(), testOrgID, targetRackID).
			Return(&pb.DeviceCollection{Id: targetRackID, Label: "Rack-B", Type: pb.CollectionType_COLLECTION_TYPE_RACK}, nil),
		mockStore.EXPECT().RemoveDevicesFromAnyRack(gomock.Any(), testOrgID, deviceIDs, targetRackID).Return(int64(1), nil),
		mockStore.EXPECT().AddDevicesToCollection(gomock.Any(), testOrgID, targetRackID, deviceIDs).Return(int64(1), nil),
		// Site cascade now fires even though targetSiteID is nil, because
		// the rack has a building. CascadeAddedDeviceSites nulls
		// device.site_id for the site-less-building rack.
		mockStore.EXPECT().GetDeviceSiteIDsByMembership(gomock.Any(), targetRackID, testOrgID).
			Return(map[string]*int64{"d1": &priorSite}, nil),
		mockStore.EXPECT().CascadeAddedDeviceSites(gomock.Any(), testOrgID, targetRackID, deviceIDs).Return(int64(1), nil),
		mockStore.EXPECT().CascadeAddedDeviceBuildings(gomock.Any(), testOrgID, targetRackID, deviceIDs).Return(int64(1), nil),
	)

	_, err := svc.AssignDevicesToRack(ctx, AssignDevicesToRackParams{
		OrgID:             testOrgID,
		TargetRackID:      &targetRackID,
		DeviceIdentifiers: deviceIDs,
	})
	require.NoError(t, err)
}

// TestService_AssignDevicesToRack_lockReparentZeroTargetReturnsOnlySources
// pins the UNION semantics of LockRacksForReparent on the clear-rack
// path: with target_rack_id=0 the SQL UNION's target arm evaluates to
// no rows (the `target_rack_id > 0` predicate filters it out), so the
// store should return only the source rack ids that actually own the
// requested devices. The service must accept that result and proceed
// without dereferencing a target rack id.
func TestService_AssignDevicesToRack_lockReparentZeroTargetReturnsOnlySources(t *testing.T) {
	svc, mockStore, _ := newTestServiceWithSites(t, nil)
	ctx := testCtx(t)

	deviceIDs := []string{"d1", "d2"}

	// gomock matchers on the call args pin the wrapper contract:
	// targetRackID 0 is what the service passes when TargetRackID is
	// nil, and the mock returns only source ids — no target row
	// because the UNION arm is filtered out by `target_rack_id > 0`.
	gomock.InOrder(
		mockStore.EXPECT().LockRacksForReparent(gomock.Any(), testOrgID, deviceIDs, int64(0)).
			Return([]int64{3, 9}, nil),
		mockStore.EXPECT().RemoveDevicesFromAnyRack(gomock.Any(), testOrgID, deviceIDs, int64(0)).Return(int64(2), nil),
	)

	out, err := svc.AssignDevicesToRack(ctx, AssignDevicesToRackParams{
		OrgID:             testOrgID,
		TargetRackID:      nil,
		DeviceIdentifiers: deviceIDs,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(2), out.RemovedCount)
}

// TestService_AssignDevicesToRack_lockReparentCrossOrgTargetReturnsEmpty
// pins the cross-org isolation contract: when the caller supplies a
// target_rack_id that belongs to a different org, the SQL
// `ds.org_id = @org_id` predicate filters the target out of the
// locking SELECT and the store returns an empty id slice. The follow
// -up LockRackPlacementForWrite call still fires for the supplied
// target id and is responsible for returning NotFound — the empty
// LockRacksForReparent result must not be treated as a leak (the test
// asserts no devices are added and a NotFound surfaces from the
// downstream placement lock, never from a partial commit).
func TestService_AssignDevicesToRack_lockReparentCrossOrgTargetReturnsEmpty(t *testing.T) {
	svc, mockStore, _ := newTestServiceWithSites(t, nil)
	ctx := testCtx(t)

	targetRackID := int64(9999) // cross-org id
	deviceIDs := []string{"d1"}

	gomock.InOrder(
		// Cross-org target id: the SQL UNION includes the id but the
		// org-scoped outer WHERE filters it out, so the mock returns
		// an empty slice. The wrapper must still forward the call as
		// the service sent it (targetRackID, not 0).
		mockStore.EXPECT().LockRacksForReparent(gomock.Any(), testOrgID, deviceIDs, targetRackID).
			Return([]int64{}, nil),
		// LockRackPlacementForWrite is the gate that owns the cross
		// -org NotFound — the empty slice from LockRacksForReparent
		// alone must not let the call slip past the lock pre-pass and
		// reach a write.
		mockStore.EXPECT().LockRackPlacementForWrite(gomock.Any(), targetRackID, testOrgID).
			Return(interfaces.RackPlacement{}, fleeterror.NewNotFoundErrorf("rack %d not found", targetRackID)),
	)

	_, err := svc.AssignDevicesToRack(ctx, AssignDevicesToRackParams{
		OrgID:             testOrgID,
		TargetRackID:      &targetRackID,
		DeviceIdentifiers: deviceIDs,
	})
	require.Error(t, err)
}
