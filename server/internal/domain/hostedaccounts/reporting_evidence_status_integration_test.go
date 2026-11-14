package hostedaccounts_test

import (
	"database/sql"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/stretchr/testify/require"
)

type reportingStreamStatusRow struct {
	clientID                int64
	endedAt                 sql.NullTime
	hasObservation          bool
	observationBucket       sql.NullTime
	observationRevision     sql.NullInt32
	observationCapturedAt   sql.NullTime
	observationSourceNewest sql.NullTime
	closingThrough          time.Time
	closedThrough           time.Time
	lagSeconds              float64
	lastClosedBucket        sql.NullTime
	lastClosedNoSourceData  sql.NullBool
	missingHours, gapHours  int
	gapFrom, gapTo          sql.NullTime
	gapReason               sql.NullString
	gapRecordedAt           sql.NullTime
}

// reportingStreamStatus reads the status rows of one interval's metric stream.
func reportingStreamStatus(t *testing.T, f fixture, interval int64, source, metric int) reportingStreamStatusRow {
	t.Helper()
	var s reportingStreamStatusRow
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT client_id,allocation_ended_at,has_observation,
 last_observation_bucket,last_observation_revision,last_observation_captured_at,last_observation_source_newest_at,
 closing_through,closed_through,closer_lag_seconds,last_closed_bucket,last_closed_no_source_data,missing_hours,gap_hours,
 last_gap_from_bucket,last_gap_to_bucket,last_gap_reason,last_gap_recorded_at
 FROM hosted_account_reporting_capture_status WHERE interval_id=$1 AND source_id=$2 AND metric=$3`, interval, source, metric).Scan(
		&s.clientID, &s.endedAt, &s.hasObservation,
		&s.observationBucket, &s.observationRevision, &s.observationCapturedAt, &s.observationSourceNewest,
		&s.closingThrough, &s.closedThrough, &s.lagSeconds, &s.lastClosedBucket, &s.lastClosedNoSourceData, &s.missingHours, &s.gapHours,
		&s.gapFrom, &s.gapTo, &s.gapReason, &s.gapRecordedAt))
	return s
}

func requireReportingTime(t *testing.T, want time.Time, got sql.NullTime, msgAndArgs ...any) {
	t.Helper()
	require.True(t, got.Valid, msgAndArgs...)
	require.True(t, want.Equal(got.Time), "want %s, got %s", want, got.Time)
}

func TestReportingCaptureStatusNeverObservedAndReallocated(t *testing.T) {
	f := setup(t)
	hour := time.Now().UTC().Truncate(time.Hour).Add(-3 * time.Hour)
	resetReportingHourControl(t, f, hour)
	client := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	next := f.client(t, "Next customer", f.user, hostedaccounts.AllCapabilities)
	interval, miner := reportingHourMinerInterval(t, f, client.AccountID, hour)

	status := reportingStreamStatus(t, f, interval, 1, 1)
	require.False(t, status.hasObservation, "a never-observed allocation remains visible")
	require.False(t, status.observationCapturedAt.Valid)
	require.False(t, status.lastClosedBucket.Valid)
	require.Zero(t, status.missingHours, "no hour has closed yet")
	require.True(t, hour.Equal(status.closedThrough))

	insertReportingHourSamples(t, f, interval, reportingHourSample{at: hour.Add(10 * time.Minute), metric: 1, value: 100e12, complete: true, source: 1})
	require.Equal(t, 1, closeReportingHours(t, f, hour.Add(time.Hour+reportingHourGrace)))
	_, err := f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset SET client_id=$1,revision=revision+1 WHERE kind=5 AND asset_id=$2`, next.ID, miner)
	require.NoError(t, err)

	ended := reportingStreamStatus(t, f, interval, 1, 1)
	require.True(t, ended.endedAt.Valid, "an interval ended inside the window stays listed")
	require.Equal(t, client.ID, ended.clientID)
	require.True(t, ended.hasObservation)
	requireReportingTime(t, hour, ended.observationBucket)
	var current int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT id FROM hosted_account_asset_interval WHERE kind=5 AND asset_id=$1 AND ended_at IS NULL`, miner).Scan(&current))
	var unseen int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_account_reporting_capture_status
 WHERE interval_id=$1 AND client_id=$2 AND NOT has_observation`, current, next.ID).Scan(&unseen))
	require.Equal(t, 4, unseen, "reallocation does not inherit the previous capture")

	// Intervals that ended before the correction window are no longer listed.
	old, _ := reportingHourMinerInterval(t, f, client.AccountID, hour)
	_, err = f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=now()-interval '20 days',ended_at=now()-interval '10 days' WHERE id=$1`, old)
	require.NoError(t, err)
	var listed int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_account_reporting_capture_status WHERE interval_id=$1`, old).Scan(&listed))
	require.Zero(t, listed)
}

func TestReportingCaptureStatusCountsMissingHoursAndLag(t *testing.T) {
	f := setup(t)
	hour := time.Now().UTC().Truncate(time.Hour).Add(-5 * time.Hour)
	resetReportingHourControl(t, f, hour)
	client := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	closedInterval, _ := reportingHourMinerInterval(t, f, client.AccountID, hour)
	require.Equal(t, 3, closeReportingHours(t, f, hour.Add(3*time.Hour+reportingHourGrace)))

	status := reportingStreamStatus(t, f, closedInterval, 1, 1)
	require.Zero(t, status.missingHours)
	require.True(t, hour.Add(3*time.Hour).Equal(status.closedThrough), "watermarks are exclusive hour ends")
	require.True(t, status.closingThrough.Equal(status.closedThrough))
	requireReportingTime(t, hour.Add(2*time.Hour), status.lastClosedBucket)
	require.True(t, status.lastClosedNoSourceData.Valid && status.lastClosedNoSourceData.Bool, "no miner source reported in the latest closed hour")
	require.GreaterOrEqual(t, status.lagSeconds, (2 * time.Hour).Seconds(), "the closer is two hours behind now")
	require.Less(t, status.lagSeconds, (3 * time.Hour).Seconds())

	// An interval backdated into the last closed hour has no revision there
	// until the revision step records it.
	late, _ := reportingHourMinerInterval(t, f, client.AccountID, hour)
	_, err := f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1 WHERE id=$2`, hour.Add(2*time.Hour+30*time.Minute), late)
	require.NoError(t, err)
	require.Equal(t, 1, reportingStreamStatus(t, f, late, 1, 1).missingHours, "one absent revision-1 row inside the window")
	require.Zero(t, reportingStreamStatus(t, f, closedInterval, 1, 1).missingHours)
	require.Equal(t, 1, reviseReportingHours(t, f, hour.Add(3*time.Hour+reportingHourGrace)))
	require.Zero(t, reportingStreamStatus(t, f, late, 1, 1).missingHours, "an unattributable row covers the hour")
}

func TestReportingCaptureStatusRespectsHoldStart(t *testing.T) {
	f := setup(t)
	hold := time.Now().UTC().Truncate(time.Hour).Add(-2 * time.Hour)
	resetReportingHourControl(t, f, hold)
	interval, _ := reportingHourInterval(t, f, hold.Add(-5*time.Hour))
	require.Equal(t, 1, closeReportingHours(t, f, hold.Add(time.Hour+reportingHourGrace)))
	status := reportingStreamStatus(t, f, interval, 1, 1)
	require.Zero(t, status.missingHours, "hours before hold_started_at are outside the hold")
	require.Zero(t, status.gapHours)
	require.False(t, status.gapFrom.Valid)
}

func TestReportingCaptureStatusReportsGapsSeparately(t *testing.T) {
	f := setup(t)
	// A closer that fell behind past the correction window records a gap.
	now := time.Now().UTC()
	hold := now.Truncate(time.Hour).Add(-12 * 24 * time.Hour)
	resetReportingHourControl(t, f, hold)
	client := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	interval, _ := reportingHourMinerInterval(t, f, client.AccountID, hold)
	beginReportingHourClose(t, f, now)
	finishReportingHourClose(t, f, now)
	var gapStart, floor time.Time
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT from_bucket,to_bucket FROM hosted_account_reporting_hour_gap`).Scan(&gapStart, &floor))
	require.True(t, hold.Equal(gapStart))
	status := reportingStreamStatus(t, f, interval, 1, 1)
	require.Zero(t, status.missingHours, "gap hours are not missing hours")
	require.Equal(t, int(floor.Sub(hold).Hours()), status.gapHours)
	requireReportingTime(t, hold, status.gapFrom, "a gap older than the window is still the latest gap")
	requireReportingTime(t, floor, status.gapTo)
	require.Equal(t, sql.NullString{String: "closer fell behind past the correction window", Valid: true}, status.gapReason)
	require.True(t, status.gapRecordedAt.Valid)

	// Inside the window, a gap covers hours that would otherwise be missing.
	backdated, _ := reportingHourMinerInterval(t, f, client.AccountID, hold)
	_, err := f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1 WHERE id=$2`, floor, backdated)
	require.NoError(t, err)
	closed := reportingStreamStatus(t, f, backdated, 1, 1).closedThrough
	hours := int(closed.Sub(floor).Hours())
	require.Positive(t, hours)
	require.Equal(t, hours, reportingStreamStatus(t, f, backdated, 1, 1).missingHours)
	_, err = f.db.ExecContext(t.Context(), `INSERT INTO hosted_account_reporting_hour_gap(from_bucket,to_bucket,reason) VALUES($1,$2,'operator gap')`, floor, floor.Add(2*time.Hour))
	require.NoError(t, err)
	status = reportingStreamStatus(t, f, backdated, 1, 1)
	require.Equal(t, hours-2, status.missingHours)
	require.Equal(t, 2, status.gapHours, "only gaps overlapping the interval count")
	requireReportingTime(t, floor, status.gapFrom)
	require.Equal(t, "operator gap", status.gapReason.String)
	status = reportingStreamStatus(t, f, interval, 1, 1)
	require.Equal(t, int(floor.Sub(hold).Hours())+2, status.gapHours)
	requireReportingTime(t, floor.Add(2*time.Hour), status.gapTo, "the latest gap is the most recent one")
}

func TestReportingCaptureStatusObservationStaysInsideWindow(t *testing.T) {
	f := setup(t)
	hour := time.Now().UTC().Truncate(time.Hour)
	interval, _ := reportingHourInterval(t, f, hour.Add(-20*24*time.Hour))
	reportingHourSnapshot(t, f, interval)
	observed := func(bucket time.Time) {
		t.Helper()
		_, err := f.db.ExecContext(t.Context(), `INSERT INTO hosted_account_reporting_hour(interval_id,bucket,revision,capture_reason,no_source_data,
 window_start,window_end,hashrate_report_count,hashrate_count,hashrate_positive_count,hashrate_sum,hashrate_complete,
 hashrate_source_oldest_at,hashrate_source_newest_at,power_report_count,power_count,
 temperature_report_count,temperature_count,state_report_count,state_count)
 VALUES($1,$2,1,'closed',false,$2,$2::timestamptz+interval '1 hour',1,1,1,100e12,true,$2,$2,0,0,0,0,0,0)`, interval, bucket)
		require.NoError(t, err)
	}
	observed(hour.Add(-10 * 24 * time.Hour))
	require.False(t, reportingStreamStatus(t, f, interval, 1, 1).hasObservation, "a value older than the correction window is not an observation")
	recent := hour.Add(-2 * 24 * time.Hour)
	observed(recent)
	status := reportingStreamStatus(t, f, interval, 1, 1)
	require.True(t, status.hasObservation)
	requireReportingTime(t, recent, status.observationBucket)
}

func TestReportingCaptureStatusObservationFollowsLatestHour(t *testing.T) {
	f := setup(t)
	hour := time.Now().UTC().Truncate(time.Hour).Add(-4 * time.Hour)
	resetReportingHourControl(t, f, hour)
	client := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	interval, _ := reportingHourMinerInterval(t, f, client.AccountID, hour)
	stateOnly, _ := reportingHourMinerInterval(t, f, client.AccountID, hour)
	insertReportingHourSamples(t, f, interval,
		reportingHourSample{at: hour.Add(10 * time.Minute), metric: 1, value: 100e12, complete: true, source: 1},
		reportingHourSample{at: hour.Add(70 * time.Minute), metric: 1, value: 110e12, complete: true, source: 1},
		reportingHourSample{at: hour.Add(130 * time.Minute), metric: 4, value: 3, source: 2},
	)
	insertReportingHourSamples(t, f, stateOnly, reportingHourSample{at: hour.Add(20 * time.Minute), metric: 4, value: 3, source: 2})
	asOf := hour.Add(3*time.Hour + reportingHourGrace)
	require.Equal(t, 6, closeReportingHours(t, f, asOf))

	hashrate := reportingStreamStatus(t, f, interval, 1, 1)
	require.True(t, hashrate.hasObservation)
	requireReportingTime(t, hour.Add(time.Hour), hashrate.observationBucket, "a later state-only hour does not move hashrate freshness")
	require.Equal(t, sql.NullInt32{Int32: 1, Valid: true}, hashrate.observationRevision)
	requireReportingTime(t, hour.Add(70*time.Minute), hashrate.observationSourceNewest)
	rows := reportingHourRows(t, f, interval)
	require.Len(t, rows, 3)
	requireReportingTime(t, rows[1].capturedAt, hashrate.observationCapturedAt)
	state := reportingStreamStatus(t, f, interval, 2, 4)
	requireReportingTime(t, hour.Add(2*time.Hour), state.observationBucket)
	require.False(t, reportingStreamStatus(t, f, interval, 1, 3).hasObservation, "temperature was never reported")
	require.True(t, reportingStreamStatus(t, f, stateOnly, 2, 4).hasObservation)
	require.False(t, reportingStreamStatus(t, f, stateOnly, 1, 1).hasObservation, "a state-only observation does not make hashrate observed")

	// A late revision of an older hour leaves stream freshness unchanged.
	insertReportingHourSamples(t, f, interval, reportingHourSample{at: hour.Add(20 * time.Minute), metric: 1, value: 90e12, complete: true, source: 1})
	require.Equal(t, 1, reviseReportingHours(t, f, asOf))
	unchanged := reportingStreamStatus(t, f, interval, 1, 1)
	require.Equal(t, hashrate.observationBucket, unchanged.observationBucket)
	require.Equal(t, hashrate.observationRevision, unchanged.observationRevision)
	require.Equal(t, hashrate.observationCapturedAt, unchanged.observationCapturedAt)
	require.Equal(t, hashrate.observationSourceNewest, unchanged.observationSourceNewest)
	require.Len(t, reportingHourRows(t, f, interval), 4, "the old hour did get a revision")

	// A revision of the latest observed hour moves it.
	insertReportingHourSamples(t, f, interval, reportingHourSample{at: hour.Add(80 * time.Minute), metric: 1, value: 120e12, complete: true, source: 1})
	require.Equal(t, 1, reviseReportingHours(t, f, asOf))
	revised := reportingStreamStatus(t, f, interval, 1, 1)
	requireReportingTime(t, hour.Add(time.Hour), revised.observationBucket)
	require.Equal(t, sql.NullInt32{Int32: 2, Valid: true}, revised.observationRevision)
	requireReportingTime(t, hour.Add(80*time.Minute), revised.observationSourceNewest)
	require.True(t, revised.observationCapturedAt.Time.After(hashrate.observationCapturedAt.Time))

	// A revision that retracts every value leaves the hour unobserved.
	_, err := f.db.ExecContext(t.Context(), `DELETE FROM hosted_account_history_sample WHERE interval_id=$1 AND metric=1 AND time>=$2`, interval, hour.Add(time.Hour))
	require.NoError(t, err)
	// The hour lost its only site source, so the state-only interval's
	// no_source_data flag is revised too.
	require.Equal(t, 2, reviseReportingHours(t, f, asOf))
	requireReportingTime(t, hour, reportingStreamStatus(t, f, interval, 1, 1).observationBucket)
}
