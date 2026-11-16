package hostedaccounts_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	hostedpb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/poolapplications"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

type poolCapabilities map[string]bool

func (c poolCapabilities) GetRawCapabilitiesForDevice(context.Context, string, string, string) sdk.Capabilities {
	return sdk.Capabilities(c)
}

func TestPoolEquipmentReviewBindsWorkerInputsAndLiveOwnership(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	require.NoError(t, f.allocate(t, f.host, f.root, account.AccountID, hostedpb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(5, miner, 0)))
	pools, _, _, cipher, ctx := poolFixture(t, f)
	ctx, err := hostedaccounts.WithRequestAccountID(ctx, account.AccountID)
	require.NoError(t, err)
	scope := poolconfigurations.Scope{OrganizationID: f.org, AccountID: account.ID}
	repo := poolconfigurations.NewRepository(cipher)
	service := poolconfigurations.NewService(f.db, repo, pools, nil)
	config, err := service.Create(ctx, account.AccountID, poolconfigurations.RevisionOptions{Name: "Production", Slots: []poolconfigurations.SlotOptions{{URL: "stratum+tcp://pool.example", Username: "payout", WorkerTemplate: "{miner_name}"}}})
	require.NoError(t, err)
	target := poolconfigurations.AssignmentTarget{Kind: poolconfigurations.TargetDevice, ID: miner, DeviceID: miner}
	caps := poolCapabilities{sdk.CapabilityPoolConfig: true, sdk.CapabilityPoolPriority: true}
	reviewer := poolapplications.NewReviewer(pools, caps)
	review := func() (poolconfigurations.Review, error) {
		return db.WithTransaction(ctx, f.db, func(q sqlc.Querier) (poolconfigurations.Review, error) {
			return repo.ReviewAssignment(ctx, q, scope, f.user, config.ID, config.Version, []poolconfigurations.AssignmentTarget{target}, reviewer)
		})
	}
	reviewed, err := review()
	require.NoError(t, err)
	confirm := func(reviewID string) (poolconfigurations.ReviewedSave, error) {
		return db.WithTransaction(ctx, f.db, func(q sqlc.Querier) (poolconfigurations.ReviewedSave, error) {
			return repo.ConfirmReview(ctx, q, scope, f.user, reviewID, reviewer, nil)
		})
	}
	_, err = f.db.ExecContext(t.Context(), `UPDATE device SET worker_name='renamed-worker' WHERE id=$1`, miner)
	require.NoError(t, err)
	_, err = confirm(reviewed.ID)
	require.True(t, fleeterror.IsFailedPreconditionError(err), "the reviewed worker suffix cannot change between review and confirmation")
	reviewed, err = review()
	require.NoError(t, err)
	saved, err := confirm(reviewed.ID)
	require.NoError(t, err)
	var encoded string
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT reviewed_binding FROM pool_configuration_operation_target WHERE operation_id=$1`, saved.Operation.ID).Scan(&encoded))
	var binding poolapplications.Binding
	require.NoError(t, json.Unmarshal([]byte(encoded), &binding))
	require.Equal(t, []string{"payout.renamed-worker"}, binding.Usernames)
	require.GreaterOrEqual(t, binding.Device.Epoch, int64(0))
	require.NotEmpty(t, binding.Ownership)
	reviewed, err = review()
	require.NoError(t, err)
	require.NoError(t, f.allocate(t, f.host, f.root, account.AccountID, hostedpb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_RECALL, selected(5, miner, 1)))
	_, err = confirm(reviewed.ID)
	require.True(t, fleeterror.IsForbiddenError(err), "queued assignment cannot be accepted after ownership changes")
}

func TestPoolEquipmentReviewContainerModeAndFirmwareCompatibility(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	module, controller := f.miningModule(t)
	require.NoError(t, f.allocate(t, f.host, f.root, account.AccountID, hostedpb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(6, module, 0)))
	pools, _, _, _, ctx := poolFixture(t, f)
	ctx, err := hostedaccounts.WithRequestAccountID(ctx, account.AccountID)
	require.NoError(t, err)
	reviewer := poolapplications.NewReviewer(pools, poolCapabilities{sdk.CapabilityPoolConfig: true, sdk.CapabilityPoolPriority: true})
	slots := []poolconfigurations.Endpoint{{URL: "stratum+tcp://pool.example", Username: "payout"}}
	check := func() error {
		return db.WithTransactionNoResult(ctx, f.db, func(q sqlc.Querier) error {
			_, err := reviewer.ReviewTargets(ctx, q, poolconfigurations.Scope{OrganizationID: f.org, AccountID: account.ID}, f.user, []poolconfigurations.AssignmentTarget{{Kind: poolconfigurations.TargetDevice, ID: controller, DeviceID: controller}}, slots)
			return err
		})
	}
	_, err = f.db.ExecContext(t.Context(), `UPDATE container_mining_group_state SET observed='{}',observed_at=NULL WHERE device_id=$1`, controller)
	require.NoError(t, err)
	require.Error(t, check(), "unknown mode is not a disabled mode")
	_, err = f.db.ExecContext(t.Context(), `UPDATE container_mining_group_state SET observed='{"supported":true,"enabled":false}',observed_at=now() WHERE device_id=$1 AND organization_id=$2`, controller, f.org)
	require.NoError(t, err)
	require.NoError(t, check())
	slots = append(slots, slots[0])
	require.Error(t, check(), "current firmware forbids duplicate enabled destinations")
	slots = slots[:1]
	slots[0].URL = "stratum+ws://pool.example"
	require.Error(t, check(), "library-valid settings can still be incompatible with CT1")
	slots[0].URL = "stratum+tcp://pool.example"
	_, err = f.db.ExecContext(t.Context(), `UPDATE container_mining_group_state SET observed='{"supported":true,"enabled":true}' WHERE device_id=$1`, controller)
	require.NoError(t, err)
	require.Error(t, check(), "whole-container assignments cannot overwrite mining groups")
}

func TestPoolEquipmentReviewRetainsFenceAndModeUntilCommit(t *testing.T) {
	for _, mutation := range []string{"fence", "mode"} {
		t.Run(mutation, func(t *testing.T) {
			f := setup(t)
			account := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
			module, controller := f.miningModule(t)
			require.NoError(t, f.allocate(t, f.host, f.root, account.AccountID, hostedpb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(6, module, 0)))
			pools, _, _, _, ctx := poolFixture(t, f)
			ctx, err := hostedaccounts.WithRequestAccountID(ctx, account.AccountID)
			require.NoError(t, err)
			_, err = f.db.ExecContext(ctx, `UPDATE container_mining_group_state SET observed='{"supported":true,"enabled":false}',observed_at=now() WHERE device_id=$1 AND organization_id=$2`, controller, f.org)
			require.NoError(t, err)
			reviewer := poolapplications.NewReviewer(pools, poolCapabilities{sdk.CapabilityPoolConfig: true, sdk.CapabilityPoolPriority: true})
			require.NoError(t, sqlc.New(f.db).EnsureDeviceWriteFences(ctx, []int64{controller}))
			tx, err := f.db.BeginTx(ctx, nil)
			require.NoError(t, err)
			defer tx.Rollback()
			var backend int
			require.NoError(t, tx.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&backend))
			_, err = reviewer.ReviewTargets(ctx, sqlc.New(tx), poolconfigurations.Scope{OrganizationID: f.org, AccountID: account.ID}, f.user, []poolconfigurations.AssignmentTarget{{Kind: poolconfigurations.TargetDevice, ID: controller, DeviceID: controller}}, []poolconfigurations.Endpoint{{URL: "stratum+tcp://pool.example", Username: "payout"}})
			require.NoError(t, err)
			mutateCtx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() {
				query := `UPDATE device_write_fence SET epoch=epoch+1 WHERE device_id=$1`
				if mutation == "mode" {
					query = `UPDATE container_mining_group_state SET observed='{"supported":true,"enabled":true}' WHERE device_id=$1`
				}
				_, err := f.db.ExecContext(mutateCtx, query, controller)
				done <- err
			}()
			require.Eventually(t, func() bool {
				var blocked bool
				err := f.db.QueryRowContext(t.Context(), `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND $1=ANY(pg_blocking_pids(pid)))`, backend).Scan(&blocked)
				return err == nil && blocked
			}, time.Second, 10*time.Millisecond)
			require.NoError(t, tx.Commit())
			require.NoError(t, <-done)
		})
	}
}

func TestPoolEquipmentReviewRejectsForeignScopeBeforePhysicalLocks(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	foreign := f
	require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO organization(org_id,name) VALUES($1,'Other') RETURNING id`, uuid.NewString()).Scan(&foreign.org))
	miner := foreign.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	tx, err := f.db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	defer tx.Rollback()
	_, err = tx.ExecContext(t.Context(), `SELECT id FROM device WHERE id=$1 FOR UPDATE`, miner)
	require.NoError(t, err)
	pools, _, _, _, ctx := poolFixture(t, f)
	ctx, err = hostedaccounts.WithRequestAccountID(ctx, account.AccountID)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	reviewer := poolapplications.NewReviewer(pools, poolCapabilities{sdk.CapabilityPoolConfig: true})
	err = db.WithTransactionNoResult(ctx, f.db, func(q sqlc.Querier) error {
		_, err := reviewer.ReviewTargets(ctx, q, poolconfigurations.Scope{OrganizationID: f.org, AccountID: account.ID}, f.user, []poolconfigurations.AssignmentTarget{{Kind: poolconfigurations.TargetDevice, ID: miner, DeviceID: miner}}, []poolconfigurations.Endpoint{{URL: "stratum+tcp://pool.example", Username: "payout"}})
		return err
	})
	require.True(t, fleeterror.IsForbiddenError(err), "must refuse another organization's target without waiting for its device lock")
}
