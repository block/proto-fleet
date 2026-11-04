package miner_test

import (
	"context"
	"database/sql"
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/passwordupdate"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/poolconfig"
	"github.com/block/proto-fleet/server/internal/domain/miner"
	"github.com/block/proto-fleet/server/internal/domain/miner/dto"
	"github.com/block/proto-fleet/server/internal/domain/miner/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
)

type fakeCommandSender struct {
	called bool
	cmd    *gatewaypb.ControlCommand
}

func (f *fakeCommandSender) SendCommand(_ context.Context, _ int64, _ gatewaypb.CommandProtocolVersion, cmd *gatewaypb.ControlCommand) (*gatewaypb.ControlAck, error) {
	f.called = true
	f.cmd = cmd
	return &gatewaypb.ControlAck{Succeeded: true, Code: gatewaypb.AckCode_ACK_CODE_OK}, nil
}

func sentMinerCommand(t *testing.T, sender *fakeCommandSender) *gatewaypb.MinerCommand {
	t.Helper()
	require.NotNil(t, sender.cmd)
	var env gatewaypb.AgentCommand
	require.NoError(t, proto.Unmarshal(sender.cmd.GetPayload(), &env))
	require.NotNil(t, env.GetMinerCommand())
	return env.GetMinerCommand()
}

func validFleetNodeCredentialBlob(suffix byte) []byte {
	blob := make([]byte, 1+len("PFNC")+12+16)
	blob[0] = 1
	copy(blob[1:], "PFNC")
	blob[len(blob)-1] = suffix
	return blob
}

func createTestFleetNode(t *testing.T, db sqlc.DBTX, name string, identityPubkey []byte) int64 {
	t.Helper()
	var fleetNodeID int64
	require.NoError(t, db.QueryRowContext(t.Context(),
		`INSERT INTO fleet_node (org_id, name, identity_pubkey, encryption_pubkey, enrollment_status)
		 VALUES (1, $1, $2, $3, 'CONFIRMED') RETURNING id`,
		name,
		identityPubkey,
		[]byte("01234567890123456789012345678901"),
	).Scan(&fleetNodeID))
	return fleetNodeID
}

// TestMinerService_ResolvesFleetNodePairedDeviceToRemoteMiner verifies that a device
// bound to a CONFIRMED fleet node resolves to a remote-node miner whose commands
// route over the ControlStream, not a directly-dialed PluginMiner.
func TestMinerService_ResolvesFleetNodePairedDeviceToRemoteMiner(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Arrange
	db, encryptService, filesService := setupTestDB(t)
	userStore := sqlstores.NewSQLUserStore(db)
	deviceIdentifier := "fleetnode-routed-device"
	deviceID := createTestDevice(t, db, deviceIdentifier)

	fleetNodeID := createTestFleetNode(t, db, "test-fleet-node", []byte("identity-pubkey"))
	_, err := db.Exec(
		`INSERT INTO fleet_node_device (fleet_node_id, device_id, org_id) VALUES ($1, $2, 1)`,
		fleetNodeID, deviceID)
	require.NoError(t, err)

	sender := &fakeCommandSender{}
	svc := miner.NewMinerService(db, userStore, encryptService, filesService, &fakePluginManager{driverName: "antminer"}).
		WithCommandSender(sender)

	// Act
	m, err := svc.GetMinerFromDeviceIdentifier(t.Context(), models.DeviceIdentifier(deviceIdentifier))

	// Assert: routed to the fleet-node remote adapter, and commands reach the sender.
	require.NoError(t, err)
	_, ok := m.(*miner.RemoteFleetNodeMiner)
	require.True(t, ok, "fleet-node-paired device should resolve to the remote telemetry wrapper")

	require.NoError(t, m.Reboot(t.Context()))
	assert.True(t, sender.called, "command should dispatch over the ControlStream sender")
}

func TestMinerService_RoutesFleetNodeEncryptedCredentialsFromMinerCredentials(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Arrange
	db, encryptService, filesService := setupTestDB(t)
	userStore := sqlstores.NewSQLUserStore(db)
	deviceIdentifier := "fleetnode-routed-with-credentials"
	deviceID := createTestDevice(t, db, deviceIdentifier)
	encryptedUsername := validFleetNodeCredentialBlob('u')
	encryptedPassword := validFleetNodeCredentialBlob('p')

	fleetNodeID := createTestFleetNode(t, db, "test-fleet-node-credentials", []byte("identity-pubkey-credentials"))
	_, err := db.Exec(
		`INSERT INTO fleet_node_device (fleet_node_id, device_id, org_id) VALUES ($1, $2, 1)`,
		fleetNodeID, deviceID)
	require.NoError(t, err)
	_, err = db.Exec(
		`INSERT INTO miner_credentials (device_id, username_enc, password_enc)
		 VALUES ($1, $2, $3)`,
		deviceID,
		base64.StdEncoding.EncodeToString(encryptedUsername),
		base64.StdEncoding.EncodeToString(encryptedPassword))
	require.NoError(t, err)

	sender := &fakeCommandSender{}
	svc := miner.NewMinerService(db, userStore, encryptService, filesService, &fakePluginManager{driverName: "antminer"}).
		WithCommandSender(sender)

	// Act
	m, err := svc.GetMinerFromDeviceIdentifier(t.Context(), models.DeviceIdentifier(deviceIdentifier))
	require.NoError(t, err)
	require.NoError(t, m.Reboot(t.Context()))

	// Assert
	target := sentMinerCommand(t, sender).GetTarget()
	assert.Equal(t, encryptedUsername, target.GetCredentialUsername())
	assert.Equal(t, encryptedPassword, target.GetCredentialPassword())
}

func TestMinerService_IgnoresMalformedFleetNodeCredentialStrings(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Arrange
	db, encryptService, filesService := setupTestDB(t)
	userStore := sqlstores.NewSQLUserStore(db)
	deviceIdentifier := "fleetnode-routed-with-malformed-credentials"
	deviceID := createTestDevice(t, db, deviceIdentifier)

	fleetNodeID := createTestFleetNode(t, db, "test-fleet-node-malformed-credentials", []byte("identity-pubkey-malformed-credentials"))
	_, err := db.Exec(
		`INSERT INTO fleet_node_device (fleet_node_id, device_id, org_id) VALUES ($1, $2, 1)`,
		fleetNodeID, deviceID)
	require.NoError(t, err)
	_, err = db.Exec(
		`INSERT INTO miner_credentials (device_id, username_enc, password_enc)
		 VALUES ($1, $2, $3)`,
		deviceID, "not base64", "also not base64")
	require.NoError(t, err)

	sender := &fakeCommandSender{}
	svc := miner.NewMinerService(db, userStore, encryptService, filesService, &fakePluginManager{driverName: "antminer"}).
		WithCommandSender(sender)

	// Act
	m, err := svc.GetMinerFromDeviceIdentifier(t.Context(), models.DeviceIdentifier(deviceIdentifier))
	require.NoError(t, err)
	require.NoError(t, m.Reboot(t.Context()))

	// Assert: malformed strings route the command but are not forwarded as node
	// credentials. They may be legacy server-encrypted values, so the node should
	// treat them as absent instead of as decryptable node ciphertext.
	target := sentMinerCommand(t, sender).GetTarget()
	assert.Empty(t, target.GetCredentialUsername())
	assert.Empty(t, target.GetCredentialPassword())
}

func TestMinerService_IgnoresLegacyServerEncryptedCredentialStrings(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Arrange
	db, encryptService, filesService := setupTestDB(t)
	userStore := sqlstores.NewSQLUserStore(db)
	deviceIdentifier := "fleetnode-routed-with-legacy-credentials"
	deviceID := createTestDevice(t, db, deviceIdentifier)

	fleetNodeID := createTestFleetNode(t, db, "test-fleet-node-legacy-credentials", []byte("identity-pubkey-legacy-credentials"))
	_, err := db.Exec(
		`INSERT INTO fleet_node_device (fleet_node_id, device_id, org_id) VALUES ($1, $2, 1)`,
		fleetNodeID, deviceID)
	require.NoError(t, err)
	_, err = db.Exec(
		`INSERT INTO miner_credentials (device_id, username_enc, password_enc)
		 VALUES ($1, $2, $3)`,
		deviceID,
		base64.StdEncoding.EncodeToString([]byte("server-encrypted-username")),
		base64.StdEncoding.EncodeToString([]byte("server-encrypted-password")))
	require.NoError(t, err)

	sender := &fakeCommandSender{}
	svc := miner.NewMinerService(db, userStore, encryptService, filesService, &fakePluginManager{driverName: "antminer"}).
		WithCommandSender(sender)

	// Act
	m, err := svc.GetMinerFromDeviceIdentifier(t.Context(), models.DeviceIdentifier(deviceIdentifier))
	require.NoError(t, err)
	require.NoError(t, m.Reboot(t.Context()))

	// Assert
	target := sentMinerCommand(t, sender).GetTarget()
	assert.Empty(t, target.GetCredentialUsername())
	assert.Empty(t, target.GetCredentialPassword())
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
	db, encryptService, filesService := setupTestDB(t)
	userStore := sqlstores.NewSQLUserStore(db)
	deviceIdentifier := "fleetnode-bound-unpaired"
	deviceID := createTestDevice(t, db, deviceIdentifier)

	fleetNodeID := createTestFleetNode(t, db, "unpaired-fleet-node", []byte("identity-pubkey"))
	_, err := db.Exec(
		`INSERT INTO fleet_node_device (fleet_node_id, device_id, org_id) VALUES ($1, $2, 1)`,
		fleetNodeID, deviceID)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE device_pairing SET pairing_status = 'UNPAIRED' WHERE device_id = $1`, deviceID)
	require.NoError(t, err)

	svc := miner.NewMinerService(db, userStore, encryptService, filesService, &fakePluginManager{driverName: "antminer"}).
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

	db, encryptService, filesService := setupTestDB(t)
	userStore := sqlstores.NewSQLUserStore(db)
	deviceIdentifier := "fleetnode-default-password-device"
	deviceID := createTestDevice(t, db, deviceIdentifier)

	fleetNodeID := createTestFleetNode(t, db, "default-password-fleet-node", []byte("identity-pubkey"))
	_, err := db.Exec(
		`INSERT INTO fleet_node_device (fleet_node_id, device_id, org_id) VALUES ($1, $2, 1)`,
		fleetNodeID, deviceID)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE device_pairing SET pairing_status = 'DEFAULT_PASSWORD' WHERE device_id = $1`, deviceID)
	require.NoError(t, err)

	sender := &fakeCommandSender{}
	svc := miner.NewMinerService(db, userStore, encryptService, filesService, &fakePluginManager{driverName: "antminer"}).
		WithCommandSender(sender)

	m, err := svc.GetMinerFromDeviceIdentifier(t.Context(), models.DeviceIdentifier(deviceIdentifier))
	require.NoError(t, err)
	_, ok := m.(*miner.RemoteFleetNodeMiner)
	require.True(t, ok, "default-password command resolution should keep fleet-node remote routing")

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
	db, _, _ := setupTestDB(t)
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

func TestMinerService_PoolWritesUseFreshNodeKeyAndRejectChangedRoute(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	db, encryptService, filesService := setupTestDB(t)
	identifier := "pool-write-fresh-key"
	deviceID := createTestDevice(t, db, identifier)
	nodeID := createTestFleetNode(t, db, "pool-write-node", []byte("pool-write-node-identity"))
	_, err := db.Exec("INSERT INTO fleet_node_device (fleet_node_id, device_id, org_id) VALUES ($1, $2, 1)", nodeID, deviceID)
	require.NoError(t, err)
	sender := &fakeCommandSender{}
	svc := miner.NewMinerService(db, sqlstores.NewSQLUserStore(db), encryptService, filesService, &fakePluginManager{driverName: "antminer"}).WithCommandSender(sender)
	m, err := svc.GetMinerFromDeviceIdentifier(t.Context(), models.DeviceIdentifier(identifier))
	require.NoError(t, err)
	pools := dto.UpdateMiningPoolsPayload{DefaultPool: dto.MiningPool{Priority: 0, URL: "stratum+tcp://pool.example.com:3333", Username: "worker", Password: "exact-password"}}
	for range 2 {
		public, private, err := passwordupdate.GenerateKeypair()
		require.NoError(t, err)
		_, err = db.Exec("UPDATE fleet_node SET encryption_pubkey = $1 WHERE id = $2", public, nodeID)
		require.NoError(t, err)
		require.NoError(t, m.UpdateMiningPools(t.Context(), pools))
		config, err := poolconfig.Decrypt(private, sentMinerCommand(t, sender).GetUpdateMiningPools().GetEncryptedConfig(), identifier)
		require.NoError(t, err)
		require.Len(t, config.GetPools(), 1)
		assert.Equal(t, "exact-password", config.GetPools()[0].GetPassword())
	}
	otherNode := createTestFleetNode(t, db, "pool-write-new-node", []byte("pool-write-new-node-identity"))
	_, err = db.Exec("UPDATE fleet_node_device SET fleet_node_id = $1 WHERE device_id = $2", otherNode, deviceID)
	require.NoError(t, err)
	sender.called = false
	require.ErrorContains(t, m.UpdateMiningPools(t.Context(), pools), "node route changed")
	assert.False(t, sender.called)
}

// A prepared execution strategy must still resolve the current binding on every
// call, including after a lost connection and identifier reuse in another org.
func TestFleetNodeRoutePreparedPlanReadsCurrentBinding(t *testing.T) {
	conn, _, _ := setupTestDB(t)
	identifier := "prepared-route-binding"
	deviceID := createTestDevice(t, conn, identifier)
	nodeID := createTestFleetNode(t, conn, "prepared-route-node", []byte("prepared-route-node"))
	_, err := conn.ExecContext(t.Context(), "INSERT INTO fleet_node_device(fleet_node_id,device_id,org_id) VALUES($1,$2,1)", nodeID, deviceID)
	require.NoError(t, err)
	writer := sqlc.New(conn)
	require.NoError(t, writer.UpsertMinerCredentials(t.Context(), sqlc.UpsertMinerCredentialsParams{DeviceID: deviceID, UsernameEnc: "first", PasswordEnc: "first"}))
	reader, err := conn.Conn(t.Context())
	require.NoError(t, err)
	defer reader.Close()
	_, err = reader.ExecContext(t.Context(), "SET plan_cache_mode=force_generic_plan")
	require.NoError(t, err)
	queries := sqlc.New(reader)
	var before sqlc.GetFleetNodeTelemetryRouteByDeviceIdentifierRow
	for range 10 {
		before, err = queries.GetFleetNodeTelemetryRouteByDeviceIdentifier(t.Context(), identifier)
		require.NoError(t, err)
	}
	var generic int
	require.NoError(t, reader.QueryRowContext(t.Context(), `SELECT sum(generic_plans) FROM pg_prepared_statements
  WHERE statement LIKE '-- name: GetFleetNodeTelemetryRouteByDeviceIdentifier%'`).Scan(&generic))
	require.Positive(t, generic)
	require.Equal(t, nodeID, before.FleetNodeID)
	require.Equal(t, int64(0), before.WriteEpoch)

	require.NoError(t, writer.UpsertMinerCredentials(t.Context(), sqlc.UpsertMinerCredentialsParams{DeviceID: deviceID, UsernameEnc: "rotated", PasswordEnc: "rotated"}))
	epoch, err := devicewrite.NewService(conn).Reserve(t.Context(), 1, deviceID)
	require.NoError(t, err)
	replacementNode := createTestFleetNode(t, conn, "replacement-route-node", []byte("replacement-route-node"))
	_, err = conn.ExecContext(t.Context(), "UPDATE fleet_node_device SET fleet_node_id=$1 WHERE device_id=$2", replacementNode, deviceID)
	require.NoError(t, err)
	after, err := queries.GetFleetNodeTelemetryRouteByDeviceIdentifier(t.Context(), identifier)
	require.NoError(t, err)
	require.Equal(t, sql.NullString{String: "rotated", Valid: true}, after.PasswordEnc)
	require.NotEqual(t, before.CredentialVersion, after.CredentialVersion)
	require.Equal(t, epoch, after.WriteEpoch)
	require.Equal(t, replacementNode, after.FleetNodeID)

	// The live identifier is unique; a deleted incarnation in another organization
	// must contribute neither ownership, credentials nor its nonzero fence epoch.
	_, err = conn.ExecContext(t.Context(), "UPDATE device SET deleted_at=statement_timestamp() WHERE id=$1", deviceID)
	require.NoError(t, err)
	_, err = conn.ExecContext(t.Context(), "INSERT INTO organization(id,org_id,name) VALUES(2,'route-org-2','Route org 2')")
	require.NoError(t, err)
	discoveredID, err := writer.UpsertDiscoveredDevice(t.Context(), sqlc.UpsertDiscoveredDeviceParams{OrgID: 2, DeviceIdentifier: identifier, IpAddress: "192.0.2.99", Port: "80", UrlScheme: "http", DriverName: "proto", IsActive: true})
	require.NoError(t, err)
	otherID, err := writer.InsertDevice(t.Context(), sqlc.InsertDeviceParams{OrgID: 2, DiscoveredDeviceID: discoveredID, DeviceIdentifier: identifier, MacAddress: "02:00:00:00:00:99"})
	require.NoError(t, err)
	_, err = writer.UpsertDevicePairing(t.Context(), sqlc.UpsertDevicePairingParams{DeviceID: otherID, PairingStatus: sqlc.PairingStatusEnumDEFAULTPASSWORD})
	require.NoError(t, err)
	var otherNode int64
	require.NoError(t, conn.QueryRowContext(t.Context(), `INSERT INTO fleet_node(org_id,name,identity_pubkey,encryption_pubkey,enrollment_status)
  VALUES(2,'other-org-node','other-org-key','01234567890123456789012345678901','CONFIRMED') RETURNING id`).Scan(&otherNode))
	_, err = conn.ExecContext(t.Context(), "INSERT INTO fleet_node_device(fleet_node_id,device_id,org_id) VALUES($1,$2,2)", otherNode, otherID)
	require.NoError(t, err)
	checkOtherOrg := func(q *sqlc.Queries) {
		t.Helper()
		row, err := q.GetFleetNodeTelemetryRouteByDeviceIdentifier(t.Context(), identifier)
		require.NoError(t, err)
		require.Equal(t, otherID, row.ID)
		require.Equal(t, int64(2), row.OrgID)
		require.Equal(t, otherNode, row.FleetNodeID)
		require.False(t, row.CredentialVersion.Valid)
		require.False(t, row.PasswordEnc.Valid)
		require.Equal(t, int64(0), row.WriteEpoch)
	}
	checkOtherOrg(queries)

	// Connection loss discards the prepared plan. A replacement session must read
	// the same current binding; this exercises reconnect, not a primary promotion.
	var pid int
	require.NoError(t, reader.QueryRowContext(t.Context(), "SELECT pg_backend_pid()").Scan(&pid))
	var terminated bool
	require.NoError(t, conn.QueryRowContext(t.Context(), "SELECT pg_terminate_backend($1)", pid).Scan(&terminated))
	require.True(t, terminated)
	_, err = queries.GetFleetNodeTelemetryRouteByDeviceIdentifier(t.Context(), identifier)
	require.Error(t, err)
	require.NoError(t, reader.Close())
	checkOtherOrg(writer)
	_, err = conn.ExecContext(t.Context(), "UPDATE fleet_node SET enrollment_status='REVOKED' WHERE id=$1", otherNode)
	require.NoError(t, err)
	_, err = writer.GetFleetNodeTelemetryRouteByDeviceIdentifier(t.Context(), identifier)
	require.ErrorIs(t, err, sql.ErrNoRows)
}
