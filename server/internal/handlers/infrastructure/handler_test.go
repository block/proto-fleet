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

func TestHandler_authGate(t *testing.T) {
	t.Parallel()

	h := NewHandler(nil)

	cases := []struct {
		name        string
		permissions []string
	}{
		{"caller without site permissions is rejected", []string{authz.PermFleetRead}},
		{"caller with no permissions is rejected", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := handlerstest.CtxWithPermissions(t, 1, tc.permissions...)

			_, err := h.ListInfrastructureDevices(ctx, connect.NewRequest(&pb.ListInfrastructureDevicesRequest{}))
			require.Error(t, err)
			var fleetErr fleeterror.FleetError
			require.ErrorAs(t, err, &fleetErr)
			assert.Equal(t, connect.CodePermissionDenied, fleetErr.GRPCCode)

			_, err = h.CreateInfrastructureDevice(ctx, connect.NewRequest(validCreateRequest()))
			require.Error(t, err)
			require.ErrorAs(t, err, &fleetErr)
			assert.Equal(t, connect.CodePermissionDenied, fleetErr.GRPCCode)
		})
	}
}

func TestHandler_writesRejectReadOnlyCallers(t *testing.T) {
	t.Parallel()

	h := NewHandler(nil)
	ctx := handlerstest.CtxWithPermissions(t, 1, authz.PermSiteRead)

	var fleetErr fleeterror.FleetError

	_, err := h.CreateInfrastructureDevice(ctx, connect.NewRequest(validCreateRequest()))
	require.Error(t, err)
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodePermissionDenied, fleetErr.GRPCCode)

	_, err = h.UpdateInfrastructureDevice(ctx, connect.NewRequest(&pb.UpdateInfrastructureDeviceRequest{Id: 1}))
	require.Error(t, err)
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodePermissionDenied, fleetErr.GRPCCode)

	_, err = h.DeleteInfrastructureDevice(ctx, connect.NewRequest(&pb.DeleteInfrastructureDeviceRequest{Id: 1}))
	require.Error(t, err)
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodePermissionDenied, fleetErr.GRPCCode)
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
