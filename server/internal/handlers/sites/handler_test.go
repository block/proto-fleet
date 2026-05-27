package sites

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	pb "github.com/block/proto-fleet/server/generated/grpc/sites/v1"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/sites"
	"github.com/block/proto-fleet/server/internal/domain/sites/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
	"github.com/block/proto-fleet/server/internal/handlers/handlerstest"
)

// testHarness wires a real *sites.Service against mock stores so handler
// tests exercise both the auth gate and the body. activitySvc is nil;
// the service's logActivity guards against that path so audit fire-and-
// forget no-ops in tests.
type testHarness struct {
	handler   *Handler
	siteStore *mocks.MockSiteStore
	tx        *mocks.MockTransactor
	ctrl      *gomock.Controller
}

func newTestHandler(t *testing.T) *testHarness {
	t.Helper()
	ctrl := gomock.NewController(t)
	siteStore := mocks.NewMockSiteStore(ctrl)
	tx := mocks.NewMockTransactor(ctrl)
	// RunInTx fake: runs the closure inline so cascade calls land
	// against the mock store without a real DB.
	tx.EXPECT().RunInTx(gomock.Any(), gomock.Any()).AnyTimes().DoAndReturn(
		func(ctx context.Context, fn func(context.Context) error) error {
			return fn(ctx)
		},
	)
	svc := sites.NewService(siteStore, tx, nil)
	return &testHarness{
		handler:   NewHandler(svc),
		siteStore: siteStore,
		tx:        tx,
		ctrl:      ctrl,
	}
}

// sitePermsCtx is the workhorse for body-level tests: a caller with
// both site:read and site:manage at org scope clears every gate this
// package defines.
func sitePermsCtx(t *testing.T, orgID int64) context.Context {
	t.Helper()
	return handlerstest.CtxWithPermissions(t, orgID, authz.PermSiteRead, authz.PermSiteManage)
}

func ptrInt64(v int64) *int64 { return &v }

// TestHandler_authGate exercises the permission gate at the handler
// boundary. Callers without the required key get PermissionDenied
// before the body runs.
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

			_, err := h.ListSites(ctx, connect.NewRequest(&pb.ListSitesRequest{}))
			require.Error(t, err)
			var fleetErr fleeterror.FleetError
			require.ErrorAs(t, err, &fleetErr)
			assert.Equal(t, tc.wantCode, fleetErr.GRPCCode)

			_, err = h.CreateSite(ctx, connect.NewRequest(&pb.CreateSiteRequest{Name: "x"}))
			require.Error(t, err)
			require.ErrorAs(t, err, &fleetErr)
			assert.Equal(t, tc.wantCode, fleetErr.GRPCCode)

			_, err = h.DeleteSite(ctx, connect.NewRequest(&pb.DeleteSiteRequest{Id: 1}))
			require.Error(t, err)
			require.ErrorAs(t, err, &fleetErr)
			assert.Equal(t, tc.wantCode, fleetErr.GRPCCode)
		})
	}
}

func TestHandler_unauthenticatedWithoutSession(t *testing.T) {
	t.Parallel()

	h := NewHandler(nil)
	_, err := h.ListSites(t.Context(), connect.NewRequest(&pb.ListSitesRequest{}))
	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodeUnauthenticated, fleetErr.GRPCCode)
}

// TestHandler_sitePermissionsPassGate confirms callers holding the
// appropriate site permission clear the gate and the body runs
// cleanly against a real service + mock stores.
func TestHandler_sitePermissionsPassGate(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		perm string
	}{
		{"PermSiteRead clears ListSites", authz.PermSiteRead},
		{"PermSiteManage clears ListSites (read is implied by holding manage at the catalog level — test wires both)", authz.PermSiteManage},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := newTestHandler(t)
			h.siteStore.EXPECT().ListSites(gomock.Any(), int64(1)).Return(nil, nil)
			ctx := handlerstest.CtxWithPermissions(t, 1, tc.perm, authz.PermSiteRead)
			_, err := h.handler.ListSites(ctx, connect.NewRequest(&pb.ListSitesRequest{}))
			assert.NoError(t, err)
		})
	}
}

func TestHandler_ListSites_returnsRowsWithAllCounts(t *testing.T) {
	t.Parallel()
	h := newTestHandler(t)
	h.siteStore.EXPECT().ListSites(gomock.Any(), int64(7)).Return([]models.SiteWithCounts{
		{
			Site:          models.Site{ID: 1, Name: "alpha"},
			DeviceCount:   42,
			BuildingCount: 5,
			RackCount:     17,
		},
	}, nil)

	resp, err := h.handler.ListSites(sitePermsCtx(t, 7), connect.NewRequest(&pb.ListSitesRequest{}))
	require.NoError(t, err)
	require.Len(t, resp.Msg.GetSites(), 1)
	row := resp.Msg.GetSites()[0]
	assert.Equal(t, int64(42), row.GetDeviceCount())
	assert.Equal(t, int64(5), row.GetBuildingCount())
	assert.Equal(t, int64(17), row.GetRackCount())
	assert.Equal(t, "alpha", row.GetSite().GetName())
}

func TestHandler_CreateSite_canonicalizesNetworkConfig(t *testing.T) {
	t.Parallel()
	h := newTestHandler(t)

	h.siteStore.EXPECT().ListAllSiteNetworkConfigs(gomock.Any(), int64(7), int64(0)).Return(nil, nil)
	h.siteStore.EXPECT().CreateSite(gomock.Any(), gomock.AssignableToTypeOf(models.CreateSiteParams{})).
		DoAndReturn(func(_ context.Context, p models.CreateSiteParams) (*models.Site, error) {
			// The canonical form drops trim whitespace and normalizes.
			assert.Equal(t, "10.0.0.0/24", p.NetworkConfig)
			return &models.Site{ID: 1, Name: p.Name, NetworkConfig: p.NetworkConfig}, nil
		})

	resp, err := h.handler.CreateSite(sitePermsCtx(t, 7), connect.NewRequest(&pb.CreateSiteRequest{
		Name:          "alpha",
		NetworkConfig: "  10.0.0.0/24  ",
	}))
	require.NoError(t, err)
	assert.Equal(t, "10.0.0.0/24", resp.Msg.GetSite().GetNetworkConfig())
}

func TestHandler_UpdateSite_happy(t *testing.T) {
	t.Parallel()
	h := newTestHandler(t)

	// Empty network_config short-circuits overlap-warning lookup, so
	// ListAllSiteNetworkConfigs is not expected on this path.
	h.siteStore.EXPECT().UpdateSite(gomock.Any(), gomock.AssignableToTypeOf(models.UpdateSiteParams{})).
		Return(&models.Site{ID: 42, Name: "renamed"}, nil)

	resp, err := h.handler.UpdateSite(sitePermsCtx(t, 7), connect.NewRequest(&pb.UpdateSiteRequest{
		Id:   42,
		Name: "renamed",
	}))
	require.NoError(t, err)
	assert.Equal(t, "renamed", resp.Msg.GetSite().GetName())
}

func TestHandler_DeleteSite_surfacesCascadeCounts(t *testing.T) {
	t.Parallel()
	h := newTestHandler(t)

	// Cascade: 7 store calls, all returning non-zero counts (the
	// LockSiteForWrite + LockBuildingsBySiteForWrite at the top of the
	// tx are part of the TOCTOU fix vs concurrent DeleteSite/
	// AssignBuildingToSite).
	h.siteStore.EXPECT().LockSiteForWrite(gomock.Any(), int64(7), int64(11)).Return(nil)
	h.siteStore.EXPECT().LockBuildingsBySiteForWrite(gomock.Any(), int64(7), int64(11)).Return(nil)
	h.siteStore.EXPECT().UnassignRacksFromBuildingsBySite(gomock.Any(), int64(7), int64(11)).Return(int64(0), nil)
	h.siteStore.EXPECT().SoftDeleteBuildingsBySite(gomock.Any(), int64(7), int64(11)).Return(int64(2), nil)
	h.siteStore.EXPECT().UnassignRacksFromSite(gomock.Any(), int64(7), int64(11)).Return(int64(4), nil)
	h.siteStore.EXPECT().UnassignDevicesFromSite(gomock.Any(), int64(7), int64(11)).Return(int64(9), nil)
	h.siteStore.EXPECT().SoftDeleteSite(gomock.Any(), int64(7), int64(11)).Return(int64(1), nil)

	resp, err := h.handler.DeleteSite(sitePermsCtx(t, 7), connect.NewRequest(&pb.DeleteSiteRequest{Id: 11}))
	require.NoError(t, err)
	assert.Equal(t, int64(9), resp.Msg.GetUnassignedDeviceCount())
	assert.Equal(t, int64(2), resp.Msg.GetDeletedBuildingCount())
	assert.Equal(t, int64(4), resp.Msg.GetUnassignedRackCount())
}

func TestHandler_ReassignDevicesToSite_success(t *testing.T) {
	t.Parallel()
	h := newTestHandler(t)

	target := int64(20)
	idents := []string{"d1", "d2"}

	h.siteStore.EXPECT().LockDevicesForReassign(gomock.Any(), int64(7), idents).Return(nil)
	h.siteStore.EXPECT().LockSiteForWrite(gomock.Any(), int64(7), target).Return(nil)
	h.siteStore.EXPECT().ListExistingDeviceIdentifiers(gomock.Any(), int64(7), idents).Return(idents, nil)
	h.siteStore.EXPECT().FindDeviceSiteConflicts(gomock.Any(), int64(7), idents).Return(map[string]int64{}, nil)
	h.siteStore.EXPECT().ReassignDevicesToSite(gomock.Any(), int64(7), gomock.AssignableToTypeOf(ptrInt64(0)), idents).Return(int64(2), nil)

	resp, err := h.handler.ReassignDevicesToSite(sitePermsCtx(t, 7), connect.NewRequest(&pb.ReassignDevicesToSiteRequest{
		TargetSiteId:      &target,
		DeviceIdentifiers: idents,
	}))
	require.NoError(t, err)
	assert.Equal(t, int64(2), resp.Msg.GetReassignedCount())
	assert.Empty(t, resp.Msg.GetConflicts())
}

func TestHandler_ReassignDevicesToSite_conflictsReturnTypedReason(t *testing.T) {
	t.Parallel()
	h := newTestHandler(t)

	target := int64(20)
	idents := []string{"d1"}
	conflictingSite := int64(30)

	h.siteStore.EXPECT().LockDevicesForReassign(gomock.Any(), int64(7), idents).Return(nil)
	h.siteStore.EXPECT().LockSiteForWrite(gomock.Any(), int64(7), target).Return(nil)
	h.siteStore.EXPECT().ListExistingDeviceIdentifiers(gomock.Any(), int64(7), idents).Return(idents, nil)
	h.siteStore.EXPECT().FindDeviceSiteConflicts(gomock.Any(), int64(7), idents).Return(map[string]int64{
		"d1": conflictingSite,
	}, nil)
	// No ReassignDevicesToSite store call — conflict path rejects entire batch.

	resp, err := h.handler.ReassignDevicesToSite(sitePermsCtx(t, 7), connect.NewRequest(&pb.ReassignDevicesToSiteRequest{
		TargetSiteId:      &target,
		DeviceIdentifiers: idents,
	}))
	require.NoError(t, err)
	assert.Equal(t, int64(0), resp.Msg.GetReassignedCount())
	require.Len(t, resp.Msg.GetConflicts(), 1)
	c := resp.Msg.GetConflicts()[0]
	assert.Equal(t, "d1", c.GetDeviceIdentifier())
	assert.Equal(t, pb.PerDeviceConflictReason_PER_DEVICE_CONFLICT_REASON_DEVICE_IN_RACK_AT_OTHER_SITE, c.GetReason())
	assert.Equal(t, conflictingSite, c.GetConflictingSiteId())
}

func TestHandler_AssignBuildingToSite_surfacesCascadeCounts(t *testing.T) {
	t.Parallel()
	h := newTestHandler(t)

	target := int64(20)

	h.siteStore.EXPECT().LockSiteForWrite(gomock.Any(), int64(7), target).Return(nil)
	h.siteStore.EXPECT().LockBuildingForWrite(gomock.Any(), int64(7), int64(50)).Return(nil)
	h.siteStore.EXPECT().AssignBuildingToSite(gomock.Any(), int64(7), int64(50), gomock.AssignableToTypeOf(ptrInt64(0))).Return(int64(1), nil)
	h.siteStore.EXPECT().ReassignRacksUnderBuilding(gomock.Any(), int64(7), int64(50), gomock.AssignableToTypeOf(ptrInt64(0))).Return(int64(3), nil)
	h.siteStore.EXPECT().ReassignDevicesUnderBuilding(gomock.Any(), int64(7), int64(50), gomock.AssignableToTypeOf(ptrInt64(0))).Return(int64(15), nil)

	resp, err := h.handler.AssignBuildingToSite(sitePermsCtx(t, 7), connect.NewRequest(&pb.AssignBuildingToSiteRequest{
		BuildingId:   50,
		TargetSiteId: &target,
	}))
	require.NoError(t, err)
	assert.Equal(t, int64(3), resp.Msg.GetReassignedRackCount())
	assert.Equal(t, int64(15), resp.Msg.GetReassignedDeviceCount())
}

func TestHandler_AssignBuildingToSite_targetUnsetCascadesToUnassigned(t *testing.T) {
	t.Parallel()
	h := newTestHandler(t)

	// target_site_id unset → service skips LockSiteForWrite (no target
	// site to lock) but still locks the building before the cascade,
	// then passes a nil targetSiteID through.
	h.siteStore.EXPECT().LockBuildingForWrite(gomock.Any(), int64(7), int64(50)).Return(nil)
	h.siteStore.EXPECT().AssignBuildingToSite(gomock.Any(), int64(7), int64(50), gomock.Nil()).Return(int64(1), nil)
	h.siteStore.EXPECT().ReassignRacksUnderBuilding(gomock.Any(), int64(7), int64(50), gomock.Nil()).Return(int64(0), nil)
	h.siteStore.EXPECT().ReassignDevicesUnderBuilding(gomock.Any(), int64(7), int64(50), gomock.Nil()).Return(int64(0), nil)

	resp, err := h.handler.AssignBuildingToSite(sitePermsCtx(t, 7), connect.NewRequest(&pb.AssignBuildingToSiteRequest{
		BuildingId: 50,
	}))
	require.NoError(t, err)
	assert.Equal(t, int64(0), resp.Msg.GetReassignedRackCount())
	assert.Equal(t, int64(0), resp.Msg.GetReassignedDeviceCount())
}
