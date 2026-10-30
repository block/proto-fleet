package hostedaccounts_test

import (
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/stretchr/testify/require"
)

func TestHashboardControllerMoveRetainsAcceptedHistory(t *testing.T) {
	for _, scenario := range []string{"existing owner", "claim predates allocation", "closed allocation period"} {
		t.Run(scenario, func(t *testing.T) {
			f := setup(t)
			client := f.client(t, "Client", f.user, hostedaccounts.AllCapabilities)
			source, _ := f.miningModule(t)
			destination, controller := f.miningModule(t)
			board := f.moduleSelection(t, source).Boards[0].Id
			require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(7, board, 0)))
			captured := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Minute)
			ownershipStart := captured.Add(-time.Minute)
			delayed := scenario != "existing owner"
			if delayed {
				ownershipStart = captured.Add(30 * time.Second)
			}
			_, err := f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1 WHERE kind=7 AND asset_id=$2`, ownershipStart, board)
			require.NoError(t, err)
			boundary := captured
			if delayed {
				boundary = ownershipStart
			}
			if scenario == "closed allocation period" {
				// Controller deletion closes reporting history but retains ownership.
				boundary = ownershipStart.Add(5 * time.Second)
				_, err = f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET ended_at=$1 WHERE kind=7 AND asset_id=$2`, boundary, board)
				require.NoError(t, err)
			}
			tx, err := f.db.BeginTx(t.Context(), nil)
			require.NoError(t, err)
			defer tx.Rollback()
			q := sqlc.New(tx)
			// Reconcile a move at its captured time, before this transaction runs.
			_, err = tx.ExecContext(t.Context(), `UPDATE hashboard_installation SET installed_at=$1,last_seen_at=$1,removed_at=$2 WHERE hashboard_id=$3 AND removed_at IS NULL`, captured.Add(-time.Minute), captured, board)
			require.NoError(t, err)
			_, err = tx.ExecContext(t.Context(), `UPDATE hashboard_installation SET removed_at=clock_timestamp() WHERE module_id=$1 AND position=1 AND removed_at IS NULL`, destination)
			require.NoError(t, err)
			_, err = tx.ExecContext(t.Context(), `UPDATE hashboard_observation SET last_seen_at=$1,released_at=$2 WHERE hashboard_id=$3`, captured.Add(-2*time.Minute), captured.Add(-time.Second), board)
			require.NoError(t, err)
			claim, err := json.Marshal([]map[string]any{{"board_id": board, "occupancy": "occupied", "source_at": captured, "module_id": destination, "position": 1}})
			require.NoError(t, err)
			require.NoError(t, q.UpsertHashboardObservations(t.Context(), sqlc.UpsertHashboardObservationsParams{DeviceID: controller, ObservedAt: captured, Boards: claim}))
			require.NoError(t, q.UpsertHashboardInstallations(t.Context(), sqlc.UpsertHashboardInstallationsParams{ObservedAt: captured, Boards: claim}))
			writeSample := func(at time.Time, hash float64) {
				t.Helper()
				data, err := json.Marshal([]map[string]any{{"board_id": board, "module_id": destination, "position": 1, "time": at, "state": "active", "state_observed_at": at, "hashrate_hs": hash, "power_w": 200}})
				require.NoError(t, err)
				require.NoError(t, q.InsertHashboardMetrics(t.Context(), sqlc.InsertHashboardMetricsParams{CollectedAt: at, Boards: data}))
				require.NoError(t, q.InsertHashboardDiagnosticObservations(t.Context(), sqlc.InsertHashboardDiagnosticObservationsParams{CollectedAt: at, Boards: data}))
			}
			writeSample(captured, 100e12)
			if delayed {
				// A later sample belongs to the new owner; the captured one does not.
				later := boundary.Add(time.Second)
				_, err = tx.ExecContext(t.Context(), `UPDATE hashboard_installation SET last_seen_at=$1 WHERE hashboard_id=$2 AND removed_at IS NULL`, later, board)
				require.NoError(t, err)
				writeSample(later, 200e12)
			}
			var started, ended time.Time
			require.NoError(t, tx.QueryRowContext(t.Context(), `SELECT started_at FROM hosted_account_asset_interval WHERE kind=7 AND asset_id=$1 AND ended_at IS NULL`, board).Scan(&started))
			require.NoError(t, tx.QueryRowContext(t.Context(), `SELECT ended_at FROM hosted_account_asset_interval WHERE kind=7 AND asset_id=$1 AND ended_at IS NOT NULL`, board).Scan(&ended))
			require.Equal(t, boundary, started.UTC())
			require.Equal(t, boundary, ended.UTC())
			var samples int
			require.NoError(t, tx.QueryRowContext(t.Context(), `SELECT count(*) FROM hashboard_metrics WHERE hashboard_id=$1 AND time=$2`, board, captured).Scan(&samples))
			require.Equal(t, 1, samples, "the telemetry query must accept the captured move sample")
			require.NoError(t, tx.Commit())
			report, err := f.svc.Dashboard(t.Context(), f.user, f.org, dashboardRequest(client.AccountID, captured.Add(-time.Minute), captured.Add(time.Minute), nil))
			require.NoError(t, err)
			var hashrates []float64
			var hashing int32
			for _, reading := range report.Readings {
				if reading.Summary.HashrateHs != nil {
					hashrates = append(hashrates, reading.Summary.GetHashrateHs())
				}
				hashing += reading.UptimeStatus.GetHashingCount()
			}
			if delayed {
				require.Equal(t, []float64{200e12}, hashrates, "the delayed claim cannot expose readings before allocation")
			} else {
				require.Equal(t, []float64{100e12}, hashrates, "processing time cannot discard the accepted move sample")
			}
			require.EqualValues(t, 1, hashing)
		})
	}
}

func TestHashboardControllerMovePreservesExplicitAllocationBoundary(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Client", f.user, hostedaccounts.AllCapabilities)
	source, sourceController := f.miningModule(t)
	_, controller := f.miningModule(t)
	boards := f.moduleSelection(t, source).Boards
	board, other := boards[0].Id, boards[1].Id
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(7, board, 0)))
	captured := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Minute)
	_, err := f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1 WHERE kind=7 AND asset_id=$2`, captured.Add(-time.Minute), board)
	require.NoError(t, err)
	tx, err := f.db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	defer tx.Rollback()
	manual := captured.Add(30 * time.Second)
	_, err = tx.ExecContext(t.Context(), `SELECT set_config('fleet.allocation_boundary',$1,true)`, manual.Format(time.RFC3339Nano))
	require.NoError(t, err)
	_, err = tx.ExecContext(t.Context(), `UPDATE hashboard_observation SET device_id=$1,last_seen_at=$2,attribution_since=$2 WHERE hashboard_id=$3`, controller, captured, board)
	require.NoError(t, err)
	var started, setting time.Time
	require.NoError(t, tx.QueryRowContext(t.Context(), `SELECT started_at FROM hosted_account_asset_interval WHERE kind=7 AND asset_id=$1 AND ended_at IS NULL`, board).Scan(&started))
	require.Equal(t, captured, started.UTC(), "a physical move uses its accepted time even within an explicit allocation transaction")
	require.NoError(t, tx.QueryRowContext(t.Context(), `SELECT current_setting('fleet.allocation_boundary')::timestamptz`).Scan(&setting))
	require.Equal(t, manual, setting.UTC())
	_, err = sqlc.New(tx).SetHostedAccountAsset(t.Context(), sqlc.SetHostedAccountAssetParams{OrganizationID: f.org, Kind: 7, AssetID: other, DeviceID: sourceController, HashboardID: sql.NullInt64{Int64: other, Valid: true}, ClientID: sql.NullInt64{Int64: client.ID, Valid: true}})
	require.NoError(t, err)
	require.NoError(t, tx.QueryRowContext(t.Context(), `SELECT started_at FROM hosted_account_asset_interval WHERE kind=7 AND asset_id=$1 AND ended_at IS NULL`, other).Scan(&started))
	require.Equal(t, manual, started.UTC(), "the move must not change the boundary of a later explicit allocation")
}
