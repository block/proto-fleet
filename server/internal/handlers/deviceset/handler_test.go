package deviceset

import (
	"context"
	"errors"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	collectionpb "github.com/block/proto-fleet/server/generated/grpc/collection/v1"
	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	dspb "github.com/block/proto-fleet/server/generated/grpc/device_set/v1"
	"github.com/block/proto-fleet/server/internal/domain/activity"
	"github.com/block/proto-fleet/server/internal/domain/collection"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
	"github.com/block/proto-fleet/server/internal/testutil"
)

const (
	testOrgID  = int64(1)
	testUserID = int64(100)
)

// testHarness wires the deviceset.Handler against a real
// collection.Service backed by mock stores so we exercise the full
// request → service → store path. The buildingStore is needed for the
// cross-org filter validation in ListDeviceSets.
type testHarness struct {
	handler         *Handler
	collectionStore *mocks.MockCollectionStore
	buildingStore   *mocks.MockBuildingStore
	ctrl            *gomock.Controller
}

func newTestHandler(t *testing.T) *testHarness {
	t.Helper()
	ctrl := gomock.NewController(t)

	collectionStore := mocks.NewMockCollectionStore(ctrl)
	buildingStore := mocks.NewMockBuildingStore(ctrl)
	tx := mocks.NewMockTransactor(ctrl)
	tx.EXPECT().RunInTx(gomock.Any(), gomock.Any()).AnyTimes().DoAndReturn(
		func(ctx context.Context, fn func(context.Context) error) error {
			return fn(ctx)
		},
	)
	tx.EXPECT().RunInTxWithResult(gomock.Any(), gomock.Any()).AnyTimes().DoAndReturn(
		func(ctx context.Context, fn func(context.Context) (any, error)) (any, error) {
			return fn(ctx)
		},
	)

	activityStore := mocks.NewMockActivityStore(ctrl)
	activityStore.EXPECT().Insert(gomock.Any(), gomock.Any()).AnyTimes().Return(nil)
	activitySvc := activity.NewService(activityStore)

	noopResolver := func(_ context.Context, _ *commonpb.DeviceSelector, _ int64) ([]string, error) {
		return nil, nil
	}

	svc := collection.NewService(
		collectionStore,
		nil, // deviceQueryer: unused in these tests
		nil, // siteStore: unused
		buildingStore,
		tx,
		noopResolver,
		nil, // telemetry: unused
		activitySvc,
	)

	return &testHarness{
		handler:         NewHandler(svc),
		collectionStore: collectionStore,
		buildingStore:   buildingStore,
		ctrl:            ctrl,
	}
}

func testCtx(t *testing.T) context.Context {
	t.Helper()
	return testutil.MockAuthContextForTesting(t.Context(), testUserID, testOrgID)
}

// TestListDeviceSets_HappyPath confirms the handler converts the
// request, runs the cross-org check, calls the store, and converts the
// response back into device_set.v1 types.
func TestListDeviceSets_HappyPath(t *testing.T) {
	h := newTestHandler(t)

	// buildingStore is hit via the cross-org filter validation only when
	// building_ids or scoped zone_keys are present — happy path uses an
	// owned building.
	h.buildingStore.EXPECT().
		BuildingsByIDs(gomock.Any(), testOrgID, []int64{7}).
		Return([]int64{7}, nil)

	h.collectionStore.EXPECT().
		ListCollections(gomock.Any(), testOrgID, collectionpb.CollectionType_COLLECTION_TYPE_RACK,
			int32(50), "", gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ int64, _ collectionpb.CollectionType,
			_ int32, _ string, _ *interfaces.SortConfig, filter *interfaces.DeviceSetFilter,
		) ([]*collectionpb.DeviceCollection, string, int32, error) {
			// Filter conversion: the handler must have translated
			// BuildingIds + ZoneKeys into the domain shape.
			require.NotNil(t, filter)
			assert.Equal(t, []int64{7}, filter.BuildingIDs)
			require.Len(t, filter.ZoneKeys, 1)
			assert.Equal(t, interfaces.ZoneKey{BuildingID: 7, Zone: "Room 2"}, filter.ZoneKeys[0])
			return []*collectionpb.DeviceCollection{{Id: 42, Label: "Rack 1"}}, "next-token", 1, nil
		})

	req := connect.NewRequest(&dspb.ListDeviceSetsRequest{
		Type:        dspb.DeviceSetType_DEVICE_SET_TYPE_RACK,
		PageSize:    50,
		BuildingIds: []int64{7},
		ZoneKeys: []*commonpb.ZoneKey{
			{BuildingId: 7, Zone: "Room 2"},
		},
	})

	resp, err := h.handler.ListDeviceSets(testCtx(t), req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Len(t, resp.Msg.DeviceSets, 1)
	assert.Equal(t, int64(42), resp.Msg.DeviceSets[0].Id)
	assert.Equal(t, "next-token", resp.Msg.NextPageToken)
	assert.Equal(t, int32(1), resp.Msg.TotalCount)
}

// TestListDeviceSets_DeprecatedZonesShim confirms the legacy `zones`
// field translates to wildcard ZoneKey entries (BuildingID == 0) so old
// clients continue to work. Wildcards skip the cross-org check, so
// buildingStore is never consulted.
func TestListDeviceSets_DeprecatedZonesShim(t *testing.T) {
	h := newTestHandler(t)

	h.collectionStore.EXPECT().
		ListCollections(gomock.Any(), testOrgID, collectionpb.CollectionType_COLLECTION_TYPE_RACK,
			int32(50), "", gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ int64, _ collectionpb.CollectionType,
			_ int32, _ string, _ *interfaces.SortConfig, filter *interfaces.DeviceSetFilter,
		) ([]*collectionpb.DeviceCollection, string, int32, error) {
			require.NotNil(t, filter)
			require.Len(t, filter.ZoneKeys, 2)
			// Both translated as wildcards (building_id == 0).
			for _, zk := range filter.ZoneKeys {
				assert.Equal(t, int64(0), zk.BuildingID, "deprecated zones must shim to wildcard ZoneKey")
			}
			return nil, "", 0, nil
		})

	req := connect.NewRequest(&dspb.ListDeviceSetsRequest{
		Type:     dspb.DeviceSetType_DEVICE_SET_TYPE_RACK,
		PageSize: 50,
		Zones:    []string{"Room 2", "Cold Aisle"},
	})

	_, err := h.handler.ListDeviceSets(testCtx(t), req)
	require.NoError(t, err)
}

// TestListDeviceSets_OversizedBuildingIDs guards the request-shape cap
// so the upstream cross-org bulk lookup can't be DoS'd via a large
// building_ids slice.
func TestListDeviceSets_OversizedBuildingIDs(t *testing.T) {
	h := newTestHandler(t)

	tooMany := make([]int64, maxDeviceSetFilterValues+1)
	for i := range tooMany {
		tooMany[i] = int64(i + 1)
	}
	req := connect.NewRequest(&dspb.ListDeviceSetsRequest{
		Type:        dspb.DeviceSetType_DEVICE_SET_TYPE_RACK,
		BuildingIds: tooMany,
	})

	_, err := h.handler.ListDeviceSets(testCtx(t), req)
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
	assert.Contains(t, err.Error(), "building_ids")
}

// TestListDeviceSets_OversizedZoneKeys is the parallel cap for
// zone_keys — same DoS rationale.
func TestListDeviceSets_OversizedZoneKeys(t *testing.T) {
	h := newTestHandler(t)

	tooMany := make([]*commonpb.ZoneKey, maxDeviceSetFilterValues+1)
	for i := range tooMany {
		tooMany[i] = &commonpb.ZoneKey{BuildingId: 0, Zone: "z"}
	}
	req := connect.NewRequest(&dspb.ListDeviceSetsRequest{
		Type:     dspb.DeviceSetType_DEVICE_SET_TYPE_RACK,
		ZoneKeys: tooMany,
	})

	_, err := h.handler.ListDeviceSets(testCtx(t), req)
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
	assert.Contains(t, err.Error(), "zone_keys")
}

// TestListDeviceSets_NilZoneKeyRejected confirms convert.go fails fast
// on a nil zone_keys entry instead of silently dropping it. Parity
// with fleetmanagement.parseFilter — silently dropping a nil zk would
// turn a malformed request into a broader query.
func TestListDeviceSets_NilZoneKeyRejected(t *testing.T) {
	h := newTestHandler(t)

	req := connect.NewRequest(&dspb.ListDeviceSetsRequest{
		Type:     dspb.DeviceSetType_DEVICE_SET_TYPE_RACK,
		ZoneKeys: []*commonpb.ZoneKey{nil},
	})

	_, err := h.handler.ListDeviceSets(testCtx(t), req)
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
	assert.Contains(t, err.Error(), "zone_keys[0]")
}

// TestListDeviceSets_EmptyLegacyZoneRejected parallels the
// zone_keys.zone non-empty rule. Empty legacy zones used to silently
// widen the result set — now they fail fast.
func TestListDeviceSets_EmptyLegacyZoneRejected(t *testing.T) {
	h := newTestHandler(t)

	req := connect.NewRequest(&dspb.ListDeviceSetsRequest{
		Type:  dspb.DeviceSetType_DEVICE_SET_TYPE_RACK,
		Zones: []string{""},
	})

	_, err := h.handler.ListDeviceSets(testCtx(t), req)
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
	assert.Contains(t, err.Error(), "zones[0]")
}

// TestListDeviceSets_CrossOrgRejected covers the security path: a
// building_id outside the caller's org must be rejected without
// echoing the rejected ID in the error message.
func TestListDeviceSets_CrossOrgRejected(t *testing.T) {
	h := newTestHandler(t)

	// 99 is cross-org; bulk lookup returns only 7.
	h.buildingStore.EXPECT().
		BuildingsByIDs(gomock.Any(), testOrgID, gomock.Any()).
		Return([]int64{7}, nil)

	req := connect.NewRequest(&dspb.ListDeviceSetsRequest{
		Type:        dspb.DeviceSetType_DEVICE_SET_TYPE_RACK,
		BuildingIds: []int64{7, 99},
	})

	_, err := h.handler.ListDeviceSets(testCtx(t), req)
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
	assert.NotContains(t, err.Error(), "99")
}

// TestListRackZones_ReturnsFlatList is the legacy RPC path. Returns
// string[] from the store unchanged — old clients still rely on this.
func TestListRackZones_ReturnsFlatList(t *testing.T) {
	h := newTestHandler(t)

	h.collectionStore.EXPECT().
		ListRackZones(gomock.Any(), testOrgID).
		Return([]string{"Room 1", "Room 2"}, nil)

	resp, err := h.handler.ListRackZones(testCtx(t), connect.NewRequest(&dspb.ListRackZonesRequest{}))

	require.NoError(t, err)
	assert.Equal(t, []string{"Room 1", "Room 2"}, resp.Msg.Zones)
}

// TestListRackZoneRefs_MapsAllFields confirms the new RPC denormalizes
// building + site labels into the wire ZoneRef. The
// ZoneRefRow → commonpb.ZoneRef mapping is what new clients depend on.
func TestListRackZoneRefs_MapsAllFields(t *testing.T) {
	h := newTestHandler(t)

	h.collectionStore.EXPECT().
		ListRackZoneRefs(gomock.Any(), testOrgID).
		Return([]interfaces.ZoneRefRow{
			{
				BuildingID:    7,
				BuildingLabel: "Building A",
				SiteID:        3,
				SiteLabel:     "Austin",
				Zone:          "Room 2",
			},
			{
				BuildingID:    0,
				BuildingLabel: "",
				SiteID:        3,
				SiteLabel:     "Austin",
				Zone:          "Uncategorized",
			},
		}, nil)

	resp, err := h.handler.ListRackZoneRefs(testCtx(t), connect.NewRequest(&dspb.ListRackZoneRefsRequest{}))

	require.NoError(t, err)
	require.Len(t, resp.Msg.Zones, 2)
	assert.Equal(t, &commonpb.ZoneRef{
		BuildingId: 7, BuildingLabel: "Building A",
		SiteId: 3, SiteLabel: "Austin", Zone: "Room 2",
	}, resp.Msg.Zones[0])
	assert.Equal(t, &commonpb.ZoneRef{
		BuildingId: 0, BuildingLabel: "",
		SiteId: 3, SiteLabel: "Austin", Zone: "Uncategorized",
	}, resp.Msg.Zones[1])
}

// TestListRackZoneRefs_EmptyOrg covers F23 at the handler boundary: an
// org with no racks returns an empty slice, not nil. Confirms the
// handler doesn't drop empty results or fan out an extra error.
func TestListRackZoneRefs_EmptyOrg(t *testing.T) {
	h := newTestHandler(t)

	h.collectionStore.EXPECT().
		ListRackZoneRefs(gomock.Any(), testOrgID).
		Return([]interfaces.ZoneRefRow{}, nil)

	resp, err := h.handler.ListRackZoneRefs(testCtx(t), connect.NewRequest(&dspb.ListRackZoneRefsRequest{}))

	require.NoError(t, err)
	assert.Empty(t, resp.Msg.Zones)
}

// TestListRackZoneRefs_StoreError surfaces store errors through the
// handler unchanged. Important for ops — a transient DB failure must
// not be swallowed.
func TestListRackZoneRefs_StoreError(t *testing.T) {
	h := newTestHandler(t)

	h.collectionStore.EXPECT().
		ListRackZoneRefs(gomock.Any(), testOrgID).
		Return(nil, errors.New("db down"))

	_, err := h.handler.ListRackZoneRefs(testCtx(t), connect.NewRequest(&dspb.ListRackZoneRefsRequest{}))
	require.Error(t, err)
}
