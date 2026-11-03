package hostedaccounts_test

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/migrations"
	"github.com/stretchr/testify/require"
)

func TestHostedReportingEvidenceCorrectionsSurviveChartExpiry(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(5, miner, 0)))
	day := time.Now().UTC().Truncate(24 * time.Hour).Add(-40 * 24 * time.Hour)
	historyEpoch(t, f, day)
	_, err := f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1 WHERE kind=5 AND asset_id=$2`, day, miner)
	require.NoError(t, err)
	for _, value := range []float64{100e12, 200e12} {
		_, err = f.db.ExecContext(t.Context(), `INSERT INTO device_metrics(time,device_identifier,hash_rate_hs,power_w) SELECT $1,device_identifier,$2,3000 FROM device WHERE id=$3 ON CONFLICT(time,device_identifier) DO UPDATE SET hash_rate_hs=EXCLUDED.hash_rate_hs`, day.Add(time.Minute), value, miner)
		require.NoError(t, err)
	}
	rows, err := f.db.QueryContext(t.Context(), `SELECT operation,value,transaction_id,captured_at,epoch_started_at,origin_time FROM hosted_account_reporting_evidence WHERE metric=1 ORDER BY event_id`)
	require.NoError(t, err)
	defer rows.Close()
	var operations []string
	var values []float64
	var transactions []int64
	for rows.Next() {
		var operation string
		var value float64
		var transaction int64
		var captured, epoch, origin time.Time
		require.NoError(t, rows.Scan(&operation, &value, &transaction, &captured, &epoch, &origin))
		operations = append(operations, operation)
		values = append(values, value)
		transactions = append(transactions, transaction)
		require.False(t, captured.Before(epoch))
		require.True(t, origin.Before(epoch), "old source time is not the hold capture boundary")
	}
	require.NoError(t, rows.Err())
	require.Equal(t, []string{"INSERT", "DELETE", "INSERT"}, operations)
	require.Equal(t, []float64{100e12, 100e12, 200e12}, values)
	require.Equal(t, transactions[1], transactions[2], "correction retains deletion and replacement in one transaction")
	var before, after int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_account_reporting_evidence`).Scan(&before))
	_, err = f.db.ExecContext(t.Context(), `SELECT drop_chunks('hosted_account_history_sample',older_than=>$1::timestamptz)`, day.Add(24*time.Hour))
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `SELECT drop_chunks('device_metrics',older_than=>$1::timestamptz)`, day.Add(24*time.Hour))
	require.NoError(t, err)
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_account_reporting_evidence`).Scan(&after))
	require.Equal(t, before, after, "chunk expiry neither removes evidence nor emits deletion events")
	var snapshots int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_account_reporting_allocation_snapshot`).Scan(&snapshots))
	require.Equal(t, 1, snapshots, "allocation identity is stored once per interval")
}

// Execute migration files only against this test's disposable database.
func reportingEvidenceMigration(t *testing.T, f fixture, direction string) error {
	t.Helper()
	script, err := migrations.Current.ReadFile("current/001055_internal_hosted_reporting_evidence." + direction + ".sql")
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), string(script))
	if err != nil {
		return fmt.Errorf("apply reporting evidence %s migration: %w", direction, err)
	}
	return nil
}

func TestHostedReportingEvidenceProspectiveUpgradeAndRollback(t *testing.T) {
	f := setup(t)
	require.NoError(t, reportingEvidenceMigration(t, f, "down"), "an empty hold can roll back")
	client := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(5, miner, 0)))
	at := time.Now().UTC().Add(-time.Hour)
	historyEpoch(t, f, at.Add(-time.Hour))
	_, err := f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1 WHERE asset_id=$2 AND kind=5`, at.Add(-time.Hour), miner)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `INSERT INTO device_metrics(time,device_identifier,hash_rate_hs) SELECT $1,device_identifier,100e12 FROM device WHERE id=$2`, at, miner)
	require.NoError(t, err)
	var samples int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_account_history_sample`).Scan(&samples))
	require.Positive(t, samples)
	_, err = f.db.ExecContext(t.Context(), `SELECT compress_chunk(c) FROM show_chunks('hosted_account_history_sample') c`)
	require.NoError(t, err)
	require.NoError(t, reportingEvidenceMigration(t, f, "up"))
	var events, snapshots int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_account_reporting_evidence`).Scan(&events))
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_account_reporting_allocation_snapshot`).Scan(&snapshots))
	require.Zero(t, events, "upgrade never copies existing samples")
	require.Zero(t, snapshots)
	// A later correction first captures the old version at its actual capture time.
	_, err = f.db.ExecContext(t.Context(), `UPDATE device_metrics SET hash_rate_hs=200e12 WHERE time=$1`, at)
	require.NoError(t, err)
	var oldSource, lateCapture bool
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT bool_and(origin_time<epoch_started_at),bool_and(captured_at>=epoch_started_at) FROM hosted_account_reporting_evidence`).Scan(&oldSource, &lateCapture))
	require.True(t, oldSource)
	require.True(t, lateCapture)
	require.ErrorContains(t, reportingEvidenceMigration(t, f, "down"), "preserve retained hosted reporting evidence")
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_account_reporting_evidence`).Scan(&events))
	require.Equal(t, samples*2, events, "refused rollback retains deletion and replacement images")
}

func TestHostedReportingEvidenceImmutableAndAtomic(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(5, miner, 0)))
	at := time.Now().UTC()
	_, err := f.db.ExecContext(t.Context(), `INSERT INTO device_metrics(time,device_identifier,hash_rate_hs) SELECT $1,device_identifier,100e12 FROM device WHERE id=$2`, at, miner)
	require.NoError(t, err)
	for _, table := range []string{"hosted_account_reporting_evidence", "hosted_account_reporting_allocation_snapshot", "hosted_account_reporting_evidence_epoch"} {
		for _, statement := range []string{"UPDATE " + table + " SET captured_at=clock_timestamp()", "DELETE FROM " + table, "TRUNCATE " + table + " CASCADE"} {
			if table == "hosted_account_reporting_evidence_epoch" {
				statement = strings.Replace(statement, "captured_at", "started_at", 1)
			}
			_, err = f.db.ExecContext(t.Context(), statement)
			require.ErrorContains(t, err, "hosted reporting evidence is append-only")
		}
	}
	var before int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_account_reporting_evidence`).Scan(&before))
	_, err = f.db.ExecContext(t.Context(), `ALTER TABLE hosted_account_reporting_evidence ADD CONSTRAINT evidence_test_failure CHECK(value IS NULL OR value<150e12) NOT VALID`)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `UPDATE device_metrics SET hash_rate_hs=200e12 WHERE time=$1`, at)
	require.ErrorContains(t, err, "evidence_test_failure")
	var raw, sample float64
	var after int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT hash_rate_hs FROM device_metrics WHERE time=$1`, at).Scan(&raw))
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT value FROM hosted_account_history_sample WHERE metric=1 AND time=$1`, at).Scan(&sample))
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_account_reporting_evidence`).Scan(&after))
	require.Equal(t, 100e12, raw)
	require.Equal(t, 100e12, sample)
	require.Equal(t, before, after, "failed capture rolls back prior deletion events too")
	_, err = f.db.ExecContext(t.Context(), `INSERT INTO device_metrics(time,device_identifier,hash_rate_hs) SELECT $1,device_identifier,200e12 FROM device WHERE id=$2`, at.Add(time.Minute), miner)
	require.ErrorContains(t, err, "evidence_test_failure")
	var count int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM device_metrics WHERE time=$1`, at.Add(time.Minute)).Scan(&count))
	require.Zero(t, count, "failed first capture rolls back the raw insert")
}

func TestHostedReportingEvidenceHashboardPayloadAndDirectUpdate(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	module, controller := f.miningModule(t)
	board := f.moduleSelection(t, module).Boards[0].Id
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(7, board, 0)))
	child, err := f.svc.CreateChild(t.Context(), f.user, f.org, client.AccountID, "Subclient", f.host, hostedaccounts.DefaultCapabilities)
	require.NoError(t, err)
	require.NoError(t, f.allocate(t, f.user, client.AccountID, child.AccountID, 1, selected(7, board, 1)))
	at := time.Now().UTC().Add(time.Second).Truncate(time.Microsecond)
	stored := at
	_, err = f.db.ExecContext(t.Context(), `INSERT INTO hashboard_metrics(hashboard_id,installation_id,time,collected_at,state,mining_state,hashrate_hs,power_w,temperature_c)
 SELECT $1,id,$2,$3,'active','',100e12,NULL,55 FROM hashboard_installation WHERE hashboard_id=$1 AND removed_at IS NULL`, board, stored, at)
	require.NoError(t, err)
	var savedClient, savedSubclient, savedDevice int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT client_id,subclient_id,device_id FROM hosted_account_reporting_allocation_snapshot`).Scan(&savedClient, &savedSubclient, &savedDevice))
	require.Equal(t, client.ID, savedClient)
	require.Equal(t, child.ID, savedSubclient)
	require.Equal(t, controller, savedDevice)
	var differences int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM (
 (SELECT time,interval_id,metric,value,complete,source_oldest_at,source_newest_at,source_id,origin_key,origin_time,origin_collected_at FROM hosted_account_history_sample
 EXCEPT SELECT time,interval_id,metric,value,complete,source_oldest_at,source_newest_at,source_id,origin_key,origin_time,origin_collected_at FROM hosted_account_reporting_evidence)
 UNION ALL
 (SELECT time,interval_id,metric,value,complete,source_oldest_at,source_newest_at,source_id,origin_key,origin_time,origin_collected_at FROM hosted_account_reporting_evidence
 EXCEPT SELECT time,interval_id,metric,value,complete,source_oldest_at,source_newest_at,source_id,origin_key,origin_time,origin_collected_at FROM hosted_account_history_sample)) difference`).Scan(&differences))
	require.Zero(t, differences, "every admitted scalar identity, window, nullable value and completeness survives")
	var missing bool
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT value IS NULL AND complete=false FROM hosted_account_reporting_evidence WHERE metric=2`).Scan(&missing))
	require.True(t, missing, "missing power is not invented as zero")
	_, err = f.db.ExecContext(t.Context(), `UPDATE hosted_account_history_sample SET value=200e12 WHERE metric=1`)
	require.NoError(t, err)
	var before, after float64
	var sameTransaction bool
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT b.value,a.value,b.transaction_id=a.transaction_id FROM hosted_account_reporting_evidence b JOIN hosted_account_reporting_evidence a ON a.event_id=b.event_id+1 WHERE b.operation='UPDATE' AND b.image='before' AND a.image='after'`).Scan(&before, &after, &sameTransaction))
	require.Equal(t, 100e12, before)
	require.Equal(t, 200e12, after)
	require.True(t, sameTransaction)
	// A current receipt cannot authorize a board sample before ownership.
	_, err = f.db.ExecContext(t.Context(), `INSERT INTO hashboard_metrics(hashboard_id,installation_id,time,collected_at,state,mining_state,hashrate_hs)
 SELECT $1,id,$2,$3,'active','',300e12 FROM hashboard_installation WHERE hashboard_id=$1 AND removed_at IS NULL`, board, at.Add(-time.Hour), at.Add(time.Second))
	require.NoError(t, err)
	var unauthorizedHash, missingPower int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FILTER(WHERE metric=1),count(*) FILTER(WHERE metric=2 AND value IS NULL AND complete=false) FROM hosted_account_reporting_evidence WHERE origin_collected_at=$1`, at.Add(time.Second)).Scan(&unauthorizedHash, &missingPower))
	require.Zero(t, unauthorizedHash, "current receipt never authorizes a contributor window before ownership")
	require.Zero(t, missingPower, "an out-of-window board sample cannot introduce missing-value evidence either")
}

func TestHostedReportingEvidenceCompressedCorrection(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(5, miner, 0)))
	at := time.Now().UTC().Truncate(time.Hour).Add(-24 * time.Hour)
	historyEpoch(t, f, at.Add(-time.Hour))
	_, err := f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1 WHERE asset_id=$2 AND kind=5`, at.Add(-time.Hour), miner)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `INSERT INTO device_metrics(time,device_identifier,hash_rate_hs) SELECT $1,device_identifier,100e12 FROM device WHERE id=$2`, at, miner)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `SELECT compress_chunk(c) FROM show_chunks('hosted_account_history_sample') c`)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `UPDATE device_metrics SET hash_rate_hs=200e12 WHERE time=$1`, at)
	require.NoError(t, err)
	var count int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_account_reporting_evidence WHERE metric=1`).Scan(&count))
	require.Equal(t, 3, count, "compressed samples retain correction deletion and replacement")
	_, err = f.db.ExecContext(t.Context(), `SELECT compress_chunk(c,if_not_compressed=>true,recompress=>true) FROM show_chunks('hosted_account_history_sample') c`)
	require.NoError(t, err)
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_account_reporting_evidence WHERE metric=1`).Scan(&count))
	require.Equal(t, 3, count, "recompression does not emit reporting evidence")
}

func TestHostedReportingEvidenceStateSources(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	module, _ := f.miningModule(t)
	board := f.moduleSelection(t, module).Boards[0].Id
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(5, miner, 0), selected(7, board, 0)))
	source := time.Now().UTC().Truncate(time.Microsecond)
	receipt := source.Add(time.Second)
	_, err := f.db.ExecContext(t.Context(), `INSERT INTO miner_state_snapshots(time,org_id,site_id,device_identifier,state) SELECT $1,org_id,site_id,device_identifier,3 FROM device WHERE id=$2`, source, miner)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `INSERT INTO hashboard_diagnostic_observation(hashboard_id,installation_id,collected_at,state,state_observed_at,mining_state) SELECT $1,id,$2,'active',$3,'' FROM hashboard_installation WHERE hashboard_id=$1 AND removed_at IS NULL`, board, receipt, source)
	require.NoError(t, err)
	rows, err := f.db.QueryContext(t.Context(), `SELECT source_id,metric,value,complete,source_oldest_at,source_newest_at,origin_time,origin_collected_at FROM hosted_account_reporting_evidence ORDER BY source_id`)
	require.NoError(t, err)
	defer rows.Close()
	var sources []int
	for rows.Next() {
		var id, metric int
		var value float64
		var complete sql.NullBool
		var oldest, newest, origin, collected time.Time
		require.NoError(t, rows.Scan(&id, &metric, &value, &complete, &oldest, &newest, &origin, &collected))
		sources = append(sources, id)
		require.Equal(t, 4, metric)
		require.Equal(t, 3.0, value, "miner and hashboard active states retain the same normalized code")
		require.False(t, complete.Valid, "state has no metric completeness flag")
		require.Equal(t, source, oldest.UTC())
		require.Equal(t, source, newest.UTC())
		expectedReceipt := source
		if id == 6 {
			expectedReceipt = receipt
		}
		require.Equal(t, expectedReceipt, origin.UTC())
		require.Equal(t, expectedReceipt, collected.UTC())
	}
	require.NoError(t, rows.Err())
	require.Equal(t, []int{2, 6}, sources)
}

func TestHostedReportingEvidencePreservesLegacyModuleIdentity(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Legacy customer", f.user, hostedaccounts.AllCapabilities)
	module, _ := f.miningModule(t)
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(6, module, 0)))
	f.legacyModuleHistory(t)
	at := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	historyEpoch(t, f, at.Add(-time.Minute))
	_, err := f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1 WHERE kind=6`, at.Add(-time.Minute))
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `INSERT INTO container_module_metrics(module_id,time,collected_at,hashrate_hs,power_w,temperature_c,coverage)
 VALUES($1,$2,$2,100e12,2000,55,jsonb_build_object(
 'hashrate',jsonb_build_object('oldest_at',$2::timestamptz,'newest_at',$2::timestamptz,'reporting',1,'expected',1,'topology_complete',true),
 'power',jsonb_build_object('oldest_at',$2::timestamptz,'newest_at',$2::timestamptz,'reporting',1,'expected',1,'topology_complete',true),
 'temperature',jsonb_build_object('oldest_at',$2::timestamptz,'newest_at',$2::timestamptz)))`, module, at)
	require.NoError(t, err)
	var events, kind int
	var asset, savedClient int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_account_reporting_evidence WHERE source_id=3`).Scan(&events))
	require.Equal(t, 3, events)
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT kind,asset_id,client_id FROM hosted_account_reporting_allocation_snapshot`).Scan(&kind, &asset, &savedClient))
	require.Equal(t, 6, kind, "historical module samples must not be reassigned to current hashboards")
	require.Equal(t, module, asset)
	require.Equal(t, client.ID, savedClient)
}

func TestHostedReportingEvidenceTopologyStampingPreservesCorrections(t *testing.T) {
	for _, compressed := range []bool{false, true} {
		t.Run(fmt.Sprintf("compressed=%t", compressed), func(t *testing.T) {
			f := setup(t)
			client := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
			module, controller := f.miningModule(t)
			board := f.moduleSelection(t, module).Boards[0].Id
			require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(7, board, 0)))
			at := time.Now().UTC().Truncate(time.Hour).Add(-24 * time.Hour)
			historyEpoch(t, f, at.Add(-time.Minute))
			_, err := f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1 WHERE kind=7 AND asset_id=$2`, at.Add(-time.Minute), board)
			require.NoError(t, err)
			_, err = f.db.ExecContext(t.Context(), `INSERT INTO hashboard_metrics(hashboard_id,installation_id,time,collected_at,state,mining_state,hashrate_hs,power_w,temperature_c)
 SELECT $1,id,$2,$2,'active','',100e12,1000,50 FROM hashboard_installation WHERE hashboard_id=$1 AND removed_at IS NULL`, board, at)
			require.NoError(t, err)
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
			var events int
			require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_account_reporting_evidence`).Scan(&events))
			require.Equal(t, 3, events, "topology-only updates do not create monitoring corrections")

			_, err = f.db.ExecContext(t.Context(), `UPDATE hashboard_metrics SET hashrate_hs=150e12 WHERE hashboard_id=$1 AND time=$2`, board, at)
			require.NoError(t, err)
			var operations, images []string
			var values []float64
			rows, err := f.db.QueryContext(t.Context(), `SELECT operation,image,value FROM hosted_account_reporting_evidence WHERE metric=1 ORDER BY event_id`)
			require.NoError(t, err)
			defer rows.Close()
			for rows.Next() {
				var operation, image string
				var value float64
				require.NoError(t, rows.Scan(&operation, &image, &value))
				operations = append(operations, operation)
				images = append(images, image)
				values = append(values, value)
			}
			require.NoError(t, rows.Err())
			require.Equal(t, []string{"INSERT", "DELETE", "INSERT"}, operations)
			require.Equal(t, []string{"after", "before", "after"}, images)
			require.Equal(t, []float64{100e12, 100e12, 150e12}, values)

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
			require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_account_reporting_evidence`).Scan(&events))
			require.Equal(t, 36, events, "real corrections retain every original, deletion and replacement image")
		})
	}
}

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
			// Compare complete normalized payloads with their retained before/after images.
			payload := func(table, filter string) string {
				t.Helper()
				var result string
				require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT COALESCE(jsonb_agg(to_jsonb(s) ORDER BY metric),'[]'::jsonb)::text FROM (
 SELECT time,interval_id,metric,value,complete,source_oldest_at,source_newest_at,source_id,origin_key,origin_time,origin_collected_at FROM `+table+` WHERE `+filter+`) s`).Scan(&result))
				return result
			}
			before := payload("hosted_account_history_sample", "true")
			require.NotEqual(t, "[]", before)
			var initialEvent int64
			require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT max(event_id) FROM hosted_account_reporting_evidence`).Scan(&initialEvent))
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
			require.Equal(t, before, payload("hosted_account_reporting_evidence", "operation='DELETE'"), "corrections retain the complete deleted sample images")
			after := payload("hosted_account_history_sample", "true")
			require.Equal(t, after, payload("hosted_account_reporting_evidence", fmt.Sprintf("operation='INSERT' AND event_id>%d", initialEvent)), "replacement samples and their retained images agree")
			var events, transactions, samples int
			require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*),count(DISTINCT transaction_id) FROM hosted_account_reporting_evidence WHERE event_id>$1`, initialEvent).Scan(&events, &transactions))
			require.Equal(t, 1, transactions, "deletion and replacement are captured atomically")
			require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_account_history_sample`).Scan(&samples))
			if correction.column == "hashboard_id" {
				require.Zero(t, samples, "a board/installation mismatch removes the old samples without admitting replacements")
				require.Equal(t, 3, events)
				return
			}
			require.Equal(t, 3, samples)
			require.Equal(t, 6, events, "every watched field deletes and replaces all three measurements")
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
