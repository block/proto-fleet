package miner_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/miner"
	"github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/miner/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	sdkMocks "github.com/block/proto-fleet/server/sdk/v1/mocks"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc/codes"
)

// frigateUpdateDevice adds the Frigate update surface to a mock device and
// counts the calls that reached it.
type frigateUpdateDevice struct {
	*sdkMocks.MockDevice
	calls int
	err   error
}

func (d *frigateUpdateDevice) hit() error { d.calls++; return d.err }

func (d *frigateUpdateDevice) FrigateUpdateSupport(context.Context) ([]sdk.FrigateInstanceSupport, error) {
	return nil, d.hit()
}
func (d *frigateUpdateDevice) FrigateStageBegin(context.Context, string, sdk.FrigateManifest) (sdk.FrigateStageProgress, error) {
	return sdk.FrigateStageProgress{}, d.hit()
}
func (d *frigateUpdateDevice) FrigateStageAppend(context.Context, string, sdk.FrigateStageAppend) (sdk.FrigateStageProgress, error) {
	return sdk.FrigateStageProgress{}, d.hit()
}
func (d *frigateUpdateDevice) FrigateStageCommit(context.Context, string, sdk.FrigateStageRef) (sdk.FrigateStageCommit, error) {
	return sdk.FrigateStageCommit{}, d.hit()
}
func (d *frigateUpdateDevice) FrigateStageAbort(context.Context, string, sdk.FrigateStageRef) error {
	return d.hit()
}
func (d *frigateUpdateDevice) FrigateUpdateStart(context.Context, sdk.FrigateStartRequest) (sdk.FrigateRollout, error) {
	return sdk.FrigateRollout{}, d.hit()
}
func (d *frigateUpdateDevice) FrigateUpdateStatus(context.Context, string) (sdk.FrigateStatus, error) {
	return sdk.FrigateStatus{}, d.hit()
}
func (d *frigateUpdateDevice) FrigateUpdateRetry(context.Context, sdk.FrigateAction) (sdk.FrigateRollout, error) {
	return sdk.FrigateRollout{}, d.hit()
}
func (d *frigateUpdateDevice) FrigateUpdateCancel(context.Context, sdk.FrigateAction) (sdk.FrigateRollout, error) {
	return sdk.FrigateRollout{}, d.hit()
}

func frigateUpdateHandle(t *testing.T, identifier models.DeviceIdentifier) (*frigateUpdateDevice, interfaces.FrigateUpdater, int64, *sql.DB) {
	t.Helper()
	conn, crypto, files := setupTestDB(t)
	deviceID := createTestDevice(t, conn, string(identifier))
	createTestMinerCredentials(t, conn, crypto, deviceID)
	ctrl := gomock.NewController(t)
	device := &frigateUpdateDevice{MockDevice: sdkMocks.NewMockDevice(ctrl)}
	driver := sdkMocks.NewMockDriver(ctrl)
	driver.EXPECT().NewDevice(gomock.Any(), string(identifier), gomock.Any(), gomock.Any()).Return(sdk.NewDeviceResult{Device: device}, nil).AnyTimes()
	manager := &fakePluginManager{driver: driver, driverName: "antminer", caps: sdk.Capabilities{sdk.CapabilityFrigateServerUpdate: true}}
	service := miner.NewMinerService(conn, sqlstores.NewSQLUserStore(conn), crypto, files, manager)
	handle, err := service.GetMinerFromDeviceIdentifier(t.Context(), identifier)
	require.NoError(t, err)
	updater, ok := handle.(interfaces.FrigateUpdater)
	require.True(t, ok, "a direct handle exposes the Frigate update surface")
	return device, updater, deviceID, conn
}

// frigateMutations are the update calls that change controller state.
func frigateMutations(ctx context.Context, u interfaces.FrigateUpdater) map[string]func() error {
	return map[string]func() error{
		"begin":  func() error { _, err := u.FrigateStageBegin(ctx, "a", sdk.FrigateManifest{}); return err },
		"append": func() error { _, err := u.FrigateStageAppend(ctx, "a", sdk.FrigateStageAppend{}); return err },
		"commit": func() error { _, err := u.FrigateStageCommit(ctx, "a", sdk.FrigateStageRef{}); return err },
		"abort":  func() error { return u.FrigateStageAbort(ctx, "a", sdk.FrigateStageRef{}) },
		"start":  func() error { _, err := u.FrigateUpdateStart(ctx, sdk.FrigateStartRequest{}); return err },
		"retry":  func() error { _, err := u.FrigateUpdateRetry(ctx, sdk.FrigateAction{}); return err },
		"cancel": func() error { _, err := u.FrigateUpdateCancel(ctx, sdk.FrigateAction{}); return err },
	}
}

func TestFrigateUpdateMutationsRejectAChangedBindingBeforeDispatch(t *testing.T) {
	for _, change := range []string{"endpoint", "credentials", "unpaired", "deleted"} {
		t.Run(change, func(t *testing.T) {
			device, updater, deviceID, conn := frigateUpdateHandle(t, "frigate-guarded-"+models.DeviceIdentifier(change))
			q := sqlc.New(conn)
			exec := func(query string, args ...any) {
				_, err := conn.ExecContext(t.Context(), query, args...)
				require.NoError(t, err)
			}
			switch change {
			case "endpoint":
				require.NoError(t, q.UpdateDeviceIPAssignment(t.Context(), sqlc.UpdateDeviceIPAssignmentParams{ID: deviceID, IpAddress: "192.0.2.40", Port: "9095", UrlScheme: "https"}))
			case "credentials":
				exec("UPDATE miner_credentials SET username_enc=password_enc WHERE device_id=$1", deviceID)
			case "unpaired":
				_, err := q.UpsertDevicePairing(t.Context(), sqlc.UpsertDevicePairingParams{DeviceID: deviceID, PairingStatus: sqlc.PairingStatusEnumUNPAIRED})
				require.NoError(t, err)
			case "deleted":
				exec("UPDATE device SET deleted_at=statement_timestamp() WHERE id=$1", deviceID)
			}
			for name, call := range frigateMutations(t.Context(), updater) {
				err := call()
				require.ErrorIs(t, err, devicewrite.ErrStaleBinding, name)
				require.True(t, devicewrite.IsNotDispatched(err), name)
			}
			require.Zero(t, device.calls, "no update call reaches a controller through a stale binding")
		})
	}
}

func TestFrigateUpdateMutationsLeaveNoUnresolvedWrite(t *testing.T) {
	device, updater, deviceID, conn := frigateUpdateHandle(t, "frigate-gated-outcomes")
	for name, call := range frigateMutations(t.Context(), updater) {
		require.NoError(t, call(), name)
	}
	// A refusal, and a reply lost in transit, both keep their error.
	device.err = &sdk.FrigateRefusal{Reason: "UPDATE_ACTIVE_JOB", Message: "busy", Code: codes.FailedPrecondition}
	_, err := updater.FrigateUpdateStart(t.Context(), sdk.FrigateStartRequest{})
	refusal := sdk.AsFrigateRefusal(err)
	require.NotNil(t, refusal, "the refusal survives the write gate")
	require.Equal(t, "UPDATE_ACTIVE_JOB", refusal.Reason)
	device.err = context.DeadlineExceeded
	_, err = updater.FrigateStageAppend(t.Context(), "a", sdk.FrigateStageAppend{})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Equal(t, 9, device.calls)

	// Update calls are reconciled from Frigate's own status, so none of them
	// leaves an unknown device write or quarantines the controller.
	var total, unresolved int
	var state string
	row := conn.QueryRowContext(t.Context(), "SELECT count(*), count(*) FILTER (WHERE status<>'complete'), (SELECT state FROM device_write_fence WHERE device_id=$1) FROM device_write_attempt WHERE device_id=$1", deviceID)
	require.NoError(t, row.Scan(&total, &unresolved, &state))
	require.Equal(t, 9, total, "every mutation was admitted as a device write")
	require.Zero(t, unresolved)
	require.Equal(t, "host", state)
}
