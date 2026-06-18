package buildings

import (
	"context"
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
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
	"github.com/block/proto-fleet/server/internal/handlers/handlerstest"
)

// testHarness wires a real *buildings.Service against mock stores.
// activitySvc is nil; the service's logActivity guards against that path.
type testHarness struct {
	handler         *Handler
	buildingStore   *mocks.MockBuildingStore
	siteStore       *mocks.MockSiteStore
	collectionStore *mocks.MockCollectionStore
	tx              *mocks.MockTransactor
	ctrl            *gomock.Controller
}

func newTestHandler(t *testing.T) *testHarness {
	t.Helper()
	ctrl := gomock.NewController(t)
	buildingStore := mocks.NewMockBuildingStore(ctrl)
	siteStore := mocks.NewMockSiteStore(ctrl)
	collectionStore := mocks.NewMockCollectionStore(ctrl)
	tx := mocks.NewMockTransactor(ctrl)
	tx.EXPECT().RunInTx(gomock.Any(), gomock.Any()).AnyTimes().DoAndReturn(
		func(ctx context.Context, fn func(context.Context) error) error {
			return fn(ctx)
		},
	)
	// RunInTxWithResult fake: same inline behavior for the per-attempt
	// counter pattern used by AssignRacksToBuilding.
	tx.EXPECT().RunInTxWithResult(gomock.Any(), gomock.Any()).AnyTimes().DoAndReturn(
		func(ctx context.Context, fn func(context.Context) (any, error)) (any, error) {
			return fn(ctx)
		},
	)
	// GetBuildingStats isn't exercised here; pass nil for stats-only deps.
	svc := buildings.NewService(buildingStore, siteStore, collectionStore, nil, nil, tx, nil)
	return &testHarness{
		handler:         NewHandler(svc),
		buildingStore:   buildingStore,
		siteStore:       siteStore,
		collectionStore: collectionStore,
		tx:              tx,
		ctrl:            ctrl,
	}
}

func sitePermsCtx(t *testing.T, orgID int64) context.Context {
	t.Helper()
	return handlerstest.CtxWithPermissions(t, orgID, authz.PermSiteRead, authz.PermSiteManage)
}

func TestHandler_authGate(t *testing.T) {
	t.Parallel()

	h := NewHandler(nil)

	cases := []struct {
		name        string
		permissions []string
		wantCode    connect.Code
	}{
		{"caller without site permissions is rejected", []string{authz.PermFleetRead}, connect.CodePermissionDenied},
		{"caller with no permissions is rejected", nil, connect.CodePermissionDenied},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := handlerstest.CtxWithPermissions(t, 1, tc.permissions...)

			_, err := h.ListBuildings(ctx, connect.NewRequest(&pb.ListBuildingsRequest{}))
			require.Error(t, err)
			var fleetErr fleeterror.FleetError
			require.ErrorAs(t, err, &fleetErr)
			assert.Equal(t, tc.wantCode, fleetErr.GRPCCode)

			_, err = h.CreateBuilding(ctx, connect.NewRequest(&pb.CreateBuildingRequest{Name: "x"}))
			require.Error(t, err)
			require.ErrorAs(t, err, &fleetErr)
			assert.Equal(t, tc.wantCode, fleetErr.GRPCCode)
		})
	}
}

func TestHandler_unauthenticatedWithoutSession(t *testing.T) {
	t.Parallel()

	h := NewHandler(nil)
	_, err := h.ListBuildings(t.Context(), connect.NewRequest(&pb.ListBuildingsRequest{}))
	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodeUnauthenticated, fleetErr.GRPCCode)
}

// TestHandler_sitePermissionsPassGate confirms that callers holding
// the right site permission clear the gate and the body runs cleanly
// against a real service + mock stores.
func TestHandler_sitePermissionsPassGate(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		perm string
	}{
		{"PermSiteRead clears ListBuildings", authz.PermSiteRead},
		{"PermSiteManage clears ListBuildings (caller also holds read for the call site)", authz.PermSiteManage},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := newTestHandler(t)
			h.buildingStore.EXPECT().ListBuildings(gomock.Any(), gomock.AssignableToTypeOf(models.ListFilter{})).Return(nil, nil)
			ctx := handlerstest.CtxWithPermissions(t, 1, tc.perm, authz.PermSiteRead)
			_, err := h.handler.ListBuildings(ctx, connect.NewRequest(&pb.ListBuildingsRequest{}))
			assert.NoError(t, err)
		})
	}
}

func TestHandler_ListBuildings_unfiltered(t *testing.T) {
	t.Parallel()
	h := newTestHandler(t)

	h.buildingStore.EXPECT().ListBuildings(gomock.Any(), gomock.AssignableToTypeOf(models.ListFilter{})).
		DoAndReturn(func(_ context.Context, f models.ListFilter) ([]models.BuildingWithCounts, error) {
			assert.Empty(t, f.SiteIDs)
			assert.False(t, f.IncludeUnassigned)
			return nil, nil
		})

	_, err := h.handler.ListBuildings(sitePermsCtx(t, 7), connect.NewRequest(&pb.ListBuildingsRequest{}))
	require.NoError(t, err)
}

func TestHandler_ListBuildings_filterBySiteIDs(t *testing.T) {
	t.Parallel()
	h := newTestHandler(t)

	h.buildingStore.EXPECT().ListBuildings(gomock.Any(), gomock.AssignableToTypeOf(models.ListFilter{})).
		DoAndReturn(func(_ context.Context, f models.ListFilter) ([]models.BuildingWithCounts, error) {
			assert.Equal(t, []int64{42, 99}, f.SiteIDs)
			assert.False(t, f.IncludeUnassigned)
			return nil, nil
		})

	_, err := h.handler.ListBuildings(sitePermsCtx(t, 7), connect.NewRequest(&pb.ListBuildingsRequest{
		SiteIds: []int64{42, 99},
	}))
	require.NoError(t, err)
}

func TestHandler_ListBuildings_filterByIncludeUnassigned(t *testing.T) {
	t.Parallel()
	h := newTestHandler(t)

	h.buildingStore.EXPECT().ListBuildings(gomock.Any(), gomock.AssignableToTypeOf(models.ListFilter{})).
		DoAndReturn(func(_ context.Context, f models.ListFilter) ([]models.BuildingWithCounts, error) {
			assert.Empty(t, f.SiteIDs)
			assert.True(t, f.IncludeUnassigned)
			return nil, nil
		})

	_, err := h.handler.ListBuildings(sitePermsCtx(t, 7), connect.NewRequest(&pb.ListBuildingsRequest{
		IncludeUnassigned: true,
	}))
	require.NoError(t, err)
}

func TestHandler_ListBuildings_filterCombined(t *testing.T) {
	t.Parallel()
	h := newTestHandler(t)

	h.buildingStore.EXPECT().ListBuildings(gomock.Any(), gomock.AssignableToTypeOf(models.ListFilter{})).
		DoAndReturn(func(_ context.Context, f models.ListFilter) ([]models.BuildingWithCounts, error) {
			assert.Equal(t, []int64{42}, f.SiteIDs)
			assert.True(t, f.IncludeUnassigned)
			return nil, nil
		})

	_, err := h.handler.ListBuildings(sitePermsCtx(t, 7), connect.NewRequest(&pb.ListBuildingsRequest{
		SiteIds:           []int64{42},
		IncludeUnassigned: true,
	}))
	require.NoError(t, err)
}

func TestHandler_ListBuildings_includesPlacementRefs(t *testing.T) {
	t.Parallel()
	h := newTestHandler(t)
	siteID := int64(42)

	h.buildingStore.EXPECT().ListBuildings(gomock.Any(), gomock.AssignableToTypeOf(models.ListFilter{})).
		Return([]models.BuildingWithCounts{{
			Building: models.Building{
				ID:        7,
				SiteID:    &siteID,
				SiteLabel: "Austin",
				Name:      "Building A",
			},
		}}, nil)

	resp, err := h.handler.ListBuildings(sitePermsCtx(t, 7), connect.NewRequest(&pb.ListBuildingsRequest{}))
	require.NoError(t, err)
	require.Len(t, resp.Msg.GetBuildings(), 1)

	building := resp.Msg.GetBuildings()[0].GetBuilding()
	require.NotNil(t, building.GetPlacement())
	require.NotNil(t, building.GetPlacement().GetSite())
	assert.Equal(t, siteID, building.GetPlacement().GetSite().GetId())
	assert.Equal(t, "Austin", building.GetPlacement().GetSite().GetLabel())
}

func TestHandler_ListBuildings_omitsStatsForNarrowedBuildingSite(t *testing.T) {
	t.Parallel()
	h := newTestHandler(t)
	siteID := int64(1)
	h.buildingStore.EXPECT().ListBuildings(gomock.Any(), gomock.AssignableToTypeOf(models.ListFilter{})).
		DoAndReturn(func(_ context.Context, f models.ListFilter) ([]models.BuildingWithCounts, error) {
			assert.True(t, f.IncludeStats)
			return []models.BuildingWithCounts{
				{
					Building:    models.Building{ID: 10, Name: "narrowed", SiteID: &siteID},
					RackCount:   1,
					DeviceCount: 1,
				},
			}, nil
		})

	ctx := handlerstest.CtxWithAssignments(t, 7,
		handlerstest.OrgAssignment(authz.PermSiteRead, authz.PermFleetRead),
		handlerstest.SiteAssignment(siteID, authz.PermSiteRead),
	)
	resp, err := h.handler.ListBuildings(ctx, connect.NewRequest(&pb.ListBuildingsRequest{}))
	require.NoError(t, err)
	require.Len(t, resp.Msg.GetBuildings(), 1)
	assert.Nil(t, resp.Msg.GetBuildings()[0].GetListStats())
}

func TestHandler_CreateBuilding_happy(t *testing.T) {
	t.Parallel()
	h := newTestHandler(t)

	h.buildingStore.EXPECT().CreateBuilding(gomock.Any(), gomock.AssignableToTypeOf(models.CreateParams{})).
		Return(&models.Building{ID: 1, Name: "Aisle-1"}, nil)

	resp, err := h.handler.CreateBuilding(sitePermsCtx(t, 7), connect.NewRequest(&pb.CreateBuildingRequest{
		Name:                  "Aisle-1",
		DefaultRackOrderIndex: pb.RackOrderIndex_RACK_ORDER_INDEX_BOTTOM_LEFT,
	}))
	require.NoError(t, err)
	assert.Equal(t, "Aisle-1", resp.Msg.GetBuilding().GetName())
}

func TestHandler_CreateBuilding_rejectsUnknownSite(t *testing.T) {
	t.Parallel()
	h := newTestHandler(t)

	// Site existence is now checked via LockSiteForWrite inside the tx so
	// a concurrent DeleteSite can't soft-delete the parent between the
	// check and the insert. The lock returns NotFound when the site is
	// missing/already soft-deleted.
	h.siteStore.EXPECT().LockSiteForWrite(gomock.Any(), int64(7), int64(123)).
		Return(fleeterror.NewNotFoundErrorf("site %d not found", 123))

	siteID := int64(123)
	_, err := h.handler.CreateBuilding(sitePermsCtx(t, 7), connect.NewRequest(&pb.CreateBuildingRequest{
		SiteId:                &siteID,
		Name:                  "Aisle-1",
		DefaultRackOrderIndex: pb.RackOrderIndex_RACK_ORDER_INDEX_BOTTOM_LEFT,
	}))
	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodeNotFound, fleetErr.GRPCCode)
}

func TestHandler_GetBuilding_happy(t *testing.T) {
	t.Parallel()
	h := newTestHandler(t)

	h.buildingStore.EXPECT().GetBuilding(gomock.Any(), int64(7), int64(42)).
		Return(&models.Building{ID: 42, Name: "Hangar"}, nil)

	resp, err := h.handler.GetBuilding(sitePermsCtx(t, 7), connect.NewRequest(&pb.GetBuildingRequest{Id: 42}))
	require.NoError(t, err)
	assert.Equal(t, int64(42), resp.Msg.GetBuilding().GetId())
	assert.Equal(t, "Hangar", resp.Msg.GetBuilding().GetName())
}

func TestHandler_GetBuilding_notFound(t *testing.T) {
	t.Parallel()
	h := newTestHandler(t)

	h.buildingStore.EXPECT().GetBuilding(gomock.Any(), int64(7), int64(999)).
		Return(nil, fleeterror.NewNotFoundErrorf("building %d not found", 999))

	_, err := h.handler.GetBuilding(sitePermsCtx(t, 7), connect.NewRequest(&pb.GetBuildingRequest{Id: 999}))
	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodeNotFound, fleetErr.GRPCCode)
}

// Cross-org access is indistinguishable from missing at the store layer —
// the SQL query filters by orgID, so a building in another org surfaces
// as ErrNoRows → NotFound. We assert NotFound (NOT PermissionDenied) to
// avoid leaking existence across orgs.
func TestHandler_GetBuilding_crossOrgIsNotFound(t *testing.T) {
	t.Parallel()
	h := newTestHandler(t)

	// Caller is org 7; the building (id 42) lives in org 9. The store
	// scopes by (orgID, id) so it returns NotFound for org 7.
	h.buildingStore.EXPECT().GetBuilding(gomock.Any(), int64(7), int64(42)).
		Return(nil, fleeterror.NewNotFoundErrorf("building %d not found", 42))

	_, err := h.handler.GetBuilding(sitePermsCtx(t, 7), connect.NewRequest(&pb.GetBuildingRequest{Id: 42}))
	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodeNotFound, fleetErr.GRPCCode)
}

func TestHandler_UpdateBuilding_happy(t *testing.T) {
	t.Parallel()
	h := newTestHandler(t)

	// UpdateBuilding now runs in a tx: lock + get current (for shrink
	// validation) before the persist call.
	h.siteStore.EXPECT().LockBuildingForWrite(gomock.Any(), int64(7), int64(1)).Return(nil)
	h.buildingStore.EXPECT().GetBuilding(gomock.Any(), int64(7), int64(1)).
		Return(&models.Building{ID: 1, Name: "old", Aisles: 0, RacksPerAisle: 0}, nil)
	h.buildingStore.EXPECT().UpdateBuilding(gomock.Any(), gomock.AssignableToTypeOf(models.UpdateParams{})).
		Return(&models.Building{ID: 1, Name: "renamed"}, nil)

	resp, err := h.handler.UpdateBuilding(sitePermsCtx(t, 7), connect.NewRequest(&pb.UpdateBuildingRequest{
		Id:                    1,
		Name:                  "renamed",
		DefaultRackOrderIndex: pb.RackOrderIndex_RACK_ORDER_INDEX_BOTTOM_LEFT,
	}))
	require.NoError(t, err)
	assert.Equal(t, "renamed", resp.Msg.GetBuilding().GetName())
}

// Shrinking aisles below the largest live aisle_index must abort the
// update with InvalidArgument and never reach UpdateBuilding.
func TestHandler_UpdateBuilding_rejectsOrphaningShrink(t *testing.T) {
	t.Parallel()
	h := newTestHandler(t)

	h.siteStore.EXPECT().LockBuildingForWrite(gomock.Any(), int64(7), int64(1)).Return(nil)
	h.buildingStore.EXPECT().GetBuilding(gomock.Any(), int64(7), int64(1)).
		Return(&models.Building{ID: 1, Aisles: 5, RacksPerAisle: 6}, nil)
	// Shrink scan uses the unbounded bounds-only query; rack at aisle
	// 3 falls outside the new 2-aisle bound.
	h.buildingStore.EXPECT().ListRacksOutsideBuildingBounds(gomock.Any(), int64(7), int64(1), int32(2), int32(6)).
		Return([]models.BuildingRack{
			{RackID: 99, RackLabel: "Rack-Z", AisleIndex: ptrInt32t(3), PositionInAisle: ptrInt32t(0)},
		}, nil)
	// UpdateBuilding must NOT be called.

	_, err := h.handler.UpdateBuilding(sitePermsCtx(t, 7), connect.NewRequest(&pb.UpdateBuildingRequest{
		Id:                    1,
		Name:                  "shrunk",
		Aisles:                2,
		RacksPerAisle:         6,
		DefaultRackOrderIndex: pb.RackOrderIndex_RACK_ORDER_INDEX_BOTTOM_LEFT,
	}))
	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodeInvalidArgument, fleetErr.GRPCCode)
}

func TestHandler_DeleteBuilding_surfacesRackCount(t *testing.T) {
	t.Parallel()
	h := newTestHandler(t)

	h.buildingStore.EXPECT().SoftDeleteBuilding(gomock.Any(), int64(7), int64(33)).Return(int64(1), nil)
	h.buildingStore.EXPECT().UnassignRacksFromBuilding(gomock.Any(), int64(7), int64(33)).Return(int64(5), nil)
	h.buildingStore.EXPECT().ClearDeviceBuildingsByBuilding(gomock.Any(), int64(7), int64(33)).Return(int64(0), nil)

	resp, err := h.handler.DeleteBuilding(sitePermsCtx(t, 7), connect.NewRequest(&pb.DeleteBuildingRequest{Id: 33}))
	require.NoError(t, err)
	assert.Equal(t, int64(5), resp.Msg.GetUnassignedRackCount())
}

// ListBuildingRacks requires PermSiteRead — callers without it are
// rejected before the service is touched.
func TestHandler_ListBuildingRacks_rejectsCallerWithoutSiteRead(t *testing.T) {
	t.Parallel()
	h := NewHandler(nil)
	ctx := handlerstest.CtxWithPermissions(t, 7, authz.PermFleetRead)
	_, err := h.ListBuildingRacks(ctx, connect.NewRequest(&pb.ListBuildingRacksRequest{BuildingId: 11}))
	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodePermissionDenied, fleetErr.GRPCCode)
}

// ListBuildingRacks: site-read permission clears the gate and threads
// org_id + building_id through to the service.
func TestHandler_ListBuildingRacks_happy(t *testing.T) {
	t.Parallel()
	h := newTestHandler(t)

	h.buildingStore.EXPECT().GetBuilding(gomock.Any(), int64(7), int64(11)).
		Return(&models.Building{ID: 11}, nil)
	h.buildingStore.EXPECT().ListBuildingRacks(gomock.Any(), int64(7), int64(11), gomock.Any(), gomock.Any()).
		Return([]models.BuildingRack{
			{RackID: 1, RackLabel: "Rack-A", AisleIndex: nil, PositionInAisle: nil},
			{RackID: 2, RackLabel: "Rack-B", AisleIndex: ptrInt32t(0), PositionInAisle: ptrInt32t(1)},
		}, "next-rack-page", nil)

	resp, err := h.handler.ListBuildingRacks(sitePermsCtx(t, 7),
		connect.NewRequest(&pb.ListBuildingRacksRequest{BuildingId: 11}))
	require.NoError(t, err)
	assert.Len(t, resp.Msg.GetRacks(), 2)
	assert.Equal(t, "Rack-A", resp.Msg.GetRacks()[0].GetRackLabel())
	assert.Equal(t, int64(2), resp.Msg.GetRacks()[1].GetRackId())
	assert.Equal(t, "next-rack-page", resp.Msg.GetNextPageToken())
}

// AssignRacksToBuilding requires PermSiteManage — callers without it
// are rejected before the service is touched.
func TestHandler_AssignRacksToBuilding_rejectsCallerWithoutSiteManage(t *testing.T) {
	t.Parallel()
	h := NewHandler(nil)
	// PermSiteRead alone does NOT satisfy PermSiteManage.
	ctx := handlerstest.CtxWithPermissions(t, 7, authz.PermSiteRead)
	buildingID := int64(11)
	_, err := h.AssignRacksToBuilding(ctx, connect.NewRequest(&pb.AssignRacksToBuildingRequest{
		Racks:            []*pb.RackPlacement{{RackId: 99}},
		TargetBuildingId: &buildingID,
	}))
	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodePermissionDenied, fleetErr.GRPCCode)
}

// AssignRacksToBuilding: PermSiteManage clears the gate and request
// fields thread through to service params; response carries
// SiteReassignedDeviceCount.
func TestHandler_AssignRacksToBuilding_happy(t *testing.T) {
	t.Parallel()
	h := newTestHandler(t)

	buildingID := int64(11)
	siteID := int64(3)

	gomock.InOrder(
		h.siteStore.EXPECT().LockBuildingForWrite(gomock.Any(), int64(7), buildingID).Return(nil),
		h.buildingStore.EXPECT().GetBuilding(gomock.Any(), int64(7), buildingID).
			Return(&models.Building{ID: buildingID, SiteID: &siteID, Aisles: 4, RacksPerAisle: 6}, nil),
		// Phase A: per-rack lock + read.
		h.collectionStore.EXPECT().LockRackPlacementForWrite(gomock.Any(), int64(99), int64(7)).
			Return(interfaces.RackPlacement{SiteID: nil}, nil),
		// Phase B1: single bulk placement write.
		h.collectionStore.EXPECT().UpdateRackPlacementBulkForBuilding(gomock.Any(), int64(7), []int64{99}, &siteID, &buildingID).
			Return(int64(1), nil),
		// Phase B2: single bulk cascade for site-changed rack.
		h.collectionStore.EXPECT().CascadeRackDeviceSitesBulk(gomock.Any(), int64(7), []int64{99}, &siteID).
			Return(int64(3), nil),
		// Phase B2b: building cascade — rack moved nil → &buildingID, so
		// device.building_id has to follow.
		h.collectionStore.EXPECT().CascadeRackDeviceBuildingsBulk(gomock.Any(), int64(7), []int64{99}, &buildingID).
			Return(int64(3), nil),
		// Phase B3: bulk pass-1 vacate.
		h.buildingStore.EXPECT().SetRackBuildingPositionBulkClear(gomock.Any(), int64(7), []int64{99}).
			Return(nil),
		// Phase B4: bulk pass-2 place.
		h.buildingStore.EXPECT().SetRackBuildingPositionBulkPlace(gomock.Any(), int64(7), []int64{99}, []int32{1}, []int32{2}).
			Return(nil),
	)

	aisle := int32(1)
	pos := int32(2)
	resp, err := h.handler.AssignRacksToBuilding(sitePermsCtx(t, 7), connect.NewRequest(&pb.AssignRacksToBuildingRequest{
		TargetBuildingId: &buildingID,
		Racks: []*pb.RackPlacement{{
			RackId:          99,
			AisleIndex:      &aisle,
			PositionInAisle: &pos,
		}},
	}))
	require.NoError(t, err)
	assert.Equal(t, int64(3), resp.Msg.GetSiteReassignedDeviceCount())
}

func ptrInt32t(v int32) *int32 { return &v }
