package miner_test

import (
	"testing"

	"github.com/block/proto-fleet/server/internal/domain/miner"
	"github.com/block/proto-fleet/server/internal/domain/miner/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/stretchr/testify/require"
)

func TestCachedRemoteHandleAllowsFirmwareTelemetryChange(t *testing.T) {
	conn, crypto, files := setupTestDB(t)
	identifier := models.DeviceIdentifier("remote-firmware-telemetry")
	deviceID := createTestDevice(t, conn, string(identifier))
	nodeID := createTestFleetNode(t, conn, "firmware-node", []byte("firmware-node-identity"))
	_, err := conn.ExecContext(t.Context(), "INSERT INTO fleet_node_device(fleet_node_id,device_id,org_id) VALUES($1,$2,1)", nodeID, deviceID)
	require.NoError(t, err)
	sender := &fakeCommandSender{}
	service := miner.NewMinerService(conn, sqlstores.NewSQLUserStore(conn), crypto, files, &fakePluginManager{driverName: "antminer"}).WithCommandSender(sender)
	handle, err := service.GetMinerFromDeviceIdentifier(t.Context(), identifier)
	require.NoError(t, err)
	require.NoError(t, handle.Reboot(t.Context()))
	sender.called = false
	_, err = conn.ExecContext(t.Context(), `UPDATE discovered_device SET firmware_version='updated-by-telemetry' WHERE id=(SELECT discovered_device_id FROM device WHERE id=$1)`, deviceID)
	require.NoError(t, err)
	require.NoError(t, handle.Reboot(t.Context()))
	require.True(t, sender.called, "firmware telemetry must not invalidate the cached remote route")
}
