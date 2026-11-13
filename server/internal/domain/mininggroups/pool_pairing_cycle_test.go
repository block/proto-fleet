package mininggroups

import (
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/poolapplications"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestPairingCycleEndsBoardWorkflowWithoutDiscardingUnresolvedWrites(t *testing.T) {
	for _, stage := range []string{"accepted", "admitted", "transfer receipt", "unknown request"} {
		t.Run(stage, func(t *testing.T) {
			ctx, f := newIdleRetryFixture(t)
			preparePermissionPoolAssignment(t, ctx, f)
			publishSharedBoardInventory(t, ctx, f)
			firmware := &simulatedGroups{snapshot: sdk.MiningGroupResult{Supported: true, Enabled: true, Revision: 1, MaxGroups: 4, Groups: []sdk.MiningGroup{{ID: "default", Revision: 1, MACs: []string{"02:00:00:00:00:01"}}}}, receipts: map[string]sdk.MiningGroupResult{}}
			guarded := &admittedGroups{firmware}
			f.service.miners = guarded
			_, err := f.service.GetState(ctx, f.target.DeviceIdentifier)
			require.NoError(t, err)
			repo := poolconfigurations.NewRepository(f.service.secrets)
			reviewer := poolapplications.NewReviewer(f.service.pools, removalCapabilities{})
			f.service.WithPoolConfigurations(repo, reviewer)
			service := poolconfigurations.NewService(f.conn, repo, f.service.pools, reviewer)
			_, err = service.Create(ctx, "", poolconfigurations.RevisionOptions{Name: "Automatic", Slots: []poolconfigurations.SlotOptions{{URL: "stratum+tcp://automatic.example", Username: "operator"}}})
			require.NoError(t, err)
			q := sqlc.New(f.conn)
			pair := func() {
				for _, status := range []sqlc.PairingStatusEnum{sqlc.PairingStatusEnumUNPAIRED, sqlc.PairingStatusEnumPAIRED} {
					_, err := q.UpsertDevicePairing(ctx, sqlc.UpsertDevicePairingParams{DeviceID: f.target.DeviceID, PairingStatus: status})
					require.NoError(t, err)
				}
			}
			pair()
			worker := poolapplications.NewWorker(f.conn, repo, reviewer, guarded, f.service.secrets).WithBoardApplications(f.service)
			var parent, child uuid.UUID
			require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT transition_id FROM pool_lifecycle_transition WHERE device_id=$1 AND asset_kind=0 AND state='pending'`, f.target.DeviceID).Scan(&parent))
			require.NoError(t, worker.ProcessLifecycle(ctx, parent))
			require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT transition_id FROM pool_lifecycle_transition WHERE device_id=$1 AND asset_kind=7 AND reason='pairing'`, f.target.DeviceID).Scan(&child))
			require.NoError(t, worker.ProcessLifecycle(ctx, child))
			row, err := q.GetPoolLifecycleTransition(ctx, child)
			require.NoError(t, err)
			target := poolconfigurations.AssignmentTarget{Kind: row.TargetKind, ID: row.TargetID, DeviceID: row.DeviceID}
			if stage != "accepted" {
				require.NoError(t, worker.Execute(ctx, f.target.OrganizationID, row.OperationID.String, target))
			}
			if stage == "transfer receipt" {
				for i := 0; i < 30 && f.state(t).ControllerOperationID == ""; i++ {
					require.NoError(t, f.service.Reconcile(ctx, f.target.OrganizationID, f.target.DeviceID, f.target.DeviceIdentifier))
				}
				require.NotEmpty(t, f.state(t).ControllerOperationID)
			} else if stage == "unknown request" {
				require.NoError(t, f.service.Reconcile(ctx, f.target.OrganizationID, f.target.DeviceID, f.target.DeviceIdentifier))
				require.NoError(t, f.service.Reconcile(ctx, f.target.OrganizationID, f.target.DeviceID, f.target.DeviceIdentifier))
				require.True(t, f.state(t).Step.Valid)
				require.Empty(t, f.state(t).ControllerOperationID)
			}
			before := f.state(t)
			pair()
			firmware.calls = nil
			if stage == "accepted" {
				require.NoError(t, worker.Execute(ctx, f.target.OrganizationID, row.OperationID.String, target))
				operation, err := service.GetOperation(ctx, "", row.OperationID.String)
				require.NoError(t, err)
				require.Equal(t, "failed", operation.Targets[0].State)
				require.Empty(t, firmware.calls, "an obsolete accepted operation must be rejected before controller I/O")
				return
			}
			for i := 0; i < 3 && active(f.state(t).Status); i++ {
				require.NoError(t, f.service.Reconcile(ctx, f.target.OrganizationID, f.target.DeviceID, f.target.DeviceIdentifier))
			}
			for _, call := range firmware.calls {
				require.True(t, call.ReadOnly(), "the ended pairing cycle must not dispatch another mutation: %s", call.Action)
			}
			attempt, err := q.LockDeviceWriteAttemptForRecovery(ctx, before.AttemptID.UUID)
			require.NoError(t, err)
			if stage == "unknown request" {
				require.True(t, active(f.state(t).Status))
				require.Equal(t, before.Step, f.state(t).Step)
				require.Equal(t, before.StepKey, f.state(t).StepKey)
				require.Equal(t, "running", attempt.Status)
				_, err = f.service.writes.Reserve(ctx, f.target.OrganizationID, f.target.DeviceID)
				require.Error(t, err, "unresolved writes must retain their physical fence")
			} else {
				require.False(t, active(f.state(t).Status), f.state(t).Message)
				require.Equal(t, "complete", attempt.Status)
			}
		})
	}
}
