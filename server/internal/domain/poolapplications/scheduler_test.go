package poolapplications

import (
	"context"
	"encoding/base64"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/block/proto-fleet/server/internal/infrastructure/encrypt"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// Keep the real ready-work queries and database. Only device processing and
// scheduler ticks are controlled, so slow work can hold slots without device I/O.
func poolSchedulerFixture(t *testing.T, accepted, preparing int) (*Worker, map[uuid.UUID]int64, func() int64) {
	t.Helper()
	cfg, err := testutil.GetTestConfig()
	require.NoError(t, err)
	database := testutil.NewDatabaseService(t, cfg)
	user := database.CreateSuperAdminUser()
	cipher, err := encrypt.NewService(&encrypt.Config{ServiceMasterKey: base64.StdEncoding.EncodeToString(make([]byte, 32))})
	require.NoError(t, err)
	repo := poolconfigurations.NewRepository(cipher)
	scope := poolconfigurations.Scope{OrganizationID: user.OrganizationID}
	configuration, err := db.WithTransaction(t.Context(), database.DB, func(q sqlc.Querier) (poolconfigurations.Configuration, error) {
		return repo.Create(t.Context(), q, scope, poolconfigurations.RevisionOptions{Name: "Default", Slots: []poolconfigurations.SlotOptions{{URL: "stratum+tcp://pool.example", Username: "worker"}}})
	})
	require.NoError(t, err)
	accept := func() int64 {
		t.Helper()
		device := database.CreateDevice(user.OrganizationID, "proto")
		_, err := db.WithTransaction(t.Context(), database.DB, func(q sqlc.Querier) (poolconfigurations.Operation, error) {
			return repo.Assign(t.Context(), q, scope, user.DatabaseID, configuration.ID, configuration.RevisionID, []poolconfigurations.AssignmentChange{{Target: poolconfigurations.AssignmentTarget{Kind: poolconfigurations.TargetDevice, ID: device.DatabaseID, DeviceID: device.DatabaseID}}})
		})
		require.NoError(t, err)
		return device.DatabaseID
	}
	for range accepted {
		accept()
	}
	transitions := map[uuid.UUID]int64{}
	for range preparing {
		device := database.CreateDevice(user.OrganizationID, "proto")
		var id uuid.UUID
		require.NoError(t, database.DB.QueryRowContext(t.Context(), `INSERT INTO pool_lifecycle_transition(organization_id,device_id,asset_kind,asset_id,reason) VALUES($1,$2,5,$2,'ownership') RETURNING transition_id`, user.OrganizationID, device.DatabaseID).Scan(&id))
		transitions[id] = device.DatabaseID
	}
	return &Worker{queries: sqlc.New(database.DB)}, transitions, accept
}

func TestPoolSchedulerPrioritizesAcceptedWorkAndPreservesLimits(t *testing.T) {
	for _, scenario := range []struct {
		name                string
		accepted, preparing int
	}{
		{"accepted update competes with eight preparations", 1, 8},
		{"preparation uses spare slots", 3, 8},
		{"accepted work fills capacity", 9, 2},
		{"preparation without accepted work", 0, 9},
		{"running devices cannot start again with spare capacity", 1, 1},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			worker, transitions, _ := poolSchedulerFixture(t, scenario.accepted, scenario.preparing)
			type task struct {
				device   int64
				accepted bool
			}
			started := make(chan task, 32)
			canceled := make(chan struct{}, 32)
			allowExit := make(chan struct{})
			hold := func(ctx context.Context, work task) error {
				started <- work
				<-ctx.Done()
				canceled <- struct{}{}
				<-allowExit
				return nil
			}
			ctx, cancel := context.WithCancel(t.Context())
			ticks := make(chan time.Time)
			done := make(chan struct{})
			go func() {
				defer close(done)
				worker.run(ctx, ticks, func(ctx context.Context, _ int64, _ string, target poolconfigurations.AssignmentTarget) error {
					return hold(ctx, task{device: target.DeviceID, accepted: true})
				}, func(ctx context.Context, id uuid.UUID) error {
					return hold(ctx, task{device: transitions[id]})
				})
			}()
			t.Cleanup(func() {
				cancel()
				close(allowExit)
				select {
				case <-done:
				case <-time.After(3 * time.Second):
					t.Error("pool scheduler did not drain")
				}
			})
			tick := func() {
				t.Helper()
				select {
				case ticks <- time.Now():
				case <-time.After(3 * time.Second):
					t.Fatal("pool scheduler stopped polling")
				}
			}
			tick()
			count := min(8, scenario.accepted+scenario.preparing)
			running := map[int64]bool{}
			accepted := 0
			for range count {
				select {
				case work := <-started:
					require.False(t, running[work.device], "only one task per device")
					running[work.device] = true
					if work.accepted {
						accepted++
					}
				case <-time.After(3 * time.Second):
					t.Fatal("expected available slots to be used")
				}
			}
			require.Equal(t, min(8, scenario.accepted), accepted, "accepted updates must get slots before preparation")
			// All callbacks stay running across multiple polling cycles. Spare slots
			// must not admit duplicates, and a full worker must stay at eight tasks.
			tick()
			tick()
			select {
			case work := <-started:
				t.Fatalf("unexpected extra task: %+v", work)
			case <-time.After(100 * time.Millisecond):
			}
			cancel()
			for range count {
				select {
				case <-canceled:
				case <-time.After(3 * time.Second):
					t.Fatal("running task was not canceled")
				}
			}
			select {
			case <-done:
				t.Fatal("scheduler exited before its tasks finished")
			default:
			}
		})
	}
}

func TestPoolSchedulerGivesReleasedSlotToNewAcceptedWork(t *testing.T) {
	worker, _, accept := poolSchedulerFixture(t, 0, 9)
	ctx, cancel := context.WithCancel(t.Context())
	ticks := make(chan time.Time)
	started := make(chan bool, 16)
	release := make(chan uuid.UUID)
	done := make(chan struct{})
	go func() {
		defer close(done)
		worker.run(ctx, ticks, func(ctx context.Context, _ int64, _ string, _ poolconfigurations.AssignmentTarget) error {
			started <- true
			<-ctx.Done()
			return nil
		}, func(ctx context.Context, id uuid.UUID) error {
			started <- false
			select {
			case <-ctx.Done():
				return nil
			case release <- id:
				_, err := worker.queries.FinishPoolLifecycleTransition(ctx, sqlc.FinishPoolLifecycleTransitionParams{TransitionID: id, State: "completed"})
				return err
			}
		})
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("pool scheduler did not stop")
		}
	})
	tick := func() {
		t.Helper()
		select {
		case ticks <- time.Now():
		case <-time.After(3 * time.Second):
			t.Fatal("pool scheduler stopped polling")
		}
	}
	tick()
	for range 8 {
		select {
		case accepted := <-started:
			require.False(t, accepted)
		case <-time.After(3 * time.Second):
			t.Fatal("expected preparation to fill capacity")
		}
	}
	tick() // Finish the first cycle with every slot still held.
	accept()
	tick() // Every subsequent cycle now sees the accepted update.
	select {
	case <-release:
	case <-time.After(3 * time.Second):
		t.Fatal("could not release a preparation slot")
	}
	deadline := time.After(3 * time.Second)
	for {
		select {
		case accepted := <-started:
			require.True(t, accepted, "new accepted work must get the freed slot ahead of the ninth preparation")
			return
		case ticks <- time.Now():
		case <-deadline:
			t.Fatal("accepted work did not use the released slot")
		}
	}
}
