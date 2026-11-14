package hostedaccounts_test

import (
	"context"
	"strings"
	"testing"
	"time"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// Columns the hour summary shares with hosted_account_history_hourly.
var reportingHourSharedColumns = []string{
	"hashrate_sum", "hashrate_count", "hashrate_positive_count", "hashrate_complete",
	"hashrate_source_oldest_at", "hashrate_source_newest_at",
	"power_sum", "power_count", "power_complete", "power_source_oldest_at", "power_source_newest_at",
	"temperature_sum", "temperature_count", "temperature_latest_value", "temperature_latest_at",
	"temperature_source_oldest_at", "temperature_source_newest_at",
	"state_count", "state_latest_value", "state_latest_at", "state_source_oldest_at", "state_source_newest_at",
}

// Every column the hour summary returns.
var reportingHourSummaryColumns = append([]string{"interval_id", "bucket",
	"hashrate_report_count", "power_report_count", "temperature_report_count", "state_report_count"},
	reportingHourSharedColumns...)

// resetReportingHourControl moves the hold start and both closer watermarks to
// hour. Test templates are cached by migration fingerprint, so the migration's
// seeded hold start reflects template build time rather than the test's clock.
func resetReportingHourControl(t *testing.T, f fixture, hour time.Time) {
	t.Helper()
	_, err := f.db.ExecContext(t.Context(), `UPDATE hosted_account_reporting_hour_control
 SET hold_started_at=$1,closing_through=$1,closed_through=$1,last_success_at=NULL WHERE singleton`, hour)
	require.NoError(t, err)
}

// reportingHourInterval allocates one miner whose interval and history epochs
// start before hour, and returns the interval and miner ids.
func reportingHourInterval(t *testing.T, f fixture, hour time.Time) (int64, int64) {
	t.Helper()
	client := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	return reportingHourMinerInterval(t, f, client.AccountID, hour)
}

// reportingHourMinerInterval allocates one more miner to an existing client.
func reportingHourMinerInterval(t *testing.T, f fixture, account string, hour time.Time) (int64, int64) {
	t.Helper()
	miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	require.NoError(t, f.allocate(t, f.host, f.root, account, 1, selected(5, miner, 0)))
	historyEpoch(t, f, hour.Add(-time.Hour))
	var interval int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1
 WHERE kind=5 AND asset_id=$2 AND ended_at IS NULL RETURNING id`, hour.Add(-time.Hour), miner).Scan(&interval))
	return interval, miner
}

type reportingHourSample struct {
	at, oldest, newest, collected time.Time
	metric                        int
	value                         any
	complete                      any
	source                        int
	key                           string
}

func insertReportingHourSamples(t *testing.T, f fixture, interval int64, samples ...reportingHourSample) {
	t.Helper()
	for _, s := range samples {
		oldest, newest, collected, source, key := s.oldest, s.newest, s.collected, s.source, s.key
		if oldest.IsZero() {
			oldest = s.at
		}
		if newest.IsZero() {
			newest = s.at
		}
		if collected.IsZero() {
			collected = s.at
		}
		if source == 0 {
			source = 3
		}
		if key == "" {
			key = "fixture"
		}
		_, err := f.db.ExecContext(t.Context(), `INSERT INTO hosted_account_history_sample(time,interval_id,metric,value,complete,
 source_oldest_at,source_newest_at,source_id,origin_key,origin_time,origin_collected_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$1,$10)`,
			s.at, interval, s.metric, s.value, s.complete, oldest, newest, source, key, collected)
		require.NoError(t, err)
	}
}

func reportingHourSummary(t *testing.T, f fixture, from, to time.Time, intervals ...int64) []map[string]any {
	t.Helper()
	query := `SELECT ` + strings.Join(reportingHourSummaryColumns, ",") + ` FROM hosted_reporting_hour_summary($1,$2,$3::bigint[]) ORDER BY interval_id,bucket` //nolint:gosec // Column names are constants.
	var filter any
	if len(intervals) > 0 {
		filter = intervals
	}
	rows, err := f.db.QueryContext(t.Context(), query, from, to, filter)
	require.NoError(t, err)
	defer rows.Close()
	var result []map[string]any
	for rows.Next() {
		values := make([]any, len(reportingHourSummaryColumns))
		pointers := make([]any, len(values))
		for i := range values {
			pointers[i] = &values[i]
		}
		require.NoError(t, rows.Scan(pointers...))
		row := map[string]any{}
		for i, column := range reportingHourSummaryColumns {
			row[column] = values[i]
		}
		result = append(result, row)
	}
	require.NoError(t, rows.Err())
	return result
}

func reportingHourTime(t *testing.T, value any) time.Time {
	t.Helper()
	at, ok := value.(time.Time)
	require.True(t, ok, "want time.Time, got %T", value)
	return at
}

func reportingHourFloat(t *testing.T, value any) float64 {
	t.Helper()
	number, ok := value.(float64)
	require.True(t, ok, "want float64, got %T", value)
	return number
}

func TestReportingHourSummaryMatchesHourlyHistory(t *testing.T) {
	f := setup(t)
	hour := time.Now().UTC().Truncate(time.Hour).Add(-3 * time.Hour)
	interval, miner := reportingHourInterval(t, f, hour)
	// Normalized miner telemetry, including a zero hashrate and a missing temperature.
	for i, metrics := range []struct {
		hashrate, power float64
		temperature     any
	}{{100e12, 3000, 61.5}, {0, 2900, nil}, {120e12, 3100, 63.25}, {110.5e12, 3050.5, 62}} {
		_, err := f.db.ExecContext(t.Context(), `INSERT INTO device_metrics(time,device_identifier,hash_rate_hs,power_w,temp_c)
 SELECT $1,device_identifier,$2,$3,$4 FROM device WHERE id=$5`, hour.Add(time.Duration(5+10*i)*time.Minute), metrics.hashrate, metrics.power, metrics.temperature, miner)
		require.NoError(t, err)
	}
	for i, state := range []int{3, 2, 3} {
		_, err := f.db.ExecContext(t.Context(), `INSERT INTO miner_state_snapshots(time,org_id,site_id,device_identifier,state)
 SELECT $1,org_id,site_id,device_identifier,$2 FROM device WHERE id=$3`, hour.Add(time.Duration(7+15*i)*time.Minute), state, miner)
		require.NoError(t, err)
	}
	// Module-shaped samples with distinct source windows and completeness flags.
	insertReportingHourSamples(t, f, interval,
		reportingHourSample{at: hour.Add(50 * time.Minute), oldest: hour.Add(-2 * time.Minute), metric: 1, value: 7.25e12, complete: true},
		reportingHourSample{at: hour.Add(55 * time.Minute), oldest: hour.Add(54 * time.Minute), newest: hour.Add(59 * time.Minute), metric: 1, value: nil, complete: false},
		reportingHourSample{at: hour.Add(56 * time.Minute), metric: 2, value: 0.1, complete: true},
		reportingHourSample{at: hour.Add(57 * time.Minute), metric: 3, value: 0.2},
	)
	// A neighbouring hour must not leak into the summary.
	insertReportingHourSamples(t, f, interval, reportingHourSample{at: hour.Add(time.Hour), metric: 1, value: 1e15, complete: true})
	refreshHostedHistory(t, f, hour.Add(-24*time.Hour), hour.Add(24*time.Hour))

	summary := reportingHourSummary(t, f, hour, hour.Add(time.Hour), interval)
	require.Len(t, summary, 1)
	row := summary[0]
	require.Equal(t, interval, row["interval_id"])
	require.True(t, hour.Equal(reportingHourTime(t, row["bucket"])))

	values := make([]any, len(reportingHourSharedColumns))
	pointers := make([]any, len(values))
	for i := range values {
		pointers[i] = &values[i]
	}
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT `+strings.Join(reportingHourSharedColumns, ",")+`
 FROM hosted_account_history_hourly WHERE interval_id=$1 AND bucket=$2`, interval, hour).Scan(pointers...))
	for i, column := range reportingHourSharedColumns {
		expected, actual := values[i], row[column]
		switch want := expected.(type) {
		case float64:
			require.IsType(t, want, actual, column)
			require.InEpsilon(t, want, reportingHourFloat(t, actual), 1e-12, column)
		case time.Time:
			require.IsType(t, want, actual, column)
			require.True(t, want.Equal(reportingHourTime(t, actual)), "%s: want %s, got %s", column, want, actual)
		default:
			require.Equal(t, expected, actual, column)
		}
	}
	require.EqualValues(t, 6, row["hashrate_report_count"], "every hashrate report counts, including missing values")
	require.EqualValues(t, 5, row["hashrate_count"])
	require.EqualValues(t, 5, row["power_report_count"])
	require.EqualValues(t, 5, row["temperature_report_count"])
	require.EqualValues(t, 3, row["state_report_count"])
	require.Equal(t, summary, reportingHourSummary(t, f, hour, hour.Add(time.Hour), interval), "identical inputs give identical rows")
	all := reportingHourSummary(t, f, hour, hour.Add(2*time.Hour))
	require.Len(t, all, 2, "a NULL interval filter summarizes every interval-hour in range")
	require.Empty(t, reportingHourSummary(t, f, hour, hour.Add(time.Hour), interval+1_000_000))
}

func TestReportingHourSummaryKeepsMissingHashrateMissing(t *testing.T) {
	f := setup(t)
	hour := time.Now().UTC().Truncate(time.Hour).Add(-2 * time.Hour)
	interval, _ := reportingHourInterval(t, f, hour)
	// Module missing-source markers: reported, but without a hashrate value.
	insertReportingHourSamples(t, f, interval,
		reportingHourSample{at: hour.Add(10 * time.Minute), metric: 1, value: nil, complete: false},
		reportingHourSample{at: hour.Add(20 * time.Minute), metric: 1, value: nil, complete: false},
	)
	summary := reportingHourSummary(t, f, hour, hour.Add(time.Hour), interval)
	require.Len(t, summary, 1)
	row := summary[0]
	require.EqualValues(t, 2, row["hashrate_report_count"])
	require.EqualValues(t, 0, row["hashrate_count"])
	require.EqualValues(t, 0, row["hashrate_positive_count"])
	require.Nil(t, row["hashrate_sum"], "silence is never summed to zero")
	require.Equal(t, false, row["hashrate_complete"])
	for _, metric := range []string{"power", "temperature", "state"} {
		require.EqualValues(t, 0, row[metric+"_report_count"])
		require.EqualValues(t, 0, row[metric+"_count"])
		require.Nil(t, row[metric+"_source_oldest_at"])
	}
	require.Nil(t, row["power_sum"])
	require.Nil(t, row["power_complete"])
	require.Nil(t, row["temperature_latest_value"])
	require.Nil(t, row["state_latest_at"])
}

func TestReportingHourSummaryLatestStateTieBreakIsDeterministic(t *testing.T) {
	f := setup(t)
	hour := time.Now().UTC().Truncate(time.Hour).Add(-2 * time.Hour)
	interval, _ := reportingHourInterval(t, f, hour)
	at := hour.Add(30 * time.Minute)
	insertReportingHourSamples(t, f, interval,
		// Same observation time: the later collection wins, whatever the insert order.
		reportingHourSample{at: at, collected: at.Add(2 * time.Second), metric: 4, value: 1, source: 4, key: "b"},
		reportingHourSample{at: at, collected: at.Add(time.Second), metric: 4, value: 3, source: 4, key: "c"},
		// Same time and collection: the greater origin key wins.
		reportingHourSample{at: at, collected: at, metric: 3, value: 40, key: "a"},
		reportingHourSample{at: at, collected: at, metric: 3, value: 50, key: "z"},
		reportingHourSample{at: at, collected: at, metric: 3, value: 45, key: "m"},
		// A later missing value does not replace the latest known value.
		reportingHourSample{at: at.Add(time.Minute), metric: 4, value: nil, source: 4, key: "d"},
	)
	first := reportingHourSummary(t, f, hour, hour.Add(time.Hour), interval)
	require.Len(t, first, 1)
	require.Equal(t, 1.0, first[0]["state_latest_value"])
	require.True(t, at.Equal(reportingHourTime(t, first[0]["state_latest_at"])))
	require.Equal(t, 50.0, first[0]["temperature_latest_value"])
	require.EqualValues(t, 3, first[0]["state_report_count"])
	require.EqualValues(t, 2, first[0]["state_count"])
	for range 5 {
		require.Equal(t, first, reportingHourSummary(t, f, hour, hour.Add(time.Hour), interval))
	}
}

func reportingHourSnapshot(t *testing.T, f fixture, interval int64) {
	t.Helper()
	_, err := f.db.ExecContext(t.Context(), `INSERT INTO hosted_account_reporting_allocation_snapshot(interval_id,organization_id,client_id,
 subclient_id,site_id,kind,asset_id,device_id,device_identifier,started_at)
 SELECT id,organization_id,client_id,subclient_id,site_id,kind,asset_id,device_id,device_identifier,started_at
 FROM hosted_account_asset_interval WHERE id=$1 ON CONFLICT(interval_id) DO NOTHING`, interval)
	require.NoError(t, err)
}

const reportingHourInsert = `INSERT INTO hosted_account_reporting_hour(interval_id,bucket,revision,capture_reason,no_source_data,
 window_start,window_end,hashrate_report_count,hashrate_count,hashrate_positive_count,power_report_count,power_count,
 temperature_report_count,temperature_count,state_report_count,state_count)
 VALUES($1,$2,$3,'closed',false,$2,$2::timestamptz+interval '1 hour',0,0,0,0,0,0,0,0,0)`

func TestReportingHourEvidenceIsAppendOnly(t *testing.T) {
	f := setup(t)
	hour := time.Now().UTC().Truncate(time.Hour).Add(-2 * time.Hour)
	interval, _ := reportingHourInterval(t, f, hour)
	reportingHourSnapshot(t, f, interval)
	_, err := f.db.ExecContext(t.Context(), reportingHourInsert, interval, hour, 1)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `INSERT INTO hosted_account_reporting_hour_gap(from_bucket,to_bucket,reason) VALUES($1,$2,'test')`, hour.Add(-48*time.Hour), hour.Add(-24*time.Hour))
	require.NoError(t, err)
	for _, statement := range []string{
		"UPDATE hosted_account_reporting_hour SET no_source_data=true",
		"DELETE FROM hosted_account_reporting_hour",
		"TRUNCATE hosted_account_reporting_hour",
		"UPDATE hosted_account_reporting_hour_gap SET reason='changed'",
		"DELETE FROM hosted_account_reporting_hour_gap",
		"TRUNCATE hosted_account_reporting_hour_gap",
		"TRUNCATE hosted_account_reporting_allocation_snapshot CASCADE",
	} {
		_, err = f.db.ExecContext(t.Context(), statement)
		require.ErrorContains(t, err, "hosted reporting evidence is append-only", statement)
	}
	var hours, gaps int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT (SELECT count(*) FROM hosted_account_reporting_hour),(SELECT count(*) FROM hosted_account_reporting_hour_gap)`).Scan(&hours, &gaps))
	require.Equal(t, 1, hours)
	require.Equal(t, 1, gaps)
}

func TestReportingHourEvidenceRejectsDuplicateAndMalformedRows(t *testing.T) {
	f := setup(t)
	hour := time.Now().UTC().Truncate(time.Hour).Add(-2 * time.Hour)
	interval, _ := reportingHourInterval(t, f, hour)
	_, err := f.db.ExecContext(t.Context(), reportingHourInsert, interval, hour, 1)
	require.ErrorContains(t, err, "fk_hosted_reporting_hour_snapshot", "rows reference the immutable allocation snapshot")
	reportingHourSnapshot(t, f, interval)
	_, err = f.db.ExecContext(t.Context(), reportingHourInsert, interval, hour, 1)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), reportingHourInsert, interval, hour, 1)
	require.ErrorContains(t, err, "pk_hosted_reporting_hour")
	_, err = f.db.ExecContext(t.Context(), reportingHourInsert, interval, hour, 2)
	require.NoError(t, err, "a correction appends the next revision")
	for name, statement := range map[string]string{
		"ck_hosted_reporting_hour_bucket":   strings.Replace(reportingHourInsert, "VALUES($1,$2,$3", "VALUES($1,$2::timestamptz+interval '1 minute',$3", 1),
		"ck_hosted_reporting_hour_revision": strings.Replace(reportingHourInsert, "VALUES($1,$2,$3", "VALUES($1,$2,0", 1),
		"ck_hosted_reporting_hour_reason":   strings.Replace(reportingHourInsert, "'closed'", "'other'", 1),
		"ck_hosted_reporting_hour_window":   strings.Replace(reportingHourInsert, "interval '1 hour'", "interval '61 minutes'", 1),
	} {
		args := []any{interval, hour.Add(time.Hour), 1}
		if name == "ck_hosted_reporting_hour_revision" {
			args = args[:2]
		}
		_, err = f.db.ExecContext(t.Context(), statement, args...)
		require.ErrorContains(t, err, name)
	}
	_, err = f.db.ExecContext(t.Context(), `INSERT INTO hosted_account_reporting_hour_gap(from_bucket,to_bucket,reason) VALUES($1,$1,'test')`, hour)
	require.ErrorContains(t, err, "ck_hosted_reporting_hour_gap_bounds")
}

func TestReportingHourControlAndMarkers(t *testing.T) {
	f := setup(t)
	control := readReportingHourControl(t, f)
	hold := control.hold
	require.Equal(t, hold.Truncate(time.Hour), hold, "the hold starts on an hour boundary")
	require.Equal(t, hold, control.closing)
	require.Equal(t, hold, control.closed)
	// last_success_at is not checked here: the job can run once in the template
	// database before the test harness pauses background jobs.
	_, err := f.db.ExecContext(t.Context(), `INSERT INTO hosted_account_reporting_hour_control(singleton,hold_started_at,closing_through,closed_through) VALUES(true,$1,$1,$1)`, hold)
	require.ErrorContains(t, err, "pk_hosted_reporting_hour_control")

	hour := time.Now().UTC().Truncate(time.Hour).Add(-5 * time.Hour)
	resetReportingHourControl(t, f, hour)
	control = readReportingHourControl(t, f)
	require.True(t, hour.Equal(control.hold) && hour.Equal(control.closing) && hour.Equal(control.closed))
	_, err = f.db.ExecContext(t.Context(), `UPDATE hosted_account_reporting_hour_control SET closed_through=closing_through+interval '1 hour'`)
	require.ErrorContains(t, err, "ck_hosted_reporting_hour_control_order", "closed hours never pass closing hours")
	_, err = f.db.ExecContext(t.Context(), `UPDATE hosted_account_reporting_hour_control SET closing_through=closing_through+interval '30 minutes'`)
	require.ErrorContains(t, err, "ck_hosted_reporting_hour_control_aligned")

	interval, _ := reportingHourInterval(t, f, hour)
	for range 2 {
		_, err = f.db.ExecContext(t.Context(), `INSERT INTO hosted_account_reporting_hour_marker(interval_id,bucket) VALUES($1,$2)`, interval, hour)
		require.NoError(t, err, "markers are not deduplicated")
	}
	var markers, ids int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*),count(DISTINCT marker_id) FROM hosted_account_reporting_hour_marker WHERE interval_id=$1 AND bucket=$2`, interval, hour).Scan(&markers, &ids))
	require.Equal(t, 2, markers)
	require.Equal(t, 2, ids)
	_, err = f.db.ExecContext(t.Context(), `DELETE FROM hosted_account_reporting_hour_marker WHERE interval_id=$1`, interval)
	require.NoError(t, err, "claims delete markers")
}

// Execute the hourly evidence migration only against this test's disposable database.
func reportingHourMigration(t *testing.T, f fixture, direction string) error {
	t.Helper()
	return f.execMigration(t, "001077_internal_hourly_reporting_evidence."+direction+".sql")
}

func TestReportingHourMigrationRollbackRequiresEmptyHold(t *testing.T) {
	f := setup(t)
	require.NoError(t, reportingHourMigration(t, f, "down"), "an empty hold can roll back")
	var exists bool
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT to_regclass('hosted_account_reporting_hour') IS NOT NULL OR to_regprocedure('hosted_reporting_hour_summary(timestamptz,timestamptz,bigint[])') IS NOT NULL`).Scan(&exists))
	require.False(t, exists)
	before := time.Now()
	require.NoError(t, reportingHourMigration(t, f, "up"))
	var hold time.Time
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT hold_started_at FROM hosted_account_reporting_hour_control`).Scan(&hold))
	require.True(t, hold.After(before), "the hold starts at the first full hour after the migration")
	require.LessOrEqual(t, hold.Sub(before), time.Hour)

	hour := time.Now().UTC().Truncate(time.Hour).Add(-2 * time.Hour)
	interval, _ := reportingHourInterval(t, f, hour)
	reportingHourSnapshot(t, f, interval)
	_, err := f.db.ExecContext(t.Context(), reportingHourInsert, interval, hour, 1)
	require.NoError(t, err)
	require.ErrorContains(t, reportingHourMigration(t, f, "down"), "preserve hourly hosted reporting evidence")
	var hours int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_account_reporting_hour`).Scan(&hours))
	require.Equal(t, 1, hours)
}

// reportingHourGrace is the closer's default grace after an hour ends.
const reportingHourGrace = 30 * time.Minute

type reportingHourControl struct {
	hold, closing, closed time.Time
	lastSuccess           *time.Time
}

func readReportingHourControl(t *testing.T, f fixture) reportingHourControl {
	t.Helper()
	var control reportingHourControl
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT hold_started_at,closing_through,closed_through,last_success_at
 FROM hosted_account_reporting_hour_control WHERE singleton`).Scan(&control.hold, &control.closing, &control.closed, &control.lastSuccess))
	return control
}

// beginReportingHourClose runs phase 1 and returns the closing watermark.
func beginReportingHourClose(t *testing.T, f fixture, asOf time.Time) time.Time {
	t.Helper()
	var closing time.Time
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT hosted_reporting_hour_begin_close($1)`, asOf).Scan(&closing))
	return closing
}

// finishReportingHourClose runs phase 2 and returns the number of rows written.
func finishReportingHourClose(t *testing.T, f fixture, asOf time.Time) int {
	t.Helper()
	var written int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT hosted_reporting_hour_finish_close($1)`, asOf).Scan(&written))
	return written
}

func closeReportingHours(t *testing.T, f fixture, asOf time.Time) int {
	t.Helper()
	beginReportingHourClose(t, f, asOf)
	return finishReportingHourClose(t, f, asOf)
}

// reviseReportingHours runs the revision step and returns the number of revisions appended.
func reviseReportingHours(t *testing.T, f fixture, asOf time.Time) int {
	t.Helper()
	var written int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT hosted_reporting_hour_revise($1)`, asOf).Scan(&written))
	return written
}

type reportingHourRow struct {
	bucket                     time.Time
	revision                   int
	capturedAt                 time.Time
	reason                     string
	noSourceData               bool
	windowStart, windowEnd     *time.Time
	hashrateReports, hashrates int
	hashrateSum                *float64
	powerReports, stateReports int
	stateCount                 int
	stateLatest                *float64
	stateLatestAt              *time.Time
}

func reportingHourRows(t *testing.T, f fixture, interval int64) []reportingHourRow {
	t.Helper()
	rows, err := f.db.QueryContext(t.Context(), `SELECT bucket,revision,captured_at,capture_reason,no_source_data,window_start,window_end,
 hashrate_report_count,hashrate_count,hashrate_sum,power_report_count,state_report_count,state_count,state_latest_value,state_latest_at
 FROM hosted_account_reporting_hour WHERE interval_id=$1 ORDER BY bucket,revision`, interval)
	require.NoError(t, err)
	defer rows.Close()
	var result []reportingHourRow
	for rows.Next() {
		var row reportingHourRow
		require.NoError(t, rows.Scan(&row.bucket, &row.revision, &row.capturedAt, &row.reason, &row.noSourceData, &row.windowStart, &row.windowEnd,
			&row.hashrateReports, &row.hashrates, &row.hashrateSum, &row.powerReports, &row.stateReports, &row.stateCount, &row.stateLatest, &row.stateLatestAt))
		result = append(result, row)
	}
	require.NoError(t, rows.Err())
	return result
}

func reportingHourMarkerCount(t *testing.T, f fixture, interval int64) int {
	t.Helper()
	var markers int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_account_reporting_hour_marker WHERE interval_id=$1`, interval).Scan(&markers))
	return markers
}

func requireReportingHourWindow(t *testing.T, row reportingHourRow, start, end time.Time) {
	t.Helper()
	require.NotNil(t, row.windowStart)
	require.NotNil(t, row.windowEnd)
	require.True(t, start.Equal(*row.windowStart), "window start: want %s, got %s", start, *row.windowStart)
	require.True(t, end.Equal(*row.windowEnd), "window end: want %s, got %s", end, *row.windowEnd)
}

func TestReportingHourCloserWritesSilentHour(t *testing.T) {
	f := setup(t)
	hour := time.Now().UTC().Truncate(time.Hour).Add(-2 * time.Hour)
	resetReportingHourControl(t, f, hour)
	interval, _ := reportingHourInterval(t, f, hour)

	require.Equal(t, 1, closeReportingHours(t, f, hour.Add(time.Hour+reportingHourGrace)))
	rows := reportingHourRows(t, f, interval)
	require.Len(t, rows, 1, "an allocated miner without samples still gets an evidence row")
	row := rows[0]
	require.True(t, hour.Equal(row.bucket))
	require.Equal(t, 1, row.revision)
	require.Equal(t, "closed", row.reason)
	requireReportingHourWindow(t, row, hour, hour.Add(time.Hour))
	require.Zero(t, row.hashrateReports, "silence has zero report counts")
	require.Zero(t, row.hashrates)
	require.Zero(t, row.powerReports)
	require.Zero(t, row.stateReports)
	require.Nil(t, row.hashrateSum, "silence is never summed to zero")
	require.Nil(t, row.stateLatest)
	require.True(t, row.noSourceData, "no miner source delivered anything at the site")
	control := readReportingHourControl(t, f)
	require.True(t, hour.Add(time.Hour).Equal(control.closed))
	require.True(t, hour.Add(time.Hour).Equal(control.closing))
	require.NotNil(t, control.lastSuccess)
}

// reportingHourBoards allocates count boards of one module and starts their
// intervals an hour before hour. It returns the interval ids.
func reportingHourBoards(t *testing.T, f fixture, hour time.Time, count int) []int64 {
	t.Helper()
	client := f.client(t, "Board customer", f.user, hostedaccounts.AllCapabilities)
	module, _ := f.miningModule(t)
	boards := f.moduleSelection(t, module).Boards
	require.GreaterOrEqual(t, len(boards), count)
	selections := make([]*pb.MiningAssetSelection, 0, count)
	for _, board := range boards[:count] {
		selections = append(selections, selected(7, board.Id, 0))
	}
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selections...))
	intervals := make([]int64, count)
	for i, board := range boards[:count] {
		require.NoError(t, f.db.QueryRowContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1
 WHERE kind=7 AND asset_id=$2 AND ended_at IS NULL RETURNING id`, hour.Add(-time.Hour), board.Id).Scan(&intervals[i]))
	}
	return intervals
}

func TestReportingHourCloserWritesClosedHashboardHour(t *testing.T) {
	f := setup(t)
	hour := time.Now().UTC().Truncate(time.Hour).Add(-3 * time.Hour)
	resetReportingHourControl(t, f, hour)
	board := reportingHourBoards(t, f, hour, 1)[0]
	insertReportingHourSamples(t, f, board,
		reportingHourSample{at: hour.Add(10 * time.Minute), metric: 1, value: 100e12, complete: true, source: 5, key: "board"},
		reportingHourSample{at: hour.Add(40 * time.Minute), metric: 1, value: 120e12, complete: true, source: 5, key: "board"},
		reportingHourSample{at: hour.Add(10 * time.Minute), metric: 2, value: 3000.0, complete: true, source: 5, key: "board"},
		reportingHourSample{at: hour.Add(15 * time.Minute), metric: 4, value: 1.0, source: 6, key: "board"},
		reportingHourSample{at: hour.Add(45 * time.Minute), metric: 4, value: 3.0, source: 6, key: "board"},
		// The next hour is still open and stays out of this hour's row.
		reportingHourSample{at: hour.Add(70 * time.Minute), metric: 1, value: 1e15, complete: true, source: 5, key: "board"},
	)

	require.Equal(t, 1, closeReportingHours(t, f, hour.Add(time.Hour+reportingHourGrace)))
	rows := reportingHourRows(t, f, board)
	require.Len(t, rows, 1)
	row := rows[0]
	require.True(t, hour.Equal(row.bucket))
	require.Equal(t, 1, row.revision)
	require.Equal(t, "closed", row.reason)
	require.False(t, row.noSourceData)
	requireReportingHourWindow(t, row, hour, hour.Add(time.Hour))
	require.Equal(t, 2, row.hashrates)
	require.InEpsilon(t, 220e12, *row.hashrateSum, 1e-12)
	require.Equal(t, 1, row.powerReports)
	require.Equal(t, 2, row.stateReports)
	require.Equal(t, 3.0, *row.stateLatest)
	require.True(t, hour.Add(45*time.Minute).Equal(*row.stateLatestAt))

	// Every metric column is the shared summary's value.
	summary := reportingHourSummary(t, f, hour, hour.Add(time.Hour), board)
	require.Len(t, summary, 1)
	values := make([]any, len(reportingHourSummaryColumns))
	pointers := make([]any, len(values))
	for i := range values {
		pointers[i] = &values[i]
	}
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT `+strings.Join(reportingHourSummaryColumns, ",")+`
 FROM hosted_account_reporting_hour WHERE interval_id=$1 AND bucket=$2 AND revision=1`, board, hour).Scan(pointers...))
	for i, column := range reportingHourSummaryColumns {
		if want, ok := summary[0][column].(time.Time); ok {
			require.True(t, want.Equal(reportingHourTime(t, values[i])), column)
			continue
		}
		require.Equal(t, summary[0][column], values[i], column)
	}

	var kind, snapshots int
	var asset int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT kind,asset_id,count(*) OVER () FROM hosted_account_reporting_allocation_snapshot WHERE interval_id=$1`, board).Scan(&kind, &asset, &snapshots))
	require.Equal(t, 7, kind)
	require.Equal(t, 1, snapshots, "the closer stores the allocation snapshot once")
	require.Zero(t, reportingHourMarkerCount(t, f, board))
}

func TestReportingHourCloserMarksMissingSourceData(t *testing.T) {
	f := setup(t)
	hour := time.Now().UTC().Truncate(time.Hour).Add(-4 * time.Hour)
	resetReportingHourControl(t, f, hour)
	boards := reportingHourBoards(t, f, hour, 3)
	// Hour H: no board source delivered anything at the site. Hour H+1: board 0
	// reported, so the other boards were silent devices.
	insertReportingHourSamples(t, f, boards[0],
		reportingHourSample{at: hour.Add(70 * time.Minute), metric: 1, value: 100e12, complete: true, source: 5, key: "board-0"})
	asOf := hour.Add(2*time.Hour + reportingHourGrace)
	require.Equal(t, 6, closeReportingHours(t, f, asOf))
	for i, board := range boards {
		rows := reportingHourRows(t, f, board)
		require.Len(t, rows, 2)
		require.True(t, rows[0].noSourceData, "board %d: an ingest outage is not device silence", i)
		require.False(t, rows[1].noSourceData, "board %d: another board at the site reported", i)
	}

	// Backfilling one board's state for H clears the flag on every board at the site.
	insertReportingHourSamples(t, f, boards[0],
		reportingHourSample{at: hour.Add(30 * time.Minute), metric: 4, value: 1.0, source: 6, key: "board-0"})
	require.Equal(t, 1, reportingHourMarkerCount(t, f, boards[0]))
	require.Equal(t, 3, reviseReportingHours(t, f, asOf))
	for i, board := range boards {
		rows := reportingHourRows(t, f, board)
		require.Len(t, rows, 3)
		require.True(t, rows[0].noSourceData, "board %d: revision 1 is unchanged", i)
		require.True(t, hour.Equal(rows[1].bucket))
		require.Equal(t, 2, rows[1].revision)
		require.Equal(t, "closed", rows[1].reason, "a revision of a closed hour stays closed")
		require.False(t, rows[1].noSourceData, "board %d: the backfill clears the flag", i)
		require.Equal(t, 1, rows[2].revision, "board %d: the next hour is unchanged", i)
	}
	require.Equal(t, 1, reportingHourRows(t, f, boards[0])[1].stateReports)
	require.Zero(t, reportingHourRows(t, f, boards[1])[1].stateReports)
	require.Zero(t, reviseReportingHours(t, f, asOf))
}

func TestReportingHourSourceCoverageStaysInsideOrganization(t *testing.T) {
	f := setup(t)
	hour := time.Now().UTC().Truncate(time.Hour).Add(-4 * time.Hour)
	resetReportingHourControl(t, f, hour)
	local, _ := reportingHourInterval(t, f, hour)
	// A second organization with one miner. Neither miner has a site.
	other := fixture{db: f.db}
	require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO organization(org_id,name) VALUES($1,'Other') RETURNING id`, uuid.NewString()).Scan(&other.org))
	miner := other.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	var foreign int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO hosted_account_asset_interval(organization_id,kind,asset_id,device_id,
 device_identifier,client_id,started_at)
 SELECT d.org_id,5,d.id,d.id,d.device_identifier,a.id,$2 FROM device d
 JOIN hosted_account a ON a.organization_id=d.org_id AND a.depth=0 WHERE d.id=$1 RETURNING id`,
		miner, hour.Add(-time.Hour)).Scan(&foreign))
	var unsited int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_account_asset_interval WHERE id=ANY($1) AND site_id IS NULL`,
		[]int64{local, foreign}).Scan(&unsited))
	require.Equal(t, 2, unsited)

	// Hour H: only the first organization's miner reports. Hour H+1: nothing does.
	insertReportingHourSamples(t, f, local, reportingHourSample{at: hour.Add(10 * time.Minute), metric: 1, value: 100e12, complete: true, source: 1})
	asOf := hour.Add(2*time.Hour + reportingHourGrace)
	require.Equal(t, 4, closeReportingHours(t, f, asOf))
	rows := reportingHourRows(t, f, local)
	require.Len(t, rows, 2)
	require.False(t, rows[0].noSourceData)
	require.True(t, rows[1].noSourceData)
	rows = reportingHourRows(t, f, foreign)
	require.Len(t, rows, 2)
	require.True(t, rows[0].noSourceData, "another organization's samples are not this organization's source data")
	require.True(t, rows[1].noSourceData)

	// A backfill in the first organization revises only that organization's rows.
	insertReportingHourSamples(t, f, local, reportingHourSample{at: hour.Add(70 * time.Minute), metric: 1, value: 100e12, complete: true, source: 1})
	require.Equal(t, 1, reviseReportingHours(t, f, asOf))
	rows = reportingHourRows(t, f, local)
	require.Len(t, rows, 3)
	require.False(t, rows[2].noSourceData)
	rows = reportingHourRows(t, f, foreign)
	require.Len(t, rows, 2)
	require.True(t, rows[1].noSourceData)
}

func TestReportingHourCloserRecordsCaptureWindow(t *testing.T) {
	f := setup(t)
	hour := time.Now().UTC().Truncate(time.Hour).Add(-3 * time.Hour)
	resetReportingHourControl(t, f, hour)
	client := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	starting, _ := reportingHourMinerInterval(t, f, client.AccountID, hour)
	ending, _ := reportingHourMinerInterval(t, f, client.AccountID, hour)
	_, err := f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$2 WHERE id=$1`, starting, hour.Add(20*time.Minute))
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET ended_at=$2 WHERE id=$1`, ending, hour.Add(40*time.Minute))
	require.NoError(t, err)

	require.Equal(t, 2, closeReportingHours(t, f, hour.Add(time.Hour+reportingHourGrace)))
	rows := reportingHourRows(t, f, starting)
	require.Len(t, rows, 1)
	requireReportingHourWindow(t, rows[0], hour.Add(20*time.Minute), hour.Add(time.Hour))
	rows = reportingHourRows(t, f, ending)
	require.Len(t, rows, 1)
	requireReportingHourWindow(t, rows[0], hour, hour.Add(40*time.Minute))
}

func TestReportingHourCloserWaitsForGrace(t *testing.T) {
	f := setup(t)
	hour := time.Now().UTC().Truncate(time.Hour).Add(-3 * time.Hour)
	resetReportingHourControl(t, f, hour)
	interval, _ := reportingHourInterval(t, f, hour)
	early := hour.Add(time.Hour + reportingHourGrace - time.Second)
	require.True(t, hour.Equal(beginReportingHourClose(t, f, early)), "the hour is not closing before its grace ends")
	require.Zero(t, finishReportingHourClose(t, f, early))
	require.Empty(t, reportingHourRows(t, f, interval))
	control := readReportingHourControl(t, f)
	require.True(t, hour.Equal(control.closed))
	require.True(t, hour.Equal(control.closing))

	require.Equal(t, 1, closeReportingHours(t, f, hour.Add(time.Hour+reportingHourGrace)))
	require.Len(t, reportingHourRows(t, f, interval), 1)
	// A future as_of is clamped to the database clock less the grace.
	closing := beginReportingHourClose(t, f, time.Now().Add(48*time.Hour))
	require.False(t, closing.After(time.Now().Add(-reportingHourGrace)))
}

func TestReportingHourLateSampleAppendsRevision(t *testing.T) {
	f := setup(t)
	hour := time.Now().UTC().Truncate(time.Hour).Add(-3 * time.Hour)
	resetReportingHourControl(t, f, hour)
	interval, _ := reportingHourInterval(t, f, hour)
	insertReportingHourSamples(t, f, interval, reportingHourSample{at: hour.Add(10 * time.Minute), metric: 1, value: 100e12, complete: true, source: 1})
	asOf := hour.Add(time.Hour + reportingHourGrace)
	require.Equal(t, 1, closeReportingHours(t, f, asOf))
	first := reportingHourRows(t, f, interval)[0]

	insertReportingHourSamples(t, f, interval, reportingHourSample{at: hour.Add(30 * time.Minute), metric: 1, value: 200e12, complete: true, source: 1})
	require.Equal(t, 1, reportingHourMarkerCount(t, f, interval), "a sample for a closed hour enqueues a marker")
	require.Equal(t, 1, reviseReportingHours(t, f, asOf))
	rows := reportingHourRows(t, f, interval)
	require.Len(t, rows, 2)
	require.Equal(t, first, rows[0], "revision 1 is unchanged")
	require.Equal(t, 2, rows[1].revision)
	require.Equal(t, "closed", rows[1].reason)
	require.Equal(t, 2, rows[1].hashrates)
	require.InEpsilon(t, 300e12, *rows[1].hashrateSum, 1e-12)
	require.True(t, rows[1].capturedAt.After(first.capturedAt))
	require.Zero(t, reportingHourMarkerCount(t, f, interval))
	require.Zero(t, reviseReportingHours(t, f, asOf))
}

func TestReportingHourRevisionBatchCountsIntervalHours(t *testing.T) {
	f := setup(t)
	hour := time.Now().UTC().Truncate(time.Hour).Add(-3 * time.Hour)
	resetReportingHourControl(t, f, hour)
	client := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	older, _ := reportingHourMinerInterval(t, f, client.AccountID, hour)
	newer, _ := reportingHourMinerInterval(t, f, client.AccountID, hour)
	// Site coverage exists at close, so later samples cannot flip no_source_data.
	insertReportingHourSamples(t, f, newer, reportingHourSample{at: hour.Add(5 * time.Minute), metric: 1, value: 100e12, complete: true, source: 1})
	asOf := hour.Add(time.Hour + reportingHourGrace)
	require.Equal(t, 2, closeReportingHours(t, f, asOf))

	for minute := 10; minute <= 30; minute += 10 {
		insertReportingHourSamples(t, f, older, reportingHourSample{at: hour.Add(time.Duration(minute) * time.Minute), metric: 1, value: 100e12, complete: true, source: 1})
	}
	insertReportingHourSamples(t, f, newer, reportingHourSample{at: hour.Add(40 * time.Minute), metric: 1, value: 100e12, complete: true, source: 1})
	require.Equal(t, 3, reportingHourMarkerCount(t, f, older))

	var written int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT hosted_reporting_hour_revise($1,batch_size=>1)`, asOf).Scan(&written))
	require.Equal(t, 1, written, "a batch of one takes the oldest marked interval-hour")
	require.Zero(t, reportingHourMarkerCount(t, f, older), "and every marker it carries")
	require.Equal(t, 1, reportingHourMarkerCount(t, f, newer))
	rows := reportingHourRows(t, f, older)
	require.Len(t, rows, 2)
	require.Equal(t, 3, rows[1].hashrates)
	require.Len(t, reportingHourRows(t, f, newer), 1)

	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT hosted_reporting_hour_revise($1,batch_size=>1)`, asOf).Scan(&written))
	require.Equal(t, 1, written)
	require.Zero(t, reportingHourMarkerCount(t, f, newer))
	require.Len(t, reportingHourRows(t, f, newer), 2)
}

func TestReportingHourIdenticalCorrectionsAddNoRevision(t *testing.T) {
	f := setup(t)
	hour := time.Now().UTC().Truncate(time.Hour).Add(-3 * time.Hour)
	resetReportingHourControl(t, f, hour)
	interval, _ := reportingHourInterval(t, f, hour)
	at := hour.Add(10 * time.Minute)
	insertReportingHourSamples(t, f, interval, reportingHourSample{at: at, metric: 1, value: 100e12, complete: true, source: 1})
	asOf := hour.Add(time.Hour + reportingHourGrace)
	require.Equal(t, 1, closeReportingHours(t, f, asOf))

	// Re-upserting identical values (the 1044 conflict path) enqueues nothing.
	_, err := f.db.ExecContext(t.Context(), `UPDATE hosted_account_history_sample SET value=value,complete=complete WHERE interval_id=$1`, interval)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `INSERT INTO hosted_account_history_sample(time,interval_id,metric,value,complete,
 source_oldest_at,source_newest_at,source_id,origin_key,origin_time,origin_collected_at)
 SELECT time,interval_id,metric,value,complete,source_oldest_at,source_newest_at,source_id,origin_key,origin_time,origin_collected_at
 FROM hosted_account_history_sample WHERE interval_id=$1
 ON CONFLICT(time,source_id,origin_key,origin_time,origin_collected_at,metric,interval_id) DO UPDATE
 SET value=EXCLUDED.value,complete=EXCLUDED.complete,source_oldest_at=EXCLUDED.source_oldest_at,source_newest_at=EXCLUDED.source_newest_at`, interval)
	require.NoError(t, err)
	require.Zero(t, reportingHourMarkerCount(t, f, interval))

	// A delete-and-reinsert correction with an identical result adds no revision.
	tx, err := f.db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	defer tx.Rollback()
	_, err = tx.ExecContext(t.Context(), `DELETE FROM hosted_account_history_sample WHERE interval_id=$1`, interval)
	require.NoError(t, err)
	_, err = tx.ExecContext(t.Context(), `INSERT INTO hosted_account_history_sample(time,interval_id,metric,value,complete,
 source_oldest_at,source_newest_at,source_id,origin_key,origin_time,origin_collected_at) VALUES($1,$2,1,100e12,true,$1,$1,1,'fixture',$1,$1)`, at, interval)
	require.NoError(t, err)
	require.NoError(t, tx.Commit())
	require.Equal(t, 2, reportingHourMarkerCount(t, f, interval), "the deletion and the insert each enqueue a marker")
	require.Zero(t, reviseReportingHours(t, f, asOf))
	require.Len(t, reportingHourRows(t, f, interval), 1)
	require.Zero(t, reportingHourMarkerCount(t, f, interval))
}

func reportingHourChunkCompressed(t *testing.T, f fixture, at time.Time) bool {
	t.Helper()
	var compressed bool
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT bool_and(is_compressed) FROM timescaledb_information.chunks
 WHERE hypertable_name='hosted_account_history_sample' AND range_start<=$1 AND range_end>$1`, at).Scan(&compressed))
	return compressed
}

func TestReportingHourCompressedCorrectionAppendsRevision(t *testing.T) {
	f := setup(t)
	hour := time.Now().UTC().Truncate(time.Hour).Add(-8 * time.Hour)
	resetReportingHourControl(t, f, hour)
	interval, miner := reportingHourInterval(t, f, hour)
	at := hour.Add(10 * time.Minute)
	_, err := f.db.ExecContext(t.Context(), `INSERT INTO device_metrics(time,device_identifier,hash_rate_hs,power_w) SELECT $1,device_identifier,100e12,3000 FROM device WHERE id=$2`, at, miner)
	require.NoError(t, err)
	asOf := hour.Add(time.Hour + reportingHourGrace)
	require.Equal(t, 1, closeReportingHours(t, f, asOf))
	_, err = f.db.ExecContext(t.Context(), `SELECT compress_chunk(c) FROM show_chunks('hosted_account_history_sample') c`)
	require.NoError(t, err)
	require.True(t, reportingHourChunkCompressed(t, f, at))

	// A raw correction deletes and reinserts the compressed normalized samples.
	_, err = f.db.ExecContext(t.Context(), `UPDATE device_metrics SET hash_rate_hs=200e12 WHERE time=$1 AND device_identifier=(SELECT device_identifier FROM device WHERE id=$2)`, at, miner)
	require.NoError(t, err)
	require.Positive(t, reportingHourMarkerCount(t, f, interval), "row triggers fire for DML on compressed chunks")
	require.Equal(t, 1, reviseReportingHours(t, f, asOf))
	rows := reportingHourRows(t, f, interval)
	require.Len(t, rows, 2)
	require.InEpsilon(t, 200e12, *rows[1].hashrateSum, 1e-12)

	// A direct update of a compressed normalized sample fires the update trigger.
	_, err = f.db.ExecContext(t.Context(), `SELECT compress_chunk(c,if_not_compressed=>true,recompress=>true) FROM show_chunks('hosted_account_history_sample') c`)
	require.NoError(t, err)
	require.True(t, reportingHourChunkCompressed(t, f, at))
	_, err = f.db.ExecContext(t.Context(), `UPDATE hosted_account_history_sample SET value=300e12 WHERE interval_id=$1 AND metric=1`, interval)
	require.NoError(t, err)
	require.Equal(t, 1, reportingHourMarkerCount(t, f, interval))
	require.Equal(t, 1, reviseReportingHours(t, f, asOf))
	rows = reportingHourRows(t, f, interval)
	require.Len(t, rows, 3)
	require.Equal(t, 3, rows[2].revision)
	require.InEpsilon(t, 300e12, *rows[2].hashrateSum, 1e-12)
}

func TestReportingHourRetroactiveEndAppendsWindowRevision(t *testing.T) {
	f := setup(t)
	hour := time.Now().UTC().Truncate(time.Hour).Add(-4 * time.Hour)
	resetReportingHourControl(t, f, hour)
	interval, _ := reportingHourInterval(t, f, hour)
	insertReportingHourSamples(t, f, interval,
		reportingHourSample{at: hour.Add(10 * time.Minute), metric: 1, value: 100e12, complete: true, source: 1},
		reportingHourSample{at: hour.Add(50 * time.Minute), metric: 1, value: 100e12, complete: true, source: 1})
	asOf := hour.Add(2*time.Hour + reportingHourGrace)
	require.Equal(t, 2, closeReportingHours(t, f, asOf))

	_, err := f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET ended_at=$2 WHERE id=$1`, interval, hour.Add(30*time.Minute))
	require.NoError(t, err)
	require.Equal(t, 2, reportingHourMarkerCount(t, f, interval), "both closed hours after the new end are marked")
	require.Equal(t, 2, reviseReportingHours(t, f, asOf))
	rows := reportingHourRows(t, f, interval)
	require.Len(t, rows, 4)
	requireReportingHourWindow(t, rows[0], hour, hour.Add(time.Hour))
	require.Equal(t, 2, rows[1].revision)
	requireReportingHourWindow(t, rows[1], hour, hour.Add(30*time.Minute))
	require.Equal(t, 2, rows[1].hashrates, "samples attributed past the new end stay counted on the interval")
	require.True(t, hour.Add(time.Hour).Equal(rows[3].bucket))
	require.Equal(t, 2, rows[3].revision)
	require.Nil(t, rows[3].windowStart, "the interval no longer overlaps the hour")
	require.Nil(t, rows[3].windowEnd)
	var snapshots int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_account_reporting_allocation_snapshot WHERE interval_id=$1`, interval).Scan(&snapshots))
	require.Equal(t, 1, snapshots, "past rows keep their interval and snapshot")
}

func TestReportingHourReallocationKeepsPastHours(t *testing.T) {
	f := setup(t)
	hour := time.Now().UTC().Truncate(time.Hour).Add(-3 * time.Hour)
	resetReportingHourControl(t, f, hour)
	client := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	module, _ := f.miningModule(t)
	board := f.moduleSelection(t, module).Boards[0].Id
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(7, board, 0)))
	var previous int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1
 WHERE kind=7 AND asset_id=$2 AND ended_at IS NULL RETURNING id`, hour.Add(-time.Hour), board).Scan(&previous))
	insertReportingHourSamples(t, f, previous, reportingHourSample{at: hour.Add(10 * time.Minute), metric: 1, value: 100e12, complete: true, source: 5})
	require.Equal(t, 1, closeReportingHours(t, f, hour.Add(time.Hour+reportingHourGrace)))

	child, err := f.svc.CreateChild(t.Context(), f.user, f.org, client.AccountID, "Subclient", f.host, hostedaccounts.DefaultCapabilities)
	require.NoError(t, err)
	require.NoError(t, f.allocate(t, f.user, client.AccountID, child.AccountID, 1, selected(7, board, 1)))
	var current int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT id FROM hosted_account_asset_interval WHERE kind=7 AND asset_id=$1 AND ended_at IS NULL`, board).Scan(&current))
	require.NotEqual(t, previous, current, "reallocation opens a new interval")
	require.Zero(t, reportingHourMarkerCount(t, f, previous), "ending at the reallocation leaves closed hours alone")

	closeReportingHours(t, f, time.Now())
	require.Zero(t, reviseReportingHours(t, f, time.Now()))
	rows := reportingHourRows(t, f, previous)
	require.NotEmpty(t, rows)
	require.True(t, hour.Equal(rows[0].bucket))
	require.Equal(t, 1, rows[0].hashrates)
	for _, row := range rows {
		require.Equal(t, 1, row.revision)
	}
	require.Empty(t, reportingHourRows(t, f, current), "the new interval starts after every closed hour")
	var subclient *int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT subclient_id FROM hosted_account_reporting_allocation_snapshot WHERE interval_id=$1`, previous).Scan(&subclient))
	require.Nil(t, subclient, "the past hour keeps its original allocation snapshot")
}

func TestReportingHourBackdatedIntervalIsUnattributable(t *testing.T) {
	f := setup(t)
	hour := time.Now().UTC().Truncate(time.Hour).Add(-4 * time.Hour)
	resetReportingHourControl(t, f, hour)
	client := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	reporting, _ := reportingHourMinerInterval(t, f, client.AccountID, hour)
	insertReportingHourSamples(t, f, reporting, reportingHourSample{at: hour.Add(10 * time.Minute), metric: 1, value: 100e12, complete: true, source: 1})
	asOf := hour.Add(2*time.Hour + reportingHourGrace)
	require.Equal(t, 2, closeReportingHours(t, f, asOf))

	// A later-created interval whose start moves behind closed_through.
	moved, _ := reportingHourMinerInterval(t, f, client.AccountID, hour)
	require.Equal(t, 2, reportingHourMarkerCount(t, f, moved))
	// A backdated interval inserted inside a closed hour.
	late := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(5, late, 0)))
	var inserted int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO hosted_account_asset_interval(organization_id,kind,asset_id,device_id,
 site_id,device_identifier,client_id,subclient_id,started_at,ended_at)
 SELECT organization_id,kind,asset_id,device_id,site_id,device_identifier,client_id,subclient_id,$2,$3
 FROM hosted_account_asset_interval WHERE kind=5 AND asset_id=$1 AND ended_at IS NULL RETURNING id`,
		late, hour.Add(20*time.Minute), hour.Add(40*time.Minute)).Scan(&inserted))
	require.Equal(t, 1, reportingHourMarkerCount(t, f, inserted))

	require.Equal(t, 3, reviseReportingHours(t, f, asOf))
	rows := reportingHourRows(t, f, moved)
	require.Len(t, rows, 2)
	for _, row := range rows {
		require.Equal(t, 1, row.revision)
		require.Equal(t, "unattributable", row.reason, "samples before the move were never attributed to this interval")
		require.Zero(t, row.hashrateReports)
	}
	requireReportingHourWindow(t, rows[0], hour, hour.Add(time.Hour))
	require.False(t, rows[0].noSourceData)
	require.True(t, rows[1].noSourceData)
	rows = reportingHourRows(t, f, inserted)
	require.Len(t, rows, 1)
	require.Equal(t, "unattributable", rows[0].reason)
	requireReportingHourWindow(t, rows[0], hour.Add(20*time.Minute), hour.Add(40*time.Minute))
	for _, row := range reportingHourRows(t, f, reporting) {
		require.Equal(t, 1, row.revision, "intervals that were closed normally are untouched")
	}
	var snapshots int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_account_reporting_allocation_snapshot WHERE interval_id=ANY($1)`, []int64{moved, inserted}).Scan(&snapshots))
	require.Equal(t, 2, snapshots)

	// A later revision of an unattributable hour stays unattributable. The
	// sample is the site's first source data in H+1, so the other miner's
	// H+1 row is revised too and its no_source_data flag clears.
	insertReportingHourSamples(t, f, moved, reportingHourSample{at: hour.Add(80 * time.Minute), metric: 1, value: 50e12, complete: true, source: 1})
	require.Equal(t, 2, reviseReportingHours(t, f, asOf))
	rows = reportingHourRows(t, f, moved)
	require.Len(t, rows, 3)
	require.Equal(t, 2, rows[2].revision)
	require.Equal(t, "unattributable", rows[2].reason)
	require.Equal(t, 1, rows[2].hashrates)
	require.False(t, rows[2].noSourceData)
	rows = reportingHourRows(t, f, reporting)
	require.Len(t, rows, 3)
	require.True(t, rows[1].noSourceData)
	require.Equal(t, 2, rows[2].revision)
	require.Equal(t, "closed", rows[2].reason)
	require.False(t, rows[2].noSourceData)
	require.Zero(t, rows[2].hashrateReports)
}

func TestReportingHourCloserRecordsGapPastCorrectionWindow(t *testing.T) {
	f := setup(t)
	start := time.Now().UTC().Truncate(time.Hour).Add(-10 * 24 * time.Hour)
	resetReportingHourControl(t, f, start)
	interval, _ := reportingHourInterval(t, f, start)
	// Phase 2 refuses expired hours; only phase 1 can record them as a gap.
	_, err := f.db.ExecContext(t.Context(), `UPDATE hosted_account_reporting_hour_control SET closing_through=closing_through+interval '1 hour'`)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `SELECT hosted_reporting_hour_finish_close($1)`, time.Now())
	require.ErrorContains(t, err, "has left the correction window")
	resetReportingHourControl(t, f, start)

	before := time.Now()
	closing := beginReportingHourClose(t, f, time.Now())
	after := time.Now()
	var from, to time.Time
	var reason string
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT from_bucket,to_bucket,reason FROM hosted_account_reporting_hour_gap`).Scan(&from, &to, &reason))
	require.True(t, start.Equal(from))
	require.False(t, to.Before(before.Add(-9*24*time.Hour).Truncate(time.Hour)))
	require.False(t, to.After(after.Add(-9*24*time.Hour).Truncate(time.Hour)))
	require.NotEmpty(t, reason)
	require.True(t, to.Add(24*time.Hour).Equal(closing), "the closer resumes at the window and closes one batch")
	require.Equal(t, 24, finishReportingHourClose(t, f, time.Now()))
	control := readReportingHourControl(t, f)
	require.True(t, closing.Equal(control.closed))
	rows := reportingHourRows(t, f, interval)
	require.Len(t, rows, 24)
	require.True(t, to.Equal(rows[0].bucket), "no zero rows for expired hours")

	require.Equal(t, 24, closeReportingHours(t, f, time.Now()))
	var gaps, covered int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT (SELECT count(*) FROM hosted_account_reporting_hour_gap),
 (SELECT count(*) FROM hosted_account_reporting_hour h JOIN hosted_account_reporting_hour_gap g ON h.bucket>=g.from_bucket AND h.bucket<g.to_bucket)`).Scan(&gaps, &covered))
	require.Equal(t, 1, gaps, "a caught-up closer records no further gap")
	require.Zero(t, covered, "no hour row exists under a gap row")

	// A marker for an hour older than the window is discarded without a revision.
	_, err = f.db.ExecContext(t.Context(), `INSERT INTO hosted_account_reporting_hour_marker(interval_id,bucket) VALUES($1,$2)`, interval, start.Add(time.Hour))
	require.NoError(t, err)
	require.Zero(t, reviseReportingHours(t, f, time.Now()))
	require.Zero(t, reportingHourMarkerCount(t, f, interval))
	require.Len(t, reportingHourRows(t, f, interval), 48)
}

func TestReportingHourPhaseTwoFailureResumes(t *testing.T) {
	f := setup(t)
	hour := time.Now().UTC().Truncate(time.Hour).Add(-3 * time.Hour)
	resetReportingHourControl(t, f, hour)
	interval, _ := reportingHourInterval(t, f, hour)
	insertReportingHourSamples(t, f, interval, reportingHourSample{at: hour.Add(10 * time.Minute), metric: 1, value: 100e12, complete: true, source: 1})
	asOf := hour.Add(time.Hour + reportingHourGrace)
	require.True(t, hour.Add(time.Hour).Equal(beginReportingHourClose(t, f, asOf)))
	insertReportingHourSamples(t, f, interval, reportingHourSample{at: hour.Add(20 * time.Minute), metric: 1, value: 100e12, complete: true, source: 1})
	require.Equal(t, 1, reportingHourMarkerCount(t, f, interval), "a write to a closing hour is marked")

	_, err := f.db.ExecContext(t.Context(), `ALTER TABLE hosted_account_reporting_hour ADD CONSTRAINT ck_reporting_hour_injected_failure CHECK(false) NOT VALID`)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `SELECT hosted_reporting_hour_finish_close($1)`, asOf)
	require.ErrorContains(t, err, "ck_reporting_hour_injected_failure")
	control := readReportingHourControl(t, f)
	require.True(t, hour.Equal(control.closed), "a failed phase 2 does not advance closed_through")
	require.True(t, hour.Add(time.Hour).Equal(control.closing))
	require.Nil(t, control.lastSuccess)
	require.Empty(t, reportingHourRows(t, f, interval))
	require.Equal(t, 1, reportingHourMarkerCount(t, f, interval), "the failed claim is rolled back")

	_, err = f.db.ExecContext(t.Context(), `ALTER TABLE hosted_account_reporting_hour DROP CONSTRAINT ck_reporting_hour_injected_failure`)
	require.NoError(t, err)
	require.Equal(t, 1, closeReportingHours(t, f, asOf))
	rows := reportingHourRows(t, f, interval)
	require.Len(t, rows, 1)
	require.Equal(t, 2, rows[0].hashrates)
	require.Zero(t, reportingHourMarkerCount(t, f, interval))
	require.True(t, hour.Add(time.Hour).Equal(readReportingHourControl(t, f).closed))
}

func reportingHourJob(t *testing.T, f fixture) int {
	t.Helper()
	var job int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT job_id FROM timescaledb_information.jobs WHERE proc_name='hosted_reporting_hour_job'`).Scan(&job))
	return job
}

func setReportingHourJobEnabled(t *testing.T, f fixture, job int, enabled bool) {
	t.Helper()
	_, err := f.db.ExecContext(t.Context(), `SELECT alter_job(job_id,config=>config||jsonb_build_object('enabled',$2::boolean))
 FROM timescaledb_information.jobs WHERE job_id=$1`, job, enabled)
	require.NoError(t, err)
}

func TestReportingHourJobClosesDueHours(t *testing.T) {
	f := setup(t)
	var config []byte
	var schedule, maxRuntime, retry string
	var fixed bool
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT config,schedule_interval::text,max_runtime::text,retry_period::text,fixed_schedule
 FROM timescaledb_information.jobs WHERE proc_name='hosted_reporting_hour_job'`).Scan(&config, &schedule, &maxRuntime, &retry, &fixed))
	require.JSONEq(t, `{"enabled":true,"grace":"30 minutes","correction_window":"9 days","max_hours":24,"lock_timeout_ms":1000,"revision_batch":10000}`, string(config))
	require.Equal(t, "00:15:00", schedule)
	require.Equal(t, "00:10:00", maxRuntime)
	require.Equal(t, "00:05:00", retry)
	require.False(t, fixed)

	job := reportingHourJob(t, f)
	hour := time.Now().UTC().Truncate(time.Hour).Add(-3 * time.Hour)
	resetReportingHourControl(t, f, hour)
	interval, _ := reportingHourInterval(t, f, hour)
	setReportingHourJobEnabled(t, f, job, false)
	_, err := f.db.ExecContext(t.Context(), `CALL run_job($1)`, job)
	require.NoError(t, err)
	control := readReportingHourControl(t, f)
	require.True(t, hour.Equal(control.closing), "a disabled job writes nothing")
	require.True(t, hour.Equal(control.closed))
	require.Empty(t, reportingHourRows(t, f, interval))

	setReportingHourJobEnabled(t, f, job, true)
	// A phase 2 failure is raised to the caller, after phase 1 committed.
	_, err = f.db.ExecContext(t.Context(), `ALTER TABLE hosted_account_reporting_hour ADD CONSTRAINT ck_reporting_hour_run_failure CHECK(false) NOT VALID`)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `CALL run_job($1)`, job)
	require.ErrorContains(t, err, "ck_reporting_hour_run_failure")
	control = readReportingHourControl(t, f)
	require.True(t, control.closing.After(hour))
	require.True(t, hour.Equal(control.closed))
	_, err = f.db.ExecContext(t.Context(), `ALTER TABLE hosted_account_reporting_hour DROP CONSTRAINT ck_reporting_hour_run_failure`)
	require.NoError(t, err)

	before := time.Now()
	_, err = f.db.ExecContext(t.Context(), `CALL run_job($1)`, job)
	require.NoError(t, err)
	after := time.Now()
	control = readReportingHourControl(t, f)
	require.False(t, control.closed.Before(before.Add(-reportingHourGrace).Truncate(time.Hour)))
	require.False(t, control.closed.After(after.Add(-reportingHourGrace).Truncate(time.Hour)))
	require.True(t, control.closed.Equal(control.closing))
	require.NotNil(t, control.lastSuccess)
	require.Len(t, reportingHourRows(t, f, interval), int(control.closed.Sub(hour)/time.Hour))
}

func TestReportingHourJobFailureReachesJobErrors(t *testing.T) {
	f := setup(t)
	var capacity int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT current_setting('timescaledb.max_background_workers')::integer-count(*)::integer
 FROM pg_stat_activity WHERE backend_type LIKE 'TimescaleDB Background Worker%' AND backend_type<>'TimescaleDB Background Worker Launcher'
 AND datname IS DISTINCT FROM current_database()`).Scan(&capacity))
	if capacity < 2 {
		t.Skip("scheduled integration needs a scheduler and a worker slot")
	}
	job := reportingHourJob(t, f)
	hour := time.Now().UTC().Truncate(time.Hour).Add(-3 * time.Hour)
	resetReportingHourControl(t, f, hour)
	interval, _ := reportingHourInterval(t, f, hour)
	_, err := f.db.ExecContext(t.Context(), `ALTER TABLE hosted_account_reporting_hour ADD CONSTRAINT ck_reporting_hour_job_failure CHECK(false) NOT VALID`)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := f.db.ExecContext(context.Background(), `SELECT alter_job($1,scheduled=>false)`, job) //nolint:usetesting // Cleanup runs after the test context is canceled.
		require.NoError(t, err)
	})
	_, err = f.db.ExecContext(t.Context(), `SELECT alter_job($1,scheduled=>true,next_start=>now())`, job)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `SELECT _timescaledb_functions.start_background_workers()`)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		var failed bool
		err := f.db.QueryRowContext(t.Context(), `SELECT EXISTS(SELECT 1 FROM timescaledb_information.job_errors
 WHERE job_id=$1 AND err_message LIKE '%ck_reporting_hour_job_failure%')`, job).Scan(&failed)
		return err == nil && failed
	}, 30*time.Second, 50*time.Millisecond, "the scheduled failure must reach job_errors")
	_, err = f.db.ExecContext(t.Context(), `SELECT alter_job($1,scheduled=>false)`, job)
	require.NoError(t, err)
	control := readReportingHourControl(t, f)
	require.True(t, control.closing.After(hour), "phase 1 committed before phase 2 failed")
	require.True(t, hour.Equal(control.closed))
	require.Empty(t, reportingHourRows(t, f, interval))
}
