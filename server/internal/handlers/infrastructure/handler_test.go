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
	svc := infrastructure.NewService(store, siteStore, infrastructure.NewDefaultDriverRegistry(), tx)
	return &testHarness{handler: NewHandler(svc), store: store, siteStore: siteStore}
}

func sitePermsCtx(t *testing.T, orgID int64) context.Context {
	t.Helper()
	return handlerstest.CtxWithPermissions(t, orgID, authz.PermSiteRead, authz.PermSiteManage)
}

const validConfig = `{"endpoint":"10.1.2.3","port":502,"unit_id":5,"register_address":2001,"write_mode":"holding_register"}`

func validCreateRequest() *pb.CreateInfrastructureDeviceRequest {
	return &pb.CreateInfrastructureDeviceRequest{
		SiteId:       10,
		BuildingName: "Building 1",
		Name:         "Zone A exhaust fans",
		DeviceKind:   models.KindFanGroup,
		FanCount:     12,
		Enabled:      true,
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
	h := newTestHandler(t)
	ctx := handlerstest.CtxWithAssignments(t, 42,
		handlerstest.SiteAssignment(99, authz.PermSiteRead, authz.PermSiteManage))

	h.store.EXPECT().GetInfrastructureDevice(gomock.Any(), int64(42), int64(7)).
		Return(deviceAtSite(7, 10), nil).Times(3)

	_, err := h.handler.GetInfrastructureDevice(ctx, connect.NewRequest(&pb.GetInfrastructureDeviceRequest{Id: 7}))
	requirePermissionDenied(t, err)

	_, err = h.handler.DeleteInfrastructureDevice(ctx, connect.NewRequest(&pb.DeleteInfrastructureDeviceRequest{Id: 7}))
	requirePermissionDenied(t, err)

	update := &pb.UpdateInfrastructureDeviceRequest{
		Id: 7, SiteId: 10, Name: "renamed", DeviceKind: models.KindFanGroup,
		FanCount: 12, Enabled: true, DriverType: "modbus_tcp", DriverConfig: validConfig,
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
		FanCount: 12, Enabled: true, DriverType: "modbus_tcp", DriverConfig: validConfig,
	}
	_, err := h.handler.UpdateInfrastructureDevice(ctx, connect.NewRequest(update))
	requirePermissionDenied(t, err)
}

func TestHandler_ListFiltersToReadableSites(t *testing.T) {
	t.Parallel()

	// Two devices at different sites; caller narrowed to site 10 sees
	// only that site's device.
	h := newTestHandler(t)
	ctx := handlerstest.CtxWithAssignments(t, 42,
		handlerstest.SiteAssignment(10, authz.PermSiteRead))

	h.store.EXPECT().ListInfrastructureDevices(gomock.Any(), models.ListFilter{OrgID: 42}).
		Return([]models.Device{*deviceAtSite(1, 10), *deviceAtSite(2, 11)}, nil)

	resp, err := h.handler.ListInfrastructureDevices(ctx, connect.NewRequest(&pb.ListInfrastructureDevicesRequest{}))
	require.NoError(t, err)
	require.Len(t, resp.Msg.GetDevices(), 1)
	assert.Equal(t, int64(1), resp.Msg.GetDevices()[0].GetId())
	assert.Equal(t, int64(10), resp.Msg.GetDevices()[0].GetSiteId())
}

func TestHandler_UpdatePredicatesWriteOnAuthorizedSite(t *testing.T) {
	t.Parallel()

	// The handler must carry the device's current site (as read for
	// authorization) into the write as ExpectedSiteID, so the store can
	// fail closed on a concurrent move.
	h := newTestHandler(t)
	ctx := sitePermsCtx(t, 42)

	h.store.EXPECT().GetInfrastructureDevice(gomock.Any(), int64(42), int64(7)).
		Return(deviceAtSite(7, 10), nil)
	h.siteStore.EXPECT().LockSiteForWrite(gomock.Any(), int64(42), int64(10)).Return(nil)
	h.store.EXPECT().UpdateInfrastructureDevice(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, params models.UpdateParams) (*models.Device, error) {
			assert.Equal(t, int64(10), params.ExpectedSiteID)
			assert.Equal(t, int64(10), params.SiteID)
			return deviceAtSite(7, 10), nil
		},
	)

	update := &pb.UpdateInfrastructureDeviceRequest{
		Id: 7, SiteId: 10, Name: "renamed", DeviceKind: models.KindFanGroup,
		FanCount: 12, Enabled: true, DriverType: "modbus_tcp", DriverConfig: validConfig,
	}
	_, err := h.handler.UpdateInfrastructureDevice(ctx, connect.NewRequest(update))
	require.NoError(t, err)
}

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
