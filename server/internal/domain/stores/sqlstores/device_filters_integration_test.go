package sqlstores_test

import (
	"context"
	"database/sql"
	"net/netip"
	"testing"
	"time"

	collectionpb "github.com/block/proto-fleet/server/generated/grpc/collection/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	stores "github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestZoneFilter_CrossOrgIsolation proves that two orgs sharing the same zone
// label cannot see each other's miners through the zone filter. The filter's
// safety boundary is `device_set_membership.org_id = $orgID` in the EXISTS
// subquery; this test asserts that boundary.
func TestZoneFilter_CrossOrgIsolation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	dbSvc := testContext.DatabaseService
	db := testContext.ServiceProvider.DB
	deviceStore := sqlstores.NewSQLDeviceStore(db)
	collectionStore := sqlstores.NewSQLCollectionStore(db)
	ctx := t.Context()

	// Two orgs that happen to use the same zone label "shared-zone".
	userA := dbSvc.CreateSuperAdminUser()
	userB := dbSvc.CreateSuperAdminUser2()

	devA := dbSvc.CreateDevice(userA.OrganizationID, "proto")
	devB := dbSvc.CreateDevice(userB.OrganizationID, "proto")

	const sharedZone = "shared-zone"

	// Create a rack collection per org with the same zone label, then add the
	// org's device to its own rack.
	rackA, err := collectionStore.CreateCollection(ctx, userA.OrganizationID, collectionpb.CollectionType_COLLECTION_TYPE_RACK, "Rack A", "")
	require.NoError(t, err)
	require.NoError(t, collectionStore.CreateRackExtension(ctx, rackA.Id, sharedZone, 4, 8, 0, 0, userA.OrganizationID))
	_, err = collectionStore.AddDevicesToCollection(ctx, userA.OrganizationID, rackA.Id, []string{devA.ID})
	require.NoError(t, err)

	rackB, err := collectionStore.CreateCollection(ctx, userB.OrganizationID, collectionpb.CollectionType_COLLECTION_TYPE_RACK, "Rack B", "")
	require.NoError(t, err)
	require.NoError(t, collectionStore.CreateRackExtension(ctx, rackB.Id, sharedZone, 4, 8, 0, 0, userB.OrganizationID))
	_, err = collectionStore.AddDevicesToCollection(ctx, userB.OrganizationID, rackB.Id, []string{devB.ID})
	require.NoError(t, err)

	filter := &stores.MinerFilter{Zones: []string{sharedZone}}

	// Org A should see only its device, never org B's.
	rowsA, _, totalA, err := deviceStore.ListMinerStateSnapshots(ctx, userA.OrganizationID, "", 100, filter, nil)
	require.NoError(t, err)
	require.Len(t, rowsA, 1, "org A should see exactly its own miner under shared-zone")
	assert.Equal(t, devA.ID, rowsA[0].DeviceIdentifier)
	assert.Equal(t, int64(1), totalA, "total count must reflect the filtered org-scoped result")

	// Org B should see only its device, never org A's.
	rowsB, _, totalB, err := deviceStore.ListMinerStateSnapshots(ctx, userB.OrganizationID, "", 100, filter, nil)
	require.NoError(t, err)
	require.Len(t, rowsB, 1, "org B should see exactly its own miner under shared-zone")
	assert.Equal(t, devB.ID, rowsB[0].DeviceIdentifier)
	assert.Equal(t, int64(1), totalB, "total count must reflect the filtered org-scoped result")
}

// TestNumericRangeFilter_HashrateGreaterThan exercises the end-to-end numeric
// filter pipeline against real Postgres/Timescale. It seeds three miners with
// different hashrates and a fourth with stale telemetry, then proves the
// filter returns only the in-window miner above the threshold.
func TestNumericRangeFilter_HashrateGreaterThan(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	dbSvc := testContext.DatabaseService
	db := testContext.ServiceProvider.DB
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

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	dbSvc := testContext.DatabaseService
	db := testContext.ServiceProvider.DB
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

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	dbSvc := testContext.DatabaseService
	db := testContext.ServiceProvider.DB
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

func TestIPCIDRFilter_MultipleCIDRsActAsOR(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	dbSvc := testContext.DatabaseService
	db := testContext.ServiceProvider.DB
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

func TestNumericAndCIDRFilter_AndCorrectly(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	dbSvc := testContext.DatabaseService
	db := testContext.ServiceProvider.DB
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

// insertMetric inserts a single device_metrics row with a given hash rate
// (raw H/s, e.g. 95e12 for 95 TH/s) at the supplied time. Other measurements
// are left NULL.
func insertMetric(t *testing.T, db *sql.DB, deviceIdentifier string, ts time.Time, hashRateHs float64) {
	t.Helper()
	q := sqlc.New(db)
	require.NoError(t, q.InsertDeviceMetrics(context.Background(), sqlc.InsertDeviceMetricsParams{
		Time:             ts,
		DeviceIdentifier: deviceIdentifier,
		HashRateHs:       sql.NullFloat64{Float64: hashRateHs, Valid: true},
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

func identifiers(rows []sqlc.ListMinerStateSnapshotsRow) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.DeviceIdentifier)
	}
	return out
}

// TestZoneFilter_ExcludesSoftDeletedRack proves that soft-deleting a rack
// removes its miners from zone-filter results, even though the membership
// rows persist (soft delete only flags device_set.deleted_at).
func TestZoneFilter_ExcludesSoftDeletedRack(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	dbSvc := testContext.DatabaseService
	db := testContext.ServiceProvider.DB
	deviceStore := sqlstores.NewSQLDeviceStore(db)
	collectionStore := sqlstores.NewSQLCollectionStore(db)
	ctx := t.Context()

	user := dbSvc.CreateSuperAdminUser()
	dev := dbSvc.CreateDevice(user.OrganizationID, "proto")

	rack, err := collectionStore.CreateCollection(ctx, user.OrganizationID, collectionpb.CollectionType_COLLECTION_TYPE_RACK, "Doomed Rack", "")
	require.NoError(t, err)
	require.NoError(t, collectionStore.CreateRackExtension(ctx, rack.Id, "doomed-zone", 4, 8, 0, 0, user.OrganizationID))
	_, err = collectionStore.AddDevicesToCollection(ctx, user.OrganizationID, rack.Id, []string{dev.ID})
	require.NoError(t, err)

	filter := &stores.MinerFilter{Zones: []string{"doomed-zone"}}

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
