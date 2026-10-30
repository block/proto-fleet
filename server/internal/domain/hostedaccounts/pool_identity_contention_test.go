package hostedaccounts_test

import (
	"context"
	"testing"
	"time"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	hostedpb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/poolapplications"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

func TestPoolWorkerReleasesIdentityBucketsBeforeDeviceIO(t *testing.T) {
	for _, scenario := range []string{"active", "canceled while adapter runs"} {
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
			other := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
			// Distinct physical identities, deliberately sharing an admission bucket.
			_, err = f.db.ExecContext(ctx, `UPDATE discovered_device SET ip_address='192.0.2.2' WHERE id=(SELECT discovered_device_id FROM device WHERE id=$1)`, other)
			require.NoError(t, err)
			_, err = f.db.ExecContext(ctx, `UPDATE device SET serial_number=(
			 SELECT 'unrelated-'||n FROM generate_series(1,10000) n
			 WHERE (hashtextextended('org:'||$1::bigint::text||':serial:unrelated-'||n,0)&255) IN
			 (SELECT hashtextextended(k,0)&255 FROM unnest(device_write_identity_keys($2)) k)
			 LIMIT 1) WHERE id=$3`, f.org, miner, other)
			require.NoError(t, err)
			var overlaps, collides bool
			require.NoError(t, f.db.QueryRowContext(ctx, `SELECT device_write_identity_keys($1) && device_write_identity_keys($2), EXISTS(
			 SELECT 1 FROM unnest(device_write_identity_keys($1)) a,unnest(device_write_identity_keys($2)) b
			 WHERE (hashtextextended(a,0)&255)=(hashtextextended(b,0)&255))`, miner, other).Scan(&overlaps, &collides))
			require.False(t, overlaps)
			require.True(t, collides)
			require.NoError(t, f.allocate(t, f.host, f.root, f.account.AccountID, hostedpb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(5, miner, 0)))
			target := poolconfigurations.AssignmentTarget{Kind: poolconfigurations.TargetDevice, ID: miner, DeviceID: miner}
			review, err := service.ReviewAssignment(ctx, f.account.AccountID, config.ID, config.Version, []poolconfigurations.AssignmentTarget{target})
			require.NoError(t, err)
			saved, err := service.ConfirmReview(ctx, f.account.AccountID, review.ID, "actor-password")
			require.NoError(t, err)
			started, resume := make(chan struct{}), make(chan struct{})
			device := &poolDevice{onWrite: func() { close(started); <-resume }}
			worker := poolapplications.NewWorker(f.db, repo, reviewer, poolMinerGetter{device}, cipher)
			workCtx, cancelWork := context.WithCancel(t.Context())
			defer cancelWork()
			done := make(chan error, 1)
			go func() { done <- worker.Execute(workCtx, f.org, saved.Operation.ID, target) }()
			defer func() {
				close(resume)
				select {
				case err := <-done:
					require.NoError(t, err)
				case <-time.After(10 * time.Second):
					t.Error("worker did not finish")
				}
			}()
			select {
			case <-started:
			case <-time.After(5 * time.Second):
				t.Fatal("worker did not dispatch")
			}
			if scenario == "canceled while adapter runs" {
				cancelWork()
			}

			admitCtx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()
			attempt, err := devicewrite.NewService(f.db).BeginHost(admitCtx, f.org, other, 0)
			require.NoError(t, err, "unrelated device admission must not wait for pool hardware I/O")
			require.NoError(t, attempt.Finish(t.Context(), nil))

			// The individual device's identity and route must remain locked, even
			// after cancellation, until the context-ignoring adapter actually returns.
			for _, mutation := range []string{
				`UPDATE device SET serial_number='replacement' WHERE id=$1`,
				`UPDATE discovered_device SET ip_address='192.0.2.3' WHERE id=(SELECT discovered_device_id FROM device WHERE id=$1)`,
			} {
				mutationCtx, cancelMutation := context.WithTimeout(t.Context(), 200*time.Millisecond)
				_, err := f.db.ExecContext(mutationCtx, mutation, miner)
				require.Error(t, err)
				require.ErrorIs(t, mutationCtx.Err(), context.DeadlineExceeded)
				cancelMutation()
			}
			var identifier string
			require.NoError(t, f.db.QueryRowContext(ctx, `SELECT device_identifier FROM device WHERE id=$1`, miner).Scan(&identifier))
			tx, err := f.db.BeginTx(ctx, nil)
			require.NoError(t, err)
			defer tx.Rollback()
			require.Error(t, devicewrite.PrepareDeletion(ctx, sqlc.New(tx), f.org, []string{identifier}), "active device must not be retired")
			require.NoError(t, tx.Rollback())
		})
	}
}

func TestPoolReviewIdentityLockingAndAdmissionRecheck(t *testing.T) {
	for _, scenario := range []string{"ordinary review retains buckets", "dispatch rechecks after binding wait"} {
		t.Run(scenario, func(t *testing.T) {
			f := newPoolStepUpFixture(t)
			ctx, err := hostedaccounts.WithRequestAccountID(f.sessionContext(t), f.account.AccountID)
			require.NoError(t, err)
			miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
			require.NoError(t, f.allocate(t, f.host, f.root, f.account.AccountID, hostedpb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(5, miner, 0)))
			reviewer := poolapplications.NewReviewer(f.pools, poolCapabilities{sdk.CapabilityPoolConfig: true})
			writes := devicewrite.NewService(f.db)
			admitted := scenario == "dispatch rechecks after binding wait"
			if admitted {
				attempt, err := writes.BeginHost(ctx, f.org, miner, 0)
				require.NoError(t, err)
				defer func() { require.NoError(t, attempt.Finish(t.Context(), nil)) }()
				ctx = attempt.Context(ctx)
			}
			blocker, err := f.db.BeginTx(ctx, nil)
			require.NoError(t, err)
			defer blocker.Rollback()
			var blockerPID int
			require.NoError(t, blocker.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&blockerPID))
			_, err = sqlc.New(blocker).LockMinerBindingDevice(ctx, miner)
			require.NoError(t, err)
			workCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() {
				review := func(ctx context.Context, q sqlc.Querier) error {
					_, err := reviewer.ReviewTargets(ctx, q, poolconfigurations.Scope{OrganizationID: f.org, AccountID: f.account.ID}, f.user,
						[]poolconfigurations.AssignmentTarget{{Kind: poolconfigurations.TargetDevice, ID: miner, DeviceID: miner}},
						[]poolconfigurations.Endpoint{{URL: "stratum+tcp://pool.example", Username: "payout"}})
					return err
				}
				if admitted {
					done <- writes.WithBindingTransaction(workCtx, review)
				} else {
					done <- db.WithTransactionNoResult(workCtx, f.db, func(q sqlc.Querier) error { return review(workCtx, q) })
				}
			}()
			require.Eventually(t, func() bool {
				var waiting bool
				err := f.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database()
				 AND $1=ANY(pg_blocking_pids(pid)) AND query LIKE '-- name: LockMinerBindingDevice%')`, blockerPID).Scan(&waiting)
				return err == nil && waiting
			}, 5*time.Second, 10*time.Millisecond)
			if admitted {
				// The initial admission was valid. Change only its ownership epoch
				// while it waits, leaving the identity comparison unchanged.
				_, err = blocker.ExecContext(ctx, `UPDATE device_write_fence SET epoch=epoch+1 WHERE device_id=$1`, miner)
				require.NoError(t, err)
			} else {
				keys, err := sqlc.New(f.db).GetDeviceWriteIdentityKeys(ctx, miner)
				require.NoError(t, err)
				lockCtx, cancelLock := context.WithTimeout(ctx, 200*time.Millisecond)
				require.Error(t, sqlc.New(f.db).LockDeviceWriteIdentities(lockCtx, keys), "ordinary review must retain identity locks")
				require.ErrorIs(t, lockCtx.Err(), context.DeadlineExceeded)
				cancelLock()
			}
			require.NoError(t, blocker.Commit())
			if admitted {
				require.ErrorIs(t, <-done, devicewrite.ErrStaleBinding, "admission must be rechecked after acquiring the binding and fence")
			} else {
				require.NoError(t, <-done)
			}
		})
	}
}
