package hostedaccounts_test

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts/reporting"
	"github.com/stretchr/testify/require"
)

// reportEvidenceHours appends one evidence row per hour of an allocated
// interval, as the hourly job's close (revision 1) or revise step would.
func reportEvidenceHours(t *testing.T, db sqlc.DBTX, f fixture, interval int64, from time.Time, hours, revision int, hashrate float64) {
	t.Helper()
	reportingHourSnapshot(t, f, interval)
	_, err := db.ExecContext(t.Context(), `INSERT INTO hosted_account_reporting_hour(interval_id,bucket,revision,capture_reason,no_source_data,window_start,window_end,
 hashrate_report_count,hashrate_count,hashrate_positive_count,hashrate_sum,hashrate_complete,hashrate_source_oldest_at,hashrate_source_newest_at,
 power_report_count,power_count,temperature_report_count,temperature_count,state_report_count,state_count)
 SELECT $1,b,$4,'closed',false,b,b+interval '1 hour',1,1,1,$5,true,b,b+interval '59 minutes',0,0,0,0,0,0
 FROM generate_series($2::timestamptz,$2::timestamptz+($3::int-1)*interval '1 hour',interval '1 hour') b`, interval, from, hours, revision, hashrate)
	require.NoError(t, err)
}

// reportAsOf is the database clock, which also stamps evidence capture times.
func reportAsOf(t *testing.T, f fixture) time.Time {
	t.Helper()
	var now time.Time
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT clock_timestamp()`).Scan(&now))
	return now.UTC()
}

// frozenReportEvidence reads a report's evidence through the publication
// queries: one "interval bucket reason revision" line per hour.
func frozenReportEvidence(t *testing.T, f fixture, report int64) []string {
	t.Helper()
	q := sqlc.New(f.db)
	intervals, err := q.ListHostedReportEvidenceIntervals(t.Context(), sqlc.ListHostedReportEvidenceIntervalsParams{ReportInternalID: report, PageLimit: 1000})
	require.NoError(t, err)
	out := []string{}
	for _, interval := range intervals {
		hours, err := q.ListHostedReportEvidence(t.Context(), sqlc.ListHostedReportEvidenceParams{ReportInternalID: report, IntervalID: interval.IntervalID})
		require.NoError(t, err)
		for _, hour := range hours {
			out = append(out, fmt.Sprintf("%d %s %s %d", interval.IntervalID, hour.Bucket.UTC().Format(time.RFC3339), hour.CaptureReason, hour.Revision))
		}
	}
	return out
}

// publishedObservations returns one evidence CSV member of an issued report.
func publishedObservations(t *testing.T, f fixture, account, report, member string) [][]string {
	t.Helper()
	data := downloadReportArtifact(t, f, f.user, account, report, "evidence.zip")
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	require.NoError(t, err)
	for _, file := range archive.File {
		if file.Name != member {
			continue
		}
		reader, err := file.Open()
		require.NoError(t, err)
		rows, err := csv.NewReader(reader).ReadAll()
		require.NoError(t, err)
		require.NoError(t, reader.Close())
		return rows
	}
	t.Fatalf("evidence archive has no %s", member)
	return nil
}
func observationColumn(t *testing.T, rows [][]string, name string) []string {
	t.Helper()
	index := slices.Index(rows[0], name)
	require.GreaterOrEqual(t, index, 0, name)
	out := make([]string, 0, len(rows)-1)
	for _, row := range rows[1:] {
		out = append(out, row[index])
	}
	return out
}

func TestHostedReportEvidenceCutoffWaitsForWritersAndExcludesLaterRevisions(t *testing.T) {
	f, account, owner, in, p := reportContextFixture(t)
	interval, hour := in.Coverage[0].AllocationIntervalID, p.Start.Add(5*time.Hour)
	saved, err := saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, account.AccountID, 0, "cutoff", in)
	require.NoError(t, err)
	reportEvidenceHours(t, f.db, f, interval, hour, 1, 1, 100e12)
	opts := hostedaccounts.DraftReportOptions{Period: p, InputRevision: saved.Revision, OwnerUserID: owner}
	opts.Period.AsOf = reportAsOf(t, f)
	first, err := f.svc.PrepareReport(t.Context(), f.host, f.org, account.AccountID, opts)
	require.NoError(t, err)
	selected := fmt.Sprintf("%d %s closed ", interval, hour.Format(time.RFC3339))
	require.Equal(t, []string{selected + "1"}, frozenReportEvidence(t, f, first.ID))

	// A reviser holds the control row from before its first insert until it commits.
	writer, err := f.db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	defer func() { _ = writer.Rollback() }()
	_, err = writer.ExecContext(t.Context(), `SELECT 1 FROM hosted_account_reporting_hour_control WHERE singleton FOR UPDATE`)
	require.NoError(t, err)
	reportEvidenceHours(t, writer, f, interval, hour, 1, 2, 200e12)
	require.Equal(t, []string{selected + "1"}, frozenReportEvidence(t, f, first.ID), "an uncommitted revision is outside the earlier cutoff")

	// A report prepared now waits for that writer instead of freezing a cutoff
	// that its row could later satisfy.
	opts.Period.Kind, opts.Period.AsOf = "ad_hoc", reportAsOf(t, f)
	request, err := f.svc.CreateReportRequest(t.Context(), f.host, f.org, account.AccountID, hostedaccounts.ReportRequest{ReceivedAt: time.Now().UTC().Add(-time.Hour), SourceRef: "cutoff request receipt", Requester: "Client", Scope: "Cutoff package", OwnerUserID: owner})
	require.NoError(t, err)
	opts.Period.RequestReceivedAt, opts.Period.RequestRef = request.ReceivedAt, request.RequestID
	type prepared struct {
		report hostedaccounts.ReportRecord
		err    error
	}
	done := make(chan prepared, 1)
	go func() {
		report, err := f.svc.PrepareReport(context.WithoutCancel(t.Context()), f.host, f.org, account.AccountID, opts)
		done <- prepared{report, err}
	}()
	require.Eventually(t, func() bool {
		var waiting int
		require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%hosted_report_evidence_cutoff%'`).Scan(&waiting))
		return waiting == 1
	}, 10*time.Second, 20*time.Millisecond, "the cutoff must wait for the in-flight evidence writer")
	select {
	case result := <-done:
		t.Fatalf("report prepared while an evidence writer was in flight: %+v", result.err)
	default:
	}
	require.NoError(t, writer.Commit())
	second := <-done
	require.NoError(t, second.err)
	require.Equal(t, []string{selected + "2"}, frozenReportEvidence(t, f, second.report.ID), "the waiting report reads the committed revision")
	require.Equal(t, []string{selected + "1"}, frozenReportEvidence(t, f, first.ID), "the commit cannot enter the earlier report")

	// A third revision after both cutoffs enters neither, including at publication.
	reportEvidenceHours(t, f.db, f, interval, hour, 1, 3, 300e12)
	issued, err := f.svc.PublishReport(t.Context(), f.host, f.org, account.AccountID, first.ReportID, first.Revision, "reviewed incomplete sources")
	require.NoError(t, err)
	rows := publishedObservations(t, f, account.AccountID, issued.ReportID, "observations.csv")
	require.Equal(t, []string{"1"}, observationColumn(t, rows, "revision"))
	require.Equal(t, []string{"100000000000000"}, observationColumn(t, rows, "hashrate_sum_hs"))
	_, err = f.db.ExecContext(t.Context(), `UPDATE hosted_report SET evidence_cutoff=clock_timestamp() WHERE id=$1`, issued.ID)
	require.ErrorContains(t, err, "issued reports are immutable")
}

func TestHostedReportEvidenceCutoffAdmitsAWriterStepWhole(t *testing.T) {
	f, account, owner, in, p := reportContextFixture(t)
	interval, hour := in.Coverage[0].AllocationIntervalID, p.Start.Add(5*time.Hour)
	saved, err := saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, account.AccountID, 0, "writer step", in)
	require.NoError(t, err)

	// One writer step inserts an hour and then a gap; the report's as_of falls
	// between the two inserts.
	writer, err := f.db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	defer func() { _ = writer.Rollback() }()
	_, err = writer.ExecContext(t.Context(), `SELECT 1 FROM hosted_account_reporting_hour_control WHERE singleton FOR UPDATE`)
	require.NoError(t, err)
	reportEvidenceHours(t, writer, f, interval, hour, 1, 1, 100e12)
	asOf := reportAsOf(t, f)
	_, err = writer.ExecContext(t.Context(), `SELECT pg_sleep(0.01)`)
	require.NoError(t, err)
	_, err = writer.ExecContext(t.Context(), `INSERT INTO hosted_account_reporting_hour_gap(from_bucket,to_bucket,reason) VALUES($1,$2,'closer fell behind past the correction window')`, hour.Add(time.Hour), hour.Add(2*time.Hour))
	require.NoError(t, err)
	require.NoError(t, writer.Commit())
	var captured, recorded time.Time
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT (SELECT captured_at FROM hosted_account_reporting_hour WHERE interval_id=$1),
 (SELECT recorded_at FROM hosted_account_reporting_hour_gap WHERE from_bucket=$2)`, interval, hour.Add(time.Hour)).Scan(&captured, &recorded))
	require.True(t, captured.Equal(recorded), "one step's rows share a capture time: %s and %s", captured, recorded)
	require.False(t, captured.After(asOf))

	opts := hostedaccounts.DraftReportOptions{Period: p, InputRevision: saved.Revision, OwnerUserID: owner}
	opts.Period.AsOf = asOf
	report, err := f.svc.PrepareReport(t.Context(), f.host, f.org, account.AccountID, opts)
	require.NoError(t, err)
	require.Equal(t, []string{
		fmt.Sprintf("%d %s closed 1", interval, hour.Format(time.RFC3339)),
		fmt.Sprintf("%d %s gap 0", interval, hour.Add(time.Hour).Format(time.RFC3339)),
	}, frozenReportEvidence(t, f, report.ID), "an as_of inside a committed step admits the whole step")
}

func TestHostedReportEvidenceKeepsCoverageEndingWithinAMicrosecondOfThePeriod(t *testing.T) {
	f, account, owner, in, p := reportContextFixture(t)
	// The segment overlaps the report by 100 ns; its stored end_at, at microsecond
	// precision, equals the period start.
	c := &in.Coverage[0]
	c.End = p.Start.Add(100 * time.Nanosecond)
	saved, err := saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, account.AccountID, 0, "sub-microsecond coverage end", in)
	require.NoError(t, err)
	var storedEnd time.Time
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT end_at FROM hosted_reporting_fact WHERE organization_id=$1 AND kind='coverage' AND allocation_interval_id=$2`, f.org, c.AllocationIntervalID).Scan(&storedEnd))
	require.True(t, storedEnd.Equal(p.Start), "stored end %s", storedEnd)
	reportEvidenceHours(t, f.db, f, c.AllocationIntervalID, p.Start, 2, 1, 100e12)

	opts := hostedaccounts.DraftReportOptions{Period: p, InputRevision: saved.Revision, OwnerUserID: owner}
	opts.Period.AsOf = reportAsOf(t, f)
	draft, err := f.svc.PrepareReport(t.Context(), f.host, f.org, account.AccountID, opts)
	require.NoError(t, err)
	require.Equal(t, []string{fmt.Sprintf("%d %s closed 1", c.AllocationIntervalID, p.Start.Format(time.RFC3339))}, frozenReportEvidence(t, f, draft.ID),
		"the hourly export keeps the one hour the segment overlaps")
	days, err := sqlc.New(f.db).ListHostedReportEvidenceDays(t.Context(), sqlc.ListHostedReportEvidenceDaysParams{ReportInternalID: draft.ID, IntervalID: c.AllocationIntervalID})
	require.NoError(t, err)
	require.Len(t, days, 1)
	require.EqualValues(t, 1, days[0].EvidenceHours.Int32, "the day rollup keeps the same hour")
	sites, err := sqlc.New(f.db).HostedReportEvidenceSites(t.Context(), draft.ID)
	require.NoError(t, err)
	require.Equal(t, []sql.NullInt64{{Int64: c.SiteID, Valid: true}}, sites)
}

func TestHostedReportCorrectionReadsLaterRevisionAndGaps(t *testing.T) {
	f, account, owner, in, p := reportContextFixture(t)
	interval, hour := in.Coverage[0].AllocationIntervalID, p.Start.Add(5*time.Hour)
	saved, err := saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, account.AccountID, 0, "correction", in)
	require.NoError(t, err)
	reportEvidenceHours(t, f.db, f, interval, hour, 2, 1, 100e12)
	opts := hostedaccounts.DraftReportOptions{Period: p, InputRevision: saved.Revision, OwnerUserID: owner}
	opts.Period.AsOf = reportAsOf(t, f)
	draft, err := f.svc.PrepareReport(t.Context(), f.host, f.org, account.AccountID, opts)
	require.NoError(t, err)
	original, err := f.svc.PublishReport(t.Context(), f.host, f.org, account.AccountID, draft.ReportID, draft.Revision, "reviewed incomplete sources")
	require.NoError(t, err)

	// After the original: one hour is revised, and the closer records a gap over
	// two hours it never wrote and over one it did.
	reportEvidenceHours(t, f.db, f, interval, hour, 1, 2, 200e12)
	_, err = f.db.ExecContext(t.Context(), `INSERT INTO hosted_account_reporting_hour_gap(from_bucket,to_bucket,reason) VALUES($1,$2,'closer fell behind past the correction window')`, hour.Add(time.Hour), hour.Add(4*time.Hour))
	require.NoError(t, err)
	opts.CorrectionOf, opts.CorrectionReason = original.ReportID, "late telemetry correction"
	opts.Period.AsOf = reportAsOf(t, f)
	draft, err = f.svc.PrepareReport(t.Context(), f.host, f.org, account.AccountID, opts)
	require.NoError(t, err)
	correction, err := f.svc.PublishReport(t.Context(), f.host, f.org, account.AccountID, draft.ReportID, draft.Revision, "reviewed incomplete sources")
	require.NoError(t, err)

	rows := publishedObservations(t, f, account.AccountID, original.ReportID, "observations.csv")
	require.Equal(t, []string{"1", "1"}, observationColumn(t, rows, "revision"), "the original keeps the revisions it was prepared with")
	require.Equal(t, []string{"closed", "closed"}, observationColumn(t, rows, "capture_reason"))
	rows = publishedObservations(t, f, account.AccountID, correction.ReportID, "observations.csv")
	require.Equal(t, []string{"2", "1", "0", "0"}, observationColumn(t, rows, "revision"), "the correction reads the later revision and the gap hours without evidence")
	require.Equal(t, []string{"closed", "closed", "gap", "gap"}, observationColumn(t, rows, "capture_reason"))
	require.Equal(t, []string{"200000000000000", "100000000000000", "", ""}, observationColumn(t, rows, "hashrate_sum_hs"))
	require.Equal(t, "closer fell behind past the correction window", observationColumn(t, rows, "gap_reason")[3])
	for _, report := range []hostedaccounts.ReportRecord{original, correction} {
		require.Len(t, frozenReportEvidence(t, f, report.ID), map[int64]int{original.ID: 2, correction.ID: 4}[report.ID], "the cutoff still selects what was published")
	}
}

func TestHostedReportEvidenceExportsGapsOfAnAllocationWithoutCapturedHours(t *testing.T) {
	f, account, owner, in, p := reportContextFixture(t)
	interval := in.Coverage[0].AllocationIntervalID
	saved, err := saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, account.AccountID, 0, "gap-only allocation", in)
	require.NoError(t, err)
	// The closer fell behind for the allocation's covered hours, so it captured
	// no hour, and therefore no allocation snapshot, for the interval.
	hour := p.Start.Add(5 * time.Hour)
	_, err = f.db.ExecContext(t.Context(), `INSERT INTO hosted_account_reporting_hour_gap(from_bucket,to_bucket,reason) VALUES($1,$2,'closer fell behind past the correction window')`, hour, hour.Add(3*time.Hour))
	require.NoError(t, err)
	var snapshots int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_account_reporting_allocation_snapshot WHERE interval_id=$1`, interval).Scan(&snapshots))
	require.Zero(t, snapshots)

	opts := hostedaccounts.DraftReportOptions{Period: p, InputRevision: saved.Revision, OwnerUserID: owner}
	opts.Period.AsOf = reportAsOf(t, f)
	draft, err := f.svc.PrepareReport(t.Context(), f.host, f.org, account.AccountID, opts)
	require.NoError(t, err)
	issued, err := f.svc.PublishReport(t.Context(), f.host, f.org, account.AccountID, draft.ReportID, draft.Revision, "reviewed incomplete sources")
	require.NoError(t, err)
	rows := publishedObservations(t, f, account.AccountID, issued.ReportID, "observations.csv")
	require.Equal(t, []string{"gap", "gap", "gap"}, observationColumn(t, rows, "capture_reason"), "recorded gap hours of a covered allocation are exported")
	require.Equal(t, hour.Format(time.RFC3339), observationColumn(t, rows, "bucket")[0])

	days, err := sqlc.New(f.db).ListHostedReportEvidenceDays(t.Context(), sqlc.ListHostedReportEvidenceDaysParams{ReportInternalID: draft.ID, IntervalID: interval})
	require.NoError(t, err)
	require.Len(t, days, 1)
	require.EqualValues(t, 3, days[0].GapHours.Int32, "the day rollup counts the same gap hours")
}

func TestHostedReportEvidenceCountsAnHourOncePerIntervalAndRollsUpByDay(t *testing.T) {
	f, account, owner, in, p := reportContextFixture(t)
	first, second := in.Coverage[0], in.Coverage[0]
	first.End = p.Start.Add(90 * time.Minute)
	second.ID, second.Start = "later-coverage", first.End
	second.End = p.Start.Add(50 * time.Hour)
	in.Coverage = []reporting.CoverageSegment{first, second}
	saved, err := saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, account.AccountID, 0, "split commercial coverage", in)
	require.NoError(t, err)
	// Hours from one before the period to past the end of coverage. The hour at
	// 01:00 overlaps both adjacent segments.
	reportEvidenceHours(t, f.db, f, first.AllocationIntervalID, p.Start.Add(-time.Hour), 60, 1, 100e12)
	reportEvidenceHours(t, f.db, f, first.AllocationIntervalID, p.Start.Add(30*time.Hour), 1, 2, 300e12)
	_, err = f.db.ExecContext(t.Context(), `INSERT INTO hosted_account_reporting_hour_gap(from_bucket,to_bucket,reason) VALUES($1,$2,'closer fell behind past the correction window')`, p.Start.Add(-48*time.Hour), p.Start.Add(-time.Hour))
	require.NoError(t, err)
	opts := hostedaccounts.DraftReportOptions{Period: p, InputRevision: saved.Revision, OwnerUserID: owner}
	opts.Period.AsOf = reportAsOf(t, f)
	draft, err := f.svc.PrepareReport(t.Context(), f.host, f.org, account.AccountID, opts)
	require.NoError(t, err)
	issued, err := f.svc.PublishReport(t.Context(), f.host, f.org, account.AccountID, draft.ReportID, draft.Revision, "reviewed incomplete sources")
	require.NoError(t, err)
	rows := publishedObservations(t, f, account.AccountID, issued.ReportID, "observations.csv")
	buckets := observationColumn(t, rows, "bucket")
	require.Len(t, buckets, 50, "hours inside the period and a coverage segment, each once")
	require.Equal(t, p.Start.Format(time.RFC3339), buckets[0])
	require.Equal(t, p.Start.Add(49*time.Hour).Format(time.RFC3339), buckets[49])
	require.Equal(t, "2", observationColumn(t, rows, "revision")[30])

	// The same evidence over a quarter, as the UTC-day rollup large reports export.
	opts.Period.Kind, opts.Period.Start = "quarterly", p.End.AddDate(0, -3, 0)
	opts.Period.AsOf = reportAsOf(t, f)
	draft, err = f.svc.PrepareReport(t.Context(), f.host, f.org, account.AccountID, opts)
	require.NoError(t, err)
	days, err := sqlc.New(f.db).ListHostedReportEvidenceDays(t.Context(), sqlc.ListHostedReportEvidenceDaysParams{ReportInternalID: draft.ID, IntervalID: first.AllocationIntervalID})
	require.NoError(t, err)
	got := []string{}
	for _, d := range days {
		got = append(got, fmt.Sprintf("%s hours=%d gaps=%d revised=%d revision=%d samples=%d sum=%.0f", d.Day.Time.UTC().Format(time.DateOnly),
			d.EvidenceHours.Int32, d.GapHours.Int32, d.RevisedHours.Int32, d.Revision.Int32, d.HashrateCount.Int64, d.HashrateSum.Float64/1e12))
	}
	day := func(offset int) string { return p.Start.AddDate(0, 0, offset).Format(time.DateOnly) }
	require.Equal(t, []string{
		day(-2) + " hours=0 gaps=24 revised=0 revision=0 samples=0 sum=0",
		day(-1) + " hours=1 gaps=23 revised=0 revision=1 samples=1 sum=100",
		day(0) + " hours=24 gaps=0 revised=0 revision=1 samples=24 sum=2400",
		day(1) + " hours=24 gaps=0 revised=1 revision=2 samples=24 sum=2600",
		day(2) + " hours=2 gaps=0 revised=0 revision=1 samples=2 sum=200",
	}, got)
}

// Capture the generated statement so the plan test executes the production SQL.
type reportEvidencePlanDB struct {
	*sql.DB
	query string
	args  []any
}

func (d *reportEvidencePlanDB) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	d.query, d.args = query, args
	rows, err := d.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("read report evidence: %w", err)
	}
	return rows, nil
}

type reportEvidencePlan struct {
	Relation  string               `json:"Relation Name"`
	Rows      float64              `json:"Actual Rows"`
	Loops     float64              `json:"Actual Loops"`
	Filtered  float64              `json:"Rows Removed by Filter"`
	Rechecked float64              `json:"Rows Removed by Index Recheck"`
	Plans     []reportEvidencePlan `json:"Plans"`
}

func (p reportEvidencePlan) visited(relation string) float64 {
	var rows float64
	if p.Relation == relation {
		rows = (p.Rows + p.Filtered + p.Rechecked) * p.Loops
	}
	for _, child := range p.Plans {
		rows += child.visited(relation)
	}
	return rows
}

func TestHostedReportEvidenceReadsOnlyThePeriodOfItsOwnIntervals(t *testing.T) {
	f, account, owner, in, p := reportContextFixture(t)
	c := in.Coverage[0]
	saved, err := saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, account.AccountID, 0, "evidence plan", in)
	require.NoError(t, err)
	// Two years of retained hours for the interval, and many other accounts' snapshots.
	const retained = 2 * 365 * 24
	reportEvidenceHours(t, f.db, f, c.AllocationIntervalID, p.End.Add(-retained*time.Hour), retained, 1, 100e12)
	other := f.client(t, "Unrelated snapshots", f.user, hostedaccounts.AllCapabilities)
	_, err = f.db.ExecContext(t.Context(), `INSERT INTO hosted_account_reporting_allocation_snapshot(interval_id,organization_id,client_id,site_id,kind,asset_id,device_id,device_identifier,started_at)
 SELECT interval_id+n*1000,organization_id,$2,site_id,kind,asset_id,device_id,device_identifier,started_at
 FROM hosted_account_reporting_allocation_snapshot CROSS JOIN generate_series(1,20000) n WHERE interval_id=$1`, c.AllocationIntervalID, other.ID)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `ANALYZE hosted_account_reporting_hour; ANALYZE hosted_account_reporting_allocation_snapshot; ANALYZE hosted_report; ANALYZE hosted_reporting_fact`)
	require.NoError(t, err)
	opts := hostedaccounts.DraftReportOptions{Period: p, InputRevision: saved.Revision, OwnerUserID: owner}
	opts.Period.AsOf = reportAsOf(t, f)
	draft, err := f.svc.PrepareReport(t.Context(), f.host, f.org, account.AccountID, opts)
	require.NoError(t, err)
	period := p.End.Sub(p.Start).Hours()
	capture := &reportEvidencePlanDB{DB: f.db}
	q := sqlc.New(capture)
	for name, read := range map[string]func() (int, error){
		"intervals": func() (int, error) {
			rows, err := q.ListHostedReportEvidenceIntervals(t.Context(), sqlc.ListHostedReportEvidenceIntervalsParams{ReportInternalID: draft.ID, PageLimit: 512})
			return len(rows), err
		},
		"hours": func() (int, error) {
			rows, err := q.ListHostedReportEvidence(t.Context(), sqlc.ListHostedReportEvidenceParams{ReportInternalID: draft.ID, IntervalID: c.AllocationIntervalID})
			return len(rows), err
		},
		"days": func() (int, error) {
			rows, err := q.ListHostedReportEvidenceDays(t.Context(), sqlc.ListHostedReportEvidenceDaysParams{ReportInternalID: draft.ID, IntervalID: c.AllocationIntervalID})
			return len(rows), err
		},
		"sites": func() (int, error) {
			rows, err := q.HostedReportEvidenceSites(t.Context(), draft.ID)
			return len(rows), err
		},
	} {
		count, err := read()
		require.NoError(t, err)
		require.EqualValues(t, map[string]float64{"intervals": 1, "hours": period, "days": period / 24, "sites": 1}[name], count, name)
		var encoded []byte
		require.NoError(t, f.db.QueryRowContext(t.Context(), "EXPLAIN (ANALYZE, FORMAT JSON) "+capture.query, capture.args...).Scan(&encoded))
		var plans []struct{ Plan reportEvidencePlan }
		require.NoError(t, json.Unmarshal(encoded, &plans))
		require.Len(t, plans, 1)
		hours := plans[0].Plan.visited("hosted_account_reporting_hour")
		snapshots := plans[0].Plan.visited("hosted_account_reporting_allocation_snapshot")
		allocations := plans[0].Plan.visited("hosted_account_asset_interval")
		t.Logf("%s: rows=%d hour rows visited=%.0f snapshot rows visited=%.0f interval rows visited=%.0f", name, count, hours, snapshots, allocations)
		require.LessOrEqual(t, hours, 2*period, "%s must read the period's hours, not the %d retained", name, retained)
		require.Less(t, snapshots, float64(100), "%s must scope allocation snapshots before reading other accounts", name)
		require.Less(t, allocations, float64(100), "%s must look allocations up by interval", name)
	}
}

func TestHostedReportPreparationAdmitsBeforeTakingTheEvidenceCutoff(t *testing.T) {
	f, account, owner, in, p := reportContextFixture(t)
	saved, err := saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, account.AccountID, 0, "admission", in)
	require.NoError(t, err)
	// An evidence writer holds the control row, so any cutoff would wait.
	writer, err := f.db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	defer func() { _ = writer.Rollback() }()
	_, err = writer.ExecContext(t.Context(), `SELECT 1 FROM hosted_account_reporting_hour_control WHERE singleton FOR UPDATE`)
	require.NoError(t, err)

	outsider := addUser(t, f.db, f.org)
	opts := hostedaccounts.DraftReportOptions{Period: p, InputRevision: saved.Revision, OwnerUserID: owner}
	opts.Period.AsOf = reportAsOf(t, f)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	_, err = f.svc.PrepareReport(ctx, outsider, f.org, account.AccountID, opts)
	require.True(t, fleeterror.IsForbiddenError(err), "an actor without reporting authority is refused before the cutoff: %v", err)
	require.NoError(t, ctx.Err(), "refusal must not wait behind the evidence writer")
}
