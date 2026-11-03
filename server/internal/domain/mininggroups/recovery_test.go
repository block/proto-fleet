package mininggroups

import (
	"context"
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/poolapplications"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func recoveryEvidence(t *testing.T, ctx context.Context, f retryFixture) devicewrite.RecoveryEvidence {
	t.Helper()
	rows, err := f.service.writes.InspectRecovery(ctx, f.target.OrganizationID, f.target.DeviceID)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	evidence := rows[0].EvidenceTemplate
	evidence.Operator = "host operator"
	evidence.MaintenanceTermination = "all dispatchers stopped"
	evidence.OwnerTermination = "exact owner terminated"
	evidence.TransportQuiescence = "all transport work ended"
	evidence.DeviceQuiescence = "device jobs conclusively ended"
	evidence.ConfigurationVerification = "controller configuration verified"
	return evidence
}

func TestOfflineRecoverySettlesSharedPoolWorkflow(t *testing.T) {
	for _, status := range []string{"pending", "applying", "blocked"} {
		t.Run(status, func(t *testing.T) {
			ctx, f := newIdleRetryFixture(t)
			_, boards := preparePermissionPoolAssignment(t, ctx, f)
			publishSharedBoardInventory(t, ctx, f)
			firmware := &simulatedGroups{snapshot: sdk.MiningGroupResult{Supported: true, Enabled: true, Revision: 1, MaxGroups: 4, Groups: []sdk.MiningGroup{{ID: "default", Revision: 1, MACs: []string{"02:00:00:00:00:01"}}}}, receipts: map[string]sdk.MiningGroupResult{}}
			f.service.miners = firmware
			_, err := f.service.GetState(ctx, f.target.DeviceIdentifier)
			require.NoError(t, err)
			repo := poolconfigurations.NewRepository(f.service.secrets)
			reviewer := poolapplications.NewReviewer(f.service.pools, sharedCapabilities{})
			f.service.WithPoolConfigurations(repo, reviewer)
			service := poolconfigurations.NewService(f.conn, repo, f.service.pools, reviewer)
			config, err := service.Create(ctx, "", poolconfigurations.RevisionOptions{Name: "Shared", Slots: []poolconfigurations.SlotOptions{{URL: "stratum+tcp://pool.example:3333", Username: "worker"}}})
			require.NoError(t, err)
			info, err := session.GetInfo(ctx)
			require.NoError(t, err)
			confirm := func() poolconfigurations.ReviewedSave {
				review, err := service.ReviewBoards(ctx, "", config.ID, config.Version, poolconfigurations.BoardSelection{HashboardIDs: boards})
				require.NoError(t, err)
				saved, err := db.WithTransaction(ctx, f.conn, func(q sqlc.Querier) (poolconfigurations.ReviewedSave, error) {
					return repo.ConfirmReview(ctx, q, poolconfigurations.Scope{OrganizationID: info.OrganizationID}, info.UserID, review.ID, reviewer)
				})
				require.NoError(t, err)
				return saved
			}
			saved := confirm()
			require.NoError(t, f.service.ExecutePoolConfiguration(ctx, info.OrganizationID, saved.Operation.ID, f.target.DeviceID))
			if status != "pending" {
				f.reconcile(t, ctx) // Persist a step without dispatching it.
			}
			_, err = f.conn.ExecContext(ctx, `UPDATE container_mining_group_state SET status=$2 WHERE device_id=$1`, f.target.DeviceID, status)
			require.NoError(t, err)
			if status == "blocked" {
				// The background worker may already have exposed write uncertainty.
				_, err = f.conn.ExecContext(ctx, `UPDATE device_write_attempt SET status='unknown',finished_at=clock_timestamp() WHERE attempt_id=$1`, f.state(t).AttemptID.UUID)
				require.NoError(t, err)
				_, err = f.conn.ExecContext(ctx, `UPDATE pool_configuration_operation_target SET state='failed',result_code='write_uncertain' WHERE operation_id=$1`, saved.Operation.ID)
				require.NoError(t, err)
			}
			before := f.state(t)
			evidence := recoveryEvidence(t, ctx, f)
			preview, err := f.service.writes.ResolveOffline(ctx, evidence, false)
			require.NoError(t, err)
			require.True(t, preview.Preview)
			require.Equal(t, before, f.state(t))

			if status == "applying" {
				// A target bookkeeping failure must roll back the recovery, too.
				_, err = f.conn.ExecContext(ctx, `CREATE FUNCTION reject_recovered_target() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'target bookkeeping unavailable'; END $$; CREATE TRIGGER reject_recovered_target BEFORE UPDATE ON pool_configuration_operation_target FOR EACH ROW EXECUTE FUNCTION reject_recovered_target()`)
				require.NoError(t, err)
				_, err = f.service.writes.ResolveOffline(ctx, evidence, true)
				require.ErrorContains(t, err, "target bookkeeping unavailable")
				require.Equal(t, before, f.state(t))
				require.Equal(t, evidence, recoveryEvidence(t, ctx, f))
				var count int
				require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT count(*) FROM device_write_recovery`).Scan(&count))
				require.Zero(t, count)
				_, err = f.conn.ExecContext(ctx, `DROP TRIGGER reject_recovered_target ON pool_configuration_operation_target`)
				require.NoError(t, err)
			}

			calls := len(firmware.calls)
			result, err := f.service.writes.ResolveOffline(ctx, evidence, true)
			require.NoError(t, err)
			require.NotEqual(t, uuid.Nil, result.RecoveryID)
			after := f.state(t)
			require.Equal(t, "failed", after.Status)
			require.Contains(t, after.Message, "offline recovery")
			require.Equal(t, before.AttemptID, after.AttemptID, "retain the recovered attempt for diagnosis")
			require.Equal(t, before.Step, after.Step, "retain the interrupted step as evidence")
			require.Equal(t, before.Bindings, after.Bindings, "recovery cannot claim hardware changed")
			operation, err := service.GetOperation(ctx, "", saved.Operation.ID)
			require.NoError(t, err)
			require.Equal(t, "failed", operation.Targets[0].State)
			require.Equal(t, "interrupted", operation.Targets[0].ResultCode)
			intents, err := f.service.queries.ListPoolGroupIntents(ctx, sqlc.ListPoolGroupIntentsParams{OrganizationID: info.OrganizationID, DeviceID: f.target.DeviceID})
			require.NoError(t, err)
			require.Empty(t, intents, "an unrelated future update must not replay recovered intent")
			f.reconcile(t, ctx)
			require.Len(t, firmware.calls, calls, "recovery must not replay the old step")
			_, err = f.service.writes.ResumeWorkflow(ctx, info.OrganizationID, f.target.DeviceID, before.AttemptID.UUID)
			require.Error(t, err)

			// A newly reviewed request can now pass the controller admission gate.
			next := confirm()
			require.NoError(t, f.service.ExecutePoolConfiguration(ctx, info.OrganizationID, next.Operation.ID, f.target.DeviceID))
			require.Equal(t, "pending", f.state(t).Status)
			require.NotEqual(t, before.AttemptID, f.state(t).AttemptID)
			for i := 0; i < 20 && active(f.state(t).Status); i++ {
				f.reconcile(t, ctx)
			}
			require.Equal(t, "complete", f.state(t).Status)
		})
	}
}

func TestOfflineRecoveryPreservesUnrelatedJournal(t *testing.T) {
	for _, mismatch := range []string{"attempt", "epoch", "terminal"} {
		t.Run(mismatch, func(t *testing.T) {
			ctx, f := newIdleRetryFixture(t)
			_, err := f.service.Submit(ctx, f.target.DeviceIdentifier, uuid.NewString(), "", nil, 0)
			require.NoError(t, err)
			evidence := recoveryEvidence(t, ctx, f)
			row := f.state(t)
			switch mismatch {
			case "attempt":
				// The recovered attempt is older than the journal now on the controller.
				other := uuid.New()
				_, err := f.conn.ExecContext(ctx, `INSERT INTO device_write_attempt(attempt_id,device_id,organization_id,epoch,identity_keys,identity_snapshot,owner_instance_id,operation_kind,command_type,status,finished_at)
SELECT $2,device_id,organization_id,epoch,identity_keys,identity_snapshot,owner_instance_id,operation_kind,command_type,'complete',clock_timestamp() FROM device_write_attempt WHERE attempt_id=$1`, evidence.AttemptID, other)
				require.NoError(t, err)
				row.AttemptID.UUID = other
			case "epoch":
				row.WriteEpoch++
			case "terminal":
				row.Status = "complete"
			}
			require.NoError(t, save(ctx, f.service.queries, row))
			before := f.state(t)
			_, err = f.service.writes.ResolveOffline(ctx, evidence, true)
			require.NoError(t, err)
			require.Equal(t, before, f.state(t), "only the exact active journal may be retired")
		})
	}
}

func TestOfflineRecoveryDiscardsOnlyLinkedLegacyIntent(t *testing.T) {
	for _, linked := range []bool{true, false} {
		t.Run(map[bool]string{true: "recovered operation", false: "previous operation"}[linked], func(t *testing.T) {
			ctx, f, _, _, _ := newTransferFailureFixture(t)
			if !linked {
				_, err := f.conn.ExecContext(ctx, `UPDATE hashboard_pool_intent SET operation_id=$1`, uuid.New())
				require.NoError(t, err)
			}
			before, err := f.service.queries.ListHashboardPoolIntents(ctx, sqlc.ListHashboardPoolIntentsParams{OrganizationID: f.target.OrganizationID, DeviceID: f.target.DeviceID})
			require.NoError(t, err)
			require.Len(t, before, 1)
			_, err = f.service.writes.ResolveOffline(ctx, recoveryEvidence(t, ctx, f), true)
			require.NoError(t, err)
			require.Equal(t, "failed", f.state(t).Status)
			after, err := f.service.queries.ListHashboardPoolIntents(ctx, sqlc.ListHashboardPoolIntentsParams{OrganizationID: f.target.OrganizationID, DeviceID: f.target.DeviceID})
			require.NoError(t, err)
			if linked {
				require.Empty(t, after, "recovered intent cannot be replayed by a later request")
			} else {
				require.Equal(t, before, after, "previously retained intent belongs to another operation")
			}
		})
	}
}
