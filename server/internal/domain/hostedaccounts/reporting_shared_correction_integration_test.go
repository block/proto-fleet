package hostedaccounts_test

import (
	"testing"

	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/stretchr/testify/require"
)

func TestHostedReportOperatorsShareCorrectionWithoutOverwritingDraft(t *testing.T) {
	f, client, reviewer, draft := draftReportFixture(t)
	original, err := f.svc.PublishReport(t.Context(), reviewer, f.org, client.AccountID, draft.ReportID, draft.Revision, "reviewed source limitations")
	require.NoError(t, err)
	var owner, reviewerUUID string
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT user_id FROM "user" WHERE id=$1`, f.host).Scan(&owner))
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT user_id FROM "user" WHERE id=$1`, reviewer).Scan(&reviewerUUID))
	opts := hostedaccounts.DraftReportOptions{Period: reportPeriod(), InputRevision: 1, OwnerUserID: owner, CorrectionOf: original.ReportID, CorrectionReason: "late source evidence"}
	correction, err := f.svc.PrepareReport(t.Context(), f.host, f.org, client.AccountID, opts)
	require.NoError(t, err)
	savedReportInputs(t, f, client.AccountID, reviewer, reportingInputs(t, f, client.AccountID, f.host), 1)
	opts.InputRevision, opts.OwnerUserID, opts.CorrectionReason = 2, reviewerUUID, "another operator's reason"
	opened, err := f.svc.PrepareReport(t.Context(), reviewer, f.org, client.AccountID, opts)
	require.NoError(t, err)
	require.Equal(t, correction, opened, "opening shared work preserves the original snapshot and ownership")
	// Explicit regeneration is still an edit with an expected revision.
	edit := opts
	edit.CorrectionOf, edit.CorrectionReason = "", ""
	edit.ReportID, edit.ExpectedRevision = correction.ReportID, correction.Revision
	edit.OwnerUserID = owner
	correction, err = f.svc.PrepareReport(t.Context(), f.host, f.org, client.AccountID, edit)
	require.NoError(t, err)
	inReview, err := f.svc.HostReport(t.Context(), reviewer, f.org, client.AccountID, correction.ReportID)
	require.NoError(t, err)
	opened, err = f.svc.PrepareReport(t.Context(), reviewer, f.org, client.AccountID, opts)
	require.NoError(t, err)
	require.Equal(t, inReview, opened, "opening shared work preserves the saved draft")
	_, err = f.svc.PublishReport(t.Context(), f.host, f.org, client.AccountID, opened.ReportID, opened.Revision, "source limitations")
	require.NoError(t, err)

}

func TestHostedReportConcurrentCorrectionRequestsCreateOneSharedDraft(t *testing.T) {
	f, client, reviewer, draft := draftReportFixture(t)
	original, err := f.svc.PublishReport(t.Context(), reviewer, f.org, client.AccountID, draft.ReportID, draft.Revision, "reviewed source limitations")
	require.NoError(t, err)
	var owner string
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT user_id FROM "user" WHERE id=$1`, f.host).Scan(&owner))
	opts := hostedaccounts.DraftReportOptions{Period: reportPeriod(), InputRevision: 1, OwnerUserID: owner, CorrectionOf: original.ReportID, CorrectionReason: "late evidence"}
	type result struct {
		record hostedaccounts.ReportRecord
		err    error
	}
	results := make(chan result, 2)
	start := make(chan struct{})
	for _, actor := range []int64{f.host, reviewer} {
		go func() {
			<-start
			record, err := f.svc.PrepareReport(t.Context(), actor, f.org, client.AccountID, opts)
			results <- result{record, err}
		}()
	}
	close(start)
	first, second := <-results, <-results
	require.NoError(t, first.err)
	require.NoError(t, second.err)
	require.Equal(t, first.record, second.record)
	var count int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_report WHERE correction_of=$1`, original.ID).Scan(&count))
	require.Equal(t, 1, count)
	// Parent access alone must not expose a shared draft with a wider site scope.
	var site int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO site(org_id,name,slug) VALUES($1,'Private draft site','private-draft-site') RETURNING id`, f.org).Scan(&site))
	_, err = f.db.ExecContext(t.Context(), `UPDATE hosted_report SET site_ids=ARRAY[$1]::bigint[] WHERE id=$2`, site, first.record.ID)
	require.NoError(t, err)
	require.NoError(t, f.svc.SetReportingAuthority(t.Context(), f.host, f.org, client.AccountID, hostedaccounts.ReportingAuthority{UserID: owner, CanCreate: true}, 1))
	_, err = f.svc.PrepareReport(t.Context(), f.host, f.org, client.AccountID, opts)
	require.Error(t, err)
}
