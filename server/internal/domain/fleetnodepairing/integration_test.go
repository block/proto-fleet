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

// Synthesized identifiers (auto:*) re-key per scan on the agent;
// server reconciles them against any prior row at the same
// (fleet_node, ip, port) endpoint so a single physical device stays a
// single row across rescans.
func TestUpsertDiscoveredDevices_ReconcilesAutoIdentifierByEndpoint(t *testing.T) {
	// Arrange
	ctx := t.Context()
	db, orgID, pairing, enrollment := setupPairingTest(t)
	nodeID := createFleetNode(t, enrollment, orgID, "node-auto-recon")

	// Scan 1: agent reports auto:<uuid-1> at 10.0.0.70.
	_, _, err := pairing.UpsertDiscoveredDevices(ctx, nodeID, orgID, []fleetnodepairing.DiscoveredDeviceReport{
		{DeviceIdentifier: "auto:uuid-1", IPAddress: "10.0.0.70", Port: "4028", URLScheme: "http", DriverName: "thirdparty"},
	})
	require.NoError(t, err)
	var firstID string
	require.NoError(t, db.QueryRow(`SELECT device_identifier FROM discovered_device WHERE ip_address = '10.0.0.70' AND port = '4028' AND discovered_by_fleet_node_id = $1`, nodeID).Scan(&firstID))
	require.Equal(t, "auto:uuid-1", firstID)

	// Act: scan 2 with a fresh auto:<uuid-2> at the same endpoint.
	accepted, rejected, err := pairing.UpsertDiscoveredDevices(ctx, nodeID, orgID, []fleetnodepairing.DiscoveredDeviceReport{
		{DeviceIdentifier: "auto:uuid-2", IPAddress: "10.0.0.70", Port: "4028", URLScheme: "http", DriverName: "thirdparty", Model: "x9000"},
	})

	// Assert: scan 2 reconciles onto scan 1's row; no new row.
	require.NoError(t, err)
	assert.Equal(t, int64(1), accepted)
	assert.Equal(t, int64(0), rejected)
	var rowCount int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM discovered_device WHERE ip_address = '10.0.0.70' AND port = '4028' AND discovered_by_fleet_node_id = $1`, nodeID).Scan(&rowCount))
	assert.Equal(t, 1, rowCount, "rescan must reuse the existing auto:* row")
	var (
		stableID string
		model    sql.NullString
	)
	require.NoError(t, db.QueryRow(`SELECT device_identifier, model FROM discovered_device WHERE ip_address = '10.0.0.70' AND port = '4028' AND discovered_by_fleet_node_id = $1`, nodeID).Scan(&stableID, &model))
	assert.Equal(t, "auto:uuid-1", stableID, "identifier stays stable across rescans")
	require.True(t, model.Valid)
	assert.Equal(t, "x9000", model.String, "rescan still refreshes the row's metadata")
}

func TestUpsertDiscoveredDevices_RejectsRetargetOfLocallyPairedDevice(t *testing.T) {
	// Arrange
	ctx := t.Context()
	db, orgID, pairing, enrollment := setupPairingTest(t)
	nodeID := createFleetNode(t, enrollment, orgID, "node-retarget")
	var ddID int64
	require.NoError(t, db.QueryRow(`INSERT INTO discovered_device (org_id, device_identifier, ip_address, port, url_scheme, driver_name, is_active, discovered_by_fleet_node_id)
		VALUES ($1, 'local-only', '10.0.0.60', '80', 'http', 'virtual', TRUE, NULL) RETURNING id`, orgID).Scan(&ddID))
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
		{DeviceIdentifier: "local-only", IPAddress: "10.0.0.99", Port: "80", URLScheme: "http", DriverName: "virtual"},
	})

	// Assert
	require.NoError(t, err)
	assert.Equal(t, int64(0), accepted)
	assert.Equal(t, int64(1), rejected, "fleet node cannot retarget a locally-paired device")
	var ip string
	require.NoError(t, db.QueryRow(`SELECT ip_address FROM discovered_device WHERE id = $1`, ddID).Scan(&ip))
	assert.Equal(t, "10.0.0.60", ip, "ip_address must not be overwritten")
}

func TestRevokeClearsPairingsAndAttribution(t *testing.T) {
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
	var attributed sql.NullInt64
	require.NoError(t, db.QueryRow(`SELECT discovered_by_fleet_node_id FROM discovered_device WHERE id = $1`, ddID).Scan(&attributed))
	assert.False(t, attributed.Valid, "revoke must clear discovered_device.discovered_by_fleet_node_id")
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

func TestUpsertDiscoveredDevicesAttributesFleetNode(t *testing.T) {
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
	var attributed sql.NullInt64
	require.NoError(t, db.QueryRow(`SELECT discovered_by_fleet_node_id FROM discovered_device WHERE device_identifier = 'disc-1' AND org_id = $1`, orgID).Scan(&attributed))
	require.True(t, attributed.Valid)
	assert.Equal(t, fleetNodeID, attributed.Int64)
}

func TestUpsertDiscoveredDevices_RejectsReportFromOtherFleetNode(t *testing.T) {
	// Arrange: fleet_node A discovers device first; then fleet_node B
	// reports the same device_identifier. B's report must be a no-op so
	// it cannot redirect the IP/endpoint that the org sees for that
	// device.
	ctx := t.Context()
	db, orgID, pairing, enrollment := setupPairingTest(t)
	fleetNodeA := createFleetNode(t, enrollment, orgID, "node-a")
	fleetNodeB := createFleetNode(t, enrollment, orgID, "node-b")
	original := fleetnodepairing.DiscoveredDeviceReport{
		DeviceIdentifier: "shared",
		IPAddress:        "10.0.0.10",
		Port:             "80",
		URLScheme:        "http",
		DriverName:       "virtual",
	}
	_, _, err := pairing.UpsertDiscoveredDevices(ctx, fleetNodeA, orgID, []fleetnodepairing.DiscoveredDeviceReport{original})
	require.NoError(t, err)

	// Act: fleet_node B tries to overwrite with a different IP.
	hostile := original
	hostile.IPAddress = "10.0.0.99"
	accepted, rejected, err := pairing.UpsertDiscoveredDevices(ctx, fleetNodeB, orgID, []fleetnodepairing.DiscoveredDeviceReport{hostile})

	// Assert
	require.NoError(t, err)
	assert.Equal(t, int64(0), accepted)
	assert.Equal(t, int64(1), rejected, "report from non-attributing fleet node must be rejected silently")
	var (
		ip         string
		attributed sql.NullInt64
	)
	require.NoError(t, db.QueryRow(`SELECT ip_address, discovered_by_fleet_node_id FROM discovered_device WHERE device_identifier = 'shared' AND org_id = $1`, orgID).Scan(&ip, &attributed))
	assert.Equal(t, "10.0.0.10", ip, "IP must not be overwritten by another fleet node")
	require.True(t, attributed.Valid)
	assert.Equal(t, fleetNodeA, attributed.Int64, "attribution must remain with the original discoverer")
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

func TestUpsertDiscoveredDevices_RejectsDisallowedScheme(t *testing.T) {
	// Arrange
	ctx := t.Context()
	_, orgID, pairing, enrollment := setupPairingTest(t)
	fleetNodeID := createFleetNode(t, enrollment, orgID, "node-bad-scheme")

	// Act
	_, _, err := pairing.UpsertDiscoveredDevices(ctx, fleetNodeID, orgID, []fleetnodepairing.DiscoveredDeviceReport{
		{DeviceIdentifier: "x", IPAddress: "10.0.0.1", Port: "80", URLScheme: "ftp", DriverName: "virtual"},
	})

	// Assert
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
}

// Defensive NOT EXISTS: a NULL-attributed row with an existing pairing
// to A must not be claimable by B (manual repairs, restored backups).
func TestUpsertDiscoveredDevices_RejectsClaimingDevicePairedToOtherFleetNode(t *testing.T) {
	// Arrange
	ctx := t.Context()
	db, orgID, pairing, enrollment := setupPairingTest(t)
	fleetNodeA := createFleetNode(t, enrollment, orgID, "node-legacy-a")
	fleetNodeB := createFleetNode(t, enrollment, orgID, "node-legacy-b")
	var ddID int64
	require.NoError(t, db.QueryRow(`INSERT INTO discovered_device (org_id, device_identifier, ip_address, port, url_scheme, driver_name, is_active, discovered_by_fleet_node_id)
		VALUES ($1, 'legacy-shared', '10.0.0.50', '80', 'http', 'virtual', TRUE, NULL) RETURNING id`, orgID).Scan(&ddID))
	var devID int64
	require.NoError(t, db.QueryRow(`INSERT INTO device (device_identifier, mac_address, serial_number, org_id, discovered_device_id)
		VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		fmt.Sprintf("legacy-dev-%d", ddID),
		fmt.Sprintf("aa:bb:cc:ff:00:%02x", ddID%256),
		fmt.Sprintf("legacy-sn-%d", ddID),
		orgID, ddID,
	).Scan(&devID))
	require.NoError(t, pairing.PairDevice(ctx, fleetNodeA, devID, orgID, nil))
	_, err := db.Exec(`UPDATE discovered_device SET discovered_by_fleet_node_id = NULL WHERE id = $1`, ddID)
	require.NoError(t, err)

	// Act: fleet_node B reports the same device_identifier with a different IP.
	accepted, rejected, err := pairing.UpsertDiscoveredDevices(ctx, fleetNodeB, orgID, []fleetnodepairing.DiscoveredDeviceReport{
		{DeviceIdentifier: "legacy-shared", IPAddress: "10.0.0.99", Port: "80", URLScheme: "http", DriverName: "virtual"},
	})

	// Assert
	require.NoError(t, err)
	assert.Equal(t, int64(0), accepted)
	assert.Equal(t, int64(1), rejected, "B cannot claim a NULL-attributed row already paired to A")
	var (
		ip         string
		attributed sql.NullInt64
	)
	require.NoError(t, db.QueryRow(`SELECT ip_address, discovered_by_fleet_node_id FROM discovered_device WHERE id = $1`, ddID).Scan(&ip, &attributed))
	assert.Equal(t, "10.0.0.50", ip, "IP must not be overwritten by claim attempt")
	assert.False(t, attributed.Valid, "row must remain NULL-attributed; the upsert is a no-op so attribution does not change")
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

func TestPairUnpair_SyncsDiscoveredDeviceAttribution(t *testing.T) {
	// Arrange: node A discovers the device, operator decides to pair
	// it to node B instead.
	ctx := t.Context()
	db, orgID, pairing, enrollment := setupPairingTest(t)
	nodeA := createFleetNode(t, enrollment, orgID, "node-attr-a")
	nodeB := createFleetNode(t, enrollment, orgID, "node-attr-b")
	_, _, err := pairing.UpsertDiscoveredDevices(ctx, nodeA, orgID, []fleetnodepairing.DiscoveredDeviceReport{
		{DeviceIdentifier: "attr-shared", IPAddress: "10.0.0.40", Port: "80", URLScheme: "http", DriverName: "virtual"},
	})
	require.NoError(t, err)
	var ddID, devID int64
	require.NoError(t, db.QueryRow(`SELECT id FROM discovered_device WHERE device_identifier = 'attr-shared' AND org_id = $1`, orgID).Scan(&ddID))
	require.NoError(t, db.QueryRow(`INSERT INTO device (device_identifier, mac_address, serial_number, org_id, discovered_device_id)
		VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		fmt.Sprintf("attr-dev-%d", ddID),
		fmt.Sprintf("aa:bb:cc:fe:00:%02x", ddID%256),
		fmt.Sprintf("attr-sn-%d", ddID),
		orgID, ddID,
	).Scan(&devID))

	// Act 1: operator pairs device to node B.
	require.NoError(t, pairing.PairDevice(ctx, nodeB, devID, orgID, nil))

	// Assert: attribution moved to B.
	var attributed sql.NullInt64
	require.NoError(t, db.QueryRow(`SELECT discovered_by_fleet_node_id FROM discovered_device WHERE id = $1`, ddID).Scan(&attributed))
	require.True(t, attributed.Valid)
	assert.Equal(t, nodeB, attributed.Int64, "pair must transfer attribution to the new owner")

	// Act 2: node B's next report goes through cleanly.
	accepted, rejected, err := pairing.UpsertDiscoveredDevices(ctx, nodeB, orgID, []fleetnodepairing.DiscoveredDeviceReport{
		{DeviceIdentifier: "attr-shared", IPAddress: "10.0.0.41", Port: "80", URLScheme: "http", DriverName: "virtual"},
	})

	// Assert: B's report was accepted.
	require.NoError(t, err)
	assert.Equal(t, int64(1), accepted)
	assert.Equal(t, int64(0), rejected)

	// Act 3: unpair clears attribution.
	require.NoError(t, pairing.UnpairDevice(ctx, devID, orgID))

	// Assert: attribution NULL again.
	require.NoError(t, db.QueryRow(`SELECT discovered_by_fleet_node_id FROM discovered_device WHERE id = $1`, ddID).Scan(&attributed))
	assert.False(t, attributed.Valid, "unpair must clear attribution so the next pair fully resets ownership")
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
