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
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
	"github.com/block/proto-fleet/server/internal/handlers/handlerstest"
)

// testHarness wires a real *buildings.Service against mock stores.
// activitySvc is nil; the service's logActivity guards against that path.
type testHarness struct {
	handler       *Handler
	buildingStore *mocks.MockBuildingStore
	siteStore     *mocks.MockSiteStore
	tx            *mocks.MockTransactor
	ctrl          *gomock.Controller
}

func newTestHandler(t *testing.T) *testHarness {
	t.Helper()
	ctrl := gomock.NewController(t)
	buildingStore := mocks.NewMockBuildingStore(ctrl)
	siteStore := mocks.NewMockSiteStore(ctrl)
	tx := mocks.NewMockTransactor(ctrl)
	tx.EXPECT().RunInTx(gomock.Any(), gomock.Any()).AnyTimes().DoAndReturn(
		func(ctx context.Context, fn func(context.Context) error) error {
			return fn(ctx)
		},
	)
	svc := buildings.NewService(buildingStore, siteStore, tx, nil)
	return &testHarness{
		handler:       NewHandler(svc),
		buildingStore: buildingStore,
		siteStore:     siteStore,
		tx:            tx,
		ctrl:          ctrl,
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
			assert.Nil(t, f.SiteID)
			assert.False(t, f.UnassignedOnly)
			return nil, nil
		})

	_, err := h.handler.ListBuildings(sitePermsCtx(t, 7), connect.NewRequest(&pb.ListBuildingsRequest{}))
	require.NoError(t, err)
}

func TestHandler_ListBuildings_filterBySiteID(t *testing.T) {
	t.Parallel()
	h := newTestHandler(t)

	h.buildingStore.EXPECT().ListBuildings(gomock.Any(), gomock.AssignableToTypeOf(models.ListFilter{})).
		DoAndReturn(func(_ context.Context, f models.ListFilter) ([]models.BuildingWithCounts, error) {
			require.NotNil(t, f.SiteID)
			assert.Equal(t, int64(42), *f.SiteID)
			assert.False(t, f.UnassignedOnly)
			return nil, nil
		})

	_, err := h.handler.ListBuildings(sitePermsCtx(t, 7), connect.NewRequest(&pb.ListBuildingsRequest{
		SiteFilter: &pb.ListBuildingsRequest_SiteId{SiteId: 42},
	}))
	require.NoError(t, err)
}

func TestHandler_ListBuildings_filterByUnassignedOnly(t *testing.T) {
	t.Parallel()
	h := newTestHandler(t)

	h.buildingStore.EXPECT().ListBuildings(gomock.Any(), gomock.AssignableToTypeOf(models.ListFilter{})).
		DoAndReturn(func(_ context.Context, f models.ListFilter) ([]models.BuildingWithCounts, error) {
			assert.Nil(t, f.SiteID)
			assert.True(t, f.UnassignedOnly)
			return nil, nil
		})

	_, err := h.handler.ListBuildings(sitePermsCtx(t, 7), connect.NewRequest(&pb.ListBuildingsRequest{
		SiteFilter: &pb.ListBuildingsRequest_UnassignedOnly{UnassignedOnly: true},
	}))
	require.NoError(t, err)
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

func TestHandler_DeleteBuilding_surfacesRackCount(t *testing.T) {
	t.Parallel()
	h := newTestHandler(t)

	h.buildingStore.EXPECT().SoftDeleteBuilding(gomock.Any(), int64(7), int64(33)).Return(int64(1), nil)
	h.buildingStore.EXPECT().UnassignRacksFromBuilding(gomock.Any(), int64(7), int64(33)).Return(int64(5), nil)

	resp, err := h.handler.DeleteBuilding(sitePermsCtx(t, 7), connect.NewRequest(&pb.DeleteBuildingRequest{Id: 33}))
	require.NoError(t, err)
	assert.Equal(t, int64(5), resp.Msg.GetUnassignedRackCount())
}
