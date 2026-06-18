package miner_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/miner"
	"github.com/block/proto-fleet/server/internal/domain/miner/models"
	"github.com/block/proto-fleet/server/internal/domain/miner/remotenode"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
)

type fakeCommandSender struct {
	called bool
}

func (f *fakeCommandSender) SendCommand(_ context.Context, _ int64, _ *gatewaypb.ControlCommand) (*gatewaypb.ControlAck, error) {
	f.called = true
	return &gatewaypb.ControlAck{Succeeded: true, Code: gatewaypb.AckCode_ACK_CODE_OK}, nil
}

// TestMinerService_ResolvesFleetNodePairedDeviceToRemoteMiner verifies that a device
// bound to a CONFIRMED fleet node resolves to a remote-node miner whose commands
// route over the ControlStream, not a directly-dialed PluginMiner.
func TestMinerService_ResolvesFleetNodePairedDeviceToRemoteMiner(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Arrange
	db, encryptService, filesService, tokenService := setupTestDB(t)
	userStore := sqlstores.NewSQLUserStore(db)
	deviceIdentifier := "fleetnode-routed-device"
	deviceID := createTestDevice(t, db, deviceIdentifier)

	var fleetNodeID int64
	require.NoError(t, db.QueryRow(
		`INSERT INTO fleet_node (org_id, name, identity_pubkey, miner_signing_pubkey, enrollment_status)
		 VALUES (1, $1, $2, $3, 'CONFIRMED') RETURNING id`,
		"test-fleet-node", []byte("identity-pubkey"), []byte("signing-pubkey"),
	).Scan(&fleetNodeID))
	_, err := db.Exec(
		`INSERT INTO fleet_node_device (fleet_node_id, device_id, org_id) VALUES ($1, $2, 1)`,
		fleetNodeID, deviceID)
	require.NoError(t, err)

	sender := &fakeCommandSender{}
	svc := miner.NewMinerService(db, userStore, encryptService, filesService, tokenService,
		&fakePluginManager{driverName: "antminer"}).
		WithCommandSender(sender)

	// Act
	m, err := svc.GetMinerFromDeviceIdentifier(t.Context(), models.DeviceIdentifier(deviceIdentifier))

	// Assert: routed to the remote-node adapter, and commands reach the sender.
	require.NoError(t, err)
	_, ok := m.(*remotenode.Miner)
	require.True(t, ok, "fleet-node-paired device should resolve to a remote-node miner")

	require.NoError(t, m.Reboot(t.Context()))
	assert.True(t, sender.called, "command should dispatch over the ControlStream sender")
}

// TestMinerService_DoesNotRouteUnpairedFleetNodeBoundDevice verifies that a device
// bound to a CONFIRMED fleet node but whose device_pairing is not PAIRED does NOT
// resolve to a remote miner (it must not receive commands until fully paired),
// matching the direct-dial paired-status gate.
func TestMinerService_DoesNotRouteUnpairedFleetNodeBoundDevice(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Arrange: a fleet-node-bound device whose pairing status is not PAIRED.
	db, encryptService, filesService, tokenService := setupTestDB(t)
	userStore := sqlstores.NewSQLUserStore(db)
	deviceIdentifier := "fleetnode-bound-unpaired"
	deviceID := createTestDevice(t, db, deviceIdentifier)

	var fleetNodeID int64
	require.NoError(t, db.QueryRow(
		`INSERT INTO fleet_node (org_id, name, identity_pubkey, miner_signing_pubkey, enrollment_status)
		 VALUES (1, $1, $2, $3, 'CONFIRMED') RETURNING id`,
		"unpaired-fleet-node", []byte("identity-pubkey"), []byte("signing-pubkey"),
	).Scan(&fleetNodeID))
	_, err := db.Exec(
		`INSERT INTO fleet_node_device (fleet_node_id, device_id, org_id) VALUES ($1, $2, 1)`,
		fleetNodeID, deviceID)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE device_pairing SET pairing_status = 'UNPAIRED' WHERE device_id = $1`, deviceID)
	require.NoError(t, err)

	svc := miner.NewMinerService(db, userStore, encryptService, filesService, tokenService,
		&fakePluginManager{driverName: "antminer"}).
		WithCommandSender(&fakeCommandSender{})

	// Act
	_, err = svc.GetMinerFromDeviceIdentifier(t.Context(), models.DeviceIdentifier(deviceIdentifier))

	// Assert: not PAIRED -> neither the fleet-node nor the direct path resolves it.
	require.Error(t, err)
}

func TestMinerService_RoutesDefaultPasswordFleetNodeDeviceForCommands(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	db, encryptService, filesService, tokenService := setupTestDB(t)
	userStore := sqlstores.NewSQLUserStore(db)
	deviceIdentifier := "fleetnode-default-password-device"
	deviceID := createTestDevice(t, db, deviceIdentifier)

	var fleetNodeID int64
	require.NoError(t, db.QueryRow(
		`INSERT INTO fleet_node (org_id, name, identity_pubkey, miner_signing_pubkey, enrollment_status)
		 VALUES (1, $1, $2, $3, 'CONFIRMED') RETURNING id`,
		"default-password-fleet-node", []byte("identity-pubkey"), []byte("signing-pubkey"),
	).Scan(&fleetNodeID))
	_, err := db.Exec(
		`INSERT INTO fleet_node_device (fleet_node_id, device_id, org_id) VALUES ($1, $2, 1)`,
		fleetNodeID, deviceID)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE device_pairing SET pairing_status = 'DEFAULT_PASSWORD' WHERE device_id = $1`, deviceID)
	require.NoError(t, err)

	sender := &fakeCommandSender{}
	svc := miner.NewMinerService(db, userStore, encryptService, filesService, tokenService,
		&fakePluginManager{driverName: "antminer"}).
		WithCommandSender(sender)

	m, err := svc.GetMinerFromDeviceIdentifier(t.Context(), models.DeviceIdentifier(deviceIdentifier))
	require.NoError(t, err)
	_, ok := m.(*remotenode.Miner)
	require.True(t, ok, "default-password command resolution should keep fleet-node routing")

	require.NoError(t, m.Reboot(t.Context()))
	assert.True(t, sender.called, "remediation miner should dispatch over the ControlStream sender")
}

// TestGetDeviceWithCredentialsAndIP_ResolvesDeviceWithSoftDeletedDiscoveryRow guards against
// dropping a still-PAIRED device whose linked discovered_device row was soft-deleted (e.g.
// the miner was re-discovered, which soft-deletes the old row and inserts a fresh one since
// the discovered_device uniqueness index is partial on deleted_at IS NULL). The direct-dial
// coordinate lookup must tolerate the linked soft-deleted row rather than return no rows.
func TestGetDeviceWithCredentialsAndIP_ResolvesDeviceWithSoftDeletedDiscoveryRow(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Arrange: a cloud-paired device whose linked discovery row is then soft-deleted.
	db, _, _, _ := setupTestDB(t)
	deviceIdentifier := "cloud-paired-stale-dd"
	deviceID := createTestDevice(t, db, deviceIdentifier)
	_, err := db.Exec(
		`UPDATE discovered_device SET deleted_at = CURRENT_TIMESTAMP
		 WHERE id = (SELECT discovered_device_id FROM device WHERE id = $1)`, deviceID)
	require.NoError(t, err)

	// Act: the direct-dial coordinate lookup the command path uses.
	row, err := sqlc.New(db).GetDeviceWithCredentialsAndIPByDeviceIdentifier(t.Context(), deviceIdentifier)

	// Assert: still resolves; the soft-deleted linked discovery row does not drop it.
	require.NoError(t, err)
	assert.Equal(t, deviceID, row.ID)
}
