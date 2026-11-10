package mininggroups

import (
	"context"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/poolapplications"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestBoardAdmissionOrdersOrganizationBeforeCollidingIdentity(t *testing.T) {
	for _, removal := range []bool{false, true} {
		name := "configuration"
		if removal {
			name = "removal"
		}
		t.Run(name, func(t *testing.T) {
			ctx, f, miner, service, config, boards, confirm := newActivationPoolFixture(t)
			_, err := f.service.Activate(ctx, f.target.DeviceIdentifier, uuid.NewString())
			require.NoError(t, err)
			for range 8 {
				if active(f.state(t).Status) {
					f.reconcile(t, ctx)
				}
			}
			require.Equal(t, statusComplete, f.state(t).Status)
			ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
			defer cancel()
			activation, err := f.conn.BeginTx(ctx, nil)
			require.NoError(t, err)
			defer activation.Rollback()
			var activationPID int
			require.NoError(t, activation.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&activationPID))
			// Distinct physical identities can share a lock stripe. Hold activation's
			// organization lock first, then request a key colliding with board admission.
			var collision string
			require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT 'serial:other-controller-'||n FROM generate_series(1,100000) n WHERE (hashtextextended('serial:other-controller-'||n,0)&255) IN (SELECT hashtextextended(k,0)&255 FROM unnest(device_write_identity_keys($1)) k) LIMIT 1`, f.target.DeviceID).Scan(&collision))
			var run func() error
			preflight, leavePreflight := make(chan struct{}), make(chan struct{})
			if removal {
				password := ""
				edited, err := service.ReviewEdit(ctx, "", config.ID, config.Version, poolconfigurations.RevisionOptions{Name: "Updated", Slots: []poolconfigurations.SlotOptions{{ID: config.Slots[0].ID, URL: "stratum+tcp://updated.example:3333", Username: "updated", Password: &password}}})
				require.NoError(t, err)
				saved := confirm(edited)
				require.NoError(t, f.service.ExecutePoolConfiguration(ctx, f.target.OrganizationID, saved.Operation.ID, f.target.DeviceID))
				for range 20 {
					if active(f.state(t).Status) {
						f.reconcile(t, ctx)
					}
				}
				require.Equal(t, statusComplete, f.state(t).Status)
				reviewer := poolapplications.NewReviewer(f.service.pools, removalCapabilities{})
				checker := poolapplications.NewChecker(f.conn, reviewer, miner, f.service.secrets, f.service.pools)
				f.service.WithPoolRemovals(checker)
				service.WithChecker(checker)
				var installation int64
				require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT id FROM hashboard_installation WHERE hashboard_id=$1 AND removed_at IS NULL`, boards[0]).Scan(&installation))
				review, err := service.ReviewRemoval(ctx, "", poolconfigurations.AssignmentTarget{Kind: poolconfigurations.TargetInstallation, ID: installation, DeviceID: f.target.DeviceID})
				require.NoError(t, err)
				p, err := f.service.pools.AuthorizePoolConfiguration(ctx, sqlc.New(f.conn), "", poolconfigurations.Read)
				require.NoError(t, err)
				require.NoError(t, db.WithTransactionNoResult(ctx, f.conn, func(q sqlc.Querier) error { return checker.EnqueueRemoval(ctx, q, p, review.ID, false) }))
				row, err := sqlc.New(f.conn).GetPoolRemoval(ctx, sqlc.GetPoolRemovalParams{OrganizationID: p.Scope.OrganizationID, ActorID: p.ActorID, RemovalID: uuid.MustParse(review.ID)})
				require.NoError(t, err)
				first := true
				run = func() error {
					return f.service.BeginBoardRemoval(devicewrite.WithPoolOperation(ctx, row.RemovalID), p, row, func(ctx context.Context, q sqlc.Querier) error {
						if first {
							first = false
							close(preflight)
							select {
							case <-leavePreflight:
							case <-ctx.Done():
								return ctx.Err()
							}
						}
						return checker.ValidateRemoval(ctx, q, p, row, true)
					})
				}
			} else {
				password := ""
				review, err := service.ReviewEdit(ctx, "", config.ID, config.Version, poolconfigurations.RevisionOptions{Name: "Updated", Slots: []poolconfigurations.SlotOptions{{ID: config.Slots[0].ID, URL: "stratum+tcp://updated.example:3333", Username: "updated", Password: &password}}})
				require.NoError(t, err)
				saved := confirm(review)
				run = func() error {
					return f.service.ExecutePoolConfiguration(ctx, f.target.OrganizationID, saved.Operation.ID, f.target.DeviceID)
				}
			}
			done := make(chan error, 1)
			locked := make(chan error, 1)
			lockOrganization := func() {
				_, err := sqlc.New(activation).LockPoolConfigurationImportOrganization(ctx, f.target.OrganizationID)
				locked <- err
			}
			if removal {
				go func() { done <- run() }()
				select {
				case <-preflight:
				case <-ctx.Done():
					t.Fatal("preflight did not start")
				}
				go lockOrganization()
				require.Eventually(t, func() bool {
					var waiting bool
					err := f.conn.QueryRowContext(ctx, `SELECT cardinality(pg_blocking_pids($1))>0`, activationPID).Scan(&waiting)
					return err == nil && waiting
				}, 3*time.Second, 10*time.Millisecond)
				close(leavePreflight)
				require.NoError(t, <-locked)
			} else {
				lockOrganization()
				require.NoError(t, <-locked)
				go func() { done <- run() }()
			}
			// Observe actual lock contention, rather than relying on goroutine timing.
			require.Eventually(t, func() bool {
				var waiting bool
				err := f.conn.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND $1=ANY(pg_blocking_pids(pid)))`, activationPID).Scan(&waiting)
				return err == nil && waiting
			}, 3*time.Second, 10*time.Millisecond)
			collisionCtx, collisionCancel := context.WithTimeout(ctx, 500*time.Millisecond)
			lockErr := sqlc.New(activation).LockDeviceWriteIdentities(collisionCtx, []string{collision})
			collisionCancel()
			_ = activation.Rollback() // Release the organization even on assertion failure.
			admissionErr := <-done
			require.NoError(t, lockErr, "board admission must not hold a colliding identity stripe while waiting for activation's organization lock")
			require.NoError(t, admissionErr)
		})
	}
}
