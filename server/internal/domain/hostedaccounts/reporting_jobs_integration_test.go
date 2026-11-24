package hostedaccounts_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"slices"
	"sync"
	"testing"
	"time"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/alerts"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts/reporting"
	"github.com/block/proto-fleet/server/migrations"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type reportingTestNotifier struct {
	mu      sync.Mutex
	fail    bool
	batches [][]alerts.Alert
}

func (n *reportingTestNotifier) DeliverTrackedSkipping(_ context.Context, batch []alerts.Alert, _ []int64) alerts.DeliveryResult {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.batches = append(n.batches, batch)
	if n.fail {
		return alerts.DeliveryResult{Failed: 1}
	}
	return alerts.DeliveryResult{Delivered: 1}
}
func (n *reportingTestNotifier) count() int { n.mu.Lock(); defer n.mu.Unlock(); return len(n.batches) }
func scheduledReportingFixture(t *testing.T) (fixture, hostedaccounts.Account, string, int64, reporting.Inputs) {
	t.Helper()
	f := setup(t)
	client := f.client(t, "Scheduled reports", f.user, hostedaccounts.AllCapabilities)
	owner, reviewer := reportActors(t, f, client.AccountID)
	miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	var site int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO site(org_id,name,slug) VALUES($1,'Schedule site','schedule-site') RETURNING id`, f.org).Scan(&site))
	_, err := f.db.ExecContext(t.Context(), `UPDATE device SET site_id=$1 WHERE id=$2`, site, miner)
	require.NoError(t, err)
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(5, miner, 0)))
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	historyEpoch(t, f, start)
	_, err = f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1 WHERE client_id=$2`, start, client.ID)
	require.NoError(t, err)
	var allocation int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT id FROM hosted_account_asset_interval WHERE kind=5 AND asset_id=$1`, miner).Scan(&allocation))
	input := reportingInputs(t, f, client.AccountID, f.host)
	input.Configuration.ContractRef = "signed reporting agreement"
	input.Configuration.Schedule = &reporting.Schedule{OwnerUserID: owner, SourceRef: "reviewed calendar", MonthlyStart: start, QuarterlyStart: start, AnnualStart: start}
	input.Orders = []reporting.Order{{ID: "order", Name: "Signed order", Start: start, SourceRef: "signed order"}}
	input.Coverage = []reporting.CoverageSegment{{ID: "covered", AllocationIntervalID: allocation, Kind: 5, AssetID: miner, SiteID: site, ContainerID: miner, OrderID: "order", Start: start, CapacityTHS: "100", SourceRef: "signed commissioning"}}
	return f, client, owner, reviewer, input
}
func TestHostedReportingJobsMaterializeExplicitPeriodsOnce(t *testing.T) {
	f, client, _, reviewer, input := scheduledReportingFixture(t)
	now := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	notifier := &reportingTestNotifier{}
	jobs := hostedaccounts.NewReportingJobs(f.svc, notifier)
	require.NoError(t, jobs.Tick(t.Context(), now))
	rowsPage, err := f.svc.HostReportQueue(t.Context(), f.host, f.org, 100, "")
	require.NoError(t, err)
	rows := rowsPage.Items
	require.Empty(t, rows, "unsaved schedules never invent due obligations")
	originalOwner := input.Configuration.Schedule.OwnerUserID
	var unassigned string
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT user_id FROM "user" WHERE id=$1`, f.user).Scan(&unassigned))
	input.Configuration.Schedule.OwnerUserID = unassigned
	_, err = saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, client.AccountID, 0, "calendar source", input)
	require.Error(t, err, "ordinary membership cannot be the scheduled reporting owner")
	input.Configuration.Schedule.OwnerUserID = originalOwner
	input.Configuration.ContractRef = ""
	savedReportInputs(t, f, client.AccountID, reviewer, input, 0)
	require.NoError(t, jobs.Tick(t.Context(), now))
	rowsPage, err = f.svc.HostReportQueue(t.Context(), f.host, f.org, 100, "")
	require.NoError(t, err)
	rows = rowsPage.Items
	require.Empty(t, rows, "no report obligations are invented without a signed agreement reference")
	input.Configuration.ContractRef = "signed reporting agreement"
	savedReportInputs(t, f, client.AccountID, reviewer, input, 1)
	results := make(chan error, 2)
	for range 2 {
		go func() { results <- hostedaccounts.NewReportingJobs(f.svc, notifier).Tick(t.Context(), now) }()
	}
	require.NoError(t, <-results)
	require.NoError(t, <-results)
	require.NoError(t, hostedaccounts.NewReportingJobs(f.svc, notifier).Tick(t.Context(), now))
	rowsPage, err = f.svc.HostReportQueue(t.Context(), f.host, f.org, 100, "")
	require.NoError(t, err)
	rows = rowsPage.Items
	require.Len(t, rows, 17)
	counts := map[string]int{}
	for _, row := range rows {
		counts[row.Kind]++
		require.Contains(t, row.Reasons, "report_missing")
		require.NotContains(t, row.Reasons, "capture_missing", "current source health belongs in reporting settings")
		require.NotContains(t, row.Reasons, "dashboard_availability_instrumentation_unavailable")
		require.NotContains(t, row.Reasons, "dashboard_downtime", "missing probes are not evidence of an outage")
		require.Equal(t, reporting.Unavailable, row.Deadlines.Contract.Status)
		require.Empty(t, row.Deadlines.Contract.Date)
		require.NotContains(t, row.Reasons, "contract_overdue")
		require.EqualValues(t, 1, row.Generation)
		require.EqualValues(t, 1, row.Attempts)
	}
	require.Equal(t, map[string]int{"monthly": 12, "quarterly": 4, "annual": 1}, counts)
	require.Equal(t, 17, notifier.count(), "restarts and unchanged states do not notify twice")
	var history int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM notification_history WHERE rule_group='hosted-reporting'`).Scan(&history))
	require.Equal(t, 17, history)
	input.Configuration.Prerequisites = []reporting.Prerequisite{{Key: "G5", Owner: "Operations", Ready: true, SourceRef: "client-approved calendar and delivery agreement"}}
	savedReportInputs(t, f, client.AccountID, reviewer, input, 2)
	require.NoError(t, jobs.Tick(t.Context(), now))
	rowsPage, err = f.svc.HostReportQueue(t.Context(), f.host, f.org, 100, "")
	require.NoError(t, err)
	rows = rowsPage.Items
	approvedOverdue := false
	for _, item := range rows {
		if slices.Contains(item.Reasons, "contract_overdue") {
			require.Equal(t, reporting.Authoritative, item.Deadlines.Contract.Status)
			approvedOverdue = true
		}
	}
	require.True(t, approvedOverdue, "only an approved calendar can assert a contractual deadline")
	var raw string
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT annotations::text FROM notification_history WHERE rule_group='hosted-reporting' LIMIT 1`).Scan(&raw))
	require.Contains(t, raw, "/accounts/reports?account="+client.AccountID)
	_, err = f.db.ExecContext(t.Context(), `UPDATE hosted_report_queue_event SET reason='rewrite'`)
	require.Error(t, err, "retry and acknowledgement history is append-only")
	down, err := migrations.Current.ReadFile("current/001086_internal_hosted_report_queue.down.sql")
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), string(down))
	require.ErrorContains(t, err, "retained report queue", "rollback cannot discard operational history")
}
func TestHostedReportingQueueFailureAcknowledgementAndDeliveryTransitions(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Queue client", f.user, hostedaccounts.AllCapabilities)
	owner, reviewer := reportActors(t, f, client.AccountID)
	savedReportInputs(t, f, client.AccountID, reviewer, reportingInputs(t, f, client.AccountID, f.host), 0)
	report, err := f.svc.PrepareReport(t.Context(), f.host, f.org, client.AccountID, hostedaccounts.DraftReportOptions{Period: reportPeriod(), InputRevision: 1, OwnerUserID: owner})
	require.NoError(t, err)
	notifier := &reportingTestNotifier{fail: true}
	jobs := hostedaccounts.NewReportingJobs(f.svc, notifier)
	now := time.Now().UTC().Round(time.Microsecond)
	require.NoError(t, jobs.Tick(t.Context(), now))
	require.NoError(t, jobs.Tick(t.Context(), now.Add(time.Minute)))
	rowsPage, err := f.svc.HostReportQueue(t.Context(), f.host, f.org, 100, "")
	require.NoError(t, err)
	rows := rowsPage.Items
	require.Len(t, rows, 1)
	row := rows[0]
	require.Equal(t, "failed", row.NotificationStatus)
	require.Equal(t, 1, notifier.count())
	require.Contains(t, row.Reasons, "draft")
	require.EqualValues(t, 1, row.Attempts)
	require.NoError(t, f.svc.AcknowledgeReportQueue(t.Context(), f.host, f.org, client.AccountID, row.QueueID, row.Generation, "owner investigating"))
	require.NoError(t, jobs.Tick(t.Context(), now.Add(10*time.Minute)))
	require.Equal(t, 1, notifier.count(), "acknowledgement suppresses unchanged state")
	require.Error(t, f.svc.RetryReportQueueNotification(t.Context(), f.host, f.org, client.AccountID, row.QueueID, row.Generation+1, "stale"))
	require.NoError(t, f.svc.RetryReportQueueNotification(t.Context(), f.host, f.org, client.AccountID, row.QueueID, row.Generation, "destination repaired"))
	notifier.fail = false
	report, err = f.svc.PrepareReport(t.Context(), f.host, f.org, client.AccountID, hostedaccounts.DraftReportOptions{Period: reportPeriod(), InputRevision: 1, OwnerUserID: owner, ReportID: report.ReportID, ExpectedRevision: report.Revision})
	require.NoError(t, err)
	require.NoError(t, jobs.Tick(t.Context(), now.Add(11*time.Minute)))
	require.NoError(t, jobs.Tick(t.Context(), now.Add(12*time.Minute)))
	rowsPage, err = f.svc.HostReportQueue(t.Context(), f.host, f.org, 100, "")
	require.NoError(t, err)
	rows = rowsPage.Items
	require.Contains(t, rows[0].Reasons, "draft")
	require.Greater(t, rows[0].Generation, row.Generation)
	require.Error(t, f.svc.AcknowledgeReportQueue(t.Context(), f.host, f.org, client.AccountID, row.QueueID, row.Generation, "old snapshot"))
	issueStoredReport(t, f, report, reviewer)
	require.NoError(t, jobs.Tick(t.Context(), now.Add(13*time.Minute)))
	rowsPage, err = f.svc.HostReportQueue(t.Context(), f.host, f.org, 100, "")
	require.NoError(t, err)
	rows = rowsPage.Items
	require.Equal(t, "unconfirmed", rows[0].DeliveryStatus)
	require.Contains(t, rows[0].Reasons, "delivery_unconfirmed")
	require.False(t, rows[0].Complete)
	delivery := hostedaccounts.DeliveryEvidence{RequestKey: uuid.NewString(), Channel: "agreed channel", Status: "failed", SourceRef: "delivery attempt", FailureCategory: "unavailable"}
	require.NoError(t, f.svc.RecordReportDelivery(t.Context(), f.host, f.org, client.AccountID, report.ReportID, delivery))
	require.NoError(t, jobs.Tick(t.Context(), now.Add(14*time.Minute)))
	rowsPage, err = f.svc.HostReportQueue(t.Context(), f.host, f.org, 100, "")
	require.NoError(t, err)
	rows = rowsPage.Items
	require.Equal(t, "failed", rows[0].DeliveryStatus)
	require.Contains(t, rows[0].Reasons, "delivery_failed")
	kinds := []string{}
	for _, event := range rows[0].Events {
		kinds = append(kinds, event.Kind)
	}
	require.Contains(t, kinds, "acknowledged")
	require.Contains(t, kinds, "retry")
	require.Contains(t, kinds, "failed")
	require.Contains(t, kinds, "sent")
	_, err = f.svc.HostReportQueue(t.Context(), f.user, f.org, 100, "")
	require.ErrorContains(t, err, "account access denied", "client-only members cannot discover host queue metadata")
	scoped, err := hostedaccounts.WithRequestAccountID(t.Context(), client.AccountID)
	require.NoError(t, err)
	_, err = f.svc.HostReportQueue(scoped, f.host, f.org, 100, "")
	require.Error(t, err)
}
func TestHostedReportingQueueLeaseFencesConcurrentAndStaleClaims(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Lease client", f.user, hostedaccounts.AllCapabilities)
	owner, reviewer := reportActors(t, f, client.AccountID)
	savedReportInputs(t, f, client.AccountID, reviewer, reportingInputs(t, f, client.AccountID, f.host), 0)
	_, err := f.svc.PrepareReport(t.Context(), f.host, f.org, client.AccountID, hostedaccounts.DraftReportOptions{Period: reportPeriod(), InputRevision: 1, OwnerUserID: owner})
	require.NoError(t, err)
	now := time.Now().UTC().Round(time.Microsecond)
	require.NoError(t, hostedaccounts.NewReportingJobs(f.svc, nil).Tick(t.Context(), now))
	q := sqlc.New(f.db)
	first, err := q.ClaimHostedReportQueueNotification(t.Context(), sqlc.ClaimHostedReportQueueNotificationParams{NowAt: now, ClaimToken: sql.NullString{String: "first", Valid: true}, LeaseUntil: sql.NullTime{Time: now.Add(time.Minute), Valid: true}})
	require.NoError(t, err)
	_, err = q.ClaimHostedReportQueueNotification(t.Context(), sqlc.ClaimHostedReportQueueNotificationParams{NowAt: now, ClaimToken: sql.NullString{String: "other", Valid: true}, LeaseUntil: sql.NullTime{Time: now.Add(time.Minute), Valid: true}})
	require.ErrorIs(t, err, sql.ErrNoRows)
	second, err := q.ClaimHostedReportQueueNotification(t.Context(), sqlc.ClaimHostedReportQueueNotificationParams{NowAt: now.Add(2 * time.Minute), ClaimToken: sql.NullString{String: "second", Valid: true}, LeaseUntil: sql.NullTime{Time: now.Add(3 * time.Minute), Valid: true}})
	require.NoError(t, err)
	changed, err := q.CompleteHostedReportQueueNotification(t.Context(), sqlc.CompleteHostedReportQueueNotificationParams{ID: first.ID, Generation: first.Generation, ClaimToken: first.ClaimToken, NotificationStatus: "sent", RetryAt: now, NowAt: now.Add(2 * time.Minute)})
	require.NoError(t, err)
	require.Zero(t, changed)
	require.Equal(t, first.ID, second.ID)
	var state hostedaccounts.ReportQueueState
	require.NoError(t, json.Unmarshal(second.Projection, &state))
	require.NotEmpty(t, state.Reasons)
}

func TestHostedReportingQueueAdHocScopeAndRetrySurviveRestart(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Request queue", f.user, hostedaccounts.AllCapabilities)
	owner, _ := reportActors(t, f, client.AccountID)
	now := time.Now().UTC().Round(time.Microsecond)
	request, err := f.svc.CreateReportRequest(t.Context(), f.host, f.org, client.AccountID, hostedaccounts.ReportRequest{ReceivedAt: now.AddDate(0, 0, -31), SourceRef: "client request", Requester: "Authorized contact", Scope: "Written operational explanation", OwnerUserID: owner, Limitations: "Reviewed evidence only"})
	require.NoError(t, err)
	require.NoError(t, hostedaccounts.NewReportingJobs(f.svc, nil).Tick(t.Context(), now))
	rowsPage, err := f.svc.HostReportQueue(t.Context(), f.host, f.org, 100, "")
	require.NoError(t, err)
	rows := rowsPage.Items
	require.Len(t, rows, 1)
	require.Equal(t, request.RequestID, rows[0].Request.RequestID)
	require.True(t, rows[0].Deadlines.Internal.ReasonableEfforts)
	require.Equal(t, reporting.Unavailable, rows[0].Deadlines.Contract.Status)
	require.Contains(t, rows[0].Reasons, "request_target_overdue")
	require.NoError(t, f.svc.SetReportingAuthority(t.Context(), f.host, f.org, client.AccountID, hostedaccounts.ReportingAuthority{UserID: owner, CanCreate: true}, 1))
	rowsPage, err = f.svc.HostReportQueue(t.Context(), f.host, f.org, 100, "")
	require.NoError(t, err)
	rows = rowsPage.Items
	require.Empty(t, rows, "unscoped ad hoc request requires full account site authority")
	notifier := &reportingTestNotifier{}
	require.NoError(t, hostedaccounts.NewReportingJobs(f.svc, notifier).Tick(t.Context(), now.Add(time.Minute)))
	require.Zero(t, notifier.count(), "a revoked owner scope must not trigger notification sending")
}

func TestHostedReportingJobsIsolateInvalidCalendarAndDrainOutbox(t *testing.T) {
	f, broken, _, reviewer, input := scheduledReportingFixture(t)
	input.Configuration.Schedule.QuarterlyStart = time.Time{}
	input.Configuration.Schedule.AnnualStart = time.Time{}
	savedReportInputs(t, f, broken.AccountID, reviewer, input, 0)
	healthy := f.client(t, "Healthy reporting account", f.user, hostedaccounts.AllCapabilities)
	owner, _ := reportActors(t, f, healthy.AccountID)
	now := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	createRequest := func() {
		_, err := f.svc.CreateReportRequest(t.Context(), f.host, f.org, healthy.AccountID, hostedaccounts.ReportRequest{ReceivedAt: now.Add(-time.Hour), SourceRef: "client request", Requester: "Client", Scope: "Written explanation", OwnerUserID: owner})
		require.NoError(t, err)
	}
	createRequest()
	require.NoError(t, hostedaccounts.NewReportingJobs(f.svc, nil).Tick(t.Context(), now))
	// Retain a previously accepted calendar to prove one legacy input cannot
	// prevent other accounts or already-durable outbox work from progressing.
	_, err := f.db.ExecContext(t.Context(), `UPDATE hosted_reporting_inputs SET revision=revision+1,source_ref='legacy leap-day calendar',
	 configuration=jsonb_set(jsonb_set(configuration,'{reportingYearMonth}','2'),'{reportingYearDay}','29')
	 WHERE account_id=$1`, broken.ID)
	require.NoError(t, err)
	createRequest()
	notifier := &reportingTestNotifier{}
	err = hostedaccounts.NewReportingJobs(f.svc, notifier).Tick(t.Context(), now.Add(time.Minute))
	require.ErrorContains(t, err, broken.AccountID)
	require.ErrorContains(t, err, "February 29")
	require.Equal(t, 3, notifier.count(), "old outbox entries and new healthy work still send")
	var pending int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_report_queue WHERE notification_status<>'sent'`).Scan(&pending))
	require.Zero(t, pending)
}

func TestHostedReportingQueueRetiresRegeneratedPeriodAndFencesOldClaim(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Regenerated report", f.user, hostedaccounts.AllCapabilities)
	owner, reviewer := reportActors(t, f, client.AccountID)
	savedReportInputs(t, f, client.AccountID, reviewer, reportingInputs(t, f, client.AccountID, f.host), 0)
	p := reportPeriod()
	p.Start = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	p.End = p.Start.AddDate(0, 1, 0)
	draft, err := f.svc.PrepareReport(t.Context(), f.host, f.org, client.AccountID, hostedaccounts.DraftReportOptions{Period: p, InputRevision: 1, OwnerUserID: owner})
	require.NoError(t, err)
	now := time.Now().UTC().Round(time.Microsecond)
	jobs := hostedaccounts.NewReportingJobs(f.svc, nil)
	require.NoError(t, jobs.Tick(t.Context(), now))
	rowsPage, err := f.svc.HostReportQueue(t.Context(), f.host, f.org, 100, "")
	require.NoError(t, err)
	rows := rowsPage.Items
	require.Len(t, rows, 1)
	old := rows[0]
	q := sqlc.New(f.db)
	claim, err := q.ClaimHostedReportQueueNotification(t.Context(), sqlc.ClaimHostedReportQueueNotificationParams{NowAt: now, ClaimToken: sql.NullString{String: "old-period", Valid: true}, LeaseUntil: sql.NullTime{Time: now.Add(time.Minute), Valid: true}})
	require.NoError(t, err)
	p.Start, p.End = p.End, p.End.AddDate(0, 1, 0)
	_, err = f.svc.PrepareReport(t.Context(), f.host, f.org, client.AccountID, hostedaccounts.DraftReportOptions{ReportID: draft.ReportID, ExpectedRevision: draft.Revision, Period: p, InputRevision: 1, OwnerUserID: owner})
	require.NoError(t, err)
	require.NoError(t, jobs.Tick(t.Context(), now.Add(time.Second)))
	rowsPage, err = f.svc.HostReportQueue(t.Context(), f.host, f.org, 100, "")
	require.NoError(t, err)
	rows = rowsPage.Items
	require.Len(t, rows, 1, "only the current period remains active")
	require.NotEqual(t, old.QueueID, rows[0].QueueID)
	retired, err := q.GetHostedReportQueue(t.Context(), sqlc.GetHostedReportQueueParams{OrganizationID: f.org, AccountID: client.ID, QueueID: old.QueueID})
	require.NoError(t, err)
	var retiredState hostedaccounts.ReportQueueState
	require.NoError(t, json.Unmarshal(retired.Projection, &retiredState))
	require.Equal(t, "quiet", retired.NotificationStatus)
	require.Equal(t, []string{"superseded"}, retiredState.Reasons)
	require.Equal(t, old.Generation+1, retired.Generation)
	retiredEvents, err := q.ListHostedReportQueueEvents(t.Context(), retired.ID)
	require.NoError(t, err)
	require.Len(t, retiredEvents, 2)
	require.Equal(t, "reporting queue work superseded", retiredEvents[1].Reason)
	var token sql.NullString
	var lease sql.NullTime
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT claim_token,lease_until FROM hosted_report_queue WHERE queue_id=$1`, old.QueueID).Scan(&token, &lease))
	require.False(t, token.Valid)
	require.False(t, lease.Valid)
	changed, err := q.CompleteHostedReportQueueNotification(t.Context(), sqlc.CompleteHostedReportQueueNotificationParams{ID: claim.ID, Generation: claim.Generation, ClaimToken: claim.ClaimToken, NotificationStatus: "sent", RetryAt: now, NowAt: now.Add(2 * time.Second)})
	require.NoError(t, err)
	require.Zero(t, changed)
	for _, generation := range []int64{old.Generation, retired.Generation} {
		require.Error(t, f.svc.RetryReportQueueNotification(t.Context(), f.host, f.org, client.AccountID, old.QueueID, generation, "retry old period"))
		require.Error(t, f.svc.AcknowledgeReportQueue(t.Context(), f.host, f.org, client.AccountID, old.QueueID, generation, "ack old period"))
	}
	require.NoError(t, jobs.Tick(t.Context(), now.Add(3*time.Second)))
	var generation int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT generation FROM hosted_report_queue WHERE queue_id=$1`, old.QueueID).Scan(&generation))
	require.Equal(t, retired.Generation, generation, "retirement is idempotent")
	notifier := &reportingTestNotifier{}
	require.NoError(t, hostedaccounts.NewReportingJobs(f.svc, notifier).Tick(t.Context(), now.Add(4*time.Second)))
	require.Equal(t, 1, notifier.count(), "only the regenerated period remains actionable")
}

func TestHostedReportingQueueRetiresVersionsReplacedByIssuedCorrection(t *testing.T) {
	f, client, reviewer, draft := draftReportFixture(t)
	original, err := f.svc.PublishReport(t.Context(), reviewer, f.org, client.AccountID, draft.ReportID, draft.Revision, "reviewed incomplete source")
	require.NoError(t, err)
	failed := hostedaccounts.DeliveryEvidence{RequestKey: uuid.NewString(), Channel: "email", Status: "failed", SentAt: original.IssuedAt, SourceRef: "delivery attempt", Evidence: "bounce", FailureCategory: "unavailable"}
	require.NoError(t, f.svc.RecordReportDelivery(t.Context(), f.host, f.org, client.AccountID, original.ReportID, failed))
	jobs := hostedaccounts.NewReportingJobs(f.svc, nil)
	now := time.Now().UTC().Round(time.Microsecond)
	active := func() map[string][]string {
		page, err := f.svc.HostReportQueue(t.Context(), f.host, f.org, 100, "")
		require.NoError(t, err)
		out := map[string][]string{}
		for _, item := range page.Items {
			out[item.ReportID] = item.Reasons
		}
		return out
	}
	require.NoError(t, jobs.Tick(t.Context(), now))
	require.Contains(t, active()[original.ReportID], "delivery_failed")

	var owner string
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT user_id FROM "user" WHERE id=$1`, f.host).Scan(&owner))
	correction, err := f.svc.PrepareReport(t.Context(), f.host, f.org, client.AccountID, hostedaccounts.DraftReportOptions{Period: reportPeriod(), InputRevision: 1, OwnerUserID: owner, CorrectionOf: original.ReportID, CorrectionReason: "late evidence"})
	require.NoError(t, err)
	require.NoError(t, jobs.Tick(t.Context(), now.Add(time.Minute)))
	items := active()
	require.Contains(t, items[original.ReportID], "delivery_failed", "an unissued correction leaves the version it corrects active")
	require.Contains(t, items[correction.ReportID], "draft")

	correction, err = f.svc.PublishReport(t.Context(), reviewer, f.org, client.AccountID, correction.ReportID, correction.Revision, "reviewed incomplete source")
	require.NoError(t, err)
	require.NoError(t, jobs.Tick(t.Context(), now.Add(2*time.Minute)))
	items = active()
	require.NotContains(t, items, original.ReportID, "a replaced version carries no further delivery work")
	require.Contains(t, items[correction.ReportID], "delivery_unconfirmed")
	var reasons string
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT projection->>'reasons' FROM hosted_report_queue WHERE account_id=$1 AND projection->>'reportId'=$2`, client.ID, original.ReportID).Scan(&reasons))
	require.JSONEq(t, `["superseded"]`, reasons, "the replaced version keeps its queue history")

	confirmed := hostedaccounts.DeliveryEvidence{RequestKey: uuid.NewString(), Channel: "email", Status: "confirmed", SentAt: correction.IssuedAt, ReceivedAt: correction.IssuedAt, SourceRef: "exact publication receipt", Evidence: "delivery receipt"}
	require.NoError(t, f.svc.RecordReportDelivery(t.Context(), f.host, f.org, client.AccountID, correction.ReportID, confirmed))
	require.NoError(t, jobs.Tick(t.Context(), now.Add(3*time.Minute)))
	require.Empty(t, active(), "delivering the replacement leaves nothing actionable for the series")
}

func TestHostedReportingRequestQueueUsesCurrentAccountAndSiteAuthority(t *testing.T) {
	for _, narrowing := range []string{"site_role", "account_policy"} {
		t.Run(narrowing, func(t *testing.T) {
			f := setup(t)
			client := f.client(t, "Private queue request", f.user, hostedaccounts.AllCapabilities)
			owner, reviewer := reportActors(t, f, client.AccountID)
			now := time.Now().UTC().Round(time.Microsecond)
			_, err := f.svc.CreateReportRequest(t.Context(), f.host, f.org, client.AccountID, hostedaccounts.ReportRequest{ReceivedAt: now.Add(-time.Hour), SourceRef: "request receipt", Requester: "Client", Scope: "Private account-wide narrative", OwnerUserID: owner})
			require.NoError(t, err)
			require.NoError(t, hostedaccounts.NewReportingJobs(f.svc, nil).Tick(t.Context(), now))
			rowsPage, err := f.svc.HostReportQueue(t.Context(), f.host, f.org, 100, "")
			require.NoError(t, err)
			rows := rowsPage.Items
			require.Len(t, rows, 1)
			requestQueue := rows[0]
			// A distinct owner's report remains deliverable while this request's
			// owner loses account-wide authority.
			healthy := f.client(t, "Healthy notification", f.user, hostedaccounts.AllCapabilities)
			_, otherReviewer := reportActors(t, f, healthy.AccountID)
			savedReportInputs(t, f, healthy.AccountID, otherReviewer, reportingInputs(t, f, healthy.AccountID, f.host), 0)
			var healthyOwner string
			require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT user_id FROM "user" WHERE id=$1`, otherReviewer).Scan(&healthyOwner))
			_, err = f.svc.PrepareReport(t.Context(), f.host, f.org, healthy.AccountID, hostedaccounts.DraftReportOptions{Period: reportPeriod(), InputRevision: 1, OwnerUserID: healthyOwner})
			require.NoError(t, err)
			var site int64
			require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO site(name,org_id,slug) VALUES('Scoped queue site',$1,'scoped-queue-site') RETURNING id`, f.org).Scan(&site))
			switch narrowing {
			case "site_role":
				var role int64
				require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT id FROM role WHERE organization_id=$1 AND builtin_key='FIELD_TECH'`, f.org).Scan(&role))
				_, err = sqlc.New(f.db).AssignRole(t.Context(), sqlc.AssignRoleParams{UserID: f.host, OrganizationID: f.org, RoleID: role, ScopeType: "site", ScopeID: sql.NullInt64{Int64: site, Valid: true}})
				require.NoError(t, err)
			case "account_policy":
				rootCtx, err := hostedaccounts.WithRequestAccountID(t.Context(), f.root)
				require.NoError(t, err)
				require.NoError(t, f.svc.ConfigureSites(rootCtx, f.host, f.org, client.AccountID, true, []int64{site}))
			}
			rowsPage, err = f.svc.HostReportQueue(t.Context(), f.host, f.org, 100, "")
			require.NoError(t, err)
			rows = rowsPage.Items
			for _, row := range rows {
				require.NotEqual(t, requestQueue.QueueID, row.QueueID, "request narratives follow current effective authority")
			}
			for _, generation := range []int64{requestQueue.Generation, requestQueue.Generation + 1} {
				require.ErrorContains(t, f.svc.AcknowledgeReportQueue(t.Context(), f.host, f.org, client.AccountID, requestQueue.QueueID, generation, "acknowledge"), "account access denied")
				require.ErrorContains(t, f.svc.RetryReportQueueNotification(t.Context(), f.host, f.org, client.AccountID, requestQueue.QueueID, generation, "retry"), "account access denied")
			}
			notifier := &reportingTestNotifier{}
			require.NoError(t, hostedaccounts.NewReportingJobs(f.svc, notifier).Tick(t.Context(), now.Add(time.Minute)))
			require.Equal(t, 1, notifier.count(), "restricted request owner cannot block a healthy owner's report")
			require.Contains(t, notifier.batches[0][0].Annotations["action_path"], healthy.AccountID)
			rowsPage, err = f.svc.HostReportQueue(t.Context(), reviewer, f.org, 100, "")
			require.NoError(t, err)
			rows = rowsPage.Items
			if narrowing == "site_role" {
				require.NotEmpty(t, rows, "another full-account reviewer can still inspect the request")
			}
		})
	}
}

func TestHostedReportingJobsStopPromptlyWhenCanceledDuringMaterialization(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Canceled reporting job", f.user, hostedaccounts.AllCapabilities)
	owner, _ := reportActors(t, f, client.AccountID)
	now := time.Now().UTC().Round(time.Microsecond)
	_, err := f.svc.CreateReportRequest(t.Context(), f.host, f.org, client.AccountID, hostedaccounts.ReportRequest{ReceivedAt: now.Add(-time.Hour), SourceRef: "client request", Requester: "Client", Scope: "Written explanation", OwnerUserID: owner})
	require.NoError(t, err)
	barrier, err := f.db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	defer barrier.Rollback()
	_, err = barrier.ExecContext(t.Context(), `SELECT id FROM organization WHERE id=$1 FOR NO KEY UPDATE`, f.org)
	require.NoError(t, err)
	var blocker int
	require.NoError(t, barrier.QueryRowContext(t.Context(), `SELECT pg_backend_pid()`).Scan(&blocker))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	notifier := &reportingTestNotifier{}
	result := make(chan error, 1)
	go func() { result <- hostedaccounts.NewReportingJobs(f.svc, notifier).Tick(ctx, now) }()
	require.Eventually(t, func() bool {
		var blocked bool
		err := f.db.QueryRowContext(t.Context(), `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND $1=ANY(pg_blocking_pids(pid)) AND query LIKE '-- name: LockHostedAccountOrganizationForShare :one%')`, blocker).Scan(&blocked)
		return err == nil && blocked
	}, 2*time.Second, 10*time.Millisecond)
	cancel()
	select {
	case err := <-result:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("reporting cancellation waited for the materialization timeout")
	}
	require.Zero(t, notifier.count())
}

func TestHostedReportingJobsShareOrganizationAdmissionAndSerializePerAccount(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Shared reporting admission", f.user, hostedaccounts.AllCapabilities)
	owner, _ := reportActors(t, f, client.AccountID)
	now := time.Now().UTC().Round(time.Microsecond)
	_, err := f.svc.CreateReportRequest(t.Context(), f.host, f.org, client.AccountID, hostedaccounts.ReportRequest{ReceivedAt: now.Add(-time.Hour), SourceRef: "client request", Requester: "Client", Scope: "Written explanation", OwnerUserID: owner})
	require.NoError(t, err)
	queued := func() (count int) {
		require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_report_queue WHERE account_id=$1`, client.ID).Scan(&count))
		return count
	}

	// A workspace read holds shared organization admission; the worker must not wait for it.
	reader, err := f.db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	defer reader.Rollback()
	_, err = reader.ExecContext(t.Context(), `SELECT id FROM organization WHERE id=$1 FOR SHARE`, f.org)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	require.NoError(t, hostedaccounts.NewReportingJobs(f.svc, nil).Tick(ctx, now))
	require.Equal(t, 1, queued())

	// Another worker on the same account holds the account lock; this one waits
	// for it while the organization's readers stay admitted.
	_, err = f.svc.CreateReportRequest(t.Context(), f.host, f.org, client.AccountID, hostedaccounts.ReportRequest{ReceivedAt: now.Add(-time.Minute), SourceRef: "second client request", Requester: "Client", Scope: "Follow-up explanation", OwnerUserID: owner})
	require.NoError(t, err)
	_, err = reader.ExecContext(t.Context(), `SELECT pg_advisory_xact_lock(hashtextextended('hosted-report-queue:' || $1::bigint::text, 0))`, client.ID)
	require.NoError(t, err)
	var blocker int
	require.NoError(t, reader.QueryRowContext(t.Context(), `SELECT pg_backend_pid()`).Scan(&blocker))
	result := make(chan error, 1)
	go func() { result <- hostedaccounts.NewReportingJobs(f.svc, nil).Tick(ctx, now) }()
	require.Eventually(t, func() bool {
		var blocked bool
		err := f.db.QueryRowContext(t.Context(), `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND $1=ANY(pg_blocking_pids(pid)) AND query LIKE '-- name: LockHostedReportQueueAccount :exec%')`, blocker).Scan(&blocked)
		return err == nil && blocked
	}, 2*time.Second, 10*time.Millisecond)
	other, err := f.db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	defer other.Rollback()
	_, err = other.ExecContext(ctx, `SELECT id FROM organization WHERE id=$1 FOR SHARE`, f.org)
	require.NoError(t, err, "a waiting worker must not hold back organization readers")
	require.NoError(t, other.Rollback())
	require.Equal(t, 1, queued())
	require.NoError(t, reader.Rollback())
	require.NoError(t, <-result)
	require.Equal(t, 2, queued())
}

func TestHostedReportingJobsScanMultipleMetadataPages(t *testing.T) {
	f, client, _, draft := draftReportFixture(t)
	_, err := f.db.ExecContext(t.Context(), `INSERT INTO hosted_report(report_id,organization_id,account_id,series_id,number,kind,period_start,period_end,as_of,timezone,period,input_revision,result,site_ids,created_by,owner_id)
 SELECT gen_random_uuid()::text,organization_id,account_id,gen_random_uuid()::text,1,kind,
 period_start+make_interval(months=>n),period_end+make_interval(months=>n),as_of+make_interval(months=>n),timezone,
 jsonb_set(jsonb_set(period,'{start}',to_jsonb(period_start+make_interval(months=>n))),'{end}',to_jsonb(period_end+make_interval(months=>n))),input_revision,
 jsonb_set(result,'{metrics,large}',jsonb_build_object('sources',jsonb_build_array(repeat('x',65536)))),site_ids,created_by,owner_id
 FROM hosted_report CROSS JOIN generate_series(1,101) n WHERE id=$1`, draft.ID)
	require.NoError(t, err)
	require.NoError(t, hostedaccounts.NewReportingJobs(f.svc, nil).Tick(t.Context(), time.Now().UTC()))
	var count int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_report_queue WHERE account_id=$1`, client.ID).Scan(&count))
	require.Equal(t, 102, count, "the worker must scan beyond its first 100 bounded summaries")
}

func TestReportingScheduleOwnerChecksCompleteFinalSiteSet(t *testing.T) {
	f, account, _, reviewer, input := scheduledReportingFixture(t)
	var owner string
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT user_id FROM "user" WHERE id=$1`, reviewer).Scan(&owner))
	input.Configuration.Schedule.OwnerUserID = owner
	saved, err := saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, account.AccountID, 0, "schedule sources", input)
	require.NoError(t, err)
	c := input.Coverage[0]
	var otherSite int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO site(org_id,name,slug) VALUES($1,'Other schedule site','other-schedule') RETURNING id`, f.org).Scan(&otherSite))
	otherContainer := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	_, err = f.db.ExecContext(t.Context(), `UPDATE device SET site_id=$1 WHERE id=$2`, otherSite, otherContainer)
	require.NoError(t, err)
	grant := hostedaccounts.ReportingAuthority{UserID: owner, CanCreate: true, SiteIDs: []int64{c.SiteID}}
	require.NoError(t, f.svc.SetReportingAuthority(t.Context(), f.host, f.org, account.AccountID, grant, 1))
	fee := reporting.FeeLine{ID: "other-site-fee", SiteID: otherSite, ContainerID: otherContainer, OrderID: c.OrderID, Start: c.Start, End: c.Start.Add(time.Hour), Amount: "1", Currency: "USD", SourceRef: "reviewed other site fee"}
	var auditBefore int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM activity_log WHERE event_type='save_reporting_inputs'`).Scan(&auditBefore))
	_, err = f.svc.MutateReportingInputs(t.Context(), f.host, f.org, account.AccountID, saved.Revision, "new site", nil, []reporting.FactMutation{reportingUpsertForTest(t, fee)})
	require.ErrorContains(t, err, "account access denied", "an unchanged schedule must cover sites introduced by a bounded fact batch")
	current, err := f.svc.ReportingInputsMetadata(t.Context(), f.host, f.org, account.AccountID)
	require.NoError(t, err)
	require.Equal(t, saved.Revision, current.Revision)
	require.Equal(t, []int64{c.SiteID}, current.SiteIDs)
	require.Zero(t, current.RegisterCounts[reporting.FactFee])
	var auditAfter int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM activity_log WHERE event_type='save_reporting_inputs'`).Scan(&auditAfter))
	require.Equal(t, auditBefore, auditAfter, "schedule failure rolls back facts, revision, scope and audit")
	grant.AllSites, grant.SiteIDs = true, nil
	require.NoError(t, f.svc.SetReportingAuthority(t.Context(), f.host, f.org, account.AccountID, grant, 2))
	current, err = f.svc.MutateReportingInputs(t.Context(), f.host, f.org, account.AccountID, saved.Revision, "authorized new site", nil, []reporting.FactMutation{reportingUpsertForTest(t, fee)})
	require.NoError(t, err)
	grant.AllSites, grant.SiteIDs = false, []int64{c.SiteID}
	require.NoError(t, f.svc.SetReportingAuthority(t.Context(), f.host, f.org, account.AccountID, grant, 3))
	input.Configuration.ContractRef = "updated signed reporting agreement"
	_, err = f.svc.MutateReportingInputs(t.Context(), f.host, f.org, account.AccountID, current.Revision, "configuration edit", &input.Configuration, nil)
	require.ErrorContains(t, err, "account access denied", "configuration-only edits check retained sites outside this request")
	current, err = f.svc.MutateReportingInputs(t.Context(), f.host, f.org, account.AccountID, current.Revision, "remove unsupported schedule site", nil, []reporting.FactMutation{{Kind: reporting.FactFee, ID: fee.ID, Delete: true}})
	require.NoError(t, err, "owner authority applies to the final site set after deletions")
	require.Equal(t, []int64{c.SiteID}, current.SiteIDs)
	grant.CanCreate = false
	require.NoError(t, f.svc.SetReportingAuthority(t.Context(), f.host, f.org, account.AccountID, grant, 4))
	_, err = f.svc.MutateReportingInputs(t.Context(), f.host, f.org, account.AccountID, current.Revision, "revoked schedule owner", &input.Configuration, nil)
	require.ErrorContains(t, err, "account access denied", "schedule owner create authority is checked fresh on every mutation")
}

func TestHostedReportingJobDiscoveryTimesOutAndRecovers(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Discovery recovery", f.user, hostedaccounts.AllCapabilities)
	owner, _ := reportActors(t, f, client.AccountID)
	now := time.Now().UTC().Round(time.Microsecond)
	_, err := f.svc.CreateReportRequest(t.Context(), f.host, f.org, client.AccountID, hostedaccounts.ReportRequest{ReceivedAt: now.Add(-time.Hour), SourceRef: "Client receipt", Requester: "Client", Scope: "Recovery report", OwnerUserID: owner})
	require.NoError(t, err)
	gate, err := f.db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	defer func() { _ = gate.Rollback() }()
	_, err = gate.ExecContext(t.Context(), `LOCK TABLE hosted_account IN ACCESS EXCLUSIVE MODE`)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	jobs := hostedaccounts.NewReportingJobs(f.svc, nil)
	result := make(chan error, 1)
	go func() { result <- jobs.Tick(ctx, now) }()
	require.Eventually(t, func() bool {
		var blocked bool
		err := f.db.QueryRowContext(t.Context(), `SELECT EXISTS(SELECT 1 FROM pg_locks l JOIN pg_stat_activity a USING(pid) WHERE l.database=(SELECT oid FROM pg_database WHERE datname=current_database()) AND l.relation='hosted_account'::regclass AND NOT l.granted AND a.query LIKE '%ListReportingJobAccounts%')`).Scan(&blocked)
		return err == nil && blocked
	}, time.Second, 10*time.Millisecond)
	select {
	case err := <-result:
		require.ErrorIs(t, err, context.DeadlineExceeded)
	case <-time.After(6 * time.Second):
		t.Fatal("account discovery remained blocked without its own deadline")
	}
	require.NoError(t, ctx.Err(), "discovery timeout must not cancel the worker context")
	require.NoError(t, gate.Commit())
	require.NoError(t, jobs.Tick(ctx, now.Add(time.Minute)))
	queue, err := f.svc.HostReportQueue(t.Context(), f.host, f.org, 100, "")
	require.NoError(t, err)
	require.Len(t, queue.Items, 1, "the next tick resumes report materialization after discovery recovers")
}
