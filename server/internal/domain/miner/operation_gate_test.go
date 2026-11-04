package miner_test

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/miner"
	"github.com/block/proto-fleet/server/internal/domain/miner/dto"
	"github.com/block/proto-fleet/server/internal/domain/miner/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	sdkMocks "github.com/block/proto-fleet/server/sdk/v1/mocks"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

func TestCachedDirectHandleRejectsChangedBindingBeforeSDKCall(t *testing.T) {
	for _, change := range []string{"endpoint", "credentials", "unpaired", "deleted", "node route"} {
		t.Run(change, func(t *testing.T) {
			conn, crypto, files := setupTestDB(t)
			identifier := models.DeviceIdentifier("guarded-binding")
			deviceID := createTestDevice(t, conn, string(identifier))
			createTestMinerCredentials(t, conn, crypto, deviceID)
			ctrl := gomock.NewController(t)
			device := sdkMocks.NewMockDevice(ctrl)
			driver := sdkMocks.NewMockDriver(ctrl)
			driver.EXPECT().NewDevice(gomock.Any(), string(identifier), gomock.Any(), gomock.Any()).Return(sdk.NewDeviceResult{Device: device}, nil).Times(1)
			device.EXPECT().Reboot(gomock.Any()).Return(nil).Times(1)
			service := miner.NewMinerService(conn, sqlstores.NewSQLUserStore(conn), crypto, files, &fakePluginManager{driver: driver, driverName: "antminer"})
			handle, err := service.GetMinerFromDeviceIdentifier(t.Context(), identifier)
			require.NoError(t, err)
			require.NoError(t, handle.Reboot(t.Context()))
			q := sqlc.New(conn)
			switch change {
			case "endpoint":
				require.NoError(t, q.UpdateDeviceIPAssignment(t.Context(), sqlc.UpdateDeviceIPAssignmentParams{ID: deviceID, IpAddress: "192.0.2.25", Port: "4028", UrlScheme: "https"}))
			case "credentials":
				encrypted, err := crypto.Encrypt([]byte("changed-fixture-value"))
				require.NoError(t, err)
				require.NoError(t, q.UpsertMinerCredentials(t.Context(), sqlc.UpsertMinerCredentialsParams{DeviceID: deviceID, UsernameEnc: encrypted, PasswordEnc: encrypted}))
			case "unpaired":
				_, err = q.UpsertDevicePairing(t.Context(), sqlc.UpsertDevicePairingParams{DeviceID: deviceID, PairingStatus: sqlc.PairingStatusEnumUNPAIRED})
				require.NoError(t, err)
			case "deleted":
				_, err = conn.ExecContext(t.Context(), "UPDATE device SET deleted_at=statement_timestamp() WHERE id=$1", deviceID)
				require.NoError(t, err)
			case "node route":
				nodeID := createTestFleetNode(t, conn, "guard-node", []byte("guard-node-identity"))
				_, err = conn.ExecContext(t.Context(), "INSERT INTO fleet_node_device(fleet_node_id,device_id,org_id) VALUES($1,$2,1)", nodeID, deviceID)
				require.NoError(t, err)
			}
			err = handle.Reboot(t.Context())
			require.Error(t, err, "a stale cached handle must not perform a second SDK call")
		})
	}
}

func TestCachedRemoteHandleRejectsChangedRouteBeforeNodeSend(t *testing.T) {
	for _, change := range []string{"endpoint", "credentials", "credential version", "node route", "unpaired", "revoked node", "deleted node", "deleted"} {
		t.Run(change, func(t *testing.T) {
			conn, crypto, files := setupTestDB(t)
			identifier := models.DeviceIdentifier("guarded-remote-binding")
			deviceID := createTestDevice(t, conn, string(identifier))
			nodeID := createTestFleetNode(t, conn, "guard-remote-node", []byte("guard-remote-identity"))
			_, err := conn.ExecContext(t.Context(), "INSERT INTO fleet_node_device(fleet_node_id,device_id,org_id) VALUES($1,$2,1)", nodeID, deviceID)
			require.NoError(t, err)
			q := sqlc.New(conn)
			require.NoError(t, q.UpsertMinerCredentials(t.Context(), sqlc.UpsertMinerCredentialsParams{DeviceID: deviceID, UsernameEnc: "initial-node-blob", PasswordEnc: "initial-node-blob"}))
			sender := &fakeCommandSender{}
			service := miner.NewMinerService(conn, sqlstores.NewSQLUserStore(conn), crypto, files, &fakePluginManager{driverName: "antminer"}).WithCommandSender(sender)
			handle, err := service.GetMinerFromDeviceIdentifier(t.Context(), identifier)
			require.NoError(t, err)
			require.NoError(t, handle.Reboot(t.Context()))
			sender.called = false
			switch change {
			case "endpoint":
				require.NoError(t, q.UpdateDeviceIPAssignment(t.Context(), sqlc.UpdateDeviceIPAssignmentParams{ID: deviceID, IpAddress: "192.0.2.26", Port: "4028", UrlScheme: "https"}))
			case "credentials":
				require.NoError(t, q.UpsertMinerCredentials(t.Context(), sqlc.UpsertMinerCredentialsParams{DeviceID: deviceID, UsernameEnc: "changed-node-blob", PasswordEnc: "changed-node-blob"}))
			case "credential version":
				_, err = conn.ExecContext(t.Context(), "UPDATE miner_credentials SET updated_at=updated_at+interval '1 second' WHERE device_id=$1", deviceID)
				require.NoError(t, err)
			case "unpaired":
				_, err = q.UpsertDevicePairing(t.Context(), sqlc.UpsertDevicePairingParams{DeviceID: deviceID, PairingStatus: sqlc.PairingStatusEnumUNPAIRED})
				require.NoError(t, err)
			case "revoked node":
				_, err = conn.ExecContext(t.Context(), "UPDATE fleet_node SET enrollment_status='REVOKED' WHERE id=$1", nodeID)
				require.NoError(t, err)
			case "deleted node":
				_, err = conn.ExecContext(t.Context(), "UPDATE fleet_node SET deleted_at=statement_timestamp() WHERE id=$1", nodeID)
				require.NoError(t, err)
			case "node route":
				_, err = conn.ExecContext(t.Context(), "DELETE FROM fleet_node_device WHERE device_id=$1", deviceID)
				require.NoError(t, err)
			case "deleted":
				_, err = conn.ExecContext(t.Context(), "UPDATE device SET deleted_at=statement_timestamp() WHERE id=$1", deviceID)
				require.NoError(t, err)
			}
			require.Error(t, handle.Reboot(t.Context()))
			require.False(t, sender.called, "a stale remote descriptor must not reach the node")
		})
	}
}

func TestOperationBindingReadFailureRemainsRetryable(t *testing.T) {
	conn, crypto, files := setupTestDB(t)
	identifier := models.DeviceIdentifier("guarded-db-failure")
	createTestDeviceWithCredentials(t, conn, crypto, string(identifier))
	ctrl := gomock.NewController(t)
	device := sdkMocks.NewMockDevice(ctrl)
	driver := sdkMocks.NewMockDriver(ctrl)
	driver.EXPECT().NewDevice(gomock.Any(), string(identifier), gomock.Any(), gomock.Any()).Return(sdk.NewDeviceResult{Device: device}, nil)
	service := miner.NewMinerService(conn, sqlstores.NewSQLUserStore(conn), crypto, files, &fakePluginManager{driver: driver, driverName: "antminer"})
	handle, err := service.GetMinerFromDeviceIdentifier(t.Context(), identifier)
	require.NoError(t, err)
	require.NoError(t, conn.Close())
	err = handle.Reboot(t.Context())
	var classified fleeterror.FleetError
	require.ErrorAs(t, err, &classified)
	require.Equal(t, connect.CodeInternal, classified.GRPCCode, "database unavailability must not become a permanent binding conflict")
}

func TestDirectSDKAttemptBlocksDrainAndOldHandleRevival(t *testing.T) {
	conn, crypto, files := setupTestDB(t)
	identifier := models.DeviceIdentifier("direct-outstanding-write")
	deviceID := createTestDevice(t, conn, string(identifier))
	createTestMinerCredentials(t, conn, crypto, deviceID)
	ctrl := gomock.NewController(t)
	device := sdkMocks.NewMockDevice(ctrl)
	driver := sdkMocks.NewMockDriver(ctrl)
	driver.EXPECT().NewDevice(gomock.Any(), string(identifier), gomock.Any(), gomock.Any()).Return(sdk.NewDeviceResult{Device: device}, nil)
	started, complete := make(chan struct{}), make(chan struct{})
	finish := sync.OnceFunc(func() { close(complete) })
	t.Cleanup(finish)
	device.EXPECT().Reboot(gomock.Any()).DoAndReturn(func(context.Context) error { close(started); <-complete; return nil }).Times(1)
	service := miner.NewMinerService(conn, sqlstores.NewSQLUserStore(conn), crypto, files, &fakePluginManager{driver: driver, driverName: "antminer"})
	handle, err := service.GetMinerFromDeviceIdentifier(t.Context(), identifier)
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { done <- handle.Reboot(t.Context()) }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("SDK write did not start")
	}
	fences := devicewrite.NewService(conn)
	epoch, err := fences.Reserve(t.Context(), 1, deviceID)
	require.NoError(t, err)
	drained, err := fences.Drained(t.Context(), 1, deviceID, epoch)
	require.NoError(t, err)
	require.False(t, drained, "an outstanding direct SDK call must block handoff")
	finish()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("SDK write did not finish")
	}
	drained, err = fences.Drained(t.Context(), 1, deviceID, epoch)
	require.NoError(t, err)
	require.False(t, drained, "SDK acceptance must not prove that reboot ended")
	require.Error(t, fences.ReleaseHost(t.Context(), 1, deviceID, epoch))
	resolveTestPhysicalAttempt(t, fences, deviceID)
	require.Error(t, handle.Reboot(t.Context()), "an old handle cannot revive after audited recovery and host restoration")
}

func TestDirectSDKErrorRetainsUnknownOutcome(t *testing.T) {
	conn, crypto, files := setupTestDB(t)
	identifier := models.DeviceIdentifier("direct-uncertain-write")
	deviceID := createTestDevice(t, conn, string(identifier))
	createTestMinerCredentials(t, conn, crypto, deviceID)
	ctrl := gomock.NewController(t)
	device := sdkMocks.NewMockDevice(ctrl)
	driver := sdkMocks.NewMockDriver(ctrl)
	driver.EXPECT().NewDevice(gomock.Any(), string(identifier), gomock.Any(), gomock.Any()).Return(sdk.NewDeviceResult{Device: device}, nil)
	device.EXPECT().Reboot(gomock.Any()).Return(errors.New("SDK write outcome unavailable"))
	service := miner.NewMinerService(conn, sqlstores.NewSQLUserStore(conn), crypto, files, &fakePluginManager{driver: driver, driverName: "antminer"})
	handle, err := service.GetMinerFromDeviceIdentifier(t.Context(), identifier)
	require.NoError(t, err)
	require.Error(t, handle.Reboot(t.Context()))
	fences := devicewrite.NewService(conn)
	_, err = fences.Reserve(t.Context(), 1, deviceID)
	require.True(t, fleeterror.IsFailedPreconditionError(err))
	var status string
	require.NoError(t, conn.QueryRowContext(t.Context(), "SELECT status FROM device_write_attempt WHERE device_id=$1", deviceID).Scan(&status))
	require.Equal(t, "unknown", status)

}

func TestReservedMinerCannotBeDeleted(t *testing.T) {
	conn, crypto, _ := setupTestDB(t)
	identifier := "reserved-deletion"
	deviceID := createTestDevice(t, conn, identifier)
	createTestMinerCredentials(t, conn, crypto, deviceID)
	_, err := devicewrite.NewService(conn).Reserve(t.Context(), 1, deviceID)
	require.NoError(t, err)
	_, err = sqlstores.NewSQLDeviceStore(conn).SoftDeleteDevices(t.Context(), []string{identifier}, 1)
	require.Error(t, err, "deletion must not bypass a configuration reservation")
	_, err = sqlc.New(conn).GetDeviceWithCredentialsAndIPByDeviceIdentifier(t.Context(), identifier)
	require.NoError(t, err, "a refused deletion must preserve the live binding")
}

func TestDirectAttemptRejectsEachMismatchedIdentity(t *testing.T) {
	for _, dimension := range []string{"database", "organization", "device", "epoch"} {
		t.Run(dimension, func(t *testing.T) {
			conn, _, _ := setupTestDB(t)
			first := createTestDevice(t, conn, "attempt-first")
			service := devicewrite.NewService(conn)
			attempt, err := service.BeginHost(t.Context(), 1, first, 0)
			require.NoError(t, err)
			targetService, orgID, deviceID, epoch := service, int64(1), first, int64(0)
			switch dimension {
			case "database":
				other, _, _ := setupTestDB(t)
				matchingID := createTestDevice(t, other, "attempt-first")
				require.Equal(t, first, matchingID, "database mismatch must hold the numeric identities equal")
				targetService = devicewrite.NewService(other)
			case "organization":
				orgID++
			case "device":
				deviceID = createTestDevice(t, conn, "attempt-second")
			case "epoch":
				epoch++
			}
			called := false
			err = targetService.RunHost(attempt.Context(t.Context()), orgID, deviceID, epoch, func(context.Context) error { called = true; return nil })
			var classified fleeterror.FleetError
			require.ErrorAs(t, err, &classified)
			require.Equal(t, connect.CodeFailedPrecondition, classified.GRPCCode)
			require.False(t, called)
			require.NoError(t, attempt.Finish(t.Context(), nil))
		})
	}
}

func TestDeletionPermitAllowsOnlyOneCapturedUnpair(t *testing.T) {
	conn, crypto, files := setupTestDB(t)
	identifier := models.DeviceIdentifier("deletion-permit")
	deviceID := createTestDevice(t, conn, string(identifier))
	createTestMinerCredentials(t, conn, crypto, deviceID)
	_, err := conn.ExecContext(t.Context(), "UPDATE discovered_device SET driver_name='proto' WHERE id=(SELECT discovered_device_id FROM device WHERE id=$1)", deviceID)
	require.NoError(t, err)
	ctrl := gomock.NewController(t)
	device := sdkMocks.NewMockDevice(ctrl)
	driver := sdkMocks.NewMockDriver(ctrl)
	driver.EXPECT().NewDevice(gomock.Any(), string(identifier), gomock.Any(), gomock.Any()).Return(sdk.NewDeviceResult{Device: device}, nil)
	device.EXPECT().Unpair(gomock.Any()).Return(nil).Times(1)
	service := miner.NewMinerService(conn, sqlstores.NewSQLUserStore(conn), crypto, files, &fakePluginManager{driver: driver, driverName: "proto"})
	handle, err := service.GetMinerFromDeviceIdentifier(t.Context(), identifier)
	require.NoError(t, err)
	_, err = sqlstores.NewSQLDeviceStore(conn).SoftDeleteDevices(t.Context(), []string{string(identifier)}, 1)
	require.NoError(t, err)
	require.Error(t, handle.Reboot(t.Context()))
	require.Error(t, handle.Unpair(t.Context()), "ordinary calls cannot bypass the missing binding")
	called := false
	err = devicewrite.NewService(conn).RunDeletionUnpair(t.Context(), 1, deviceID, "mismatched-binding-digest", func(context.Context) error { called = true; return nil })
	require.Error(t, err)
	require.False(t, called)
	var claimed bool
	var attempts int
	require.NoError(t, conn.QueryRowContext(t.Context(), "SELECT attempt_id IS NOT NULL FROM device_deletion_unpair_permit WHERE device_id=$1", deviceID).Scan(&claimed))
	require.NoError(t, conn.QueryRowContext(t.Context(), "SELECT count(*) FROM device_write_attempt WHERE device_id=$1", deviceID).Scan(&attempts))
	require.False(t, claimed, "binding validation must precede permit consumption")
	require.Zero(t, attempts)
	require.NoError(t, service.UnpairDeletedMiner(t.Context(), 1, handle))
	require.Error(t, service.UnpairDeletedMiner(t.Context(), 1, handle), "a deletion permit is single-use")
}

func TestUnclaimedDeletionPermitIsSupersededByRediscovery(t *testing.T) {
	conn, crypto, files := setupTestDB(t)
	identifier := models.DeviceIdentifier("retired-ready-handle")
	deviceID := createTestDevice(t, conn, string(identifier))
	createTestMinerCredentials(t, conn, crypto, deviceID)
	_, err := conn.ExecContext(t.Context(), "UPDATE discovered_device SET driver_name='proto' WHERE id=(SELECT discovered_device_id FROM device WHERE id=$1)", deviceID)
	require.NoError(t, err)
	ctrl := gomock.NewController(t)
	device := sdkMocks.NewMockDevice(ctrl)
	driver := sdkMocks.NewMockDriver(ctrl)
	driver.EXPECT().NewDevice(gomock.Any(), string(identifier), gomock.Any(), gomock.Any()).Return(sdk.NewDeviceResult{Device: device}, nil)
	service := miner.NewMinerService(conn, sqlstores.NewSQLUserStore(conn), crypto, files, &fakePluginManager{driver: driver, driverName: "proto"})
	handle, err := service.GetMinerFromDeviceIdentifier(t.Context(), identifier)
	require.NoError(t, err)
	_, err = sqlstores.NewSQLDeviceStore(conn).SoftDeleteDevices(t.Context(), []string{string(identifier)}, 1)
	require.NoError(t, err)
	successor := createTestDevice(t, conn, "successor-ready")
	_, err = conn.ExecContext(t.Context(), "UPDATE device fresh SET serial_number=old.serial_number,mac_address=old.mac_address FROM device old WHERE fresh.id=$1 AND old.id=$2", successor, deviceID)
	require.NoError(t, err)
	require.Error(t, service.UnpairDeletedMiner(t.Context(), 1, handle), "ready unpair must skip a rediscovered physical device")
}

func TestClaimedDeletionAttemptFencesRediscoveredDevice(t *testing.T) {
	for _, uncertain := range []bool{false, true} {
		t.Run(map[bool]string{false: "confirmed", true: "uncertain"}[uncertain], func(t *testing.T) {
			conn, crypto, files := setupTestDB(t)
			identifier := models.DeviceIdentifier("retired-claimed-handle")
			deviceID := createTestDevice(t, conn, string(identifier))
			createTestMinerCredentials(t, conn, crypto, deviceID)
			_, err := conn.ExecContext(t.Context(), "UPDATE discovered_device SET driver_name='proto' WHERE id=(SELECT discovered_device_id FROM device WHERE id=$1)", deviceID)
			require.NoError(t, err)
			ctrl := gomock.NewController(t)
			device := sdkMocks.NewMockDevice(ctrl)
			driver := sdkMocks.NewMockDriver(ctrl)
			driver.EXPECT().NewDevice(gomock.Any(), string(identifier), gomock.Any(), gomock.Any()).Return(sdk.NewDeviceResult{Device: device}, nil)
			started, complete := make(chan struct{}), make(chan struct{})
			finish := sync.OnceFunc(func() { close(complete) })
			t.Cleanup(finish)
			outcome := error(nil)
			if uncertain {
				outcome = errors.New("unpair outcome unknown")
			}
			device.EXPECT().Unpair(gomock.Any()).DoAndReturn(func(context.Context) error { close(started); <-complete; return outcome })
			service := miner.NewMinerService(conn, sqlstores.NewSQLUserStore(conn), crypto, files, &fakePluginManager{driver: driver, driverName: "proto"})
			handle, err := service.GetMinerFromDeviceIdentifier(t.Context(), identifier)
			require.NoError(t, err)
			_, err = sqlstores.NewSQLDeviceStore(conn).SoftDeleteDevices(t.Context(), []string{string(identifier)}, 1)
			require.NoError(t, err)
			done := make(chan error, 1)
			go func() { done <- service.UnpairDeletedMiner(t.Context(), 1, handle) }()
			select {
			case <-started:
			case <-time.After(5 * time.Second):
				t.Fatal("captured unpair did not start")
			}
			successor := createTestDevice(t, conn, "successor-claimed")
			_, err = conn.ExecContext(t.Context(), "UPDATE device fresh SET serial_number=old.serial_number,mac_address=old.mac_address FROM device old WHERE fresh.id=$1 AND old.id=$2", successor, deviceID)
			require.NoError(t, err)
			fences := devicewrite.NewService(conn)
			_, err = fences.BeginHost(t.Context(), 1, successor, 0)
			require.Error(t, err, "old physical call must block admission on a fresh database row")
			_, err = fences.Reserve(t.Context(), 1, successor)
			require.Error(t, err, "old physical call must block incoming reservation")
			finish()
			select {
			case err := <-done:
				if uncertain {
					require.Error(t, err)
				} else {
					require.NoError(t, err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("captured unpair did not finish")
			}
			_, err = fences.BeginHost(t.Context(), 1, successor, 0)
			require.Error(t, err, "rediscovery cannot clear either uncertain or accepted predecessor work")
			_, err = fences.Reserve(t.Context(), 1, successor)
			require.Error(t, err, "SDK acknowledgement cannot permit incoming reservation")
			resolveTestPhysicalAttempt(t, fences, deviceID)
			next, err := fences.BeginHost(t.Context(), 1, successor, 0)
			require.NoError(t, err, "audited completion restores physical identity admission")
			require.NoError(t, next.Finish(t.Context(), nil))
		})
	}
}

func TestDeletionPermitSelectsCapturedIncarnation(t *testing.T) {
	conn, crypto, files := setupTestDB(t)
	identifier := models.DeviceIdentifier("same-retired-identifier")
	makeProto := func() int64 {
		id := createTestDevice(t, conn, string(identifier))
		createTestMinerCredentials(t, conn, crypto, id)
		_, err := conn.ExecContext(t.Context(), "UPDATE discovered_device SET driver_name='proto' WHERE id=(SELECT discovered_device_id FROM device WHERE id=$1)", id)
		require.NoError(t, err)
		return id
	}
	first := makeProto()
	_, err := sqlstores.NewSQLDeviceStore(conn).SoftDeleteDevices(t.Context(), []string{string(identifier)}, 1)
	require.NoError(t, err)
	second := makeProto()
	ctrl := gomock.NewController(t)
	device := sdkMocks.NewMockDevice(ctrl)
	driver := sdkMocks.NewMockDriver(ctrl)
	driver.EXPECT().NewDevice(gomock.Any(), string(identifier), gomock.Any(), gomock.Any()).Return(sdk.NewDeviceResult{Device: device}, nil)
	device.EXPECT().Unpair(gomock.Any()).Return(nil).Times(1)
	service := miner.NewMinerService(conn, sqlstores.NewSQLUserStore(conn), crypto, files, &fakePluginManager{driver: driver, driverName: "proto"})
	handle, err := service.GetMinerFromDeviceIdentifier(t.Context(), identifier)
	require.NoError(t, err)
	_, err = sqlstores.NewSQLDeviceStore(conn).SoftDeleteDevices(t.Context(), []string{string(identifier)}, 1)
	require.NoError(t, err)
	require.NoError(t, service.UnpairDeletedMiner(t.Context(), 1, handle))
	var firstClaimed, secondClaimed bool
	require.NoError(t, conn.QueryRowContext(t.Context(), "SELECT attempt_id IS NOT NULL FROM device_deletion_unpair_permit WHERE device_id=$1", first).Scan(&firstClaimed))
	require.NoError(t, conn.QueryRowContext(t.Context(), "SELECT attempt_id IS NOT NULL FROM device_deletion_unpair_permit WHERE device_id=$1", second).Scan(&secondClaimed))
	require.False(t, firstClaimed, "another retired incarnation must not lose its permit")
	require.True(t, secondClaimed)
}

func TestMixedDeletionRejectsAtomically(t *testing.T) {
	conn, crypto, _ := setupTestDB(t)
	host := createTestDevice(t, conn, "mixed-host-proto")
	reserved := createTestDevice(t, conn, "mixed-reserved")
	for _, id := range []int64{host, reserved} {
		createTestMinerCredentials(t, conn, crypto, id)
	}
	_, err := conn.ExecContext(t.Context(), "UPDATE discovered_device SET driver_name='proto' WHERE id=(SELECT discovered_device_id FROM device WHERE id=$1)", host)
	require.NoError(t, err)
	node := createTestFleetNode(t, conn, "mixed-node", []byte("mixed-node-identity"))
	_, err = conn.ExecContext(t.Context(), "INSERT INTO fleet_node_device(fleet_node_id,device_id,org_id) VALUES($1,$2,1)", node, reserved)
	require.NoError(t, err)
	q := sqlc.New(conn)
	require.NoError(t, q.EnsureDeviceWriteFences(t.Context(), []int64{host, reserved}))
	epoch, err := devicewrite.NewService(conn).Reserve(t.Context(), 1, reserved)
	require.NoError(t, err)
	require.Equal(t, int64(1), epoch)
	_, err = conn.ExecContext(t.Context(), "INSERT INTO queue_message(command_batch_log_uuid,device_id,command_type,status,retry_count) VALUES('mixed-delete',$1,'Reboot','PENDING',0)", host)
	require.NoError(t, err)
	// Snapshot real rows after reservation; a refused batch must change none of them.
	tables := []string{"device", "discovered_device", "miner_credentials", "device_pairing", "fleet_node_device", "device_write_fence", "queue_message", "device_deletion_unpair_permit"}
	snapshot := func() map[string]string {
		result := map[string]string{}
		for _, table := range tables {
			var encoded string
			require.NoError(t, conn.QueryRowContext(t.Context(), "SELECT COALESCE(jsonb_agg(to_jsonb(row) ORDER BY to_jsonb(row)::text),'[]'::jsonb)::text FROM "+table+" row").Scan(&encoded))
			result[table] = encoded
		}
		return result
	}
	before := snapshot()
	count, err := sqlstores.NewSQLDeviceStore(conn).SoftDeleteDevices(t.Context(), []string{"mixed-host-proto", "mixed-reserved"}, 1)
	var classified fleeterror.FleetError
	require.ErrorAs(t, err, &classified)
	require.Equal(t, connect.CodeFailedPrecondition, classified.GRPCCode)
	require.Zero(t, count)
	require.Equal(t, before, snapshot())
}

func TestDeletionPermitRejectsAmbientTransactionWithoutClaim(t *testing.T) {
	conn, crypto, files := setupTestDB(t)
	identifier := models.DeviceIdentifier("ambient-deletion")
	id := createTestDevice(t, conn, string(identifier))
	createTestMinerCredentials(t, conn, crypto, id)
	_, err := conn.ExecContext(t.Context(), "UPDATE discovered_device SET driver_name='proto' WHERE id=(SELECT discovered_device_id FROM device WHERE id=$1)", id)
	require.NoError(t, err)
	ctrl := gomock.NewController(t)
	device := sdkMocks.NewMockDevice(ctrl)
	driver := sdkMocks.NewMockDriver(ctrl)
	driver.EXPECT().NewDevice(gomock.Any(), string(identifier), gomock.Any(), gomock.Any()).Return(sdk.NewDeviceResult{Device: device}, nil)
	device.EXPECT().Unpair(gomock.Any()).Return(nil).Times(1)
	service := miner.NewMinerService(conn, sqlstores.NewSQLUserStore(conn), crypto, files, &fakePluginManager{driver: driver, driverName: "proto"})
	handle, err := service.GetMinerFromDeviceIdentifier(t.Context(), identifier)
	require.NoError(t, err)
	_, err = sqlstores.NewSQLDeviceStore(conn).SoftDeleteDevices(t.Context(), []string{string(identifier)}, 1)
	require.NoError(t, err)
	err = db.WithTransactionNoResult(t.Context(), conn, func(q sqlc.Querier) error {
		return service.UnpairDeletedMiner(db.WithTxQueries(t.Context(), q), 1, handle)
	})
	var classified fleeterror.FleetError
	require.ErrorAs(t, err, &classified)
	require.Equal(t, connect.CodeFailedPrecondition, classified.GRPCCode)
	var attempts int
	var claimed bool
	require.NoError(t, conn.QueryRowContext(t.Context(), "SELECT count(*) FROM device_write_attempt WHERE device_id=$1", id).Scan(&attempts))
	require.Zero(t, attempts)
	require.NoError(t, conn.QueryRowContext(t.Context(), "SELECT attempt_id IS NOT NULL FROM device_deletion_unpair_permit WHERE device_id=$1", id).Scan(&claimed))
	require.False(t, claimed)
	require.NoError(t, service.UnpairDeletedMiner(t.Context(), 1, handle), "refused transactional I/O must leave the permit usable")
}

// Fixture evidence represents the offline operator procedure after all mocked
// operations returned. No production code infers quiescence from that return.
func resolveTestPhysicalAttempt(t *testing.T, fences *devicewrite.Service, deviceID int64) {
	t.Helper()
	inspections, err := fences.InspectRecovery(t.Context(), 1, deviceID)
	require.NoError(t, err)
	require.Len(t, inspections, 1)
	evidence := inspections[0].EvidenceTemplate
	evidence.Operator = "fixture host operator"
	evidence.MaintenanceTermination = "fixture supervisor: all dispatchers stopped"
	evidence.OwnerTermination = "fixture supervisor: exact recorded owner exited"
	evidence.TransportQuiescence = "fixture transport quiescence receipt"
	evidence.DeviceQuiescence = "fixture authoritative device operation completion"
	evidence.ConfigurationVerification = "fixture qualified configuration receipt"
	evidence.RestoreHost = inspections[0].FenceState == "reserved"
	_, err = fences.ResolveOffline(t.Context(), evidence, true)
	require.NoError(t, err)
}

func TestOrderedDirectPoolWritePermitsHandoffAndFencesOldHandle(t *testing.T) {
	for _, lostResponse := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "lost response"}[lostResponse], func(t *testing.T) {
			conn, crypto, files := setupTestDB(t)
			identifier := models.DeviceIdentifier("ordered-pool-write")
			deviceID := createTestDevice(t, conn, string(identifier))
			createTestMinerCredentials(t, conn, crypto, deviceID)
			ctrl := gomock.NewController(t)
			device := sdkMocks.NewMockDevice(ctrl)
			driver := sdkMocks.NewMockDriver(ctrl)
			driver.EXPECT().NewDevice(gomock.Any(), string(identifier), gomock.Any(), gomock.Any()).Return(sdk.NewDeviceResult{Device: device}, nil).Times(1)
			var outcome error
			if lostResponse {
				outcome = context.DeadlineExceeded
			}
			gomock.InOrder(
				device.EXPECT().UpdateMiningPools(gomock.Any(), gomock.Any()).Return(outcome),
				device.EXPECT().UpdateMiningPools(gomock.Any(), gomock.Any()).Return(nil),
			)
			service := miner.NewMinerService(conn, sqlstores.NewSQLUserStore(conn), crypto, files, &fakePluginManager{driver: driver, driverName: "antminer"})
			oldHandle, err := service.GetMinerFromDeviceIdentifier(t.Context(), identifier)
			require.NoError(t, err)
			pools := dto.UpdateMiningPoolsPayload{DefaultPool: dto.MiningPool{URL: "stratum+tcp://pool.example.com:3333", Username: "old-owner", Password: "fixture"}}
			err = oldHandle.UpdateMiningPools(t.Context(), pools)
			if lostResponse {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			var state string
			require.NoError(t, conn.QueryRowContext(t.Context(), "SELECT status FROM device_write_attempt WHERE device_id=$1", deviceID).Scan(&state))
			require.Equal(t, "complete", state, "an ordered configuration write must not require offline recovery")
			fences := devicewrite.NewService(conn)
			epoch, err := fences.Reserve(t.Context(), 1, deviceID)
			require.NoError(t, err)
			drained, err := fences.Drained(t.Context(), 1, deviceID, epoch)
			require.NoError(t, err)
			require.True(t, drained)
			// Return to host custody at a new epoch; the old handle must remain fenced.
			require.NoError(t, fences.ReleaseHost(t.Context(), 1, deviceID, epoch))
			require.Error(t, oldHandle.UpdateMiningPools(t.Context(), pools), "the prior owner's retry must fail before a second SDK call")
			fresh, err := service.GetMinerFromDeviceIdentifier(t.Context(), identifier)
			require.NoError(t, err)
			pools.DefaultPool.Username = "new-owner"
			require.NoError(t, fresh.UpdateMiningPools(t.Context(), pools))
		})
	}
}

func TestOrderedRemoteMiningControlPermitsHandoff(t *testing.T) {
	conn, crypto, files := setupTestDB(t)
	identifier := models.DeviceIdentifier("ordered-remote-write")
	deviceID := createTestDevice(t, conn, string(identifier))
	nodeID := createTestFleetNode(t, conn, "ordered-node", []byte("ordered-node"))
	_, err := conn.ExecContext(t.Context(), "INSERT INTO fleet_node_device(fleet_node_id,device_id,org_id) VALUES($1,$2,1)", nodeID, deviceID)
	require.NoError(t, err)
	sender := &fakeCommandSender{}
	service := miner.NewMinerService(conn, sqlstores.NewSQLUserStore(conn), crypto, files, &fakePluginManager{driverName: "antminer"}).WithCommandSender(sender)
	handle, err := service.GetMinerFromDeviceIdentifier(t.Context(), identifier)
	require.NoError(t, err)
	require.NoError(t, handle.StopMining(t.Context()))
	require.True(t, sender.called)
	fences := devicewrite.NewService(conn)
	epoch, err := fences.Reserve(t.Context(), 1, deviceID)
	require.NoError(t, err)
	drained, err := fences.Drained(t.Context(), 1, deviceID, epoch)
	require.NoError(t, err)
	require.True(t, drained)
	sender.called = false
	require.Error(t, handle.StopMining(t.Context()))
	require.False(t, sender.called, "old-epoch control must not reach the node")
}

func TestOrderedPluginRPCCompletionDoesNotHideRunningBackend(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "canceled RPC"}[canceled], func(t *testing.T) {
			conn, crypto, files := setupTestDB(t)
			identifier := models.DeviceIdentifier("ordered-plugin-rpc")
			deviceID := createTestDevice(t, conn, string(identifier))
			createTestMinerCredentials(t, conn, crypto, deviceID)
			ctrl := gomock.NewController(t)
			backend := sdkMocks.NewMockDevice(ctrl)
			driver := sdkMocks.NewMockDriver(ctrl)
			driver.EXPECT().NewDevice(gomock.Any(), string(identifier), gomock.Any(), gomock.Any()).Return(sdk.NewDeviceResult{Device: backend}, nil)
			backend.EXPECT().ID().Return(string(identifier))
			entered, release, returned := make(chan struct{}), make(chan struct{}), make(chan struct{})
			finish := sync.OnceFunc(func() { close(release) })
			t.Cleanup(finish)
			backend.EXPECT().StopMining(gomock.Any()).DoAndReturn(func(context.Context) error {
				close(entered)
				<-release
				close(returned)
				return nil
			})
			listener := bufconn.Listen(1 << 20)
			t.Cleanup(func() { _ = listener.Close() })
			wire := grpc.NewServer()
			plugin := &sdk.DriverPlugin{Impl: driver}
			require.NoError(t, plugin.GRPCServer(nil, wire))
			go func() { _ = wire.Serve(listener) }()
			t.Cleanup(wire.Stop)
			clientConn, err := grpc.NewClient("passthrough:///ordered-write", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }))
			require.NoError(t, err)
			t.Cleanup(func() { _ = clientConn.Close() })
			client, err := plugin.GRPCClient(t.Context(), nil, clientConn)
			require.NoError(t, err)
			rpcDriver, ok := client.(sdk.Driver)
			require.True(t, ok)
			service := miner.NewMinerService(conn, sqlstores.NewSQLUserStore(conn), crypto, files, &fakePluginManager{driver: rpcDriver, driverName: "antminer"})
			handle, err := service.GetMinerFromDeviceIdentifier(t.Context(), identifier)
			require.NoError(t, err)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- handle.StopMining(ctx) }()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("plugin backend did not start")
			}
			if canceled {
				cancel()
			} else {
				finish()
			}
			select {
			case err := <-done:
				if canceled {
					require.Error(t, err)
				} else {
					require.NoError(t, err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("plugin RPC did not return")
			}
			var state string
			require.NoError(t, conn.QueryRowContext(t.Context(), "SELECT status FROM device_write_attempt WHERE device_id=$1", deviceID).Scan(&state))
			fences := devicewrite.NewService(conn)
			epoch, err := fences.Reserve(t.Context(), 1, deviceID)
			if canceled {
				select {
				case <-returned:
					t.Fatal("backend returned before the cancellation assertion")
				default:
				}
				require.Equal(t, "unknown", state)
				require.True(t, fleeterror.IsFailedPreconditionError(err))
				finish()
				select {
				case <-returned:
				case <-time.After(5 * time.Second):
					t.Fatal("backend did not exit")
				}
				_, err = fences.Reserve(t.Context(), 1, deviceID)
				require.True(t, fleeterror.IsFailedPreconditionError(err), "client cancellation provides no durable terminal evidence")
			} else {
				require.Equal(t, "complete", state)
				require.NoError(t, err)
				drained, err := fences.Drained(t.Context(), 1, deviceID, epoch)
				require.NoError(t, err)
				require.True(t, drained)
			}
		})
	}
}
