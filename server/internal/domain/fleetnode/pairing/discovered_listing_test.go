package pairing_test

import (
	"database/sql"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	fleetnodepairing "github.com/block/proto-fleet/server/internal/domain/fleetnode/pairing"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
)

func upsertNodeDiscovered(t *testing.T, svc *fleetnodepairing.Service, orgID, fleetNodeID int64, identifier string) {
	t.Helper()
	accepted, rejected, err := svc.UpsertDiscoveredDevices(t.Context(), fleetNodeID, orgID, []fleetnodepairing.DiscoveredDeviceReport{
		{DeviceIdentifier: identifier, IPAddress: "10.0.0.5", Port: "80", URLScheme: "http", DriverName: "virtual", Model: "M", Manufacturer: "Mf", FirmwareVersion: "v1"},
	})
	require.NoError(t, err)
	require.Equal(t, int64(0), rejected)
	require.Len(t, accepted, 1)
}

// deviceForDiscovered creates a device row linked to a fleet-node-discovered
// discovered_device, returning the device id.
func deviceForDiscovered(t *testing.T, db *sql.DB, orgID int64, ddIdentifier string) int64 {
	t.Helper()
	var ddID int64
	require.NoError(t, db.QueryRow(
		`SELECT id FROM discovered_device WHERE org_id=$1 AND device_identifier=$2 AND deleted_at IS NULL`,
		orgID, ddIdentifier,
	).Scan(&ddID))
	var devID int64
	require.NoError(t, db.QueryRow(
		`INSERT INTO device (device_identifier, mac_address, serial_number, org_id, discovered_device_id)
		 VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		fmt.Sprintf("dev-%s", ddIdentifier),
		fmt.Sprintf("aa:bb:cc:00:%02x:%02x", ddID/256%256, ddID%256),
		fmt.Sprintf("sn-%s", ddIdentifier),
		orgID, ddID,
	).Scan(&devID))
	return devID
}

func setPairingStatus(t *testing.T, db *sql.DB, devID int64, status string) {
	t.Helper()
	_, err := db.Exec(
		`INSERT INTO device_pairing (device_id, pairing_status) VALUES ($1, $2)
		 ON CONFLICT (device_id) DO UPDATE SET pairing_status = EXCLUDED.pairing_status`,
		devID, status,
	)
	require.NoError(t, err)
}

func discoveredIdentifiers(devices []fleetnodepairing.FleetNodeDiscoveredDevice) []string {
	out := make([]string, 0, len(devices))
	for _, d := range devices {
		out = append(out, d.DeviceIdentifier)
	}
	return out
}

// A node-paired device is also device_pairing=PAIRED but the node dials it, so the
// cloud-dial guard and the discovery promotion guard must treat it as node-dialed:
// not "active cloud pairing", and its own node can still refresh it, while a
// genuinely cloud-PAIRED device stays protected.
func TestCloudPairingGuards_DistinguishNodeBoundFromCloudPaired(t *testing.T) {
	// Arrange
	ctx := t.Context()
	db, orgID, pairing, enrollment := setupPairingTest(t)
	store := sqlstores.NewSQLFleetNodePairingStore(db)
	node := createFleetNode(t, enrollment, orgID, "node-guard")

	upsertNodeDiscovered(t, pairing, orgID, node, "mac:guard-bound")
	boundDev := deviceForDiscovered(t, db, orgID, "mac:guard-bound")
	require.NoError(t, pairing.PairDevice(ctx, node, boundDev, orgID, nil))
	setPairingStatus(t, db, boundDev, "PAIRED") // node-paired devices are PAIRED

	upsertNodeDiscovered(t, pairing, orgID, node, "mac:guard-cloud")
	cloudDev := deviceForDiscovered(t, db, orgID, "mac:guard-cloud")
	setPairingStatus(t, db, cloudDev, "PAIRED") // cloud-paired: no fleet_node_device
	upsertNodeDiscovered(t, pairing, orgID, node, "mac:guard-default")
	defaultDev := deviceForDiscovered(t, db, orgID, "mac:guard-default")
	setPairingStatus(t, db, defaultDev, "DEFAULT_PASSWORD") // cloud-paired-like: no fleet_node_device

	// Act: cloud-dial guard on both devices.
	boundIsCloud, err := store.DeviceHasActiveCloudPairing(ctx, boundDev, orgID)
	require.NoError(t, err)
	cloudIsCloud, err := store.DeviceHasActiveCloudPairing(ctx, cloudDev, orgID)
	require.NoError(t, err)
	defaultIsCloud, err := store.DeviceHasActiveCloudPairing(ctx, defaultDev, orgID)
	require.NoError(t, err)

	// Assert
	assert.False(t, boundIsCloud, "a node-bound PAIRED device is node-dialed, not cloud-dialed")
	assert.True(t, cloudIsCloud, "a PAIRED device with no fleet_node_device is cloud-dialed")
	assert.True(t, defaultIsCloud, "a DEFAULT_PASSWORD device with no fleet_node_device is cloud-dialed")

	// Act: promotion guard. The owning node re-scans both devices.
	accepted, rejected, err := pairing.UpsertDiscoveredDevices(ctx, node, orgID, []fleetnodepairing.DiscoveredDeviceReport{
		{DeviceIdentifier: "mac:guard-bound", IPAddress: "10.0.0.9", Port: "80", URLScheme: "http", DriverName: "virtual"},
		{DeviceIdentifier: "mac:guard-cloud", IPAddress: "10.0.0.9", Port: "80", URLScheme: "http", DriverName: "virtual"},
		{DeviceIdentifier: "mac:guard-default", IPAddress: "10.0.0.9", Port: "80", URLScheme: "http", DriverName: "virtual"},
	})
	require.NoError(t, err)

	// Assert
	assert.Equal(t, []int{0}, accepted, "own-node PAIRED device refreshes; cloud paired-like devices are rejected")
	assert.Equal(t, int64(2), rejected)
}

func TestListFleetNodeDiscoveredDevices_FiltersByNode(t *testing.T) {
	// Arrange
	ctx := t.Context()
	_, orgID, pairing, enrollment := setupPairingTest(t)
	nodeA := createFleetNode(t, enrollment, orgID, "node-A-list")
	nodeB := createFleetNode(t, enrollment, orgID, "node-B-list")
	upsertNodeDiscovered(t, pairing, orgID, nodeA, "mac:list-a1")
	upsertNodeDiscovered(t, pairing, orgID, nodeA, "mac:list-a2")
	upsertNodeDiscovered(t, pairing, orgID, nodeB, "mac:list-b1")

	// Act
	all, _, err := pairing.ListDiscoveredDevicesForFleetNode(ctx, orgID, nil, nil, nil)
	require.NoError(t, err)
	aOnly, _, err := pairing.ListDiscoveredDevicesForFleetNode(ctx, orgID, &nodeA, nil, nil)
	require.NoError(t, err)

	// Assert
	allIDs := discoveredIdentifiers(all)
	assert.Subset(t, allIDs, []string{"mac:list-a1", "mac:list-a2", "mac:list-b1"})
	aIDs := discoveredIdentifiers(aOnly)
	assert.Subset(t, aIDs, []string{"mac:list-a1", "mac:list-a2"})
	assert.NotContains(t, aIDs, "mac:list-b1")
	for _, d := range aOnly {
		assert.Equal(t, nodeA, d.FleetNodeID)
	}
}

func TestListFleetNodeDiscoveredDevices_ExcludesWhenAnyDeviceRowIsBound(t *testing.T) {
	// Arrange: one discovered device with two live device rows -- one bound to
	// the node, one not. The whole discovered device must be excluded, not
	// surfaced through its still-unbound row.
	ctx := t.Context()
	db, orgID, pairing, enrollment := setupPairingTest(t)
	node := createFleetNode(t, enrollment, orgID, "node-multi")
	upsertNodeDiscovered(t, pairing, orgID, node, "mac:multi")
	boundDev := deviceForDiscovered(t, db, orgID, "mac:multi")
	require.NoError(t, pairing.PairDevice(ctx, node, boundDev, orgID, nil))
	var ddID int64
	require.NoError(t, db.QueryRow(
		`SELECT id FROM discovered_device WHERE org_id=$1 AND device_identifier=$2 AND deleted_at IS NULL`,
		orgID, "mac:multi",
	).Scan(&ddID))
	_, err := db.Exec(
		`INSERT INTO device (device_identifier, mac_address, serial_number, org_id, discovered_device_id)
		 VALUES ($1, $2, $3, $4, $5)`,
		"dev-multi-unbound", "aa:bb:cc:00:99:99", "sn-multi-unbound", orgID, ddID,
	)
	require.NoError(t, err)

	// Act
	got, _, err := pairing.ListDiscoveredDevicesForFleetNode(ctx, orgID, &node, nil, nil)
	require.NoError(t, err)

	// Assert
	assert.NotContains(t, discoveredIdentifiers(got), "mac:multi",
		"a discovered device with any bound device row must not surface, even if another row is unbound")
}

func TestListFleetNodeDiscoveredDevices_Paginates(t *testing.T) {
	// Arrange: three devices discovered by one node.
	ctx := t.Context()
	_, orgID, pairing, enrollment := setupPairingTest(t)
	node := createFleetNode(t, enrollment, orgID, "node-paginate")
	upsertNodeDiscovered(t, pairing, orgID, node, "mac:pg-1")
	upsertNodeDiscovered(t, pairing, orgID, node, "mac:pg-2")
	upsertNodeDiscovered(t, pairing, orgID, node, "mac:pg-3")
	limit := int64(2)

	// Act: a full first page, then the remainder via the returned cursor.
	page1, next, err := pairing.ListDiscoveredDevicesForFleetNode(ctx, orgID, &node, nil, &limit)
	require.NoError(t, err)
	require.NotNil(t, next, "a full page must return a cursor")
	page2, next2, err := pairing.ListDiscoveredDevicesForFleetNode(ctx, orgID, &node, next, &limit)
	require.NoError(t, err)

	// Assert: 2 + 1 rows, no overlap, cursor exhausted on the short final page.
	assert.Len(t, page1, 2)
	assert.Len(t, page2, 1)
	assert.Nil(t, next2, "a short final page returns no cursor")
	got := append(discoveredIdentifiers(page1), discoveredIdentifiers(page2)...)
	assert.ElementsMatch(t, []string{"mac:pg-1", "mac:pg-2", "mac:pg-3"}, got)
}

func TestListFleetNodeDiscoveredDevices_ExcludesPairedIncludesAuthNeeded(t *testing.T) {
	// Arrange: three node-discovered devices -- one bound to the node, one
	// cloud-PAIRED, one AUTHENTICATION_NEEDED, plus a plain unpaired one.
	ctx := t.Context()
	db, orgID, pairing, enrollment := setupPairingTest(t)
	node := createFleetNode(t, enrollment, orgID, "node-exclude-list")
	upsertNodeDiscovered(t, pairing, orgID, node, "mac:keep")
	upsertNodeDiscovered(t, pairing, orgID, node, "mac:bound")
	upsertNodeDiscovered(t, pairing, orgID, node, "mac:cloudpaired")
	upsertNodeDiscovered(t, pairing, orgID, node, "mac:clouddefault")
	upsertNodeDiscovered(t, pairing, orgID, node, "mac:authneeded")

	boundDev := deviceForDiscovered(t, db, orgID, "mac:bound")
	require.NoError(t, pairing.PairDevice(ctx, node, boundDev, orgID, nil))

	cloudDev := deviceForDiscovered(t, db, orgID, "mac:cloudpaired")
	setPairingStatus(t, db, cloudDev, "PAIRED")
	defaultDev := deviceForDiscovered(t, db, orgID, "mac:clouddefault")
	setPairingStatus(t, db, defaultDev, "DEFAULT_PASSWORD")

	authDev := deviceForDiscovered(t, db, orgID, "mac:authneeded")
	setPairingStatus(t, db, authDev, "AUTHENTICATION_NEEDED")

	// Act
	got, _, err := pairing.ListDiscoveredDevicesForFleetNode(ctx, orgID, &node, nil, nil)
	require.NoError(t, err)

	// Assert
	ids := discoveredIdentifiers(got)
	assert.Contains(t, ids, "mac:keep")
	assert.Contains(t, ids, "mac:authneeded", "AUTHENTICATION_NEEDED rows must surface for retry")
	assert.NotContains(t, ids, "mac:bound", "a device bound to the node is already paired")
	assert.NotContains(t, ids, "mac:cloudpaired", "a cloud-PAIRED device is not offered for fleet-node pairing")
	assert.NotContains(t, ids, "mac:clouddefault", "a cloud-DEFAULT_PASSWORD device is not offered for fleet-node pairing")
	for _, d := range got {
		if d.DeviceIdentifier == "mac:authneeded" {
			assert.Equal(t, "AUTHENTICATION_NEEDED", d.PairingStatus)
		}
	}
}
