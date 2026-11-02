package hostedaccounts_test

import (
	"context"
	"testing"
	"time"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	hostedpb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/poolapplications"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

func TestPoolWorkerInterruptionPreservesAdmissionBoundary(t *testing.T) {
	for _, scenario := range []string{
		"canceled before admission", "admission budget expires", "admission commit canceled",
		"permission revoked before retry", "canceled after admission", "canceled during write",
	} {
		t.Run(scenario, func(t *testing.T) {
			f := newPoolStepUpFixture(t)
			_, _, _, cipher, _ := poolFixture(t, f.fixture)
			ctx, err := hostedaccounts.WithRequestAccountID(f.sessionContext(t), f.account.AccountID)
			require.NoError(t, err)
			reviewer := poolapplications.NewReviewer(f.pools, poolCapabilities{sdk.CapabilityPoolConfig: true})
			repo := poolconfigurations.NewRepository(cipher)
			service := poolconfigurations.NewService(f.db, repo, f.pools, reviewer)
			config, err := service.Create(ctx, f.account.AccountID, poolconfigurations.RevisionOptions{Name: "Production", Slots: []poolconfigurations.SlotOptions{{URL: "stratum+tcp://pool.example", Username: "payout"}}})
			require.NoError(t, err)
			miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
			require.NoError(t, f.allocate(t, f.host, f.root, f.account.AccountID, hostedpb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(5, miner, 0)))
			target := poolconfigurations.AssignmentTarget{Kind: poolconfigurations.TargetDevice, ID: miner, DeviceID: miner}
			review, err := service.ReviewAssignment(ctx, f.account.AccountID, config.ID, config.Version, []poolconfigurations.AssignmentTarget{target})
			require.NoError(t, err)
			saved, err := service.ConfirmReview(ctx, f.account.AccountID, review.ID, "actor-password")
			require.NoError(t, err)
			device := &poolDevice{}
			worker := poolapplications.NewWorker(f.db, repo, reviewer, poolMinerGetter{device}, cipher)
			workCtx, cancel := context.WithCancel(t.Context())
			defer cancel()
			afterAdmission := scenario == "canceled after admission" || scenario == "canceled during write"
			switch scenario {
			case "canceled during write":
				device.onWrite = cancel
				device.writeError = context.Canceled
				err = worker.Execute(workCtx, f.org, saved.Operation.ID, target)
			case "admission commit canceled":
				// The claim callback has run, but COMMIT rolls it back. Its local
				// attempt counter must not be mistaken for committed admission.
				_, err = f.db.ExecContext(ctx, `CREATE FUNCTION cancel_test_pool_admission() RETURNS trigger LANGUAGE plpgsql AS $$
				BEGIN RAISE EXCEPTION 'simulated commit cancellation' USING ERRCODE='57014'; END; $$;
				CREATE CONSTRAINT TRIGGER cancel_test_pool_admission AFTER UPDATE ON pool_configuration_operation_target
				DEFERRABLE INITIALLY DEFERRED FOR EACH ROW WHEN (NEW.state='applying')
				EXECUTE FUNCTION cancel_test_pool_admission();`)
				require.NoError(t, err)
				err = worker.Execute(workCtx, f.org, saved.Operation.ID, target)
				_, dropErr := f.db.ExecContext(ctx, `DROP TRIGGER cancel_test_pool_admission ON pool_configuration_operation_target`)
				require.NoError(t, dropErr)
			default:
				blocker, blockErr := f.db.BeginTx(ctx, nil)
				require.NoError(t, blockErr)
				defer blocker.Rollback()
				var pid int
				require.NoError(t, blocker.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&pid))
				queryName := "LockDeviceWriteIdentities"
				if afterAdmission {
					_, blockErr = blocker.ExecContext(ctx, `SELECT id FROM organization WHERE id=$1 FOR NO KEY UPDATE`, f.org)
					require.NoError(t, blockErr)
					queryName = "LockPoolEquipmentOrganization"
				} else {
					keys, keyErr := sqlc.New(f.db).GetDeviceWriteIdentityKeys(ctx, miner)
					require.NoError(t, keyErr)
					require.NoError(t, sqlc.New(blocker).LockDeviceWriteIdentities(ctx, keys))
				}
				done := make(chan error, 1)
				go func() { done <- worker.Execute(workCtx, f.org, saved.Operation.ID, target) }()
				require.Eventually(t, func() bool {
					var waiting bool
					err := f.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database()
					 AND $1=ANY(pg_blocking_pids(pid)) AND query LIKE $2)`, pid, "-- name: "+queryName+"%").Scan(&waiting)
					return err == nil && waiting
				}, 3*time.Second, 10*time.Millisecond)
				if scenario != "admission budget expires" {
					cancel()
				}
				select {
				case err = <-done:
				case <-time.After(10 * time.Second):
					t.Fatal("worker did not finish after interruption")
				}
				require.NoError(t, blocker.Rollback())
			}
			var state, code string
			var attempts, physicalAttempts int
			var linked bool
			require.NoError(t, f.db.QueryRowContext(ctx, `SELECT state,result_code,attempts,physical_attempt_id IS NOT NULL
			 FROM pool_configuration_operation_target WHERE operation_id=$1`, saved.Operation.ID).Scan(&state, &code, &attempts, &linked))
			require.NoError(t, f.db.QueryRowContext(ctx, `SELECT count(*) FROM device_write_attempt WHERE device_id=$1`, miner).Scan(&physicalAttempts))
			if afterAdmission {
				require.NoError(t, err)
				require.Equal(t, "failed", state)
				require.True(t, linked)
				require.Equal(t, 1, attempts)
				require.Equal(t, 1, physicalAttempts)
				wantCode, writes := "interrupted", 0
				if scenario == "canceled during write" {
					wantCode, writes = "write_uncertain", 1
				}
				require.Equal(t, wantCode, code)
				require.NoError(t, worker.Execute(t.Context(), f.org, saved.Operation.ID, target))
				require.Equal(t, writes, device.writes, "admitted work must never be automatically replayed")
				if scenario == "canceled after admission" {
					retry, err := service.ReviewRetry(ctx, f.account.AccountID, saved.Operation.ID, []poolconfigurations.AssignmentTarget{target})
					require.NoError(t, err)
					confirmed, err := service.ConfirmReview(ctx, f.account.AccountID, retry.ID, "actor-password")
					require.NoError(t, err)
					require.NotEqual(t, saved.Operation.ID, confirmed.Operation.ID)
					require.NoError(t, worker.Execute(t.Context(), f.org, confirmed.Operation.ID, target))
					require.Equal(t, 1, device.writes, "interrupted admitted work requires a fresh confirmed operation")
				}
			} else {
				require.Equal(t, "pending", state)
				require.Empty(t, code)
				require.False(t, linked)
				require.Zero(t, attempts)
				require.Zero(t, physicalAttempts)
				require.Zero(t, device.writes)
				require.Error(t, err)
				if scenario == "admission budget expires" {
					require.NoError(t, workCtx.Err(), "the inner admission budget can expire before the caller's context")
				}
				if scenario == "permission revoked before retry" {
					require.NoError(t, f.svc.ConfigureChild(ctx, f.host, f.org, f.account.AccountID, hostedaccounts.AllCapabilities&^hostedaccounts.ChangePools, false))
				}
				require.NoError(t, worker.Execute(t.Context(), f.org, saved.Operation.ID, target))
				operation, err := service.GetOperation(ctx, f.account.AccountID, saved.Operation.ID)
				require.NoError(t, err)
				if scenario == "permission revoked before retry" {
					require.Equal(t, "failed", operation.Targets[0].State)
					require.Equal(t, "rejected", operation.Targets[0].ResultCode)
					require.Zero(t, device.writes)
				} else {
					require.Equal(t, "verified", operation.Targets[0].State)
					require.Equal(t, 1, device.writes, "the same approved operation can run after a pre-admission interruption")
				}
			}
		})
	}
}
