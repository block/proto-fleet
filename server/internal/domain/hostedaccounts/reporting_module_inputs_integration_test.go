package hostedaccounts_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts/reporting"
	"github.com/stretchr/testify/require"
)

func reviewedModuleInputs(t *testing.T, f fixture, account string) reporting.Inputs {
	t.Helper()
	in, module, _ := currentBoardReportingInputs(t, f, account)
	rows, err := f.db.QueryContext(t.Context(), `SELECT hashboard_id FROM hashboard_installation WHERE module_id=$1 AND position>1 AND removed_at IS NULL ORDER BY position`, module)
	require.NoError(t, err)
	defer func() { require.NoError(t, rows.Close()) }()
	var boards []int64
	for rows.Next() {
		var board int64
		require.NoError(t, rows.Scan(&board))
		boards = append(boards, board)
	}
	require.NoError(t, rows.Err())
	require.NoError(t, rows.Close())
	for _, board := range boards {
		require.NoError(t, f.allocate(t, f.host, f.root, account, 1, selected(7, board, 0)))
		c := in.Coverage[0]
		c.ID = fmt.Sprintf("board-%d", board)
		c.AssetID = board
		require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT id,started_at FROM hosted_account_asset_interval WHERE kind=7 AND asset_id=$1 AND ended_at IS NULL`, board).Scan(&c.AllocationIntervalID, &c.Start))
		in.Coverage = append(in.Coverage, c)
	}
	start := in.Coverage[len(in.Coverage)-1].Start.UTC()
	var end time.Time
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT clock_timestamp()`).Scan(&end))
	end = end.UTC()
	refs := []string{}
	for i := range in.Coverage {
		in.Coverage[i].Start = start
		in.Coverage[i].End = end
		refs = append(refs, in.Coverage[i].ID)
	}
	in.Hashrates = nil
	in.PoolObservations = nil
	c := in.Coverage[0]
	in.ModuleCoverage = []reporting.ModuleCoverageSegment{{ID: "module", ModuleID: module, SiteID: c.SiteID, ContainerID: c.ContainerID, OrderID: c.OrderID, Start: start, End: end, BoardCoverageIDs: refs, WholeModule: true, SourceRef: "reviewed module membership"}}
	in.Obligations = []reporting.ObligationInterval{{ID: "episode", EpisodeID: "repair", Kind: 6, AssetID: module, SiteID: c.SiteID, ContainerID: c.ContainerID, OrderID: c.OrderID, Start: start, AsOf: end, ReceivedAt: end, Qualified: true, SourceRef: "reviewed repair record"}}
	return in
}

func TestReportingModuleCoverageUsesHistoricalMembershipAndOwnership(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Module owner", f.user, hostedaccounts.AllCapabilities)
	reviewer, _ := reportingBoundaryActors(t, f, client.AccountID)
	in := reviewedModuleInputs(t, f, client.AccountID)
	// Retiring placement and transferring boards later must not rewrite the proven window.
	_, err := f.db.ExecContext(t.Context(), `UPDATE hashboard_installation SET removed_at=$1 WHERE module_id=$2 AND removed_at IS NULL`, in.ModuleCoverage[0].End, in.ModuleCoverage[0].ModuleID)
	require.NoError(t, err)
	_, err = saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, client.AccountID, 0, "historical mapping", in)
	require.NoError(t, err)
	got, err := reportingInputsForTest(f.svc, t.Context(), reviewer, f.org, client.AccountID)
	require.NoError(t, err)
	require.Equal(t, in.ModuleCoverage, got.Inputs.ModuleCoverage)
	foreign := f.client(t, "Other owner", f.user, hostedaccounts.AllCapabilities)
	reportingBoundaryActors(t, f, foreign.AccountID)
	_, err = saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, foreign.AccountID, 0, "foreign mapping", in)
	require.Error(t, err)
}

func TestReportingModuleCoverageRejectsUnprovenHistory(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*testing.T, fixture, *reporting.Inputs)
	}{
		{"wrong_module", func(t *testing.T, f fixture, in *reporting.Inputs) {
			module, _ := f.miningModule(t)
			in.ModuleCoverage[0].ModuleID = module
			in.Obligations[0].AssetID = module
		}},
		{"missing_site_history", func(t *testing.T, f fixture, in *reporting.Inputs) {
			_, err := f.db.ExecContext(t.Context(), `UPDATE hashboard_installation SET site_id=NULL WHERE hashboard_id=$1`, in.Coverage[0].AssetID)
			require.NoError(t, err)
		}},
		{"removed_mid_window", func(t *testing.T, f fixture, in *reporting.Inputs) {
			m := in.ModuleCoverage[0]
			_, err := f.db.ExecContext(t.Context(), `UPDATE hashboard_installation SET removed_at=$1 WHERE hashboard_id=$2`, m.Start.Add(m.End.Sub(m.Start)/2), in.Coverage[0].AssetID)
			require.NoError(t, err)
		}},
		{"ambiguous_history", func(t *testing.T, f fixture, in *reporting.Inputs) {
			m := in.ModuleCoverage[0]
			_, err := f.db.ExecContext(t.Context(), `INSERT INTO hashboard_installation(hashboard_id,module_id,position,site_id,device_id,tank_id,module_position,installed_at,last_seen_at,removed_at) SELECT hashboard_id,module_id,position,site_id,device_id,tank_id,module_position,$1,$2,$2 FROM hashboard_installation WHERE hashboard_id=$3 AND removed_at IS NULL`, m.Start, m.End, in.Coverage[0].AssetID)
			require.NoError(t, err)
		}},
		{"ownership_window", func(t *testing.T, f fixture, in *reporting.Inputs) {
			m := &in.ModuleCoverage[0]
			m.Start = m.Start.Add(-time.Hour)
		}},
		{"duplicate_reference", func(t *testing.T, f fixture, in *reporting.Inputs) {
			in.ModuleCoverage[0].BoardCoverageIDs[2] = in.ModuleCoverage[0].BoardCoverageIDs[0]
		}},
		{"wrong_order", func(t *testing.T, f fixture, in *reporting.Inputs) { in.ModuleCoverage[0].OrderID = "other" }},
		{"obligation_beyond_mapping", func(t *testing.T, f fixture, in *reporting.Inputs) {
			in.Obligations[0].AsOf = in.ModuleCoverage[0].End.Add(time.Minute)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := setup(t)
			client := f.client(t, "Unproven mapping", f.user, hostedaccounts.AllCapabilities)
			reportingBoundaryActors(t, f, client.AccountID)
			in := reviewedModuleInputs(t, f, client.AccountID)
			tc.edit(t, f, &in)
			_, err := saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, client.AccountID, 0, "invalid mapping", in)
			require.Error(t, err)
		})
	}
}

func TestReportingPartialModuleCoverageSupportsReviewedRepairRecords(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Partial owner", f.user, hostedaccounts.AllCapabilities)
	reportingBoundaryActors(t, f, client.AccountID)
	in := reviewedModuleInputs(t, f, client.AccountID)
	in.Coverage = in.Coverage[:1]
	in.ModuleCoverage[0].BoardCoverageIDs = in.ModuleCoverage[0].BoardCoverageIDs[:1]
	in.ModuleCoverage[0].WholeModule = false
	_, err := saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, client.AccountID, 0, "partial association", in)
	require.NoError(t, err)
	in.ModuleCoverage[0].WholeModule = true
	_, err = saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, client.AccountID, 1, "unsupported whole module", in)
	require.ErrorContains(t, err, "module coverage")
}

func TestReportingModuleInstallationValidationBatches(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Batched module history", f.user, hostedaccounts.AllCapabilities)
	reportingBoundaryActors(t, f, client.AccountID)
	in := reviewedModuleInputs(t, f, client.AccountID)
	base := in.ModuleCoverage[0]
	// Split one proven placement into 251 adjacent reviewed segments, so each
	// claim remains independently valid across the 250-module batch boundary.
	start := base.Start.Add(time.Microsecond)
	end := start.Add(251 * time.Microsecond)
	for i := range in.Coverage {
		in.Coverage[i].Start = start
		in.Coverage[i].End = end
	}
	in.ModuleCoverage = nil
	in.Obligations = nil
	for i := range 251 {
		segment := base
		segment.ID = fmt.Sprintf("module-batch-%d", i)
		segment.Start = start.Add(time.Duration(i) * time.Microsecond)
		segment.End = segment.Start.Add(time.Microsecond)
		in.ModuleCoverage = append(in.ModuleCoverage, segment)
	}
	_, err := f.db.ExecContext(t.Context(), `UPDATE hashboard_installation SET removed_at=$1 WHERE module_id=$2 AND removed_at IS NULL`, end, base.ModuleID)
	require.NoError(t, err)
	saved, err := saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, client.AccountID, 0, "batched module evidence", in)
	require.NoError(t, err)
	// Drift affecting only the last mapping must reject the next bounded operation.
	_, err = f.db.ExecContext(t.Context(), `UPDATE hashboard_installation SET removed_at=$1 WHERE module_id=$2 AND position=1`, in.ModuleCoverage[250].Start, base.ModuleID)
	require.NoError(t, err)
	_, err = saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, client.AccountID, saved.Revision, "invalid last batch", in)
	require.ErrorContains(t, err, "unambiguous historical board installations")
	got, err := reportingInputsForTest(f.svc, t.Context(), f.host, f.org, client.AccountID)
	require.NoError(t, err)
	require.EqualValues(t, saved.Revision, got.Revision)
}

func TestReportingModuleInstallationCandidatePagesPreserveEachMappingWindow(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Paged installations", f.user, hostedaccounts.AllCapabilities)
	reportingBoundaryActors(t, f, account.AccountID)
	input := reviewedModuleInputs(t, f, account.AccountID)
	base := input.ModuleCoverage[0]
	start := base.Start.Add(time.Microsecond)
	end := start.Add(4 * time.Millisecond)
	for i := range input.Coverage {
		input.Coverage[i].Start, input.Coverage[i].End = start, end
	}
	first, last := base, base
	first.ID, first.Start, first.End = "first", start, start.Add(time.Millisecond)
	last.ID, last.Start, last.End = "last", start.Add(3*time.Millisecond), end
	input.ModuleCoverage = []reporting.ModuleCoverageSegment{first, last}
	input.Obligations = nil
	// These candidates lie in the gap between mappings. A batched query may
	// fetch them, but each original window must decide their relevance.
	_, err := f.db.ExecContext(t.Context(), `INSERT INTO hashboard_installation(hashboard_id,module_id,position,site_id,device_id,tank_id,module_position,installed_at,last_seen_at,removed_at)
 SELECT hashboard_id,module_id,position,site_id,device_id,tank_id,module_position,$1,$2,$2 FROM hashboard_installation CROSS JOIN generate_series(1,207) WHERE hashboard_id=$3 AND removed_at IS NULL`, start.Add(1500*time.Microsecond), start.Add(2500*time.Microsecond), input.Coverage[0].AssetID)
	require.NoError(t, err)
	saved, err := saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, account.AccountID, 0, "separate mapping windows", input)
	require.NoError(t, err)
	// A conflicting partial installation after the first candidate page does
	// not qualify as selected membership; the second scan must still catch it.
	_, err = f.db.ExecContext(t.Context(), `INSERT INTO hashboard_installation(hashboard_id,module_id,position,site_id,device_id,tank_id,module_position,installed_at,last_seen_at,removed_at)
 SELECT hashboard_id,module_id,position,site_id,device_id,tank_id,module_position,$1,$2,$2 FROM hashboard_installation WHERE hashboard_id=$3 AND removed_at IS NULL`, last.Start.Add(time.Microsecond), last.End.Add(-time.Microsecond), input.Coverage[0].AssetID)
	require.NoError(t, err)
	_, err = f.svc.MutateReportingInputs(t.Context(), f.host, f.org, account.AccountID, saved.Revision, "late conflicting installation", nil, []reporting.FactMutation{reportingUpsertForTest(t, reporting.Order{ID: "unrelated", Name: "Unrelated", Start: start, SourceRef: "signed"})})
	require.ErrorContains(t, err, `module coverage "last" requires unambiguous historical board installations`)
}
