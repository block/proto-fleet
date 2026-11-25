package hostedaccounts_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestHostedReportingQueueLoadsReportMetadataOnly(t *testing.T) {
	f, client, reviewer, draft := draftReportFixture(t)
	_, err := f.db.ExecContext(t.Context(), `INSERT INTO hosted_report(report_id,organization_id,account_id,series_id,number,kind,period_start,period_end,as_of,timezone,period,input_revision,result,site_ids,created_by,owner_id)
 SELECT gen_random_uuid()::text,organization_id,account_id,gen_random_uuid()::text,1,kind,period_start,period_end,as_of,timezone,period,input_revision,
 jsonb_set(result,'{metrics,large}',jsonb_build_object('value','1','sources',jsonb_build_array(repeat('x',1000000)))),site_ids,created_by,owner_id
 FROM hosted_report CROSS JOIN generate_series(1,6) WHERE id=$1`, draft.ID)
	require.NoError(t, err)
	issueStoredReport(t, f, draft, reviewer)
	sent := time.Now().UTC()
	require.NoError(t, f.svc.RecordReportDelivery(t.Context(), f.host, f.org, client.AccountID, draft.ReportID, hostedaccounts.DeliveryEvidence{RequestKey: uuid.NewString(), Channel: "private channel", Status: "confirmed", SentAt: &sent, SourceRef: "private receipt", Evidence: "private delivery evidence"}))
	require.NoError(t, f.svc.RecordReportDelivery(t.Context(), f.host, f.org, client.AccountID, draft.ReportID, hostedaccounts.DeliveryEvidence{RequestKey: uuid.NewString(), Channel: "private channel", Status: "failed", SourceRef: "later attempt", FailureCategory: "unavailable"}))
	_, err = f.db.ExecContext(t.Context(), `INSERT INTO hosted_report_queue(queue_id,organization_id,account_id,work_key,owner_id,site_ids,projection,fingerprint)
 SELECT gen_random_uuid()::text,organization_id,account_id,report_id,owner_id,site_ids,jsonb_build_object('reportId',report_id,'reasons',jsonb_build_array('draft')),report_id
 FROM hosted_report WHERE account_id=$1`, client.ID)
	require.NoError(t, err)

	page, err := f.svc.HostReportQueue(t.Context(), f.host, f.org, 100, "")
	require.NoError(t, err)
	require.Len(t, page.Items, 7)
	foundIssued := false
	for _, item := range page.Items {
		r := item.Report
		require.NotNil(t, r)
		require.Equal(t, client.AccountID, r.AccountID)
		require.Equal(t, client.Name, r.AccountName)
		require.NotEmpty(t, r.ReportID)
		require.NotEmpty(t, r.SeriesID)
		require.Equal(t, draft.Number, r.Number)
		require.Equal(t, draft.Revision, r.Revision)
		require.Equal(t, draft.InputRevision, r.InputRevision)
		require.Equal(t, draft.CreatedBy, r.CreatedBy)
		require.Equal(t, draft.OwnerID, r.OwnerID)
		require.Equal(t, draft.Period, r.Period)
		require.Equal(t, draft.Result.Complete, r.Result.Complete)
		expectedDeadlines := draft.Result.Deadlines
		expectedDeadlines.Contract.Sources = nil
		expectedDeadlines.Internal.Sources = nil
		require.Equal(t, expectedDeadlines, r.Result.Deadlines)
		require.Empty(t, r.Result.Metrics)
		require.Empty(t, r.Result.Obligations)
		require.Empty(t, r.Result.Sources)
		require.Empty(t, r.Result.Containers)
		require.Empty(t, r.Result.Orders)
		require.Empty(t, r.Result.Reconciliations)
		require.Empty(t, r.Result.Interruptions)
		require.Empty(t, r.Deliveries)
		if r.ReportID == draft.ReportID {
			foundIssued = true
			require.Equal(t, "issued", r.Status)
			require.NotNil(t, r.IssuedAt)
			require.Equal(t, "confirmed", r.DeliveryStatus, "a later failure cannot replace confirmed delivery")
		} else {
			require.Equal(t, "draft", r.Status)
			require.Nil(t, r.IssuedAt)
		}
	}
	require.True(t, foundIssued)
	payload, err := json.Marshal(page)
	require.NoError(t, err)
	require.Less(t, len(payload), 30000, "stored megabyte report results and delivery evidence must not enter queue pages")
	require.NotContains(t, string(payload), "private delivery evidence")
}

func TestHostedReportingQueueSummaryRechecksReportSitesWithoutRowLock(t *testing.T) {
	f, client, _, draft := draftReportFixture(t)
	var site int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO site(org_id,name,slug) VALUES($1,'Report site','report-site') RETURNING id`, f.org).Scan(&site))
	_, err := f.db.ExecContext(t.Context(), `UPDATE hosted_report SET site_ids=ARRAY[$2]::bigint[] WHERE id=$1`, draft.ID, site)
	require.NoError(t, err)
	// A queue snapshot with no sites must still check the report's current scope.
	_, err = f.db.ExecContext(t.Context(), `INSERT INTO hosted_report_queue(queue_id,organization_id,account_id,work_key,owner_id,site_ids,projection,fingerprint)
 VALUES(gen_random_uuid()::text,$1,$2,$3,$4,'{}',jsonb_build_object('reportId',$3::text,'reasons',jsonb_build_array('draft')),$3)`, f.org, client.ID, draft.ReportID, draft.OwnerID)
	require.NoError(t, err)
	var actor string
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT user_id FROM "user" WHERE id=$1`, f.host).Scan(&actor))
	require.NoError(t, f.svc.SetReportingAuthority(t.Context(), f.host, f.org, client.AccountID, hostedaccounts.ReportingAuthority{UserID: actor, CanCreate: true, SiteIDs: []int64{site}}, 1))

	barrier, err := f.db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	defer barrier.Rollback()
	_, err = barrier.ExecContext(t.Context(), `SELECT id FROM hosted_report WHERE id=$1 FOR UPDATE`, draft.ID)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	page, err := f.svc.HostReportQueue(ctx, f.host, f.org, 100, "")
	require.NoError(t, err, "queue metadata reads must not wait for an exclusive report row lock")
	require.Len(t, page.Items, 1)
	require.Equal(t, []int64{site}, page.Items[0].Report.SiteIDs)
	require.NoError(t, barrier.Rollback())

	var otherSite int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO site(org_id,name,slug) VALUES($1,'Other site','other-site') RETURNING id`, f.org).Scan(&otherSite))
	require.NoError(t, f.svc.SetReportingAuthority(t.Context(), f.host, f.org, client.AccountID, hostedaccounts.ReportingAuthority{UserID: actor, CanCreate: true, SiteIDs: []int64{otherSite}}, 2))
	page, err = f.svc.HostReportQueue(t.Context(), f.host, f.org, 100, "")
	require.NoError(t, err)
	require.Empty(t, page.Items, "live report site scope applies even when the queue snapshot has no sites")
}
