package hostedaccounts_test

import (
	"fmt"
	"slices"
	"testing"
	"time"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts/reporting"
	"github.com/stretchr/testify/require"
)

func TestHostedReportEvidenceMembershipIsFrozenAndCommerciallyScoped(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Reports client", f.user, hostedaccounts.AllCapabilities)
	owner, reviewer := reportActors(t, f, client.AccountID)
	miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	uncovered := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	var site, otherSite int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO site(org_id,name,slug) VALUES($1,'Report site','report-site') RETURNING id`, f.org).Scan(&site))
	require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO site(org_id,name,slug) VALUES($1,'Uncovered site','uncovered-site') RETURNING id`, f.org).Scan(&otherSite))
	_, err := f.db.ExecContext(t.Context(), `UPDATE device SET site_id=$1 WHERE id=$2`, site, miner)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `UPDATE device SET site_id=$1 WHERE id=$2`, otherSite, uncovered)
	require.NoError(t, err)
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(5, miner, 0), selected(5, uncovered, 0)))
	day := time.Now().UTC().Truncate(24 * time.Hour).Add(-40 * 24 * time.Hour)
	historyEpoch(t, f, day)
	_, err = f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1 WHERE client_id=$2`, day, client.ID)
	require.NoError(t, err)
	var allocation int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT id FROM hosted_account_asset_interval WHERE kind=5 AND asset_id=$1`, miner).Scan(&allocation))
	input := reportingInputs(t, f, client.AccountID, f.host)
	input.Orders = []reporting.Order{{ID: "order", Name: "Signed order", Start: day, SourceRef: "signed order"}}
	input.Coverage = []reporting.CoverageSegment{{ID: "covered", AllocationIntervalID: allocation, Kind: 5, AssetID: miner, SiteID: site, ContainerID: miner, OrderID: "order", Start: day, CapacityTHS: "100", SourceRef: "commissioning evidence"}}
	savedReportInputs(t, f, client.AccountID, reviewer, input, 0)
	var unallocated int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT id FROM hosted_account_asset_interval WHERE kind=5 AND asset_id=$1`, uncovered).Scan(&unallocated))
	for _, interval := range []int64{allocation, unallocated} {
		reportEvidenceHours(t, f.db, f, interval, day.Add(time.Hour), 1, 1, 100e12)
	}
	// A report scoped to one site must not select another noncommercial allocation.
	require.NoError(t, f.svc.SetReportingAuthority(t.Context(), f.host, f.org, client.AccountID, hostedaccounts.ReportingAuthority{UserID: owner, CanCreate: true, SiteIDs: []int64{site}}, 1))
	periodStart := time.Date(day.Year(), day.Month(), 1, 0, 0, 0, 0, time.UTC)
	opts := hostedaccounts.DraftReportOptions{Period: reporting.Period{Kind: "monthly", Start: periodStart, End: periodStart.AddDate(0, 1, 0), AsOf: reportAsOf(t, f), Timezone: "UTC"}, InputRevision: 1, OwnerUserID: owner}
	r, err := f.svc.PrepareReport(t.Context(), f.host, f.org, client.AccountID, opts)
	require.NoError(t, err)
	require.Equal(t, []int64{site}, r.SiteIDs)
	hour := fmt.Sprintf("%d %s closed ", allocation, day.Add(time.Hour).Format(time.RFC3339))
	require.Equal(t, []string{hour + "1"}, frozenReportEvidence(t, f, r.ID), "only the commercially covered interval is selected")
	reportEvidenceHours(t, f.db, f, allocation, day.Add(time.Hour), 1, 2, 200e12)
	require.Equal(t, []string{hour + "1"}, frozenReportEvidence(t, f, r.ID), "a later committed revision cannot enter selected evidence")
	opts.ReportID = r.ReportID
	opts.ExpectedRevision = 1
	opts.Period.AsOf = reportAsOf(t, f)
	r, err = f.svc.PrepareReport(t.Context(), f.host, f.org, client.AccountID, opts)
	require.NoError(t, err)
	require.Equal(t, []string{hour + "2"}, frozenReportEvidence(t, f, r.ID), "unpublished regeneration takes a new cutoff")
	// Missing commercial coverage stays an incomplete draft, even with hourly
	// evidence already present for the account.
	savedReportInputs(t, f, client.AccountID, reviewer, reportingInputs(t, f, client.AccountID, f.host), 1)
	emptyOpts := opts
	emptyOpts.Period.Kind = "quarterly"
	emptyOpts.Period.Start = emptyOpts.Period.End.AddDate(0, -3, 0)
	emptyOpts.ReportID = ""
	emptyOpts.ExpectedRevision = 0
	emptyOpts.InputRevision = 2
	emptyReport, err := f.svc.PrepareReport(t.Context(), f.host, f.org, client.AccountID, emptyOpts)
	require.NoError(t, err)
	require.False(t, emptyReport.Result.Complete)
	require.Empty(t, emptyReport.SiteIDs)
	require.Empty(t, frozenReportEvidence(t, f, emptyReport.ID))
	issueStoredReport(t, f, r, reviewer)
	for _, query := range []string{`UPDATE hosted_report SET evidence_cutoff=clock_timestamp() WHERE id=$1`, `UPDATE hosted_report SET evidence_cutoff=NULL WHERE id=$1`} {
		_, err = f.db.ExecContext(t.Context(), query, r.ID)
		require.Error(t, err, "a published cutoff cannot be changed")
	}
	require.NoError(t, f.svc.SetReportingAuthority(t.Context(), f.host, f.org, client.AccountID, hostedaccounts.ReportingAuthority{UserID: owner, CanCreate: true, SiteIDs: []int64{otherSite}}, 2))
	_, err = f.svc.HostReport(t.Context(), f.host, f.org, client.AccountID, r.ReportID)
	require.Error(t, err, "site authority is checked over frozen artifact scope")
}

func TestHostedReportAdHocRequestCapturesReceiptAndAccountability(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Requests client", f.user, hostedaccounts.AllCapabilities)
	owner, _ := reportActors(t, f, client.AccountID)
	received := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	request, err := f.svc.CreateReportRequest(t.Context(), f.host, f.org, client.AccountID, hostedaccounts.ReportRequest{ReceivedAt: received, SourceRef: "customer email receipt", Requester: "contract representative", Scope: "reviewed monthly energy export", OwnerUserID: owner, Limitations: "meter certification pending", CommunicationRef: "acknowledgement/1"})
	require.NoError(t, err)
	require.Equal(t, received.AddDate(0, 0, 30), request.TargetAt)
	page, err := f.svc.ReportRequests(t.Context(), f.host, f.org, client.AccountID, hostedaccounts.ReportRequestListOptions{})
	require.NoError(t, err)
	require.Empty(t, page.NextPageToken)
	list := page.Requests
	require.Len(t, list, 1)
	require.True(t, request.ReceivedAt.Equal(list[0].ReceivedAt))
	require.True(t, request.TargetAt.Equal(list[0].TargetAt))
	list[0].ReceivedAt = request.ReceivedAt
	list[0].TargetAt = request.TargetAt
	require.Equal(t, request, list[0])
	_, err = f.svc.ReportRequests(t.Context(), f.user, f.org, client.AccountID, hostedaccounts.ReportRequestListOptions{})
	require.Error(t, err, "private client membership is not host request authority")
}

func TestHostedReportRequestHistoryPagesNewestFirstWithoutGapsOrRepeats(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Request history client", f.user, hostedaccounts.AllCapabilities)
	other := f.client(t, "Other request history client", f.user, hostedaccounts.AllCapabilities)
	owner, _ := reportActors(t, f, client.AccountID)
	base := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	// Two requests share a receipt time across the first page boundary, so
	// paging must break the tie by id.
	received := []time.Time{base, base.Add(time.Hour), base.Add(2 * time.Hour), base.Add(2 * time.Hour), base.Add(3 * time.Hour)}
	created := make([]string, len(received))
	for i, at := range received {
		request, err := f.svc.CreateReportRequest(t.Context(), f.host, f.org, client.AccountID, hostedaccounts.ReportRequest{ReceivedAt: at, SourceRef: fmt.Sprintf("request email %d", i), Requester: "client", Scope: "energy export", OwnerUserID: owner})
		require.NoError(t, err)
		created[i] = request.RequestID
	}

	var seen []string
	var times []time.Time
	options := hostedaccounts.ReportRequestListOptions{PageSize: 2}
	for pages := 0; ; pages++ {
		require.Less(t, pages, 3, "five requests fit in three pages of two")
		page, err := f.svc.ReportRequests(t.Context(), f.host, f.org, client.AccountID, options)
		require.NoError(t, err)
		require.LessOrEqual(t, len(page.Requests), 2)
		for _, request := range page.Requests {
			require.Equal(t, owner, request.OwnerUserID)
			seen = append(seen, request.RequestID)
			times = append(times, request.ReceivedAt)
		}
		if page.NextPageToken == "" {
			break
		}
		_, err = f.svc.ReportRequests(t.Context(), f.host, f.org, other.AccountID, hostedaccounts.ReportRequestListOptions{PageToken: page.NextPageToken})
		require.ErrorContains(t, err, "invalid report request list cursor", "a cursor is bound to the account that issued it")
		options.PageToken = page.NextPageToken
	}
	require.ElementsMatch(t, created, seen)
	require.True(t, slices.IsSortedFunc(times, func(a, b time.Time) int { return b.Compare(a) }), "newest receipt first")

	_, err := f.svc.ReportRequests(t.Context(), f.host, f.org, client.AccountID, hostedaccounts.ReportRequestListOptions{PageSize: 101})
	require.ErrorContains(t, err, "invalid report request list request")
	_, err = f.svc.ReportRequests(t.Context(), f.host, f.org, client.AccountID, hostedaccounts.ReportRequestListOptions{PageToken: "not-a-cursor"})
	require.ErrorContains(t, err, "invalid report request list cursor")
}
