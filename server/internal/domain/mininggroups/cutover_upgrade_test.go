package mininggroups

import (
	"database/sql"
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/block/proto-fleet/server/migrations"
	"github.com/stretchr/testify/require"
)

func reapplyCutoverMigration(t *testing.T, conn *sql.DB, orgID int64) []byte {
	t.Helper()
	ctx := t.Context()
	_, err := conn.ExecContext(ctx, `DELETE FROM pool_configuration_board_intent WHERE organization_id=$1`, orgID)
	require.NoError(t, err)
	down, err := migrations.Current.ReadFile("current/001056_internal_pool_configuration_cutover.down.sql")
	require.NoError(t, err)
	up, err := migrations.Current.ReadFile("current/001056_internal_pool_configuration_cutover.up.sql")
	require.NoError(t, err)
	tx, err := conn.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, string(down))
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, string(up))
	require.NoError(t, err)
	require.NoError(t, tx.Commit())
	return down
}

func TestCutoverUpgradeBackfillsOnlyVerifiedBoardEffectsWithoutDeviceWrites(t *testing.T) {
	for _, outcome := range []string{"verified", "failed"} {
		t.Run(outcome, func(t *testing.T) {
			ctx, f := newIdleRetryFixture(t)
			service, saved, firmware := newConfirmedSharedBoardApplication(t, ctx, f)
			require.NoError(t, f.service.ExecutePoolConfiguration(ctx, f.target.OrganizationID, saved.Operation.ID, f.target.DeviceID))
			if outcome == "verified" {
				for iteration := 0; iteration < 30 && active(f.state(t).Status); iteration++ {
					f.reconcile(t, ctx)
				}
				require.Equal(t, "complete", f.state(t).Status)
			} else {
				// Admission created immutable execution material, but recovery
				// explicitly ended the request before a firmware write succeeded.
				_, err := f.service.writes.ResolveOffline(ctx, recoveryEvidence(t, ctx, f), true)
				require.NoError(t, err)
				require.Equal(t, "failed", f.state(t).Status)
			}
			operation, err := service.GetOperation(ctx, "", saved.Operation.ID)
			require.NoError(t, err)
			require.Len(t, operation.Targets, 1)
			require.Equal(t, outcome, operation.Targets[0].State)
			work, err := f.service.queries.GetPoolConfigurationWork(ctx, sqlc.GetPoolConfigurationWorkParams{OrganizationID: f.target.OrganizationID, OperationID: saved.Operation.ID, TargetKind: operation.Targets[0].Kind, TargetID: operation.Targets[0].ID})
			require.NoError(t, err)
			require.True(t, work.GroupSnapshotID.Valid, "both outcomes retain immutable execution material")
			var attemptsBefore int
			require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT count(*) FROM device_write_attempt WHERE device_id=$1`, f.target.DeviceID).Scan(&attemptsBefore))
			callsBefore := len(firmware.calls)
			bindingsBefore := f.state(t).Bindings
			// Reconstruct the predecessor schema while retaining real operation,
			// assignment, snapshot, and write-attempt evidence from the workflow.
			down := reapplyCutoverMigration(t, f.conn, f.target.OrganizationID)
			intents, err := sqlc.New(f.conn).ListPoolGroupIntents(ctx, sqlc.ListPoolGroupIntentsParams{OrganizationID: f.target.OrganizationID, DeviceID: f.target.DeviceID})
			require.NoError(t, err)
			if outcome == "verified" {
				require.Len(t, intents, 1, "verified pre-upgrade board state must remain canonical after migration")
				require.Equal(t, operation.Targets[0].ID, intents[0].InstallationID)
				require.Equal(t, work.GroupSnapshotID.String, intents[0].GroupSnapshotID.String)
				_, err = f.conn.ExecContext(ctx, string(down))
				require.ErrorContains(t, err, "retained execution intent cannot be discarded", "rollback must refuse to erase backfilled board effects")
			} else {
				require.Empty(t, intents, "having a snapshot and a failed attempt does not establish a successful board effect")
			}
			gaps, err := sqlc.New(f.conn).ListPoolCutoverGaps(ctx, f.target.OrganizationID)
			require.NoError(t, err)
			missingEffect := false
			for _, gap := range gaps {
				missingEffect = missingEffect || gap.Kind == "unresolved_board_execution"
			}
			require.Equal(t, outcome == "failed", missingEffect, "upgrade must report attempted board work without established effective state")
			var attemptsAfter int
			require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT count(*) FROM device_write_attempt WHERE device_id=$1`, f.target.DeviceID).Scan(&attemptsAfter))
			require.Equal(t, attemptsBefore, attemptsAfter, "schema adoption must not fabricate hardware attempts")
			require.Len(t, firmware.calls, callsBefore)
			require.JSONEq(t, string(bindingsBefore), string(f.state(t).Bindings))
			var queued int
			require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT count(*) FROM queue_message WHERE device_id=$1`, f.target.DeviceID).Scan(&queued))
			require.Zero(t, queued)
		})
	}
}

func TestCutoverUpgradePreservesEarlierVerifiedEffectsAfterLaterUnappliedRequests(t *testing.T) {
	for _, laterState := range []string{"pending", "failed", "retired_then_pending"} {
		t.Run(laterState, func(t *testing.T) {
			ctx, f := newIdleRetryFixture(t)
			service, saved, firmware := newConfirmedSharedBoardApplication(t, ctx, f)
			require.NoError(t, f.service.ExecutePoolConfiguration(ctx, f.target.OrganizationID, saved.Operation.ID, f.target.DeviceID))
			for iteration := 0; iteration < 30 && active(f.state(t).Status); iteration++ {
				f.reconcile(t, ctx)
			}
			require.Equal(t, "complete", f.state(t).Status)
			original, err := f.service.queries.ListPoolGroupIntents(ctx, sqlc.ListPoolGroupIntentsParams{OrganizationID: f.target.OrganizationID, DeviceID: f.target.DeviceID})
			require.NoError(t, err)
			require.Len(t, original, 1)
			if laterState == "retired_then_pending" {
				require.NoError(t, db.WithTransactionNoResult(ctx, f.conn, func(q sqlc.Querier) error {
					return f.service.sharedRepository.RetireAssignment(ctx, q, poolconfigurations.Scope{OrganizationID: f.target.OrganizationID}, poolconfigurations.AssignmentTarget{Kind: poolconfigurations.TargetInstallation, ID: original[0].InstallationID, DeviceID: f.target.DeviceID}, saved.Operation.Targets[0].AssignmentVersion)
				}))
			}
			nextConfig, err := service.Create(ctx, "", poolconfigurations.RevisionOptions{Name: "Later unapplied settings", Slots: []poolconfigurations.SlotOptions{{URL: "stratum+tcp://later.example:3333", Username: "later"}}})
			require.NoError(t, err)
			review, err := service.ReviewBoards(ctx, "", nextConfig.ID, nextConfig.Version, poolconfigurations.BoardSelection{HashboardIDs: []int64{original[0].HashboardID}})
			require.NoError(t, err)
			info, err := session.GetInfo(ctx)
			require.NoError(t, err)
			next, err := db.WithTransaction(ctx, f.conn, func(q sqlc.Querier) (poolconfigurations.ReviewedSave, error) {
				return f.service.sharedRepository.ConfirmReview(ctx, q, poolconfigurations.Scope{OrganizationID: info.OrganizationID}, info.UserID, review.ID, f.service.sharedReviewer, nil)
			})
			require.NoError(t, err)
			if laterState == "failed" {
				require.NoError(t, f.service.ExecutePoolConfiguration(ctx, info.OrganizationID, next.Operation.ID, f.target.DeviceID))
				_, err = f.service.writes.ResolveOffline(ctx, recoveryEvidence(t, ctx, f), true)
				require.NoError(t, err)
			}
			later, err := service.GetOperation(ctx, "", next.Operation.ID)
			require.NoError(t, err)
			expectedState := laterState
			if laterState == "retired_then_pending" {
				expectedState = "pending"
			}
			require.Equal(t, expectedState, later.Targets[0].State)
			previous, err := service.GetOperation(ctx, "", saved.Operation.ID)
			require.NoError(t, err)
			require.Equal(t, "verified", previous.Targets[0].State)
			require.Greater(t, later.Targets[0].AssignmentVersion, previous.Targets[0].AssignmentVersion)
			callsBefore := len(firmware.calls)
			var attemptsBefore int
			require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT count(*) FROM device_write_attempt WHERE device_id=$1`, f.target.DeviceID).Scan(&attemptsBefore))
			reapplyCutoverMigration(t, f.conn, f.target.OrganizationID)
			intents, err := sqlc.New(f.conn).ListPoolGroupIntents(ctx, sqlc.ListPoolGroupIntentsParams{OrganizationID: f.target.OrganizationID, DeviceID: f.target.DeviceID})
			require.NoError(t, err)
			if laterState == "retired_then_pending" {
				require.Empty(t, intents, "upgrade must not revive effects across an explicit retirement generation")
			} else {
				require.Len(t, intents, 1, "a later assignment generation must not hide the last verified pre-upgrade effect")
				require.Equal(t, original[0].GroupSnapshotID.String, intents[0].GroupSnapshotID.String)
				material, err := sqlc.New(f.conn).GetPoolGroupSnapshot(ctx, sqlc.GetPoolGroupSnapshotParams{OrganizationID: f.target.OrganizationID, DeviceID: f.target.DeviceID, SnapshotID: intents[0].GroupSnapshotID.String})
				require.NoError(t, err)
				require.Equal(t, saved.Configuration.RevisionID, material.RevisionID)
				require.NotEqual(t, nextConfig.RevisionID, material.RevisionID)
			}
			if laterState == "failed" {
				gaps, err := sqlc.New(f.conn).ListPoolCutoverGaps(ctx, f.target.OrganizationID)
				require.NoError(t, err)
				missingEffect := false
				for _, gap := range gaps {
					missingEffect = missingEffect || gap.Kind == "unresolved_board_execution"
				}
				require.True(t, missingEffect, "retaining older verified intent must not hide a later potentially partial attempt from maintenance")
			}
			var attemptsAfter int
			require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT count(*) FROM device_write_attempt WHERE device_id=$1`, f.target.DeviceID).Scan(&attemptsAfter))
			require.Equal(t, attemptsBefore, attemptsAfter)
			require.Len(t, firmware.calls, callsBefore)
		})
	}
}
