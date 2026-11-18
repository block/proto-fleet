package hostedaccounts_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func draftReportFixture(t *testing.T) (fixture, hostedaccounts.Account, int64, hostedaccounts.ReportRecord) {
	t.Helper()
	f := setup(t)
	client := f.client(t, "Reports client", f.user, hostedaccounts.AllCapabilities)
	owner, reviewer := reportActors(t, f, client.AccountID)
	savedReportInputs(t, f, client.AccountID, reviewer, reportingInputs(t, f, client.AccountID, f.host), 0)
	report, err := f.svc.PrepareReport(t.Context(), f.host, f.org, client.AccountID, hostedaccounts.DraftReportOptions{Period: reportPeriod(), InputRevision: 1, OwnerUserID: owner})
	require.NoError(t, err)
	report, err = f.svc.HostReport(t.Context(), reviewer, f.org, client.AccountID, report.ReportID)
	require.NoError(t, err)
	return f, client, reviewer, report
}
func downloadReportArtifact(t *testing.T, f fixture, actor int64, account, report, name string) []byte {
	t.Helper()
	var data bytes.Buffer
	page, err := f.svc.ReportArtifactPage(t.Context(), actor, f.org, account, report, name, 0, name == "source-inputs.json")
	require.NoError(t, err)
	for sequence := range page.Chunks {
		chunk, err := f.svc.ReportArtifactPage(t.Context(), actor, f.org, account, report, name, sequence, name == "source-inputs.json")
		require.NoError(t, err)
		require.Equal(t, page.AuthorityFingerprint, chunk.AuthorityFingerprint)
		data.Write(chunk.Data)
	}
	require.EqualValues(t, data.Len(), page.Bytes)
	digest := sha256.Sum256(data.Bytes())
	require.Equal(t, page.SHA256, hex.EncodeToString(digest[:]))
	return data.Bytes()
}
func TestHostedReportPublicationFreezesExactArtifactsAndRecordsException(t *testing.T) {
	f, client, reviewer, draft := draftReportFixture(t)
	_, err := f.svc.PublishReport(t.Context(), reviewer, f.org, client.AccountID, draft.ReportID, draft.Revision, "")
	require.Error(t, err, "incomplete issue needs an explicit exception")
	issued, err := f.svc.PublishReport(t.Context(), reviewer, f.org, client.AccountID, draft.ReportID, draft.Revision, "Meter and contract definitions pending; interim reviewed issue")
	require.NoError(t, err)
	require.Equal(t, "issued", issued.Status)
	require.NotNil(t, issued.IssuedAt)
	require.False(t, issued.Result.Complete)
	html := downloadReportArtifact(t, f, f.user, client.AccountID, issued.ReportID, "report.html")
	archive := downloadReportArtifact(t, f, f.user, client.AccountID, issued.ReportID, "evidence.zip")
	sources := downloadReportArtifact(t, f, reviewer, client.AccountID, issued.ReportID, "source-inputs.json")
	require.Contains(t, string(sources), "hosted-report-source-inputs/v1")
	_, err = f.svc.ReportArtifactPage(t.Context(), f.user, f.org, client.AccountID, issued.ReportID, "source-inputs.json", 0, false)
	require.Error(t, err, "source values are host-only")
	require.Contains(t, string(html), "Content-Security-Policy")
	require.Greater(t, len(archive), 100)
	for _, statement := range []string{`UPDATE hosted_report_artifact SET sha256=repeat('0',64)`, `DELETE FROM hosted_report_artifact_chunk`, `TRUNCATE hosted_report_artifact_chunk`, `INSERT INTO hosted_report_artifact_chunk(artifact_id,sequence,data) SELECT id,99999,'changed'::bytea FROM hosted_report_artifact LIMIT 1`} {
		_, err = f.db.ExecContext(t.Context(), statement)
		require.Error(t, err)
	}
	// New reviewed inputs do not change the issued artifact bytes.
	changed := reportingInputs(t, f, client.AccountID, f.host)
	changed.Configuration.DeliveryChannel = "new source configuration"
	savedReportInputs(t, f, client.AccountID, reviewer, changed, 1)
	require.Equal(t, sources, downloadReportArtifact(t, f, reviewer, client.AccountID, issued.ReportID, "source-inputs.json"))
	require.NotContains(t, string(sources), "new source configuration")
	require.Equal(t, html, downloadReportArtifact(t, f, f.user, client.AccountID, issued.ReportID, "report.html"))
	require.Equal(t, archive, downloadReportArtifact(t, f, f.user, client.AccountID, issued.ReportID, "evidence.zip"))
	other := f.client(t, "Other client", f.user, hostedaccounts.AllCapabilities)
	_, err = f.svc.ReportArtifactPage(t.Context(), f.user, f.org, other.AccountID, issued.ReportID, "report.html", 0, false)
	require.Error(t, err)
	require.NoError(t, f.svc.ConfigureChild(t.Context(), f.host, f.org, client.AccountID, hostedaccounts.AllCapabilities, true))
	_, err = f.svc.ReportArtifactPage(t.Context(), f.user, f.org, client.AccountID, issued.ReportID, "report.html", 0, false)
	require.Error(t, err, "suspension revokes subsequent artifact chunks")
}
func TestHostedReportPublicationAuditRollbackPreservesDraftAndNoArtifacts(t *testing.T) {
	f, client, reviewer, draft := draftReportFixture(t)
	_, err := f.db.ExecContext(t.Context(), `CREATE FUNCTION fail_publication_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.event_type='publish_report' THEN RAISE EXCEPTION 'publication audit failed'; END IF; RETURN NEW; END $$; CREATE TRIGGER publication_audit_failure BEFORE INSERT ON activity_log FOR EACH ROW EXECUTE FUNCTION fail_publication_audit()`)
	require.NoError(t, err)
	_, err = f.svc.PublishReport(t.Context(), reviewer, f.org, client.AccountID, draft.ReportID, draft.Revision, "reviewed exception")
	require.Error(t, err)
	var artifacts int
	var status string
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_report_artifact`).Scan(&artifacts))
	require.Zero(t, artifacts)
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT status FROM hosted_report WHERE id=$1`, draft.ID).Scan(&status))
	require.Equal(t, "draft", status)
	reports, err := f.svc.AccountReports(t.Context(), f.user, f.org, client.AccountID, hostedaccounts.ReportListOptions{})
	require.NoError(t, err)
	require.Empty(t, reports.Reports)
	_, err = f.db.ExecContext(t.Context(), `DROP TRIGGER publication_audit_failure ON activity_log`)
	require.NoError(t, err)
	_, err = f.svc.PublishReport(t.Context(), reviewer, f.org, client.AccountID, draft.ReportID, draft.Revision, "reviewed exception")
	require.NoError(t, err)
}
func TestHostedReportPublicationRejectsStaleInputs(t *testing.T) {
	f, client, reviewer, draft := draftReportFixture(t)
	savedReportInputs(t, f, client.AccountID, reviewer, reportingInputs(t, f, client.AccountID, f.host), 1)
	_, err := f.svc.PublishReport(t.Context(), reviewer, f.org, client.AccountID, draft.ReportID, draft.Revision, "reviewed exception")
	require.Error(t, err)
	var artifacts int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_report_artifact`).Scan(&artifacts))
	require.Zero(t, artifacts)
}

func TestHostedReportSharedCorrectionMustFollowLatestIssuedParent(t *testing.T) {
	f, client, reviewer, draft := draftReportFixture(t)
	original, err := f.svc.PublishReport(t.Context(), reviewer, f.org, client.AccountID, draft.ReportID, draft.Revision, "reviewed exception")
	require.NoError(t, err)
	var owner string
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT user_id FROM "user" WHERE id=$1`, f.host).Scan(&owner))
	options := hostedaccounts.DraftReportOptions{Period: reportPeriod(), InputRevision: 1, OwnerUserID: owner, CorrectionOf: original.ReportID, CorrectionReason: "late evidence"}
	first, err := f.svc.PrepareReport(t.Context(), f.host, f.org, client.AccountID, options)
	require.NoError(t, err)
	second, err := f.svc.PrepareReport(t.Context(), f.host, f.org, client.AccountID, options)
	require.NoError(t, err)
	require.Equal(t, first.ReportID, second.ReportID)
	require.Equal(t, first.Number, second.Number)
	first, err = f.svc.HostReport(t.Context(), reviewer, f.org, client.AccountID, first.ReportID)
	require.NoError(t, err)
	first, err = f.svc.PublishReport(t.Context(), reviewer, f.org, client.AccountID, first.ReportID, first.Revision, "reviewed exception")
	require.NoError(t, err)
	_, err = f.svc.PrepareReport(t.Context(), f.host, f.org, client.AccountID, options)
	require.Error(t, err, "new corrections must target the latest issued version")
	options.CorrectionOf = first.ReportID
	second, err = f.svc.PrepareReport(t.Context(), f.host, f.org, client.AccountID, options)
	require.NoError(t, err)
	require.Greater(t, second.Number, first.Number)
	visible, err := f.svc.AccountReports(t.Context(), f.user, f.org, client.AccountID, hostedaccounts.ReportListOptions{})
	require.NoError(t, err)
	require.Len(t, visible.Reports, 1)
	require.Equal(t, first.ReportID, visible.Reports[0].ReportID)
}

func TestHostedReportConcurrentPublicationHasOneAtomicWinner(t *testing.T) {
	f, client, operator, draft := draftReportFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	gate, err := f.db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer gate.Rollback()
	_, err = gate.ExecContext(ctx, `SELECT id FROM hosted_report WHERE id=$1 FOR UPDATE`, draft.ID)
	require.NoError(t, err)
	var gatePID int
	require.NoError(t, gate.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&gatePID))
	results := make(chan error, 2)
	publish := func() {
		_, err := f.svc.PublishReport(ctx, operator, f.org, client.AccountID, draft.ReportID, draft.Revision, "Missing evidence is disclosed")
		results <- err
	}
	go publish()
	var firstPID int
	require.Eventually(t, func() bool {
		return f.db.QueryRowContext(ctx, `SELECT pid FROM pg_stat_activity WHERE datname=current_database() AND query LIKE '-- name: LockHostedReport :one%' AND $1=ANY(pg_blocking_pids(pid))`, gatePID).Scan(&firstPID) == nil
	}, 3*time.Second, 10*time.Millisecond)
	go publish()
	require.Eventually(t, func() bool {
		var waiting bool
		err := f.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND query LIKE '-- name: LockReportingInputsForReport :one%' AND $1=ANY(pg_blocking_pids(pid)))`, firstPID).Scan(&waiting)
		return err == nil && waiting
	}, 3*time.Second, 10*time.Millisecond)
	require.NoError(t, gate.Commit())
	first, second := <-results, <-results
	if first == nil {
		require.Error(t, second)
	} else {
		require.NoError(t, second)
	}
	var status string
	var revision int64
	var artifacts, issues int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT status,revision FROM hosted_report WHERE id=$1`, draft.ID).Scan(&status, &revision))
	require.Equal(t, "issued", status)
	require.Equal(t, draft.Revision+1, revision)
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_report_artifact WHERE report_internal_id=$1`, draft.ID).Scan(&artifacts))
	require.Equal(t, 3, artifacts)
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM activity_log WHERE organization_id=$1 AND event_type='publish_report'`, f.org).Scan(&issues))
	require.Equal(t, 1, issues)
	require.NotEmpty(t, downloadReportArtifact(t, f, f.user, client.AccountID, draft.ReportID, "report.html"))
	visible, err := f.svc.AccountReports(t.Context(), f.user, f.org, client.AccountID, hostedaccounts.ReportListOptions{})
	require.NoError(t, err)
	require.Len(t, visible.Reports, 1)
}

func TestHostedReportPublicationRejectsLegacyDuplicateOriginal(t *testing.T) {
	f, client, reviewer, first := draftReportFixture(t)
	duplicate := uuid.NewString()
	// Model two originals retained from before the preparation guard existed.
	_, err := f.db.ExecContext(t.Context(), `INSERT INTO hosted_report(report_id,organization_id,account_id,series_id,number,kind,period_start,period_end,as_of,timezone,period,input_revision,result,site_ids,created_by,owner_id)
 SELECT $1,organization_id,account_id,$2,number,kind,period_start,period_end,as_of,timezone,period,input_revision,result,site_ids,created_by,owner_id FROM hosted_report WHERE id=$3`, duplicate, uuid.NewString(), first.ID)
	require.NoError(t, err)
	_, err = f.svc.PublishReport(t.Context(), reviewer, f.org, client.AccountID, first.ReportID, first.Revision, "reviewed source limitations")
	require.NoError(t, err)
	_, err = f.svc.PublishReport(t.Context(), reviewer, f.org, client.AccountID, duplicate, 1, "reviewed source limitations")
	require.ErrorContains(t, err, "an original report has already been issued")
	require.NotContains(t, err.Error(), first.ReportID, "publication conflicts must not disclose another report identity")
	var issued, artifacts int
	var status string
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_report WHERE account_id=$1 AND status='issued'`, client.ID).Scan(&issued))
	require.Equal(t, 1, issued)
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT status FROM hosted_report WHERE report_id=$1`, duplicate).Scan(&status))
	require.Equal(t, "draft", status)
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_report_artifact a JOIN hosted_report r ON r.id=a.report_internal_id WHERE r.report_id=$1`, duplicate).Scan(&artifacts))
	require.Zero(t, artifacts, "rejected duplicate cannot freeze client artifacts")
}

func TestHostedReportPublicationDeadlineRollsBackArtifacts(t *testing.T) {
	f, client, reviewer, draft := draftReportFixture(t)
	_, err := f.db.ExecContext(t.Context(), `CREATE FUNCTION slow_report_artifact() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_sleep(1); RETURN NEW; END $$;
 CREATE TRIGGER slow_report_artifact BEFORE INSERT ON hosted_report_artifact_chunk FOR EACH ROW EXECUTE FUNCTION slow_report_artifact()`)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 250*time.Millisecond)
	defer cancel()
	_, err = f.svc.PublishReport(ctx, reviewer, f.org, client.AccountID, draft.ReportID, draft.Revision, "reviewed limitations")
	require.True(t, fleeterror.IsFailedPreconditionError(err), "%v", err)
	require.ErrorContains(t, err, "execution time budget")
	var artifacts, audits int
	var status string
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_report_artifact`).Scan(&artifacts))
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM activity_log WHERE event_type='publish_report'`).Scan(&audits))
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT status FROM hosted_report WHERE id=$1`, draft.ID).Scan(&status))
	require.Zero(t, artifacts)
	require.Zero(t, audits)
	require.Equal(t, "draft", status)
	_, err = f.db.ExecContext(t.Context(), `DROP TRIGGER slow_report_artifact ON hosted_report_artifact_chunk`)
	require.NoError(t, err)
	_, err = f.svc.PublishReport(t.Context(), reviewer, f.org, client.AccountID, draft.ReportID, draft.Revision, "reviewed limitations")
	require.NoError(t, err)
}

func TestHostedReportPreparationDeadlineLeavesNoDraft(t *testing.T) {
	f, account, owner, in, p := reportContextFixture(t)
	saved, err := saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, account.AccountID, 0, "inputs", in)
	require.NoError(t, err)
	barrier, err := f.db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	defer barrier.Rollback()
	_, err = barrier.ExecContext(t.Context(), `SELECT account_id FROM hosted_reporting_inputs WHERE account_id=$1 FOR NO KEY UPDATE`, account.ID)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 250*time.Millisecond)
	defer cancel()
	_, err = f.svc.PrepareReport(ctx, f.host, f.org, account.AccountID, hostedaccounts.DraftReportOptions{Period: p, InputRevision: saved.Revision, OwnerUserID: owner})
	require.True(t, fleeterror.IsFailedPreconditionError(err), "%v", err)
	require.ErrorContains(t, err, "execution time budget")
	var reports int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_report`).Scan(&reports))
	require.Zero(t, reports)
}
