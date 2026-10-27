package pairing_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/apikey"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	fleetnodeenrollment "github.com/block/proto-fleet/server/internal/domain/fleetnode/enrollment"
	fleetnodepairing "github.com/block/proto-fleet/server/internal/domain/fleetnode/pairing"
	minermodels "github.com/block/proto-fleet/server/internal/domain/miner/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	telemetrymodels "github.com/block/proto-fleet/server/internal/domain/telemetry/models"
	"github.com/block/proto-fleet/server/internal/testutil"
)

func setupPairingTest(t *testing.T) (*sql.DB, int64, *fleetnodepairing.Service, *fleetnodeenrollment.Service) {
	t.Helper()
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	db := testutil.GetTestDB(t)
	_, err := db.Exec(`INSERT INTO organization (id, org_id, name) VALUES (1, 'test-org', 'Test Org') ON CONFLICT DO NOTHING`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO "user" (id, user_id, username, password_hash) VALUES (1, 'test-user', 'op', 'dummy') ON CONFLICT DO NOTHING`)
	require.NoError(t, err)

	apiKeyStore := sqlstores.NewSQLApiKeyStore(db)
	apiKeySvc := apikey.NewService(apiKeyStore, nil)
	transactor := sqlstores.NewSQLTransactor(db)
	enrollmentStore := sqlstores.NewSQLFleetNodeEnrollmentStore(db)
	enrollmentSvc := fleetnodeenrollment.NewService(enrollmentStore, apiKeySvc, transactor, nil)
	pairingStore := sqlstores.NewSQLFleetNodePairingStore(db)
	pairingSvc := fleetnodepairing.NewService(pairingStore, enrollmentStore, transactor).
		WithProvisioning(sqlstores.NewSQLDeviceStore(db), sqlstores.NewSQLDiscoveredDeviceStore(db), nil)

	return db, 1, pairingSvc, enrollmentSvc
}

func createFleetNode(t *testing.T, enrollment *fleetnodeenrollment.Service, orgID int64, name string) int64 {
	t.Helper()
	id, pendingEnrollmentID := createPendingFleetNode(t, enrollment, orgID, name)
	_, _, err := enrollment.ConfirmExpected(t.Context(), id, orgID, pendingEnrollmentID, 1)
	require.NoError(t, err)
	return id
}

func createPendingFleetNode(t *testing.T, enrollment *fleetnodeenrollment.Service, orgID int64, name string) (int64, int64) {
	t.Helper()
	pubKey, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	code, pendingEnrollmentID, _, err := enrollment.CreateCodeWithEnrollmentID(t.Context(), 1, orgID, time.Hour)
	require.NoError(t, err)
	node, _, err := enrollment.RegisterFleetNode(t.Context(), code, name, pubKey, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	return node.ID, pendingEnrollmentID
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
	_, err := db.Exec(
		`INSERT INTO miner_credentials (device_id, username_enc, password_enc)
		 VALUES ($1, 'server-username', 'server-password')`,
		deviceID,
	)
	require.NoError(t, err)

	// Act 1: pair
	require.NoError(t, pairing.PairDevice(ctx, fleetNodeID, deviceID, orgID, &assignedBy))

	// Assert pair clears any credentials from the previous cloud-owned route.
	var credentialRows int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM miner_credentials WHERE device_id=$1`, deviceID).Scan(&credentialRows))
	assert.Zero(t, credentialRows)

	_, err = db.Exec(
		`INSERT INTO miner_credentials (device_id, username_enc, password_enc)
		 VALUES ($1, 'stale-repeat-username', 'stale-repeat-password')`,
		deviceID,
	)
	require.NoError(t, err)
	require.NoError(t, pairing.PairDevice(ctx, fleetNodeID, deviceID, orgID, &assignedBy))
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM miner_credentials WHERE device_id=$1`, deviceID).Scan(&credentialRows))
	assert.Equal(t, 1, credentialRows, "idempotent same-node pair preserves working node credentials")

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
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM miner_credentials WHERE device_id=$1`, deviceID).Scan(&credentialRows))
	assert.Zero(t, credentialRows)
}

func TestUnpairDoesNotDeleteCredentialsAcrossOrgs(t *testing.T) {
	// Arrange
	ctx := t.Context()
	db, orgID, pairing, enrollment := setupPairingTest(t)
	fleetNodeID := createFleetNode(t, enrollment, orgID, "node-cross-org-unpair")
	deviceID := insertDevice(t, db, orgID)
	require.NoError(t, pairing.PairDevice(ctx, fleetNodeID, deviceID, orgID, nil))
	_, err := db.Exec(
		`INSERT INTO miner_credentials (device_id, username_enc, password_enc)
		 VALUES ($1, 'node-owned-username', 'node-owned-password')`,
		deviceID,
	)
	require.NoError(t, err)

	const otherOrgID = int64(2)
	_, err = db.Exec(`INSERT INTO organization (id, org_id, name) VALUES ($1, 'other-org', 'Other Org') ON CONFLICT DO NOTHING`, otherOrgID)
	require.NoError(t, err)

	// Act: the target device id exists, but not as a fleet-node binding in this org.
	require.NoError(t, pairing.UnpairDevice(ctx, deviceID, otherOrgID))

	// Assert
	var credentialRows int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM miner_credentials WHERE device_id=$1`, deviceID).Scan(&credentialRows))
	assert.Equal(t, 1, credentialRows)
}

func TestPairUnpairInvalidatesMinerCache(t *testing.T) {
	// Arrange: record the device ids whose miner cache gets invalidated.
	ctx := t.Context()
	db, orgID, pairing, enrollment := setupPairingTest(t)
	fleetNodeID := createFleetNode(t, enrollment, orgID, "node-invalidate")
	deviceID := insertDevice(t, db, orgID)
	var invalidated []int64
	pairing.WithMinerInvalidator(func(_ context.Context, id int64) { invalidated = append(invalidated, id) })

	// Act
	require.NoError(t, pairing.PairDevice(ctx, fleetNodeID, deviceID, orgID, nil))
	require.NoError(t, pairing.UnpairDevice(ctx, deviceID, orgID))

	// Assert: both transitions evict the device's (possibly stale direct) miner handle
	// so routing re-resolves instead of dialing a cached handle until the cache TTL.
	assert.Equal(t, []int64{deviceID, deviceID}, invalidated)
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

// A device the cloud actively dials (paired-like with no fleet_node_device) must
// not be pairable to a fleet node: the discovery upsert guard would then reject
// the node's refreshes, leaving a device that reads as fleet-node paired but never
// refreshes.
func TestPairRejectsCloudPairedLikeDevice(t *testing.T) {
	for _, pairingStatus := range []string{"PAIRED", "DEFAULT_PASSWORD"} {
		t.Run(pairingStatus, func(t *testing.T) {
			// Arrange
			ctx := t.Context()
			db, orgID, pairing, enrollment := setupPairingTest(t)
			nodeID := createFleetNode(t, enrollment, orgID, "node-cloud-paired")
			deviceID := insertDevice(t, db, orgID)
			_, err := db.Exec(`INSERT INTO device_pairing (device_id, pairing_status, paired_at) VALUES ($1, $2, NOW())`, deviceID, pairingStatus)
			require.NoError(t, err)

			// Act
			err = pairing.PairDevice(ctx, nodeID, deviceID, orgID, nil)

			// Assert
			require.Error(t, err)
			assert.True(t, fleeterror.IsFailedPreconditionError(err), "expected FailedPrecondition for cloud-paired-like device")
		})
	}
}

// After a fleet node is revoked (soft-deleted), a replacement node must be able
// to re-discover the same stable mac:/serial: device. The upsert guard treats a
// revoked attributing node as reclaimable; attribution moves to the new node and
// stays non-NULL, so cloud-exclusion is preserved. A live node's rows are not
// reclaimable (covered by the publish-only-accepted gateway test).
func TestUpsertReclaimsRowFromRevokedFleetNode(t *testing.T) {
	// Arrange
	ctx := t.Context()
	db, orgID, pairing, enrollment := setupPairingTest(t)
	nodeA := createFleetNode(t, enrollment, orgID, "node-revoked-origin")
	nodeB := createFleetNode(t, enrollment, orgID, "node-replacement-scan")
	const ident = "mac:aa:bb:cc:dd:ee:01"
	report := fleetnodepairing.DiscoveredDeviceReport{DeviceIdentifier: ident, IPAddress: "192.168.1.50", Port: "4028", URLScheme: "http", DriverName: "virtual"}
	acceptedA, _, err := pairing.UpsertDiscoveredDevices(ctx, nodeA, orgID, []fleetnodepairing.DiscoveredDeviceReport{report})
	require.NoError(t, err)
	require.Equal(t, []fleetnodepairing.DiscoveredDeviceReport{report}, acceptedA)
	require.NoError(t, enrollment.RevokeFleetNode(ctx, nodeA, orgID))

	// Act: the replacement node re-discovers the same stable-identifier device.
	acceptedB, rejected, err := pairing.UpsertDiscoveredDevices(ctx, nodeB, orgID, []fleetnodepairing.DiscoveredDeviceReport{report})

	// Assert: reclaimed, with attribution moved to node B.
	require.NoError(t, err)
	assert.Equal(t, []fleetnodepairing.DiscoveredDeviceReport{report}, acceptedB)
	assert.Equal(t, int64(0), rejected)
	var attributed int64
	require.NoError(t, db.QueryRow(`SELECT discovered_by_fleet_node_id FROM discovered_device WHERE org_id = $1 AND device_identifier = $2`, orgID, ident).Scan(&attributed))
	assert.Equal(t, nodeB, attributed)
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
	_, err := db.Exec(`INSERT INTO organization (id, org_id, name) VALUES (2, 'other-org', 'Other Org') ON CONFLICT DO NOTHING`)
	require.NoError(t, err)
	otherNodeID := createFleetNode(t, enrollment, 2, "node-other-org")
	deviceID := insertDevice(t, db, orgID)

	// Act
	err = pairing.PairDevice(ctx, otherNodeID, deviceID, orgID, nil)

	// Assert
	require.Error(t, err)
	assert.True(t, fleeterror.IsNotFoundError(err))
}

// Pairing a device to a replacement fleet node must transfer discovery
// attribution, so the new node's reports refresh the row instead of being
// rejected by the upsert's attribution guard.
func TestPairDeviceTransfersDiscoveryAttribution(t *testing.T) {
	// Arrange
	ctx := t.Context()
	db, orgID, pairing, enrollment := setupPairingTest(t)
	nodeA := createFleetNode(t, enrollment, orgID, "node-original")
	nodeB := createFleetNode(t, enrollment, orgID, "node-replacement")
	deviceID := insertDevice(t, db, orgID)
	_, err := db.Exec(`UPDATE discovered_device SET discovered_by_fleet_node_id = $1
		FROM device WHERE device.id = $2 AND device.discovered_device_id = discovered_device.id`, nodeA, deviceID)
	require.NoError(t, err)

	// Act: pair the device to replacement node B.
	require.NoError(t, pairing.PairDevice(ctx, nodeB, deviceID, orgID, nil))

	// Assert: attribution now points to node B.
	var ident, ip, port string
	var attributed int64
	require.NoError(t, db.QueryRow(`SELECT dd.device_identifier, dd.ip_address, dd.port, dd.discovered_by_fleet_node_id
		FROM discovered_device dd JOIN device d ON d.discovered_device_id = dd.id
		WHERE d.id = $1`, deviceID).Scan(&ident, &ip, &port, &attributed))
	assert.Equal(t, nodeB, attributed)

	// Assert: node B can now refresh the row (pre-transfer the upsert guard
	// rejected it on attribution mismatch).
	accepted, rejected, err := pairing.UpsertDiscoveredDevices(ctx, nodeB, orgID, []fleetnodepairing.DiscoveredDeviceReport{
		{DeviceIdentifier: ident, IPAddress: ip, Port: port, URLScheme: "http", DriverName: "virtual"},
	})
	require.NoError(t, err)
	assert.Len(t, accepted, 1)
	assert.Equal(t, int64(0), rejected)
}

func TestUpsertDiscoveredDevices_RescanReusesRowForStableIdentifier(t *testing.T) {
	// Arrange: the agent now synthesizes a stable identifier per device, so a
	// rescan reports the same auto:* identifier. The upsert must reuse the one
	// row (no server-side reconciliation) and refresh its metadata.
	ctx := t.Context()
	db, orgID, pairing, enrollment := setupPairingTest(t)
	nodeID := createFleetNode(t, enrollment, orgID, "node-auto-stable")

	// Scan 1.
	_, _, err := pairing.UpsertDiscoveredDevices(ctx, nodeID, orgID, []fleetnodepairing.DiscoveredDeviceReport{
		{DeviceIdentifier: "auto:stable", IPAddress: "10.0.0.70", Port: "4028", URLScheme: "http", DriverName: "thirdparty"},
	})
	require.NoError(t, err)

	// Act: scan 2 reports the same stable identifier with refreshed metadata.
	accepted, rejected, err := pairing.UpsertDiscoveredDevices(ctx, nodeID, orgID, []fleetnodepairing.DiscoveredDeviceReport{
		{DeviceIdentifier: "auto:stable", IPAddress: "10.0.0.70", Port: "4028", URLScheme: "http", DriverName: "thirdparty", Model: "x9000"},
	})

	// Assert: one row, accepted, metadata refreshed.
	require.NoError(t, err)
	assert.Len(t, accepted, 1)
	assert.Equal(t, int64(0), rejected)
	var rowCount int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM discovered_device WHERE ip_address = '10.0.0.70' AND port = '4028' AND discovered_by_fleet_node_id = $1`, nodeID).Scan(&rowCount))
	assert.Equal(t, 1, rowCount, "rescan with the same identifier must reuse the existing row")
	var model sql.NullString
	require.NoError(t, db.QueryRow(`SELECT model FROM discovered_device WHERE device_identifier = 'auto:stable' AND org_id = $1`, orgID).Scan(&model))
	require.True(t, model.Valid)
	assert.Equal(t, "x9000", model.String, "rescan refreshes the row's metadata")
}

func TestUpsertDiscoveredDevices_PromotedNodeReusesSyntheticIdentity(t *testing.T) {
	for _, tc := range []struct {
		name    string
		prefix  string
		class   commonpb.DeviceClass
		driver  string
		port    string
		rpcPort int32
	}{
		{name: "container", prefix: "container:", class: commonpb.DeviceClass_DEVICE_CLASS_CONTAINER, driver: "proto", port: "443", rpcPort: 50051},
		{name: "rig", prefix: "auto:", class: commonpb.DeviceClass_DEVICE_CLASS_RIG, driver: "antminer", port: "4028"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			db, orgID, pairing, enrollment := setupPairingTest(t)
			active := createFleetNode(t, enrollment, orgID, "synthetic-active")
			promoted := createFleetNode(t, enrollment, orgID, "synthetic-promoted")
			unrelated := createFleetNode(t, enrollment, orgID, "synthetic-unrelated")
			report := fleetnodepairing.DiscoveredDeviceReport{
				DeviceIdentifier: tc.prefix + "active-hash", IPAddress: "10.0.0.70", Port: tc.port,
				URLScheme: "https", DriverName: tc.driver, DeviceClass: tc.class, Model: "test-model",
			}
			accepted, rejected, err := pairing.UpsertDiscoveredDevices(ctx, active, orgID, []fleetnodepairing.DiscoveredDeviceReport{report})
			require.NoError(t, err)
			require.Equal(t, int64(0), rejected)
			require.Equal(t, []fleetnodepairing.DiscoveredDeviceReport{report}, accepted)
			deviceID := deviceForDiscovered(t, db, orgID, report.DeviceIdentifier)
			require.NoError(t, pairing.PairDevice(ctx, active, deviceID, orgID, nil))
			setPairingStatus(t, db, deviceID, "PAIRED")

			other := report
			other.DeviceIdentifier = tc.prefix + "unrelated-hash"
			accepted, rejected, err = pairing.UpsertDiscoveredDevices(ctx, unrelated, orgID, []fleetnodepairing.DiscoveredDeviceReport{other})
			require.NoError(t, err)
			require.Zero(t, rejected)
			require.Equal(t, []fleetnodepairing.DiscoveredDeviceReport{other}, accepted)

			tx, err := db.BeginTx(ctx, nil)
			require.NoError(t, err)
			t.Cleanup(func() { _ = tx.Rollback() })
			q := sqlc.New(tx)
			moved, err := q.MoveFleetNodeDevices(ctx, sqlc.MoveFleetNodeDevicesParams{FromNodeID: active, ToNodeID: promoted, OrgID: orgID})
			require.NoError(t, err)
			require.Equal(t, []string{"dev-" + report.DeviceIdentifier}, moved)
			require.NoError(t, q.MoveDiscoveredDeviceAttribution(ctx, sqlc.MoveDiscoveredDeviceAttributionParams{FromNodeID: sql.NullInt64{Int64: active, Valid: true}, ToNodeID: sql.NullInt64{Int64: promoted, Valid: true}, OrgID: orgID}))
			require.NoError(t, tx.Commit())

			rescan := report
			rescan.DeviceIdentifier = tc.prefix + "promoted-hash"
			rescan.FirmwareVersion = "updated"
			rescan.URLScheme = "http"
			if tc.rpcPort != 0 {
				rescan.Port, rescan.URLScheme = "50051", "http"
				rescan.RPCPort = tc.rpcPort
			}
			accepted, rejected, err = pairing.UpsertDiscoveredDevices(ctx, promoted, orgID, []fleetnodepairing.DiscoveredDeviceReport{rescan})
			require.NoError(t, err)
			require.Zero(t, rejected)
			require.Len(t, accepted, 1)
			assert.Equal(t, report.DeviceIdentifier, accepted[0].DeviceIdentifier, "published discovery must use the paired canonical identity")
			assert.Equal(t, tc.class, accepted[0].DeviceClass)
			assert.Equal(t, tc.prefix+"promoted-hash", rescan.DeviceIdentifier, "the caller's report remains unchanged")

			var count int
			require.NoError(t, db.QueryRow(`SELECT count(*) FROM discovered_device WHERE org_id=$1 AND ip_address=$2 AND deleted_at IS NULL`, orgID, report.IPAddress).Scan(&count))
			assert.Equal(t, 2, count, "rediscovery must not add a third row for the inherited paired device")
			var identifier, firmware, port string
			var attributed int64
			require.NoError(t, db.QueryRow(`SELECT dd.device_identifier, dd.firmware_version, dd.port, dd.discovered_by_fleet_node_id
				FROM discovered_device dd JOIN device d ON d.discovered_device_id=dd.id WHERE d.id=$1`, deviceID).Scan(&identifier, &firmware, &port, &attributed))
			assert.Equal(t, report.DeviceIdentifier, identifier)
			assert.Equal(t, "updated", firmware)
			assert.Equal(t, report.Port, port, "native CT1 reports retain the browser endpoint")
			assert.Equal(t, promoted, attributed)
			unpaired, _, err := pairing.ListDiscoveredDevicesForFleetNode(ctx, orgID, &promoted, nil, nil)
			require.NoError(t, err)
			assert.Empty(t, unpaired, "a promoted paired device must not appear available for pairing")

			stable := rescan
			stable.DeviceIdentifier = "serial:distinct-device"
			accepted, rejected, err = pairing.UpsertDiscoveredDevices(ctx, promoted, orgID, []fleetnodepairing.DiscoveredDeviceReport{stable})
			require.NoError(t, err)
			require.Zero(t, rejected)
			require.Len(t, accepted, 1)
			assert.Equal(t, stable.DeviceIdentifier, accepted[0].DeviceIdentifier)

			foreign := rescan
			foreign.DeviceIdentifier = other.DeviceIdentifier
			accepted, rejected, err = pairing.UpsertDiscoveredDevices(ctx, promoted, orgID, []fleetnodepairing.DiscoveredDeviceReport{foreign})
			require.NoError(t, err)
			assert.Equal(t, int64(1), rejected)
			assert.Empty(t, accepted)
		})
	}
}

func TestUpsertDiscoveredDevices_SyntheticEndpointRequiresMatchingIdentity(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*fleetnodepairing.DiscoveredDeviceReport)
	}{
		{name: "driver", change: func(r *fleetnodepairing.DiscoveredDeviceReport) { r.DriverName = "different-driver" }},
		{name: "model", change: func(r *fleetnodepairing.DiscoveredDeviceReport) { r.Model = "different-model" }},
		{name: "class", change: func(r *fleetnodepairing.DiscoveredDeviceReport) {
			r.DeviceClass = commonpb.DeviceClass_DEVICE_CLASS_UNSPECIFIED
		}},
		{name: "port", change: func(r *fleetnodepairing.DiscoveredDeviceReport) { r.Port = "4029" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			db, orgID, pairing, enrollment := setupPairingTest(t)
			node := createFleetNode(t, enrollment, orgID, "synthetic-identity")
			original := fleetnodepairing.DiscoveredDeviceReport{
				DeviceIdentifier: "auto:original", IPAddress: "10.0.0.70", Port: "4028", URLScheme: "http",
				DriverName: "antminer", DeviceClass: commonpb.DeviceClass_DEVICE_CLASS_RIG, Model: "test-model",
			}
			_, _, err := pairing.UpsertDiscoveredDevices(ctx, node, orgID, []fleetnodepairing.DiscoveredDeviceReport{original})
			require.NoError(t, err)
			distinct := original
			distinct.DeviceIdentifier = "auto:distinct"
			tc.change(&distinct)
			accepted, rejected, err := pairing.UpsertDiscoveredDevices(ctx, node, orgID, []fleetnodepairing.DiscoveredDeviceReport{distinct})
			require.NoError(t, err)
			require.Zero(t, rejected)
			require.Equal(t, []fleetnodepairing.DiscoveredDeviceReport{distinct}, accepted)
			var count int
			require.NoError(t, db.QueryRow(`SELECT count(*) FROM discovered_device WHERE org_id=$1 AND discovered_by_fleet_node_id=$2 AND deleted_at IS NULL`, orgID, node).Scan(&count))
			assert.Equal(t, 2, count, "a different identity tuple must not overwrite the prior synthetic device")
		})
	}
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
	require.NoError(t, db.QueryRow(`INSERT INTO discovered_device (org_id, device_identifier, ip_address, port, url_scheme, driver_name, is_active, discovered_by_fleet_node_id)
		VALUES ($1, 'unpaired-shared', '10.0.0.60', '80', 'http', 'virtual', TRUE, $2) RETURNING id`, orgID, nodeID).Scan(&ddID))
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
	assert.Len(t, accepted, 1)
	assert.Equal(t, int64(0), rejected)
	var ip string
	require.NoError(t, db.QueryRow(`SELECT ip_address FROM discovered_device WHERE id = $1`, ddID).Scan(&ip))
	assert.Equal(t, "10.0.0.99", ip, "originating node must be able to refresh an unpaired device row")
}

func TestUpsertDiscoveredDevices_RejectsClaimingCloudDiscoveredBareDevice(t *testing.T) {
	// Arrange: a cloud-origin discovered row has no fleet-node attribution and
	// no active pairing yet. A fleet node must not be able to take ownership of
	// that identifier because generic Pair requests route credentials by the DB
	// attribution.
	ctx := t.Context()
	db, orgID, pairing, enrollment := setupPairingTest(t)
	fleetNodeID := createFleetNode(t, enrollment, orgID, "node-cloud-bare-claim")
	var ddID int64
	require.NoError(t, db.QueryRow(`INSERT INTO discovered_device (org_id, device_identifier, ip_address, port, url_scheme, driver_name, is_active, discovered_by_fleet_node_id)
		VALUES ($1, 'cloud-bare-shared', '10.0.0.60', '80', 'http', 'virtual', TRUE, NULL) RETURNING id`, orgID).Scan(&ddID))

	// Act
	accepted, rejected, err := pairing.UpsertDiscoveredDevices(ctx, fleetNodeID, orgID, []fleetnodepairing.DiscoveredDeviceReport{
		{DeviceIdentifier: "cloud-bare-shared", IPAddress: "10.0.0.99", Port: "80", URLScheme: "http", DriverName: "virtual"},
	})

	// Assert: rejected; cloud-origin endpoint and attribution remain untouched.
	require.NoError(t, err)
	assert.Empty(t, accepted)
	assert.Equal(t, int64(1), rejected)
	var (
		ip         string
		attributed sql.NullInt64
	)
	require.NoError(t, db.QueryRow(`SELECT ip_address, discovered_by_fleet_node_id FROM discovered_device WHERE id = $1`, ddID).Scan(&ip, &attributed))
	assert.Equal(t, "10.0.0.60", ip)
	assert.False(t, attributed.Valid)
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

func TestRevokeClearsPairings_PreservesAttribution(t *testing.T) {
	// Arrange
	ctx := t.Context()
	db, orgID, pairing, enrollment := setupPairingTest(t)
	nodeID := createFleetNode(t, enrollment, orgID, "node-to-revoke")
	identifier := "mac:p-revoke-shared"
	upsertNodeDiscovered(t, pairing, orgID, nodeID, identifier)
	assignedBy := int64(1)
	status, err := pairing.PersistFleetNodePairResult(ctx, pairPersistMeta(t, db, nodeID, orgID, &assignedBy, identifier), pairResult(identifier, gatewaypb.PairOutcome_PAIR_OUTCOME_PAIRED))
	require.NoError(t, err)
	require.Equal(t, fleetnodepairing.StatusPaired, status)
	siblingNodeID := createFleetNode(t, enrollment, orgID, "unaffected-node")
	siblingIdentifier := "mac:p-revoke-sibling"
	upsertNodeDiscovered(t, pairing, orgID, siblingNodeID, siblingIdentifier)
	siblingStatus, err := pairing.PersistFleetNodePairResult(ctx, pairPersistMeta(t, db, siblingNodeID, orgID, &assignedBy, siblingIdentifier), pairResult(siblingIdentifier, gatewaypb.PairOutcome_PAIR_OUTCOME_PAIRED))
	require.NoError(t, err)
	require.Equal(t, fleetnodepairing.StatusPaired, siblingStatus)
	var ddID int64
	require.NoError(t, db.QueryRow(`SELECT id FROM discovered_device WHERE device_identifier = $1 AND org_id = $2`, identifier, orgID).Scan(&ddID))
	devID := deviceIDByIdentifier(t, db, orgID, identifier)
	_, err = db.Exec(
		`INSERT INTO miner_credentials (device_id, username_enc, password_enc)
		 VALUES ($1, 'node-owned-username', 'node-owned-password')`,
		devID,
	)
	require.NoError(t, err)

	// Act
	require.NoError(t, enrollment.RevokeFleetNode(ctx, nodeID, orgID))

	// Assert: pairings deleted but origin attribution stays so cloud pairing
	// keeps refusing to dial the agent-reported IP.
	var pairings int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM fleet_node_device WHERE fleet_node_id = $1`, nodeID).Scan(&pairings))
	assert.Equal(t, 0, pairings, "revoke must delete fleet_node_device rows")
	var credentials int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM miner_credentials WHERE device_id = $1`, devID).Scan(&credentials))
	assert.Equal(t, 0, credentials, "revoke must delete node-owned miner credentials")
	var pairingStatus, deviceStatus string
	require.NoError(t, db.QueryRow(`SELECT pairing_status FROM device_pairing WHERE device_id = $1`, devID).Scan(&pairingStatus))
	require.NoError(t, db.QueryRow(`SELECT status FROM device_status WHERE device_id = $1`, devID).Scan(&deviceStatus))
	assert.Equal(t, "UNPAIRED", pairingStatus)
	assert.Equal(t, "OFFLINE", deviceStatus)
	assert.Equal(t, "PAIRED", devicePairingStatus(t, db, orgID, siblingIdentifier))
	assert.True(t, deviceBoundToNode(t, db, orgID, siblingNodeID, siblingIdentifier))
	var siblingDeviceStatus string
	siblingDeviceID := deviceIDByIdentifier(t, db, orgID, siblingIdentifier)
	require.NoError(t, db.QueryRow(`SELECT status FROM device_status WHERE device_id = $1`, siblingDeviceID).Scan(&siblingDeviceStatus))
	assert.Equal(t, "ACTIVE", siblingDeviceStatus)
	var statusDetails sql.NullString
	require.NoError(t, db.QueryRow(`SELECT status_details FROM device_status WHERE device_id = $1`, devID).Scan(&statusDetails))
	assert.Equal(t, "fleet_node_revoked", statusDetails.String)
	var attributed sql.NullInt64
	require.NoError(t, db.QueryRow(`SELECT discovered_by_fleet_node_id FROM discovered_device WHERE id = $1`, ddID).Scan(&attributed))
	require.True(t, attributed.Valid, "revoke must NOT clear discovered_by_fleet_node_id (immutable origin)")
	assert.Equal(t, nodeID, attributed.Int64)

	// A queued telemetry result from the revoked Node must not restore Hashing.
	deviceStore := sqlstores.NewSQLDeviceStore(db)
	require.NoError(t, deviceStore.UpsertDeviceStatuses(ctx, []interfaces.DeviceStatusUpdate{{
		DeviceIdentifier: telemetrymodels.DeviceIdentifier(identifier), Status: minermodels.MinerStatusActive,
	}}))
	require.NoError(t, db.QueryRow(`SELECT status FROM device_status WHERE device_id = $1`, devID).Scan(&deviceStatus))
	assert.Equal(t, "OFFLINE", deviceStatus)

	// Direct binding must not claim a revoked miner before provisioning has
	// restored its pairing and status.
	replacementID := createFleetNode(t, enrollment, orgID, "replacement-node")
	err = pairing.PairDevice(ctx, replacementID, devID, orgID, &assignedBy)
	require.Error(t, err)
	assert.True(t, fleeterror.IsFailedPreconditionError(err))
	assert.ErrorContains(t, err, "discover and pair")
	assert.False(t, deviceBoundToNode(t, db, orgID, replacementID, identifier))
	eligible, err := deviceStore.GetAllPairedDeviceIdentifiers(ctx)
	require.NoError(t, err)
	assert.NotContains(t, eligible, telemetrymodels.DeviceIdentifier(identifier))

	// The replacement Node can still rediscover and pair the same miner.
	upsertNodeDiscovered(t, pairing, orgID, replacementID, identifier)
	// Rediscovery changes attribution but must not re-enable stale status writes.
	require.NoError(t, deviceStore.UpsertDeviceStatus(ctx, telemetrymodels.DeviceIdentifier(identifier), minermodels.MinerStatusActive, ""))
	require.NoError(t, deviceStore.UpsertDeviceStatuses(ctx, []interfaces.DeviceStatusUpdate{{
		DeviceIdentifier: telemetrymodels.DeviceIdentifier(identifier), Status: minermodels.MinerStatusActive,
	}}))
	require.NoError(t, db.QueryRow(`SELECT status FROM device_status WHERE device_id = $1`, devID).Scan(&deviceStatus))
	assert.Equal(t, "OFFLINE", deviceStatus)
	status, err = pairing.PersistFleetNodePairResult(ctx, pairPersistMeta(t, db, replacementID, orgID, &assignedBy, identifier), pairResult(identifier, gatewaypb.PairOutcome_PAIR_OUTCOME_PAIRED))
	require.NoError(t, err)
	assert.Equal(t, fleetnodepairing.StatusPaired, status)
	assert.Equal(t, "PAIRED", devicePairingStatus(t, db, orgID, identifier))
	assert.True(t, deviceBoundToNode(t, db, orgID, replacementID, identifier))
	require.NoError(t, db.QueryRow(`SELECT status FROM device_status WHERE device_id = $1`, devID).Scan(&deviceStatus))
	assert.Equal(t, "ACTIVE", deviceStatus)
	require.NoError(t, db.QueryRow(`SELECT status_details FROM device_status WHERE device_id = $1`, devID).Scan(&statusDetails))
	assert.False(t, statusDetails.Valid)
	eligible, err = deviceStore.GetAllPairedDeviceIdentifiers(ctx)
	require.NoError(t, err)
	assert.Contains(t, eligible, telemetrymodels.DeviceIdentifier(identifier))
}

func TestRevocationBlocksTelemetryStartedBeforeCommit(t *testing.T) {
	ctx := t.Context()
	db, orgID, pairing, enrollment := setupPairingTest(t)
	nodeID := createFleetNode(t, enrollment, orgID, "node-revoke-racing-status")
	identifier := "mac:p-revoke-racing-status"
	upsertNodeDiscovered(t, pairing, orgID, nodeID, identifier)
	assignedBy := int64(1)
	status, err := pairing.PersistFleetNodePairResult(ctx, pairPersistMeta(t, db, nodeID, orgID, &assignedBy, identifier), pairResult(identifier, gatewaypb.PairOutcome_PAIR_OUTCOME_PAIRED))
	require.NoError(t, err)
	require.Equal(t, fleetnodepairing.StatusPaired, status)
	deviceID := deviceIDByIdentifier(t, db, orgID, identifier)

	// Hold the status row lock until the telemetry upsert has started with the
	// pre-revocation snapshot. This reproduces the ON CONFLICT wait in Postgres.
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	_, err = tx.ExecContext(ctx, `UPDATE device_status SET status = 'OFFLINE', status_details = 'fleet_node_revoked' WHERE device_id = $1`, deviceID)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `UPDATE device_pairing SET pairing_status = 'UNPAIRED' WHERE device_id = $1`, deviceID)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `DELETE FROM fleet_node_device WHERE device_id = $1`, deviceID)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `UPDATE fleet_node SET enrollment_status = 'REVOKED' WHERE id = $1 AND org_id = $2`, nodeID, orgID)
	require.NoError(t, err)

	store := sqlstores.NewSQLDeviceStore(db)
	writeDone := make(chan error, 1)
	go func() {
		writeDone <- store.UpsertDeviceStatuses(ctx, []interfaces.DeviceStatusUpdate{{
			DeviceIdentifier: telemetrymodels.DeviceIdentifier(identifier), Status: minermodels.MinerStatusActive,
		}})
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		err := db.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM pg_stat_activity
				WHERE datname = current_database()
				  AND wait_event_type = 'Lock'
				  AND query LIKE 'INSERT INTO device_status%'
			)`).Scan(&waiting)
		return err == nil && waiting
	}, 5*time.Second, 10*time.Millisecond)
	require.NoError(t, tx.Commit())
	require.NoError(t, <-writeDone)
	var finalStatus, details string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT status, status_details FROM device_status WHERE device_id = $1`, deviceID).Scan(&finalStatus, &details))
	require.Equal(t, "OFFLINE", finalStatus)
	require.Equal(t, "fleet_node_revoked", details)
}

func TestUnpair_PreservesAttribution(t *testing.T) {
	// Arrange: agent reports a device, operator pairs it, operator unpairs it.
	// The discovered_device row must remain attributed so cloud-side pairing
	// keeps refusing the agent-supplied IP.
	ctx := t.Context()
	db, orgID, pairing, enrollment := setupPairingTest(t)
	nodeID := createFleetNode(t, enrollment, orgID, "node-unpair-attr")
	_, _, err := pairing.UpsertDiscoveredDevices(ctx, nodeID, orgID, []fleetnodepairing.DiscoveredDeviceReport{
		{DeviceIdentifier: "unpair-shared", IPAddress: "10.0.0.31", Port: "80", URLScheme: "http", DriverName: "virtual"},
	})
	require.NoError(t, err)
	var ddID int64
	require.NoError(t, db.QueryRow(`SELECT id FROM discovered_device WHERE device_identifier = 'unpair-shared' AND org_id = $1`, orgID).Scan(&ddID))
	var devID int64
	require.NoError(t, db.QueryRow(`INSERT INTO device (device_identifier, mac_address, serial_number, org_id, discovered_device_id)
		VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		fmt.Sprintf("unpair-dev-%d", ddID),
		fmt.Sprintf("aa:bb:cc:ab:00:%02x", ddID%256),
		fmt.Sprintf("unpair-sn-%d", ddID),
		orgID, ddID,
	).Scan(&devID))
	require.NoError(t, pairing.PairDevice(ctx, nodeID, devID, orgID, nil))

	// Act
	require.NoError(t, pairing.UnpairDevice(ctx, devID, orgID))

	// Assert
	var attributed sql.NullInt64
	require.NoError(t, db.QueryRow(`SELECT discovered_by_fleet_node_id FROM discovered_device WHERE id = $1`, ddID).Scan(&attributed))
	require.True(t, attributed.Valid, "unpair must NOT clear discovered_by_fleet_node_id (immutable origin)")
	assert.Equal(t, nodeID, attributed.Int64)
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
	pendingID, _ := createPendingFleetNode(t, enrollment, orgID, "node-pending")
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
	assert.Len(t, accepted, 2)
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
	assert.Empty(t, accepted)
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

func TestUpsertDiscoveredDevices_PersistsSchemeUpToProtoLimit(t *testing.T) {
	// Arrange: a non-http scheme longer than the old VARCHAR(10) column but
	// within the gateway proto's advertised max_len of 32. Before the column
	// was widened this overflowed and the whole batch failed as an internal
	// error instead of persisting.
	ctx := t.Context()
	db, orgID, pairing, enrollment := setupPairingTest(t)
	fleetNodeID := createFleetNode(t, enrollment, orgID, "node-long-scheme")
	longScheme := "abcdefghij0123456789abcdefghij12" // exactly 32 chars
	require.Len(t, longScheme, 32)

	// Act
	accepted, rejected, err := pairing.UpsertDiscoveredDevices(ctx, fleetNodeID, orgID, []fleetnodepairing.DiscoveredDeviceReport{
		{DeviceIdentifier: "long-scheme-1", IPAddress: "10.0.0.1", Port: "80", URLScheme: longScheme, DriverName: "virtual"},
	})

	// Assert: accepted and persisted with the full scheme intact.
	require.NoError(t, err)
	assert.Len(t, accepted, 1)
	assert.Equal(t, int64(0), rejected)
	var stored string
	require.NoError(t, db.QueryRow(`SELECT url_scheme FROM discovered_device WHERE device_identifier = 'long-scheme-1' AND org_id = $1`, orgID).Scan(&stored))
	assert.Equal(t, longScheme, stored)
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
	assert.Empty(t, accepted)
	assert.Equal(t, int64(1), rejected, "B cannot claim a NULL-attributed row already paired to A")
	var (
		ip         string
		attributed sql.NullInt64
	)
	require.NoError(t, db.QueryRow(`SELECT ip_address, discovered_by_fleet_node_id FROM discovered_device WHERE id = $1`, ddID).Scan(&ip, &attributed))
	assert.Equal(t, "10.0.0.50", ip, "IP must not be overwritten by claim attempt")
	assert.False(t, attributed.Valid, "row must remain NULL-attributed; the upsert is a no-op so attribution does not change")
}

func TestUpsertDiscoveredDevices_RejectsClaimingCloudPairedLikeDevice(t *testing.T) {
	for _, pairingStatus := range []string{"PAIRED", "DEFAULT_PASSWORD"} {
		t.Run(pairingStatus, func(t *testing.T) {
			// Arrange: a cloud-paired-like miner — a NULL-attributed discovered_device
			// promoted to a paired-like device_pairing row but NO fleet_node_device assignment.
			ctx := t.Context()
			db, orgID, pairing, enrollment := setupPairingTest(t)
			fleetNodeID := createFleetNode(t, enrollment, orgID, "node-cloud-claim")
			identifier := "cloud-shared-" + pairingStatus
			var ddID int64
			require.NoError(t, db.QueryRow(`INSERT INTO discovered_device (org_id, device_identifier, ip_address, port, url_scheme, driver_name, is_active, discovered_by_fleet_node_id)
				VALUES ($1, $2, '10.0.0.60', '80', 'http', 'virtual', TRUE, NULL) RETURNING id`, orgID, identifier).Scan(&ddID))
			var devID int64
			require.NoError(t, db.QueryRow(`INSERT INTO device (device_identifier, mac_address, serial_number, org_id, discovered_device_id)
				VALUES ($1, $2, $3, $4, $5) RETURNING id`,
				fmt.Sprintf("cloud-dev-%d", ddID),
				fmt.Sprintf("aa:bb:cc:ee:00:%02x", ddID%256),
				fmt.Sprintf("cloud-sn-%d", ddID),
				orgID, ddID,
			).Scan(&devID))
			_, err := db.Exec(`INSERT INTO device_pairing (device_id, pairing_status, paired_at) VALUES ($1, $2, CURRENT_TIMESTAMP)`, devID, pairingStatus)
			require.NoError(t, err)

			// Act: a fleet node reports the same device_identifier with a different IP.
			accepted, rejected, err := pairing.UpsertDiscoveredDevices(ctx, fleetNodeID, orgID, []fleetnodepairing.DiscoveredDeviceReport{
				{DeviceIdentifier: identifier, IPAddress: "10.0.0.99", Port: "80", URLScheme: "http", DriverName: "virtual"},
			})

			// Assert: rejected; the cloud-managed endpoint and attribution are untouched.
			require.NoError(t, err)
			assert.Empty(t, accepted)
			assert.Equal(t, int64(1), rejected, "a fleet node must not overwrite a cloud-paired-like device's endpoint")
			var (
				ip         string
				attributed sql.NullInt64
			)
			require.NoError(t, db.QueryRow(`SELECT ip_address, discovered_by_fleet_node_id FROM discovered_device WHERE id = $1`, ddID).Scan(&ip, &attributed))
			assert.Equal(t, "10.0.0.60", ip, "cloud-paired-like endpoint must not be overwritten")
			assert.False(t, attributed.Valid, "attribution must not be claimed")
		})
	}
}

func TestUpsertDiscoveredDevices_RefreshesDevicePairedToReportingNode(t *testing.T) {
	// Arrange: a device discovered and paired to THIS fleet node (device +
	// fleet_node_device). A re-scan from the same node must still refresh it.
	ctx := t.Context()
	db, orgID, pairing, enrollment := setupPairingTest(t)
	fleetNodeID := createFleetNode(t, enrollment, orgID, "node-self-refresh")
	_, _, err := pairing.UpsertDiscoveredDevices(ctx, fleetNodeID, orgID, []fleetnodepairing.DiscoveredDeviceReport{
		{DeviceIdentifier: "self-dev", IPAddress: "10.0.0.70", Port: "80", URLScheme: "http", DriverName: "virtual"},
	})
	require.NoError(t, err)
	var ddID int64
	require.NoError(t, db.QueryRow(`SELECT id FROM discovered_device WHERE device_identifier = 'self-dev' AND org_id = $1`, orgID).Scan(&ddID))
	var devID int64
	require.NoError(t, db.QueryRow(`INSERT INTO device (device_identifier, mac_address, serial_number, org_id, discovered_device_id)
		VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		fmt.Sprintf("self-dev-%d", ddID),
		fmt.Sprintf("aa:bb:cc:dd:00:%02x", ddID%256),
		fmt.Sprintf("self-sn-%d", ddID),
		orgID, ddID,
	).Scan(&devID))
	require.NoError(t, pairing.PairDevice(ctx, fleetNodeID, devID, orgID, nil))

	// Act: the same node re-scans with a refreshed endpoint.
	accepted, rejected, err := pairing.UpsertDiscoveredDevices(ctx, fleetNodeID, orgID, []fleetnodepairing.DiscoveredDeviceReport{
		{DeviceIdentifier: "self-dev", IPAddress: "10.0.0.71", Port: "8080", URLScheme: "http", DriverName: "virtual"},
	})

	// Assert: accepted and refreshed — the reporting node owns the device.
	require.NoError(t, err)
	assert.Len(t, accepted, 1)
	assert.Equal(t, int64(0), rejected)
	var ip, port string
	require.NoError(t, db.QueryRow(`SELECT ip_address, port FROM discovered_device WHERE id = $1`, ddID).Scan(&ip, &port))
	assert.Equal(t, "10.0.0.71", ip)
	assert.Equal(t, "8080", port)
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
	assert.Len(t, accepted, 1)
}
