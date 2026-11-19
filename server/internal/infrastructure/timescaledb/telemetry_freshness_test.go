package timescaledb_test

import (
	"database/sql"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/plugins/mappers"
	models "github.com/block/proto-fleet/server/internal/domain/telemetry/models/v2"
	"github.com/block/proto-fleet/server/internal/infrastructure/timescaledb"
	"github.com/block/proto-fleet/server/internal/testutil"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

func TestCommittedTelemetryFreshnessInventoryAndRecovery(t *testing.T) {
	database := testutil.NewDatabaseService(t, nil)
	owner := database.CreateSuperAdminUser()
	db := database.DB
	ctx := t.Context()
	device := database.CreateDevice(owner.OrganizationID, "proto")
	never := database.CreateDevice(owner.OrganizationID, "proto")
	_, err := db.ExecContext(ctx, `INSERT INTO device_pairing(device_id,pairing_status,paired_at)
VALUES ($1,'PAIRED',now()-interval '1 hour'),($2,'AUTHENTICATION_NEEDED',now()-interval '1 hour')
ON CONFLICT(device_id) DO UPDATE SET pairing_status=EXCLUDED.pairing_status,paired_at=EXCLUDED.paired_at`, device.DatabaseID, never.DatabaseID)
	require.NoError(t, err)
	config := timescaledb.DefaultConfig()
	config.TrackTelemetryFreshness = true
	store, err := timescaledb.NewTelemetryStore(db, config)
	require.NoError(t, err)
	now := time.Now().UTC().Truncate(time.Microsecond)
	sample := models.DeviceMetrics{DeviceIdentifier: device.ID, Timestamp: now, FreshnessObservedAt: now.Add(-time.Second), HashrateHS: &models.MetricValue{Value: 0}}
	require.NoError(t, store.StoreDeviceMetrics(ctx, sample))
	summary := func() (int64, int64, int64, int64) {
		t.Helper()
		var expected, fresh, stale, unknown int64
		err := db.QueryRowContext(ctx, `SELECT coalesce(sum(expected_count),0),coalesce(sum(fresh_count),0),coalesce(sum(stale_count),0),coalesce(sum(unknown_count),0) FROM telemetry_freshness_summary WHERE org_id=$1`, owner.OrganizationID).Scan(&expected, &fresh, &stale, &unknown)
		require.NoError(t, err)
		return expected, fresh, stale, unknown
	}
	expected, fresh, stale, unknown := summary()
	require.EqualValues(t, 2, expected)
	require.EqualValues(t, 1, fresh)
	require.Zero(t, stale)
	require.EqualValues(t, 1, unknown, "never-seen inventory must remain in denominator")
	// A replayed older measurement must not advance the committed watermark.
	_, err = db.ExecContext(ctx, `UPDATE telemetry_freshness SET ingested_at=now()-interval '1 minute' WHERE device_id=$1`, device.DatabaseID)
	require.NoError(t, err)
	sample.Timestamp = now.Add(time.Millisecond)
	sample.FreshnessObservedAt = now.Add(-time.Hour)
	require.NoError(t, store.StoreDeviceMetrics(ctx, sample))
	var observed time.Time
	require.NoError(t, db.QueryRowContext(ctx, `SELECT observed_at FROM telemetry_freshness WHERE device_id=$1`, device.DatabaseID).Scan(&observed))
	require.WithinDuration(t, now.Add(-time.Second), observed, time.Microsecond)
	// A recoverable watermark failure must retain telemetry and the prior watermark.
	_, err = db.ExecContext(ctx, `CREATE FUNCTION reject_freshness() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'test failure'; END $$;
CREATE TRIGGER reject_freshness BEFORE INSERT OR UPDATE ON telemetry_freshness FOR EACH ROW EXECUTE FUNCTION reject_freshness()`)
	require.NoError(t, err)
	sample.Timestamp = now.Add(2 * time.Millisecond)
	require.NoError(t, store.StoreDeviceMetrics(ctx, sample))
	var count int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM device_metrics WHERE device_identifier=$1 AND time=$2`, device.ID, sample.Timestamp).Scan(&count))
	require.Equal(t, 1, count)
	require.NoError(t, db.QueryRowContext(ctx, `SELECT observed_at FROM telemetry_freshness WHERE device_id=$1`, device.DatabaseID).Scan(&observed))
	require.WithinDuration(t, now.Add(-time.Second), observed, time.Microsecond)
	_, err = db.ExecContext(ctx, `DROP TRIGGER reject_freshness ON telemetry_freshness`)
	require.NoError(t, err)
	// Explicit maintenance expires; unknown targets then return automatically.
	_, err = db.ExecContext(ctx, `INSERT INTO telemetry_freshness_exclusion(device_id,reason,expires_at) VALUES($1,'test maintenance',now()+interval '1 hour')`, never.DatabaseID)
	require.NoError(t, err)
	expected, _, _, unknown = summary()
	require.EqualValues(t, 1, expected)
	require.Zero(t, unknown)
	_, err = db.ExecContext(ctx, `UPDATE telemetry_freshness_exclusion SET created_at=now()-interval '2 hours',expires_at=now()-interval '1 hour' WHERE device_id=$1`, never.DatabaseID)
	require.NoError(t, err)
	expected, _, _, unknown = summary()
	require.EqualValues(t, 2, expected)
	require.EqualValues(t, 1, unknown)
	// Stale, invalid/future, grace and removal are distinct states.
	_, err = db.ExecContext(ctx, `UPDATE telemetry_freshness SET observed_at=now()-interval '10 minutes' WHERE device_id=$1`, device.DatabaseID)
	require.NoError(t, err)
	_, _, stale, _ = summary()
	require.EqualValues(t, 1, stale)
	_, err = db.ExecContext(ctx, `UPDATE telemetry_freshness SET observed_at=now()+interval '1 hour' WHERE device_id=$1`, device.DatabaseID)
	require.NoError(t, err)
	_, _, _, unknown = summary()
	require.EqualValues(t, 2, unknown)
	_, err = db.ExecContext(ctx, `UPDATE device_pairing SET paired_at=now() WHERE device_id=$1`, never.DatabaseID)
	require.NoError(t, err)
	expected, _, _, _ = summary()
	require.EqualValues(t, 1, expected)
	_, err = db.ExecContext(ctx, `UPDATE device SET deleted_at=now() WHERE id=$1`, device.DatabaseID)
	require.NoError(t, err)
	expected, _, _, _ = summary()
	require.Zero(t, expected)
	// The generated query is the public summary contract used by operators.
	_, err = sqlc.New(db).GetTelemetryFreshnessSummary(ctx)
	require.NoError(t, err)
}

// Seed inventory in one bulk statement, then write the watermarks in two
// multi-target batches. The summary view must count each target one time.
// The test uses the real view and the real batch query, with no raw-history scan.
func TestTelemetryFreshnessSummaryCountsBatchedWatermarks(t *testing.T) {
	if testing.Short() {
		t.Skip("PostgreSQL inventory exercise")
	}
	const (
		targets   = 1000
		batchSize = 500
	)
	database := testutil.NewDatabaseService(t, nil)
	owner := database.CreateSuperAdminUser()
	db := database.DB
	ctx := t.Context()
	_, err := db.ExecContext(ctx, `WITH discovered AS (
 INSERT INTO discovered_device(org_id,device_identifier,ip_address,port,url_scheme,driver_name,is_active)
 SELECT $1,'fresh-scale-'||n,'10.'||((n/65536)%256)||'.'||((n/256)%256)||'.'||(n%256),'4028','http','proto',true FROM generate_series(1,$2::int) n RETURNING id,device_identifier
), devices AS (
 INSERT INTO device(org_id,device_identifier,mac_address,discovered_device_id)
 SELECT $1,device_identifier,'00:00:00:00:00:00',id FROM discovered RETURNING id
)
INSERT INTO device_pairing(device_id,pairing_status,paired_at) SELECT id,'PAIRED',now()-interval '1 hour' FROM devices`, owner.OrganizationID, targets)
	require.NoError(t, err)
	started := time.Now()
	var expected, unknown int64
	err = db.QueryRowContext(ctx, `SELECT sum(expected_count),sum(unknown_count) FROM telemetry_freshness_summary WHERE org_id=$1`, owner.OrganizationID).Scan(&expected, &unknown)
	require.NoError(t, err)
	require.EqualValues(t, targets, expected)
	require.EqualValues(t, targets, unknown)
	t.Logf("%d-target freshness evaluation: %s", targets, time.Since(started))
	writer := sqlc.New(db)
	writeStarted := time.Now()
	for offset := 0; offset < targets; offset += batchSize {
		params := sqlc.RecordTelemetryFreshnessBatchParams{}
		for i := offset + 1; i <= offset+batchSize; i++ {
			params.Identifiers = append(params.Identifiers, fmt.Sprintf("fresh-scale-%d", i))
			params.ObservedTimes = append(params.ObservedTimes, time.Now().Add(-time.Second))
			params.Known = append(params.Known, true)
			params.SourceNodes = append(params.SourceNodes, 0)
		}
		require.NoError(t, writer.RecordTelemetryFreshnessBatch(ctx, params))
	}
	t.Logf("%d initial watermark inserts in %d-target batches: %s", targets, batchSize, time.Since(writeStarted))
	started = time.Now()
	err = db.QueryRowContext(ctx, `SELECT sum(expected_count),sum(unknown_count) FROM telemetry_freshness_summary WHERE org_id=$1`, owner.OrganizationID).Scan(&expected, &unknown)
	require.NoError(t, err)
	require.EqualValues(t, targets, expected)
	require.Zero(t, unknown)
	t.Logf("%d populated watermark evaluation: %s", targets, time.Since(started))
	// Prove successful SQL output is serializable for the independent observer.
	var payload sql.NullString
	require.NoError(t, db.QueryRowContext(ctx, `SELECT json_agg(row_to_json(s))::text FROM telemetry_freshness_summary s WHERE org_id=$1`, owner.OrganizationID).Scan(&payload))
	require.True(t, payload.Valid)
}

func TestTelemetryFreshnessRejectsPreviousNodeAssignments(t *testing.T) {
	database := testutil.NewDatabaseService(t, nil)
	owner := database.CreateSuperAdminUser()
	db, ctx := database.DB, t.Context()
	device := database.CreateDevice(owner.OrganizationID, "proto")
	_, err := db.ExecContext(ctx, `INSERT INTO device_pairing(device_id,pairing_status,paired_at)
VALUES($1,'PAIRED',now()-interval '1 hour') ON CONFLICT(device_id) DO UPDATE SET pairing_status='PAIRED',paired_at=EXCLUDED.paired_at`, device.DatabaseID)
	require.NoError(t, err)
	newNode := func(name, key string) int64 {
		t.Helper()
		var id int64
		err := db.QueryRowContext(ctx, `INSERT INTO fleet_node(org_id,name,identity_pubkey,encryption_pubkey,enrollment_status)
VALUES($1,$2,decode(repeat($3,32),'hex'),decode(repeat($3,32),'hex'),'CONFIRMED') RETURNING id`, owner.OrganizationID, name, key).Scan(&id)
		require.NoError(t, err)
		return id
	}
	a, b := newNode("source-a", "01"), newNode("source-b", "02")
	config := timescaledb.DefaultConfig()
	config.TrackTelemetryFreshness = true
	store, err := timescaledb.NewTelemetryStore(db, config)
	require.NoError(t, err)
	sample := models.DeviceMetrics{DeviceIdentifier: device.ID, Timestamp: time.Now(), FreshnessObservedAt: time.Now().Add(-time.Second), HashrateHS: &models.MetricValue{Value: 1}}
	require.NoError(t, store.StoreDeviceMetrics(ctx, sample))
	_, err = db.ExecContext(ctx, `INSERT INTO fleet_node_device(fleet_node_id,device_id,org_id,assigned_at) VALUES($1,$2,$3,now()-interval '6 minutes')`, a, device.DatabaseID, owner.OrganizationID)
	require.NoError(t, err)
	state := func(wantFresh, wantUnknown int64) {
		t.Helper()
		var fresh, unknown int64
		err := db.QueryRowContext(ctx, `SELECT sum(fresh_count),sum(unknown_count) FROM telemetry_freshness_summary WHERE org_id=$1`, owner.OrganizationID).Scan(&fresh, &unknown)
		require.NoError(t, err)
		require.Equal(t, wantFresh, fresh)
		require.Equal(t, wantUnknown, unknown)
	}
	state(0, 1)
	sample.FreshnessSourceNodeID = a
	sample.Timestamp = sample.Timestamp.Add(time.Millisecond)
	require.NoError(t, store.StoreDeviceMetrics(ctx, sample))
	state(1, 0)
	_, err = db.ExecContext(ctx, `UPDATE fleet_node_device SET fleet_node_id=$1,assigned_at=now()-interval '6 minutes' WHERE device_id=$2`, b, device.DatabaseID)
	require.NoError(t, err)
	state(0, 1)
	// A late successful response from A must not populate B's watermark.
	sample.Timestamp = sample.Timestamp.Add(time.Millisecond)
	require.NoError(t, store.StoreDeviceMetrics(ctx, sample))
	state(0, 1)
	// The new route must clear the previous route's known state immediately,
	// even when its first response contains only status.
	unknown := sample
	unknown.FreshnessSourceNodeID = b
	unknown.HashrateHS = nil
	unknown.Timestamp = unknown.Timestamp.Add(time.Millisecond)
	require.NoError(t, store.StoreDeviceMetrics(ctx, unknown))
	var newKnown bool
	var newObserved sql.NullTime
	var newSource int64
	require.NoError(t, db.QueryRowContext(ctx, `SELECT observation_known,observed_at,source_node_id FROM telemetry_freshness WHERE device_id=$1`, device.DatabaseID).Scan(&newKnown, &newObserved, &newSource))
	require.False(t, newKnown)
	require.False(t, newObserved.Valid)
	require.Equal(t, b, newSource)
	state(0, 1)
	sample.Timestamp = unknown.Timestamp
	sample.FreshnessSourceNodeID = b
	sample.FreshnessObservedAt = time.Now().Add(-5 * time.Minute).UTC().Truncate(time.Microsecond)
	sample.Timestamp = sample.Timestamp.Add(time.Millisecond)
	require.NoError(t, store.StoreDeviceMetrics(ctx, sample))
	var resetObserved time.Time
	require.NoError(t, db.QueryRowContext(ctx, `SELECT observed_at FROM telemetry_freshness WHERE device_id=$1`, device.DatabaseID).Scan(&resetObserved))
	require.Equal(t, sample.FreshnessObservedAt, resetObserved.UTC())
	var stale int64
	require.NoError(t, db.QueryRowContext(ctx, `SELECT sum(stale_count) FROM telemetry_freshness_summary WHERE org_id=$1`, owner.OrganizationID).Scan(&stale))
	require.EqualValues(t, 1, stale)
	state(0, 0)
	sample.FreshnessSourceNodeID = a
	sample.Timestamp = sample.Timestamp.Add(time.Millisecond)
	require.NoError(t, store.StoreDeviceMetrics(ctx, sample))
	state(0, 0)
	var source int64
	require.NoError(t, db.QueryRowContext(ctx, `SELECT source_node_id FROM telemetry_freshness WHERE device_id=$1`, device.DatabaseID).Scan(&source))
	require.Equal(t, b, source)
}

func TestCachedSDKTelemetryCannotRenewCommittedFreshness(t *testing.T) {
	database := testutil.NewDatabaseService(t, nil)
	owner := database.CreateSuperAdminUser()
	device := database.CreateDevice(owner.OrganizationID, "asicrs")
	db := database.DB
	ctx := t.Context()
	_, err := db.ExecContext(ctx, `INSERT INTO device_pairing(device_id,pairing_status,paired_at)
VALUES ($1,'PAIRED',now()-interval '1 hour')
ON CONFLICT(device_id) DO UPDATE SET pairing_status=EXCLUDED.pairing_status,paired_at=EXCLUDED.paired_at`, device.DatabaseID)
	require.NoError(t, err)
	config := timescaledb.DefaultConfig()
	config.TrackTelemetryFreshness = true
	store, err := timescaledb.NewTelemetryStore(db, config)
	require.NoError(t, err)

	// ASIC-rs scalar metrics have no timestamp metadata. Its SDK snapshot must
	// retain the original poll time across cache hits, even with a 300s TTL.
	observedAt := time.Now().UTC().Add(-3 * time.Minute).Truncate(time.Microsecond)
	cached := sdk.DeviceMetrics{
		DeviceID:  device.ID,
		Timestamp: observedAt,
		PowerW:    &sdk.MetricValue{Value: 3000, Kind: sdk.MetricKindGauge},
	}
	for range 2 {
		require.NoError(t, store.StoreDeviceMetrics(ctx, mappers.SDKDeviceMetricsToV2(cached)))
		var committed time.Time
		require.NoError(t, db.QueryRowContext(ctx, `SELECT observed_at FROM telemetry_freshness WHERE device_id=$1`, device.DatabaseID).Scan(&committed))
		require.Equal(t, observedAt, committed.UTC(), "successful cached uploads cannot advance the watermark")
		var fresh, stale, unknown int64
		require.NoError(t, db.QueryRowContext(ctx, `SELECT sum(fresh_count),sum(stale_count),sum(unknown_count) FROM telemetry_freshness_summary WHERE org_id=$1`, owner.OrganizationID).Scan(&fresh, &stale, &unknown))
		require.Zero(t, fresh)
		require.EqualValues(t, 1, stale)
		require.Zero(t, unknown)
		// Force the next upload past coalescing so unchanged evidence, rather
		// than the 30s write gate, must keep the watermark from advancing.
		_, err = db.ExecContext(ctx, `UPDATE telemetry_freshness SET ingested_at=now()-interval '1 minute' WHERE device_id=$1`, device.DatabaseID)
		require.NoError(t, err)
	}

	// A new observation with the same numeric value does restore freshness.
	cached.Timestamp = time.Now().UTC().Truncate(time.Microsecond)
	require.NoError(t, store.StoreDeviceMetrics(ctx, mappers.SDKDeviceMetricsToV2(cached)))
	var fresh int64
	require.NoError(t, db.QueryRowContext(ctx, `SELECT sum(fresh_count) FROM telemetry_freshness_summary WHERE org_id=$1`, owner.OrganizationID).Scan(&fresh))
	require.EqualValues(t, 1, fresh)
}

func TestRejectedContainerTelemetryCannotRenewCommittedFreshness(t *testing.T) {
	for _, tracked := range []bool{false, true} {
		t.Run(fmt.Sprintf("prior_watermark_%t", tracked), func(t *testing.T) {
			database := testutil.NewDatabaseService(t, nil)
			owner := database.CreateSuperAdminUser()
			device := database.CreateDevice(owner.OrganizationID, "proto")
			db, ctx := database.DB, t.Context()
			_, err := db.ExecContext(ctx, `UPDATE discovered_device SET device_class=2 WHERE id=(SELECT discovered_device_id FROM device WHERE id=$1)`, device.DatabaseID)
			require.NoError(t, err)
			_, err = db.ExecContext(ctx, `INSERT INTO device_pairing(device_id,pairing_status,paired_at)
VALUES ($1,'PAIRED',now()-interval '1 hour')
ON CONFLICT(device_id) DO UPDATE SET pairing_status=EXCLUDED.pairing_status,paired_at=EXCLUDED.paired_at`, device.DatabaseID)
			require.NoError(t, err)
			config := timescaledb.DefaultConfig()
			config.TrackTelemetryFreshness = tracked
			store, err := timescaledb.NewTelemetryStore(db, config)
			require.NoError(t, err)
			old := time.Now().UTC().Add(-3 * time.Minute).Truncate(time.Microsecond)
			initial := models.DeviceMetrics{
				DeviceIdentifier: device.ID, Timestamp: old, FreshnessObservedAt: old,
				PowerW:    &models.MetricValue{Value: 3000, Kind: models.MetricKindGauge},
				Container: &sdk.ContainerSnapshot{ObservedAt: old},
			}
			require.NoError(t, store.StoreDeviceMetrics(ctx, initial))
			require.True(t, initial.Container.InventoryAccepted)
			config.TrackTelemetryFreshness = true
			store, err = timescaledb.NewTelemetryStore(db, config)
			require.NoError(t, err)
			// Force any existing watermark beyond the write-coalescing gate.
			_, err = db.ExecContext(ctx, `UPDATE telemetry_freshness SET ingested_at=now()-interval '1 minute' WHERE device_id=$1`, device.DatabaseID)
			require.NoError(t, err)
			for _, offset := range []time.Duration{-time.Minute, -time.Second} {
				// After moving to a Node with an older clock, the snapshot is
				// rejected while its server-clock freshness time looks current.
				rejected := initial
				rejected.Timestamp = old.Add(offset)
				rejected.FreshnessObservedAt = time.Now().UTC()
				rejected.Container = &sdk.ContainerSnapshot{ObservedAt: rejected.Timestamp}
				require.NoError(t, store.StoreDeviceMetrics(ctx, rejected))
				require.False(t, rejected.Container.InventoryAccepted)
				var count int64
				require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM device_metrics WHERE device_identifier=$1 AND time=$2`, device.ID, rejected.Timestamp).Scan(&count))
				require.Zero(t, count, "rejected container measurement must not be persisted")
				var observed sql.NullTime
				require.NoError(t, db.QueryRowContext(ctx, `SELECT max(observed_at) FROM telemetry_freshness WHERE device_id=$1`, device.DatabaseID).Scan(&observed))
				if tracked {
					require.True(t, observed.Valid)
					require.Equal(t, old, observed.Time.UTC(), "rejected snapshots must not advance the watermark")
				} else {
					require.False(t, observed.Valid, "rejected snapshots must not establish a watermark")
				}
				var fresh, stale, unknown int64
				require.NoError(t, db.QueryRowContext(ctx, `SELECT sum(fresh_count),sum(stale_count),sum(unknown_count) FROM telemetry_freshness_summary WHERE org_id=$1`, owner.OrganizationID).Scan(&fresh, &stale, &unknown))
				require.Zero(t, fresh)
				if tracked {
					require.EqualValues(t, 1, stale)
					require.Zero(t, unknown)
				} else {
					require.Zero(t, stale)
					require.EqualValues(t, 1, unknown)
				}
			}
			// A subsequent accepted measurement can restore freshness.
			current := time.Now().UTC().Truncate(time.Microsecond)
			initial.Timestamp, initial.FreshnessObservedAt = current, current
			initial.Container = &sdk.ContainerSnapshot{ObservedAt: current}
			require.NoError(t, store.StoreDeviceMetrics(ctx, initial))
			require.True(t, initial.Container.InventoryAccepted)
			var fresh int64
			require.NoError(t, db.QueryRowContext(ctx, `SELECT sum(fresh_count) FROM telemetry_freshness_summary WHERE org_id=$1`, owner.OrganizationID).Scan(&fresh))
			require.EqualValues(t, 1, fresh)
		})
	}
}

func freshnessStoreFixture(t *testing.T) (*sql.DB, *timescaledb.TimescaleTelemetryStore, testutil.DeviceIdentification, int64) {
	t.Helper()
	database := testutil.NewDatabaseService(t, nil)
	owner := database.CreateSuperAdminUser()
	device := database.CreateDevice(owner.OrganizationID, "proto")
	_, err := database.DB.ExecContext(t.Context(), `INSERT INTO device_pairing(device_id,pairing_status,paired_at)
VALUES($1,'PAIRED',now()-interval '1 hour') ON CONFLICT(device_id) DO UPDATE SET pairing_status='PAIRED',paired_at=EXCLUDED.paired_at`, device.DatabaseID)
	require.NoError(t, err)
	config := timescaledb.DefaultConfig()
	config.TrackTelemetryFreshness = true
	store, err := timescaledb.NewTelemetryStore(database.DB, config)
	require.NoError(t, err)
	return database.DB, store, device, owner.OrganizationID
}

func TestTelemetryFreshnessCoalescesSameRouteAndPreservesKnownEvidence(t *testing.T) {
	db, store, device, org := freshnessStoreFixture(t)
	ctx := t.Context()
	now := time.Now().UTC().Truncate(time.Microsecond)
	sample := models.DeviceMetrics{DeviceIdentifier: device.ID, Timestamp: now.Add(-time.Minute),
		FreshnessObservedAt: now.Add(-time.Minute), PowerW: &models.MetricValue{Value: 1, Kind: models.MetricKindGauge}}
	require.NoError(t, store.StoreDeviceMetrics(ctx, sample))
	row := func() (time.Time, time.Time, bool) {
		t.Helper()
		var observed, ingested time.Time
		var known bool
		require.NoError(t, db.QueryRowContext(ctx, `SELECT observed_at,ingested_at,observation_known FROM telemetry_freshness WHERE device_id=$1`, device.DatabaseID).Scan(&observed, &ingested, &known))
		return observed.UTC(), ingested.UTC(), known
	}
	before, beforeIngested, _ := row()
	sample.Timestamp = now
	sample.FreshnessObservedAt = now
	require.NoError(t, store.StoreDeviceMetrics(ctx, sample))
	observed, ingested, known := row()
	require.Equal(t, before, observed)
	require.Equal(t, beforeIngested, ingested)
	require.True(t, known)
	for _, outsideWindow := range []bool{false, true} {
		for _, kind := range []string{"status_only", "zero_time", "future_time", "nan_only"} {
			t.Run(fmt.Sprintf("%s/outside_%t", kind, outsideWindow), func(t *testing.T) {
				if outsideWindow {
					_, err := db.ExecContext(ctx, `UPDATE telemetry_freshness SET ingested_at=now()-interval '1 minute' WHERE device_id=$1`, device.DatabaseID)
					require.NoError(t, err)
				}
				oldObserved, oldIngested, _ := row()
				invalid := sample
				switch kind {
				case "status_only":
					invalid.PowerW = nil
				case "zero_time":
					invalid.FreshnessObservedAt = time.Time{}
				case "future_time":
					invalid.FreshnessObservedAt = now.Add(time.Hour)
				case "nan_only":
					invalid.PowerW = &models.MetricValue{Value: math.NaN(), Kind: models.MetricKindGauge}
				}
				require.NoError(t, store.StoreDeviceMetrics(ctx, invalid))
				gotObserved, gotIngested, gotKnown := row()
				require.Equal(t, oldObserved, gotObserved)
				require.True(t, gotKnown)
				if !outsideWindow {
					require.Equal(t, oldIngested, gotIngested)
				}
			})
		}
	}
	_, err := db.ExecContext(ctx, `UPDATE telemetry_freshness SET observed_at=now()-interval '3 minutes' WHERE device_id=$1`, device.DatabaseID)
	require.NoError(t, err)
	invalid := sample
	invalid.PowerW = nil
	require.NoError(t, store.StoreDeviceMetrics(ctx, invalid))
	var stale int64
	require.NoError(t, db.QueryRowContext(ctx, `SELECT sum(stale_count) FROM telemetry_freshness_summary WHERE org_id=$1`, org).Scan(&stale))
	require.EqualValues(t, 1, stale)
}

func TestTelemetryFreshnessUnknownToKnownBypassesGateAndAcceptsZero(t *testing.T) {
	for _, kind := range []string{"status_only", "zero_time", "future_time", "nan_only"} {
		t.Run(kind, func(t *testing.T) {
			db, store, device, _ := freshnessStoreFixture(t)
			now := time.Now().UTC().Truncate(time.Microsecond)
			sample := models.DeviceMetrics{DeviceIdentifier: device.ID, Timestamp: now, FreshnessObservedAt: now, PowerW: &models.MetricValue{Value: 0, Kind: models.MetricKindGauge}}
			invalid := sample
			switch kind {
			case "status_only":
				invalid.PowerW = nil
			case "zero_time":
				invalid.FreshnessObservedAt = time.Time{}
			case "future_time":
				invalid.FreshnessObservedAt = now.Add(time.Hour)
			case "nan_only":
				invalid.PowerW = &models.MetricValue{Value: math.NaN(), Kind: models.MetricKindGauge}
			}
			require.NoError(t, store.StoreDeviceMetrics(t.Context(), invalid))
			var known bool
			require.NoError(t, db.QueryRowContext(t.Context(), `SELECT observation_known FROM telemetry_freshness WHERE device_id=$1`, device.DatabaseID).Scan(&known))
			require.False(t, known)
			require.NoError(t, store.StoreDeviceMetrics(t.Context(), sample))
			var observed time.Time
			require.NoError(t, db.QueryRowContext(t.Context(), `SELECT observation_known,observed_at FROM telemetry_freshness WHERE device_id=$1`, device.DatabaseID).Scan(&known, &observed))
			require.True(t, known)
			require.Equal(t, now, observed.UTC())
		})
	}
}

func TestTelemetryFreshnessHandoffUsesActualMoveAndAdmissionGrace(t *testing.T) {
	db, store, device, org := freshnessStoreFixture(t)
	ctx := t.Context()
	node := func(name, key string) int64 {
		t.Helper()
		var id int64
		require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO fleet_node(org_id,name,identity_pubkey,encryption_pubkey,enrollment_status) VALUES($1,$2,decode(repeat($3,32),'hex'),decode(repeat($3,32),'hex'),'CONFIRMED') RETURNING id`, org, name, key).Scan(&id))
		return id
	}
	a, b := node("active", "01"), node("standby", "02")
	_, err := db.ExecContext(ctx, `INSERT INTO fleet_node_device(fleet_node_id,device_id,org_id,assigned_at) VALUES($1,$2,$3,now()-interval '1 hour')`, a, device.DatabaseID, org)
	require.NoError(t, err)
	sample := models.DeviceMetrics{DeviceIdentifier: device.ID, Timestamp: time.Now(), FreshnessObservedAt: time.Now().Add(-time.Second), FreshnessSourceNodeID: a, PowerW: &models.MetricValue{Value: 1}}
	require.NoError(t, store.StoreDeviceMetrics(ctx, sample))
	moved, err := sqlc.New(db).MoveFleetNodeDevices(ctx, sqlc.MoveFleetNodeDevicesParams{FromNodeID: a, ToNodeID: b, OrgID: org})
	require.NoError(t, err)
	require.Contains(t, moved, device.ID)
	var expected, grace, unknown int64
	require.NoError(t, db.QueryRowContext(ctx, `SELECT sum(expected_count),sum(grace_count),sum(unknown_count) FROM telemetry_freshness_summary WHERE org_id=$1`, org).Scan(&expected, &grace, &unknown))
	require.Zero(t, expected)
	require.EqualValues(t, 1, grace)
	require.Zero(t, unknown)
	_, err = db.ExecContext(ctx, `UPDATE fleet_node_device SET assigned_at=now()-interval '6 minutes' WHERE device_id=$1`, device.DatabaseID)
	require.NoError(t, err)
	require.NoError(t, db.QueryRowContext(ctx, `SELECT sum(expected_count),sum(grace_count),sum(unknown_count) FROM telemetry_freshness_summary WHERE org_id=$1`, org).Scan(&expected, &grace, &unknown))
	require.EqualValues(t, 1, expected)
	require.Zero(t, grace)
	require.EqualValues(t, 1, unknown)
}

func TestTelemetryFreshnessCommitFailureRollsBackMeasurementsAndWatermark(t *testing.T) {
	db, store, device, _ := freshnessStoreFixture(t)
	ctx := t.Context()
	_, err := db.ExecContext(ctx, `CREATE FUNCTION reject_freshness_commit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'commit rejected'; END $$;
CREATE CONSTRAINT TRIGGER reject_freshness_commit AFTER INSERT ON telemetry_freshness DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION reject_freshness_commit()`)
	require.NoError(t, err)
	sample := models.DeviceMetrics{DeviceIdentifier: device.ID, Timestamp: time.Now(), FreshnessObservedAt: time.Now().Add(-time.Second), PowerW: &models.MetricValue{Value: 1}}
	require.ErrorContains(t, store.StoreDeviceMetrics(ctx, sample), "commit rejected")
	for _, query := range []string{`SELECT count(*) FROM telemetry_freshness WHERE device_id=$1`, `SELECT count(*) FROM device_metrics WHERE device_identifier=$1`} {
		var count int
		var key any = device.DatabaseID
		if strings.Contains(query, "device_identifier") {
			key = device.ID
		}
		require.NoError(t, db.QueryRowContext(ctx, query, key).Scan(&count))
		require.Zero(t, count)
	}
}

func TestTelemetryFreshnessMissingRelationOrLockTimeoutRetainsTelemetry(t *testing.T) {
	for _, failure := range []string{"missing_relation", "row_lock"} {
		t.Run(failure, func(t *testing.T) {
			db, store, device, _ := freshnessStoreFixture(t)
			ctx := t.Context()
			now := time.Now().UTC().Truncate(time.Microsecond)
			sample := models.DeviceMetrics{DeviceIdentifier: device.ID, Timestamp: now.Add(-time.Minute), FreshnessObservedAt: now.Add(-time.Minute), PowerW: &models.MetricValue{Value: 1}}
			require.NoError(t, store.StoreDeviceMetrics(ctx, sample))
			if failure == "missing_relation" {
				_, err := db.ExecContext(ctx, `ALTER TABLE telemetry_freshness RENAME TO unavailable_freshness`)
				require.NoError(t, err)
			} else {
				_, err := db.ExecContext(ctx, `UPDATE telemetry_freshness SET ingested_at=now()-interval '1 minute' WHERE device_id=$1`, device.DatabaseID)
				require.NoError(t, err)
				held, err := db.BeginTx(ctx, nil)
				require.NoError(t, err)
				defer held.Rollback()
				_, err = held.ExecContext(ctx, `UPDATE telemetry_freshness SET observed_at=observed_at WHERE device_id=$1`, device.DatabaseID)
				require.NoError(t, err)
			}
			sample.Timestamp = now
			sample.FreshnessObservedAt = now
			require.NoError(t, store.StoreDeviceMetrics(ctx, sample))
			var count int64
			require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM device_metrics WHERE device_identifier=$1 AND time=$2`, device.ID, now).Scan(&count))
			require.EqualValues(t, 1, count)
			var observed time.Time
			query := `SELECT observed_at FROM telemetry_freshness WHERE device_id=$1`
			if failure == "missing_relation" {
				query = `SELECT observed_at FROM unavailable_freshness WHERE device_id=$1`
			}
			require.NoError(t, db.QueryRowContext(ctx, query, device.DatabaseID).Scan(&observed))
			require.Equal(t, now.Add(-time.Minute), observed.UTC())
		})
	}
}
