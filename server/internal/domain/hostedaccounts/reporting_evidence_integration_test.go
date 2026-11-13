package hostedaccounts_test

import (
	"fmt"
	"testing"
	"time"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/migrations"
	"github.com/stretchr/testify/require"
)

// Roll back the reporting evidence holds only against this test's disposable
// database. Hourly evidence (1077) retires the 1055 raw hold and references its
// allocation snapshot, so it rolls back first, in the same implicit transaction.
func rollbackReportingEvidence(t *testing.T, f fixture) error {
	t.Helper()
	script, err := migrations.Current.ReadFile("current/001055_internal_hosted_reporting_evidence.down.sql")
	require.NoError(t, err)
	hourly, err := migrations.Current.ReadFile("current/001077_internal_hourly_reporting_evidence.down.sql")
	require.NoError(t, err)
	var hourlyApplied bool
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT to_regclass('hosted_account_reporting_hour') IS NOT NULL`).Scan(&hourlyApplied))
	statements := string(script)
	if hourlyApplied {
		statements = string(hourly) + "\n" + statements
	}
	if _, err = f.db.ExecContext(t.Context(), statements); err != nil {
		return fmt.Errorf("apply reporting evidence down migration: %w", err)
	}
	return nil
}

// rawHoldCatalog describes every object migration 1055 creates or alters:
// relations, columns, constraints, indexes, triggers, functions, the status
// view, the event sequence and the epoch and event row counts.
func rawHoldCatalog(t *testing.T, f fixture) []string {
	t.Helper()
	rows, err := f.db.QueryContext(t.Context(), `WITH rel AS (
 SELECT c.oid,c.relname,c.relkind FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
 WHERE n.nspname='public' AND c.relname IN ('hosted_account_reporting_evidence','hosted_account_reporting_evidence_epoch',
  'hosted_account_reporting_allocation_snapshot','hosted_account_reporting_capture_status','hosted_account_reporting_evidence_event_id_seq'))
SELECT entry FROM (
 SELECT 'relation '||r.relname||' kind='||c.relkind::text||' persistence='||c.relpersistence::text||' options='||COALESCE(c.reloptions::text,'')
  ||' acl='||COALESCE(c.relacl::text,'')||' comment='||COALESCE(obj_description(c.oid,'pg_class'),'') AS entry
 FROM rel r JOIN pg_class c ON c.oid=r.oid
 UNION ALL
 SELECT 'column '||r.relname||'.'||a.attname||' '||a.attnum||' '||format_type(a.atttypid,a.atttypmod)||' notnull='||a.attnotnull
  ||' default='||COALESCE(pg_get_expr(d.adbin,d.adrelid),'')
 FROM rel r JOIN pg_attribute a ON a.attrelid=r.oid AND a.attnum>0 AND NOT a.attisdropped
 LEFT JOIN pg_attrdef d ON d.adrelid=a.attrelid AND d.adnum=a.attnum
 UNION ALL
 SELECT 'constraint '||r.relname||'.'||c.conname||' '||pg_get_constraintdef(c.oid) FROM rel r JOIN pg_constraint c ON c.conrelid=r.oid
 UNION ALL
 SELECT 'index '||pg_get_indexdef(i.indexrelid) FROM rel r JOIN pg_index i ON i.indrelid=r.oid
 UNION ALL
 SELECT 'trigger '||pg_get_triggerdef(t.oid)||' enabled='||t.tgenabled::text FROM pg_trigger t
 WHERE NOT t.tgisinternal AND t.tgrelid IN (SELECT r.oid FROM rel r
  UNION ALL SELECT 'hosted_account_history_sample'::regclass UNION ALL SELECT 'hashboard_metrics'::regclass)
 UNION ALL
 SELECT 'function '||p.oid::regprocedure::text||' '||pg_get_functiondef(p.oid) FROM pg_proc p
 WHERE p.proname IN ('record_hosted_reporting_evidence','reject_hosted_reporting_evidence_mutation','record_hosted_hashboard_history_sample')
 UNION ALL
 SELECT 'view '||pg_get_viewdef(r.oid) FROM rel r WHERE r.relkind='v'
 UNION ALL
 SELECT 'sequence '||s.seqtypid::regtype::text||' '||s.seqstart||' '||s.seqincrement||' '||s.seqmin||' '||s.seqmax||' '||s.seqcache||' '||s.seqcycle
 FROM rel r JOIN pg_sequence s ON s.seqrelid=r.oid
 UNION ALL
 SELECT 'depends '||d.objid::regclass::text||' on '||d.refobjid::regclass::text||'.'||d.refobjsubid||' '||d.deptype::text
 FROM pg_depend d JOIN rel r ON r.oid=d.objid WHERE d.classid='pg_class'::regclass AND d.refclassid='pg_class'::regclass
 UNION ALL
 SELECT 'rows epoch='||(SELECT count(*) FROM hosted_account_reporting_evidence_epoch)
  ||' evidence='||(SELECT count(*) FROM hosted_account_reporting_evidence)
) catalog ORDER BY entry`)
	require.NoError(t, err)
	defer rows.Close()
	var catalog []string
	for rows.Next() {
		var entry string
		require.NoError(t, rows.Scan(&entry))
		catalog = append(catalog, entry)
	}
	require.NoError(t, rows.Err())
	return catalog
}

func requireRawHoldRetired(t *testing.T, f fixture) {
	t.Helper()
	var evidence, epoch, streamIndex, observationIndex, captureFunction, captureTrigger bool
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT
 to_regclass('hosted_account_reporting_evidence') IS NOT NULL,
 to_regclass('hosted_account_reporting_evidence_epoch') IS NOT NULL,
 to_regclass('idx_hosted_account_reporting_evidence_stream_event') IS NOT NULL,
 to_regclass('idx_hosted_account_reporting_evidence_stream_observation') IS NOT NULL,
 to_regprocedure('record_hosted_reporting_evidence()') IS NOT NULL,
 EXISTS(SELECT 1 FROM pg_trigger WHERE tgrelid='hosted_account_history_sample'::regclass AND tgname='record_hosted_reporting_evidence')`).Scan(
		&evidence, &epoch, &streamIndex, &observationIndex, &captureFunction, &captureTrigger))
	require.False(t, evidence, "the raw event table is retired")
	require.False(t, epoch, "the raw-hold epoch is retired")
	require.False(t, streamIndex)
	require.False(t, observationIndex)
	require.False(t, captureFunction, "the raw capture function is retired")
	require.False(t, captureTrigger, "normalized samples no longer copy raw events")

	var snapshot, immutable, snapshotTrigger bool
	var hashboardTrigger string
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT
 to_regclass('hosted_account_reporting_allocation_snapshot') IS NOT NULL,
 to_regprocedure('reject_hosted_reporting_evidence_mutation()') IS NOT NULL,
 EXISTS(SELECT 1 FROM pg_trigger WHERE tgrelid='hosted_account_reporting_allocation_snapshot'::regclass AND tgname='hosted_reporting_allocation_snapshot_immutable'),
 (SELECT pg_get_triggerdef(oid) FROM pg_trigger WHERE tgrelid='hashboard_metrics'::regclass AND tgname='record_hosted_hashboard_history')`).Scan(
		&snapshot, &immutable, &snapshotTrigger, &hashboardTrigger))
	require.True(t, snapshot, "hourly evidence keeps the allocation snapshot")
	require.True(t, immutable, "hourly evidence reuses the immutability function")
	require.True(t, snapshotTrigger)
	require.Contains(t, hashboardTrigger, "UPDATE OF hashboard_id, installation_id", "the narrowed 1055 hashboard trigger stays")
}

func TestReportingEvidenceUpgradeRetiresRawHold(t *testing.T) {
	f := setup(t)
	requireRawHoldRetired(t, f)
	var closed time.Time
	var missing int
	client := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(5, miner, 0)))
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT closed_through,missing_hours FROM hosted_account_reporting_capture_status
 WHERE asset_id=$1 AND kind=5 AND source_id=1 AND metric=1`, miner).Scan(&closed, &missing))
	require.Zero(t, missing, "the rebuilt status view reads the hourly hold")
}

func TestReportingEvidenceUpgradeDeletesPopulatedRawHold(t *testing.T) {
	f := setup(t)
	require.NoError(t, reportingHourMigration(t, f, "down"), "an empty hourly hold restores the raw hold")
	client := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(5, miner, 0)))
	_, err := f.db.ExecContext(t.Context(), `INSERT INTO device_metrics(time,device_identifier,hash_rate_hs,power_w) SELECT $1,device_identifier,100e12,3000 FROM device WHERE id=$2`, time.Now().UTC(), miner)
	require.NoError(t, err)
	var events int
	var snapshotBefore string
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_account_reporting_evidence`).Scan(&events))
	require.Positive(t, events, "the restored raw hold captures new samples")
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT jsonb_agg(to_jsonb(s) ORDER BY interval_id)::text FROM hosted_account_reporting_allocation_snapshot s`).Scan(&snapshotBefore))

	require.NoError(t, reportingHourMigration(t, f, "up"), "upgrade deletes populated raw evidence")
	requireRawHoldRetired(t, f)
	var snapshotAfter string
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT jsonb_agg(to_jsonb(s) ORDER BY interval_id)::text FROM hosted_account_reporting_allocation_snapshot s`).Scan(&snapshotAfter))
	require.JSONEq(t, snapshotBefore, snapshotAfter, "allocation snapshots survive the retirement")
	var streams, observed int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*),count(*) FILTER(WHERE has_observation)
 FROM hosted_account_reporting_capture_status WHERE asset_id=$1 AND kind=5`, miner).Scan(&streams, &observed))
	require.Equal(t, 4, streams)
	require.Zero(t, observed, "raw events are not carried into the hourly hold")
}

// The 1055 runbook paused raw capture by dropping its trigger; 1077 accepts
// that state as well as an active hold.
func TestReportingEvidenceUpgradeFromPausedRawHold(t *testing.T) {
	f := setup(t)
	require.NoError(t, reportingHourMigration(t, f, "down"), "an empty hourly hold restores the raw hold")
	_, err := f.db.ExecContext(t.Context(), `DROP TRIGGER record_hosted_reporting_evidence ON hosted_account_history_sample`)
	require.NoError(t, err, "pause raw capture")
	require.NoError(t, reportingHourMigration(t, f, "up"), "the upgrade accepts a paused raw hold")
	requireRawHoldRetired(t, f)
	var hours, control, status, job bool
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT
 to_regclass('hosted_account_reporting_hour') IS NOT NULL,
 EXISTS(SELECT 1 FROM hosted_account_reporting_hour_control),
 to_regclass('hosted_account_reporting_capture_status') IS NOT NULL,
 EXISTS(SELECT 1 FROM timescaledb_information.jobs WHERE proc_name='hosted_reporting_hour_job')`).Scan(&hours, &control, &status, &job))
	require.True(t, hours)
	require.True(t, control)
	require.True(t, status)
	require.True(t, job)
}

func TestReportingEvidenceRollbackRestoresRawHold(t *testing.T) {
	f := setup(t)
	before := time.Now()
	require.NoError(t, reportingHourMigration(t, f, "down"), "an empty hourly hold can roll back")
	restored := rawHoldCatalog(t, f)
	require.Contains(t, restored, "rows epoch=1 evidence=0")
	var epoch time.Time
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT started_at FROM hosted_account_reporting_evidence_epoch`).Scan(&epoch))
	require.False(t, epoch.Before(before), "rollback starts a new raw-hold epoch")

	// The restored capture trigger appends raw events again.
	client := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(5, miner, 0)))
	tx, err := f.db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	defer tx.Rollback()
	_, err = tx.ExecContext(t.Context(), `INSERT INTO device_metrics(time,device_identifier,hash_rate_hs) SELECT $1,device_identifier,100e12 FROM device WHERE id=$2`, time.Now().UTC(), miner)
	require.NoError(t, err)
	var samples, events int
	require.NoError(t, tx.QueryRowContext(t.Context(), `SELECT (SELECT count(*) FROM hosted_account_history_sample),(SELECT count(*) FROM hosted_account_reporting_evidence)`).Scan(&samples, &events))
	require.Positive(t, samples)
	require.Equal(t, samples, events, "every normalized sample appends one raw event")
	require.NoError(t, tx.Rollback())

	// Reference: the same objects exactly as migration 1055 creates them here.
	require.NoError(t, f.execMigration(t, "001055_internal_hosted_reporting_evidence.down.sql"))
	require.NoError(t, f.execMigration(t, "001055_internal_hosted_reporting_evidence.up.sql"))
	require.Equal(t, rawHoldCatalog(t, f), restored, "rollback restores the 1055 objects exactly")

	require.NoError(t, reportingHourMigration(t, f, "up"), "the upgrade runs again after a rollback")
	requireRawHoldRetired(t, f)
	_, err = f.db.ExecContext(t.Context(), `INSERT INTO hosted_account_reporting_hour_gap(from_bucket,to_bucket,reason) VALUES($1,$1::timestamptz+interval '1 hour','test')`,
		time.Now().UTC().Truncate(time.Hour).Add(-48*time.Hour))
	require.NoError(t, err)
	require.ErrorContains(t, reportingHourMigration(t, f, "down"), "preserve hourly hosted reporting evidence", "a gap row also preserves the hold")
}

func (f fixture) execMigration(t *testing.T, name string) error {
	t.Helper()
	script, err := migrations.Current.ReadFile("current/" + name)
	require.NoError(t, err)
	if _, err = f.db.ExecContext(t.Context(), string(script)); err != nil {
		return fmt.Errorf("apply %s: %w", name, err)
	}
	return nil
}

// boardHourInterval allocates one hashboard whose interval starts before hour
// and returns the board and interval ids.
func boardHourInterval(t *testing.T, f fixture, hour time.Time) (int64, int64, int64) {
	t.Helper()
	client := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	module, controller := f.miningModule(t)
	board := f.moduleSelection(t, module).Boards[0].Id
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(7, board, 0)))
	historyEpoch(t, f, hour.Add(-time.Minute))
	var interval int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1
 WHERE kind=7 AND asset_id=$2 AND ended_at IS NULL RETURNING id`, hour.Add(-time.Minute), board).Scan(&interval))
	return board, controller, interval
}

// The narrowed 1055 hashboard trigger stays: topology stamping must not
// re-normalize samples, so it never marks a closed hour for revision.
func TestHostedReportingEvidenceTopologyStampingPreservesCorrections(t *testing.T) {
	for _, compressed := range []bool{false, true} {
		t.Run(fmt.Sprintf("compressed=%t", compressed), func(t *testing.T) {
			f := setup(t)
			at := time.Now().UTC().Truncate(time.Hour).Add(-24 * time.Hour)
			resetReportingHourControl(t, f, at)
			board, controller, interval := boardHourInterval(t, f, at)
			_, err := f.db.ExecContext(t.Context(), `INSERT INTO hashboard_metrics(hashboard_id,installation_id,time,collected_at,state,mining_state,hashrate_hs,power_w,temperature_c)
 SELECT $1,id,$2,$2,'active','',100e12,1000,50 FROM hashboard_installation WHERE hashboard_id=$1 AND removed_at IS NULL`, board, at)
			require.NoError(t, err)
			asOf := at.Add(time.Hour + reportingHourGrace)
			require.Equal(t, 1, closeReportingHours(t, f, asOf))
			require.Zero(t, reportingHourMarkerCount(t, f, interval))
			var topology int64
			require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO container_topology_version(device_id,fingerprint,topology) VALUES($1,'reporting-evidence','{}') RETURNING id`, controller).Scan(&topology))
			if compressed {
				for _, table := range []string{"hashboard_metrics", "hosted_account_history_sample"} {
					_, err = f.db.ExecContext(t.Context(), `SELECT compress_chunk(c) FROM show_chunks($1::regclass) c`, table)
					require.NoError(t, err)
				}
			}
			_, err = f.db.ExecContext(t.Context(), `SELECT stamp_hashboard_topology($1,$2,jsonb_build_array(jsonb_build_object('hashboard_id',$3::bigint,'time',$2::timestamptz)))`, topology, at, board)
			require.NoError(t, err)
			var stamped int64
			require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT topology_version_id FROM hashboard_metrics WHERE hashboard_id=$1 AND time=$2`, board, at).Scan(&stamped))
			require.Equal(t, topology, stamped, "the real topology write still completes")
			require.Zero(t, reportingHourMarkerCount(t, f, interval), "topology-only updates do not mark the closed hour")

			_, err = f.db.ExecContext(t.Context(), `UPDATE hashboard_metrics SET hashrate_hs=150e12 WHERE hashboard_id=$1 AND time=$2`, board, at)
			require.NoError(t, err)
			require.Positive(t, reportingHourMarkerCount(t, f, interval), "a real correction marks the closed hour")
			require.Equal(t, 1, reviseReportingHours(t, f, asOf))
			rows := reportingHourRows(t, f, interval)
			require.Len(t, rows, 2)
			require.Equal(t, 2, rows[1].revision)
			require.NotNil(t, rows[1].hashrateSum)
			require.InEpsilon(t, 150e12, *rows[1].hashrateSum, 1e-12)

			for _, correction := range []struct {
				query  string
				metric int
				value  float64
			}{
				{"UPDATE hashboard_metrics SET power_w=1100 WHERE hashboard_id=$1 AND time=$2", 2, 1100},
				{"UPDATE hashboard_metrics SET temperature_c=56 WHERE hashboard_id=$1 AND time=$2", 3, 56},
			} {
				_, err = f.db.ExecContext(t.Context(), correction.query, board, at)
				require.NoError(t, err)
				var value float64
				require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT value FROM hosted_account_history_sample WHERE metric=$1`, correction.metric).Scan(&value))
				require.Equal(t, correction.value, value, "each measurement can be corrected independently")
			}

			_, err = f.db.ExecContext(t.Context(), `UPDATE hashboard_metrics SET hashrate_hs=NULL WHERE hashboard_id=$1 AND time=$2`, board, at)
			require.NoError(t, err)
			var missing bool
			require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT value IS NULL AND complete=false FROM hosted_account_history_sample WHERE metric=1`).Scan(&missing))
			require.True(t, missing, "a missing measurement still replaces the prior value")
			_, err = f.db.ExecContext(t.Context(), `UPDATE hashboard_metrics SET time=$3 WHERE hashboard_id=$1 AND time=$2`, board, at, at.Add(time.Minute))
			require.NoError(t, err)
			var source time.Time
			require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT origin_time FROM hosted_account_history_sample WHERE metric=1`).Scan(&source))
			require.Equal(t, at.Add(time.Minute), source.UTC(), "source identity corrections still re-normalize")
			_, err = f.db.ExecContext(t.Context(), `DELETE FROM hashboard_metrics WHERE hashboard_id=$1`, board)
			require.NoError(t, err)
			var remaining int
			require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_account_history_sample`).Scan(&remaining))
			require.Zero(t, remaining)
			require.Equal(t, 1, reviseReportingHours(t, f, asOf), "the corrections collapse into one revision")
			rows = reportingHourRows(t, f, interval)
			require.Len(t, rows, 3)
			require.Zero(t, rows[2].hashrateReports, "deleted samples leave the hour reported as silent")
			require.Nil(t, rows[2].hashrateSum)
		})
	}
}

// Every column the narrowed hashboard trigger watches still re-normalizes.
func TestHostedReportingEvidenceHashboardCorrectionColumns(t *testing.T) {
	for _, correction := range []struct {
		column string
		value  any
		values [3]float64
	}{
		{"hashboard_id", nil, [3]float64{100e12, 1000, 50}},
		{"installation_id", nil, [3]float64{100e12, 1000, 50}},
		{"time", nil, [3]float64{100e12, 1000, 50}},
		{"collected_at", nil, [3]float64{100e12, 1000, 50}},
		{"state", "failed", [3]float64{100e12, 1000, 50}},
		{"state_observed_at", nil, [3]float64{100e12, 1000, 50}},
		{"hashrate_hs", 150e12, [3]float64{150e12, 1000, 50}},
		{"power_w", 1100, [3]float64{100e12, 1100, 50}},
		{"temperature_c", 56, [3]float64{100e12, 1000, 56}},
	} {
		t.Run(correction.column, func(t *testing.T) {
			f := setup(t)
			client := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
			module, _ := f.miningModule(t)
			boards := f.moduleSelection(t, module).Boards
			board := boards[0].Id
			require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(7, board, 0)))
			at := time.Now().UTC().Add(time.Second).Truncate(time.Microsecond)
			_, err := f.db.ExecContext(t.Context(), `INSERT INTO hashboard_metrics(hashboard_id,installation_id,time,collected_at,state,state_observed_at,mining_state,hashrate_hs,power_w,temperature_c)
 SELECT $1,id,$2,$2,'active',$2,'',100e12,1000,50 FROM hashboard_installation WHERE hashboard_id=$1 AND removed_at IS NULL`, board, at)
			require.NoError(t, err)
			var samples int
			require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_account_history_sample`).Scan(&samples))
			require.Equal(t, 3, samples, "every hashboard metric is normalized")
			value := correction.value
			source, receipt := at, at
			switch correction.column {
			case "hashboard_id":
				value = boards[1].Id
			case "installation_id":
				// A prior placement of this same board on the same controller/site is valid.
				var installation int64
				require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO hashboard_installation(hashboard_id,module_id,position,site_id,installed_at,last_seen_at,removed_at)
 SELECT hashboard_id,module_id,position,site_id,installed_at-INTERVAL '1 day',installed_at,installed_at
 FROM hashboard_installation WHERE hashboard_id=$1 AND removed_at IS NULL RETURNING id`, board).Scan(&installation))
				value = installation
			case "time":
				source = at.Add(time.Second)
				value = source
			case "collected_at":
				receipt = at.Add(time.Second)
				value = receipt
			case "state_observed_at":
				value = at.Add(time.Second)
			}
			result, err := f.db.ExecContext(t.Context(), fmt.Sprintf(`UPDATE hashboard_metrics SET %s=$3 WHERE hashboard_id=$1 AND time=$2`, correction.column), board, at, value)
			require.NoError(t, err)
			updated, err := result.RowsAffected()
			require.NoError(t, err)
			require.EqualValues(t, 1, updated)
			require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_account_history_sample`).Scan(&samples))
			if correction.column == "hashboard_id" {
				require.Zero(t, samples, "a board/installation mismatch removes the old samples without admitting replacements")
				return
			}
			require.Equal(t, 3, samples)
			for metric, expected := range correction.values {
				var actual float64
				var actualTime, origin, collected time.Time
				var key string
				require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT value,time,origin_time,origin_collected_at,origin_key FROM hosted_account_history_sample WHERE metric=$1`, metric+1).Scan(&actual, &actualTime, &origin, &collected, &key))
				require.Equal(t, expected, actual)
				require.Equal(t, source, actualTime.UTC())
				require.Equal(t, source, origin.UTC())
				require.Equal(t, receipt, collected.UTC())
				require.Equal(t, fmt.Sprint(board), key)
			}
		})
	}
}
