package hostedaccounts_test

import (
	"context"
	"errors"
	"testing"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	hostedpb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/miner/dto"
	"github.com/block/proto-fleet/server/internal/domain/poolapplications"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

func TestNewOwnerCanObserveCompletedRetiredPoolRemoval(t *testing.T) {
	for _, scenario := range []string{"completed", "unavailable", "unknown", "replacement assignment"} {
		t.Run(scenario, func(t *testing.T) {
			f := newPoolStepUpFixture(t)
			_, _, _, cipher, _ := poolFixture(t, f.fixture)
			ctx, err := hostedaccounts.WithRequestAccountID(f.sessionContext(t), f.account.AccountID)
			require.NoError(t, err)
			reviewer := poolapplications.NewReviewer(f.pools, poolCapabilities{sdk.CapabilityPoolConfig: true, sdk.CapabilityPoolClear: true})
			repo := poolconfigurations.NewRepository(cipher)
			device := &poolDevice{}
			getter := poolMinerGetter{device}
			checker := poolapplications.NewChecker(f.db, reviewer, getter, cipher, f.pools)
			readiness := poolapplications.NewReadiness(f.db, reviewer, getter)
			service := poolconfigurations.NewService(f.db, repo, f.pools, reviewer).WithChecker(checker).WithReadiness(readiness)
			worker := poolapplications.NewWorker(f.db, repo, reviewer, getter, cipher).WithRemovals(checker)
			options := poolconfigurations.RevisionOptions{Name: "Production", Slots: []poolconfigurations.SlotOptions{{URL: "stratum+tcp://pool.example", Username: "worker"}}}
			config, err := service.Create(ctx, f.account.AccountID, options)
			require.NoError(t, err)
			miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
			target := poolconfigurations.AssignmentTarget{Kind: poolconfigurations.TargetDevice, ID: miner, DeviceID: miner}
			require.NoError(t, f.allocate(t, f.host, f.root, f.account.AccountID, hostedpb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(5, miner, 0)))
			review, err := service.ReviewAssignment(ctx, f.account.AccountID, config.ID, config.Version, []poolconfigurations.AssignmentTarget{target})
			require.NoError(t, err)
			assigned, err := service.ConfirmReview(ctx, f.account.AccountID, review.ID, poolconfigurations.Confirmation{Password: "actor-password"})
			require.NoError(t, err)
			require.NoError(t, worker.Execute(t.Context(), f.org, assigned.Operation.ID, target))
			removal, err := service.ReviewRemoval(ctx, f.account.AccountID, target)
			require.NoError(t, err)
			device.onWrite = func() { device.readError = errors.New("offline readback") }
			result, err := confirmAndRunRemoval(t, service, worker, ctx, f.account.AccountID, removal.ID, false, "actor-password")
			require.NoError(t, err)
			require.Equal(t, "uncertain", result.State)
			var attemptStatus string
			require.NoError(t, f.db.QueryRowContext(ctx, `SELECT w.status FROM device_write_attempt w JOIN pool_configuration_operation_target t ON t.physical_attempt_id=w.attempt_id WHERE t.operation_id=$1`, removal.ID).Scan(&attemptStatus))
			require.Equal(t, "complete", attemptStatus)
			require.NoError(t, f.allocate(t, f.host, f.root, f.account.AccountID, hostedpb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_RECALL, selected(5, miner, 1)))
			other := f.client(t, "Next owner", f.user, hostedaccounts.AllCapabilities)
			nextCtx, err := hostedaccounts.WithRequestAccountID(f.sessionContext(t), other.AccountID)
			require.NoError(t, err)
			require.NoError(t, f.allocate(t, f.host, f.root, other.AccountID, hostedpb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(5, miner, 2)))
			next, err := service.Create(nextCtx, other.AccountID, options)
			require.NoError(t, err)
			reads := 0
			device.onRead = func(context.Context) error { reads++; return nil }
			device.onWrite, device.readError = nil, nil
			// Current hardware need not be empty: settling obsolete intent must neither
			// clear these settings nor claim the old removal succeeded.
			device.pools = []dto.MiningPool{{URL: "stratum+tcp://current.example", Username: "new-owner"}}
			_, err = service.ReviewAssignment(ctx, f.account.AccountID, config.ID, config.Version, []poolconfigurations.AssignmentTarget{target})
			require.Error(t, err, "the former owner cannot recover by observing the new owner's device")
			require.Zero(t, reads)
			if scenario == "unavailable" {
				device.readError = errors.New("offline")
			}
			if scenario == "unknown" {
				_, err = f.db.ExecContext(ctx, `UPDATE device_write_attempt SET status='unknown' WHERE attempt_id=(SELECT physical_attempt_id FROM pool_configuration_operation_target WHERE operation_id=$1)`, removal.ID)
				require.NoError(t, err)
			}
			if scenario == "replacement assignment" {
				// Seed a newer assignment generation while retaining the old reservation.
				// Recovery must never retire or rewrite this replacement.
				_, err = f.db.ExecContext(ctx, `UPDATE pool_configuration_assignment SET configuration_id=$1,revision_id=$2,version=version+1,retired_at=NULL,operation_id=NULL,adopted_at=clock_timestamp() WHERE organization_id=$3 AND target_kind='device' AND target_id=$4`, next.ID, next.RevisionID, f.org, miner)
				require.NoError(t, err)
			}
			q := sqlc.New(f.db)
			binding := sqlc.GetPoolConfigurationAssignmentParams{OrganizationID: f.org, TargetKind: target.Kind, TargetID: target.ID}
			before, err := q.GetPoolConfigurationAssignment(ctx, binding)
			require.NoError(t, err)
			writes := device.writes
			_, err = service.ReviewAssignment(nextCtx, other.AccountID, next.ID, next.Version, []poolconfigurations.AssignmentTarget{target})
			var state string
			require.NoError(t, f.db.QueryRowContext(ctx, `SELECT state FROM pool_configuration_operation_target WHERE operation_id=$1`, removal.ID).Scan(&state))
			if scenario == "unavailable" || scenario == "unknown" {
				require.Error(t, err)
				require.Equal(t, "uncertain", state, "unavailable or unknown attempts retain their reservation")
			} else {
				require.NoError(t, err)
				require.Equal(t, "superseded", state)
				require.Positive(t, reads)
			}
			if scenario == "unknown" {
				require.Zero(t, reads, "unknown delivery cannot be resolved by readback")
			}
			after, err := q.GetPoolConfigurationAssignment(ctx, binding)
			require.NoError(t, err)
			require.Equal(t, before, after, "only the obsolete operation is settled")
			require.Equal(t, writes, device.writes)
			require.Equal(t, "new-owner", device.pools[0].Username)
		})
	}
}
