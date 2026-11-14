package hostedaccounts_test

import (
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts/reporting"
	"github.com/stretchr/testify/require"
)

func TestHostedReportCurrentBoardEvidencePublishesAndStaysFrozen(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Board reports", f.user, hostedaccounts.AllCapabilities)
	owner, reviewer := reportActors(t, f, client.AccountID)
	input, _, board := currentBoardReportingInputs(t, f, client.AccountID)
	period := reportPeriod()
	historyEpoch(t, f, period.Start)
	_, err := f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1 WHERE id=$2`, period.Start, input.Coverage[0].AllocationIntervalID)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `UPDATE hashboard_installation SET installed_at=$1 WHERE hashboard_id=$2`, period.Start, board)
	require.NoError(t, err)
	input.Orders[0].Start = period.Start
	input.Coverage[0].Start, input.Coverage[0].End = period.Start, period.End
	input.Hashrates[0].Start, input.Hashrates[0].End = period.Start, period.End
	input.PoolObservations[0].SourceAt, input.PoolObservations[0].ReceivedAt = period.End.Add(-time.Minute), period.End
	// Hourly evidence is held for the original board allocation, not a module surrogate.
	observed := period.Start.Add(time.Hour)
	reportEvidenceHours(t, f.db, f, input.Coverage[0].AllocationIntervalID, observed, 1, 1, 100e12)
	savedReportInputs(t, f, client.AccountID, reviewer, input, 0)
	period.AsOf = reportAsOf(t, f)
	draft, err := f.svc.PrepareReport(t.Context(), f.host, f.org, client.AccountID, hostedaccounts.DraftReportOptions{Period: period, InputRevision: 1, OwnerUserID: owner})
	require.NoError(t, err)
	require.Equal(t, "100000000000000", draft.Result.Metrics["hashrate_performance_hs"].Value)
	require.Equal(t, reporting.Unavailable, draft.Result.Metrics["backlog_offline_month_end"].Status)
	var kind int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT kind FROM hosted_account_reporting_allocation_snapshot WHERE interval_id=$1 AND asset_id=$2`, input.Coverage[0].AllocationIntervalID, board).Scan(&kind))
	require.Equal(t, 7, kind)
	require.Len(t, frozenReportEvidence(t, f, draft.ID), 1)
	reviewed, err := f.svc.HostReport(t.Context(), reviewer, f.org, client.AccountID, draft.ReportID)
	require.NoError(t, err)
	issued, err := f.svc.PublishReport(t.Context(), reviewer, f.org, client.AccountID, draft.ReportID, reviewed.Revision, "Reviewed board evidence; module repair mapping and contractual formulas remain unavailable")
	require.NoError(t, err)
	before := downloadReportArtifact(t, f, f.user, client.AccountID, issued.ReportID, "evidence.zip")
	reportEvidenceHours(t, f.db, f, input.Coverage[0].AllocationIntervalID, observed, 1, 2, 200e12)
	after := downloadReportArtifact(t, f, f.user, client.AccountID, issued.ReportID, "evidence.zip")
	require.Equal(t, before, after)
	got, err := f.svc.AccountReport(t.Context(), f.user, f.org, client.AccountID, issued.ReportID)
	require.NoError(t, err)
	require.Equal(t, "100000000000000", got.Result.Metrics["hashrate_performance_hs"].Value)
}
