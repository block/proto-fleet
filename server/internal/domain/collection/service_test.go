package collection

import (
	"context"
	"testing"

	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/collection/v1"
	commonpb "github.com/btc-mining/proto-fleet/server/generated/grpc/common/v1"
	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"
	"github.com/btc-mining/proto-fleet/server/internal/domain/stores/interfaces/mocks"
	"github.com/btc-mining/proto-fleet/server/internal/testutil"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testOrgID        = int64(1)
	testUserID       = int64(100)
	testCollectionID = int64(42)
)

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

	svc := NewService(mockStore, mockTransactor, noopResolver)
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
	svc := NewService(mockStore, mockTransactor, resolver)

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
	svc := NewService(mockStore, mockTransactor, resolver)

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
	svc := NewService(mockStore, mockTransactor, resolver)

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
	svc := NewService(mockStore, mockTransactor, resolver)

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
	svc := NewService(mockStore, mockTransactor, resolver)

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
