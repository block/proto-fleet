package sqlstores_test

import (
	"context"
	"database/sql"
	"net/netip"
	"testing"
	"time"

	collectionpb "github.com/block/proto-fleet/server/generated/grpc/collection/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	stores "github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMinerSearchFilter_MatchesAllIdentifierFields(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	dbSvc := testutil.NewDatabaseService(t, nil)
	db := dbSvc.DB
	deviceStore := sqlstores.NewSQLDeviceStore(db)
	ctx := t.Context()
	user := dbSvc.CreateSuperAdminUser()

	type searchFixture struct {
		query string
		id    string
		setup func(int64)
	}
	fixtures := []searchFixture{
		{query: "name-marker", setup: func(deviceID int64) { updateDeviceSearchFields(t, db, deviceID, "name-marker", "", "", "") }},
		{query: "serial-marker", setup: func(deviceID int64) { updateDeviceSearchFields(t, db, deviceID, "", "serial-marker", "", "") }},
		{query: "aa:bb:cc", setup: func(deviceID int64) { updateDeviceSearchFields(t, db, deviceID, "", "", "AA:BB:CC:DD:EE:FF", "") }},
		{query: "192.0.2.42", setup: func(deviceID int64) { setDeviceIP(t, db, deviceID, "192.0.2.42") }},
		{query: "worker-marker", setup: func(deviceID int64) { updateDeviceSearchFields(t, db, deviceID, "", "", "", "worker-marker") }},
	}

	for i := range fixtures {
		dev := dbSvc.CreateDevice(user.OrganizationID, "proto")
		fixtures[i].id = dev.ID
		fixtures[i].setup(dev.DatabaseID)
	}

	for _, fixture := range fixtures {
		rows, _, total, err := deviceStore.ListMinerStateSnapshots(ctx, user.OrganizationID, "", 100, &stores.MinerFilter{
			SearchQuery: fixture.query,
		}, nil)
		require.NoError(t, err)
		assert.Equal(t, int64(1), total, "search %q should match one miner", fixture.query)
		assert.Equal(t, []string{fixture.id}, identifiers(rows), "search %q should match its field", fixture.query)
	}
}

// TestNumericRangeFilter_HashrateGreaterThan exercises the end-to-end numeric
// filter pipeline against real Postgres/Timescale. It seeds three miners with
// different hashrates and a fourth with stale telemetry, then proves the
// filter returns only the in-window miner above the threshold.
func TestNumericRangeFilter_HashrateGreaterThan(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	dbSvc := testutil.NewDatabaseService(t, nil)
	db := dbSvc.DB
	deviceStore := sqlstores.NewSQLDeviceStore(db)
	ctx := t.Context()

	user := dbSvc.CreateSuperAdminUser()
	devAbove := dbSvc.CreateDevice(user.OrganizationID, "proto") // 95 TH/s
	devBelow := dbSvc.CreateDevice(user.OrganizationID, "proto") // 85 TH/s
	devStale := dbSvc.CreateDevice(user.OrganizationID, "proto") // 95 TH/s, 30 min ago

	now := time.Now().UTC()
	insertMetric(t, db, devAbove.ID, now, 95e12) // 95 TH/s
	insertMetric(t, db, devBelow.ID, now, 85e12) // 85 TH/s
	insertMetric(t, db, devStale.ID, now.Add(-30*time.Minute), 95e12)

	threshold := 90.0
	filter := &stores.MinerFilter{
		NumericRanges: []stores.NumericRange{
			{Field: stores.NumericFilterFieldHashrateTHs, Min: &threshold},
		},
	}

	rows, _, total, err := deviceStore.ListMinerStateSnapshots(ctx, user.OrganizationID, "", 100, filter, nil)
	require.NoError(t, err)

	ids := identifiers(rows)
	assert.Contains(t, ids, devAbove.ID, "miner with 95 TH/s in window must match")
	assert.NotContains(t, ids, devBelow.ID, "miner at 85 TH/s must be filtered out")
	assert.NotContains(t, ids, devStale.ID, "miner with stale telemetry must be excluded (matches dash rendering)")
	assert.Equal(t, int64(1), total)
}

// TestNumericRangeFilter_ExcludesOfflineDevices guards the rule that a
// numeric filter excludes OFFLINE miners even when they have fresh telemetry.
// Mirrors the client UI: getMinerMeasurement returns null for OFFLINE miners
// regardless of measurement age, rendering an em-dash.
func TestNumericRangeFilter_ExcludesOfflineDevices(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	dbSvc := testutil.NewDatabaseService(t, nil)
	db := dbSvc.DB
	deviceStore := sqlstores.NewSQLDeviceStore(db)
	ctx := t.Context()

	user := dbSvc.CreateSuperAdminUser()
	devOnline := dbSvc.CreateDevice(user.OrganizationID, "proto")
	devOffline := dbSvc.CreateDevice(user.OrganizationID, "proto")

	now := time.Now().UTC()
	insertMetric(t, db, devOnline.ID, now, 95e12)
	insertMetric(t, db, devOffline.ID, now, 95e12)
	setDeviceStatus(t, db, devOffline.DatabaseID, sqlc.DeviceStatusEnumOFFLINE)

	threshold := 90.0
	filter := &stores.MinerFilter{
		NumericRanges: []stores.NumericRange{
			{Field: stores.NumericFilterFieldHashrateTHs, Min: &threshold},
		},
	}

	rows, _, total, err := deviceStore.ListMinerStateSnapshots(ctx, user.OrganizationID, "", 100, filter, nil)
	require.NoError(t, err)

	ids := identifiers(rows)
	assert.Contains(t, ids, devOnline.ID)
	assert.NotContains(t, ids, devOffline.ID, "OFFLINE miner must be excluded under numeric filter")
	assert.Equal(t, int64(1), total)
}

func TestIPCIDRFilter_MatchesIPv4Range(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	dbSvc := testutil.NewDatabaseService(t, nil)
	db := dbSvc.DB
	deviceStore := sqlstores.NewSQLDeviceStore(db)
	ctx := t.Context()

	user := dbSvc.CreateSuperAdminUser()
	devInRange := dbSvc.CreateDevice(user.OrganizationID, "proto")
	devOutOfRange := dbSvc.CreateDevice(user.OrganizationID, "proto")
	devOtherSubnet := dbSvc.CreateDevice(user.OrganizationID, "proto")

	setDeviceIP(t, db, devInRange.DatabaseID, "192.168.1.50")
	setDeviceIP(t, db, devOutOfRange.DatabaseID, "192.168.2.50")
	setDeviceIP(t, db, devOtherSubnet.DatabaseID, "10.5.5.5")

	filter := &stores.MinerFilter{
		IPCIDRs: []netip.Prefix{netip.MustParsePrefix("192.168.1.0/24")},
	}

	rows, _, total, err := deviceStore.ListMinerStateSnapshots(ctx, user.OrganizationID, "", 100, filter, nil)
	require.NoError(t, err)

	ids := identifiers(rows)
	assert.Contains(t, ids, devInRange.ID)
	assert.NotContains(t, ids, devOutOfRange.ID)
	assert.NotContains(t, ids, devOtherSubnet.ID)
	assert.Equal(t, int64(1), total)
}

func TestIPRangeFilter_RangeOnlyFiltersRowsAndCounts(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	dbSvc := testutil.NewDatabaseService(t, nil)
	db := dbSvc.DB
	deviceStore := sqlstores.NewSQLDeviceStore(db)
	ctx := t.Context()

	user := dbSvc.CreateSuperAdminUser()
	devInRange := dbSvc.CreateDevice(user.OrganizationID, "proto")
	devOutOfRange := dbSvc.CreateDevice(user.OrganizationID, "proto")

	setDeviceIP(t, db, devInRange.DatabaseID, "192.168.1.15")
	setDeviceIP(t, db, devOutOfRange.DatabaseID, "192.168.1.50")

	now := time.Now().UTC()
	insertMetric(t, db, devInRange.ID, now, 95e12)
	insertMetric(t, db, devOutOfRange.ID, now, 95e12)
	setDeviceStatus(t, db, devInRange.DatabaseID, sqlc.DeviceStatusEnumACTIVE)
	setDeviceStatus(t, db, devOutOfRange.DatabaseID, sqlc.DeviceStatusEnumACTIVE)

	// GetMinerStateCounts only counts devices with an actionable pairing status,
	// so pair both; the range filter is then the only reason devOutOfRange is
	// excluded.
	pairDeviceForFilterTest(t, ctx, deviceStore, user.OrganizationID, devInRange.ID)
	pairDeviceForFilterTest(t, ctx, deviceStore, user.OrganizationID, devOutOfRange.ID)

	// Range-only filter (no ip_cidrs). Regression guard: the count/total paths
	// previously routed to the dynamic builder only for ip_cidrs, so a
	// range-only filter left totals and state counts computed fleet-wide while
	// the listed rows were range-filtered.
	filter := &stores.MinerFilter{
		IPRanges: []stores.IPRange{
			{Start: netip.MustParseAddr("192.168.1.10"), End: netip.MustParseAddr("192.168.1.20")},
		},
	}

	rows, _, total, err := deviceStore.ListMinerStateSnapshots(ctx, user.OrganizationID, "", 100, filter, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{devInRange.ID}, identifiers(rows))
	assert.Equal(t, int64(1), total, "total must be range-filtered, not fleet-wide")

	counts, err := deviceStore.GetMinerStateCounts(ctx, user.OrganizationID, filter)
	require.NoError(t, err)
	assert.Equal(t, int32(1), counts.HashingCount)
	assert.Equal(
		t,
		int64(1),
		int64(counts.HashingCount+counts.BrokenCount+counts.OfflineCount+counts.SleepingCount),
		"state counts must be range-filtered, not fleet-wide",
	)
}

func TestIPCIDRFilter_MultipleCIDRsActAsOR(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	dbSvc := testutil.NewDatabaseService(t, nil)
	db := dbSvc.DB
	deviceStore := sqlstores.NewSQLDeviceStore(db)
	ctx := t.Context()

	user := dbSvc.CreateSuperAdminUser()
	devSubnetA := dbSvc.CreateDevice(user.OrganizationID, "proto")
	devSubnetB := dbSvc.CreateDevice(user.OrganizationID, "proto")
	devOutside := dbSvc.CreateDevice(user.OrganizationID, "proto")

	setDeviceIP(t, db, devSubnetA.DatabaseID, "192.168.1.50")
	setDeviceIP(t, db, devSubnetB.DatabaseID, "10.0.0.50")
	setDeviceIP(t, db, devOutside.DatabaseID, "172.16.0.1")

	filter := &stores.MinerFilter{
		IPCIDRs: []netip.Prefix{
			netip.MustParsePrefix("192.168.1.0/24"),
			netip.MustParsePrefix("10.0.0.0/8"),
		},
	}

	rows, _, total, err := deviceStore.ListMinerStateSnapshots(ctx, user.OrganizationID, "", 100, filter, nil)
	require.NoError(t, err)

	ids := identifiers(rows)
	assert.Contains(t, ids, devSubnetA.ID)
	assert.Contains(t, ids, devSubnetB.ID)
	assert.NotContains(t, ids, devOutside.ID)
	assert.Equal(t, int64(2), total)
}

func TestIPCIDRFilter_IgnoresBlankStoredIPs(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	dbSvc := testutil.NewDatabaseService(t, nil)
	db := dbSvc.DB
	deviceStore := sqlstores.NewSQLDeviceStore(db)
	ctx := t.Context()

	user := dbSvc.CreateSuperAdminUser()
	devValid := dbSvc.CreateDevice(user.OrganizationID, "proto")
	devBlank := dbSvc.CreateDevice(user.OrganizationID, "proto")

	setDeviceIP(t, db, devValid.DatabaseID, "192.168.1.50")
	setDeviceIP(t, db, devBlank.DatabaseID, "")

	filter := &stores.MinerFilter{
		IPCIDRs: []netip.Prefix{netip.MustParsePrefix("192.168.1.0/24")},
	}

	rows, _, total, err := deviceStore.ListMinerStateSnapshots(ctx, user.OrganizationID, "", 100, filter, nil)
	require.NoError(t, err)

	ids := identifiers(rows)
	assert.Equal(t, []string{devValid.ID}, ids, "blank stored IPs should be ignored, not fail the query")
	assert.Equal(t, int64(1), total)
}

func TestNumericAndCIDRFilter_AndCorrectly(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	dbSvc := testutil.NewDatabaseService(t, nil)
	db := dbSvc.DB
	deviceStore := sqlstores.NewSQLDeviceStore(db)
	ctx := t.Context()

	user := dbSvc.CreateSuperAdminUser()
	devMatch := dbSvc.CreateDevice(user.OrganizationID, "proto")        // in subnet, fast
	devSlowInSubnet := dbSvc.CreateDevice(user.OrganizationID, "proto") // in subnet, slow
	devFastOutside := dbSvc.CreateDevice(user.OrganizationID, "proto")  // outside subnet, fast

	setDeviceIP(t, db, devMatch.DatabaseID, "192.168.1.10")
	setDeviceIP(t, db, devSlowInSubnet.DatabaseID, "192.168.1.20")
	setDeviceIP(t, db, devFastOutside.DatabaseID, "10.0.0.1")

	now := time.Now().UTC()
	insertMetric(t, db, devMatch.ID, now, 95e12)
	insertMetric(t, db, devSlowInSubnet.ID, now, 50e12)
	insertMetric(t, db, devFastOutside.ID, now, 95e12)

	threshold := 90.0
	filter := &stores.MinerFilter{
		NumericRanges: []stores.NumericRange{
			{Field: stores.NumericFilterFieldHashrateTHs, Min: &threshold},
		},
		IPCIDRs: []netip.Prefix{netip.MustParsePrefix("192.168.1.0/24")},
	}

	rows, _, total, err := deviceStore.ListMinerStateSnapshots(ctx, user.OrganizationID, "", 100, filter, nil)
	require.NoError(t, err)

	ids := identifiers(rows)
	assert.Equal(t, []string{devMatch.ID}, ids, "only the miner satisfying both numeric and CIDR predicates should match")
	assert.Equal(t, int64(1), total)
}

func TestGetDeviceIdentifiersByOrgWithFilter_NumericRange(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	dbSvc := testutil.NewDatabaseService(t, nil)
	db := dbSvc.DB
	deviceStore := sqlstores.NewSQLDeviceStore(db)
	ctx := t.Context()

	user := dbSvc.CreateSuperAdminUser()
	devAbove := dbSvc.CreateDevice(user.OrganizationID, "proto")
	devBelow := dbSvc.CreateDevice(user.OrganizationID, "proto")
	pairDeviceForFilterTest(t, ctx, deviceStore, user.OrganizationID, devAbove.ID)
	pairDeviceForFilterTest(t, ctx, deviceStore, user.OrganizationID, devBelow.ID)

	now := time.Now().UTC()
	insertMetric(t, db, devAbove.ID, now, 95e12)
	insertMetric(t, db, devBelow.ID, now, 85e12)

	threshold := 90.0
	filter := &stores.MinerFilter{
		NumericRanges: []stores.NumericRange{
			{Field: stores.NumericFilterFieldHashrateTHs, Min: &threshold},
		},
	}

	identifiers, err := deviceStore.GetDeviceIdentifiersByOrgWithFilter(ctx, user.OrganizationID, filter)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{devAbove.ID}, identifiers)
}

func TestCountMinersByState_NumericAndCIDRFiltersMatchList(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	dbSvc := testutil.NewDatabaseService(t, nil)
	db := dbSvc.DB
	deviceStore := sqlstores.NewSQLDeviceStore(db)
	ctx := t.Context()

	user := dbSvc.CreateSuperAdminUser()
	devHashing := dbSvc.CreateDevice(user.OrganizationID, "proto")
	devBroken := dbSvc.CreateDevice(user.OrganizationID, "proto")
	devOutsideSubnet := dbSvc.CreateDevice(user.OrganizationID, "proto")
	pairDeviceForFilterTest(t, ctx, deviceStore, user.OrganizationID, devHashing.ID)
	pairDeviceForFilterTest(t, ctx, deviceStore, user.OrganizationID, devBroken.ID)
	pairDeviceForFilterTest(t, ctx, deviceStore, user.OrganizationID, devOutsideSubnet.ID)

	setDeviceIP(t, db, devHashing.DatabaseID, "192.168.1.10")
	setDeviceIP(t, db, devBroken.DatabaseID, "192.168.1.20")
	setDeviceIP(t, db, devOutsideSubnet.DatabaseID, "10.0.0.1")

	now := time.Now().UTC()
	insertMetric(t, db, devHashing.ID, now, 95e12)
	insertMetric(t, db, devBroken.ID, now, 95e12)
	insertMetric(t, db, devOutsideSubnet.ID, now, 95e12)

	setDeviceStatus(t, db, devHashing.DatabaseID, sqlc.DeviceStatusEnumACTIVE)
	setDeviceStatus(t, db, devBroken.DatabaseID, sqlc.DeviceStatusEnumERROR)
	setDeviceStatus(t, db, devOutsideSubnet.DatabaseID, sqlc.DeviceStatusEnumACTIVE)

	threshold := 90.0
	filter := &stores.MinerFilter{
		NumericRanges: []stores.NumericRange{
			{Field: stores.NumericFilterFieldHashrateTHs, Min: &threshold},
		},
		IPCIDRs: []netip.Prefix{netip.MustParsePrefix("192.168.1.0/24")},
	}

	rows, _, total, err := deviceStore.ListMinerStateSnapshots(ctx, user.OrganizationID, "", 100, filter, nil)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{devHashing.ID, devBroken.ID}, identifiers(rows))
	assert.Equal(t, int64(2), total)

	counts, err := deviceStore.GetMinerStateCounts(ctx, user.OrganizationID, filter)
	require.NoError(t, err)
	assert.Equal(t, int32(1), counts.HashingCount)
	assert.Equal(t, int32(1), counts.BrokenCount)
	assert.Equal(t, int32(0), counts.OfflineCount)
	assert.Equal(t, int32(0), counts.SleepingCount)
	assert.Equal(t, total, int64(counts.HashingCount+counts.BrokenCount))
}

// insertMetric inserts a single device_metrics row with a given hash rate
// (raw H/s, e.g. 95e12 for 95 TH/s) at the supplied time. Other measurements
// are left NULL.
func insertMetric(t *testing.T, db *sql.DB, deviceIdentifier string, ts time.Time, hashRateHs float64) {
	t.Helper()
	q := sqlc.New(db)
	require.NoError(t, q.InsertDeviceMetricsBatch(context.Background(), sqlc.InsertDeviceMetricsBatchParams{
		Times:             []time.Time{ts},
		DeviceIdentifiers: []string{deviceIdentifier},
		HashRateHs:        []float64{hashRateHs},
		HashRateHsKinds:   []string{"metric_kind_gauge"},
		TempC:             []float64{0},
		TempCKinds:        []string{""},
		FanRpm:            []float64{0},
		FanRpmKinds:       []string{""},
		PowerW:            []float64{0},
		PowerWKinds:       []string{""},
		EfficiencyJh:      []float64{0},
		EfficiencyJhKinds: []string{""},
		Health:            []string{"health_healthy_active"},
	}))
}

func setDeviceStatus(t *testing.T, db *sql.DB, deviceDatabaseID int64, status sqlc.DeviceStatusEnum) {
	t.Helper()
	q := sqlc.New(db)
	require.NoError(t, q.UpsertDeviceStatus(context.Background(), sqlc.UpsertDeviceStatusParams{
		DeviceID:        deviceDatabaseID,
		Status:          status,
		StatusTimestamp: sql.NullTime{Time: time.Now().UTC(), Valid: true},
	}))
}

func setDeviceIP(t *testing.T, db *sql.DB, deviceDatabaseID int64, ip string) {
	t.Helper()
	_, err := db.ExecContext(context.Background(),
		`UPDATE discovered_device SET ip_address = $1
         FROM device d
         WHERE discovered_device.id = d.discovered_device_id AND d.id = $2`,
		ip, deviceDatabaseID)
	require.NoError(t, err)
}

func updateDeviceSearchFields(t *testing.T, db *sql.DB, deviceDatabaseID int64, customName, serial, mac, worker string) {
	t.Helper()
	_, err := db.ExecContext(context.Background(),
		// mac_address is NOT NULL, so an empty argument keeps the existing value
		// rather than clearing the column like the nullable fields do.
		`UPDATE device
         SET custom_name = NULLIF($1, ''), serial_number = NULLIF($2, ''),
             mac_address = COALESCE(NULLIF($3, ''), mac_address), worker_name = NULLIF($4, '')
         WHERE id = $5`,
		customName, serial, mac, worker, deviceDatabaseID)
	require.NoError(t, err)
}

func identifiers(rows []sqlc.ListMinerStateSnapshotsRow) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.DeviceIdentifier)
	}
	return out
}

func pairDeviceForFilterTest(t *testing.T, ctx context.Context, store *sqlstores.SQLDeviceStore, orgID int64, deviceIdentifier string) {
	t.Helper()
	require.NoError(t, store.UpsertDevicePairing(
		ctx,
		&pb.Device{DeviceIdentifier: deviceIdentifier},
		orgID,
		string(sqlc.PairingStatusEnumPAIRED),
	))
}

// TestZoneFilter_ExcludesSoftDeletedRack proves that soft-deleting a rack
// removes its miners from zone-filter results, even though the membership
// rows persist (soft delete only flags device_set.deleted_at).
func TestZoneFilter_ExcludesSoftDeletedRack(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	dbSvc := testutil.NewDatabaseService(t, nil)
	db := dbSvc.DB
	deviceStore := sqlstores.NewSQLDeviceStore(db)
	collectionStore := sqlstores.NewSQLCollectionStore(db)
	ctx := t.Context()

	user := dbSvc.CreateSuperAdminUser()
	dev := dbSvc.CreateDevice(user.OrganizationID, "proto")

	rack, err := collectionStore.CreateCollection(ctx, user.OrganizationID, collectionpb.CollectionType_COLLECTION_TYPE_RACK, "Doomed Rack", "")
	require.NoError(t, err)
	require.NoError(t, collectionStore.CreateRackExtension(ctx, stores.CreateRackExtensionParams{
		OrgID: user.OrganizationID, CollectionID: rack.Id, Rows: 4, Columns: 8, Zone: "doomed-zone",
	}))
	_, err = collectionStore.AddDevicesToCollection(ctx, user.OrganizationID, rack.Id, []string{dev.ID})
	require.NoError(t, err)

	filter := &stores.MinerFilter{
		ZoneKeys: []stores.ZoneKey{{BuildingID: 0, Zone: "doomed-zone"}},
	}

	// Sanity check: device shows up before deletion.
	rows, _, total, err := deviceStore.ListMinerStateSnapshots(ctx, user.OrganizationID, "", 100, filter, nil)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, int64(1), total)

	// Soft-delete the rack. Membership and rack-extension rows persist.
	_, err = collectionStore.SoftDeleteCollection(ctx, user.OrganizationID, rack.Id)
	require.NoError(t, err)

	// Filter must now return zero — and the total must agree, not just the page.
	rows, _, total, err = deviceStore.ListMinerStateSnapshots(ctx, user.OrganizationID, "", 100, filter, nil)
	require.NoError(t, err)
	assert.Empty(t, rows, "soft-deleted rack must not surface in zone filter results")
	assert.Equal(t, int64(0), total, "total count must agree with the empty page (P1 invariant)")
}

// TestSearchFilter_CountsAndModelGroupsMatchList proves the search filter routes
// consistently across all three surfaces that pair a count with the miner list:
// the list total, the dashboard state breakdown, and the bulk-action modal's
// model groups. A search that reaches the rows but not one of the counts would
// show the operator a count for the whole fleet beside the searched rows.
func TestSearchFilter_CountsAndModelGroupsMatchList(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	dbSvc := testutil.NewDatabaseService(t, nil)
	db := dbSvc.DB
	deviceStore := sqlstores.NewSQLDeviceStore(db)
	ctx := t.Context()

	user := dbSvc.CreateSuperAdminUser()
	devHashing := dbSvc.CreateDevice(user.OrganizationID, "proto")
	devBroken := dbSvc.CreateDevice(user.OrganizationID, "proto")
	devUnmatched := dbSvc.CreateDevice(user.OrganizationID, "proto")
	pairDeviceForFilterTest(t, ctx, deviceStore, user.OrganizationID, devHashing.ID)
	pairDeviceForFilterTest(t, ctx, deviceStore, user.OrganizationID, devBroken.ID)
	pairDeviceForFilterTest(t, ctx, deviceStore, user.OrganizationID, devUnmatched.ID)

	// Only the first two carry the marker the search will look for.
	updateDeviceSearchFields(t, db, devHashing.DatabaseID, "", "", "", "marked-worker-a")
	updateDeviceSearchFields(t, db, devBroken.DatabaseID, "", "", "", "marked-worker-b")
	updateDeviceSearchFields(t, db, devUnmatched.DatabaseID, "", "", "", "other-worker")

	setDeviceStatus(t, db, devHashing.DatabaseID, sqlc.DeviceStatusEnumACTIVE)
	setDeviceStatus(t, db, devBroken.DatabaseID, sqlc.DeviceStatusEnumERROR)
	setDeviceStatus(t, db, devUnmatched.DatabaseID, sqlc.DeviceStatusEnumACTIVE)

	now := time.Now().UTC()
	insertMetric(t, db, devHashing.ID, now, 95e12)
	insertMetric(t, db, devBroken.ID, now, 95e12)
	insertMetric(t, db, devUnmatched.ID, now, 95e12)

	filter := &stores.MinerFilter{SearchQuery: "marked-worker"}

	rows, _, total, err := deviceStore.ListMinerStateSnapshots(ctx, user.OrganizationID, "", 100, filter, nil)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{devHashing.ID, devBroken.ID}, identifiers(rows))
	assert.Equal(t, int64(2), total, "list total must exclude the unmatched miner")

	counts, err := deviceStore.GetMinerStateCounts(ctx, user.OrganizationID, filter)
	require.NoError(t, err)
	assert.Equal(t, int32(1), counts.HashingCount)
	assert.Equal(t, int32(1), counts.BrokenCount)
	assert.Equal(t, total, int64(counts.HashingCount+counts.BrokenCount+counts.OfflineCount+counts.SleepingCount),
		"state breakdown must sum to the list total")

	groups, err := deviceStore.GetMinerModelGroups(ctx, user.OrganizationID, filter)
	require.NoError(t, err)
	var grouped int32
	for _, g := range groups {
		grouped += g.Count
	}
	assert.Equal(t, total, int64(grouped), "model group counts must sum to the list total")
}
