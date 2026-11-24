package hostedaccounts_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/jackc/pgx/v5/pgconn"

	hostedpb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/poolapplications"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

type poolContainer struct {
	poolDevice
	observations int
	result       sdk.MiningGroupResult
	readError    error
	observe      func(context.Context) (sdk.MiningGroupResult, error)
}

func (m *poolContainer) MiningGroups(ctx context.Context, request sdk.MiningGroupRequest) (sdk.MiningGroupResult, error) {
	if request.Action != "observe" {
		return sdk.MiningGroupResult{}, errors.New("readiness must not mutate mining groups")
	}
	m.observations++
	if m.observe != nil {
		return m.observe(ctx)
	}
	return m.result, m.readError
}

func TestPoolContainerRefreshAndUpdateUseSelectedAccountAuthority(t *testing.T) {
	for _, scenario := range []string{"disabled groups", "enabled groups", "unavailable", "permission denied"} {
		t.Run(scenario, func(t *testing.T) {
			f := newPoolStepUpFixture(t)
			_, _, _, cipher, _ := poolFixture(t, f.fixture)
			ctx, err := hostedaccounts.WithRequestAccountID(f.sessionContext(t), f.account.AccountID)
			require.NoError(t, err)
			module, controller := f.miningModule(t)
			require.NoError(t, f.allocate(t, f.host, f.root, f.account.AccountID, hostedpb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(6, module, 0)))
			device := &poolContainer{result: sdk.MiningGroupResult{Supported: true}}
			if scenario == "enabled groups" {
				device.result.Enabled = true
			}
			if scenario == "unavailable" {
				device.readError = errors.New("private-device-error")
			}
			getter := poolMinerGetter{device}
			reviewer := poolapplications.NewReviewer(f.pools, poolCapabilities{sdk.CapabilityPoolConfig: true, sdk.CapabilityPoolPriority: true})
			repo := poolconfigurations.NewRepository(cipher)
			service := poolconfigurations.NewService(f.db, repo, f.pools, reviewer).WithReadiness(poolapplications.NewReadiness(f.db, reviewer, getter))
			configuration, err := service.Create(ctx, f.account.AccountID, poolconfigurations.RevisionOptions{Name: "Container pools", Slots: []poolconfigurations.SlotOptions{{URL: "stratum+tcp://primary.example", Username: "payout"}, {URL: "stratum+tcp://backup.example", Username: "backup"}}})
			require.NoError(t, err)
			if scenario == "permission denied" {
				require.NoError(t, f.svc.ConfigureChild(t.Context(), f.host, f.org, f.account.AccountID, hostedaccounts.AllCapabilities&^hostedaccounts.ChangePools, false))
			}
			target := poolconfigurations.AssignmentTarget{Kind: poolconfigurations.TargetDevice, ID: controller, DeviceID: controller}
			review, err := service.ReviewAssignment(ctx, f.account.AccountID, configuration.ID, configuration.Version, []poolconfigurations.AssignmentTarget{target})
			if scenario != "disabled groups" {
				require.Error(t, err)
				require.NotContains(t, err.Error(), "private-device-error")
				require.Zero(t, device.writes)
				if scenario == "permission denied" {
					require.Zero(t, device.observations)
				} else {
					require.Equal(t, 1, device.observations)
				}
				return
			}
			require.NoError(t, err, "a hosted user need not ask an operator to refresh container mode")
			saved, err := service.ConfirmReview(ctx, f.account.AccountID, review.ID, poolconfigurations.Confirmation{Password: "actor-password"})
			require.NoError(t, err)
			worker := poolapplications.NewWorker(f.db, repo, reviewer, getter, cipher)
			require.NoError(t, worker.Execute(t.Context(), f.org, saved.Operation.ID, target))
			require.Equal(t, 2, device.observations, "refresh mode again before dispatch")
			require.Equal(t, 1, device.writes)
			require.Len(t, device.pools, 2, "the whole-container update includes the backup")
			result, err := service.GetOperation(ctx, f.account.AccountID, saved.Operation.ID)
			require.NoError(t, err)
			require.Equal(t, "verified", result.Targets[0].State)
		})
	}
}

func TestPoolReadinessDeadlineReleasesLocksAndDoesNotSaveReview(t *testing.T) {
	for _, earlierDeadline := range []bool{false, true} {
		name := "server deadline"
		if earlierDeadline {
			name = "earlier caller deadline"
		}
		t.Run(name, func(t *testing.T) {
			f := newPoolStepUpFixture(t)
			_, _, _, cipher, _ := poolFixture(t, f.fixture)
			ctx, err := hostedaccounts.WithRequestAccountID(f.sessionContext(t), f.account.AccountID)
			require.NoError(t, err)
			module, controller := f.miningModule(t)
			require.NoError(t, f.allocate(t, f.host, f.root, f.account.AccountID, hostedpb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(6, module, 0)))
			started := make(chan context.Context, 1)
			device := &poolContainer{observe: func(ctx context.Context) (sdk.MiningGroupResult, error) {
				started <- ctx
				<-ctx.Done() // A connected node accepts the request but never acknowledges it.
				return sdk.MiningGroupResult{}, ctx.Err()
			}}
			reviewer := poolapplications.NewReviewer(f.pools, poolCapabilities{sdk.CapabilityPoolConfig: true})
			service := poolconfigurations.NewService(f.db, poolconfigurations.NewRepository(cipher), f.pools, reviewer).
				WithReadiness(poolapplications.NewReadiness(f.db, reviewer, poolMinerGetter{device}))
			configuration, err := service.Create(ctx, f.account.AccountID, poolconfigurations.RevisionOptions{Name: "Container pools", Slots: []poolconfigurations.SlotOptions{{URL: "stratum+tcp://pool.example", Username: "payout"}}})
			require.NoError(t, err)
			ctx, cancel := context.WithCancel(ctx)
			defer cancel()
			if earlierDeadline {
				var stop context.CancelFunc
				ctx, stop = context.WithTimeout(ctx, 2*time.Second)
				defer stop()
			}
			done := make(chan error, 1)
			finished := make(chan struct{})
			go func() {
				defer close(finished)
				_, err := service.ReviewAssignment(ctx, f.account.AccountID, configuration.ID, configuration.Version, []poolconfigurations.AssignmentTarget{{Kind: poolconfigurations.TargetDevice, ID: controller, DeviceID: controller}})
				done <- err
			}()
			// Always stop and join the read before the database fixture is cleaned up,
			// including when the regression fails because no server deadline exists.
			defer func() {
				cancel()
				select {
				case <-finished:
				case <-time.After(5 * time.Second):
					t.Error("readiness did not stop after cancellation")
				}
			}()
			var observation context.Context
			select {
			case observation = <-started:
			case <-time.After(5 * time.Second):
				t.Fatal("readiness observation did not start")
			}
			deadline, ok := observation.Deadline()
			require.True(t, ok, "readiness must impose a server deadline without relying on its caller")
			require.LessOrEqual(t, time.Until(deadline), 30*time.Second)
			if earlierDeadline {
				callerDeadline, _ := ctx.Deadline()
				require.Equal(t, callerDeadline, deadline, "a shorter caller deadline must be preserved")
			}
			// This separate connection confirms the lock is held during the observation.
			tx, err := f.db.BeginTx(t.Context(), nil)
			require.NoError(t, err)
			defer tx.Rollback()
			_, err = tx.ExecContext(t.Context(), `SELECT id FROM device WHERE id=$1 FOR UPDATE NOWAIT`, controller)
			var pgErr *pgconn.PgError
			require.ErrorAs(t, err, &pgErr)
			require.Equal(t, "55P03", pgErr.Code)
			require.NoError(t, tx.Rollback())
			if !earlierDeadline {
				// The caller deadline case proves the expiry result. Both cases end
				// the same derived context, so this case does not wait 30 seconds.
				cancel()
			}
			select {
			case err = <-done:
			case <-time.After(10 * time.Second):
				t.Fatal("readiness did not return")
			}
			require.Error(t, err)
			if earlierDeadline {
				require.True(t, fleeterror.IsUnavailableError(err))
				require.ErrorIs(t, err, context.DeadlineExceeded)
				require.Contains(t, err.Error(), "try reviewing again")
			}
			require.Zero(t, device.writes)
			require.Equal(t, 1, device.observations)
			// Reacquire the identity, device, fence and readiness locks on a new transaction.
			lockCtx, stop := context.WithTimeout(t.Context(), 2*time.Second)
			defer stop()
			tx, err = f.db.BeginTx(lockCtx, nil)
			require.NoError(t, err)
			defer tx.Rollback()
			q := sqlc.New(tx)
			keys, err := q.GetDeviceWriteIdentityKeys(lockCtx, controller)
			require.NoError(t, err)
			require.NoError(t, q.LockDeviceWriteIdentities(lockCtx, keys))
			_, err = q.LockMinerBindingDevice(lockCtx, controller)
			require.NoError(t, err)
			_, err = q.LockDeviceWriteFences(lockCtx, []int64{controller})
			require.NoError(t, err)
			require.NoError(t, q.EnsureMiningGroupState(lockCtx, sqlc.EnsureMiningGroupStateParams{DeviceID: controller, OrganizationID: f.org}))
			_, err = q.LockMiningGroupState(lockCtx, sqlc.LockMiningGroupStateParams{DeviceID: controller, OrganizationID: f.org})
			require.NoError(t, err)
			require.NoError(t, tx.Rollback())
			var reviews, assignments, operations, activeReads int
			require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT (SELECT count(*) FROM pool_configuration_review), (SELECT count(*) FROM pool_configuration_assignment), (SELECT count(*) FROM pool_configuration_operation), (SELECT count(*) FROM device_write_attempt WHERE status IN ('running','unknown'))`).Scan(&reviews, &assignments, &operations, &activeReads))
			require.Zero(t, reviews)
			require.Zero(t, assignments)
			require.Zero(t, operations)
			require.Zero(t, activeReads, "the observation admission must also finish")
		})
	}
}
