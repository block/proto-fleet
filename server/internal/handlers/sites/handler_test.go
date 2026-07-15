package sites

import (
	"context"
	"errors"
	"testing"

	"connectrpc.com/authn"
	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	pb "github.com/block/proto-fleet/server/generated/grpc/sites/v1"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/sites"
	"github.com/block/proto-fleet/server/internal/domain/sites/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
	"github.com/block/proto-fleet/server/internal/handlers/handlerstest"
)

// testHarness wires a real *sites.Service against mock stores so handler
// tests exercise both the auth gate and the body. activitySvc is nil;
// the service's logActivity guards against that path so audit fire-and-
// forget no-ops in tests.
type testHarness struct {
	handler         *Handler
	siteStore       *mocks.MockSiteStore
	buildingStore   *mocks.MockBuildingStore
	collectionStore *mocks.MockCollectionStore
	tx              *mocks.MockTransactor
	ctrl            *gomock.Controller
}

func newTestHandler(t *testing.T) *testHarness {
	t.Helper()
	ctrl := gomock.NewController(t)
	siteStore := mocks.NewMockSiteStore(ctrl)
	buildingStore := mocks.NewMockBuildingStore(ctrl)
	collectionStore := mocks.NewMockCollectionStore(ctrl)
	tx := mocks.NewMockTransactor(ctrl)
	// RunInTx fake: runs the closure inline so cascade calls land
	// against the mock store without a real DB.
	tx.EXPECT().RunInTx(gomock.Any(), gomock.Any()).AnyTimes().DoAndReturn(
		func(ctx context.Context, fn func(context.Context) error) error {
			return fn(ctx)
		},
	)
	// RunInTxWithResult fake: same inline behavior for the per-attempt
	// counter pattern used by AssignBuildingsToSite / AssignRacksToSite.
	tx.EXPECT().RunInTxWithResult(gomock.Any(), gomock.Any()).AnyTimes().DoAndReturn(
		func(ctx context.Context, fn func(context.Context) (any, error)) (any, error) {
			return fn(ctx)
		},
	)
	// GetSiteStats isn't exercised by these tests; pass nil for the
	// stats-only dependencies and rely on the service's nil-guard.
	svc := sites.NewService(siteStore, buildingStore, collectionStore, nil, nil, tx, nil)
	return &testHarness{
		handler:         NewHandler(svc),
		siteStore:       siteStore,
		buildingStore:   buildingStore,
		collectionStore: collectionStore,
		tx:              tx,
		ctrl:            ctrl,
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
func ptrBool(v bool) *bool    { return &v }

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
			Site:                      models.Site{ID: 1, Name: "alpha"},
			DeviceCount:               42,
			BuildingCount:             5,
			RackCount:                 17,
			InfrastructureDeviceCount: 3,
		},
	}, nil)

	resp, err := h.handler.ListSites(sitePermsCtx(t, 7), connect.NewRequest(&pb.ListSitesRequest{}))
	require.NoError(t, err)
	require.Len(t, resp.Msg.GetSites(), 1)
	row := resp.Msg.GetSites()[0]
	assert.Equal(t, int64(42), row.GetDeviceCount())
	assert.Equal(t, int64(5), row.GetBuildingCount())
	assert.Equal(t, int64(17), row.GetRackCount())
	assert.Equal(t, int64(3), row.GetInfrastructureDeviceCount())
	assert.Equal(t, "alpha", row.GetSite().GetName())
}

func TestHandler_ListSites_omitsStatsForNarrowedSite(t *testing.T) {
	t.Parallel()
	h := newTestHandler(t)
	h.siteStore.EXPECT().ListSites(gomock.Any(), int64(7)).Return([]models.SiteWithCounts{
		{
			Site:          models.Site{ID: 1, Name: "narrowed"},
			DeviceCount:   1,
			BuildingCount: 1,
			RackCount:     1,
		},
	}, nil)

	ctx := handlerstest.CtxWithAssignments(t, 7,
		handlerstest.OrgAssignment(authz.PermSiteRead, authz.PermFleetRead),
		handlerstest.SiteAssignment(1, authz.PermSiteRead),
	)
	resp, err := h.handler.ListSites(ctx, connect.NewRequest(&pb.ListSitesRequest{}))
	require.NoError(t, err)
	require.Len(t, resp.Msg.GetSites(), 1)
	assert.Nil(t, resp.Msg.GetSites()[0].GetListStats())
}

func TestHandler_ResolveSiteBySlug_allowsSiteScopedRead(t *testing.T) {
	t.Parallel()
	h := newTestHandler(t)
	h.siteStore.EXPECT().GetSiteBySlug(gomock.Any(), int64(7), "north").Return(&models.Site{
		ID:    42,
		Name:  "North",
		Slug:  "north",
		OrgID: 7,
	}, nil)

	ctx := handlerstest.CtxWithAssignments(t, 7, handlerstest.SiteAssignment(42, authz.PermSiteRead))
	resp, err := h.handler.ResolveSiteBySlug(ctx, connect.NewRequest(&pb.ResolveSiteBySlugRequest{Slug: "north"}))

	require.NoError(t, err)
	assert.Equal(t, int64(42), resp.Msg.GetSite().GetId())
	assert.Equal(t, "north", resp.Msg.GetSite().GetSlug())
}

func TestHandler_ResolveSiteBySlug_masksOtherSiteScopedRead(t *testing.T) {
	t.Parallel()
	h := newTestHandler(t)
	h.siteStore.EXPECT().GetSiteBySlug(gomock.Any(), int64(7), "north").Return(&models.Site{
		ID:    42,
		Name:  "North",
		Slug:  "north",
		OrgID: 7,
	}, nil)

	ctx := handlerstest.CtxWithAssignments(t, 7, handlerstest.SiteAssignment(99, authz.PermSiteRead))
	_, err := h.handler.ResolveSiteBySlug(ctx, connect.NewRequest(&pb.ResolveSiteBySlugRequest{Slug: "north"}))

	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodeNotFound, fleetErr.GRPCCode)
}

func TestHandler_ResolveSiteBySlug_propagatesPermissionWiringErrors(t *testing.T) {
	t.Parallel()
	h := newTestHandler(t)
	h.siteStore.EXPECT().GetSiteBySlug(gomock.Any(), int64(7), "north").Return(&models.Site{
		ID:    42,
		Name:  "North",
		Slug:  "north",
		OrgID: 7,
	}, nil)

	ctx := authn.SetInfo(t.Context(), &session.Info{OrganizationID: 7})
	_, err := h.handler.ResolveSiteBySlug(ctx, connect.NewRequest(&pb.ResolveSiteBySlugRequest{Slug: "north"}))

	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodeInternal, fleetErr.GRPCCode)
}

func TestHandler_CreateSite_canonicalizesNetworkConfig(t *testing.T) {
	t.Parallel()
	h := newTestHandler(t)

	h.siteStore.EXPECT().ListAllSiteNetworkConfigs(gomock.Any(), int64(7), int64(0)).Return(nil, nil)
	h.siteStore.EXPECT().ListSiteSlugs(gomock.Any(), int64(7)).Return(nil, nil)
	h.siteStore.EXPECT().CreateSite(gomock.Any(), gomock.AssignableToTypeOf(models.CreateSiteParams{})).
		DoAndReturn(func(_ context.Context, p models.CreateSiteParams) (*models.Site, error) {
			// The canonical form drops trim whitespace and normalizes.
			assert.Equal(t, "10.0.0.0/24", p.NetworkConfig)
			assert.Equal(t, "alpha", p.Slug)
			return &models.Site{ID: 1, Name: p.Name, Slug: p.Slug, NetworkConfig: p.NetworkConfig}, nil
		})

	resp, err := h.handler.CreateSite(sitePermsCtx(t, 7), connect.NewRequest(&pb.CreateSiteRequest{
		Name:          "alpha",
		NetworkConfig: "  10.0.0.0/24  ",
	}))
	require.NoError(t, err)
	assert.Equal(t, "10.0.0.0/24", resp.Msg.GetSite().GetNetworkConfig())
	assert.Equal(t, "alpha", resp.Msg.GetSite().GetSlug())
}

func TestHandler_UpdateSite_happy(t *testing.T) {
	t.Parallel()
	h := newTestHandler(t)

	// Empty network_config short-circuits overlap-warning lookup, so
	// ListAllSiteNetworkConfigs is not expected on this path. The name
	// changes, so the slug is regenerated: GetSite reads the current row and
	// ListSiteSlugs supplies the org's used slugs for collision avoidance.
	h.siteStore.EXPECT().GetSite(gomock.Any(), int64(7), int64(42)).
		Return(&models.Site{ID: 42, Name: "old name", Slug: "old-name"}, nil)
	h.siteStore.EXPECT().ListSiteSlugs(gomock.Any(), int64(7)).Return([]string{"old-name"}, nil)
	h.siteStore.EXPECT().UpdateSite(gomock.Any(), gomock.AssignableToTypeOf(models.UpdateSiteParams{})).
		DoAndReturn(func(_ context.Context, p models.UpdateSiteParams) (*models.Site, error) {
			if p.Slug != "renamed" {
				return nil, errors.New("expected regenerated slug renamed, got " + p.Slug)
			}
			return &models.Site{ID: 42, Name: p.Name, Slug: p.Slug}, nil
		})

	resp, err := h.handler.UpdateSite(sitePermsCtx(t, 7), connect.NewRequest(&pb.UpdateSiteRequest{
		Id:   42,
		Name: "renamed",
	}))
	require.NoError(t, err)
	assert.Equal(t, "renamed", resp.Msg.GetSite().GetName())
	assert.Equal(t, "renamed", resp.Msg.GetSite().GetSlug())
}

func TestHandler_DeleteSite_surfacesCascadeCounts(t *testing.T) {
	t.Parallel()
	h := newTestHandler(t)

	// Cascade store calls, all returning non-zero counts (the
	// LockSiteForWrite + LockBuildingsBySiteForWrite at the top of the
	// tx are part of the TOCTOU fix vs concurrent DeleteSite/
	// AssignBuildingToSite).
	h.siteStore.EXPECT().LockSiteForWrite(gomock.Any(), int64(7), int64(11)).Return(nil)
	h.siteStore.EXPECT().LockBuildingsBySiteForWrite(gomock.Any(), int64(7), int64(11)).Return(nil)
	h.siteStore.EXPECT().LockInfrastructureDevicesBySiteForWrite(gomock.Any(), int64(7), int64(11)).Return([]int64{70}, nil)
	h.siteStore.EXPECT().UnassignRacksFromBuildingsBySite(gomock.Any(), int64(7), int64(11)).Return(int64(0), nil)
	h.buildingStore.EXPECT().ClearDeviceBuildingsBySite(gomock.Any(), int64(7), int64(11)).Return(int64(0), nil)
	h.siteStore.EXPECT().SoftDeleteBuildingsBySite(gomock.Any(), int64(7), int64(11)).Return(int64(2), nil)
	h.siteStore.EXPECT().UnassignRacksFromSite(gomock.Any(), int64(7), int64(11)).Return(int64(4), nil)
	h.siteStore.EXPECT().UnassignDevicesFromSite(gomock.Any(), int64(7), int64(11)).Return(int64(9), nil)
	h.siteStore.EXPECT().DeleteCurtailmentResponseProfilesBySite(gomock.Any(), int64(7), int64(11)).Return(int64(3), nil)
	h.siteStore.EXPECT().CountResponseProfilesByInfrastructureDevices(gomock.Any(), int64(7), []int64{70}).Return(int64(0), nil)
	h.siteStore.EXPECT().SoftDeleteInfrastructureDevicesBySite(gomock.Any(), int64(7), int64(11)).Return(int64(6), nil)
	h.siteStore.EXPECT().SoftDeleteSite(gomock.Any(), int64(7), int64(11)).Return(int64(1), nil)

	resp, err := h.handler.DeleteSite(sitePermsCtx(t, 7), connect.NewRequest(&pb.DeleteSiteRequest{Id: 11}))
	require.NoError(t, err)
	assert.Equal(t, int64(9), resp.Msg.GetUnassignedDeviceCount())
	assert.Equal(t, int64(2), resp.Msg.GetDeletedBuildingCount())
	assert.Equal(t, int64(4), resp.Msg.GetUnassignedRackCount())
	assert.Equal(t, int64(6), resp.Msg.GetDeletedInfrastructureDeviceCount())
}

// TestHandler_DeleteSite_deniedWhenNarrowedAwayFromTargetSite is the
// regression test for the cascade-bypass finding: an org-wide
// site:manage grant narrowed away at the target site by a site-scoped
// assignment must be denied before any cascade store call runs. The
// mock stores have no expectations, so any cascade call would fail the
// test.
func TestHandler_DeleteSite_deniedWhenNarrowedAwayFromTargetSite(t *testing.T) {
	t.Parallel()
	h := newTestHandler(t)

	// Org-wide site:manage, but a site-scope assignment at site 11
	// (granting only site:read) narrows site:manage away there.
	ctx := handlerstest.CtxWithAssignments(t, 7,
		handlerstest.OrgAssignment(authz.PermSiteRead, authz.PermSiteManage),
		handlerstest.SiteAssignment(11, authz.PermSiteRead),
	)

	_, err := h.handler.DeleteSite(ctx, connect.NewRequest(&pb.DeleteSiteRequest{Id: 11}))
	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodePermissionDenied, fleetErr.GRPCCode)

	// The same caller can still delete a site they are not narrowed
	// away from — the narrowing check is per-target, not a blanket
	// restriction.
	h.siteStore.EXPECT().LockSiteForWrite(gomock.Any(), int64(7), int64(12)).Return(nil)
	h.siteStore.EXPECT().LockBuildingsBySiteForWrite(gomock.Any(), int64(7), int64(12)).Return(nil)
	h.siteStore.EXPECT().LockInfrastructureDevicesBySiteForWrite(gomock.Any(), int64(7), int64(12)).Return(nil, nil)
	h.siteStore.EXPECT().UnassignRacksFromBuildingsBySite(gomock.Any(), int64(7), int64(12)).Return(int64(0), nil)
	h.buildingStore.EXPECT().ClearDeviceBuildingsBySite(gomock.Any(), int64(7), int64(12)).Return(int64(0), nil)
	h.siteStore.EXPECT().SoftDeleteBuildingsBySite(gomock.Any(), int64(7), int64(12)).Return(int64(0), nil)
	h.siteStore.EXPECT().UnassignRacksFromSite(gomock.Any(), int64(7), int64(12)).Return(int64(0), nil)
	h.siteStore.EXPECT().UnassignDevicesFromSite(gomock.Any(), int64(7), int64(12)).Return(int64(0), nil)
	h.siteStore.EXPECT().DeleteCurtailmentResponseProfilesBySite(gomock.Any(), int64(7), int64(12)).Return(int64(0), nil)
	h.siteStore.EXPECT().CountResponseProfilesByInfrastructureDevices(gomock.Any(), int64(7), []int64(nil)).Return(int64(0), nil)
	h.siteStore.EXPECT().SoftDeleteInfrastructureDevicesBySite(gomock.Any(), int64(7), int64(12)).Return(int64(0), nil)
	h.siteStore.EXPECT().SoftDeleteSite(gomock.Any(), int64(7), int64(12)).Return(int64(1), nil)

	_, err = h.handler.DeleteSite(ctx, connect.NewRequest(&pb.DeleteSiteRequest{Id: 12}))
	require.NoError(t, err)
}

func TestHandler_AssignDevicesToSite_success(t *testing.T) {
	t.Parallel()
	h := newTestHandler(t)

	target := int64(20)
	idents := []string{"d1", "d2"}

	h.siteStore.EXPECT().LockDevicesForReassign(gomock.Any(), int64(7), idents).Return(nil)
	h.siteStore.EXPECT().LockSiteForWrite(gomock.Any(), int64(7), target).Return(nil)
	h.siteStore.EXPECT().ListExistingDeviceIdentifiers(gomock.Any(), int64(7), idents).Return(idents, nil)
	h.siteStore.EXPECT().FindDevicesInSiteLessRacks(gomock.Any(), int64(7), idents).Return(nil, nil)
	h.siteStore.EXPECT().FindDeviceSiteConflicts(gomock.Any(), int64(7), idents).Return(map[string]int64{}, nil)
	h.siteStore.EXPECT().AssignDevicesToSite(gomock.Any(), int64(7), gomock.AssignableToTypeOf(ptrInt64(0)), idents).Return(int64(2), nil)
	h.buildingStore.EXPECT().ClearDeviceBuildingsOnSiteMismatch(gomock.Any(), int64(7), idents, gomock.AssignableToTypeOf(ptrInt64(0))).Return(int64(0), nil)

	resp, err := h.handler.AssignDevicesToSite(sitePermsCtx(t, 7), connect.NewRequest(&pb.AssignDevicesToSiteRequest{
		TargetSiteId:      &target,
		DeviceIdentifiers: idents,
	}))
	require.NoError(t, err)
	assert.Equal(t, int64(2), resp.Msg.GetReassignedCount())
	assert.Empty(t, resp.Msg.GetConflicts())
}

func TestHandler_AssignDevicesToSite_conflictsReturnTypedReason(t *testing.T) {
	t.Parallel()
	h := newTestHandler(t)

	target := int64(20)
	idents := []string{"d1"}
	conflictingSite := int64(30)

	h.siteStore.EXPECT().LockDevicesForReassign(gomock.Any(), int64(7), idents).Return(nil)
	h.siteStore.EXPECT().LockSiteForWrite(gomock.Any(), int64(7), target).Return(nil)
	h.siteStore.EXPECT().ListExistingDeviceIdentifiers(gomock.Any(), int64(7), idents).Return(idents, nil)
	h.siteStore.EXPECT().FindDevicesInSiteLessRacks(gomock.Any(), int64(7), idents).Return(nil, nil)
	h.siteStore.EXPECT().FindDeviceSiteConflicts(gomock.Any(), int64(7), idents).Return(map[string]int64{
		"d1": conflictingSite,
	}, nil)
	// No AssignDevicesToSite store call — conflict path rejects entire batch.

	resp, err := h.handler.AssignDevicesToSite(sitePermsCtx(t, 7), connect.NewRequest(&pb.AssignDevicesToSiteRequest{
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

// TestHandler_AssignDevicesToSite_forceClearRequiresRackManage pins
// the auth gate on the force-clear branch. Site:manage alone clears
// the no-clear path but rejects force_clear=true — the cascade deletes
// device_set_membership rows, which sibling rack RPCs require
// rack:manage to perform. The caller must hold both keys for the
// flagged path.
func TestHandler_AssignDevicesToSite_forceClearRequiresRackManage(t *testing.T) {
	t.Parallel()

	target := int64(20)
	idents := []string{"d1"}

	t.Run("site:manage alone + force_clear=false succeeds", func(t *testing.T) {
		t.Parallel()
		h := newTestHandler(t)
		h.siteStore.EXPECT().LockDevicesForReassign(gomock.Any(), int64(7), idents).Return(nil)
		h.siteStore.EXPECT().LockSiteForWrite(gomock.Any(), int64(7), target).Return(nil)
		h.siteStore.EXPECT().ListExistingDeviceIdentifiers(gomock.Any(), int64(7), idents).Return(idents, nil)
		h.siteStore.EXPECT().FindDevicesInSiteLessRacks(gomock.Any(), int64(7), idents).Return(nil, nil)
		h.siteStore.EXPECT().FindDeviceSiteConflicts(gomock.Any(), int64(7), idents).Return(map[string]int64{}, nil)
		h.siteStore.EXPECT().AssignDevicesToSite(gomock.Any(), int64(7), gomock.AssignableToTypeOf(ptrInt64(0)), idents).Return(int64(1), nil)
		h.buildingStore.EXPECT().ClearDeviceBuildingsOnSiteMismatch(gomock.Any(), int64(7), idents, gomock.AssignableToTypeOf(ptrInt64(0))).Return(int64(0), nil)

		ctx := handlerstest.CtxWithPermissions(t, 7, authz.PermSiteManage)
		_, err := h.handler.AssignDevicesToSite(ctx, connect.NewRequest(&pb.AssignDevicesToSiteRequest{
			TargetSiteId:                        &target,
			DeviceIdentifiers:                   idents,
			ForceClearConflictingRackMembership: ptrBool(false),
		}))
		require.NoError(t, err)
	})

	t.Run("site:manage alone + force_clear=true is rejected", func(t *testing.T) {
		t.Parallel()
		h := newTestHandler(t)
		// No store expectations — the auth gate fires before the service.

		ctx := handlerstest.CtxWithPermissions(t, 7, authz.PermSiteManage)
		_, err := h.handler.AssignDevicesToSite(ctx, connect.NewRequest(&pb.AssignDevicesToSiteRequest{
			TargetSiteId:                        &target,
			DeviceIdentifiers:                   idents,
			ForceClearConflictingRackMembership: ptrBool(true),
		}))
		require.Error(t, err)
		var fleetErr fleeterror.FleetError
		require.ErrorAs(t, err, &fleetErr)
		assert.Equal(t, connect.CodePermissionDenied, fleetErr.GRPCCode)
	})

	t.Run("site:manage + rack:manage + force_clear=true succeeds", func(t *testing.T) {
		t.Parallel()
		h := newTestHandler(t)
		h.siteStore.EXPECT().LockDevicesForReassign(gomock.Any(), int64(7), idents).Return(nil)
		h.siteStore.EXPECT().LockSiteForWrite(gomock.Any(), int64(7), target).Return(nil)
		h.siteStore.EXPECT().ListExistingDeviceIdentifiers(gomock.Any(), int64(7), idents).Return(idents, nil)
		h.siteStore.EXPECT().FindDevicesInSiteLessRacks(gomock.Any(), int64(7), idents).Return(nil, nil)
		h.siteStore.EXPECT().FindDeviceSiteConflicts(gomock.Any(), int64(7), idents).Return(map[string]int64{}, nil)
		h.siteStore.EXPECT().AssignDevicesToSite(gomock.Any(), int64(7), gomock.AssignableToTypeOf(ptrInt64(0)), idents).Return(int64(1), nil)
		h.buildingStore.EXPECT().ClearDeviceBuildingsOnSiteMismatch(gomock.Any(), int64(7), idents, gomock.AssignableToTypeOf(ptrInt64(0))).Return(int64(0), nil)

		ctx := handlerstest.CtxWithPermissions(t, 7, authz.PermSiteManage, authz.PermRackManage)
		_, err := h.handler.AssignDevicesToSite(ctx, connect.NewRequest(&pb.AssignDevicesToSiteRequest{
			TargetSiteId:                        &target,
			DeviceIdentifiers:                   idents,
			ForceClearConflictingRackMembership: ptrBool(true),
		}))
		require.NoError(t, err)
	})
}

func TestHandler_AssignBuildingsToSite_surfacesCascadeCounts(t *testing.T) {
	t.Parallel()
	h := newTestHandler(t)

	target := int64(20)

	h.siteStore.EXPECT().LockSiteForWrite(gomock.Any(), int64(7), target).Return(nil)
	h.siteStore.EXPECT().LockBuildingForWrite(gomock.Any(), int64(7), int64(50)).Return(nil)
	h.siteStore.EXPECT().AssignBuildingsToSiteBulk(gomock.Any(), int64(7), []int64{50}, gomock.AssignableToTypeOf(ptrInt64(0))).Return(int64(1), nil)
	h.siteStore.EXPECT().ReassignRacksUnderBuildingsBulk(gomock.Any(), int64(7), []int64{50}, gomock.AssignableToTypeOf(ptrInt64(0))).Return(int64(3), nil)
	h.siteStore.EXPECT().ReassignDevicesUnderBuildingsBulk(gomock.Any(), int64(7), []int64{50}, gomock.AssignableToTypeOf(ptrInt64(0))).Return(int64(15), nil)
	h.buildingStore.EXPECT().CascadeDirectDeviceSitesByBuildings(gomock.Any(), int64(7), []int64{50}, gomock.AssignableToTypeOf(ptrInt64(0))).Return(int64(0), nil)

	resp, err := h.handler.AssignBuildingsToSite(sitePermsCtx(t, 7), connect.NewRequest(&pb.AssignBuildingsToSiteRequest{
		BuildingIds:  []int64{50},
		TargetSiteId: &target,
	}))
	require.NoError(t, err)
	assert.Equal(t, int64(3), resp.Msg.GetReassignedRackCount())
	assert.Equal(t, int64(15), resp.Msg.GetReassignedDeviceCount())
}

func TestHandler_AssignBuildingsToSite_targetUnsetCascadesToUnassigned(t *testing.T) {
	t.Parallel()
	h := newTestHandler(t)

	// target_site_id unset → service skips LockSiteForWrite (no target
	// site to lock) but still locks the building before the bulk cascade
	// writes, then passes a nil targetSiteID through.
	h.siteStore.EXPECT().LockBuildingForWrite(gomock.Any(), int64(7), int64(50)).Return(nil)
	h.siteStore.EXPECT().AssignBuildingsToSiteBulk(gomock.Any(), int64(7), []int64{50}, gomock.Nil()).Return(int64(1), nil)
	h.siteStore.EXPECT().ReassignRacksUnderBuildingsBulk(gomock.Any(), int64(7), []int64{50}, gomock.Nil()).Return(int64(0), nil)
	h.siteStore.EXPECT().ReassignDevicesUnderBuildingsBulk(gomock.Any(), int64(7), []int64{50}, gomock.Nil()).Return(int64(0), nil)
	h.buildingStore.EXPECT().CascadeDirectDeviceSitesByBuildings(gomock.Any(), int64(7), []int64{50}, gomock.Nil()).Return(int64(0), nil)

	resp, err := h.handler.AssignBuildingsToSite(sitePermsCtx(t, 7), connect.NewRequest(&pb.AssignBuildingsToSiteRequest{
		BuildingIds: []int64{50},
	}))
	require.NoError(t, err)
	assert.Equal(t, int64(0), resp.Msg.GetReassignedRackCount())
	assert.Equal(t, int64(0), resp.Msg.GetReassignedDeviceCount())
}

func TestHandler_AssignBuildingsToSite_bulkAggregatesCascadeCounts(t *testing.T) {
	t.Parallel()
	h := newTestHandler(t)

	target := int64(20)

	// Phase A locks both buildings in sorted ID order; Phase B issues
	// one bulk write per kind across both buildings.
	h.siteStore.EXPECT().LockSiteForWrite(gomock.Any(), int64(7), target).Return(nil)
	h.siteStore.EXPECT().LockBuildingForWrite(gomock.Any(), int64(7), int64(50)).Return(nil)
	h.siteStore.EXPECT().LockBuildingForWrite(gomock.Any(), int64(7), int64(51)).Return(nil)
	h.siteStore.EXPECT().AssignBuildingsToSiteBulk(gomock.Any(), int64(7), []int64{50, 51}, gomock.AssignableToTypeOf(ptrInt64(0))).Return(int64(2), nil)
	h.siteStore.EXPECT().ReassignRacksUnderBuildingsBulk(gomock.Any(), int64(7), []int64{50, 51}, gomock.AssignableToTypeOf(ptrInt64(0))).Return(int64(6), nil)
	h.siteStore.EXPECT().ReassignDevicesUnderBuildingsBulk(gomock.Any(), int64(7), []int64{50, 51}, gomock.AssignableToTypeOf(ptrInt64(0))).Return(int64(30), nil)
	h.buildingStore.EXPECT().CascadeDirectDeviceSitesByBuildings(gomock.Any(), int64(7), []int64{50, 51}, gomock.AssignableToTypeOf(ptrInt64(0))).Return(int64(0), nil)

	// Pass IDs out of order to verify deterministic locking via sort.
	resp, err := h.handler.AssignBuildingsToSite(sitePermsCtx(t, 7), connect.NewRequest(&pb.AssignBuildingsToSiteRequest{
		BuildingIds:  []int64{51, 50},
		TargetSiteId: &target,
	}))
	require.NoError(t, err)
	assert.Equal(t, int64(6), resp.Msg.GetReassignedRackCount())
	assert.Equal(t, int64(30), resp.Msg.GetReassignedDeviceCount())
}

func TestHandler_AssignRacksToSite_partialUpdateCascadesAndClearsBuilding(t *testing.T) {
	t.Parallel()
	h := newTestHandler(t)

	target := int64(20)
	rackID := int64(50)
	priorSite := int64(9)
	priorBuilding := int64(11)

	h.siteStore.EXPECT().LockSiteForWrite(gomock.Any(), int64(7), target).Return(nil)
	h.collectionStore.EXPECT().LockRackPlacementForWrite(gomock.Any(), rackID, int64(7)).
		Return(interfaces.RackPlacement{SiteID: &priorSite, BuildingID: &priorBuilding, Zone: "Z1"}, nil)
	// Bulk placement update clears building + zone in SQL; bulk cascade
	// follows for the same rack set.
	h.collectionStore.EXPECT().UpdateRackPlacementBulkForSite(gomock.Any(), int64(7), []int64{rackID}, &target).Return(nil)
	h.collectionStore.EXPECT().CascadeRackDeviceSitesBulk(gomock.Any(), int64(7), []int64{rackID}, &target).Return(int64(8), nil)
	h.collectionStore.EXPECT().CascadeRackDeviceBuildingsBulk(gomock.Any(), int64(7), []int64{rackID}, gomock.Nil()).Return(int64(0), nil)

	resp, err := h.handler.AssignRacksToSite(sitePermsCtx(t, 7), connect.NewRequest(&pb.AssignRacksToSiteRequest{
		RackIds:      []int64{rackID},
		TargetSiteId: &target,
	}))
	require.NoError(t, err)
	assert.Equal(t, int64(8), resp.Msg.GetReassignedDeviceCount())
	assert.Equal(t, int64(1), resp.Msg.GetClearedBuildingCount())
}

func TestHandler_AssignRacksToSite_targetUnsetUnassigns(t *testing.T) {
	t.Parallel()
	h := newTestHandler(t)

	rackID := int64(50)
	priorSite := int64(9)

	// target_site_id unset → no LockSiteForWrite (no target to lock).
	h.collectionStore.EXPECT().LockRackPlacementForWrite(gomock.Any(), rackID, int64(7)).
		Return(interfaces.RackPlacement{SiteID: &priorSite}, nil) // no building set
	// site changes (priorSite → nil) but no building to clear.
	h.collectionStore.EXPECT().UpdateRackPlacementBulkForSite(gomock.Any(), int64(7), []int64{rackID}, gomock.Nil()).Return(nil)
	h.collectionStore.EXPECT().CascadeRackDeviceSitesBulk(gomock.Any(), int64(7), []int64{rackID}, gomock.Nil()).Return(int64(3), nil)
	h.collectionStore.EXPECT().CascadeRackDeviceBuildingsBulk(gomock.Any(), int64(7), []int64{rackID}, gomock.Nil()).Return(int64(0), nil)

	resp, err := h.handler.AssignRacksToSite(sitePermsCtx(t, 7), connect.NewRequest(&pb.AssignRacksToSiteRequest{
		RackIds: []int64{rackID},
	}))
	require.NoError(t, err)
	assert.Equal(t, int64(3), resp.Msg.GetReassignedDeviceCount())
	assert.Equal(t, int64(0), resp.Msg.GetClearedBuildingCount())
}

func TestHandler_AssignRacksToSite_sameSiteIsNoOp(t *testing.T) {
	t.Parallel()
	h := newTestHandler(t)

	target := int64(20)
	rackID := int64(50)

	h.siteStore.EXPECT().LockSiteForWrite(gomock.Any(), int64(7), target).Return(nil)
	// rack already at target site — filtered out of the bulk write set;
	// no UpdateRackPlacementBulkForSite or CascadeRackDeviceSitesBulk call.
	h.collectionStore.EXPECT().LockRackPlacementForWrite(gomock.Any(), rackID, int64(7)).
		Return(interfaces.RackPlacement{SiteID: &target, BuildingID: ptrInt64(11), Zone: "Z1"}, nil)

	resp, err := h.handler.AssignRacksToSite(sitePermsCtx(t, 7), connect.NewRequest(&pb.AssignRacksToSiteRequest{
		RackIds:      []int64{rackID},
		TargetSiteId: &target,
	}))
	require.NoError(t, err)
	assert.Equal(t, int64(0), resp.Msg.GetReassignedDeviceCount())
	assert.Equal(t, int64(0), resp.Msg.GetClearedBuildingCount())
}

func TestHandler_AssignRacksToSite_bulkAggregates(t *testing.T) {
	t.Parallel()
	h := newTestHandler(t)

	target := int64(20)
	priorSite := int64(9)

	h.siteStore.EXPECT().LockSiteForWrite(gomock.Any(), int64(7), target).Return(nil)
	// Phase A locks both racks in sorted id order; Phase B issues one
	// bulk placement update and one bulk cascade across both racks.
	h.collectionStore.EXPECT().LockRackPlacementForWrite(gomock.Any(), int64(50), int64(7)).
		Return(interfaces.RackPlacement{SiteID: &priorSite, BuildingID: ptrInt64(11)}, nil)
	h.collectionStore.EXPECT().LockRackPlacementForWrite(gomock.Any(), int64(51), int64(7)).
		Return(interfaces.RackPlacement{SiteID: &priorSite}, nil) // no building
	h.collectionStore.EXPECT().UpdateRackPlacementBulkForSite(gomock.Any(), int64(7), []int64{50, 51}, &target).Return(nil)
	h.collectionStore.EXPECT().CascadeRackDeviceSitesBulk(gomock.Any(), int64(7), []int64{50, 51}, &target).Return(int64(6), nil)
	h.collectionStore.EXPECT().CascadeRackDeviceBuildingsBulk(gomock.Any(), int64(7), []int64{50, 51}, gomock.Nil()).Return(int64(0), nil)

	// IDs passed out-of-order to verify the sort happens.
	resp, err := h.handler.AssignRacksToSite(sitePermsCtx(t, 7), connect.NewRequest(&pb.AssignRacksToSiteRequest{
		RackIds:      []int64{51, 50},
		TargetSiteId: &target,
	}))
	require.NoError(t, err)
	assert.Equal(t, int64(6), resp.Msg.GetReassignedDeviceCount())
	assert.Equal(t, int64(1), resp.Msg.GetClearedBuildingCount(), "only rack with prior building counts")
}
