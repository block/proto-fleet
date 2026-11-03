package hostedaccounts_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/infrastructure/timescaledb"
	"github.com/stretchr/testify/require"
)

func TestHostedReportingEvidenceConcurrentFirstCaptureRetriesStateTick(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	a := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	b := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	warm := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(5, a, 0), selected(5, b, 0)))
	at := time.Now().UTC().Truncate(time.Microsecond).Add(time.Second)
	// Create source and normalized chunks before introducing concurrent writers.
	_, err := f.db.ExecContext(t.Context(), `INSERT INTO device_metrics(time,device_identifier,hash_rate_hs) SELECT $1,device_identifier,100e12 FROM device WHERE id=$2`, at, warm)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `INSERT INTO miner_state_snapshots(time,org_id,site_id,device_identifier,state) SELECT $1,org_id,site_id,device_identifier,3 FROM device WHERE id=$2`, at.Add(-time.Second), warm)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `INSERT INTO hosted_account_history_sample(time,interval_id,metric,value,source_oldest_at,source_newest_at,source_id,origin_key,origin_time,origin_collected_at)
 SELECT $1,id,1,100e12,$1,$1,1,'chunk-warmup',$1,$1 FROM hosted_account_asset_interval WHERE asset_id=$2 AND kind=5`, at, a)
	require.NoError(t, err)
	// Warmup belongs to an earlier, closed allocation, leaving both current
	// intervals without snapshots while keeping all physical chunks committed.
	_, err = f.db.ExecContext(t.Context(), `UPDATE device SET device_identifier=device_identifier || '-new' WHERE id=$1`, a)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `CREATE SEQUENCE reporting_state_first_event;
 CREATE FUNCTION hold_first_reporting_state() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN
  IF NEW.source_id=2 AND nextval('reporting_state_first_event')=1 THEN
   PERFORM set_config('deadlock_timeout','1s',true);
   PERFORM pg_advisory_xact_lock(10480,NEW.interval_id::integer);
   PERFORM pg_advisory_xact_lock(10481,1);
  END IF;
  RETURN NEW;
 END $$;
 CREATE TRIGGER hold_first_reporting_state BEFORE INSERT ON hosted_account_reporting_evidence
 FOR EACH ROW EXECUTE FUNCTION hold_first_reporting_state()`)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	metrics, err := f.db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer metrics.Rollback()
	_, err = metrics.ExecContext(ctx, `SET LOCAL deadlock_timeout = '1min'`)
	require.NoError(t, err)
	gate, err := f.db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer gate.Rollback()
	// Cancel blocked statements before transaction cleanup if an assertion fails.
	defer cancel()
	_, err = gate.ExecContext(ctx, `SELECT pg_advisory_xact_lock(10481,1)`)
	require.NoError(t, err)
	var metricsPID int
	require.NoError(t, metrics.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&metricsPID))
	store, err := timescaledb.NewTelemetryStore(f.db, timescaledb.DefaultConfig())
	require.NoError(t, err)
	finished := make(chan error, 1)
	go func() { finished <- store.InsertMinerStateSnapshot(ctx, at) }()
	var firstInterval int64
	var statePID int
	require.Eventually(t, func() bool {
		return f.db.QueryRowContext(ctx, `SELECT objid::bigint,pid FROM pg_locks WHERE locktype='advisory' AND classid=10480 AND granted AND database=(SELECT oid FROM pg_database WHERE datname=current_database())`).Scan(&firstInterval, &statePID) == nil
	}, 3*time.Second, 10*time.Millisecond)
	var first int64
	require.NoError(t, f.db.QueryRowContext(ctx, `SELECT asset_id FROM hosted_account_asset_interval WHERE id=$1`, firstInterval).Scan(&first))
	require.Contains(t, []int64{a, b}, first)
	other := a
	if first == a {
		other = b
	}
	insertMetric := func(id int64) error {
		_, insertErr := metrics.ExecContext(ctx, `INSERT INTO device_metrics(time,device_identifier,hash_rate_hs) SELECT $1,device_identifier,100e12 FROM device WHERE id=$2`, at, id)
		if insertErr != nil {
			return fmt.Errorf("insert metric for device %d: %w", id, insertErr)
		}
		return nil
	}
	require.NoError(t, insertMetric(other))
	metricFinished := make(chan error, 1)
	go func() { metricFinished <- insertMetric(first) }()
	// Queue metrics behind state's first snapshot while state still waits at the
	// gate. Releasing the gate then starts a new state wait with the cycle already
	// formed, so its first deadlock check must see it, even under scheduler delays.
	require.Eventually(t, func() bool {
		var blocked bool
		e := f.db.QueryRowContext(ctx, `SELECT $1=ANY(pg_blocking_pids($2))`, statePID, metricsPID).Scan(&blocked)
		return e == nil && blocked
	}, 3*time.Second, 10*time.Millisecond)
	require.NoError(t, gate.Commit())
	select {
	case metricErr := <-metricFinished:
		require.NoError(t, metricErr, "state must detect the deadlock before the metrics transaction")
	case <-ctx.Done():
		t.Fatal("metrics insert did not complete after the state deadlock")
	}
	require.NoError(t, metrics.Commit())
	select {
	case stateErr := <-finished:
		require.NoError(t, stateErr, "the whole state tick must retry after the deadlock")
	case <-ctx.Done():
		t.Fatal("state tick did not complete after the metrics batch committed")
	}
	var rows, events int
	require.NoError(t, f.db.QueryRowContext(ctx, `SELECT count(*) FROM miner_state_snapshots WHERE time=$1`, at).Scan(&rows))
	require.Equal(t, 3, rows, "retry preserves the original tick time for the whole fleet")
	require.NoError(t, f.db.QueryRowContext(ctx, `SELECT count(*) FROM hosted_account_reporting_evidence WHERE source_id=2 AND origin_time=$1`, at).Scan(&events))
	require.Equal(t, 2, events, "the aborted attempt contributes no partial evidence")
}
