package infrastructure

import (
	"context"
	"encoding/json"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	pb "github.com/block/proto-fleet/server/generated/grpc/infrastructure/v1"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/infrastructure"
	"github.com/block/proto-fleet/server/internal/domain/infrastructure/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
	"github.com/block/proto-fleet/server/internal/handlers/handlerstest"
)

// testHarness wires a real *infrastructure.Service (with the real
// driver registry) against mock stores, mirroring the buildings
// handler test setup.
type testHarness struct {
	handler   *Handler
	store     *mocks.MockInfrastructureDeviceStore
	siteStore *mocks.MockSiteStore
}

func newTestHandler(t *testing.T) *testHarness {
	t.Helper()
	ctrl := gomock.NewController(t)
	store := mocks.NewMockInfrastructureDeviceStore(ctrl)
	siteStore := mocks.NewMockSiteStore(ctrl)
	tx := mocks.NewMockTransactor(ctrl)
	tx.EXPECT().RunInTx(gomock.Any(), gomock.Any()).AnyTimes().DoAndReturn(
		func(ctx context.Context, fn func(context.Context) error) error {
			return fn(ctx)
		},
	)
	svc := infrastructure.NewService(store, siteStore, infrastructure.NewDefaultDriverRegistry(), tx, nil)
	return &testHarness{handler: NewHandler(svc), store: store, siteStore: siteStore}
}

func sitePermsCtx(t *testing.T, orgID int64) context.Context {
	t.Helper()
	return handlerstest.CtxWithPermissions(t, orgID, authz.PermSiteRead, authz.PermSiteManage)
}

const validConfig = `{"endpoint":"10.1.2.3","port":502,"unit_id":5,"register_address":2001,"write_mode":"holding_register"}`

func validCreateRequest() *pb.CreateInfrastructureDeviceRequest {
	enabled := true
	return &pb.CreateInfrastructureDeviceRequest{
		SiteId:       10,
		BuildingName: "Building 1",
		Name:         "Zone A exhaust fans",
		DeviceKind:   models.KindFanGroup,
		FanCount:     12,
		Enabled:      &enabled,
		DriverType:   "modbus_tcp",
		DriverConfig: validConfig,
	}
}

func deviceAtSite(id, siteID int64) *models.Device {
	return &models.Device{
		ID:           id,
		OrgID:        42,
		SiteID:       siteID,
		Name:         "Zone A exhaust fans",
		DeviceKind:   models.KindFanGroup,
		FanCount:     12,
		Enabled:      true,
		DriverType:   "modbus_tcp",
		DriverConfig: json.RawMessage(validConfig),
	}
}

func requirePermissionDenied(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodePermissionDenied, fleetErr.GRPCCode)
}

func requireNotFound(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodeNotFound, fleetErr.GRPCCode)
}

func TestHandler_CreateAuthGate(t *testing.T) {
	t.Parallel()

	// Create authorizes before touching the service, so a nil handler
	// suffices for the denial paths.
	h := NewHandler(nil)

	cases := []struct {
		name        string
		permissions []string
	}{
		{"caller without site permissions is rejected", []string{authz.PermFleetRead}},
		{"caller with no permissions is rejected", nil},
		{"caller with only site:read is rejected", []string{authz.PermSiteRead}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := handlerstest.CtxWithPermissions(t, 1, tc.permissions...)
			_, err := h.CreateInfrastructureDevice(ctx, connect.NewRequest(validCreateRequest()))
			requirePermissionDenied(t, err)
		})
	}
}

func TestHandler_CreateRejectsManagerOfOtherSite(t *testing.T) {
	t.Parallel()

	// site:manage narrowed to site 99 does not authorize creating a
	// device at site 10.
	h := NewHandler(nil)
	ctx := handlerstest.CtxWithAssignments(t, 42,
		handlerstest.SiteAssignment(99, authz.PermSiteRead, authz.PermSiteManage))
	_, err := h.CreateInfrastructureDevice(ctx, connect.NewRequest(validCreateRequest()))
	requirePermissionDenied(t, err)
}

func TestHandler_GetDeleteUpdateAuthorizeAgainstDeviceSite(t *testing.T) {
	t.Parallel()

	// The device lives at site 10; the caller's grants are narrowed to
	// site 99, so resolve-then-authorize must deny all three verbs.
	// The denial is masked as NotFound — the caller cannot read site
	// 10, so an existing device ID must be indistinguishable from a
	// missing one.
	h := newTestHandler(t)
	ctx := handlerstest.CtxWithAssignments(t, 42,
		handlerstest.SiteAssignment(99, authz.PermSiteRead, authz.PermSiteManage))

	h.store.EXPECT().GetInfrastructureDevice(gomock.Any(), int64(42), int64(7)).
		Return(deviceAtSite(7, 10), nil).Times(3)

	_, err := h.handler.GetInfrastructureDevice(ctx, connect.NewRequest(&pb.GetInfrastructureDeviceRequest{Id: 7}))
	requireNotFound(t, err)

	_, err = h.handler.DeleteInfrastructureDevice(ctx, connect.NewRequest(&pb.DeleteInfrastructureDeviceRequest{Id: 7}))
	requireNotFound(t, err)

	update := &pb.UpdateInfrastructureDeviceRequest{
		Id: 7, SiteId: 10, Name: "renamed", DeviceKind: models.KindFanGroup,
		FanCount: 12, DriverType: "modbus_tcp", DriverConfig: validConfig,
	}
	_, err = h.handler.UpdateInfrastructureDevice(ctx, connect.NewRequest(update))
	requireNotFound(t, err)
}

// TestHandler_UpdateDeleteDenyReadOnlyCaller pins the boundary between
// the two denial shapes: a caller who CAN read the device's site but
// lacks site:manage gets PermissionDenied (the device's existence is
// already visible to them), not the NotFound mask reserved for
// unreadable sites.
func TestHandler_UpdateDeleteDenyReadOnlyCaller(t *testing.T) {
	t.Parallel()

	h := newTestHandler(t)
	ctx := handlerstest.CtxWithAssignments(t, 42,
		handlerstest.SiteAssignment(10, authz.PermSiteRead))

	h.store.EXPECT().GetInfrastructureDevice(gomock.Any(), int64(42), int64(7)).
		Return(deviceAtSite(7, 10), nil).Times(2)

	_, err := h.handler.DeleteInfrastructureDevice(ctx, connect.NewRequest(&pb.DeleteInfrastructureDeviceRequest{Id: 7}))
	requirePermissionDenied(t, err)

	update := &pb.UpdateInfrastructureDeviceRequest{
		Id: 7, SiteId: 10, Name: "renamed", DeviceKind: models.KindFanGroup,
		FanCount: 12, DriverType: "modbus_tcp", DriverConfig: validConfig,
	}
	_, err = h.handler.UpdateInfrastructureDevice(ctx, connect.NewRequest(update))
	requirePermissionDenied(t, err)
}

func TestHandler_UpdateMoveRequiresManageOnBothSites(t *testing.T) {
	t.Parallel()

	// Caller manages the device's current site (10) but not the target
	// site (11): moving the device must be denied.
	h := newTestHandler(t)
	ctx := handlerstest.CtxWithAssignments(t, 42,
		handlerstest.SiteAssignment(10, authz.PermSiteRead, authz.PermSiteManage))

	h.store.EXPECT().GetInfrastructureDevice(gomock.Any(), int64(42), int64(7)).
		Return(deviceAtSite(7, 10), nil)

	update := &pb.UpdateInfrastructureDeviceRequest{
		Id: 7, SiteId: 11, Name: "moved", DeviceKind: models.KindFanGroup,
		FanCount: 12, DriverType: "modbus_tcp", DriverConfig: validConfig,
	}
	_, err := h.handler.UpdateInfrastructureDevice(ctx, connect.NewRequest(update))
	requirePermissionDenied(t, err)
}

func TestHandler_ListFiltersToReadableSites(t *testing.T) {
	t.Parallel()

	// Caller narrowed to site 10: the readable allowlist is pushed into
	// the store filter so unreadable rows are never fetched, and the
	// caller — holding only site:read — gets no driver_config.
	h := newTestHandler(t)
	ctx := handlerstest.CtxWithAssignments(t, 42,
		handlerstest.SiteAssignment(10, authz.PermSiteRead))

	h.store.EXPECT().ListInfrastructureDevices(gomock.Any(),
		models.ListFilter{OrgID: 42, SiteIDs: []int64{10}}).
		Return([]models.Device{*deviceAtSite(1, 10)}, nil)

	resp, err := h.handler.ListInfrastructureDevices(ctx, connect.NewRequest(&pb.ListInfrastructureDevicesRequest{}))
	require.NoError(t, err)
	require.Len(t, resp.Msg.GetDevices(), 1)
	assert.Equal(t, int64(1), resp.Msg.GetDevices()[0].GetId())
	assert.Equal(t, int64(10), resp.Msg.GetDevices()[0].GetSiteId())
	assert.Empty(t, resp.Msg.GetDevices()[0].GetDriverConfig(),
		"site:read caller must not receive driver_config")
}

func TestHandler_ListPushesNarrowingDenylistIntoFilter(t *testing.T) {
	t.Parallel()

	// Org-wide site:read narrowed away at site 11 (zero-permission
	// site assignment): the handler queries with site 11 excluded
	// rather than fetching the whole org and dropping rows.
	h := newTestHandler(t)
	ctx := handlerstest.CtxWithAssignments(t, 42,
		handlerstest.OrgAssignment(authz.PermSiteRead),
		handlerstest.SiteAssignment(11))

	h.store.EXPECT().ListInfrastructureDevices(gomock.Any(),
		models.ListFilter{OrgID: 42, ExcludedSiteIDs: []int64{11}}).
		Return([]models.Device{*deviceAtSite(1, 10)}, nil)

	resp, err := h.handler.ListInfrastructureDevices(ctx, connect.NewRequest(&pb.ListInfrastructureDevicesRequest{}))
	require.NoError(t, err)
	require.Len(t, resp.Msg.GetDevices(), 1)
	assert.Equal(t, int64(10), resp.Msg.GetDevices()[0].GetSiteId())
}

func TestHandler_ListIntersectsRequestFilterWithReadableSites(t *testing.T) {
	t.Parallel()

	// Caller readable at sites 10 and 12 asks for sites 10 and 11: the
	// store filter is the intersection (10 only).
	h := newTestHandler(t)
	ctx := handlerstest.CtxWithAssignments(t, 42,
		handlerstest.SiteAssignment(10, authz.PermSiteRead),
		handlerstest.SiteAssignment(12, authz.PermSiteRead))

	h.store.EXPECT().ListInfrastructureDevices(gomock.Any(),
		models.ListFilter{OrgID: 42, SiteIDs: []int64{10}}).
		Return([]models.Device{*deviceAtSite(1, 10)}, nil)

	resp, err := h.handler.ListInfrastructureDevices(ctx,
		connect.NewRequest(&pb.ListInfrastructureDevicesRequest{SiteIds: []int64{10, 11}}))
	require.NoError(t, err)
	require.Len(t, resp.Msg.GetDevices(), 1)
}

func TestHandler_ListFiltersToCurtailmentManageableSitesWhenRequested(t *testing.T) {
	t.Parallel()

	h := newTestHandler(t)
	ctx := handlerstest.CtxWithAssignments(t, 42,
		handlerstest.OrgAssignment(authz.PermSiteRead, authz.PermCurtailmentManage),
		handlerstest.SiteAssignment(11, authz.PermSiteRead),
	)

	h.store.EXPECT().ListInfrastructureDevices(gomock.Any(), models.ListFilter{OrgID: 42}).
		Return([]models.Device{*deviceAtSite(1, 10), *deviceAtSite(2, 11)}, nil)

	resp, err := h.handler.ListInfrastructureDevices(ctx, connect.NewRequest(&pb.ListInfrastructureDevicesRequest{
		RequireCurtailmentManage: true,
	}))

	require.NoError(t, err)
	require.Len(t, resp.Msg.GetDevices(), 1)
	assert.Equal(t, int64(10), resp.Msg.GetDevices()[0].GetSiteId())
}

func TestHandler_ListReturnsEmptyWithoutQueryWhenNoReadableSites(t *testing.T) {
	t.Parallel()

	// No store EXPECT: a caller with no readable site must get an empty
	// response without a query — an empty SiteIDs filter would mean
	// "all sites", so passing the empty allowlist through would leak.
	h := newTestHandler(t)

	cases := []struct {
		name       string
		assignment authz.Assignment
		req        *pb.ListInfrastructureDevicesRequest
	}{
		{
			name:       "no site grants at all",
			assignment: handlerstest.SiteAssignment(10, authz.PermFleetRead),
			req:        &pb.ListInfrastructureDevicesRequest{},
		},
		{
			name:       "requested sites disjoint from readable sites",
			assignment: handlerstest.SiteAssignment(10, authz.PermSiteRead),
			req:        &pb.ListInfrastructureDevicesRequest{SiteIds: []int64{11}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := handlerstest.CtxWithAssignments(t, 42, tc.assignment)
			resp, err := h.handler.ListInfrastructureDevices(ctx, connect.NewRequest(tc.req))
			require.NoError(t, err)
			assert.Empty(t, resp.Msg.GetDevices())
		})
	}
}

func TestHandler_DriverConfigRedactedForReadOnlyCallers(t *testing.T) {
	t.Parallel()

	// driver_config carries the OT control topology: Get returns it
	// only to site:manage holders; site:read callers get the display
	// fields with an empty blob. List behaves the same per device.
	h := newTestHandler(t)
	h.store.EXPECT().GetInfrastructureDevice(gomock.Any(), int64(42), int64(7)).
		Return(deviceAtSite(7, 10), nil).Times(2)

	readOnly := handlerstest.CtxWithAssignments(t, 42,
		handlerstest.SiteAssignment(10, authz.PermSiteRead))
	resp, err := h.handler.GetInfrastructureDevice(readOnly, connect.NewRequest(&pb.GetInfrastructureDeviceRequest{Id: 7}))
	require.NoError(t, err)
	assert.Empty(t, resp.Msg.GetDevice().GetDriverConfig())
	assert.Equal(t, "modbus_tcp", resp.Msg.GetDevice().GetDriverType(),
		"display fields remain visible to site:read callers")

	manager := handlerstest.CtxWithAssignments(t, 42,
		handlerstest.SiteAssignment(10, authz.PermSiteRead, authz.PermSiteManage))
	resp, err = h.handler.GetInfrastructureDevice(manager, connect.NewRequest(&pb.GetInfrastructureDeviceRequest{Id: 7}))
	require.NoError(t, err)
	assert.JSONEq(t, validConfig, resp.Msg.GetDevice().GetDriverConfig())
}

func TestHandler_UpdatePredicatesWriteOnAuthorizedSite(t *testing.T) {
	t.Parallel()

	// The handler must carry the device's current site (as read for
	// authorization) into the write as ExpectedSiteID, so the store can
	// fail closed on a concurrent move.
	h := newTestHandler(t)
	ctx := sitePermsCtx(t, 42)

	h.store.EXPECT().GetInfrastructureDevice(gomock.Any(), int64(42), int64(7)).
		Return(deviceAtSite(7, 10), nil).Times(2)
	h.siteStore.EXPECT().LockSiteForWrite(gomock.Any(), int64(42), int64(10)).Return(nil)
	h.store.EXPECT().LockInfrastructureDeviceForWrite(gomock.Any(), int64(42), int64(7), int64(10)).Return(nil)
	h.store.EXPECT().UpdateInfrastructureDevice(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, params models.UpdateParams) (*models.Device, error) {
			assert.Equal(t, int64(10), params.ExpectedSiteID)
			assert.Equal(t, int64(10), params.SiteID)
			return deviceAtSite(7, 10), nil
		},
	)

	update := &pb.UpdateInfrastructureDeviceRequest{
		Id: 7, SiteId: 10, Name: "renamed", DeviceKind: models.KindFanGroup,
		FanCount: 12, DriverType: "modbus_tcp", DriverConfig: validConfig,
	}
	_, err := h.handler.UpdateInfrastructureDevice(ctx, connect.NewRequest(update))
	require.NoError(t, err)
}

func TestHandler_UpdateCarriesEnabledPresenceIntoParams(t *testing.T) {
	t.Parallel()

	// The enabled pointer passes through untouched: omitted stays nil
	// so the store's UPDATE preserves the row's current value
	// atomically (COALESCE in SQL), and an explicit value is carried
	// as-is. The store-level preservation semantics are pinned by the
	// domain integration test.
	cases := []struct {
		name            string
		requestEnabled  *bool
		expectedEnabled *bool
	}{
		{"omitted stays nil (SQL preserves current value)", nil, nil},
		{"explicit true carried", boolPtr(true), boolPtr(true)},
		{"explicit false carried", boolPtr(false), boolPtr(false)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := newTestHandler(t)
			ctx := sitePermsCtx(t, 42)

			h.store.EXPECT().GetInfrastructureDevice(gomock.Any(), int64(42), int64(7)).
				Return(deviceAtSite(7, 10), nil).Times(2)
			h.siteStore.EXPECT().LockSiteForWrite(gomock.Any(), int64(42), int64(10)).Return(nil)
			h.store.EXPECT().LockInfrastructureDeviceForWrite(gomock.Any(), int64(42), int64(7), int64(10)).Return(nil)
			if tc.requestEnabled != nil && !*tc.requestEnabled {
				h.store.EXPECT().CountActiveCurtailmentEventsByInfrastructureDevice(gomock.Any(), int64(42), int64(7)).Return(int64(0), nil)
			}
			h.store.EXPECT().UpdateInfrastructureDevice(gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ context.Context, params models.UpdateParams) (*models.Device, error) {
					assert.Equal(t, tc.expectedEnabled, params.Enabled)
					return deviceAtSite(7, 10), nil
				},
			)

			update := &pb.UpdateInfrastructureDeviceRequest{
				Id: 7, SiteId: 10, Name: "renamed", DeviceKind: models.KindFanGroup,
				FanCount: 12, Enabled: tc.requestEnabled,
				DriverType: "modbus_tcp", DriverConfig: validConfig,
			}
			_, err := h.handler.UpdateInfrastructureDevice(ctx, connect.NewRequest(update))
			require.NoError(t, err)
		})
	}
}

func boolPtr(b bool) *bool { return &b }

func TestHandler_unauthenticatedWithoutSession(t *testing.T) {
	t.Parallel()

	h := NewHandler(nil)
	_, err := h.ListInfrastructureDevices(t.Context(), connect.NewRequest(&pb.ListInfrastructureDevicesRequest{}))
	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodeUnauthenticated, fleetErr.GRPCCode)
}

func TestHandler_CreateTranslatesRoundTrip(t *testing.T) {
	t.Parallel()

	h := newTestHandler(t)
	ctx := sitePermsCtx(t, 42)

	h.siteStore.EXPECT().LockSiteForWrite(gomock.Any(), int64(42), int64(10)).Return(nil)
	h.store.EXPECT().CreateInfrastructureDevice(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, params models.CreateParams) (*models.Device, error) {
			// Translation carries the org from the session and the
			// request fields into domain params.
			assert.Equal(t, int64(42), params.OrgID)
			assert.Equal(t, int64(10), params.SiteID)
			assert.Equal(t, "Zone A exhaust fans", params.Name)
			assert.JSONEq(t, validConfig, string(params.DriverConfig))
			return &models.Device{
				ID:           7,
				OrgID:        params.OrgID,
				SiteID:       params.SiteID,
				SiteLabel:    "Denton",
				BuildingName: params.BuildingName,
				Name:         params.Name,
				DeviceKind:   params.DeviceKind,
				FanCount:     params.FanCount,
				Enabled:      params.Enabled,
				DriverType:   params.DriverType,
				DriverConfig: json.RawMessage(validConfig),
			}, nil
		},
	)

	resp, err := h.handler.CreateInfrastructureDevice(ctx, connect.NewRequest(validCreateRequest()))
	require.NoError(t, err)
	device := resp.Msg.GetDevice()
	require.NotNil(t, device)
	assert.Equal(t, int64(7), device.GetId())
	assert.Equal(t, "Denton", device.GetSiteLabel())
	assert.Equal(t, int32(12), device.GetFanCount())
	assert.JSONEq(t, validConfig, device.GetDriverConfig())
}

func TestHandler_CreateRejectsEmptyDriverConfig(t *testing.T) {
	t.Parallel()

	h := newTestHandler(t)
	ctx := sitePermsCtx(t, 42)

	req := validCreateRequest()
	req.DriverConfig = ""
	_, err := h.handler.CreateInfrastructureDevice(ctx, connect.NewRequest(req))
	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodeInvalidArgument, fleetErr.GRPCCode)
	assert.Contains(t, err.Error(), "driver_config is required")
}

func TestHandler_CreateDefaultsOmittedEnabledToTrue(t *testing.T) {
	t.Parallel()

	h := newTestHandler(t)
	ctx := sitePermsCtx(t, 42)

	h.siteStore.EXPECT().LockSiteForWrite(gomock.Any(), int64(42), int64(10)).Return(nil).Times(2)
	var seen []bool
	h.store.EXPECT().CreateInfrastructureDevice(gomock.Any(), gomock.Any()).Times(2).DoAndReturn(
		func(_ context.Context, params models.CreateParams) (*models.Device, error) {
			seen = append(seen, params.Enabled)
			return deviceAtSite(7, 10), nil
		},
	)

	// Omitted enabled defaults to true (matching the column default).
	req := validCreateRequest()
	req.Enabled = nil
	_, err := h.handler.CreateInfrastructureDevice(ctx, connect.NewRequest(req))
	require.NoError(t, err)

	// Explicit false is preserved.
	disabled := false
	req = validCreateRequest()
	req.Enabled = &disabled
	_, err = h.handler.CreateInfrastructureDevice(ctx, connect.NewRequest(req))
	require.NoError(t, err)

	assert.Equal(t, []bool{true, false}, seen)
}

func TestHandler_CreateRejectsBlankDriverType(t *testing.T) {
	t.Parallel()

	h := newTestHandler(t)
	ctx := sitePermsCtx(t, 42)

	req := validCreateRequest()
	req.DriverType = "   "
	_, err := h.handler.CreateInfrastructureDevice(ctx, connect.NewRequest(req))
	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodeInvalidArgument, fleetErr.GRPCCode)
	assert.Contains(t, err.Error(), "driver_type is required")
}
