package hostedaccounts_test

import (
	"testing"

	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestHostedReportingQueuePaginationAcrossAccountsIsBoundedAndStable(t *testing.T) {
	f := setup(t)
	first := f.client(t, "First queue", f.user, hostedaccounts.AllCapabilities)
	second := f.client(t, "Second queue", f.user, hostedaccounts.AllCapabilities)
	reportActors(t, f, first.AccountID)
	reportActors(t, f, second.AccountID)
	_, err := f.db.ExecContext(t.Context(), `INSERT INTO hosted_report_queue(queue_id,organization_id,account_id,work_key,owner_id,site_ids,projection,fingerprint)
 SELECT gen_random_uuid()::text,$1,CASE WHEN n%2=0 THEN $2::bigint ELSE $3::bigint END,'page-'||n,$4,'{}','{"kind":"monthly","reasons":["report_missing"]}',n::text FROM generate_series(1,125) n`, f.org, first.ID, second.ID, f.host)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `INSERT INTO hosted_report_queue(queue_id,organization_id,account_id,work_key,owner_id,site_ids,projection,fingerprint,notification_status)
 VALUES(gen_random_uuid()::text,$1,$2,'quiet',$3,'{}','{"reasons":[]}','quiet','quiet'),
 (gen_random_uuid()::text,$1,$2,'superseded',$3,'{}','{"reasons":["superseded"]}','superseded','pending')`, f.org, first.ID, f.host)
	require.NoError(t, err)
	page, err := f.svc.HostReportQueue(t.Context(), f.host, f.org, 0, "")
	require.NoError(t, err)
	require.Len(t, page.Items, 50, "default raw page size is bounded")
	require.NotEmpty(t, page.NextPageToken)
	seen := map[string]bool{}
	accounts := map[string]bool{}
	check := func(page hostedaccounts.ReportQueuePage) {
		t.Helper()
		for _, item := range page.Items {
			require.False(t, seen[item.QueueID], "cursor must not repeat work")
			seen[item.QueueID] = true
			accounts[item.AccountID] = true
		}
	}
	check(page)
	// An insertion above the cursor must not shift or repeat older pages.
	newest := uuid.NewString()
	_, err = f.db.ExecContext(t.Context(), `INSERT INTO hosted_report_queue(queue_id,organization_id,account_id,work_key,owner_id,site_ids,projection,fingerprint)
 VALUES($1,$2,$3,'later-insert',$4,'{}','{"reasons":["report_missing"]}','new')`, newest, f.org, first.ID, f.host)
	require.NoError(t, err)
	page, err = f.svc.HostReportQueue(t.Context(), f.host, f.org, 50, page.NextPageToken)
	require.NoError(t, err)
	require.Len(t, page.Items, 50)
	check(page)
	page, err = f.svc.HostReportQueue(t.Context(), f.host, f.org, 50, page.NextPageToken)
	require.NoError(t, err)
	require.Len(t, page.Items, 25)
	require.Empty(t, page.NextPageToken)
	check(page)
	require.Len(t, seen, 125, "all active work remains reachable without an overall cutoff")
	require.False(t, seen[newest])
	require.Len(t, accounts, 2, "one page stream orders all admitted accounts together")
	maxPage, err := f.svc.HostReportQueue(t.Context(), f.host, f.org, 100, "")
	require.NoError(t, err)
	require.Len(t, maxPage.Items, 100)
	for _, size := range []int32{-1, 101} {
		_, err = f.svc.HostReportQueue(t.Context(), f.host, f.org, size, "")
		require.ErrorContains(t, err, "invalid reporting queue page")
	}
	_, err = f.svc.HostReportQueue(t.Context(), f.host, f.org, 50, "invalid")
	require.ErrorContains(t, err, "invalid reporting queue cursor")
	_, err = f.svc.HostReportQueue(t.Context(), f.user, f.org, 50, maxPage.NextPageToken)
	require.ErrorContains(t, err, "invalid reporting queue cursor")
}

func TestHostedReportingQueueCursorAdvancesPastFilteredRawPage(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Scoped queue", f.user, hostedaccounts.AllCapabilities)
	owner, _ := reportActors(t, f, account.AccountID)
	var site int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO site(org_id,name,slug) VALUES($1,'Queue site','queue-site') RETURNING id`, f.org).Scan(&site))
	require.NoError(t, f.svc.SetReportingAuthority(t.Context(), f.host, f.org, account.AccountID, hostedaccounts.ReportingAuthority{UserID: owner, CanCreate: true, SiteIDs: []int64{site}}, 1))
	// The newest raw row is outside the actor's current site authority.
	_, err := f.db.ExecContext(t.Context(), `INSERT INTO hosted_report_queue(queue_id,organization_id,account_id,work_key,owner_id,site_ids,projection,fingerprint)
 VALUES(gen_random_uuid()::text,$1,$2,'visible',$3,'{}','{"reasons":["draft"]}','visible'),
 (gen_random_uuid()::text,$1,$2,'filtered',$3,ARRAY[$4]::bigint[],'{"reasons":["draft"]}','filtered')`, f.org, account.ID, f.host, site+100)
	require.NoError(t, err)
	page, err := f.svc.HostReportQueue(t.Context(), f.host, f.org, 1, "")
	require.NoError(t, err)
	require.Empty(t, page.Items)
	require.NotEmpty(t, page.NextPageToken, "filtered work must not strand older admitted items")
	page, err = f.svc.HostReportQueue(t.Context(), f.host, f.org, 1, page.NextPageToken)
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.Empty(t, page.NextPageToken)
}
