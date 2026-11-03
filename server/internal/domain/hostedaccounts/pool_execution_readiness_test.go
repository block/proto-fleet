package hostedaccounts_test

import (
	"context"
	"errors"
	"testing"

	hostedpb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/poolapplications"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

func TestPoolContainerChecksModeWithinWriteAdmissionBeforeClaim(t *testing.T) {
	for _, scenario := range []string{"ready", "mode changed", "controller busy", "unsupported", "read timeout", "read unavailable", "claim commit canceled", "permission revoked", "lost write response"} {
		t.Run(scenario, func(t *testing.T) {
			f := newPoolStepUpFixture(t)
			_, _, _, cipher, _ := poolFixture(t, f.fixture)
			ctx, err := hostedaccounts.WithRequestAccountID(f.sessionContext(t), f.account.AccountID)
			require.NoError(t, err)
			module, controller := f.miningModule(t)
			require.NoError(t, f.allocate(t, f.host, f.root, f.account.AccountID, hostedpb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(6, module, 0)))
			device := &poolContainer{result: sdk.MiningGroupResult{Supported: true}}
			getter := poolMinerGetter{device}
			reviewer := poolapplications.NewReviewer(f.pools, poolCapabilities{sdk.CapabilityPoolConfig: true})
			repo := poolconfigurations.NewRepository(cipher)
			service := poolconfigurations.NewService(f.db, repo, f.pools, reviewer).WithReadiness(poolapplications.NewReadiness(f.db, reviewer, getter))
			config, err := service.Create(ctx, f.account.AccountID, poolconfigurations.RevisionOptions{Name: "Container", Slots: []poolconfigurations.SlotOptions{{URL: "stratum+tcp://pool.example", Username: "worker"}}})
			require.NoError(t, err)
			target := poolconfigurations.AssignmentTarget{Kind: poolconfigurations.TargetDevice, ID: controller, DeviceID: controller}
			review, err := service.ReviewAssignment(ctx, f.account.AccountID, config.ID, config.Version, []poolconfigurations.AssignmentTarget{target})
			require.NoError(t, err)
			saved, err := service.ConfirmReview(ctx, f.account.AccountID, review.ID, "actor-password")
			require.NoError(t, err)
			_, err = f.db.ExecContext(ctx, `UPDATE container_mining_group_state SET observed_at=now()-interval '3 minutes' WHERE device_id=$1`, controller)
			require.NoError(t, err)
			device.observations = 0
			device.observe = func(context.Context) (sdk.MiningGroupResult, error) {
				var active int
				require.NoError(t, f.db.QueryRowContext(ctx, `SELECT count(*) FROM device_write_attempt WHERE device_id=$1 AND status='running' AND operation_kind='mutation' AND command_type='UpdateMiningPools'`, controller).Scan(&active))
				require.Equal(t, 1, active, "observe under the same physical admission that will protect the write")
				var state string
				require.NoError(t, f.db.QueryRowContext(ctx, `SELECT state FROM pool_configuration_operation_target WHERE operation_id=$1`, saved.Operation.ID).Scan(&state))
				require.Equal(t, "pending", state, "a failed observation must not claim the pool update")
				if scenario == "read timeout" {
					return sdk.MiningGroupResult{}, context.DeadlineExceeded
				}
				if scenario == "read unavailable" {
					return sdk.MiningGroupResult{}, errors.New("private read failure")
				}
				return device.result, nil
			}
			switch scenario {
			case "mode changed":
				device.result.Enabled = true
			case "controller busy":
				device.result.Pending = true
			case "unsupported":
				device.result.Supported = false
			case "permission revoked":
				require.NoError(t, f.svc.ConfigureChild(ctx, f.host, f.org, f.account.AccountID, hostedaccounts.AllCapabilities&^hostedaccounts.ChangePools, false))
			case "lost write response":
				device.writeError = errors.New("lost reply")
			}
			if scenario == "claim commit canceled" {
				_, err = f.db.ExecContext(ctx, `CREATE FUNCTION cancel_test_container_claim() RETURNS trigger LANGUAGE plpgsql AS $$
                BEGIN RAISE EXCEPTION 'simulated commit cancellation' USING ERRCODE='57014'; END; $$;
                CREATE CONSTRAINT TRIGGER cancel_test_container_claim AFTER UPDATE ON pool_configuration_operation_target
                DEFERRABLE INITIALLY DEFERRED FOR EACH ROW WHEN (NEW.state='applying') EXECUTE FUNCTION cancel_test_container_claim();`)
				require.NoError(t, err)
			}
			device.onWrite = func() {
				var linked bool
				require.NoError(t, f.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pool_configuration_operation_target t JOIN device_write_attempt a ON a.attempt_id=t.physical_attempt_id
                WHERE t.operation_id=$1 AND t.state='applying' AND a.status='running' AND a.command_type='UpdateMiningPools')`, saved.Operation.ID).Scan(&linked))
				require.True(t, linked, "claim and physical attempt must be committed before the pool write")
			}
			retryable := scenario == "read timeout" || scenario == "read unavailable" || scenario == "claim commit canceled"
			worker := poolapplications.NewWorker(f.db, repo, reviewer, getter, cipher)
			err = worker.Execute(t.Context(), f.org, saved.Operation.ID, target)
			if retryable {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			result, err := service.GetOperation(ctx, f.account.AccountID, saved.Operation.ID)
			require.NoError(t, err)
			wantState, wantCode, writes := "failed", "rejected", 0
			switch scenario {
			case "ready":
				wantState, wantCode, writes = "verified", "", 1
			case "read timeout", "read unavailable", "claim commit canceled":
				wantState, wantCode = "pending", ""
			case "lost write response":
				wantCode, writes = "write_uncertain", 1
			}
			require.Equal(t, wantState, result.Targets[0].State)
			require.Equal(t, wantCode, result.Targets[0].ResultCode)
			require.Equal(t, writes, device.writes)
			if scenario == "permission revoked" {
				require.Zero(t, device.observations)
			} else {
				require.Equal(t, 1, device.observations)
			}
			if retryable {
				var attempts int
				var linked bool
				require.NoError(t, f.db.QueryRowContext(ctx, `SELECT attempts,physical_attempt_id IS NOT NULL FROM pool_configuration_operation_target WHERE operation_id=$1`, saved.Operation.ID).Scan(&attempts, &linked))
				require.Zero(t, attempts)
				require.False(t, linked)
				if scenario == "claim commit canceled" {
					_, err = f.db.ExecContext(ctx, `DROP TRIGGER cancel_test_container_claim ON pool_configuration_operation_target`)
					require.NoError(t, err)
				}
				var active int
				require.NoError(t, f.db.QueryRowContext(ctx, `SELECT count(*) FROM device_write_attempt WHERE device_id=$1 AND status IN ('running','unknown')`, controller).Scan(&active))
				require.Zero(t, active, "returned read/pre-claim failures release physical admission")
				device.observe = nil
				require.NoError(t, worker.Execute(t.Context(), f.org, saved.Operation.ID, target))
				require.Equal(t, 1, device.writes, "the same request can run after a read timeout")
			} else {
				require.NoError(t, worker.Execute(t.Context(), f.org, saved.Operation.ID, target))
				require.Equal(t, writes, device.writes, "terminal or uncertain work cannot replay")
			}
		})
	}
}
