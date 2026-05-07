package sqlstores_test

import (
	"context"
	"database/sql"
	"net/netip"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	collectionpb "github.com/block/proto-fleet/server/generated/grpc/collection/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/miner/models"
	stores "github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
)

// pairDevice marks a device PAIRED so it shows up in GetMinerModelGroups,
// which scopes to PAIRED rows only.
func pairDevice(t *testing.T, ctx context.Context, store *sqlstores.SQLDeviceStore, orgID int64, deviceIdentifier string) {
	t.Helper()
	require.NoError(t, store.UpsertDevicePairing(
		ctx,
		&pb.Device{DeviceIdentifier: deviceIdentifier},
		orgID,
		string(sqlc.PairingStatusEnumPAIRED),
	))
}

// assertModalInvariant fetches model groups and the filtered list total under
// the same filter, then asserts the bulk-password modal's load-bearing
// invariant: Σ groups[i].count == ListMinerStateSnapshots(filter).total.
// The modal cannot show a different fleet-size than the table behind it.
func assertModalInvariant(t *testing.T, ctx context.Context, store *sqlstores.SQLDeviceStore, orgID int64, filter *stores.MinerFilter) {
	t.Helper()
	groups, err := store.GetMinerModelGroups(ctx, orgID, filter)
	require.NoError(t, err)
	_, _, listTotal, err := store.ListMinerStateSnapshots(ctx, orgID, "", 100, filter, nil)
	require.NoError(t, err)
	var groupTotal int32
	for _, g := range groups {
		groupTotal += g.Count
	}
	assert.Equal(t, listTotal, int64(groupTotal),
		"bulk-password modal invariant: model-group counts must match filtered list total")
}

// setDiscoveredDeviceModel patches discovered_device.model directly because
// CreateDevice hardcodes model="TestMiner" and exposes no override.
func setDiscoveredDeviceModel(t *testing.T, db *sql.DB, deviceIdentifier, model string) {
	t.Helper()
	res, err := db.Exec(`
		UPDATE discovered_device
		SET model = $1
		WHERE id = (SELECT discovered_device_id FROM device WHERE device_identifier = $2)
	`, model, deviceIdentifier)
	require.NoError(t, err)
	rows, err := res.RowsAffected()
	require.NoError(t, err)
	require.Equal(t, int64(1), rows)
}

// markDiscoveredDeviceInactive flips dd.is_active = FALSE on the device's
// discovered_device row. Used to verify that GetMinerModelGroups excludes
// inactive rows the same way the list/count queries do.
func markDiscoveredDeviceInactive(t *testing.T, db *sql.DB, deviceIdentifier string) {
	t.Helper()
	res, err := db.Exec(`
		UPDATE discovered_device
		SET is_active = FALSE
		WHERE id = (SELECT discovered_device_id FROM device WHERE device_identifier = $1)
	`, deviceIdentifier)
	require.NoError(t, err)
	rows, err := res.RowsAffected()
	require.NoError(t, err)
	require.Equal(t, int64(1), rows)
}

// softDeleteDiscoveredDevice sets dd.deleted_at on the device's
// discovered_device row.
func softDeleteDiscoveredDevice(t *testing.T, db *sql.DB, deviceIdentifier string) {
	t.Helper()
	res, err := db.Exec(`
		UPDATE discovered_device
		SET deleted_at = NOW()
		WHERE id = (SELECT discovered_device_id FROM device WHERE device_identifier = $1)
	`, deviceIdentifier)
	require.NoError(t, err)
	rows, err := res.RowsAffected()
	require.NoError(t, err)
	require.Equal(t, int64(1), rows)
}

// TestGetMinerModelGroups_FirmwareFilter proves the firmware filter narrows the
// model-group counts so the bulk-password modal agrees with the filtered list.
func TestGetMinerModelGroups_FirmwareFilter(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	tc := testutil.InitializeDBServiceInfrastructure(t)
	dbSvc := tc.DatabaseService
	db := tc.ServiceProvider.DB
	deviceStore := sqlstores.NewSQLDeviceStore(db)
	ctx := t.Context()

	user := dbSvc.CreateSuperAdminUser()

	d1 := dbSvc.CreateDevice(user.OrganizationID, "proto")
	d2 := dbSvc.CreateDevice(user.OrganizationID, "proto")
	d3 := dbSvc.CreateDevice(user.OrganizationID, "proto")
	for _, d := range []string{d1.ID, d2.ID, d3.ID} {
		pairDevice(t, ctx, deviceStore, user.OrganizationID, d)
	}

	err := deviceStore.UpdateFirmwareVersion(ctx, models.DeviceIdentifier(d1.ID), "v3.5.1")
	require.NoError(t, err)
	err = deviceStore.UpdateFirmwareVersion(ctx, models.DeviceIdentifier(d2.ID), "v3.5.1")
	require.NoError(t, err)
	err = deviceStore.UpdateFirmwareVersion(ctx, models.DeviceIdentifier(d3.ID), "v3.5.2")
	require.NoError(t, err)

	// No filter: all three paired devices roll up into a single TestMiner group.
	groups, err := deviceStore.GetMinerModelGroups(ctx, user.OrganizationID, nil)
	require.NoError(t, err)
	require.Len(t, groups, 1)
	assert.Equal(t, int32(3), groups[0].Count)

	// Firmware filter narrows to the matching subset.
	singleFirmware := &stores.MinerFilter{FirmwareVersions: []string{"v3.5.1"}}
	groups, err = deviceStore.GetMinerModelGroups(ctx, user.OrganizationID, singleFirmware)
	require.NoError(t, err)
	require.Len(t, groups, 1)
	assert.Equal(t, int32(2), groups[0].Count, "firmware=v3.5.1 should narrow count to the two matching devices")
	assertModalInvariant(t, ctx, deviceStore, user.OrganizationID, singleFirmware)

	// Multi-value firmware filter unions the matches.
	multiFirmware := &stores.MinerFilter{FirmwareVersions: []string{"v3.5.1", "v3.5.2"}}
	groups, err = deviceStore.GetMinerModelGroups(ctx, user.OrganizationID, multiFirmware)
	require.NoError(t, err)
	require.Len(t, groups, 1)
	assert.Equal(t, int32(3), groups[0].Count)
	assertModalInvariant(t, ctx, deviceStore, user.OrganizationID, multiFirmware)

	// No matches: filter returns no groups.
	emptyFirmware := &stores.MinerFilter{FirmwareVersions: []string{"v9.9.9"}}
	groups, err = deviceStore.GetMinerModelGroups(ctx, user.OrganizationID, emptyFirmware)
	require.NoError(t, err)
	assert.Empty(t, groups)
	assertModalInvariant(t, ctx, deviceStore, user.OrganizationID, emptyFirmware)
}

// TestGetMinerModelGroups_ZoneFilter proves the zone filter narrows the
// model-group counts and excludes miners not in any rack.
func TestGetMinerModelGroups_ZoneFilter(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	tc := testutil.InitializeDBServiceInfrastructure(t)
	dbSvc := tc.DatabaseService
	db := tc.ServiceProvider.DB
	deviceStore := sqlstores.NewSQLDeviceStore(db)
	collectionStore := sqlstores.NewSQLCollectionStore(db)
	ctx := t.Context()

	user := dbSvc.CreateSuperAdminUser()

	inZoneA := dbSvc.CreateDevice(user.OrganizationID, "proto")
	inZoneB := dbSvc.CreateDevice(user.OrganizationID, "proto")
	noZone := dbSvc.CreateDevice(user.OrganizationID, "proto")
	for _, d := range []string{inZoneA.ID, inZoneB.ID, noZone.ID} {
		pairDevice(t, ctx, deviceStore, user.OrganizationID, d)
	}

	rackA, err := collectionStore.CreateCollection(ctx, user.OrganizationID, collectionpb.CollectionType_COLLECTION_TYPE_RACK, "Rack A", "")
	require.NoError(t, err)
	require.NoError(t, collectionStore.CreateRackExtension(ctx, rackA.Id, "zone-a", 4, 8, 0, 0, user.OrganizationID))
	_, err = collectionStore.AddDevicesToCollection(ctx, user.OrganizationID, rackA.Id, []string{inZoneA.ID})
	require.NoError(t, err)

	rackB, err := collectionStore.CreateCollection(ctx, user.OrganizationID, collectionpb.CollectionType_COLLECTION_TYPE_RACK, "Rack B", "")
	require.NoError(t, err)
	require.NoError(t, collectionStore.CreateRackExtension(ctx, rackB.Id, "zone-b", 4, 8, 0, 0, user.OrganizationID))
	_, err = collectionStore.AddDevicesToCollection(ctx, user.OrganizationID, rackB.Id, []string{inZoneB.ID})
	require.NoError(t, err)

	singleZone := &stores.MinerFilter{Zones: []string{"zone-a"}}
	groups, err := deviceStore.GetMinerModelGroups(ctx, user.OrganizationID, singleZone)
	require.NoError(t, err)
	require.Len(t, groups, 1)
	assert.Equal(t, int32(1), groups[0].Count, "zone=zone-a should match only the device in rack A")
	assertModalInvariant(t, ctx, deviceStore, user.OrganizationID, singleZone)

	multiZone := &stores.MinerFilter{Zones: []string{"zone-a", "zone-b"}}
	groups, err = deviceStore.GetMinerModelGroups(ctx, user.OrganizationID, multiZone)
	require.NoError(t, err)
	require.Len(t, groups, 1)
	assert.Equal(t, int32(2), groups[0].Count, "noZone is unassigned, so zone filter must exclude it")
	assertModalInvariant(t, ctx, deviceStore, user.OrganizationID, multiZone)

	missingZone := &stores.MinerFilter{Zones: []string{"missing-zone"}}
	groups, err = deviceStore.GetMinerModelGroups(ctx, user.OrganizationID, missingZone)
	require.NoError(t, err)
	assert.Empty(t, groups)
	assertModalInvariant(t, ctx, deviceStore, user.OrganizationID, missingZone)
}

// TestGetMinerModelGroups_ZoneWithComma guards the comma-in-value case. Zones
// allow free-form names like "Austin, Building 1"; an earlier CSV transport
// would have split that into two zones and produced zero matches.
func TestGetMinerModelGroups_ZoneWithComma(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	tc := testutil.InitializeDBServiceInfrastructure(t)
	dbSvc := tc.DatabaseService
	db := tc.ServiceProvider.DB
	deviceStore := sqlstores.NewSQLDeviceStore(db)
	collectionStore := sqlstores.NewSQLCollectionStore(db)
	ctx := t.Context()

	user := dbSvc.CreateSuperAdminUser()
	dev := dbSvc.CreateDevice(user.OrganizationID, "proto")
	pairDevice(t, ctx, deviceStore, user.OrganizationID, dev.ID)

	const zone = "Austin, Building 1"
	rack, err := collectionStore.CreateCollection(ctx, user.OrganizationID, collectionpb.CollectionType_COLLECTION_TYPE_RACK, "Rack", "")
	require.NoError(t, err)
	require.NoError(t, collectionStore.CreateRackExtension(ctx, rack.Id, zone, 4, 8, 0, 0, user.OrganizationID))
	_, err = collectionStore.AddDevicesToCollection(ctx, user.OrganizationID, rack.Id, []string{dev.ID})
	require.NoError(t, err)

	commaFilter := &stores.MinerFilter{Zones: []string{zone}}
	groups, err := deviceStore.GetMinerModelGroups(ctx, user.OrganizationID, commaFilter)
	require.NoError(t, err)
	require.Len(t, groups, 1)
	assert.Equal(t, int32(1), groups[0].Count)
	assertModalInvariant(t, ctx, deviceStore, user.OrganizationID, commaFilter)
}

// TestGetMinerModelGroups_FirmwareAndZoneFilters proves both filters compose
// (AND semantics) and that group counts agree with the filtered list — the
// invariant the bulk-password modal depends on.
func TestGetMinerModelGroups_FirmwareAndZoneFilters(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	tc := testutil.InitializeDBServiceInfrastructure(t)
	dbSvc := tc.DatabaseService
	db := tc.ServiceProvider.DB
	deviceStore := sqlstores.NewSQLDeviceStore(db)
	collectionStore := sqlstores.NewSQLCollectionStore(db)
	ctx := t.Context()

	user := dbSvc.CreateSuperAdminUser()

	// Two model groups so we can verify firmware + zone narrow counts WITHIN
	// each group, not just collapse them into one.
	s21a := dbSvc.CreateDevice(user.OrganizationID, "proto")
	s21b := dbSvc.CreateDevice(user.OrganizationID, "proto")
	m60a := dbSvc.CreateDevice(user.OrganizationID, "proto")
	m60b := dbSvc.CreateDevice(user.OrganizationID, "proto")
	for _, d := range []string{s21a.ID, s21b.ID, m60a.ID, m60b.ID} {
		pairDevice(t, ctx, deviceStore, user.OrganizationID, d)
	}

	setDiscoveredDeviceModel(t, db, s21a.ID, "S21 XP")
	setDiscoveredDeviceModel(t, db, s21b.ID, "S21 XP")
	setDiscoveredDeviceModel(t, db, m60a.ID, "M60")
	setDiscoveredDeviceModel(t, db, m60b.ID, "M60")

	// Firmware: s21a + m60a on v1, s21b + m60b on v2.
	err := deviceStore.UpdateFirmwareVersion(ctx, models.DeviceIdentifier(s21a.ID), "v1")
	require.NoError(t, err)
	err = deviceStore.UpdateFirmwareVersion(ctx, models.DeviceIdentifier(m60a.ID), "v1")
	require.NoError(t, err)
	err = deviceStore.UpdateFirmwareVersion(ctx, models.DeviceIdentifier(s21b.ID), "v2")
	require.NoError(t, err)
	err = deviceStore.UpdateFirmwareVersion(ctx, models.DeviceIdentifier(m60b.ID), "v2")
	require.NoError(t, err)

	// Zones: s21a + s21b in zone-a, m60a + m60b in zone-b.
	rackA, err := collectionStore.CreateCollection(ctx, user.OrganizationID, collectionpb.CollectionType_COLLECTION_TYPE_RACK, "Rack A", "")
	require.NoError(t, err)
	require.NoError(t, collectionStore.CreateRackExtension(ctx, rackA.Id, "zone-a", 4, 8, 0, 0, user.OrganizationID))
	_, err = collectionStore.AddDevicesToCollection(ctx, user.OrganizationID, rackA.Id, []string{s21a.ID, s21b.ID})
	require.NoError(t, err)

	rackB, err := collectionStore.CreateCollection(ctx, user.OrganizationID, collectionpb.CollectionType_COLLECTION_TYPE_RACK, "Rack B", "")
	require.NoError(t, err)
	require.NoError(t, collectionStore.CreateRackExtension(ctx, rackB.Id, "zone-b", 4, 8, 0, 0, user.OrganizationID))
	_, err = collectionStore.AddDevicesToCollection(ctx, user.OrganizationID, rackB.Id, []string{m60a.ID, m60b.ID})
	require.NoError(t, err)

	// Firmware=v1 + Zone=zone-a → only s21a (S21 XP, count=1); M60 drops out.
	filter := &stores.MinerFilter{
		FirmwareVersions: []string{"v1"},
		Zones:            []string{"zone-a"},
	}
	groups, err := deviceStore.GetMinerModelGroups(ctx, user.OrganizationID, filter)
	require.NoError(t, err)
	require.Len(t, groups, 1)
	assert.Equal(t, "S21 XP", groups[0].Model)
	assert.Equal(t, int32(1), groups[0].Count)
	assertModalInvariant(t, ctx, deviceStore, user.OrganizationID, filter)
}

// TestGetMinerModelGroups_ExcludesInactiveAndSoftDeletedDiscoveredDevices
// guards the modal invariant against discovered_device state drift. The
// list/count queries already exclude rows where dd.is_active = FALSE or
// dd.deleted_at IS NOT NULL; without matching predicates here, model-group
// counts would silently include miners the table has hidden.
func TestGetMinerModelGroups_ExcludesInactiveAndSoftDeletedDiscoveredDevices(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	tc := testutil.InitializeDBServiceInfrastructure(t)
	dbSvc := tc.DatabaseService
	db := tc.ServiceProvider.DB
	deviceStore := sqlstores.NewSQLDeviceStore(db)
	ctx := t.Context()

	user := dbSvc.CreateSuperAdminUser()

	healthy := dbSvc.CreateDevice(user.OrganizationID, "proto")
	inactive := dbSvc.CreateDevice(user.OrganizationID, "proto")
	deleted := dbSvc.CreateDevice(user.OrganizationID, "proto")
	for _, d := range []string{healthy.ID, inactive.ID, deleted.ID} {
		pairDevice(t, ctx, deviceStore, user.OrganizationID, d)
	}

	// Sanity: all three count before any are removed from scope.
	groups, err := deviceStore.GetMinerModelGroups(ctx, user.OrganizationID, nil)
	require.NoError(t, err)
	require.Len(t, groups, 1)
	assert.Equal(t, int32(3), groups[0].Count)
	assertModalInvariant(t, ctx, deviceStore, user.OrganizationID, nil)

	markDiscoveredDeviceInactive(t, db, inactive.ID)
	softDeleteDiscoveredDevice(t, db, deleted.ID)

	// Only the healthy device should count now.
	groups, err = deviceStore.GetMinerModelGroups(ctx, user.OrganizationID, nil)
	require.NoError(t, err)
	require.Len(t, groups, 1)
	assert.Equal(t, int32(1), groups[0].Count,
		"inactive and soft-deleted discovered_device rows must be excluded")
	assertModalInvariant(t, ctx, deviceStore, user.OrganizationID, nil)
}

// TestGetMinerModelGroups_NumericRangeFilter proves the model-group counts
// honor numeric range predicates, holding the modal invariant against the
// filtered list. Without dynamic plumbing, the static sqlc query would return
// every paired device's model regardless of telemetry.
func TestGetMinerModelGroups_NumericRangeFilter(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	tc := testutil.InitializeDBServiceInfrastructure(t)
	dbSvc := tc.DatabaseService
	db := tc.ServiceProvider.DB
	deviceStore := sqlstores.NewSQLDeviceStore(db)
	ctx := t.Context()

	user := dbSvc.CreateSuperAdminUser()

	above := dbSvc.CreateDevice(user.OrganizationID, "proto") // 95 TH/s
	below := dbSvc.CreateDevice(user.OrganizationID, "proto") // 85 TH/s
	stale := dbSvc.CreateDevice(user.OrganizationID, "proto") // 95 TH/s but 30 min ago
	for _, d := range []string{above.ID, below.ID, stale.ID} {
		pairDevice(t, ctx, deviceStore, user.OrganizationID, d)
	}

	now := time.Now().UTC()
	insertMetric(t, db, above.ID, now, 95e12)
	insertMetric(t, db, below.ID, now, 85e12)
	insertMetric(t, db, stale.ID, now.Add(-30*time.Minute), 95e12)

	threshold := 90.0
	filter := &stores.MinerFilter{
		NumericRanges: []stores.NumericRange{
			{Field: stores.NumericFilterFieldHashrateTHs, Min: &threshold},
		},
	}

	groups, err := deviceStore.GetMinerModelGroups(ctx, user.OrganizationID, filter)
	require.NoError(t, err)
	require.Len(t, groups, 1, "all three miners share the TestMiner model — single group expected")
	assert.Equal(t, int32(1), groups[0].Count, "only the in-window miner above 90 TH/s should count")
	assertModalInvariant(t, ctx, deviceStore, user.OrganizationID, filter)
}

// TestGetMinerModelGroups_IPCIDRFilter mirrors the numeric test for the IP
// CIDR predicate, again holding the modal invariant.
func TestGetMinerModelGroups_IPCIDRFilter(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	tc := testutil.InitializeDBServiceInfrastructure(t)
	dbSvc := tc.DatabaseService
	db := tc.ServiceProvider.DB
	deviceStore := sqlstores.NewSQLDeviceStore(db)
	ctx := t.Context()

	user := dbSvc.CreateSuperAdminUser()

	inRange := dbSvc.CreateDevice(user.OrganizationID, "proto")
	outOfRange := dbSvc.CreateDevice(user.OrganizationID, "proto")
	for _, d := range []string{inRange.ID, outOfRange.ID} {
		pairDevice(t, ctx, deviceStore, user.OrganizationID, d)
	}

	setDeviceIP(t, db, inRange.DatabaseID, "192.168.1.50")
	setDeviceIP(t, db, outOfRange.DatabaseID, "10.0.0.5")

	filter := &stores.MinerFilter{
		IPCIDRs: []netip.Prefix{netip.MustParsePrefix("192.168.1.0/24")},
	}

	groups, err := deviceStore.GetMinerModelGroups(ctx, user.OrganizationID, filter)
	require.NoError(t, err)
	require.Len(t, groups, 1)
	assert.Equal(t, int32(1), groups[0].Count, "only the miner inside the CIDR should count")
	assertModalInvariant(t, ctx, deviceStore, user.OrganizationID, filter)
}

// TestGetMinerModelGroups_NumericPlusCIDR proves AND semantics across the two
// new filter kinds inside GetMinerModelGroups, matching the list query.
func TestGetMinerModelGroups_NumericPlusCIDR(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	tc := testutil.InitializeDBServiceInfrastructure(t)
	dbSvc := tc.DatabaseService
	db := tc.ServiceProvider.DB
	deviceStore := sqlstores.NewSQLDeviceStore(db)
	ctx := t.Context()

	user := dbSvc.CreateSuperAdminUser()

	match := dbSvc.CreateDevice(user.OrganizationID, "proto")        // in subnet, fast
	slowInSubnet := dbSvc.CreateDevice(user.OrganizationID, "proto") // in subnet, slow
	fastOutside := dbSvc.CreateDevice(user.OrganizationID, "proto")  // outside subnet, fast
	for _, d := range []string{match.ID, slowInSubnet.ID, fastOutside.ID} {
		pairDevice(t, ctx, deviceStore, user.OrganizationID, d)
	}

	setDeviceIP(t, db, match.DatabaseID, "192.168.1.10")
	setDeviceIP(t, db, slowInSubnet.DatabaseID, "192.168.1.20")
	setDeviceIP(t, db, fastOutside.DatabaseID, "10.0.0.1")

	now := time.Now().UTC()
	insertMetric(t, db, match.ID, now, 95e12)
	insertMetric(t, db, slowInSubnet.ID, now, 50e12)
	insertMetric(t, db, fastOutside.ID, now, 95e12)

	threshold := 90.0
	filter := &stores.MinerFilter{
		NumericRanges: []stores.NumericRange{
			{Field: stores.NumericFilterFieldHashrateTHs, Min: &threshold},
		},
		IPCIDRs: []netip.Prefix{netip.MustParsePrefix("192.168.1.0/24")},
	}

	groups, err := deviceStore.GetMinerModelGroups(ctx, user.OrganizationID, filter)
	require.NoError(t, err)
	require.Len(t, groups, 1)
	assert.Equal(t, int32(1), groups[0].Count, "only the miner satisfying both predicates should count")
	assertModalInvariant(t, ctx, deviceStore, user.OrganizationID, filter)
}
