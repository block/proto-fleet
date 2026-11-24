package hostedaccounts_test

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts/reporting"
	"github.com/stretchr/testify/require"
)

// The hourly rows below are synthetic captured evidence, not a test of the
// hourly job. Preparation freezes them by its cutoff and publication replays them.
func TestHostedReportPublicEvidenceFrozenBoundaryHours(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Boundary evidence client", f.user, hostedaccounts.AllCapabilities)
	owner, reviewer := reportActors(t, f, client.AccountID)
	miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	var site int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO site(org_id,name,slug) VALUES($1,'Boundary site','boundary-site') RETURNING id`, f.org).Scan(&site))
	_, err := f.db.ExecContext(t.Context(), `UPDATE device SET site_id=$1 WHERE id=$2`, site, miner)
	require.NoError(t, err)
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(5, miner, 0)))
	start := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, 0)
	historyStart := start.Add(-24 * time.Hour)
	historyEpoch(t, f, historyStart)
	_, err = f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1 WHERE client_id=$2`, historyStart, client.ID)
	require.NoError(t, err)
	var interval int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT id FROM hosted_account_asset_interval WHERE client_id=$1`, client.ID).Scan(&interval))

	// PostgreSQL rounds these distinct original 400ns boundaries to the same
	// microsecond as the start of the report. Selection must conservatively
	// retain both adjacent coverage segments for the boundary-spanning hour.
	boundary := start.Add(400 * time.Nanosecond)
	input := reportingInputs(t, f, client.AccountID, f.host)
	input.Orders = []reporting.Order{{ID: "order", Name: "order", Start: historyStart, SourceRef: "signed order"}}
	input.Coverage = []reporting.CoverageSegment{
		{ID: "earliest", AllocationIntervalID: interval, Kind: 5, AssetID: miner, SiteID: site, ContainerID: miner, OrderID: "order", Start: historyStart, End: start.Add(-2 * time.Hour), CapacityTHS: "100", SourceRef: "pre-period commercial context"},
		{ID: "before", AllocationIntervalID: interval, Kind: 5, AssetID: miner, SiteID: site, ContainerID: miner, OrderID: "order", Start: start.Add(-2 * time.Hour), End: boundary, CapacityTHS: "100", SourceRef: "earlier coverage"},
		{ID: "after", AllocationIntervalID: interval, Kind: 5, AssetID: miner, SiteID: site, ContainerID: miner, OrderID: "order", Start: boundary, CapacityTHS: "100", SourceRef: "later coverage"},
	}
	savedReportInputs(t, f, client.AccountID, reviewer, input, 0)
	reportEvidenceHours(t, f.db, f, interval, start, 2, 1, 10)
	period := reporting.Period{Kind: "monthly", Start: start, End: end, AsOf: reportAsOf(t, f), Timezone: "UTC"}
	draft, err := f.svc.PrepareReport(t.Context(), f.host, f.org, client.AccountID, hostedaccounts.DraftReportOptions{Period: period, InputRevision: 1, OwnerUserID: owner})
	require.NoError(t, err)
	frozen := frozenReportEvidence(t, f, draft.ID)
	require.Equal(t, []string{
		fmt.Sprintf("%d %s closed 1", interval, start.Format(time.RFC3339)),
		fmt.Sprintf("%d %s closed 1", interval, start.Add(time.Hour).Format(time.RFC3339)),
	}, frozen)
	// A revision captured after the draft's cutoff cannot join it.
	reportEvidenceHours(t, f.db, f, interval, start, 1, 2, 20)
	require.Equal(t, frozen, frozenReportEvidence(t, f, draft.ID))
	issued, err := f.svc.PublishReport(t.Context(), reviewer, f.org, client.AccountID, draft.ReportID, draft.Revision, "reviewed incomplete sources")
	require.NoError(t, err)
	members := scopedPublicationMembers(t, downloadReportArtifact(t, f, f.user, client.AccountID, issued.ReportID, "evidence.zip"))
	coverage, _ := scopedPublicationTable(t, members, "coverage")
	require.Contains(t, coverage, "before")
	require.Contains(t, coverage, "after")
	var hostSource struct {
		Inputs reporting.Inputs `json:"inputs"`
	}
	require.NoError(t, json.Unmarshal(downloadReportArtifact(t, f, reviewer, client.AccountID, issued.ReportID, "source-inputs.json"), &hostSource))
	hostCoverage := map[string]reporting.CoverageSegment{}
	for _, c := range hostSource.Inputs.Coverage {
		hostCoverage[c.ID] = c
	}
	require.Equal(t, input.Coverage[1], hostCoverage["before"], "host source retains exact original boundary coverage")
	require.NotContains(t, hostCoverage, "earliest", "coverage ending before the period supports no frozen evidence")
	observations, err := csv.NewReader(bytes.NewReader(members["observations.csv"])).ReadAll()
	require.NoError(t, err)
	require.Equal(t, []string{"1", "1"}, observationColumn(t, observations, "revision"), "the ZIP must replay exactly the frozen hours")
	require.Equal(t, []string{"10", "10"}, observationColumn(t, observations, "hashrate_sum_hs"))
}
