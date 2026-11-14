package hostedaccounts_test

import (
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts/reporting"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func reportActors(t *testing.T, f fixture, account string) (string, int64) {
	t.Helper()
	var owner string
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT user_id FROM "user" WHERE id=$1`, f.host).Scan(&owner))
	require.NoError(t, f.svc.SetReportingAuthority(t.Context(), f.host, f.org, account, hostedaccounts.ReportingAuthority{UserID: owner, CanCreate: true, AllSites: true}, 0))
	reviewer := addUser(t, f.db, f.org)
	var role int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT id FROM role WHERE organization_id=$1 AND builtin_key='SUPER_ADMIN'`, f.org).Scan(&role))
	_, err := sqlc.New(f.db).AssignRole(t.Context(), sqlc.AssignRoleParams{UserID: reviewer, OrganizationID: f.org, RoleID: role, ScopeType: "org"})
	require.NoError(t, err)
	var reviewerUUID string
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT user_id FROM "user" WHERE id=$1`, reviewer).Scan(&reviewerUUID))
	require.NoError(t, f.svc.SetReportingAuthority(t.Context(), f.host, f.org, account, hostedaccounts.ReportingAuthority{UserID: reviewerUUID, CanCreate: true, AllSites: true}, 0))
	return owner, reviewer
}
func reportPeriod() reporting.Period {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	return reporting.Period{Kind: "monthly", Start: start, End: start.AddDate(0, 1, 0), AsOf: time.Now().UTC(), Timezone: "UTC"}
}
func savedReportInputs(t *testing.T, f fixture, account string, actor int64, input reporting.Inputs, previous int64) {
	t.Helper()
	_, err := saveReportingInputsForTest(f.svc, t.Context(), actor, f.org, account, previous, "source worksheet", input)
	require.NoError(t, err)
}

func TestHostedReportDraftUsesLatestSavedInputsAndPublishesWithoutReview(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Reports client", f.user, hostedaccounts.AllCapabilities)
	owner, _ := reportActors(t, f, account.AccountID)
	input := reportingInputs(t, f, account.AccountID, f.host)
	opts := hostedaccounts.DraftReportOptions{Period: reportPeriod(), InputRevision: 1, OwnerUserID: owner}
	_, err := f.svc.PrepareReport(t.Context(), f.host, f.org, account.AccountID, opts)
	require.Error(t, err, "inputs must exist before generation")
	savedReportInputs(t, f, account.AccountID, f.host, input, 0)
	report, err := f.svc.PrepareReport(t.Context(), f.host, f.org, account.AccountID, opts)
	require.NoError(t, err)
	require.Equal(t, "draft", report.Status)
	require.False(t, report.Result.Complete)
	_, err = f.svc.AccountReport(t.Context(), f.user, f.org, account.AccountID, report.ReportID)
	require.Error(t, err, "drafts remain private")
	_, err = f.svc.PublishReport(t.Context(), f.host, f.org, account.AccountID, report.ReportID, 99, "source limitations")
	require.Error(t, err, "publication requires the exact revision")
	savedReportInputs(t, f, account.AccountID, f.host, input, 1)
	_, err = f.svc.PublishReport(t.Context(), f.host, f.org, account.AccountID, report.ReportID, report.Revision, "source limitations")
	require.Error(t, err, "new saved inputs require regeneration")
	opts.ReportID, opts.ExpectedRevision, opts.InputRevision = report.ReportID, report.Revision, 2
	regenerated, err := f.svc.PrepareReport(t.Context(), f.host, f.org, account.AccountID, opts)
	require.NoError(t, err)
	require.Equal(t, report.Revision+1, regenerated.Revision)
	issued, err := f.svc.PublishReport(t.Context(), f.host, f.org, account.AccountID, report.ReportID, regenerated.Revision, "missing contractual source definitions")
	require.NoError(t, err, "one authorized creator can generate and publish")
	require.Equal(t, "issued", issued.Status)
	require.Equal(t, f.host, issued.CreatedBy)
	require.Equal(t, f.host, issued.IssuedBy)
	_, err = f.svc.AccountReport(t.Context(), f.user, f.org, account.AccountID, report.ReportID)
	require.NoError(t, err, "explicit publication makes the saved report client-visible")
	other := f.client(t, "Other", f.user, hostedaccounts.AllCapabilities)
	_, err = f.svc.AccountReport(t.Context(), f.user, f.org, other.AccountID, report.ReportID)
	require.Error(t, err)
	scoped, err := hostedaccounts.WithRequestAccountID(t.Context(), account.AccountID)
	require.NoError(t, err)
	_, err = f.svc.PrepareReport(scoped, f.host, f.org, account.AccountID, opts)
	require.Error(t, err, "client preview is read-only")
}

// Issue directly here to isolate persistence/read/delivery guards from artifact generation.
func issueStoredReport(t *testing.T, f fixture, r hostedaccounts.ReportRecord, reviewer int64) {
	t.Helper()
	_, err := f.db.ExecContext(t.Context(), `UPDATE hosted_report SET status='issued',issued_at=clock_timestamp(),issued_by=$2 WHERE id=$1`, r.ID, reviewer)
	require.NoError(t, err)
}
func TestHostedReportCorrectionsDeliveryAndIssuedHistory(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Reports client", f.user, hostedaccounts.AllCapabilities)
	owner, reviewer := reportActors(t, f, client.AccountID)
	savedReportInputs(t, f, client.AccountID, reviewer, reportingInputs(t, f, client.AccountID, f.host), 0)
	opts := hostedaccounts.DraftReportOptions{Period: reportPeriod(), InputRevision: 1, OwnerUserID: owner}
	r, err := f.svc.PrepareReport(t.Context(), f.host, f.org, client.AccountID, opts)
	require.NoError(t, err)
	issueStoredReport(t, f, r, f.host)
	for _, statement := range []string{`UPDATE hosted_report SET result='{}'`, `DELETE FROM hosted_report`, `TRUNCATE hosted_report CASCADE`} {
		_, err = f.db.ExecContext(t.Context(), statement)
		require.Error(t, err)
	}
	opts.CorrectionOf = r.ReportID
	opts.CorrectionReason = "new source evidence"
	correction, err := f.svc.PrepareReport(t.Context(), f.host, f.org, client.AccountID, opts)
	require.NoError(t, err)
	require.EqualValues(t, 2, correction.Number)
	require.Equal(t, r.ReportID, correction.CorrectionOf)
	reports, err := f.svc.AccountReports(t.Context(), f.user, f.org, client.AccountID, hostedaccounts.ReportListOptions{})
	require.NoError(t, err)
	require.Len(t, reports.Reports, 1)
	require.Equal(t, r.ReportID, reports.Reports[0].ReportID, "draft correction cannot hide issued original")
	issueStoredReport(t, f, correction, reviewer)
	_, err = f.svc.PrepareReport(t.Context(), f.host, f.org, client.AccountID, opts)
	require.Error(t, err, "a new correction must follow the latest issued version")
	opts.CorrectionOf = correction.ReportID
	next, err := f.svc.PrepareReport(t.Context(), f.host, f.org, client.AccountID, opts)
	require.NoError(t, err)
	require.EqualValues(t, 3, next.Number)
	reports, err = f.svc.AccountReports(t.Context(), f.user, f.org, client.AccountID, hostedaccounts.ReportListOptions{})
	require.NoError(t, err)
	require.Len(t, reports.Reports, 1)
	require.Equal(t, correction.ReportID, reports.Reports[0].ReportID)
	_, err = f.svc.AccountReport(t.Context(), f.user, f.org, client.AccountID, r.ReportID)
	require.NoError(t, err, "original issue remains accessible")
	d := hostedaccounts.DeliveryEvidence{RequestKey: uuid.NewString(), Channel: "agreed portal notification", Status: "failed", SourceRef: "send-log/1", FailureCategory: "unavailable"}
	require.NoError(t, f.svc.RecordReportDelivery(t.Context(), f.host, f.org, client.AccountID, r.ReportID, d))
	require.NoError(t, f.svc.RecordReportDelivery(t.Context(), f.host, f.org, client.AccountID, r.ReportID, d))
	d.Status = "confirmed"
	require.Error(t, f.svc.RecordReportDelivery(t.Context(), f.host, f.org, client.AccountID, r.ReportID, d), "confirmation requires actual evidence")
	sent := time.Now().UTC()
	d.RequestKey = uuid.NewString()
	d.SentAt = &sent
	d.Evidence = "provider receipt 2"
	d.FailureCategory = ""
	require.NoError(t, f.svc.RecordReportDelivery(t.Context(), f.host, f.org, client.AccountID, r.ReportID, d))
	require.NoError(t, f.svc.RecordReportDelivery(t.Context(), f.host, f.org, client.AccountID, r.ReportID, d))
	d.RequestKey = uuid.NewString()
	require.Error(t, f.svc.RecordReportDelivery(t.Context(), f.host, f.org, client.AccountID, r.ReportID, d), "new retry cannot duplicate confirmed delivery")
	r, err = f.svc.HostReport(t.Context(), f.host, f.org, client.AccountID, r.ReportID)
	require.NoError(t, err)
	require.Len(t, r.Deliveries, 2)
	require.Equal(t, "failed", r.Deliveries[0].Status)
	require.Equal(t, "confirmed", r.Deliveries[1].Status)
	require.NoError(t, f.svc.SetReportingAuthority(t.Context(), f.host, f.org, client.AccountID, hostedaccounts.ReportingAuthority{UserID: owner}, 1))
	_, err = f.svc.HostReport(t.Context(), f.host, f.org, client.AccountID, r.ReportID)
	require.Error(t, err, "revoked authority cannot read stored reports")
}

func TestHostedReportLifecycleAuditFailureRollsBack(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Reports client", f.user, hostedaccounts.AllCapabilities)
	owner, reviewer := reportActors(t, f, client.AccountID)
	savedReportInputs(t, f, client.AccountID, reviewer, reportingInputs(t, f, client.AccountID, f.host), 0)
	_, err := f.db.ExecContext(t.Context(), `CREATE FUNCTION fail_report_lifecycle_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.event_type IN ('prepare_report','record_report_delivery') THEN RAISE EXCEPTION 'audit rejected'; END IF; RETURN NEW; END $$; CREATE TRIGGER report_lifecycle_audit_failure BEFORE INSERT ON activity_log FOR EACH ROW EXECUTE FUNCTION fail_report_lifecycle_audit()`)
	require.NoError(t, err)
	opts := hostedaccounts.DraftReportOptions{Period: reportPeriod(), InputRevision: 1, OwnerUserID: owner}
	_, err = f.svc.PrepareReport(t.Context(), f.host, f.org, client.AccountID, opts)
	require.Error(t, err)
	var n int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_report`).Scan(&n))
	require.Zero(t, n)
	_, err = f.db.ExecContext(t.Context(), `ALTER TABLE activity_log DISABLE TRIGGER report_lifecycle_audit_failure`)
	require.NoError(t, err)
	r, err := f.svc.PrepareReport(t.Context(), f.host, f.org, client.AccountID, opts)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `ALTER TABLE activity_log ENABLE TRIGGER report_lifecycle_audit_failure`)
	require.NoError(t, err)
	issueStoredReport(t, f, r, reviewer)
	require.Error(t, f.svc.RecordReportDelivery(t.Context(), f.host, f.org, client.AccountID, r.ReportID, hostedaccounts.DeliveryEvidence{RequestKey: uuid.NewString(), Channel: "email", Status: "failed", SourceRef: "outbox/1", FailureCategory: "unavailable"}))
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_report_delivery`).Scan(&n))
	require.Zero(t, n)
}

func TestHostedReportRejectsFutureEvidenceTimes(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Reports client", f.user, hostedaccounts.AllCapabilities)
	owner, reviewer := reportActors(t, f, client.AccountID)
	savedReportInputs(t, f, client.AccountID, reviewer, reportingInputs(t, f, client.AccountID, f.host), 0)
	opts := hostedaccounts.DraftReportOptions{Period: reportPeriod(), InputRevision: 1, OwnerUserID: owner}
	opts.Period.AsOf = time.Now().Add(time.Minute)
	_, err := f.svc.PrepareReport(t.Context(), f.host, f.org, client.AccountID, opts)
	require.ErrorContains(t, err, "as-of cannot be in the future")
	opts.Period.AsOf = time.Now().Add(4 * time.Second)
	r, err := f.svc.PrepareReport(t.Context(), f.host, f.org, client.AccountID, opts)
	require.NoError(t, err, "small clock skew is permitted")
	issueStoredReport(t, f, r, reviewer)
	sent, future := time.Now().UTC(), time.Now().Add(time.Minute)
	for _, d := range []hostedaccounts.DeliveryEvidence{
		{SentAt: &future},
		{SentAt: &sent, ReceivedAt: &future},
	} {
		d.RequestKey, d.Channel, d.Status, d.SourceRef, d.Evidence = uuid.NewString(), "email", "confirmed", "receipt", "provider receipt"
		require.ErrorContains(t, f.svc.RecordReportDelivery(t.Context(), f.host, f.org, client.AccountID, r.ReportID, d), "timestamps cannot be in the future")
	}
	_, err = f.svc.CreateReportRequest(t.Context(), f.host, f.org, client.AccountID, hostedaccounts.ReportRequest{ReceivedAt: future, SourceRef: "request email", Requester: "client", Scope: "energy export", OwnerUserID: owner})
	require.ErrorContains(t, err, "receipt cannot be in the future")
	_, err = f.svc.CreateReportRequest(t.Context(), f.host, f.org, client.AccountID, hostedaccounts.ReportRequest{ReceivedAt: sent, TargetAt: future, SourceRef: "request email", Requester: "client", Scope: "energy export", OwnerUserID: owner})
	require.NoError(t, err, "planned request targets may be in the future")
}

func TestHostedReportOfficialPeriodHasOneOriginal(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Reports client", f.user, hostedaccounts.AllCapabilities)
	owner, reviewer := reportActors(t, f, client.AccountID)
	savedReportInputs(t, f, client.AccountID, reviewer, reportingInputs(t, f, client.AccountID, f.host), 0)
	for _, kind := range []string{"monthly", "quarterly", "annual", "ad_hoc"} {
		t.Run(kind, func(t *testing.T) {
			period := reportPeriod()
			period.Kind = kind
			if kind == "quarterly" {
				period.Start = period.End.AddDate(0, -3, 0)
			}
			if kind == "annual" {
				period.Start = time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
				period.End = period.Start.AddDate(1, 0, 0)
			}
			opts := hostedaccounts.DraftReportOptions{Period: period, InputRevision: 1, OwnerUserID: owner}
			if kind == "ad_hoc" {
				for range 2 {
					request, err := f.svc.CreateReportRequest(t.Context(), f.host, f.org, client.AccountID, hostedaccounts.ReportRequest{ReceivedAt: period.Start.Add(-time.Hour), SourceRef: "client request receipt", Requester: "Client representative", Scope: "Independent operational response", OwnerUserID: owner})
					require.NoError(t, err)
					opts.Period.RequestRef = request.RequestID
					opts.Period.RequestReceivedAt = request.ReceivedAt
					_, err = f.svc.PrepareReport(t.Context(), f.host, f.org, client.AccountID, opts)
					require.NoError(t, err, "independent requested ad hoc responses are allowed")
				}
				return
			}
			first, err := f.svc.PrepareReport(t.Context(), f.host, f.org, client.AccountID, opts)
			require.NoError(t, err)
			_, err = f.svc.PrepareReport(t.Context(), f.host, f.org, client.AccountID, opts)
			require.ErrorContains(t, err, first.ReportID)
			opts.ReportID, opts.ExpectedRevision = first.ReportID, first.Revision
			_, err = f.svc.PrepareReport(t.Context(), f.host, f.org, client.AccountID, opts)
			require.NoError(t, err, "existing draft may be regenerated")
		})
	}
}
