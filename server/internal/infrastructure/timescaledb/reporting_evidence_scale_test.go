package timescaledb

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

func (r *containerRollupRig) representativeSnapshot(offset time.Duration) *sdk.ContainerSnapshot {
	s := r.snapshot(offset, false, 100e12, 100e12)
	s.Tanks = s.Tanks[:1]
	tank := s.Tanks[0]
	module := tank.Modules[0]
	s.Tanks = nil
	for ti := int32(1); ti <= 6; ti++ {
		next := tank
		next.Position = ti
		next.Modules = nil
		for mi := int32(1); mi <= 16; mi++ {
			m := module
			m.Position = mi
			m.ReportedID = fmt.Sprintf("module-%d-%d", ti, mi)
			m.Hashboards = nil
			// Every module must own its mutable rollup.
			metrics := *module.Metrics
			rollup := *module.Metrics.Rollup
			metrics.Rollup = &rollup
			m.Metrics = &metrics
			for bi := int32(1); bi <= 3; bi++ {
				board := module.Hashboards[0]
				board.Position = bi
				board.Serial = fmt.Sprintf("board-%d-%d-%d", ti, mi, bi)
				m.Hashboards = append(m.Hashboards, board)
			}
			next.Modules = append(next.Modules, m)
		}
		power := *tank.Metrics
		power.PowerSource = fmt.Sprintf("pdu-%d", ti)
		power.SourceTimes = map[string]time.Time{power.PowerSource: s.ObservedAt}
		next.Metrics = &power
		s.Tanks = append(s.Tanks, next)
	}
	sdk.CalculateContainerRollups(s)
	return s
}

// The reporting estate below is one fully allocated CT1: 96 modules and 288
// allocated hashboards (kind 7), polled every 45 seconds.
const (
	scaleBoards       = 288
	scalePollsPerHour = 80
	scaleClosedHours  = 25
	scaleHistoryDays  = 30
)

// TestStoredContainerRollupsReportingEvidenceRepresentativeScale measures, for
// one fully allocated CT1: live ingestion with and without the correction
// marker triggers; closing one hour and a 24-hour catch-up run; marker volume
// and revision cost of corrections; capture-status cost after 30 days of hour
// rows; and evidence bytes per interval-hour.
func TestStoredContainerRollupsReportingEvidenceRepresentativeScale(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	r := newContainerRollupRig(t)
	ctx := t.Context()
	exec := func(query string, args ...any) int {
		t.Helper()
		result, err := r.db.ExecContext(ctx, query, args...)
		require.NoError(t, err)
		affected, err := result.RowsAffected()
		require.NoError(t, err)
		return int(affected)
	}
	// Corrections below rewrite many compressed hours in one statement, past
	// Timescale's default per-transaction decompression limit.
	execCompressed := func(query string, args ...any) int {
		t.Helper()
		tx, err := r.db.BeginTx(ctx, nil)
		require.NoError(t, err)
		_, err = tx.ExecContext(ctx, `SET LOCAL timescaledb.max_tuples_decompressed_per_dml_transaction=0`)
		require.NoError(t, err)
		result, err := tx.ExecContext(ctx, query, args...)
		require.NoError(t, err)
		affected, err := result.RowsAffected()
		require.NoError(t, err)
		require.NoError(t, tx.Commit())
		return int(affected)
	}
	scalar := func(query string, args ...any) (int, time.Duration) {
		t.Helper()
		var value int
		start := time.Now()
		require.NoError(t, r.db.QueryRowContext(ctx, query, args...).Scan(&value))
		return value, time.Since(start)
	}
	markers := func() int {
		t.Helper()
		n, _ := scalar(`SELECT count(*) FROM hosted_account_reporting_hour_marker`)
		return n
	}

	r.store(t, r.representativeSnapshot(0))
	var client int64
	require.NoError(t, r.db.QueryRowContext(ctx, `INSERT INTO hosted_account(account_id,organization_id,parent_id,depth,name,capabilities)
 SELECT gen_random_uuid()::text,organization_id,id,1,'Capture sizing',capabilities FROM hosted_account
 WHERE depth=0 AND organization_id=(SELECT org_id FROM device WHERE id=$1) RETURNING id`, r.databaseID).Scan(&client))
	exec(`INSERT INTO hosted_account_asset(organization_id,kind,asset_id,device_id,hashboard_id,client_id)
 SELECT d.org_id,7,i.hashboard_id,d.id,i.hashboard_id,$1 FROM hashboard_installation i JOIN device d ON d.id=i.device_id WHERE d.id=$2 AND i.removed_at IS NULL`, client, r.databaseID)

	// Closed hours are [first, first+25h): one steady-state close, then a
	// 24-hour catch-up run. The template's control row reflects template build
	// time, so restart the hold at first. Intervals start 30 days earlier so the
	// status view's missing-hour scan covers the whole correction window.
	first := time.Now().UTC().Truncate(time.Hour).Add(-(scaleClosedHours + 1) * time.Hour)
	history := first.Add(-scaleHistoryDays * 24 * time.Hour)
	exec(`UPDATE hosted_account_reporting_hour_control
 SET hold_started_at=$1,closing_through=$1,closed_through=$1,last_success_at=NULL WHERE singleton`, first)
	require.Equal(t, scaleBoards, exec(`UPDATE hosted_account_asset_interval SET started_at=$1 WHERE client_id=$2`, history, client))
	require.Zero(t, markers(), "moving an interval start behind the hold enqueues nothing")

	// Live ingestion, with and without the marker triggers, in the open hour.
	// Open hours never enqueue markers, so the enabled mode measures only the
	// trigger's admission check.
	enqueueTriggers := []string{"enqueue_hosted_reporting_hour_sample", "enqueue_hosted_reporting_hour_sample_update"}
	definitions := make([]string, 0, len(enqueueTriggers))
	for _, name := range enqueueTriggers {
		var definition string
		require.NoError(t, r.db.QueryRowContext(ctx, `SELECT pg_get_triggerdef(oid) FROM pg_trigger
 WHERE tgrelid='hosted_account_history_sample'::regclass AND tgname=$1`, name).Scan(&definition))
		definitions = append(definitions, definition)
		exec(`DROP TRIGGER ` + name + ` ON hosted_account_history_sample`) //nolint:gosec // Trigger names are constants.
	}
	r.base = time.Now().UTC().Add(time.Second).Truncate(time.Microsecond)
	const polls = scalePollsPerHour / 2
	measure := func(first int) []time.Duration {
		t.Helper()
		durations := make([]time.Duration, 0, polls)
		for poll := range polls {
			snapshot := r.representativeSnapshot(time.Duration(first+poll) * time.Millisecond)
			start := time.Now()
			r.store(t, snapshot)
			durations = append(durations, time.Since(start))
		}
		return durations
	}
	without := measure(0)
	for _, definition := range definitions {
		exec(definition)
	}
	with := measure(polls)
	var samples, metricSamples, states, pollTimes int
	require.NoError(t, r.db.QueryRowContext(ctx, `SELECT count(*),count(*) FILTER(WHERE source_id=5),count(*) FILTER(WHERE source_id=6),count(DISTINCT time)
 FROM hosted_account_history_sample`).Scan(&samples, &metricSamples, &states, &pollTimes))
	// Topology stamping must not re-normalize unchanged measurements.
	require.Equal(t, scaleBoards*4*scalePollsPerHour, samples, "each board stores only its three metrics and state observation")
	require.Equal(t, scaleBoards*3*scalePollsPerHour, metricSamples)
	require.Equal(t, scaleBoards*scalePollsPerHour, states)
	require.Equal(t, scalePollsPerHour, pollTimes)
	require.Zero(t, markers(), "samples in hours not yet closing never enqueue revision markers")

	// Replay the live hour into each closed hour at a 45-second cadence. Even
	// intervals never report temperature, so their temperature streams have no
	// observation in any hour row: the status view's slowest lookup.
	start := time.Now()
	loaded := exec(`WITH poll AS (
 SELECT p.time,(dense_rank() OVER (ORDER BY p.time)-1)::integer AS n FROM (SELECT DISTINCT time FROM hosted_account_history_sample) p)
INSERT INTO hosted_account_history_sample(time,interval_id,metric,value,complete,source_oldest_at,source_newest_at,
 source_id,origin_key,origin_time,origin_collected_at)
SELECT s.time+d.shift,s.interval_id,s.metric,s.value,s.complete,s.source_oldest_at+d.shift,s.source_newest_at+d.shift,
 s.source_id,s.origin_key,s.origin_time+d.shift,s.origin_collected_at+d.shift
FROM hosted_account_history_sample s JOIN poll p ON p.time=s.time
CROSS JOIN generate_series(0,$2-1) h
CROSS JOIN LATERAL (SELECT $1::timestamptz+h*INTERVAL '1 hour'+p.n*INTERVAL '45 seconds'-s.time AS shift) d
WHERE NOT (s.metric=hosted_history_metric_code('temperature') AND s.interval_id%2=0)`, first, scaleClosedHours)
	loadTime := time.Since(start)
	samplesPerHour := scalePollsPerHour * (scaleBoards*4 - scaleBoards/2)
	require.Equal(t, scaleClosedHours*samplesPerHour, loaded)
	require.Zero(t, markers())

	// Steady state: close one hour that is still uncompressed.
	closeHours := func(asOf time.Time, wantClosing time.Time, wantRows int) (time.Duration, time.Duration) {
		t.Helper()
		var closing time.Time
		start := time.Now()
		require.NoError(t, r.db.QueryRowContext(ctx, `SELECT hosted_reporting_hour_begin_close($1)`, asOf).Scan(&closing))
		phaseOne := time.Since(start)
		require.True(t, wantClosing.Equal(closing), "closing_through: want %s, got %s", wantClosing, closing)
		written, phaseTwo := scalar(`SELECT hosted_reporting_hour_finish_close($1)`, asOf)
		require.Equal(t, wantRows, written)
		return phaseOne, phaseTwo
	}
	steadyLock, steadyClose := closeHours(first.Add(time.Hour+30*time.Minute), first.Add(time.Hour), scaleBoards)

	// Catch-up: a closer 24 hours behind closes max_hours in one run. Raw
	// samples compress after six hours, so most of those hours are compressed.
	start = time.Now()
	compressed := exec(`SELECT compress_chunk(c,if_not_compressed=>true)
 FROM show_chunks('hosted_account_history_sample',older_than=>now()-INTERVAL '6 hours') c`)
	compressTime := time.Since(start)
	catchUpLock, catchUpClose := closeHours(first.Add(scaleClosedHours*time.Hour+30*time.Minute), first.Add(scaleClosedHours*time.Hour), (scaleClosedHours-1)*scaleBoards)
	var hours, minRows, maxRows int
	require.NoError(t, r.db.QueryRowContext(ctx, `SELECT count(*),min(n),max(n) FROM (
 SELECT h.bucket,count(*) AS n FROM hosted_account_reporting_hour h JOIN hosted_account_asset_interval i ON i.id=h.interval_id
 WHERE i.kind=7 AND h.revision=1 AND h.capture_reason='closed' GROUP BY h.bucket) b`).Scan(&hours, &minRows, &maxRows))
	require.Equal(t, scaleClosedHours, hours)
	require.Equal(t, scaleBoards, minRows, "every allocated hashboard gets one row per closed hour")
	require.Equal(t, scaleBoards, maxRows)
	require.Zero(t, markers(), "no writer touched a closing hour")

	// Source corrections replace normalized samples: delete, then insert. This
	// mirrors that path for every board's metrics at the given poll times.
	correct := func(times []time.Time) (int, time.Duration) {
		t.Helper()
		before := markers()
		start := time.Now()
		replaced := execCompressed(`WITH old AS (
 DELETE FROM hosted_account_history_sample WHERE source_id=5 AND time=ANY($1) RETURNING *)
INSERT INTO hosted_account_history_sample(time,interval_id,metric,value,complete,source_oldest_at,source_newest_at,
 source_id,origin_key,origin_time,origin_collected_at)
SELECT time,interval_id,metric,CASE WHEN metric=hosted_history_metric_code('hashrate') THEN value+1 ELSE value END,complete,
 source_oldest_at,source_newest_at,source_id,origin_key,origin_time,origin_collected_at FROM old`, times)
		elapsed := time.Since(start)
		require.Equal(t, len(times)*scaleBoards*5/2, replaced, "three metrics per board, temperature on odd intervals only")
		return markers() - before, elapsed
	}
	pollAt := func(hour, poll int) time.Time {
		return first.Add(time.Duration(hour)*time.Hour + time.Duration(poll)*45*time.Second)
	}
	revise := func() (int, time.Duration) {
		t.Helper()
		return scalar(`SELECT hosted_reporting_hour_revise($1)`, time.Now())
	}

	// One 288-board correction in the newest closed hour.
	singleMarkers, singleCorrection := correct([]time.Time{pollAt(scaleClosedHours-1, 40)})
	singleRevisions, singleRevise := revise()
	require.Equal(t, scaleBoards, singleRevisions)
	require.Zero(t, markers())

	// The same correction in each of the 24 older, mostly compressed, hours.
	// Markers are not deduplicated, but a run claims revision_batch
	// interval-hours with every marker they carry, so this backlog of six
	// markers per interval-hour drains in one run.
	backlogTimes := make([]time.Time, 0, scaleClosedHours-1)
	for hour := range scaleClosedHours - 1 {
		backlogTimes = append(backlogTimes, pollAt(hour, 20))
	}
	backlogMarkers, backlogCorrection := correct(backlogTimes)
	claims := explainMarkerClaims(t, r, first.Add(scaleClosedHours*time.Hour))
	var drainRevisions []int
	var drainTimes []time.Duration
	for markers() > 0 {
		require.Less(t, len(drainRevisions), 20, "the marker backlog drains")
		revisions, elapsed := revise()
		drainRevisions = append(drainRevisions, revisions)
		drainTimes = append(drainTimes, elapsed)
	}
	var drained int
	for _, revisions := range drainRevisions {
		drained += revisions
	}
	require.Equal(t, (scaleClosedHours-1)*scaleBoards, drained)
	require.Len(t, drainRevisions, 1)

	// Worst single revision run: one marker per interval-hour for every closed
	// hour, all claimed and recomputed by one default-sized batch.
	distinctTimes := make([]time.Time, 0, scaleClosedHours)
	for hour := range scaleClosedHours {
		distinctTimes = append(distinctTimes, pollAt(hour, 60))
	}
	start = time.Now()
	updated := execCompressed(`UPDATE hosted_account_history_sample SET value=value+1
 WHERE source_id=5 AND metric=hosted_history_metric_code('hashrate') AND time=ANY($1)`, distinctTimes)
	distinctCorrection := time.Since(start)
	require.Equal(t, scaleClosedHours*scaleBoards, updated)
	require.Equal(t, scaleClosedHours*scaleBoards, markers(), "an in-place update enqueues one marker")
	distinctRevisions, distinctRevise := revise()
	require.Equal(t, scaleClosedHours*scaleBoards, distinctRevisions)

	// Thirty days of earlier evidence, copied from each interval's newest row.
	// Odd intervals carry every metric; their rows size the hold.
	historyRows := func(parity int) (int, int64) {
		t.Helper()
		var before, after int64
		require.NoError(t, r.db.QueryRowContext(ctx, `SELECT pg_total_relation_size('hosted_account_reporting_hour')`).Scan(&before))
		inserted := exec(`INSERT INTO hosted_account_reporting_hour
SELECT l.interval_id,h,1,l.captured_at,l.capture_reason,l.no_source_data,
 CASE WHEN l.window_start IS NOT NULL THEN h END,CASE WHEN l.window_end IS NOT NULL THEN h+INTERVAL '1 hour' END,
 l.hashrate_report_count,l.hashrate_count,l.hashrate_positive_count,l.hashrate_sum,l.hashrate_complete,
 l.hashrate_source_oldest_at,l.hashrate_source_newest_at,l.power_report_count,l.power_count,l.power_sum,l.power_complete,
 l.power_source_oldest_at,l.power_source_newest_at,l.temperature_report_count,l.temperature_count,l.temperature_sum,
 l.temperature_latest_value,l.temperature_latest_at,l.temperature_source_oldest_at,l.temperature_source_newest_at,
 l.state_report_count,l.state_count,l.state_latest_value,l.state_latest_at,l.state_source_oldest_at,l.state_source_newest_at
FROM (SELECT DISTINCT ON (interval_id) * FROM hosted_account_reporting_hour WHERE interval_id%2=$3
 ORDER BY interval_id,bucket DESC,revision DESC) l
CROSS JOIN generate_series($1::timestamptz,$2::timestamptz-INTERVAL '1 hour',INTERVAL '1 hour') h`, history, first, parity)
		require.NoError(t, r.db.QueryRowContext(ctx, `SELECT pg_total_relation_size('hosted_account_reporting_hour')`).Scan(&after))
		return inserted, after - before
	}
	fullRows, fullBytes := historyRows(1)
	silentRows, silentBytes := historyRows(0)
	require.Equal(t, scaleHistoryDays*24*scaleBoards/2, fullRows)
	require.Equal(t, fullRows, silentRows)
	exec(`UPDATE hosted_account_reporting_hour_control SET hold_started_at=$1 WHERE singleton`, history)
	var totalRows int
	var totalBytes, snapshotBytes int64
	require.NoError(t, r.db.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM hosted_account_reporting_hour),
 pg_total_relation_size('hosted_account_reporting_hour'),pg_total_relation_size('hosted_account_reporting_allocation_snapshot')`).Scan(&totalRows, &totalBytes, &snapshotBytes))

	// Capture status with 30 days of rows and the 9-day missing-hour scan.
	var streams, observed, missing int
	start = time.Now()
	require.NoError(t, r.db.QueryRowContext(ctx, `SELECT count(*),count(*) FILTER(WHERE has_observation),sum(missing_hours)
 FROM hosted_account_reporting_capture_status`).Scan(&streams, &observed, &missing))
	statusTime := time.Since(start)
	require.Equal(t, scaleBoards*4, streams)
	require.Equal(t, scaleBoards*4-scaleBoards/2, observed, "even intervals never observe temperature")
	require.Zero(t, missing)
	var org int64
	require.NoError(t, r.db.QueryRowContext(ctx, `SELECT org_id FROM device WHERE id=$1`, r.databaseID).Scan(&org))
	listed, listTime := scalar(`SELECT count(*) FROM (SELECT interval_id,site_id,source_id,metric,has_observation,
 last_observation_captured_at,last_observation_source_newest_at FROM hosted_account_reporting_capture_status
 WHERE organization_id=$1 AND (client_id=$2 OR subclient_id=$2) AND allocation_ended_at IS NULL
 ORDER BY interval_id,source_id,metric) s`, org, client)
	require.Equal(t, streams, listed)
	_, unobservedTime := scalar(`SELECT count(*) FROM hosted_account_reporting_capture_status WHERE NOT has_observation`)

	// The real source path in an hour that is closing: correcting one poll of
	// hashboard metrics re-normalizes three samples per board.
	exec(`UPDATE hosted_account_reporting_hour_control SET closing_through=$1 WHERE singleton`, time.Now().UTC().Truncate(time.Hour).Add(time.Hour))
	var last time.Time
	require.NoError(t, r.db.QueryRowContext(ctx, `SELECT max(collected_at) FROM hashboard_metrics`).Scan(&last))
	before := markers()
	start = time.Now()
	exec(`UPDATE hashboard_metrics SET hashrate_hs=hashrate_hs+1 WHERE collected_at=$1`, last)
	sourceCorrection := time.Since(start)
	sourceMarkers := markers() - before
	require.Equal(t, scaleBoards*3*2, sourceMarkers, "each corrected sample is deleted and re-inserted")

	describe := func(values []time.Duration) string {
		var total time.Duration
		for _, value := range values {
			total += value
		}
		sorted := slices.Clone(values)
		slices.Sort(sorted)
		return fmt.Sprintf("mean=%s p95=%s", total/time.Duration(len(values)), sorted[(len(sorted)*95-1)/100])
	}
	perRow := func(bytes int64, rows int) float64 { return float64(bytes) / float64(rows) }
	var report strings.Builder
	fmt.Fprintf(&report, "one CT1: 96 modules, %d allocated hashboards, %d polls/hour (local Docker DB; WAL and backups excluded)\n", scaleBoards, scalePollsPerHour)
	fmt.Fprintf(&report, "ingestion, %d polls each: marker triggers dropped %s; installed %s; samples/poll=%d\n", polls, describe(without), describe(with), samples/scalePollsPerHour)
	fmt.Fprintf(&report, "replayed %d samples into %d closed hours in %s; compressed %d chunks in %s\n", loaded, scaleClosedHours, loadTime, compressed, compressTime)
	fmt.Fprintf(&report, "close 1 hour (uncompressed, %d samples): phase 1 %s, phase 2 %s, %d rows\n", samplesPerHour, steadyLock, steadyClose, scaleBoards)
	fmt.Fprintf(&report, "close 24 hours (catch-up, mostly compressed): phase 1 %s, phase 2 %s, %d rows\n", catchUpLock, catchUpClose, (scaleClosedHours-1)*scaleBoards)
	fmt.Fprintf(&report, "source correction of one poll in a closing hour: %d markers in %s\n", sourceMarkers, sourceCorrection)
	fmt.Fprintf(&report, "correction of one poll in 1 closed hour: %d markers in %s; revise %s, %d revisions\n", singleMarkers, singleCorrection, singleRevise, singleRevisions)
	fmt.Fprintf(&report, "correction of one poll in each of 24 closed hours: %d markers in %s; drained in %d runs %v revisions %v\n", backlogMarkers, backlogCorrection, len(drainRevisions), drainRevisions, drainTimes)
	fmt.Fprintf(&report, "marker claims with that backlog:\n%s", claims)
	fmt.Fprintf(&report, "in-place correction of one poll in each of %d closed hours: %d markers in %s; one revise %s, %d revisions\n", scaleClosedHours, scaleClosedHours*scaleBoards, distinctCorrection, distinctRevise, distinctRevisions)
	fmt.Fprintf(&report, "hour rows: full %.1f B/row, no-temperature %.1f B/row (%d rows each); table %d rows %d B (%.1f B/row); snapshots %d B for %d intervals\n",
		perRow(fullBytes, fullRows), perRow(silentBytes, silentRows), fullRows, totalRows, totalBytes, perRow(totalBytes, totalRows), snapshotBytes, scaleBoards)
	fmt.Fprintf(&report, "per CT1-day at %d rows: %.0f B (full rows)\n", scaleBoards*24, perRow(fullBytes, fullRows)*float64(scaleBoards*24))
	fmt.Fprintf(&report, "status with %d days of rows: all %d streams %s; ListReportingCaptureStreams shape %s; %d unobserved streams %s\n",
		scaleHistoryDays+1, streams, statusTime, listTime, streams-observed, unobservedTime)
	t.Log(report.String())
}

// explainMarkerClaims runs the closer's two marker claims, as written in
// hosted_reporting_hour_finish_close and hosted_reporting_hour_revise, under
// EXPLAIN ANALYZE and rolls them back. It returns their scan lines and times.
func explainMarkerClaims(t *testing.T, r *containerRollupRig, closedThrough time.Time) string {
	t.Helper()
	tx, err := r.db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	defer func() { require.NoError(t, tx.Rollback()) }()
	var report strings.Builder
	for _, claim := range []struct {
		name, query string
	}{
		{"closing-hour claim", `DELETE FROM hosted_account_reporting_hour_marker m WHERE m.bucket>=$1::timestamptz AND m.bucket<$1::timestamptz+INTERVAL '1 hour'`},
		{"revision claim", `WITH batch AS (
 SELECT q.interval_id,q.bucket FROM hosted_account_reporting_hour_marker q WHERE q.bucket<$1
 GROUP BY q.interval_id,q.bucket ORDER BY min(q.marker_id) LIMIT 10000)
DELETE FROM hosted_account_reporting_hour_marker m USING batch b
WHERE m.interval_id=b.interval_id AND m.bucket=b.bucket RETURNING m.interval_id,m.bucket`},
	} {
		fmt.Fprintf(&report, "  %s:", claim.name)
		func() {
			rows, err := tx.QueryContext(t.Context(), `EXPLAIN (ANALYZE,COSTS OFF) `+claim.query, closedThrough) //nolint:gosec // Queries are constants.
			require.NoError(t, err)
			defer rows.Close()
			for rows.Next() {
				var line string
				require.NoError(t, rows.Scan(&line))
				if strings.Contains(line, "Scan") || strings.Contains(line, "Execution Time") {
					fmt.Fprintf(&report, " [%s]", strings.TrimSpace(line))
				}
			}
			require.NoError(t, rows.Err())
		}()
		report.WriteString("\n")
	}
	return report.String()
}
