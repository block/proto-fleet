package miner_test

import (
	"context"
	"database/sql"
	"sync"
	"testing"
	"time"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/miner"
	"github.com/block/proto-fleet/server/internal/domain/miner/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	sdkMocks "github.com/block/proto-fleet/server/sdk/v1/mocks"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Prove the writer actually reached a PostgreSQL lock wait, rather than merely
// giving its goroutine insufficient time to run.
func waitForBindingWriter(t *testing.T, conn *sql.DB, pid int) {
	t.Helper()
	require.Eventually(t, func() bool {
		var waiting bool
		err := conn.QueryRowContext(t.Context(), "SELECT COALESCE(wait_event_type='Lock',false) FROM pg_stat_activity WHERE pid=$1", pid).Scan(&waiting)
		return err == nil && waiting
	}, 5*time.Second, 10*time.Millisecond)
}

func TestDirectDispatchSerializesBindingWritersThroughCanceledCall(t *testing.T) {
	for _, change := range []string{"endpoint", "credentials", "pairing", "delete", "node insertion"} {
		t.Run(change, func(t *testing.T) {
			conn, crypto, files := setupTestDB(t)
			identifier := models.DeviceIdentifier("binding-lock-" + change)
			deviceID := createTestDevice(t, conn, string(identifier))
			createTestMinerCredentials(t, conn, crypto, deviceID)
			nodeID := createTestFleetNode(t, conn, "binding-lock-node", []byte("binding-lock-node"))
			ctrl := gomock.NewController(t)
			dev := sdkMocks.NewMockDevice(ctrl)
			driver := sdkMocks.NewMockDriver(ctrl)
			driver.EXPECT().NewDevice(gomock.Any(), string(identifier), gomock.Any(), gomock.Any()).Return(sdk.NewDeviceResult{Device: dev}, nil)
			entered, release := make(chan struct{}), make(chan struct{})
			finish := sync.OnceFunc(func() { close(release) })
			t.Cleanup(finish)
			dev.EXPECT().Reboot(gomock.Any()).DoAndReturn(func(context.Context) error { close(entered); <-release; return nil })
			service := miner.NewMinerService(conn, sqlstores.NewSQLUserStore(conn), crypto, files, &fakePluginManager{driver: driver, driverName: "antminer"})
			handle, err := service.GetMinerFromDeviceIdentifier(t.Context(), identifier)
			require.NoError(t, err)
			callCtx, cancel := context.WithCancel(t.Context())
			defer cancel()
			callDone := make(chan error, 1)
			go func() { callDone <- handle.Reboot(callCtx) }()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("SDK call did not start")
			}
			writer, err := conn.Conn(t.Context())
			require.NoError(t, err)
			defer writer.Close()
			var pid int
			require.NoError(t, writer.QueryRowContext(t.Context(), "SELECT pg_backend_pid()").Scan(&pid))
			writerDone := make(chan error, 1)
			go func() {
				q := sqlc.New(writer)
				var err error
				switch change {
				case "endpoint":
					err = q.UpdateDeviceIPAssignment(t.Context(), sqlc.UpdateDeviceIPAssignmentParams{ID: deviceID, IpAddress: "192.0.2.55", Port: "4028", UrlScheme: "http"})
				case "credentials":
					err = q.UpsertMinerCredentials(t.Context(), sqlc.UpsertMinerCredentialsParams{DeviceID: deviceID, UsernameEnc: "replacement", PasswordEnc: "replacement"})
				case "pairing":
					_, err = q.UpsertDevicePairing(t.Context(), sqlc.UpsertDevicePairingParams{DeviceID: deviceID, PairingStatus: sqlc.PairingStatusEnumUNPAIRED})
				case "delete":
					_, err = writer.ExecContext(t.Context(), "UPDATE device SET deleted_at=statement_timestamp() WHERE id=$1", deviceID)
				case "node insertion":
					_, err = writer.ExecContext(t.Context(), "INSERT INTO fleet_node_device(fleet_node_id,device_id,org_id) VALUES($1,$2,1)", nodeID, deviceID)
				}
				writerDone <- err
			}()
			waitForBindingWriter(t, conn, pid)
			cancel()
			// The RPC is canceled, but this controlled adapter is still executing.
			// Canceling the lease must not let the binding writer commit prematurely.
			waitForBindingWriter(t, conn, pid)
			select {
			case err := <-writerDone:
				t.Fatalf("writer committed while SDK call was running: %v", err)
			default:
			}
			finish()
			require.NoError(t, <-writerDone)
			require.NoError(t, <-callDone)
			require.Error(t, handle.Reboot(t.Context()), "writer invalidates the old handle after actual call completion")
		})
	}
}

type bindingBlockingSender struct{ entered, release chan struct{} }

func (s *bindingBlockingSender) SendCommand(context.Context, int64, gatewaypb.CommandProtocolVersion, *gatewaypb.ControlCommand) (*gatewaypb.ControlAck, error) {
	close(s.entered)
	<-s.release
	return &gatewaypb.ControlAck{Succeeded: true, Code: gatewaypb.AckCode_ACK_CODE_OK}, nil
}

func TestRemoteDispatchSerializesExistingRouteAndNodeWriters(t *testing.T) {
	for _, change := range []string{"route deletion", "node retirement", "endpoint", "credentials"} {
		t.Run(change, func(t *testing.T) {
			conn, crypto, files := setupTestDB(t)
			identifier := models.DeviceIdentifier("remote-binding-lock-" + change)
			deviceID := createTestDevice(t, conn, string(identifier))
			nodeID := createTestFleetNode(t, conn, "remote-binding-lock-node", []byte("remote-binding-lock-node"))
			_, err := conn.ExecContext(t.Context(), "INSERT INTO fleet_node_device(fleet_node_id,device_id,org_id) VALUES($1,$2,1)", nodeID, deviceID)
			require.NoError(t, err)
			sender := &bindingBlockingSender{entered: make(chan struct{}), release: make(chan struct{})}
			finish := sync.OnceFunc(func() { close(sender.release) })
			t.Cleanup(finish)
			service := miner.NewMinerService(conn, sqlstores.NewSQLUserStore(conn), crypto, files, &fakePluginManager{driverName: "antminer"}).WithCommandSender(sender)
			handle, err := service.GetMinerFromDeviceIdentifier(t.Context(), identifier)
			require.NoError(t, err)
			callDone := make(chan error, 1)
			go func() { callDone <- handle.Reboot(t.Context()) }()
			select {
			case <-sender.entered:
			case <-time.After(5 * time.Second):
				t.Fatal("node send did not start")
			}
			writer, err := conn.Conn(t.Context())
			require.NoError(t, err)
			defer writer.Close()
			var pid int
			require.NoError(t, writer.QueryRowContext(t.Context(), "SELECT pg_backend_pid()").Scan(&pid))
			writerDone := make(chan error, 1)
			go func() {
				var err error
				switch change {
				case "route deletion":
					_, err = writer.ExecContext(t.Context(), "DELETE FROM fleet_node_device WHERE device_id=$1", deviceID)
				case "node retirement":
					_, err = writer.ExecContext(t.Context(), "UPDATE fleet_node SET deleted_at=statement_timestamp() WHERE id=$1", nodeID)
				case "endpoint":
					err = sqlc.New(writer).UpdateDeviceIPAssignment(t.Context(), sqlc.UpdateDeviceIPAssignmentParams{ID: deviceID, IpAddress: "192.0.2.65", Port: "4028", UrlScheme: "http"})
				case "credentials":
					err = sqlc.New(writer).UpsertMinerCredentials(t.Context(), sqlc.UpsertMinerCredentialsParams{DeviceID: deviceID, UsernameEnc: "replacement", PasswordEnc: "replacement"})
				}
				writerDone <- err
			}()
			waitForBindingWriter(t, conn, pid)
			finish()
			require.NoError(t, <-writerDone)
			require.NoError(t, <-callDone)
			require.Error(t, handle.Reboot(t.Context()))
		})
	}
}

func TestBindingDispatchUsesSinglePoolConnection(t *testing.T) {
	conn, crypto, files := setupTestDB(t)
	identifier := models.DeviceIdentifier("single-connection-binding")
	deviceID := createTestDevice(t, conn, string(identifier))
	createTestMinerCredentials(t, conn, crypto, deviceID)
	ctrl := gomock.NewController(t)
	dev := sdkMocks.NewMockDevice(ctrl)
	driver := sdkMocks.NewMockDriver(ctrl)
	driver.EXPECT().NewDevice(gomock.Any(), string(identifier), gomock.Any(), gomock.Any()).Return(sdk.NewDeviceResult{Device: dev}, nil)
	dev.EXPECT().Reboot(gomock.Any()).Return(nil)
	service := miner.NewMinerService(conn, sqlstores.NewSQLUserStore(conn), crypto, files, &fakePluginManager{driver: driver, driverName: "antminer"})
	handle, err := service.GetMinerFromDeviceIdentifier(t.Context(), identifier)
	require.NoError(t, err)
	conn.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	require.NoError(t, handle.Reboot(ctx), "dispatch recheck must use the leased connection, not request a second one")
}

func TestSDKGenerationRejectionSurvivesFleetAdapterMapping(t *testing.T) {
	conn, crypto, files := setupTestDB(t)
	identifier := models.DeviceIdentifier("generation-adapter-rejection")
	deviceID := createTestDevice(t, conn, string(identifier))
	createTestMinerCredentials(t, conn, crypto, deviceID)
	ctrl := gomock.NewController(t)
	dev := sdkMocks.NewMockDevice(ctrl)
	driver := sdkMocks.NewMockDriver(ctrl)
	driver.EXPECT().NewDevice(gomock.Any(), string(identifier), gomock.Any(), gomock.Any()).Return(sdk.NewDeviceResult{Device: dev}, nil)
	rejected, err := status.New(codes.FailedPrecondition, "registration changed").WithDetails(&errdetails.ErrorInfo{Reason: "SDK_BACKEND_GENERATION_MISMATCH", Domain: "protofleet.sdk"})
	require.NoError(t, err)
	dev.EXPECT().Reboot(gomock.Any()).Return(rejected.Err())
	service := miner.NewMinerService(conn, sqlstores.NewSQLUserStore(conn), crypto, files, &fakePluginManager{driver: driver, driverName: "antminer"})
	handle, err := service.GetMinerFromDeviceIdentifier(t.Context(), identifier)
	require.NoError(t, err)
	err = handle.Reboot(t.Context())
	require.Error(t, err)
	require.True(t, devicewrite.IsNotDispatched(err))
	var unresolved int
	require.NoError(t, conn.QueryRowContext(t.Context(), "SELECT count(*) FROM device_write_attempt WHERE device_id=$1 AND status NOT IN ('complete', 'resolved')", deviceID).Scan(&unresolved))
	require.Zero(t, unresolved)
	_, err = devicewrite.NewService(conn).Reserve(t.Context(), 1, deviceID)
	require.NoError(t, err, "pre-backend rejection must not prevent handoff")
}
