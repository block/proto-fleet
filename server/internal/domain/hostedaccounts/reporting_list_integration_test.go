package hostedaccounts_test

import (
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestHostedReportListsUseBoundedMetadataPages(t *testing.T) {
	f, client, _, draft := draftReportFixture(t)
	for i := 1; i <= 6; i++ {
		_, err := f.db.ExecContext(t.Context(), `INSERT INTO hosted_report(report_id,organization_id,account_id,series_id,number,kind,period_start,period_end,as_of,timezone,period,input_revision,result,site_ids,created_by,owner_id)
   SELECT $2,organization_id,account_id,$3,1,kind,period_start+make_interval(months=>$4),period_end+make_interval(months=>$4),as_of+make_interval(months=>$4),timezone,period,input_revision,
    jsonb_set(result,'{metrics,large}',jsonb_build_object('value','1','sources',jsonb_build_array(repeat('x',1000000)))),site_ids,created_by,owner_id
   FROM hosted_report WHERE id=$1`, draft.ID, uuid.NewString(), uuid.NewString(), i)
		require.NoError(t, err)
	}
	// Include equal-date rows to exercise the stable id tie breaker.
	_, err := f.db.ExecContext(t.Context(), `INSERT INTO hosted_report(report_id,organization_id,account_id,series_id,number,kind,period_start,period_end,as_of,timezone,period,input_revision,result,site_ids,created_by,owner_id)
 SELECT $2,organization_id,account_id,$3,1,kind,period_start,period_end,as_of,timezone,period,input_revision,result,site_ids,created_by,owner_id FROM hosted_report WHERE id=$1`, draft.ID, uuid.NewString(), uuid.NewString())
	require.NoError(t, err)
	options := hostedaccounts.ReportListOptions{PageSize: 2}
	seen := map[string]bool{}
	pages := 0
	for {
		page, err := f.svc.HostReports(t.Context(), f.host, f.org, client.AccountID, options)
		require.NoError(t, err)
		require.LessOrEqual(t, len(page.Reports), 2)
		for _, r := range page.Reports {
			require.False(t, seen[r.ReportID])
			seen[r.ReportID] = true
			require.Empty(t, r.Result.Metrics)
			require.Empty(t, r.Result.Obligations)
			require.Empty(t, r.Deliveries)
			require.Positive(t, r.InputRevision)
			payload, err := json.Marshal(r)
			require.NoError(t, err)
			require.Less(t, len(payload), 20000, "large frozen results must not enter a list page")
		}
		pages++
		if page.NextPageToken == "" {
			break
		}
		options.PageToken = page.NextPageToken
	}
	require.Equal(t, 4, pages)
	require.Len(t, seen, 8)
	first, err := f.svc.HostReports(t.Context(), f.host, f.org, client.AccountID, hostedaccounts.ReportListOptions{PageSize: 1})
	require.NoError(t, err)
	_, err = f.svc.AccountReports(t.Context(), f.user, f.org, client.AccountID, hostedaccounts.ReportListOptions{PageToken: first.NextPageToken})
	require.ErrorContains(t, err, "cursor")
	_, err = f.svc.HostReports(t.Context(), f.host, f.org, client.AccountID, hostedaccounts.ReportListOptions{PageSize: 101})
	require.Error(t, err)
	empty, err := f.svc.HostReports(t.Context(), f.host, f.org, client.AccountID, hostedaccounts.ReportListOptions{Search: "not-a-report"})
	require.NoError(t, err)
	require.Empty(t, empty.Reports)
}

func TestHostedReportListSkipsUnauthorizedPagesWithoutStalling(t *testing.T) {
	f, client, _, draft := draftReportFixture(t)
	var excluded int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO site(org_id,name,slug) VALUES($1,'excluded','excluded') RETURNING id`, f.org).Scan(&excluded))
	_, err := f.db.ExecContext(t.Context(), `INSERT INTO hosted_report(report_id,organization_id,account_id,series_id,number,kind,period_start,period_end,as_of,timezone,period,input_revision,result,site_ids,created_by,owner_id)
 SELECT $2,organization_id,account_id,$3,1,kind,period_start,period_end,as_of,timezone,period,input_revision,result,ARRAY[$4::bigint],created_by,owner_id FROM hosted_report WHERE id=$1`, draft.ID, uuid.NewString(), uuid.NewString(), excluded)
	require.NoError(t, err)
	// The reporting grant excludes the newer row, but still permits the original.
	var actor string
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT user_id FROM "user" WHERE id=$1`, f.host).Scan(&actor))
	require.NoError(t, f.svc.SetReportingAuthority(t.Context(), f.host, f.org, client.AccountID, hostedaccounts.ReportingAuthority{UserID: actor, CanCreate: true, SiteIDs: draft.SiteIDs}, 1))
	first, err := f.svc.HostReports(t.Context(), f.host, f.org, client.AccountID, hostedaccounts.ReportListOptions{PageSize: 1})
	require.NoError(t, err)
	require.Len(t, first.Reports, 1)
	require.Equal(t, draft.ReportID, first.Reports[0].ReportID)
	require.Empty(t, first.NextPageToken)
	// A later visible report produces a cursor from that visible report, even
	// when several hidden rows fill entire raw pages before it.
	for i := 1; i <= 5; i++ {
		_, err = f.db.ExecContext(t.Context(), `INSERT INTO hosted_report(report_id,organization_id,account_id,series_id,number,kind,period_start,period_end,as_of,timezone,period,input_revision,result,site_ids,created_by,owner_id)
  SELECT $2,organization_id,account_id,$3,1,kind,period_start+interval '1 day'*$4,period_end+interval '1 day'*$4,as_of+interval '1 day'*$4,timezone,period,input_revision,result,
  CASE WHEN $4=1 THEN site_ids ELSE ARRAY[$5::bigint] END,created_by,owner_id FROM hosted_report WHERE id=$1`, draft.ID, uuid.NewString(), uuid.NewString(), i, excluded)
		require.NoError(t, err)
	}
	first, err = f.svc.HostReports(t.Context(), f.host, f.org, client.AccountID, hostedaccounts.ReportListOptions{PageSize: 1})
	require.NoError(t, err)
	require.Len(t, first.Reports, 1)
	require.NotEmpty(t, first.NextPageToken)
	payload, err := base64.RawURLEncoding.DecodeString(first.NextPageToken)
	require.NoError(t, err)
	var cursor struct {
		BeforeID    int64
		BeforeStart time.Time
	}
	require.NoError(t, json.Unmarshal(payload, &cursor))
	require.Equal(t, first.Reports[0].ID, cursor.BeforeID)
	var visibleStart time.Time
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT period_start FROM hosted_report WHERE id=$1`, first.Reports[0].ID).Scan(&visibleStart))
	require.True(t, visibleStart.Equal(cursor.BeforeStart), "cursor start %s must equal visible start %s", cursor.BeforeStart, visibleStart)
	second, err := f.svc.HostReports(t.Context(), f.host, f.org, client.AccountID, hostedaccounts.ReportListOptions{PageSize: 1, PageToken: first.NextPageToken})
	require.NoError(t, err)
	require.Len(t, second.Reports, 1)
	require.Equal(t, draft.ReportID, second.Reports[0].ReportID)
	require.Empty(t, second.NextPageToken)
	// Removing the last visible report leaves no hidden-row continuation token.
	_, err = f.db.ExecContext(t.Context(), `DELETE FROM hosted_report WHERE id=$1`, draft.ID)
	require.NoError(t, err)
	second, err = f.svc.HostReports(t.Context(), f.host, f.org, client.AccountID, hostedaccounts.ReportListOptions{PageSize: 1, PageToken: first.NextPageToken})
	require.NoError(t, err)
	require.Empty(t, second.Reports)
	require.Empty(t, second.NextPageToken)
}

func TestHostedReportListSearchMatchesVisibleReportName(t *testing.T) {
	f, client, _, _ := draftReportFixture(t)
	page, err := f.svc.HostReports(t.Context(), f.host, f.org, client.AccountID, hostedaccounts.ReportListOptions{Search: "service report"})
	require.NoError(t, err)
	require.Len(t, page.Reports, 1)
}

func TestHostedReportLatestVersionIsScopedToAccount(t *testing.T) {
	f, client, reviewer, draft := draftReportFixture(t)
	_, err := f.svc.PublishReport(t.Context(), reviewer, f.org, client.AccountID, draft.ReportID, draft.Revision, "reviewed limitations")
	require.NoError(t, err)
	other := f.client(t, "Other series owner", f.user, hostedaccounts.AllCapabilities)
	var parent int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO hosted_report(report_id,organization_id,account_id,series_id,number,kind,period_start,period_end,as_of,timezone,period,input_revision,result,site_ids,created_by,owner_id,status,issued_at,issued_by,evidence_cutoff)
 SELECT $2,organization_id,$3,series_id,1,kind,period_start,period_end,as_of,timezone,period,input_revision,result,site_ids,created_by,owner_id,'issued',clock_timestamp(),created_by,clock_timestamp() FROM hosted_report WHERE id=$1 RETURNING id`, draft.ID, uuid.NewString(), other.ID).Scan(&parent))
	_, err = f.db.ExecContext(t.Context(), `INSERT INTO hosted_report(report_id,organization_id,account_id,series_id,number,correction_of,correction_reason,kind,period_start,period_end,as_of,timezone,period,input_revision,result,site_ids,created_by,owner_id,status,issued_at,issued_by,evidence_cutoff)
 SELECT $2,organization_id,account_id,series_id,2,id,'correction',kind,period_start,period_end,as_of,timezone,period,input_revision,result,site_ids,created_by,owner_id,'issued',clock_timestamp(),created_by,clock_timestamp() FROM hosted_report WHERE id=$1`, parent, uuid.NewString())
	require.NoError(t, err)
	page, err := f.svc.AccountReports(t.Context(), f.user, f.org, client.AccountID, hostedaccounts.ReportListOptions{})
	require.NoError(t, err)
	require.Len(t, page.Reports, 1)
	require.Equal(t, draft.ReportID, page.Reports[0].ReportID)
}
