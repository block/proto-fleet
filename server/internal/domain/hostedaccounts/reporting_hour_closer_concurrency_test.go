package hostedaccounts_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/stretchr/testify/require"
)

const reportingHourSampleInsert = `INSERT INTO hosted_account_history_sample(time,interval_id,metric,value,complete,
 source_oldest_at,source_newest_at,source_id,origin_key,origin_time,origin_collected_at) VALUES($1,$2,1,$3,true,$1,$1,1,'race',$1,$1)`

// waitForReportingHourBlock waits until the waiter's backend is blocked by the holder's backend.
func waitForReportingHourBlock(ctx context.Context, t *testing.T, f fixture, waiter, holder int) {
	t.Helper()
	require.Eventually(t, func() bool {
		var blocked bool
		err := f.db.QueryRowContext(ctx, `SELECT $1=ANY(pg_blocking_pids($2))`, holder, waiter).Scan(&blocked)
		return err == nil && blocked
	}, 5*time.Second, 10*time.Millisecond)
}

// The close waits out a writer already inside the sample table; a writer that
// arrives after the close boundary leaves a marker. Every sample ends up in
// revision 1 or in a later revision, never in neither.
func TestReportingHourCloserLateWriterRace(t *testing.T) {
	f := setup(t)
	hour := time.Now().UTC().Truncate(time.Hour).Add(-2 * time.Hour)
	resetReportingHourControl(t, f, hour)
	interval, _ := reportingHourInterval(t, f, hour)
	asOf := hour.Add(time.Hour + reportingHourGrace)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	inFlight, err := f.db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer inFlight.Rollback()
	_, err = inFlight.ExecContext(ctx, reportingHourSampleInsert, hour.Add(10*time.Minute), interval, 100e12)
	require.NoError(t, err)
	var writerPID int
	require.NoError(t, inFlight.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&writerPID))

	closer, err := f.db.Conn(ctx)
	require.NoError(t, err)
	defer closer.Close()
	var closerPID int
	require.NoError(t, closer.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&closerPID))
	begun := make(chan error, 1)
	go func() {
		_, beginErr := closer.ExecContext(ctx, `SELECT hosted_reporting_hour_begin_close($1::timestamptz,lock_timeout_ms=>20000)`, asOf)
		begun <- beginErr
	}()
	waitForReportingHourBlock(ctx, t, f, closerPID, writerPID)
	require.True(t, hour.Equal(readReportingHourControl(t, f).closing), "the boundary waits for the in-flight writer")
	require.NoError(t, inFlight.Commit())
	select {
	case beginErr := <-begun:
		require.NoError(t, beginErr)
	case <-ctx.Done():
		t.Fatal("phase 1 did not finish after the writer committed")
	}
	require.True(t, hour.Add(time.Hour).Equal(readReportingHourControl(t, f).closing))
	require.Zero(t, reportingHourMarkerCount(t, f, interval), "a sample committed before the boundary needs no marker")

	// A writer arriving after the boundary is still in flight while phase 2 runs.
	late, err := f.db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer late.Rollback()
	_, err = late.ExecContext(ctx, reportingHourSampleInsert, hour.Add(20*time.Minute), interval, 200e12)
	require.NoError(t, err)
	require.Equal(t, 1, finishReportingHourClose(t, f, asOf))
	require.NoError(t, late.Commit())

	rows := reportingHourRows(t, f, interval)
	require.Len(t, rows, 1)
	require.Equal(t, 1, rows[0].hashrates, "revision 1 holds the sample committed before the boundary")
	require.Equal(t, 1, reportingHourMarkerCount(t, f, interval), "the late writer's marker survives phase 2")
	require.Equal(t, 1, reviseReportingHours(t, f, asOf))
	rows = reportingHourRows(t, f, interval)
	require.Len(t, rows, 2)
	require.Equal(t, 2, rows[1].revision)
	require.Equal(t, 2, rows[1].hashrates, "the late sample lands in revision 2")
	require.InEpsilon(t, 300e12, *rows[1].hashrateSum, 1e-12)
	require.Zero(t, reportingHourMarkerCount(t, f, interval))
}

// The close also waits out an allocation writer whose interval trigger ran
// before the boundary moved, so revision 1 sees its committed bounds. Neither
// write leaves a marker: the closer covers both.
// lock_timeout applies to each lock wait. Phase 1 takes two table locks and
// must spend one budget across both, or writers queue for twice the setting.
func TestReportingHourCloserSharesLockBudgetAcrossTables(t *testing.T) {
	f := setup(t)
	hour := time.Now().UTC().Truncate(time.Hour).Add(-2 * time.Hour)
	resetReportingHourControl(t, f, hour)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	hold := func(table string) *sql.Tx {
		tx, err := f.db.BeginTx(ctx, nil)
		require.NoError(t, err)
		_, err = tx.ExecContext(ctx, `LOCK TABLE `+table+` IN ROW EXCLUSIVE MODE`)
		require.NoError(t, err)
		return tx
	}
	samples, intervals := hold("hosted_account_history_sample"), hold("hosted_account_asset_interval")
	defer intervals.Rollback()
	// The sample writer leaves after three quarters of the budget; the
	// allocation writer never does.
	released := make(chan error, 1)
	go func() {
		time.Sleep(3 * time.Second)
		released <- samples.Rollback()
	}()
	start := time.Now()
	_, err := f.db.ExecContext(ctx, `SELECT hosted_reporting_hour_begin_close($1::timestamptz,lock_timeout_ms=>4000)`, hour.Add(time.Hour+reportingHourGrace))
	elapsed := time.Since(start)
	require.ErrorContains(t, err, "lock timeout")
	require.NoError(t, <-released)
	require.GreaterOrEqual(t, elapsed, 3*time.Second, "phase 1 waited for the sample writer")
	require.Less(t, elapsed, 6*time.Second, "the interval lock gets the rest of the budget, not a second full one")
	require.True(t, hour.Equal(readReportingHourControl(t, f).closing))
}

func TestReportingHourCloserWaitsForIntervalWriter(t *testing.T) {
	f := setup(t)
	hour := time.Now().UTC().Truncate(time.Hour).Add(-2 * time.Hour)
	resetReportingHourControl(t, f, hour)
	client := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	ending, _ := reportingHourMinerInterval(t, f, client.AccountID, hour)
	late := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(5, late, 0)))
	asOf := hour.Add(time.Hour + reportingHourGrace)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	inFlight, err := f.db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer inFlight.Rollback()
	_, err = inFlight.ExecContext(ctx, `UPDATE hosted_account_asset_interval SET ended_at=$2 WHERE id=$1`, ending, hour.Add(40*time.Minute))
	require.NoError(t, err)
	var inserted int64
	require.NoError(t, inFlight.QueryRowContext(ctx, `INSERT INTO hosted_account_asset_interval(organization_id,kind,asset_id,device_id,
 site_id,device_identifier,client_id,subclient_id,started_at,ended_at)
 SELECT organization_id,kind,asset_id,device_id,site_id,device_identifier,client_id,subclient_id,$2,$3
 FROM hosted_account_asset_interval WHERE kind=5 AND asset_id=$1 AND ended_at IS NULL RETURNING id`,
		late, hour.Add(20*time.Minute), hour.Add(50*time.Minute)).Scan(&inserted))
	var writerPID int
	require.NoError(t, inFlight.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&writerPID))

	closer, err := f.db.Conn(ctx)
	require.NoError(t, err)
	defer closer.Close()
	var closerPID int
	require.NoError(t, closer.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&closerPID))
	begun := make(chan error, 1)
	go func() {
		_, beginErr := closer.ExecContext(ctx, `SELECT hosted_reporting_hour_begin_close($1::timestamptz,lock_timeout_ms=>20000)`, asOf)
		begun <- beginErr
	}()
	waitForReportingHourBlock(ctx, t, f, closerPID, writerPID)
	require.True(t, hour.Equal(readReportingHourControl(t, f).closing), "the boundary waits for the in-flight allocation writer")
	require.NoError(t, inFlight.Commit())
	select {
	case beginErr := <-begun:
		require.NoError(t, beginErr)
	case <-ctx.Done():
		t.Fatal("phase 1 did not finish after the allocation writer committed")
	}
	require.True(t, hour.Add(time.Hour).Equal(readReportingHourControl(t, f).closing))
	require.Zero(t, reportingHourMarkerCount(t, f, ending))
	require.Zero(t, reportingHourMarkerCount(t, f, inserted))

	require.Equal(t, 2, finishReportingHourClose(t, f, asOf))
	rows := reportingHourRows(t, f, ending)
	require.Len(t, rows, 1)
	require.Equal(t, "closed", rows[0].reason)
	requireReportingHourWindow(t, rows[0], hour, hour.Add(40*time.Minute))
	rows = reportingHourRows(t, f, inserted)
	require.Len(t, rows, 1, "the inserted interval's hour is not missing")
	require.Equal(t, "closed", rows[0].reason)
	requireReportingHourWindow(t, rows[0], hour.Add(20*time.Minute), hour.Add(50*time.Minute))
	require.Zero(t, reviseReportingHours(t, f, asOf))
}

// A claim deletes only markers it can see. A writer still in flight for an
// already-marked hour keeps its own marker, without either side waiting.
func TestReportingHourRevisionKeepsInFlightWriterMarker(t *testing.T) {
	f := setup(t)
	hour := time.Now().UTC().Truncate(time.Hour).Add(-3 * time.Hour)
	resetReportingHourControl(t, f, hour)
	interval, _ := reportingHourInterval(t, f, hour)
	asOf := hour.Add(time.Hour + reportingHourGrace)
	_, err := f.db.ExecContext(t.Context(), reportingHourSampleInsert, hour.Add(10*time.Minute), interval, 100e12)
	require.NoError(t, err)
	require.Equal(t, 1, closeReportingHours(t, f, asOf))
	_, err = f.db.ExecContext(t.Context(), reportingHourSampleInsert, hour.Add(20*time.Minute), interval, 200e12)
	require.NoError(t, err)
	require.Equal(t, 1, reportingHourMarkerCount(t, f, interval))

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	writer, err := f.db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer writer.Rollback()
	_, err = writer.ExecContext(ctx, reportingHourSampleInsert, hour.Add(30*time.Minute), interval, 400e12)
	require.NoError(t, err)
	var written int
	require.NoError(t, f.db.QueryRowContext(ctx, `SELECT hosted_reporting_hour_revise($1)`, asOf).Scan(&written), "the revision step does not wait for the writer")
	require.Equal(t, 1, written)
	require.NoError(t, writer.Commit())

	require.Equal(t, 1, reportingHourMarkerCount(t, f, interval), "the in-flight writer's marker survives the claim")
	rows := reportingHourRows(t, f, interval)
	require.Len(t, rows, 2)
	require.Equal(t, 2, rows[1].hashrates)
	require.InEpsilon(t, 300e12, *rows[1].hashrateSum, 1e-12)
	require.Equal(t, 1, reviseReportingHours(t, f, asOf))
	rows = reportingHourRows(t, f, interval)
	require.Len(t, rows, 3)
	require.Equal(t, 3, rows[2].revision)
	require.Equal(t, 3, rows[2].hashrates)
	require.InEpsilon(t, 700e12, *rows[2].hashrateSum, 1e-12)
	require.Zero(t, reportingHourMarkerCount(t, f, interval))
}

func TestReportingHourConcurrentClosesWriteOneRevision(t *testing.T) {
	f := setup(t)
	hour := time.Now().UTC().Truncate(time.Hour).Add(-4 * time.Hour)
	resetReportingHourControl(t, f, hour)
	client := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	for range 3 {
		interval, _ := reportingHourMinerInterval(t, f, client.AccountID, hour)
		for offset := range 3 {
			_, err := f.db.ExecContext(t.Context(), reportingHourSampleInsert, hour.Add(time.Duration(offset)*time.Hour+10*time.Minute), interval, 100e12)
			require.NoError(t, err)
		}
	}
	asOf := hour.Add(3*time.Hour + reportingHourGrace)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	start := make(chan struct{})
	results := make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			if _, err := f.db.ExecContext(ctx, `SELECT hosted_reporting_hour_begin_close($1)`, asOf); err != nil {
				results <- err
				return
			}
			_, err := f.db.ExecContext(ctx, `SELECT hosted_reporting_hour_finish_close($1)`, asOf)
			results <- err
		}()
	}
	close(start)
	for range 2 {
		require.NoError(t, <-results)
	}
	var rows, hours, revision int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*),count(DISTINCT (interval_id,bucket)),max(revision) FROM hosted_account_reporting_hour`).Scan(&rows, &hours, &revision))
	require.Equal(t, 9, rows)
	require.Equal(t, 9, hours, "exactly one row per interval-hour")
	require.Equal(t, 1, revision)
	require.True(t, hour.Add(3*time.Hour).Equal(readReportingHourControl(t, f).closed))
}
