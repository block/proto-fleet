package hostedaccounts_test

import (
	"context"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts/reporting"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func currentBoardReportingInputs(t *testing.T, f fixture, account string) (reporting.Inputs, int64, int64) {
	t.Helper()
	module, controller := f.miningModule(t)
	var site, board, interval int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO site(org_id,name,slug) VALUES($1,$2,$2) RETURNING id`, f.org, uuid.NewString()).Scan(&site))
	_, err := f.db.ExecContext(t.Context(), `UPDATE device SET site_id=$1 WHERE id=$2`, site, controller)
	require.NoError(t, err)
	f.observeModuleAtCurrentSite(t, module, controller)
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT hashboard_id FROM hashboard_installation WHERE module_id=$1 AND position=1 AND removed_at IS NULL`, module).Scan(&board))
	require.NoError(t, f.allocate(t, f.host, f.root, account, 1, selected(7, board, 0)))
	var start, end time.Time
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT id,started_at,clock_timestamp() FROM hosted_account_asset_interval WHERE kind=7 AND asset_id=$1 AND ended_at IS NULL`, board).Scan(&interval, &start, &end))
	input := reportingInputs(t, f, account, f.host)
	input.Orders = []reporting.Order{{ID: "order", Name: "Reviewed board order", Start: start, SourceRef: "signed order"}}
	input.Coverage = []reporting.CoverageSegment{{ID: "board-coverage", AllocationIntervalID: interval, Kind: 7, AssetID: board, SiteID: site, ContainerID: controller, OrderID: "order", Start: start, End: end, CapacityTHS: "100", SourceRef: "current board allocation"}}
	input.Hashrates = []reporting.HashrateInterval{{ID: "board-hash", Kind: 7, AssetID: board, SiteID: site, ContainerID: controller, OrderID: "order", Start: start, End: end, HashrateHS: "100000000000000", SourceID: "reviewed-source", SourceRef: "board telemetry worksheet", Complete: true}}
	input.PoolObservations = []reporting.PoolObservation{{ID: "board-pool", Kind: 7, AssetID: board, SourceAt: start, ReceivedAt: end, State: "unknown", SourceID: "pool-source", SourceRef: "pool worksheet"}}
	return input, module, board
}

func TestReportingInputsSaveCurrentHashboardAllocations(t *testing.T) {
	for _, subclient := range []bool{false, true} {
		t.Run(map[bool]string{false: "client", true: "subclient"}[subclient], func(t *testing.T) {
			f := setup(t)
			client := f.client(t, "Board owner", f.user, hostedaccounts.AllCapabilities)
			input, _, board := currentBoardReportingInputs(t, f, client.AccountID)
			account := client
			if subclient {
				child, err := f.svc.CreateChild(t.Context(), f.user, f.org, client.AccountID, "Subclient", f.user, hostedaccounts.AllCapabilities)
				require.NoError(t, err)
				require.NoError(t, f.allocate(t, f.user, client.AccountID, child.AccountID, 1, selected(7, board, 1)))
				account = child.Account
				require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT id,started_at,clock_timestamp() FROM hosted_account_asset_interval WHERE kind=7 AND asset_id=$1 AND ended_at IS NULL`, board).Scan(&input.Coverage[0].AllocationIntervalID, &input.Coverage[0].Start, &input.Coverage[0].End))
				c := input.Coverage[0]
				input.Orders[0].Start = c.Start
				input.Hashrates[0].Start = c.Start
				input.Hashrates[0].End = c.End
				input.PoolObservations[0].SourceAt = c.Start
				input.PoolObservations[0].ReceivedAt = c.End
			}
			reviewer, _ := reportingBoundaryActors(t, f, account.AccountID)
			saved, err := saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, account.AccountID, 0, "current board sources", input)
			require.NoError(t, err)
			got, err := reportingInputsForTest(f.svc, t.Context(), reviewer, f.org, account.AccountID)
			require.NoError(t, err)
			require.Positive(t, got.UpdatedBy)
			require.Equal(t, saved.Revision, got.Revision)
			require.EqualValues(t, 7, got.Inputs.Coverage[0].Kind)
			require.Equal(t, board, got.Inputs.Coverage[0].AssetID)
			require.Len(t, got.Inputs.Hashrates, 1)
			require.Len(t, got.Inputs.PoolObservations, 1)
			var legacy int
			require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_account_asset_interval WHERE kind=6 AND organization_id=$1`, f.org).Scan(&legacy))
			require.Zero(t, legacy, "current board fixtures must not reconstruct legacy module history")
		})
	}
}

func TestReportingHashboardInputsRejectForeignOwnerAndWrongHistoricalContainer(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Board owner", f.user, hostedaccounts.AllCapabilities)
	_, _ = reportingBoundaryActors(t, f, client.AccountID)
	input, module, _ := currentBoardReportingInputs(t, f, client.AccountID)
	_, otherController := f.miningModule(t)
	_, err := f.db.ExecContext(t.Context(), `UPDATE device SET site_id=$1 WHERE id=$2`, input.Coverage[0].SiteID, otherController)
	require.NoError(t, err)
	wrong := input
	wrong.Coverage = append([]reporting.CoverageSegment(nil), input.Coverage...)
	wrong.Coverage[0].ContainerID = otherController
	_, err = saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, client.AccountID, 0, "wrong controller", wrong)
	require.ErrorContains(t, err, "allocation interval")
	foreign := f.client(t, "Other owner", f.user, hostedaccounts.AllCapabilities)
	var foreignBoard, foreignInterval int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT hashboard_id FROM hashboard_installation WHERE module_id=$1 AND position=2 AND removed_at IS NULL`, module).Scan(&foreignBoard))
	require.NoError(t, f.allocate(t, f.host, f.root, foreign.AccountID, 1, selected(7, foreignBoard, 0)))
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT id FROM hosted_account_asset_interval WHERE kind=7 AND asset_id=$1 AND ended_at IS NULL`, foreignBoard).Scan(&foreignInterval))
	foreignInput := input
	foreignInput.Coverage = append([]reporting.CoverageSegment(nil), input.Coverage...)
	foreignInput.Coverage[0].AssetID = foreignBoard
	foreignInput.Coverage[0].AllocationIntervalID = foreignInterval
	_, err = saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, client.AccountID, 0, "foreign board in shared container", foreignInput)
	require.ErrorContains(t, err, "account access denied")
	// The correct retained controller remains valid after its live placement changes.
	_, err = f.db.ExecContext(t.Context(), `UPDATE device SET site_id=NULL WHERE id=$1`, input.Coverage[0].ContainerID)
	require.NoError(t, err)
	_, err = saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, client.AccountID, 0, "retained placement", input)
	require.NoError(t, err)
}

func TestReportingHashboardTransferCannotRewritePriorCustomerEvidence(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Original owner", f.user, hostedaccounts.AllCapabilities)
	other := f.client(t, "New owner", f.user, hostedaccounts.AllCapabilities)
	_, _ = reportingBoundaryActors(t, f, client.AccountID)
	reportingBoundaryActors(t, f, other.AccountID)
	input, _, board := currentBoardReportingInputs(t, f, client.AccountID)
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 2, selected(7, board, 1)))
	require.NoError(t, f.allocate(t, f.host, f.root, other.AccountID, 1, selected(7, board, 2)))
	_, err := saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, client.AccountID, 0, "historical owner", input)
	require.NoError(t, err)
	_, err = saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, other.AccountID, 0, "cannot inherit old facts", input)
	require.Error(t, err)
	input.Coverage[0].End = time.Now().UTC()
	_, err = saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, client.AccountID, 1, "extends past transfer", input)
	require.ErrorContains(t, err, "allocation interval")
}

func TestReportingReadCallsShareOrganizationAdmissionLocks(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Read sharing", f.user, hostedaccounts.AllCapabilities)
	reportingBoundaryActors(t, f, client.AccountID)
	_, err := saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, client.AccountID, 0, "empty reviewed setup", reportingInputs(t, f, client.AccountID, f.host))
	require.NoError(t, err)
	tx, err := f.db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	defer func() { require.NoError(t, tx.Rollback()) }()
	var id int64
	require.NoError(t, tx.QueryRowContext(t.Context(), `SELECT id FROM organization WHERE id=$1 FOR SHARE`, f.org).Scan(&id))
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	_, err = reportingInputsForTest(f.svc, ctx, f.host, f.org, client.AccountID)
	require.NoError(t, err)
	_, err = f.svc.HostReportingAccounts(ctx, f.host, f.org)
	require.NoError(t, err)
	_, err = f.svc.HostReportingSetup(ctx, f.host, f.org, client.AccountID)
	require.NoError(t, err)
}
