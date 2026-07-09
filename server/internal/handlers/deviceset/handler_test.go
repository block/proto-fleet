package deviceset

import (
	"context"
	"errors"
	"testing"

	"connectrpc.com/authn"
	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	collectionpb "github.com/block/proto-fleet/server/generated/grpc/collection/v1"
	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	dspb "github.com/block/proto-fleet/server/generated/grpc/device_set/v1"
	"github.com/block/proto-fleet/server/internal/domain/activity"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/collection"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
	"github.com/block/proto-fleet/server/internal/testutil"
	"google.golang.org/protobuf/types/known/wrapperspb"
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
	siteStore       *mocks.MockSiteStore
	buildingStore   *mocks.MockBuildingStore
	ctrl            *gomock.Controller
}

func newTestHandler(t *testing.T) *testHarness {
	t.Helper()
	ctrl := gomock.NewController(t)

	collectionStore := mocks.NewMockCollectionStore(ctrl)
	siteStore := mocks.NewMockSiteStore(ctrl)
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
		siteStore,
		buildingStore,
		tx,
		noopResolver,
		nil, // telemetry: unused
		activitySvc,
	)

	return &testHarness{
		handler:         NewHandler(svc),
		collectionStore: collectionStore,
		siteStore:       siteStore,
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
			return []*collectionpb.DeviceCollection{{
				Id:    42,
				Label: "Rack 1",
				Placement: &commonpb.PlacementRefs{
					Site:     &commonpb.ResourceRef{Id: 3, Label: "Austin"},
					Building: &commonpb.ResourceRef{Id: 7, Label: "Building A"},
				},
			}}, "next-token", 1, nil
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
	deviceSet := resp.Msg.DeviceSets[0]
	assert.Equal(t, int64(42), deviceSet.Id)
	require.NotNil(t, deviceSet.GetPlacement())
	require.NotNil(t, deviceSet.GetPlacement().GetSite())
	assert.Equal(t, int64(3), deviceSet.GetPlacement().GetSite().GetId())
	assert.Equal(t, "Austin", deviceSet.GetPlacement().GetSite().GetLabel())
	require.NotNil(t, deviceSet.GetPlacement().GetBuilding())
	assert.Equal(t, int64(7), deviceSet.GetPlacement().GetBuilding().GetId())
	assert.Equal(t, "Building A", deviceSet.GetPlacement().GetBuilding().GetLabel())
	assert.Equal(t, "next-token", resp.Msg.NextPageToken)
	assert.Equal(t, int32(1), resp.Msg.TotalCount)
}

func TestListDeviceSets_SiteAndTelemetryFilters(t *testing.T) {
	h := newTestHandler(t)

	h.siteStore.EXPECT().
		SitesByIDs(gomock.Any(), testOrgID, []int64{3}).
		Return([]int64{3}, nil)

	h.collectionStore.EXPECT().
		ListCollections(gomock.Any(), testOrgID, collectionpb.CollectionType_COLLECTION_TYPE_RACK,
			int32(50), "", gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ int64, _ collectionpb.CollectionType,
			_ int32, _ string, _ *interfaces.SortConfig, filter *interfaces.DeviceSetFilter,
		) ([]*collectionpb.DeviceCollection, string, int32, error) {
			require.NotNil(t, filter)
			assert.Equal(t, []int64{3}, filter.SiteIDs)
			assert.True(t, filter.IncludeUnassigned)
			require.Len(t, filter.TelemetryRanges, 1)
			assert.Equal(t, interfaces.NumericFilterFieldTemperatureC, filter.TelemetryRanges[0].Field)
			assert.Equal(t, 40.0, *filter.TelemetryRanges[0].Min)
			assert.Equal(t, 80.0, *filter.TelemetryRanges[0].Max)
			assert.True(t, filter.TelemetryRanges[0].MinInclusive)
			assert.True(t, filter.TelemetryRanges[0].MaxInclusive)
			return nil, "", 0, nil
		})

	req := connect.NewRequest(&dspb.ListDeviceSetsRequest{
		Type:              dspb.DeviceSetType_DEVICE_SET_TYPE_RACK,
		PageSize:          50,
		SiteIds:           []int64{3},
		IncludeUnassigned: true,
		TelemetryRanges: []*commonpb.FleetListTelemetryRangeFilter{{
			Field:        commonpb.FleetListTelemetryField_FLEET_LIST_TELEMETRY_FIELD_TEMPERATURE_C,
			Min:          wrapperspb.Double(40),
			Max:          wrapperspb.Double(80),
			MinInclusive: true,
			MaxInclusive: true,
		}},
	})

	_, err := h.handler.ListDeviceSets(testCtx(t), req)
	require.NoError(t, err)
}

func TestListDeviceSets_SiteFilterCrossOrgRejected(t *testing.T) {
	h := newTestHandler(t)

	h.siteStore.EXPECT().
		SitesByIDs(gomock.Any(), testOrgID, []int64{99}).
		Return([]int64{}, nil)

	req := connect.NewRequest(&dspb.ListDeviceSetsRequest{
		Type:    dspb.DeviceSetType_DEVICE_SET_TYPE_RACK,
		SiteIds: []int64{99},
	})

	_, err := h.handler.ListDeviceSets(testCtx(t), req)
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
	assert.NotContains(t, err.Error(), "99")
}

func TestListDeviceSets_SiteFilterAllowedForGroups(t *testing.T) {
	h := newTestHandler(t)

	h.siteStore.EXPECT().
		SitesByIDs(gomock.Any(), testOrgID, []int64{3}).
		Return([]int64{3}, nil)

	h.collectionStore.EXPECT().
		ListCollections(gomock.Any(), testOrgID, collectionpb.CollectionType_COLLECTION_TYPE_GROUP,
			int32(50), "", gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ int64, _ collectionpb.CollectionType,
			_ int32, _ string, _ *interfaces.SortConfig, filter *interfaces.DeviceSetFilter,
		) ([]*collectionpb.DeviceCollection, string, int32, error) {
			require.NotNil(t, filter)
			assert.Equal(t, []int64{3}, filter.SiteIDs)
			assert.False(t, filter.IncludeUnassigned)
			return nil, "", 0, nil
		})

	req := connect.NewRequest(&dspb.ListDeviceSetsRequest{
		Type:     dspb.DeviceSetType_DEVICE_SET_TYPE_GROUP,
		PageSize: 50,
		SiteIds:  []int64{3},
	})

	_, err := h.handler.ListDeviceSets(testCtx(t), req)
	require.NoError(t, err)
}

func TestListDeviceSets_OrgRackReadPlusSiteRackReadAllowsFilteredSite(t *testing.T) {
	h := newTestHandler(t)

	h.siteStore.EXPECT().
		SitesByIDs(gomock.Any(), testOrgID, []int64{3}).
		Return([]int64{3}, nil)

	h.collectionStore.EXPECT().
		ListCollections(gomock.Any(), testOrgID, collectionpb.CollectionType_COLLECTION_TYPE_GROUP,
			int32(50), "", gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ int64, _ collectionpb.CollectionType,
			_ int32, _ string, _ *interfaces.SortConfig, filter *interfaces.DeviceSetFilter,
		) ([]*collectionpb.DeviceCollection, string, int32, error) {
			require.NotNil(t, filter)
			assert.Equal(t, []int64{3}, filter.SiteIDs)
			assert.False(t, filter.IncludeUnassigned)
			return nil, "", 0, nil
		})

	req := connect.NewRequest(&dspb.ListDeviceSetsRequest{
		Type:     dspb.DeviceSetType_DEVICE_SET_TYPE_GROUP,
		PageSize: 50,
		SiteIds:  []int64{3},
	})

	_, err := h.handler.ListDeviceSets(
		ctxWithAssignments(
			orgAssignmentLocal(authz.PermRackRead),
			siteAssignmentLocal(3, authz.PermRackRead),
		),
		req,
	)
	require.NoError(t, err)
}

func TestListDeviceSets_SiteOnlyRackReadDeniedBeforeFilteredSite(t *testing.T) {
	h := newTestHandler(t)

	req := connect.NewRequest(&dspb.ListDeviceSetsRequest{
		Type:    dspb.DeviceSetType_DEVICE_SET_TYPE_GROUP,
		SiteIds: []int64{3},
	})

	_, err := h.handler.ListDeviceSets(ctxWithAssignments(siteAssignmentLocal(3, authz.PermRackRead)), req)
	require.Error(t, err)
	var fe fleeterror.FleetError
	require.ErrorAs(t, err, &fe)
	assert.Equal(t, connect.CodePermissionDenied, fe.GRPCCode)
}

func TestListDeviceSets_SiteNarrowingDeniesFilteredSite(t *testing.T) {
	h := newTestHandler(t)

	req := connect.NewRequest(&dspb.ListDeviceSetsRequest{
		Type:    dspb.DeviceSetType_DEVICE_SET_TYPE_GROUP,
		SiteIds: []int64{3},
	})

	_, err := h.handler.ListDeviceSets(
		ctxWithAssignments(
			orgAssignmentLocal(authz.PermRackRead),
			siteAssignmentLocal(3, authz.PermSiteRead),
		),
		req,
	)
	require.Error(t, err)
	var fe fleeterror.FleetError
	require.ErrorAs(t, err, &fe)
	assert.Equal(t, connect.CodePermissionDenied, fe.GRPCCode)
}

func TestListDeviceSets_IncludeUnassignedRequiresOrgRackRead(t *testing.T) {
	h := newTestHandler(t)

	req := connect.NewRequest(&dspb.ListDeviceSetsRequest{
		Type:              dspb.DeviceSetType_DEVICE_SET_TYPE_GROUP,
		SiteIds:           []int64{3},
		IncludeUnassigned: true,
	})

	_, err := h.handler.ListDeviceSets(ctxWithAssignments(siteAssignmentLocal(3, authz.PermRackRead)), req)
	require.Error(t, err)
	var fe fleeterror.FleetError
	require.ErrorAs(t, err, &fe)
	assert.Equal(t, connect.CodePermissionDenied, fe.GRPCCode)
}

func TestListDeviceSetMembers_SiteNarrowingDeniesFilteredSite(t *testing.T) {
	h := newTestHandler(t)

	req := connect.NewRequest(&dspb.ListDeviceSetMembersRequest{
		DeviceSetId: 42,
		SiteIds:     []int64{3},
	})

	_, err := h.handler.ListDeviceSetMembers(
		ctxWithAssignments(
			orgAssignmentLocal(authz.PermRackRead),
			siteAssignmentLocal(3, authz.PermSiteRead),
		),
		req,
	)
	require.Error(t, err)
	var fe fleeterror.FleetError
	require.ErrorAs(t, err, &fe)
	assert.Equal(t, connect.CodePermissionDenied, fe.GRPCCode)
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

func TestListDeviceSets_InvalidTelemetryRangeRejected(t *testing.T) {
	h := newTestHandler(t)

	req := connect.NewRequest(&dspb.ListDeviceSetsRequest{
		Type: dspb.DeviceSetType_DEVICE_SET_TYPE_GROUP,
		TelemetryRanges: []*commonpb.FleetListTelemetryRangeFilter{{
			Field: commonpb.FleetListTelemetryField_FLEET_LIST_TELEMETRY_FIELD_POWER_KW,
		}},
	})

	_, err := h.handler.ListDeviceSets(testCtx(t), req)
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
	assert.Contains(t, err.Error(), "telemetry_ranges[0]")
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

// TestListDeviceSets_SiteIDs threads site_ids + include_unassigned
// through to the domain filter shape. Site IDs skip the cross-org
// building lookup; the SQL org_id predicate is the isolation barrier.
func TestListDeviceSets_SiteIDs(t *testing.T) {
	h := newTestHandler(t)

	// Cross-org validation: both requested sites belong to the org.
	h.siteStore.EXPECT().
		SitesByIDs(gomock.Any(), testOrgID, gomock.Any()).
		Return([]int64{3, 5}, nil)

	h.collectionStore.EXPECT().
		ListCollections(gomock.Any(), testOrgID, collectionpb.CollectionType_COLLECTION_TYPE_RACK,
			int32(50), "", gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ int64, _ collectionpb.CollectionType,
			_ int32, _ string, _ *interfaces.SortConfig, filter *interfaces.DeviceSetFilter,
		) ([]*collectionpb.DeviceCollection, string, int32, error) {
			require.NotNil(t, filter)
			assert.Equal(t, []int64{3, 5}, filter.SiteIDs)
			assert.True(t, filter.IncludeUnassigned)
			return nil, "", 0, nil
		})

	req := connect.NewRequest(&dspb.ListDeviceSetsRequest{
		Type:              dspb.DeviceSetType_DEVICE_SET_TYPE_RACK,
		PageSize:          50,
		SiteIds:           []int64{3, 5},
		IncludeUnassigned: true,
	})

	_, err := h.handler.ListDeviceSets(testCtx(t), req)
	require.NoError(t, err)
}

// TestListDeviceSets_SiteIDsCrossOrgRejected covers the per-org
// enforcement from #265: a site_id outside the caller's org is rejected
// without echoing the rejected ID, mirroring the building_ids path.
func TestListDeviceSets_SiteIDsCrossOrgRejected(t *testing.T) {
	h := newTestHandler(t)

	// 99 is cross-org; ownership lookup returns only 3.
	h.siteStore.EXPECT().
		SitesByIDs(gomock.Any(), testOrgID, gomock.Any()).
		Return([]int64{3}, nil)

	req := connect.NewRequest(&dspb.ListDeviceSetsRequest{
		Type:    dspb.DeviceSetType_DEVICE_SET_TYPE_RACK,
		SiteIds: []int64{3, 99},
	})

	_, err := h.handler.ListDeviceSets(testCtx(t), req)
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
	assert.NotContains(t, err.Error(), "99")
}

func TestListDeviceSets_SiteIDsAllowedForGroupType(t *testing.T) {
	h := newTestHandler(t)

	h.siteStore.EXPECT().
		SitesByIDs(gomock.Any(), testOrgID, []int64{3}).
		Return([]int64{3}, nil)

	h.collectionStore.EXPECT().
		ListCollections(gomock.Any(), testOrgID, collectionpb.CollectionType_COLLECTION_TYPE_GROUP,
			int32(50), "", gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ int64, _ collectionpb.CollectionType,
			_ int32, _ string, _ *interfaces.SortConfig, filter *interfaces.DeviceSetFilter,
		) ([]*collectionpb.DeviceCollection, string, int32, error) {
			require.NotNil(t, filter)
			assert.Equal(t, []int64{3}, filter.SiteIDs)
			return nil, "", 0, nil
		})

	req := connect.NewRequest(&dspb.ListDeviceSetsRequest{
		Type:     dspb.DeviceSetType_DEVICE_SET_TYPE_GROUP,
		PageSize: 50,
		SiteIds:  []int64{3},
	})

	_, err := h.handler.ListDeviceSets(testCtx(t), req)
	require.NoError(t, err)
}

// TestListDeviceSets_NonPositiveSiteID parallels the miner-list
// site_ids[i] must be > 0 rule.
func TestListDeviceSets_NonPositiveSiteID(t *testing.T) {
	h := newTestHandler(t)

	req := connect.NewRequest(&dspb.ListDeviceSetsRequest{
		Type:    dspb.DeviceSetType_DEVICE_SET_TYPE_RACK,
		SiteIds: []int64{3, 0},
	})

	_, err := h.handler.ListDeviceSets(testCtx(t), req)
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
	assert.Contains(t, err.Error(), "site_ids[1]")
}

// TestListDeviceSets_OversizedSiteIDs is the array cap. Parallels the
// building_ids / zone_keys caps.
func TestListDeviceSets_OversizedSiteIDs(t *testing.T) {
	h := newTestHandler(t)

	tooMany := make([]int64, maxDeviceSetFilterValues+1)
	for i := range tooMany {
		tooMany[i] = int64(i + 1)
	}
	req := connect.NewRequest(&dspb.ListDeviceSetsRequest{
		Type:    dspb.DeviceSetType_DEVICE_SET_TYPE_RACK,
		SiteIds: tooMany,
	})

	_, err := h.handler.ListDeviceSets(testCtx(t), req)
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
	assert.Contains(t, err.Error(), "site_ids")
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

// ctxWithPerms builds an auth context with an explicit permission set
// so we can assert AssignDevicesToRack's PermRackManage gate.
func ctxWithPerms(perms ...string) context.Context {
	ctx := authn.SetInfo(context.Background(), &session.Info{
		AuthMethod:     session.AuthMethodSession,
		OrganizationID: testOrgID,
		UserID:         testUserID,
	})
	return middleware.WithEffectivePermissions(ctx, authz.NewEffectivePermissions([]authz.Assignment{{
		AssignmentID: 1,
		ScopeType:    authz.ScopeOrg,
		Permissions:  perms,
	}}))
}

func ctxWithAssignments(assignments ...authz.Assignment) context.Context {
	ctx := authn.SetInfo(context.Background(), &session.Info{
		AuthMethod:     session.AuthMethodSession,
		OrganizationID: testOrgID,
		UserID:         testUserID,
	})
	return middleware.WithEffectivePermissions(ctx, authz.NewEffectivePermissions(assignments))
}

func orgAssignmentLocal(perms ...string) authz.Assignment {
	return authz.Assignment{
		AssignmentID: 1,
		ScopeType:    authz.ScopeOrg,
		Permissions:  perms,
	}
}

func siteAssignmentLocal(siteID int64, perms ...string) authz.Assignment {
	return authz.Assignment{
		AssignmentID: 2,
		ScopeType:    authz.ScopeSite,
		SiteID:       &siteID,
		Permissions:  perms,
	}
}

// deviceListSelector builds a DeviceSelector that selects the given
// identifiers (the device_list variant), the only variant accepted by
// AssignDevicesToRack and the only variant the noopResolver-backed
// group endpoint tests exercise.
func deviceListSelector(ids ...string) *commonpb.DeviceSelector {
	return &commonpb.DeviceSelector{
		SelectionType: &commonpb.DeviceSelector_DeviceList{
			DeviceList: &commonpb.DeviceIdentifierList{
				DeviceIdentifiers: ids,
			},
		},
	}
}

// allDevicesSelector builds the all_devices DeviceSelector variant.
// AssignDevicesToRack must reject it with InvalidArgument.
func allDevicesSelector() *commonpb.DeviceSelector {
	return &commonpb.DeviceSelector{
		SelectionType: &commonpb.DeviceSelector_AllDevices{AllDevices: true},
	}
}

func ptrInt64Local(v int64) *int64 { return &v }

// TestAssignDevicesToRack_PermissionRequired confirms the
// PermRackManage gate rejects callers without rack:manage. No store
// calls should fire.
func TestAssignDevicesToRack_PermissionRequired(t *testing.T) {
	h := newTestHandler(t)

	// Caller has *some* permission but not PermRackManage.
	ctx := ctxWithPerms(authz.PermSiteRead)
	req := connect.NewRequest(&dspb.AssignDevicesToRackRequest{
		TargetRackId:   ptrInt64Local(42),
		DeviceSelector: deviceListSelector("d1"),
	})

	_, err := h.handler.AssignDevicesToRack(ctx, req)
	require.Error(t, err)
	var fe fleeterror.FleetError
	require.ErrorAs(t, err, &fe, "expected FleetError, got %T", err)
	assert.Equal(t, connect.CodePermissionDenied, fe.GRPCCode)
}

// TestSaveRack_PlacementRequiresSiteManage confirms that placing a rack under
// a site/building requires site:manage, even though SaveRack's base gate is
// only rack:manage. A rack:manage-only caller that sends placement intent
// (an explicit site_id/building_id) is rejected before any store write —
// matching the dedicated AssignRacksToSite/Building RPCs.
func TestSaveRack_PlacementRequiresSiteManage(t *testing.T) {
	h := newTestHandler(t)

	// Has rack:manage but NOT site:manage.
	ctx := ctxWithPerms(authz.PermRackManage)
	req := connect.NewRequest(&dspb.SaveRackRequest{
		Label:          "Rack",
		RackInfo:       &dspb.RackInfo{BuildingId: ptrInt64Local(7)},
		DeviceSelector: deviceListSelector(),
	})

	_, err := h.handler.SaveRack(ctx, req)
	require.Error(t, err)
	var fe fleeterror.FleetError
	require.ErrorAs(t, err, &fe, "expected FleetError, got %T", err)
	assert.Equal(t, connect.CodePermissionDenied, fe.GRPCCode)
}

// TestAssignDevicesToRack_HappyPathAssigns covers the assign branch:
// target_rack_id set, devices flow through the lock → label-read →
// remove → add → cascade chain, and the handler round-trips the
// per-step counts onto the wire response.
func TestAssignDevicesToRack_HappyPathAssigns(t *testing.T) {
	h := newTestHandler(t)

	targetRackID := int64(42)
	rackSite := int64(7)
	deviceIDs := []string{"d1", "d2"}

	gomock.InOrder(
		h.collectionStore.EXPECT().
			LockRacksForReparent(gomock.Any(), testOrgID, deviceIDs, targetRackID).
			Return([]int64{targetRackID}, nil),
		h.collectionStore.EXPECT().
			LockRackPlacementForWrite(gomock.Any(), targetRackID, testOrgID).
			Return(interfaces.RackPlacement{SiteID: &rackSite}, nil),
		h.collectionStore.EXPECT().
			GetCollection(gomock.Any(), testOrgID, targetRackID).
			Return(&collectionpb.DeviceCollection{Id: targetRackID, Label: "Rack-B", Type: collectionpb.CollectionType_COLLECTION_TYPE_RACK}, nil),
		h.collectionStore.EXPECT().
			RemoveDevicesFromAnyRack(gomock.Any(), testOrgID, deviceIDs, targetRackID).
			Return(int64(2), nil),
		h.collectionStore.EXPECT().
			AddDevicesToCollection(gomock.Any(), testOrgID, targetRackID, deviceIDs).
			Return(int64(2), nil),
		h.collectionStore.EXPECT().
			GetDeviceSiteIDsByMembership(gomock.Any(), targetRackID, testOrgID).
			Return(map[string]*int64{"d1": nil, "d2": &rackSite}, nil),
		h.collectionStore.EXPECT().
			CascadeAddedDeviceSites(gomock.Any(), testOrgID, targetRackID, deviceIDs).
			Return(int64(1), nil),
		// Building cascade peer — fires after the site cascade so new
		// rack members inherit the rack's building_id too.
		h.collectionStore.EXPECT().
			CascadeAddedDeviceBuildings(gomock.Any(), testOrgID, targetRackID, deviceIDs).
			Return(int64(0), nil),
	)

	resp, err := h.handler.AssignDevicesToRack(testCtx(t), connect.NewRequest(&dspb.AssignDevicesToRackRequest{
		TargetRackId:   &targetRackID,
		DeviceSelector: deviceListSelector(deviceIDs...),
	}))
	require.NoError(t, err)
	assert.Equal(t, int64(2), resp.Msg.AssignedCount)
	assert.Equal(t, int64(2), resp.Msg.RemovedCount)
	assert.Equal(t, int64(1), resp.Msg.SiteReassignedCount)
}

// TestAssignDevicesToRack_UnassignBranch covers target_rack_id unset:
// clears prior rack membership, no Get/Lock/Add/Cascade.
// RemoveDevicesFromAnyRack is called with targetRackID = 0 (sentinel
// meaning "don't exclude any rack").
func TestAssignDevicesToRack_UnassignBranch(t *testing.T) {
	h := newTestHandler(t)

	deviceIDs := []string{"d1"}
	gomock.InOrder(
		h.collectionStore.EXPECT().
			LockRacksForReparent(gomock.Any(), testOrgID, deviceIDs, int64(0)).
			Return([]int64{}, nil),
		h.collectionStore.EXPECT().
			RemoveDevicesFromAnyRack(gomock.Any(), testOrgID, deviceIDs, int64(0)).
			Return(int64(1), nil),
	)

	resp, err := h.handler.AssignDevicesToRack(testCtx(t), connect.NewRequest(&dspb.AssignDevicesToRackRequest{
		TargetRackId:   nil,
		DeviceSelector: deviceListSelector(deviceIDs...),
	}))
	require.NoError(t, err)
	assert.Equal(t, int64(0), resp.Msg.AssignedCount)
	assert.Equal(t, int64(1), resp.Msg.RemovedCount)
	assert.Equal(t, int64(0), resp.Msg.SiteReassignedCount)
}

// TestAssignDevicesToRack_RejectsAllDevicesSelector confirms that the
// all_devices selector variant is rejected with InvalidArgument at the
// handler boundary, before any store call fires. Moving every paired
// device into a single rack is never the intended operation and the
// filter-based variants are reserved for a future expansion.
func TestAssignDevicesToRack_RejectsAllDevicesSelector(t *testing.T) {
	h := newTestHandler(t)

	resp, err := h.handler.AssignDevicesToRack(testCtx(t), connect.NewRequest(&dspb.AssignDevicesToRackRequest{
		TargetRackId:   ptrInt64Local(42),
		DeviceSelector: allDevicesSelector(),
	}))
	require.Error(t, err)
	require.Nil(t, resp)
	var fe fleeterror.FleetError
	require.ErrorAs(t, err, &fe)
	assert.Equal(t, connect.CodeInvalidArgument, fe.GRPCCode)
}

// TestAssignDevicesToRack_RejectsEmptyIdentifier confirms that a
// device_list containing an empty-string identifier is rejected at the
// handler boundary. common.v1.DeviceSelector.device_list carries no
// buf.validate constraints, unlike the deprecated repeated-string field
// it replaced, so handler-side validation is the only stop before the
// store layer silently matches no rows.
func TestAssignDevicesToRack_RejectsEmptyIdentifier(t *testing.T) {
	h := newTestHandler(t)

	resp, err := h.handler.AssignDevicesToRack(testCtx(t), connect.NewRequest(&dspb.AssignDevicesToRackRequest{
		TargetRackId:   ptrInt64Local(42),
		DeviceSelector: deviceListSelector("d1", ""),
	}))
	require.Error(t, err)
	require.Nil(t, resp)
	var fe fleeterror.FleetError
	require.ErrorAs(t, err, &fe)
	assert.Equal(t, connect.CodeInvalidArgument, fe.GRPCCode)
}

// TestAssignDevicesToRack_RejectsOverlongIdentifier confirms a single
// identifier longer than the documented 256-char cap is rejected.
// Parallels the buf.validate items.string.max_len on the old field.
func TestAssignDevicesToRack_RejectsOverlongIdentifier(t *testing.T) {
	h := newTestHandler(t)

	long := make([]byte, 257)
	for i := range long {
		long[i] = 'a'
	}
	resp, err := h.handler.AssignDevicesToRack(testCtx(t), connect.NewRequest(&dspb.AssignDevicesToRackRequest{
		TargetRackId:   ptrInt64Local(42),
		DeviceSelector: deviceListSelector(string(long)),
	}))
	require.Error(t, err)
	require.Nil(t, resp)
	var fe fleeterror.FleetError
	require.ErrorAs(t, err, &fe)
	assert.Equal(t, connect.CodeInvalidArgument, fe.GRPCCode)
}

// TestAssignDevicesToRack_RejectsOversizedList confirms the
// max_items: 10000 cap. Without this bound the store-side ANY()
// expansion can be DoS'd by passing an unbounded list.
func TestAssignDevicesToRack_RejectsOversizedList(t *testing.T) {
	h := newTestHandler(t)

	tooMany := make([]string, 10001)
	for i := range tooMany {
		tooMany[i] = "d"
	}
	resp, err := h.handler.AssignDevicesToRack(testCtx(t), connect.NewRequest(&dspb.AssignDevicesToRackRequest{
		TargetRackId:   ptrInt64Local(42),
		DeviceSelector: deviceListSelector(tooMany...),
	}))
	require.Error(t, err)
	require.Nil(t, resp)
	var fe fleeterror.FleetError
	require.ErrorAs(t, err, &fe)
	assert.Equal(t, connect.CodeInvalidArgument, fe.GRPCCode)
}

// TestAddDevicesToGroup_HappyPath asserts that the handler verifies the
// target is a group, calls the underlying AddDevicesToGroup path, and
// round-trips the added_count onto the wire response.
//
// The default test harness uses a noopResolver that returns nil for
// every selector; this test wires a resolver that returns the supplied
// identifiers so the call threads through to the store mock.
func TestAddDevicesToGroup_HappyPath(t *testing.T) {
	targetGroupID := int64(11)
	deviceIDs := []string{"d1", "d2"}

	h := newGroupHandlerWithResolver(t, deviceIDs)
	gomock.InOrder(
		h.collectionStore.EXPECT().
			GetCollection(gomock.Any(), testOrgID, targetGroupID).
			Return(&collectionpb.DeviceCollection{Id: targetGroupID, Label: "Group A", Type: collectionpb.CollectionType_COLLECTION_TYPE_GROUP}, nil),
		h.collectionStore.EXPECT().
			AddDevicesToCollectionReturningAdded(gomock.Any(), testOrgID, targetGroupID, deviceIDs).
			Return(deviceIDs, nil),
	)

	resp, err := h.handler.AddDevicesToGroup(testCtx(t), connect.NewRequest(&dspb.AddDevicesToGroupRequest{
		TargetGroupId:  targetGroupID,
		DeviceSelector: deviceListSelector(deviceIDs...),
	}))
	require.NoError(t, err)
	assert.Equal(t, int64(2), resp.Msg.AddedCount)
}

// TestAddDevicesToGroup_RejectsRackTarget asserts the handler returns
// InvalidArgument when target_group_id points at a rack, so callers
// can't smuggle a rack mutation through the group endpoint and bypass
// AssignDevicesToRack's atomic prior-rack removal + site cascade.
func TestAddDevicesToGroup_RejectsRackTarget(t *testing.T) {
	h := newTestHandler(t)

	targetID := int64(11)
	h.collectionStore.EXPECT().
		GetCollection(gomock.Any(), testOrgID, targetID).
		Return(&collectionpb.DeviceCollection{Id: targetID, Label: "Rack-X", Type: collectionpb.CollectionType_COLLECTION_TYPE_RACK}, nil)

	_, err := h.handler.AddDevicesToGroup(testCtx(t), connect.NewRequest(&dspb.AddDevicesToGroupRequest{
		TargetGroupId:  targetID,
		DeviceSelector: deviceListSelector("d1"),
	}))
	require.Error(t, err)
	var fe fleeterror.FleetError
	require.ErrorAs(t, err, &fe)
	assert.Equal(t, connect.CodeInvalidArgument, fe.GRPCCode)
}

// TestAddDevicesToGroup_PermissionRequired confirms the PermRackManage
// gate. No store calls fire when the caller lacks the permission.
func TestAddDevicesToGroup_PermissionRequired(t *testing.T) {
	h := newTestHandler(t)

	ctx := ctxWithPerms(authz.PermSiteRead)
	_, err := h.handler.AddDevicesToGroup(ctx, connect.NewRequest(&dspb.AddDevicesToGroupRequest{
		TargetGroupId:  42,
		DeviceSelector: deviceListSelector("d1"),
	}))
	require.Error(t, err)
	var fe fleeterror.FleetError
	require.ErrorAs(t, err, &fe)
	assert.Equal(t, connect.CodePermissionDenied, fe.GRPCCode)
}

// TestAddDevicesToGroup_RejectsCrossOrgTarget pins the wire-visible
// NotFound code when target_group_id belongs to a different org.
// The service layer's GetCollection(orgID, id) returns NotFound for any
// collection not owned by the caller's org; this test exercises the
// end-to-end path so a future refactor cannot accidentally leak the
// existence of cross-org collections.
func TestAddDevicesToGroup_RejectsCrossOrgTarget(t *testing.T) {
	targetID := int64(11)
	h := newGroupHandlerWithResolver(t, []string{"d1"})
	h.collectionStore.EXPECT().
		GetCollection(gomock.Any(), testOrgID, targetID).
		Return(nil, fleeterror.NewNotFoundErrorf("collection not found"))

	_, err := h.handler.AddDevicesToGroup(testCtx(t), connect.NewRequest(&dspb.AddDevicesToGroupRequest{
		TargetGroupId:  targetID,
		DeviceSelector: deviceListSelector("d1"),
	}))
	require.Error(t, err)
	var fe fleeterror.FleetError
	require.ErrorAs(t, err, &fe)
	assert.Equal(t, connect.CodeNotFound, fe.GRPCCode)
}

// TestRemoveDevicesFromGroup_HappyPath asserts the handler verifies the
// target is a group and forwards to the group remove path.
func TestRemoveDevicesFromGroup_HappyPath(t *testing.T) {
	targetGroupID := int64(11)
	deviceIDs := []string{"d1"}

	h := newGroupHandlerWithResolver(t, deviceIDs)
	gomock.InOrder(
		h.collectionStore.EXPECT().
			GetCollection(gomock.Any(), testOrgID, targetGroupID).
			Return(&collectionpb.DeviceCollection{Id: targetGroupID, Label: "Group A", Type: collectionpb.CollectionType_COLLECTION_TYPE_GROUP}, nil),
		h.collectionStore.EXPECT().
			RemoveDevicesFromCollectionReturningRemoved(gomock.Any(), testOrgID, targetGroupID, deviceIDs).
			Return(deviceIDs, nil),
	)

	resp, err := h.handler.RemoveDevicesFromGroup(testCtx(t), connect.NewRequest(&dspb.RemoveDevicesFromGroupRequest{
		TargetGroupId:  targetGroupID,
		DeviceSelector: deviceListSelector(deviceIDs...),
	}))
	require.NoError(t, err)
	assert.Equal(t, int64(1), resp.Msg.RemovedCount)
}

// TestRemoveDevicesFromGroup_RejectsRackTarget asserts a rack target
// returns InvalidArgument with no follow-up store mutation.
func TestRemoveDevicesFromGroup_RejectsRackTarget(t *testing.T) {
	h := newTestHandler(t)

	targetID := int64(11)
	h.collectionStore.EXPECT().
		GetCollection(gomock.Any(), testOrgID, targetID).
		Return(&collectionpb.DeviceCollection{Id: targetID, Label: "Rack-X", Type: collectionpb.CollectionType_COLLECTION_TYPE_RACK}, nil)

	_, err := h.handler.RemoveDevicesFromGroup(testCtx(t), connect.NewRequest(&dspb.RemoveDevicesFromGroupRequest{
		TargetGroupId:  targetID,
		DeviceSelector: deviceListSelector("d1"),
	}))
	require.Error(t, err)
	var fe fleeterror.FleetError
	require.ErrorAs(t, err, &fe)
	assert.Equal(t, connect.CodeInvalidArgument, fe.GRPCCode)
}

// TestRemoveDevicesFromGroup_PermissionRequired confirms the
// PermRackManage gate.
func TestRemoveDevicesFromGroup_PermissionRequired(t *testing.T) {
	h := newTestHandler(t)

	ctx := ctxWithPerms(authz.PermSiteRead)
	_, err := h.handler.RemoveDevicesFromGroup(ctx, connect.NewRequest(&dspb.RemoveDevicesFromGroupRequest{
		TargetGroupId:  42,
		DeviceSelector: deviceListSelector("d1"),
	}))
	require.Error(t, err)
	var fe fleeterror.FleetError
	require.ErrorAs(t, err, &fe)
	assert.Equal(t, connect.CodePermissionDenied, fe.GRPCCode)
}

// newGroupHandlerWithResolver builds a harness like newTestHandler but
// wires a resolver that returns the supplied identifiers for any
// selector. The default harness uses a noopResolver that returns nil,
// which is fine for handler-level rejection tests but not for the
// group happy-path tests that need to thread a non-empty identifier
// list through to AddDevicesToGroup / RemoveDevicesFromGroup.
func newGroupHandlerWithResolver(t *testing.T, ids []string) *testHarness {
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

	resolver := func(_ context.Context, _ *commonpb.DeviceSelector, _ int64) ([]string, error) {
		return ids, nil
	}

	svc := collection.NewService(
		collectionStore,
		nil,
		nil,
		buildingStore,
		tx,
		resolver,
		nil,
		activitySvc,
	)
	return &testHarness{
		handler:         NewHandler(svc),
		collectionStore: collectionStore,
		buildingStore:   buildingStore,
		ctrl:            ctrl,
	}
}
