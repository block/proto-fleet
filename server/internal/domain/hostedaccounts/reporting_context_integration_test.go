package hostedaccounts_test

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts/reporting"
	"github.com/stretchr/testify/require"
)

func reportContextFixture(t *testing.T) (fixture, hostedaccounts.Account, string, reporting.Inputs, reporting.Period) {
	t.Helper()
	f := setup(t)
	account := f.client(t, "Context report", f.user, hostedaccounts.AllCapabilities)
	owner, _ := reportActors(t, f, account.AccountID)
	miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	var site int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO site(org_id,name,slug) VALUES($1,'Context','context') RETURNING id`, f.org).Scan(&site))
	_, err := f.db.ExecContext(t.Context(), `UPDATE device SET site_id=$1 WHERE id=$2`, site, miner)
	require.NoError(t, err)
	require.NoError(t, f.allocate(t, f.host, f.root, account.AccountID, 1, selected(5, miner, 0)))
	p := reportPeriod()
	p.AsOf = p.AsOf.Round(time.Microsecond)
	start := p.Start.AddDate(-2, 0, 0)
	historyEpoch(t, f, start)
	_, err = f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1 WHERE client_id=$2`, start, account.ID)
	require.NoError(t, err)
	var interval int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT id FROM hosted_account_asset_interval WHERE client_id=$1`, account.ID).Scan(&interval))
	in := reportingInputs(t, f, account.AccountID, f.host)
	in.Orders = []reporting.Order{{ID: "order", Name: "Commercial order", Start: start, SourceRef: "signed"}}
	in.Coverage = []reporting.CoverageSegment{{ID: "coverage", AllocationIntervalID: interval, Kind: 5, AssetID: miner, SiteID: site, ContainerID: miner, OrderID: "order", Start: start, CapacityTHS: "100", SourceRef: "coverage"}}
	return f, account, owner, in, p
}

func decodedContextSource(t *testing.T, f fixture, account hostedaccounts.Account, report hostedaccounts.ReportRecord) reporting.Inputs {
	t.Helper()
	data := downloadReportArtifact(t, f, f.host, account.AccountID, report.ReportID, "source-inputs.json")
	var envelope struct {
		Schema string           `json:"schema"`
		Inputs reporting.Inputs `json:"inputs"`
	}
	require.NoError(t, json.Unmarshal(data, &envelope))
	require.Equal(t, "hosted-report-source-inputs/v1", envelope.Schema)
	return envelope.Inputs
}

func TestReportingContextLatestPoolAndGlobalS3Parity(t *testing.T) {
	f, account, owner, in, p := reportContextFixture(t)
	c := in.Coverage[0]
	module, controller := f.miningModule(t)
	_, err := f.db.ExecContext(t.Context(), `UPDATE device SET site_id=$1 WHERE id=$2`, c.SiteID, controller)
	require.NoError(t, err)
	f.observeModuleAtCurrentSite(t, module, controller)
	require.NoError(t, f.allocate(t, f.host, f.root, account.AccountID, 1, selected(6, module, 0)))
	f.legacyModuleHistory(t)
	_, err = f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1,ended_at=$2 WHERE kind=6 AND asset_id=$3`, c.Start, p.End, module)
	require.NoError(t, err)
	var allocation int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT id FROM hosted_account_asset_interval WHERE kind=6 AND asset_id=$1 LIMIT 1`, module).Scan(&allocation))
	commercial := c
	commercial.ID = "module-old"
	commercial.Kind = 6
	commercial.AssetID = module
	commercial.ContainerID = controller
	commercial.AllocationIntervalID = allocation
	commercial.End = p.Start.Add(-time.Hour).Add(100 * time.Nanosecond)
	in.Coverage = append(in.Coverage, commercial)
	next := commercial
	next.ID = "module-next"
	next.Start = commercial.End
	next.End = p.End
	next.OrderID = "other"
	in.Coverage = append(in.Coverage, next)
	// A same-microsecond later receipt cannot suppress the timely original point.
	in.PoolObservations = []reporting.PoolObservation{
		{ID: "pre-period", Kind: c.Kind, AssetID: c.AssetID, SourceID: "pool", SourceAt: p.Start.Add(-time.Hour), ReceivedAt: p.Start, State: "reporting", SourceRef: "pre-period source"},
		{ID: "tie-a", Kind: c.Kind, AssetID: c.AssetID, SourceID: "pool", SourceAt: p.End.Add(-time.Hour), ReceivedAt: p.End, State: "reporting", SourceRef: "latest online"},
		{ID: "tie-b", Kind: c.Kind, AssetID: c.AssetID, SourceID: "pool", SourceAt: p.End.Add(-time.Hour), ReceivedAt: p.End, State: "failed", SourceRef: "latest offline"},
		{ID: "late", Kind: c.Kind, AssetID: c.AssetID, SourceID: "pool", SourceAt: p.End.Add(-time.Minute), ReceivedAt: p.AsOf.Round(time.Microsecond).Add(time.Nanosecond), State: "reporting", SourceRef: "late receipt"},
	}
	p.AsOf = p.AsOf.Round(time.Microsecond)
	in.Fees = []reporting.FeeLine{{ID: "next-period-nanosecond", SiteID: c.SiteID, ContainerID: c.ContainerID, OrderID: c.OrderID, Start: p.End.Add(time.Nanosecond), End: p.End.Add(time.Hour), Amount: "1", Currency: "USD", SourceRef: "next period"}}
	first := reporting.ObligationInterval{ID: "episode-first", EpisodeID: "episode", Kind: 6, AssetID: module, SiteID: c.SiteID, ContainerID: controller, OrderID: c.OrderID, Start: p.Start.Add(-90 * 24 * time.Hour), End: p.Start.Add(-60 * 24 * time.Hour), AsOf: p.End, ReceivedAt: p.Start, Severity: "S3", TicketRef: "private-ticket", TicketAt: p.Start.Add(-90 * 24 * time.Hour), Qualified: true, SourceRef: "true start"}
	current := first
	current.ID = "episode-current"
	current.Start = commercial.End.Add(-50 * time.Nanosecond)
	current.End = commercial.End
	// A later state in a different order suppresses the old scope's ticket.
	in.Orders = append(in.Orders, reporting.Order{ID: "other", Name: "Other order", Start: c.Start, SourceRef: "signed other"})
	suppressor := first
	suppressor.ID = "episode-suppressor"
	suppressor.Start = p.Start.Add(-time.Hour)
	suppressor.End = p.Start
	suppressor.OrderID = "other"
	suppressor.ClosedAt = p.Start
	suppressor.Severity = "S2"
	// Both states precede the report and round to the same database timestamp.
	// The exact later cross-order state must suppress the earlier open ticket.
	suppressor.Start = commercial.End
	suppressor.End = p.Start
	in.Obligations = []reporting.ObligationInterval{first, current, suppressor}
	saved, err := saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, account.AccountID, 0, "context", in)
	require.NoError(t, err)
	want, err := reporting.CalculateContext(t.Context(), p, in)
	require.NoError(t, err)
	draft, err := f.svc.PrepareReport(t.Context(), f.host, f.org, account.AccountID, hostedaccounts.DraftReportOptions{Period: p, InputRevision: saved.Revision, OwnerUserID: owner})
	require.NoError(t, err)
	requireReportingResultEqual(t, want, draft.Result)
	issued, err := f.svc.PublishReport(t.Context(), f.host, f.org, account.AccountID, draft.ReportID, draft.Revision, "reviewed incomplete context")
	require.NoError(t, err)
	selected := decodedContextSource(t, f, account, issued)
	require.Empty(t, selected.Fees, "conservative database timestamp candidates do not enter selected source or its input budget")
	require.Len(t, selected.PoolObservations, 2, "latest source ties retained; late receipt and older points omitted")
	require.Contains(t, selected.Obligations, first)
	got, err := reporting.CalculateStoredContext(t.Context(), p, selected)
	require.NoError(t, err)
	requireReportingResultEqual(t, want, got)
}

func TestReportingContextIgnoresAccumulatedHistoryAndStreamsRelevantFacts(t *testing.T) {
	f, account, owner, in, p := reportContextFixture(t)
	saved, err := saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, account.AccountID, 0, "baseline", in)
	require.NoError(t, err)
	revision := saved.Revision
	c := in.Coverage[0]
	const total = 10201
	for offset := 0; offset < total; offset += 200 {
		changes := []reporting.FactMutation{}
		for i := offset; i < min(offset+200, total); i++ {
			v := reporting.FeeLine{ID: fmt.Sprintf("old-%05d", i), SiteID: c.SiteID, ContainerID: c.ContainerID, OrderID: c.OrderID, Start: p.Start.AddDate(-1, 0, 0), End: p.Start.AddDate(-1, 0, 0).Add(time.Hour), Amount: "1", Currency: "USD", SourceRef: strings.Repeat("s", 1000)}
			changes = append(changes, reportingUpsertForTest(t, v))
		}
		m, err := f.svc.MutateReportingInputs(t.Context(), f.host, f.org, account.AccountID, revision, "historical fees", nil, changes)
		require.NoError(t, err)
		revision = m.Revision
	}
	q := sqlc.New(f.db)
	size, err := q.ReportingContextSize(t.Context(), sqlc.ReportingContextSizeParams{OrganizationID: f.org, AccountID: account.ID, WindowStart: p.Start, WindowSeconds: p.Start.Unix(), WindowNanos: int64(p.Start.Nanosecond()), PeriodEnd: p.End, AsOf: p.AsOf, EndSeconds: p.End.Unix(), EndNanos: int64(p.End.Nanosecond())})
	require.NoError(t, err)
	require.EqualValues(t, 2, size.FactCount, "only order and coverage enter selected input")
	var stored int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT sum(payload_bytes) FROM hosted_reporting_fact WHERE account_id=$1`, account.ID).Scan(&stored))
	require.Greater(t, stored, int64(8<<20))
	draft, err := f.svc.PrepareReport(t.Context(), f.host, f.org, account.AccountID, hostedaccounts.DraftReportOptions{Period: p, InputRevision: revision, OwnerUserID: owner})
	require.NoError(t, err)
	want, err := reporting.CalculateContext(t.Context(), p, in)
	require.NoError(t, err)
	requireReportingResultEqual(t, want, draft.Result)
	// Move all existing fee identities into the report in bounded accepted batches.
	for offset := 0; offset < total; offset += 200 {
		changes := []reporting.FactMutation{}
		for i := offset; i < min(offset+200, total); i++ {
			v := reporting.FeeLine{ID: fmt.Sprintf("old-%05d", i), SiteID: c.SiteID, ContainerID: c.ContainerID, OrderID: c.OrderID, Start: p.Start, End: p.End, Amount: "1", Currency: "USD", SourceRef: "reviewed fee"}
			changes = append(changes, reportingUpsertForTest(t, v))
		}
		m, err := f.svc.MutateReportingInputs(t.Context(), f.host, f.org, account.AccountID, revision, "current fees", nil, changes)
		require.NoError(t, err)
		revision = m.Revision
	}
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	draft, err = f.svc.PrepareReport(t.Context(), f.host, f.org, account.AccountID, hostedaccounts.DraftReportOptions{Period: p, InputRevision: revision, OwnerUserID: owner, ReportID: draft.ReportID, ExpectedRevision: draft.Revision})
	require.NoError(t, err)
	runtime.ReadMemStats(&after)
	allocated := after.TotalAlloc - before.TotalAlloc
	t.Logf("selected facts=%d prepare total allocated bytes=%d heap before=%d heap after=%d", total+2, allocated, before.HeapAlloc, after.HeapAlloc)
	require.Less(t, allocated, uint64(256<<20))
	require.Equal(t, fmt.Sprint(total), draft.Result.Metrics["accrued_hosting_fees"].Amounts[0].Value)
	issued, err := f.svc.PublishReport(t.Context(), f.host, f.org, account.AccountID, draft.ReportID, draft.Revision, "reviewed incomplete context")
	require.NoError(t, err)
	selected := decodedContextSource(t, f, account, issued)
	require.Len(t, selected.Fees, total)
	require.Equal(t, "1", selected.Fees[0].Amount)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = f.svc.PrepareReport(ctx, f.host, f.org, account.AccountID, hostedaccounts.DraftReportOptions{Period: p, InputRevision: revision, OwnerUserID: owner})
	require.Error(t, err)
}

func requireReportingResultEqual(t *testing.T, want, got reporting.Result) {
	t.Helper()
	a, err := json.Marshal(want)
	require.NoError(t, err)
	b, err := json.Marshal(got)
	require.NoError(t, err)
	require.JSONEq(t, string(a), string(b))
}

func TestReportingContextAnnualCalendarAndFullSupportParity(t *testing.T) {
	f, account, owner, in, p := reportContextFixture(t)
	p.Kind = "annual"
	p.Start = time.Date(2025, 1, 1, 0, 0, 0, 0, time.FixedZone("local", 5*3600+45*60))
	p.End = p.Start.AddDate(1, 0, 0)
	p.Timezone = "Asia/Kathmandu"
	p.AsOf = p.AsOf.Round(time.Microsecond)
	in.Configuration.Timezone = p.Timezone
	c := in.Coverage[0]
	c.Start = p.Start.AddDate(-1, 0, 0)
	in.Coverage[0] = c
	in.Orders[0].Start = c.Start
	_, err := f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1 WHERE id=$2`, c.Start, c.AllocationIntervalID)
	require.NoError(t, err)
	historyEpoch(t, f, c.Start)
	in.Configuration.Sources = []reporting.Source{{ID: "meter", Metric: "meter", Certified: true, SourceRef: "meter certification"}, {ID: "hash", Metric: "hashrate", Certified: true, SourceRef: "hash certification"}}
	in.Configuration.Prerequisites = []reporting.Prerequisite{{Key: "G2", Owner: "owner", Ready: true, SourceRef: "approved"}}
	for i, start := range []time.Time{p.Start.AddDate(-1, 0, 0), p.Start} {
		end := start.AddDate(1, 0, 0)
		in.Meters = append(in.Meters, reporting.MeterInterval{ID: fmt.Sprint("meter-", i), MeterID: "physical", SourceID: "meter", SiteID: c.SiteID, ContainerID: c.ContainerID, OrderID: c.OrderID, Start: start, End: end, KWh: fmt.Sprint(100 + i*10), Quality: "measured", SourceRef: "original meter"})
		in.Hashrates = append(in.Hashrates, reporting.HashrateInterval{ID: fmt.Sprint("hash-", i), Kind: c.Kind, AssetID: c.AssetID, SiteID: c.SiteID, ContainerID: c.ContainerID, OrderID: c.OrderID, SourceID: "hash", Start: start, End: end, HashrateHS: "100000000000000", Complete: true, SourceRef: "original hashrate"})
		in.Incidents = append(in.Incidents, reporting.Incident{ID: fmt.Sprint("incident-", i), SiteID: c.SiteID, ContainerID: c.ContainerID, OrderID: c.OrderID, Start: start.Add(time.Hour), End: start.Add(2 * time.Hour), AsOf: p.AsOf, Category: "maintenance", Summary: "planned", RootCause: "scheduled", SourceRef: "maintenance"})
	}
	saved, err := saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, account.AccountID, 0, "annual evidence", in)
	require.NoError(t, err)
	want, err := reporting.CalculateContext(t.Context(), p, in)
	require.NoError(t, err)
	draft, err := f.svc.PrepareReport(t.Context(), f.host, f.org, account.AccountID, hostedaccounts.DraftReportOptions{Period: p, InputRevision: saved.Revision, OwnerUserID: owner})
	require.NoError(t, err)
	requireReportingResultEqual(t, want, draft.Result)
	issued, err := f.svc.PublishReport(t.Context(), f.host, f.org, account.AccountID, draft.ReportID, draft.Revision, "reviewed annual comparison")
	require.NoError(t, err)
	selected := decodedContextSource(t, f, account, issued)
	require.Len(t, selected.Meters, 2)
	require.Len(t, selected.Hashrates, 2)
	require.Len(t, selected.Incidents, 2)
	require.True(t, in.Hashrates[0].Start.Equal(selected.Hashrates[0].Start))
}

func TestReportingContextByteBudgetRejectsBeforeDraftMutation(t *testing.T) {
	f, account, owner, in, p := reportContextFixture(t)
	saved, err := saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, account.AccountID, 0, "base", in)
	require.NoError(t, err)
	revision := saved.Revision
	c := in.Coverage[0]
	for i := range 38 {
		v := reporting.PoolObservation{ID: fmt.Sprintf("large-%03d", i), Kind: c.Kind, AssetID: c.AssetID, SourceID: "pool", SourceAt: p.End.Add(-time.Minute), ReceivedAt: p.End, State: "unknown", SourceRef: "pool", Reason: strings.Repeat("r", 900000)}
		m, err := f.svc.MutateReportingInputs(t.Context(), f.host, f.org, account.AccountID, revision, "large tied evidence", nil, []reporting.FactMutation{reportingUpsertForTest(t, v)})
		require.NoError(t, err)
		revision = m.Revision
	}
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	_, err = f.svc.PrepareReport(t.Context(), f.host, f.org, account.AccountID, hostedaccounts.DraftReportOptions{Period: p, InputRevision: revision, OwnerUserID: owner})
	require.ErrorContains(t, err, "selected report input exceeds byte or heap budget")
	runtime.ReadMemStats(&after)
	require.Less(t, after.TotalAlloc-before.TotalAlloc, uint64(8<<20), "metadata rejection must not decode the oversized selected payload")
	var drafts int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_report WHERE account_id=$1`, account.ID).Scan(&drafts))
	require.Zero(t, drafts)
	var artifacts, audits int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_report_artifact`).Scan(&artifacts))
	require.Zero(t, artifacts)
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM activity_log WHERE event_type IN ('prepare_report','publish_report')`).Scan(&audits))
	require.Zero(t, audits)
	// A per-report resource failure does not impose an account-lifetime quota.
	fee := reporting.FeeLine{ID: "later-small-update", SiteID: c.SiteID, ContainerID: c.ContainerID, OrderID: c.OrderID, Start: p.Start, End: p.End, Amount: "1", Currency: "USD", SourceRef: "small reviewed update"}
	metadata, err := f.svc.MutateReportingInputs(t.Context(), f.host, f.org, account.AccountID, revision, "small update after report exhaustion", nil, []reporting.FactMutation{reportingUpsertForTest(t, fee)})
	require.NoError(t, err)
	require.Equal(t, revision+1, metadata.Revision)
	page, err := f.svc.ReportingFactPage(t.Context(), f.host, f.org, account.AccountID, reporting.FactPoolObservation, metadata.Revision, "", 1)
	require.NoError(t, err)
	require.Len(t, page.Facts, 1)
	require.NotEmpty(t, page.NextCursor)
	encoded, err := json.Marshal(page)
	require.NoError(t, err)
	require.LessOrEqual(t, len(encoded), hostedaccounts.MaxReportingFactPageBytes)
}

func TestReportingContextChecksStoredScopeBeforeRevision(t *testing.T) {
	f, account, owner, in, p := reportContextFixture(t)
	_, err := saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, account.AccountID, 0, "reviewed inputs", in)
	require.NoError(t, err)
	var otherSite int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO site(org_id,name,slug) VALUES($1,'Other scope','other-scope') RETURNING id`, f.org).Scan(&otherSite))
	require.NoError(t, f.svc.SetReportingAuthority(t.Context(), f.host, f.org, account.AccountID, hostedaccounts.ReportingAuthority{UserID: owner, CanCreate: true, SiteIDs: []int64{otherSite}}, 1))
	_, err = f.svc.PrepareReport(t.Context(), f.host, f.org, account.AccountID, hostedaccounts.DraftReportOptions{Period: p, InputRevision: 999, OwnerUserID: owner})
	require.ErrorContains(t, err, "account access denied")
}

func TestReportingContextPoolSelectionUsesExactPeriodProbe(t *testing.T) {
	f, account, owner, in, p := reportContextFixture(t)
	p.Kind = "ad_hoc"
	request, err := f.svc.CreateReportRequest(t.Context(), f.host, f.org, account.AccountID, hostedaccounts.ReportRequest{ReceivedAt: time.Now().UTC().Add(-time.Hour), SourceRef: "exact period request receipt", Requester: "Client", Scope: "Exact period package", OwnerUserID: owner})
	require.NoError(t, err)
	p.RequestRef, p.RequestReceivedAt = request.RequestID, request.ReceivedAt
	p.End = p.End.Add(900 * time.Nanosecond)
	in.Coverage[0].End = p.End.Add(-50 * time.Nanosecond)
	c := in.Coverage[0]
	in.PoolObservations = []reporting.PoolObservation{{ID: "inactive-at-probe", Kind: c.Kind, AssetID: c.AssetID, SourceID: "pool", SourceAt: p.End.Add(-time.Hour), ReceivedAt: p.End, State: "reporting", SourceRef: "earlier point"}}
	saved, err := saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, account.AccountID, 0, "exact period source", in)
	require.NoError(t, err)
	draft, err := f.svc.PrepareReport(t.Context(), f.host, f.org, account.AccountID, hostedaccounts.DraftReportOptions{Period: p, InputRevision: saved.Revision, OwnerUserID: owner})
	require.NoError(t, err)
	issued, err := f.svc.PublishReport(t.Context(), f.host, f.org, account.AccountID, draft.ReportID, draft.Revision, "reviewed exact period")
	require.NoError(t, err)
	selected := decodedContextSource(t, f, account, issued)
	require.Empty(t, selected.PoolObservations, "an asset ending before the exact last instant is not active pool context")
}

func TestReportingPreparationPreflightsCoverageBeforeTakingEvidence(t *testing.T) {
	f, account, owner, in, p := reportContextFixture(t)
	early, current := in.Coverage[0], in.Coverage[0]
	early.End = p.Start.Add(time.Hour)
	early.SourceRef = "https://evidence.example/report?token=private"
	current.ID, current.Start, current.SourceRef = "current", early.End, "reviewed current coverage"
	in.Coverage = []reporting.CoverageSegment{early, current}
	saved, err := saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, account.AccountID, 0, "split coverage", in)
	require.NoError(t, err)
	reportEvidenceHours(t, f.db, f, early.AllocationIntervalID, p.Start, 2, 1, 100e12)
	p.AsOf = reportAsOf(t, f)
	opts := hostedaccounts.DraftReportOptions{Period: p, InputRevision: saved.Revision, OwnerUserID: owner}
	_, err = f.svc.PrepareReport(t.Context(), f.host, f.org, account.AccountID, opts)
	require.ErrorContains(t, err, "credential-bearing")
	var reports, audits int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_report`).Scan(&reports))
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM activity_log WHERE event_type='prepare_report'`).Scan(&audits))
	require.Zero(t, reports)
	require.Zero(t, audits)
	early.SourceRef = "reviewed early coverage"
	in.Coverage[0] = early
	saved, err = saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, account.AccountID, saved.Revision, "review early reference", in)
	require.NoError(t, err)
	opts.InputRevision = saved.Revision
	draft, err := f.svc.PrepareReport(t.Context(), f.host, f.org, account.AccountID, opts)
	require.NoError(t, err)
	issued, err := f.svc.PublishReport(t.Context(), f.host, f.org, account.AccountID, draft.ReportID, draft.Revision, "reviewed limitations")
	require.NoError(t, err)
	selected := decodedContextSource(t, f, account, issued)
	require.Len(t, selected.Coverage, 2)
	require.Len(t, frozenReportEvidence(t, f, issued.ID), 2)
	want, err := reporting.CalculateStoredContext(t.Context(), p, selected)
	require.NoError(t, err)
	requireReportingResultEqual(t, want, draft.Result)
}
