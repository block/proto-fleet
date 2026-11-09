package hostedaccounts_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/session"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"

	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/miner/dto"
	"github.com/block/proto-fleet/server/internal/domain/miner/interfaces"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	hostedpb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/poolapplications"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestPoolRemovalPreservesHardwareUnlessExplicitlyCleared(t *testing.T) {
	for _, scenario := range []string{"keep", "clear", "slow review", "pending", "unverified", "unsupported", "stale", "failed clear", "replay after reassignment", "readback failure", "crash after clear", "offline readback recovery", "lost acknowledgement", "unresolved write", "revoked"} {
		t.Run(scenario, func(t *testing.T) {
			f := newPoolStepUpFixture(t)
			_, _, _, cipher, _ := poolFixture(t, f.fixture)
			ctx, err := hostedaccounts.WithRequestAccountID(f.sessionContext(t), f.account.AccountID)
			require.NoError(t, err)
			caps := poolCapabilities{sdk.CapabilityPoolConfig: true, sdk.CapabilityPoolClear: scenario != "unsupported"}
			reviewer := poolapplications.NewReviewer(f.pools, caps)
			repo := poolconfigurations.NewRepository(cipher)
			device := &poolDevice{}
			checker := poolapplications.NewChecker(f.db, reviewer, poolMinerGetter{device}, cipher, f.pools)
			worker := poolapplications.NewWorker(f.db, repo, reviewer, poolMinerGetter{device}, cipher).WithRemovals(checker)
			service := poolconfigurations.NewService(f.db, repo, f.pools, reviewer).WithChecker(checker)
			config, err := service.Create(ctx, f.account.AccountID, poolconfigurations.RevisionOptions{Name: "Production", Slots: []poolconfigurations.SlotOptions{{URL: "stratum+tcp://pool.example", Username: "worker"}}})
			require.NoError(t, err)
			miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
			require.NoError(t, f.allocate(t, f.host, f.root, f.account.AccountID, hostedpb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(5, miner, 0)))
			target := poolconfigurations.AssignmentTarget{Kind: poolconfigurations.TargetDevice, ID: miner, DeviceID: miner}
			assign := func() {
				r, err := service.ReviewAssignment(ctx, f.account.AccountID, config.ID, config.Version, []poolconfigurations.AssignmentTarget{target})
				require.NoError(t, err)
				saved, err := service.ConfirmReview(ctx, f.account.AccountID, r.ID, poolconfigurations.Confirmation{Password: "actor-password"})
				require.NoError(t, err)
				if scenario != "pending" {
					require.NoError(t, poolapplications.NewWorker(f.db, repo, reviewer, poolMinerGetter{device}, cipher).Execute(t.Context(), f.org, saved.Operation.ID, target))
				}
			}
			assign()
			if scenario == "pending" {
				_, err := service.ReviewRemoval(ctx, f.account.AccountID, target)
				require.ErrorContains(t, err, "finish")
				return
			}
			if scenario == "unresolved write" {
				writes := devicewrite.NewService(f.db)
				require.Error(t, writes.WithHostAttempt(ctx, f.org, miner, 0, commandtype.UpdateMiningPools, func(ctx context.Context) error {
					return devicewrite.Dispatch(ctx, func(context.Context) error { return devicewrite.UncertainDelivery(errors.New("lost acknowledgement")) })
				}))
				_, err := service.ReviewRemoval(ctx, f.account.AccountID, target)
				require.Error(t, err)
				return
			}
			if scenario == "unverified" {
				device.readError = errors.New("offline")
			}
			if scenario == "slow review" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, 20*time.Second)
				defer cancel()
				device.onRead = func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }
			}
			review, err := service.ReviewRemoval(ctx, f.account.AccountID, target)
			require.NoError(t, err)
			keep := scenario == "keep" || scenario == "replay after reassignment"
			if scenario == "stale" {
				assign()
			}
			if scenario == "failed clear" {
				device.writeError = sdk.WriteRefused(errors.New("refused"), sdk.WriteStageConfigurationRead, sdk.WriteReasonReadFailed)
			}
			if scenario == "readback failure" || scenario == "crash after clear" || scenario == "offline readback recovery" {
				device.onWrite = func() { device.readError = errors.New("readback unavailable") }
			}
			if scenario == "lost acknowledgement" {
				device.writeError = devicewrite.UncertainDelivery(errors.New("lost acknowledgement"))
			}
			if scenario == "revoked" {
				require.NoError(t, f.svc.ConfigureChild(t.Context(), f.host, f.org, f.account.AccountID, hostedaccounts.AllCapabilities&^hostedaccounts.ChangePools, false))
			}
			before := device.writes
			result, err := confirmAndRunRemoval(t, service, worker, ctx, f.account.AccountID, review.ID, keep, "actor-password")
			if scenario == "slow review" || scenario == "unverified" || scenario == "unsupported" || scenario == "stale" || scenario == "revoked" {
				require.Error(t, err)
				require.Equal(t, before, device.writes)
				if scenario != "stale" && scenario != "revoked" {
					result, err = confirmAndRunRemoval(t, service, worker, ctx, f.account.AccountID, review.ID, true, "actor-password")
					require.NoError(t, err)
					require.Equal(t, "completed", result.State)
				}
				return
			}
			require.NoError(t, err)
			if scenario == "crash after clear" {
				// Reproduce the durable state left when the physical call completes,
				// but the process stops before saving the removal outcome.
				require.Equal(t, "uncertain", result.State)
				_, err = f.db.ExecContext(ctx, `UPDATE pool_configuration_operation_target SET state='applying' WHERE operation_id=$1`, review.ID)
				require.NoError(t, err)
				var attemptStatus string
				require.NoError(t, f.db.QueryRowContext(ctx, `SELECT w.status FROM device_write_attempt w JOIN pool_configuration_operation_target t ON t.physical_attempt_id=w.attempt_id WHERE t.operation_id=$1`, review.ID).Scan(&attemptStatus))
				require.Equal(t, "complete", attemptStatus)
				device.readError, device.onWrite = nil, nil
				beforeRecovery := device.writes
				_, err = service.ReviewRetry(ctx, f.account.AccountID, review.ID, []poolconfigurations.AssignmentTarget{target})
				require.NoError(t, err)
				require.Equal(t, "completed", mustPoolRemoval(t, ctx, service, f.account.AccountID, review.ID).State)
				require.Equal(t, beforeRecovery, device.writes, "recovery must not send a second clear")
				return
			}
			if scenario == "readback failure" || scenario == "offline readback recovery" || scenario == "lost acknowledgement" {
				require.Equal(t, "uncertain", result.State)
				reopened, err := service.ReviewRemoval(ctx, f.account.AccountID, target)
				require.NoError(t, err)
				require.Equal(t, review.ID, reopened.ID)
				_, err = service.ReviewAssignment(ctx, f.account.AccountID, config.ID, config.Version, []poolconfigurations.AssignmentTarget{target})
				require.Error(t, err)
				require.Error(t, devicewrite.NewService(f.db).WithHostAttempt(ctx, f.org, miner, 0, commandtype.UpdateMiningPools, func(context.Context) error { t.Fatal("unfinished removal must block other commands"); return nil }))
				beforeRecovery := device.writes
				if scenario == "offline readback recovery" {
					// The device call finished, but readback remains unavailable.
					// Recovery must not need another live read or claim clear success.
					writes := devicewrite.NewService(f.db)
					inspections, err := writes.InspectRecovery(ctx, f.org, miner)
					require.NoError(t, err)
					require.Len(t, inspections, 1)
					require.Equal(t, "complete", inspections[0].Attempt.Status)
					evidence := inspections[0].EvidenceTemplate
					evidence.Operator = "test operator"
					evidence.MaintenanceTermination = "all dispatchers stopped"
					evidence.OwnerTermination = "owning process stopped"
					evidence.TransportQuiescence = "transport drained"
					evidence.DeviceQuiescence = "device settled"
					_, err = writes.ResolveOffline(ctx, evidence, true)
					require.Error(t, err, "manual configuration evidence remains mandatory")
					evidence.ConfigurationVerification = "operator verified configuration through equipment console"
					require.Equal(t, uuid.MustParse(review.ID), evidence.PoolRemovalID)
					for _, wrongID := range []uuid.UUID{uuid.Nil, uuid.New()} {
						wrong := evidence
						wrong.PoolRemovalID = wrongID
						_, err = writes.ResolveOffline(ctx, wrong, true)
						require.Error(t, err, "recovery must bind the exact removal journal")
					}
					preview, err := writes.ResolveOffline(ctx, evidence, false)
					require.NoError(t, err)
					require.True(t, preview.Preview)
					require.Equal(t, "uncertain", mustPoolRemoval(t, ctx, service, f.account.AccountID, review.ID).State)
					_, err = f.db.ExecContext(ctx, `CREATE FUNCTION reject_pool_recovery() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'audit unavailable'; END $$; CREATE TRIGGER reject_pool_recovery BEFORE INSERT ON device_write_recovery FOR EACH ROW EXECUTE FUNCTION reject_pool_recovery()`)
					require.NoError(t, err)
					_, err = writes.ResolveOffline(ctx, evidence, true)
					require.ErrorContains(t, err, "audit unavailable")
					require.Equal(t, "uncertain", mustPoolRemoval(t, ctx, service, f.account.AccountID, review.ID).State)
					_, err = f.db.ExecContext(ctx, `DROP TRIGGER reject_pool_recovery ON device_write_recovery`)
					require.NoError(t, err)
					resolved, err := writes.ResolveOffline(ctx, evidence, true)
					require.NoError(t, err)
					require.Equal(t, evidence.FenceEpoch+1, resolved.FenceEpoch)
					recovered := mustPoolRemoval(t, ctx, service, f.account.AccountID, review.ID)
					require.Equal(t, "failed", recovered.State)
					require.Equal(t, "interrupted", recovered.ResultCode)
					var physical, prior string
					require.NoError(t, f.db.QueryRowContext(ctx, `SELECT a.status,r.prior_status FROM device_write_attempt a JOIN device_write_recovery r USING(attempt_id) WHERE a.attempt_id=$1`, evidence.AttemptID).Scan(&physical, &prior))
					require.Equal(t, "complete", physical, "recovery must preserve physical completion evidence")
					require.Equal(t, "complete", prior)
					assignment, err := sqlc.New(f.db).GetPoolConfigurationAssignment(ctx, sqlc.GetPoolConfigurationAssignmentParams{OrganizationID: f.org, TargetKind: target.Kind, TargetID: target.ID})
					require.NoError(t, err)
					require.False(t, assignment.RetiredAt.Valid)
					require.Equal(t, beforeRecovery, device.writes)
					require.NotNil(t, device.readError)
					_, err = writes.ResolveOffline(ctx, evidence, true)
					require.Error(t, err, "stale recovery cannot resolve twice")
					inspections, err = writes.InspectRecovery(ctx, f.org, miner)
					require.NoError(t, err)
					require.Empty(t, inspections, "ordinary completed attempts must not become recoverable")
					evidence.FenceEpoch = resolved.FenceEpoch
					_, err = writes.ResolveOffline(ctx, evidence, true)
					require.Error(t, err, "even current custody cannot make a completed attempt recoverable without an unfinished removal")
					fresh, err := service.ReviewRemoval(ctx, f.account.AccountID, target)
					require.NoError(t, err)
					require.NotEqual(t, review.ID, fresh.ID)
					return
				}
				device.readError = nil
				device.onWrite = nil
				result, err = confirmAndRunRemoval(t, service, worker, ctx, f.account.AccountID, review.ID, false, "")
				require.NoError(t, err)
				require.Equal(t, beforeRecovery, device.writes)
				if scenario == "lost acknowledgement" {
					require.Equal(t, "uncertain", result.State)
					require.Equal(t, "operator_recovery_required", result.ResultCode)
					operation, err := service.GetOperation(ctx, f.account.AccountID, review.ID)
					require.NoError(t, err)
					require.Equal(t, "operator_recovery_required", operation.Targets[0].ResultCode)
					writes := devicewrite.NewService(f.db)
					inspections, err := writes.InspectRecovery(ctx, f.org, miner)
					require.NoError(t, err)
					require.Len(t, inspections, 1)
					i := inspections[0]
					_, err = writes.ResolveOffline(ctx, devicewrite.RecoveryEvidence{AttemptID: i.Attempt.AttemptID, OrganizationID: f.org, DeviceID: miner, AttemptEpoch: i.Attempt.Epoch, FenceEpoch: i.FenceEpoch, FenceState: i.FenceState, QuarantineOrigin: i.QuarantineOrigin, OwnerInstanceID: i.Attempt.OwnerInstanceID, OperationKind: i.Attempt.OperationKind, PriorStatus: i.Attempt.Status, IdentitySHA256: i.IdentitySHA256, Operator: "test operator", MaintenanceTermination: "test dispatchers stopped", OwnerTermination: "test owner stopped", TransportQuiescence: "test transport drained", DeviceQuiescence: "test device settled", ConfigurationVerification: "test configuration read"}, true)
					require.NoError(t, err)
					recovered, err := service.GetRemoval(ctx, f.account.AccountID, review.ID)
					require.NoError(t, err)
					require.Equal(t, "failed", recovered.State)
					require.Equal(t, "interrupted", recovered.ResultCode)
					fresh, err := service.ReviewRemoval(ctx, f.account.AccountID, target)
					require.NoError(t, err)
					require.NotEqual(t, review.ID, fresh.ID)
					return
				}
				require.Equal(t, "completed", result.State)
			}
			assignments, err := service.GetAssignments(ctx, f.account.AccountID, nil, []int64{miner}, nil)
			require.NoError(t, err)
			if scenario == "failed clear" {
				require.NotEqual(t, "completed", result.State)
				require.Equal(t, config.ID, assignments.Assignments[0].ConfigurationID)
				return
			}
			require.Equal(t, "completed", result.State)
			require.Empty(t, assignments.Assignments[0].ConfigurationID)
			if keep {
				require.Equal(t, before, device.writes)
				require.Len(t, device.pools, 1)
			} else {
				require.Equal(t, before+1, device.writes)
				require.Empty(t, device.pools)
			}
			if scenario == "replay after reassignment" {
				assign()
			}
			before = device.writes
			replay, err := confirmAndRunRemoval(t, service, worker, ctx, f.account.AccountID, review.ID, keep, "")
			require.NoError(t, err)
			require.Equal(t, "completed", replay.State)
			require.Equal(t, before, device.writes)
			if scenario == "replay after reassignment" {
				a, err := service.GetAssignments(ctx, f.account.AccountID, nil, []int64{miner}, nil)
				require.NoError(t, err)
				require.Equal(t, config.ID, a.Assignments[0].ConfigurationID)
			}
			configAfter, err := service.Get(ctx, f.account.AccountID, config.ID)
			require.NoError(t, err)
			require.Equal(t, config.ID, configAfter.ID)
		})
	}
}

func mustPoolRemoval(t *testing.T, ctx context.Context, service *poolconfigurations.Service, accountID, removalID string) poolconfigurations.Removal {
	t.Helper()
	result, err := service.GetRemoval(ctx, accountID, removalID)
	require.NoError(t, err)
	return result
}

func TestBoardPoolRemovalKeepsSiblingAssignmentsAndRejectsMovedInstallations(t *testing.T) {
	for _, moved := range []bool{false, true} {
		t.Run(map[bool]string{false: "preserve siblings", true: "replaced installation"}[moved], func(t *testing.T) {
			f := newPoolStepUpFixture(t)
			module, controller := f.miningModule(t)
			_, err := f.db.ExecContext(t.Context(), `UPDATE hashboard_installation SET metadata=jsonb_build_object('macAddress','02:00:00:00:00:0' || position) WHERE module_id=$1`, module)
			require.NoError(t, err)
			require.NoError(t, f.allocate(t, f.host, f.root, f.account.AccountID, hostedpb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(6, module, 0)))
			_, _, _, cipher, _ := poolFixture(t, f.fixture)
			ctx, err := hostedaccounts.WithRequestAccountID(f.sessionContext(t), f.account.AccountID)
			require.NoError(t, err)
			require.NoError(t, sqlc.New(f.db).EnsureDeviceWriteFences(ctx, []int64{controller}))
			_, err = f.db.ExecContext(ctx, `INSERT INTO container_mining_group_state(device_id,organization_id,observed,observed_at) VALUES($1,$2,'{"supported":true,"enabled":true}',now())`, controller, f.org)
			require.NoError(t, err)
			repo := poolconfigurations.NewRepository(cipher)
			reviewer := poolapplications.NewReviewer(f.pools, poolCapabilities{sdk.CapabilityPoolConfig: true})
			device := &poolDevice{}
			checker := poolapplications.NewChecker(f.db, reviewer, poolMinerGetter{device}, cipher, f.pools)
			worker := poolapplications.NewWorker(f.db, repo, reviewer, poolMinerGetter{device}, cipher).WithRemovals(checker)
			service := poolconfigurations.NewService(f.db, repo, f.pools, reviewer).WithChecker(checker)
			config, err := service.Create(ctx, f.account.AccountID, poolconfigurations.RevisionOptions{Name: "Boards", Slots: []poolconfigurations.SlotOptions{{URL: "stratum+tcp://pool.example", Username: "worker"}}})
			require.NoError(t, err)
			selection := poolconfigurations.BoardSelection{ModuleIDs: []int64{module}}
			review, err := service.ReviewBoards(ctx, f.account.AccountID, config.ID, config.Version, selection)
			require.NoError(t, err)
			require.Len(t, review.Targets, 3)
			for _, target := range review.Targets {
				require.NoError(t, db.WithTransactionNoResult(ctx, f.db, func(q sqlc.Querier) error {
					return repo.Adopt(ctx, q, poolconfigurations.Scope{OrganizationID: f.org, AccountID: f.account.ID}, target, config.ID, config.RevisionID)
				}))
			}
			removal, err := service.ReviewRemoval(ctx, f.account.AccountID, review.Targets[0])
			require.NoError(t, err)
			require.False(t, removal.ClearSupported)
			if moved {
				_, err = f.db.ExecContext(ctx, `UPDATE hashboard_installation SET removed_at=now() WHERE id=$1`, review.Targets[0].ID)
				require.NoError(t, err)
			}
			result, err := confirmAndRunRemoval(t, service, worker, ctx, f.account.AccountID, removal.ID, true, "actor-password")
			if moved {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, "completed", result.State)
			}
			require.Zero(t, device.writes)
			for _, sibling := range review.Targets[1:] {
				assignment, err := sqlc.New(f.db).GetPoolConfigurationAssignment(ctx, sqlc.GetPoolConfigurationAssignmentParams{OrganizationID: f.org, TargetKind: sibling.Kind, TargetID: sibling.ID})
				require.NoError(t, err)
				require.False(t, assignment.RetiredAt.Valid)
			}
		})
	}
}

// Separately model persistent and runtime state: a successful delete or an empty
// stored list does not prove that a controller stopped using its old pool.
type removalTestDevice struct {
	poolContainer
	runtimeOverride   bool
	storedUnsupported bool
}

func (d *removalTestDevice) GetMiningPools(ctx context.Context) ([]interfaces.MinerConfiguredPool, error) {
	if d.runtimeOverride {
		return []interfaces.MinerConfiguredPool{{URL: "stratum+tcp://override.example", Username: "override"}}, nil
	}
	return d.poolDevice.GetMiningPools(ctx)
}

func (d *removalTestDevice) GetStoredMiningPools(ctx context.Context) ([]interfaces.MinerStoredPool, error) {
	if d.storedUnsupported {
		return nil, fleeterror.NewUnimplementedError("firmware lacks durable readback")
	}
	return d.poolDevice.GetStoredMiningPools(ctx)
}

func TestPoolRemovalRequiresLiveClearScopeAndRuntimeReadback(t *testing.T) {
	for _, scenario := range []string{"legacy CT1", "groups enabled", "groups enable after review", "runtime remains", "MDKv1 only"} {
		t.Run(scenario, func(t *testing.T) {
			f := newPoolStepUpFixture(t)
			_, _, _, cipher, _ := poolFixture(t, f.fixture)
			ctx, err := hostedaccounts.WithRequestAccountID(f.sessionContext(t), f.account.AccountID)
			require.NoError(t, err)
			var controller int64
			if scenario == "MDKv1 only" {
				controller = f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
				require.NoError(t, f.allocate(t, f.host, f.root, f.account.AccountID, hostedpb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(5, controller, 0)))
			} else {
				module, id := f.miningModule(t)
				controller = id
				require.NoError(t, f.allocate(t, f.host, f.root, f.account.AccountID, hostedpb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(6, module, 0)))
			}
			device := &removalTestDevice{poolContainer: poolContainer{result: sdk.MiningGroupResult{Supported: true}}}
			device.pools = []dto.MiningPool{{URL: "stratum+tcp://pool.example", Username: "worker"}}
			reviewer := poolapplications.NewReviewer(f.pools, poolCapabilities{sdk.CapabilityPoolConfig: true, sdk.CapabilityPoolClear: true})
			repo := poolconfigurations.NewRepository(cipher)
			checker := poolapplications.NewChecker(f.db, reviewer, poolMinerGetter{device}, cipher, f.pools)
			worker := poolapplications.NewWorker(f.db, repo, reviewer, poolMinerGetter{device}, cipher).WithRemovals(checker)
			service := poolconfigurations.NewService(f.db, repo, f.pools, reviewer).WithChecker(checker)
			config, err := service.Create(ctx, f.account.AccountID, poolconfigurations.RevisionOptions{Name: "Production", Slots: []poolconfigurations.SlotOptions{{URL: "stratum+tcp://pool.example", Username: "worker"}}})
			require.NoError(t, err)
			target := poolconfigurations.AssignmentTarget{Kind: poolconfigurations.TargetDevice, ID: controller, DeviceID: controller}
			require.NoError(t, db.WithTransactionNoResult(ctx, f.db, func(q sqlc.Querier) error {
				return repo.Adopt(ctx, q, poolconfigurations.Scope{OrganizationID: f.org, AccountID: f.account.ID}, target, config.ID, config.RevisionID)
			}))
			if scenario == "groups enabled" {
				device.result.Enabled = true
			}
			if scenario == "MDKv1 only" {
				device.storedUnsupported = true
			}
			review, err := service.ReviewRemoval(ctx, f.account.AccountID, target)
			require.NoError(t, err)
			if scenario == "MDKv1 only" {
				require.Equal(t, "verified", review.VerificationState, "legacy runtime verification remains available")
				require.False(t, review.ClearSupported, "missing durable readback must gate clearing during review")
			} else if scenario != "groups enabled" {
				require.True(t, review.ClearSupported)
				require.Equal(t, "verified", review.VerificationState)
			}
			if scenario == "groups enable after review" {
				device.result.Enabled = true
			}
			if scenario == "runtime remains" {
				device.onWrite = func() { device.runtimeOverride = true }
			}
			result, err := confirmAndRunRemoval(t, service, worker, ctx, f.account.AccountID, review.ID, false, "actor-password")
			if scenario == "groups enabled" || scenario == "MDKv1 only" {
				require.Error(t, err)
				require.Zero(t, device.writes)
				result, err = confirmAndRunRemoval(t, service, worker, ctx, f.account.AccountID, review.ID, true, "actor-password")
				require.NoError(t, err)
				require.Equal(t, "completed", result.State)
				require.Len(t, device.pools, 1)
				return
			}
			require.NoError(t, err)
			switch scenario {
			case "legacy CT1":
				require.Equal(t, "completed", result.State)
				require.Empty(t, device.pools)
				require.Equal(t, 1, device.writes)
			case "groups enable after review":
				require.Equal(t, "failed", result.State)
				require.Zero(t, device.writes)
			case "runtime remains":
				require.Equal(t, "uncertain", result.State)
				require.Empty(t, device.pools)
				result, err = confirmAndRunRemoval(t, service, worker, ctx, f.account.AccountID, review.ID, false, "")
				require.NoError(t, err)
				require.Equal(t, "failed", result.State)
				require.Equal(t, "pools_remain", result.ResultCode)
				require.Equal(t, 1, device.writes, "recovery must only observe")
			}
			assignment, err := sqlc.New(f.db).GetPoolConfigurationAssignment(ctx, sqlc.GetPoolConfigurationAssignmentParams{OrganizationID: f.org, TargetKind: target.Kind, TargetID: target.ID})
			require.NoError(t, err)
			require.Equal(t, scenario == "legacy CT1", assignment.RetiredAt.Valid)
		})
	}
}

// A user confirmation only accepts intent. Tests explicitly tick the same worker
// used in production before inspecting hardware or final operation state.
func confirmAndRunRemoval(t *testing.T, service *poolconfigurations.Service, worker *poolapplications.Worker, ctx context.Context, accountID, id string, keep bool, password string) (poolconfigurations.Removal, error) {
	t.Helper()
	saved, err := service.ConfirmReview(ctx, accountID, id, poolconfigurations.Confirmation{Password: password, KeepDeviceSettings: keep})
	if err != nil {
		return poolconfigurations.Removal{}, err
	}
	require.NotNil(t, saved.Operation)
	r, err := service.GetRemoval(ctx, accountID, id)
	if err != nil {
		return r, err
	}
	if saved.Replay && (r.State == "uncertain" || r.State == "applying") {
		_, err = service.ReviewRetry(ctx, accountID, id, []poolconfigurations.AssignmentTarget{r.Target})
		if err != nil {
			return r, err
		}
	} else if r.State == "pending" {
		require.NotNil(t, worker)
		info, err := session.GetInfo(ctx)
		if err != nil {
			return r, err
		}
		if err := worker.Execute(ctx, info.OrganizationID, id, r.Target); err != nil {
			return r, err
		}
	}
	return service.GetRemoval(ctx, accountID, id)
}
