package hostedaccounts_test

import (
	"archive/zip"
	"bytes"
	"io"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/stretchr/testify/require"
)

func TestHostedReportReviewedModuleMappingPublishesAndStaysFrozen(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Module reports", f.user, hostedaccounts.AllCapabilities)
	owner, reviewer := reportActors(t, f, client.AccountID)
	in := reviewedModuleInputs(t, f, client.AccountID)
	p := reportPeriod()
	historyEpoch(t, f, p.Start)
	for i := range in.Coverage {
		c := &in.Coverage[i]
		_, err := f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1 WHERE id=$2`, p.Start, c.AllocationIntervalID)
		require.NoError(t, err)
		_, err = f.db.ExecContext(t.Context(), `UPDATE hashboard_installation SET installed_at=$1 WHERE hashboard_id=$2`, p.Start, c.AssetID)
		require.NoError(t, err)
		c.Start = p.Start
		c.End = p.End
	}
	in.Orders[0].Start = p.Start
	in.ModuleCoverage[0].Start = p.Start
	in.ModuleCoverage[0].End = p.End
	in.Obligations[0].Start = p.Start
	in.Obligations[0].AsOf = p.End
	in.Obligations[0].ReceivedAt = p.End
	savedReportInputs(t, f, client.AccountID, reviewer, in, 0)
	p.AsOf = time.Now().UTC()
	draft, err := f.svc.PrepareReport(t.Context(), f.host, f.org, client.AccountID, hostedaccounts.DraftReportOptions{Period: p, InputRevision: 1, OwnerUserID: owner})
	require.NoError(t, err)
	require.Equal(t, "1", draft.Result.Metrics["offline_units"].Value)
	issued, err := f.svc.PublishReport(t.Context(), reviewer, f.org, client.AccountID, draft.ReportID, draft.Revision, "Reviewed mapping and outage; remaining contractual definitions unapproved")
	require.NoError(t, err)
	before := downloadReportArtifact(t, f, f.user, client.AccountID, issued.ReportID, "evidence.zip")
	archive, err := zip.NewReader(bytes.NewReader(before), int64(len(before)))
	require.NoError(t, err)
	found := false
	for _, file := range archive.File {
		if file.Name != "inputs/module_coverage.csv" {
			continue
		}
		found = true
		content, err := file.Open()
		require.NoError(t, err)
		data, err := io.ReadAll(content)
		require.NoError(t, err)
		require.NoError(t, content.Close())
		require.Contains(t, string(data), "reviewed module membership")
		require.Contains(t, string(data), "board_coverage_ids")
	}
	require.True(t, found, "module attribution must accompany issued counts")
	in.ModuleCoverage[0].WholeModule = false
	savedReportInputs(t, f, client.AccountID, reviewer, in, 1)
	after := downloadReportArtifact(t, f, f.user, client.AccountID, issued.ReportID, "evidence.zip")
	require.Equal(t, before, after, "later attribution changes must not rewrite published evidence")
}
