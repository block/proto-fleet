package fleetnodepairing_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/apikey"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetnodeenrollment"
	"github.com/block/proto-fleet/server/internal/domain/fleetnodepairing"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
)

func setupPairingTest(t *testing.T) (*sql.DB, int64, *fleetnodepairing.Service, *fleetnodeenrollment.Service) {
	t.Helper()
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	db := testutil.GetTestDB(t)
	_, err := db.Exec(`INSERT INTO organization (id, org_id, name, miner_auth_private_key) VALUES (1, 'test-org', 'Test Org', 'dummy-key') ON CONFLICT DO NOTHING`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO "user" (id, user_id, username, password_hash) VALUES (1, 'test-user', 'op', 'dummy') ON CONFLICT DO NOTHING`)
	require.NoError(t, err)

	apiKeyStore := sqlstores.NewSQLApiKeyStore(db)
	apiKeySvc := apikey.NewService(apiKeyStore, nil)
	transactor := sqlstores.NewSQLTransactor(db)
	enrollmentStore := sqlstores.NewSQLFleetNodeEnrollmentStore(db)
	enrollmentSvc := fleetnodeenrollment.NewService(enrollmentStore, apiKeySvc, transactor, nil)
	pairingStore := sqlstores.NewSQLFleetNodePairingStore(db)
	pairingSvc := fleetnodepairing.NewService(pairingStore, enrollmentStore, transactor)

	return db, 1, pairingSvc, enrollmentSvc
}

func createFleetNode(t *testing.T, enrollment *fleetnodeenrollment.Service, orgID int64, name string) int64 {
	t.Helper()
	id := createPendingFleetNode(t, enrollment, orgID, name)
	_, _, err := enrollment.Confirm(t.Context(), id, orgID)
	require.NoError(t, err)
	return id
}

func createPendingFleetNode(t *testing.T, enrollment *fleetnodeenrollment.Service, orgID int64, name string) int64 {
	t.Helper()
	pubKey, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	signing, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	code, _, err := enrollment.CreateCode(t.Context(), 1, orgID, time.Hour)
	require.NoError(t, err)
	node, _, err := enrollment.RegisterFleetNode(t.Context(), code, name, pubKey, signing)
	require.NoError(t, err)
	return node.ID
}

// Suffix device_identifier/serial with the row id to avoid collisions
// on the partial unique indexes when tests run in parallel.
func insertDevice(t *testing.T, db *sql.DB, orgID int64) int64 {
	t.Helper()
	var ddID int64
	err := db.QueryRow(`INSERT INTO discovered_device (org_id, device_identifier, ip_address, port, url_scheme, driver_name, is_active)
		VALUES ($1, gen_random_uuid()::text, '10.0.0.1', '80', 'http', 'virtual', TRUE) RETURNING id`, orgID).Scan(&ddID)
	require.NoError(t, err)
	var devID int64
	err = db.QueryRow(`INSERT INTO device (device_identifier, mac_address, serial_number, org_id, discovered_device_id)
		VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		fmt.Sprintf("dev-%d", ddID),
		fmt.Sprintf("aa:bb:cc:00:00:%02x", ddID%256),
		fmt.Sprintf("sn-%d", ddID),
		orgID,
		ddID,
	).Scan(&devID)
	require.NoError(t, err)
	return devID
}

func TestPairUnpairListRoundTrip(t *testing.T) {
	// Arrange
	ctx := t.Context()
	db, orgID, pairing, enrollment := setupPairingTest(t)
	fleetNodeID := createFleetNode(t, enrollment, orgID, "node-pair-list")
	deviceID := insertDevice(t, db, orgID)
	assignedBy := int64(1)

	// Act 1: pair
	require.NoError(t, pairing.PairDevice(ctx, fleetNodeID, deviceID, orgID, &assignedBy))

	// Act 2: list scoped to this fleet node
	pairs, err := pairing.ListDevicesForFleetNode(ctx, fleetNodeID, orgID)
	require.NoError(t, err)

	// Assert pair present
	require.Len(t, pairs, 1)
	assert.Equal(t, fleetNodeID, pairs[0].FleetNodeID)
	assert.Equal(t, deviceID, pairs[0].DeviceID)
	require.NotNil(t, pairs[0].AssignedBy)
	assert.Equal(t, assignedBy, *pairs[0].AssignedBy)

	// Act 3: unpair
	require.NoError(t, pairing.UnpairDevice(ctx, deviceID, orgID))

	// Assert unpair removes row
	pairs, err = pairing.ListDevicesForFleetNode(ctx, fleetNodeID, orgID)
	require.NoError(t, err)
	assert.Len(t, pairs, 0)
}

func TestPairRejectsDeviceAlreadyPaired(t *testing.T) {
	// Arrange
	ctx := t.Context()
	db, orgID, pairing, enrollment := setupPairingTest(t)
	node1 := createFleetNode(t, enrollment, orgID, "node-already-1")
	node2 := createFleetNode(t, enrollment, orgID, "node-already-2")
	deviceID := insertDevice(t, db, orgID)
	require.NoError(t, pairing.PairDevice(ctx, node1, deviceID, orgID, nil))

	// Act
	err := pairing.PairDevice(ctx, node2, deviceID, orgID, nil)

	// Assert
	require.Error(t, err)
	assert.True(t, fleeterror.IsFailedPreconditionError(err), "expected FailedPrecondition for double-pair")
}

func TestPairRejectsUnknownFleetNode(t *testing.T) {
	// Arrange
	ctx := t.Context()
	db, orgID, pairing, _ := setupPairingTest(t)
	deviceID := insertDevice(t, db, orgID)

	// Act
	err := pairing.PairDevice(ctx, 99999, deviceID, orgID, nil)

	// Assert
	require.Error(t, err)
	assert.True(t, fleeterror.IsNotFoundError(err))
}

func TestPairRejectsFleetNodeFromDifferentOrg(t *testing.T) {
	// Arrange
	ctx := t.Context()
	db, orgID, pairing, enrollment := setupPairingTest(t)
	_, err := db.Exec(`INSERT INTO organization (id, org_id, name, miner_auth_private_key) VALUES (2, 'other-org', 'Other Org', 'k') ON CONFLICT DO NOTHING`)
	require.NoError(t, err)
	otherNodeID := createFleetNode(t, enrollment, 2, "node-other-org")
	deviceID := insertDevice(t, db, orgID)

	// Act
	err = pairing.PairDevice(ctx, otherNodeID, deviceID, orgID, nil)

	// Assert
	require.Error(t, err)
	assert.True(t, fleeterror.IsNotFoundError(err))
}

func TestUpsertDiscoveredDevices_RefreshesUnpairedDeviceFromOriginatingNode(t *testing.T) {
	// Arrange: simulate a device that has a promoted `device` row but no
	// fleet_node_device pairing — either operator hasn't paired yet or has
	// since unpaired. The originating fleet node must still be able to
	// refresh the discovered_device row; under the older WHERE fnd IS NULL
	// predicate this was blocked, freezing is_active / last_seen / ip.
	ctx := t.Context()
	db, orgID, pairing, enrollment := setupPairingTest(t)
	nodeID := createFleetNode(t, enrollment, orgID, "node-refresh")
	var ddID int64
	require.NoError(t, db.QueryRow(`INSERT INTO discovered_device (org_id, device_identifier, ip_address, port, url_scheme, driver_name, is_active)
		VALUES ($1, 'unpaired-shared', '10.0.0.60', '80', 'http', 'virtual', TRUE) RETURNING id`, orgID).Scan(&ddID))
	_, err := db.Exec(`INSERT INTO device (device_identifier, mac_address, serial_number, org_id, discovered_device_id)
		VALUES ($1, $2, $3, $4, $5)`,
		fmt.Sprintf("local-dev-%d", ddID),
		fmt.Sprintf("aa:bb:cc:ee:00:%02x", ddID%256),
		fmt.Sprintf("local-sn-%d", ddID),
		orgID, ddID,
	)
	require.NoError(t, err)

	// Act
	accepted, rejected, err := pairing.UpsertDiscoveredDevices(ctx, nodeID, orgID, []fleetnodepairing.DiscoveredDeviceReport{
		{DeviceIdentifier: "unpaired-shared", IPAddress: "10.0.0.99", Port: "80", URLScheme: "http", DriverName: "virtual"},
	})

	// Assert: refresh accepted, IP updated to the new value.
	require.NoError(t, err)
	assert.Equal(t, int64(1), accepted)
	assert.Equal(t, int64(0), rejected)
	var ip string
	require.NoError(t, db.QueryRow(`SELECT ip_address FROM discovered_device WHERE id = $1`, ddID).Scan(&ip))
	assert.Equal(t, "10.0.0.99", ip, "originating node must be able to refresh an unpaired device row")
}

func TestUpsertDiscoveredDevices_BatchValidationErrorRollsBack(t *testing.T) {
	// Arrange: one valid + one invalid report in the same batch. The
	// service must reject the whole batch up-front and persist nothing.
	ctx := t.Context()
	db, orgID, pairing, enrollment := setupPairingTest(t)
	nodeID := createFleetNode(t, enrollment, orgID, "node-rollback")

	// Act: report[1] has a non-private IP that validateReport rejects.
	_, _, err := pairing.UpsertDiscoveredDevices(ctx, nodeID, orgID, []fleetnodepairing.DiscoveredDeviceReport{
		{DeviceIdentifier: "rollback-ok", IPAddress: "10.0.0.5", Port: "80", URLScheme: "http", DriverName: "virtual"},
		{DeviceIdentifier: "rollback-bad", IPAddress: "8.8.8.8", Port: "80", URLScheme: "http", DriverName: "virtual"},
	})

	// Assert: error returned, neither row persisted.
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
	var rowCount int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM discovered_device WHERE org_id = $1 AND device_identifier IN ('rollback-ok', 'rollback-bad')`, orgID).Scan(&rowCount))
	assert.Equal(t, 0, rowCount, "validation failure must roll back the whole batch")
}

func TestRevokeClearsPairings(t *testing.T) {
	// Arrange
	ctx := t.Context()
	db, orgID, pairing, enrollment := setupPairingTest(t)
	nodeID := createFleetNode(t, enrollment, orgID, "node-to-revoke")
	_, _, err := pairing.UpsertDiscoveredDevices(ctx, nodeID, orgID, []fleetnodepairing.DiscoveredDeviceReport{
		{DeviceIdentifier: "revoke-shared", IPAddress: "10.0.0.30", Port: "80", URLScheme: "http", DriverName: "virtual"},
	})
	require.NoError(t, err)
	var ddID int64
	require.NoError(t, db.QueryRow(`SELECT id FROM discovered_device WHERE device_identifier = 'revoke-shared' AND org_id = $1`, orgID).Scan(&ddID))
	var devID int64
	require.NoError(t, db.QueryRow(`INSERT INTO device (device_identifier, mac_address, serial_number, org_id, discovered_device_id)
		VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		fmt.Sprintf("revoke-dev-%d", ddID),
		fmt.Sprintf("aa:bb:cc:dd:00:%02x", ddID%256),
		fmt.Sprintf("revoke-sn-%d", ddID),
		orgID, ddID,
	).Scan(&devID))
	require.NoError(t, pairing.PairDevice(ctx, nodeID, devID, orgID, nil))

	// Act
	require.NoError(t, enrollment.RevokeFleetNode(ctx, nodeID, orgID))

	// Assert
	var pairings int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM fleet_node_device WHERE fleet_node_id = $1`, nodeID).Scan(&pairings))
	assert.Equal(t, 0, pairings, "revoke must delete fleet_node_device rows")
}

func TestPairRejectsSoftDeletedFleetNode(t *testing.T) {
	// Arrange
	ctx := t.Context()
	db, orgID, pairing, enrollment := setupPairingTest(t)
	nodeID := createFleetNode(t, enrollment, orgID, "node-soft-deleted")
	deviceID := insertDevice(t, db, orgID)
	_, err := db.Exec(`UPDATE fleet_node SET deleted_at = CURRENT_TIMESTAMP WHERE id = $1 AND org_id = $2`, nodeID, orgID)
	require.NoError(t, err)

	// Act
	pairErr := pairing.PairDevice(ctx, nodeID, deviceID, orgID, nil)

	// Assert
	require.Error(t, pairErr)
	assert.True(t, fleeterror.IsNotFoundError(pairErr), "soft-deleted node must surface NotFound")
	var pairings int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM fleet_node_device WHERE fleet_node_id = $1`, nodeID).Scan(&pairings))
	assert.Equal(t, 0, pairings, "no stranded pairing row from a revoked node")
}

func TestPairRejectsPendingFleetNode(t *testing.T) {
	// Arrange
	ctx := t.Context()
	db, orgID, pairing, enrollment := setupPairingTest(t)
	pendingID := createPendingFleetNode(t, enrollment, orgID, "node-pending")
	deviceID := insertDevice(t, db, orgID)

	// Act
	err := pairing.PairDevice(ctx, pendingID, deviceID, orgID, nil)

	// Assert
	require.Error(t, err)
	assert.True(t, fleeterror.IsFailedPreconditionError(err), "expected FailedPrecondition for non-confirmed fleet node")
}

func TestPairRejectsUnknownDevice(t *testing.T) {
	// Arrange
	ctx := t.Context()
	_, orgID, pairing, enrollment := setupPairingTest(t)
	fleetNodeID := createFleetNode(t, enrollment, orgID, "node-no-device")

	// Act
	err := pairing.PairDevice(ctx, fleetNodeID, 99999, orgID, nil)

	// Assert
	require.Error(t, err)
	assert.True(t, fleeterror.IsNotFoundError(err))
}

func TestUpsertDiscoveredDevicesPersistsRow(t *testing.T) {
	// Arrange
	ctx := t.Context()
	db, orgID, pairing, enrollment := setupPairingTest(t)
	fleetNodeID := createFleetNode(t, enrollment, orgID, "node-discoverer")
	reports := []fleetnodepairing.DiscoveredDeviceReport{
		{
			DeviceIdentifier: "disc-1",
			IPAddress:        "10.0.0.10",
			Port:             "80",
			URLScheme:        "http",
			DriverName:       "virtual",
			Model:            "X9",
			Manufacturer:     "Acme",
			FirmwareVersion:  "1.0.0",
		},
		{
			DeviceIdentifier: "disc-2",
			IPAddress:        "10.0.0.11",
			Port:             "80",
			URLScheme:        "http",
			DriverName:       "virtual",
		},
	}

	// Act
	accepted, rejected, err := pairing.UpsertDiscoveredDevices(ctx, fleetNodeID, orgID, reports)

	// Assert
	require.NoError(t, err)
	assert.Equal(t, int64(2), accepted)
	assert.Equal(t, int64(0), rejected)
	var ip string
	require.NoError(t, db.QueryRow(`SELECT ip_address FROM discovered_device WHERE device_identifier = 'disc-1' AND org_id = $1`, orgID).Scan(&ip))
	assert.Equal(t, "10.0.0.10", ip)
}

func TestUpsertDiscoveredDevices_RejectsInvalidIPAddress(t *testing.T) {
	// Arrange
	ctx := t.Context()
	_, orgID, pairing, enrollment := setupPairingTest(t)
	fleetNodeID := createFleetNode(t, enrollment, orgID, "node-bad-ip")

	// Act
	_, _, err := pairing.UpsertDiscoveredDevices(ctx, fleetNodeID, orgID, []fleetnodepairing.DiscoveredDeviceReport{
		{DeviceIdentifier: "x", IPAddress: "not-an-ip", Port: "80", URLScheme: "http", DriverName: "virtual"},
	})

	// Assert
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
}

func TestUpsertDiscoveredDevices_RejectsInvalidPort(t *testing.T) {
	// Arrange
	ctx := t.Context()
	_, orgID, pairing, enrollment := setupPairingTest(t)
	fleetNodeID := createFleetNode(t, enrollment, orgID, "node-bad-port")

	// Act
	_, _, err := pairing.UpsertDiscoveredDevices(ctx, fleetNodeID, orgID, []fleetnodepairing.DiscoveredDeviceReport{
		{DeviceIdentifier: "x", IPAddress: "10.0.0.1", Port: "999999", URLScheme: "http", DriverName: "virtual"},
	})

	// Assert
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
}

func TestUpsertDiscoveredDevices_AcceptsVirtualScheme(t *testing.T) {
	// Arrange
	ctx := t.Context()
	_, orgID, pairing, enrollment := setupPairingTest(t)
	fleetNodeID := createFleetNode(t, enrollment, orgID, "node-virtual-scheme")

	// Act
	accepted, _, err := pairing.UpsertDiscoveredDevices(ctx, fleetNodeID, orgID, []fleetnodepairing.DiscoveredDeviceReport{
		{DeviceIdentifier: "virt-1", IPAddress: "10.0.0.1", Port: "80", URLScheme: "virtual", DriverName: "virtual"},
	})

	// Assert
	require.NoError(t, err)
	assert.Equal(t, int64(1), accepted)
}

// NOT EXISTS guard: a device already paired with fleet node A must not
// have its discovered_device row overwritten by a report from fleet node B.
func TestUpsertDiscoveredDevices_RejectsClaimingDevicePairedToOtherFleetNode(t *testing.T) {
	// Arrange
	ctx := t.Context()
	db, orgID, pairing, enrollment := setupPairingTest(t)
	fleetNodeA := createFleetNode(t, enrollment, orgID, "node-legacy-a")
	fleetNodeB := createFleetNode(t, enrollment, orgID, "node-legacy-b")
	var ddID int64
	require.NoError(t, db.QueryRow(`INSERT INTO discovered_device (org_id, device_identifier, ip_address, port, url_scheme, driver_name, is_active)
		VALUES ($1, 'legacy-shared', '10.0.0.50', '80', 'http', 'virtual', TRUE) RETURNING id`, orgID).Scan(&ddID))
	var devID int64
	require.NoError(t, db.QueryRow(`INSERT INTO device (device_identifier, mac_address, serial_number, org_id, discovered_device_id)
		VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		fmt.Sprintf("legacy-dev-%d", ddID),
		fmt.Sprintf("aa:bb:cc:ff:00:%02x", ddID%256),
		fmt.Sprintf("legacy-sn-%d", ddID),
		orgID, ddID,
	).Scan(&devID))
	require.NoError(t, pairing.PairDevice(ctx, fleetNodeA, devID, orgID, nil))

	// Act: fleet_node B reports the same device_identifier with a different IP.
	accepted, rejected, err := pairing.UpsertDiscoveredDevices(ctx, fleetNodeB, orgID, []fleetnodepairing.DiscoveredDeviceReport{
		{DeviceIdentifier: "legacy-shared", IPAddress: "10.0.0.99", Port: "80", URLScheme: "http", DriverName: "virtual"},
	})

	// Assert
	require.NoError(t, err)
	assert.Equal(t, int64(0), accepted)
	assert.Equal(t, int64(1), rejected, "B cannot claim a row already paired to A")
	var ip string
	require.NoError(t, db.QueryRow(`SELECT ip_address FROM discovered_device WHERE id = $1`, ddID).Scan(&ip))
	assert.Equal(t, "10.0.0.50", ip, "IP must not be overwritten by claim attempt")
}

func TestUpsertDiscoveredDevices_RejectsNonPrivateIPs(t *testing.T) {
	// Arrange
	ctx := t.Context()
	_, orgID, pairing, enrollment := setupPairingTest(t)
	fleetNodeID := createFleetNode(t, enrollment, orgID, "node-ip-ranges")

	cases := []struct {
		name string
		ip   string
	}{
		{"loopback v4", "127.0.0.1"},
		{"loopback v6", "::1"},
		{"link-local v4", "169.254.1.1"},
		{"link-local v6", "fe80::1"},
		{"public v4", "8.8.8.8"},
		{"public v6", "2606:4700:4700::1111"},
		{"multicast v4", "224.0.0.1"},
		{"unspecified v4", "0.0.0.0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Act
			_, _, err := pairing.UpsertDiscoveredDevices(ctx, fleetNodeID, orgID, []fleetnodepairing.DiscoveredDeviceReport{
				{DeviceIdentifier: "x-" + tc.name, IPAddress: tc.ip, Port: "80", URLScheme: "http", DriverName: "virtual"},
			})

			// Assert
			require.Error(t, err)
			assert.True(t, fleeterror.IsInvalidArgumentError(err), "expected InvalidArgument for %s (%s)", tc.name, tc.ip)
		})
	}
}

func TestUpsertDiscoveredDevices_AcceptsRFC4193IPv6(t *testing.T) {
	// Arrange: RFC4193 ULA range fc00::/7 is the IPv6 equivalent of
	// RFC1918 and must be accepted by the validator.
	ctx := t.Context()
	_, orgID, pairing, enrollment := setupPairingTest(t)
	fleetNodeID := createFleetNode(t, enrollment, orgID, "node-ipv6-ula")

	// Act
	accepted, _, err := pairing.UpsertDiscoveredDevices(ctx, fleetNodeID, orgID, []fleetnodepairing.DiscoveredDeviceReport{
		{DeviceIdentifier: "ula-1", IPAddress: "fd00::1", Port: "80", URLScheme: "http", DriverName: "virtual"},
	})

	// Assert
	require.NoError(t, err)
	assert.Equal(t, int64(1), accepted)
}
