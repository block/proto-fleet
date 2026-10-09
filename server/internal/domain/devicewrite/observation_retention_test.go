package devicewrite_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/stretchr/testify/require"
)

func TestCompletedObservationsRetireDirectAdmission(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "read failure"}[failed], func(t *testing.T) {
			database, orgID, deviceID, _, _ := queuedFenceFixture(t)
			service := devicewrite.NewService(database.DB)
			outcome := error(nil)
			if failed {
				outcome = errors.New("observation failed after no physical mutation")
			}
			for range 20 {
				err := service.WithHostAttempt(t.Context(), orgID, deviceID, 0, func(ctx context.Context) error {
					return devicewrite.Observe(ctx, func(context.Context) error { return outcome })
				})
				if failed {
					require.ErrorIs(t, err, outcome)
				} else {
					require.NoError(t, err)
				}
			}
			var count int
			require.NoError(t, database.DB.QueryRowContext(t.Context(), "SELECT count(*) FROM device_write_attempt WHERE device_id=$1", deviceID).Scan(&count))
			require.Zero(t, count, "completed dashboard polling must not retain one admission row per request")
		})
	}
}

func TestObservationCleanupPreservesDurableEvidence(t *testing.T) {
	for _, kind := range []string{"queued", "successful mutation", "unknown mutation", "running observation"} {
		t.Run(kind, func(t *testing.T) {
			database, orgID, deviceID, message, _ := queuedFenceFixture(t)
			service := devicewrite.NewService(database.DB)
			var attempt *devicewrite.Attempt
			var err error
			if kind == "queued" {
				attempt, err = service.BeginQueuedHost(t.Context(), message)
			} else {
				attempt, err = service.BeginHost(t.Context(), orgID, deviceID, 0)
			}
			require.NoError(t, err)
			ctx := attempt.Context(t.Context())
			require.NoError(t, devicewrite.Observe(ctx, func(context.Context) error { return nil }))
			outcome := error(nil)
			if kind == "unknown mutation" {
				outcome = errors.New("physical outcome unknown")
			}
			if kind == "successful mutation" || kind == "unknown mutation" {
				err = devicewrite.Dispatch(ctx, func(context.Context) error { return outcome })
				if outcome != nil {
					require.ErrorIs(t, err, outcome)
				} else {
					require.NoError(t, err)
				}
				// A later observation cannot erase the monotonic mutation marker.
				require.NoError(t, devicewrite.Observe(ctx, func(context.Context) error { return nil }))
			}
			if kind != "running observation" {
				require.NoError(t, attempt.Finish(t.Context(), outcome))
			}
			var status string
			require.NoError(t, database.DB.QueryRowContext(t.Context(), "SELECT status FROM device_write_attempt WHERE device_id=$1", deviceID).Scan(&status))
			want := "complete"
			if kind == "unknown mutation" {
				want = "unknown"
			} else if kind == "running observation" {
				want = "running"
			}
			require.Equal(t, want, status)
			if kind == "unknown mutation" {
				_, err := service.Reserve(t.Context(), orgID, deviceID)
				require.Error(t, err)
			} else if kind == "running observation" {
				epoch, err := service.Reserve(t.Context(), orgID, deviceID)
				require.NoError(t, err)
				drained, err := devicewrite.NewService(database.DB).Drained(t.Context(), orgID, deviceID, epoch)
				require.NoError(t, err)
				require.False(t, drained, "completed-observation cleanup must not recover outstanding evidence")
			}
		})
	}
}

func TestCompletedUndispatchedDeletionPermitIsRetained(t *testing.T) {
	database, orgID, deviceID, _, _ := queuedFenceFixture(t)
	_, err := database.DB.ExecContext(t.Context(), "INSERT INTO device_pairing(device_id,pairing_status,paired_at) VALUES($1,'PAIRED',statement_timestamp())", deviceID)
	require.NoError(t, err)
	identifier, err := sqlc.New(database.DB).GetDeviceIdentifierByID(t.Context(), deviceID)
	require.NoError(t, err)
	deleted, err := sqlstores.NewSQLDeviceStore(database.DB).SoftDeleteDevices(t.Context(), []string{identifier}, orgID)
	require.NoError(t, err)
	require.EqualValues(t, 1, deleted)
	var bindingHash string
	require.NoError(t, database.DB.QueryRowContext(t.Context(), "SELECT binding_hash FROM device_deletion_unpair_permit WHERE device_id=$1", deviceID).Scan(&bindingHash))
	service := devicewrite.NewService(database.DB)
	require.NoError(t, service.RunDeletionUnpair(t.Context(), orgID, deviceID, bindingHash, func(context.Context) error { return nil }))
	var status string
	require.NoError(t, database.DB.QueryRowContext(t.Context(), "SELECT a.status FROM device_deletion_unpair_permit p JOIN device_write_attempt a ON a.attempt_id=p.attempt_id WHERE p.device_id=$1", deviceID).Scan(&status))
	require.Equal(t, "complete", status, "a claimed permit keeps its single-use evidence even when its callback dispatched nothing")
	require.Error(t, service.RunDeletionUnpair(t.Context(), orgID, deviceID, bindingHash, func(context.Context) error { t.Fatal("claimed permit reused"); return nil }))
}

func TestRetiredObservationContextAndDuplicateFinishCannotDispatch(t *testing.T) {
	database, orgID, deviceID, _, _ := queuedFenceFixture(t)
	service := devicewrite.NewService(database.DB)
	attempt, err := service.BeginHost(t.Context(), orgID, deviceID, 0)
	require.NoError(t, err)
	ctx := attempt.Context(t.Context())
	require.NoError(t, devicewrite.Observe(ctx, func(context.Context) error { return nil }))
	var start sync.WaitGroup
	start.Add(1)
	results := make(chan error, 2)
	for range 2 {
		go func() { start.Wait(); results <- attempt.Finish(t.Context(), nil) }()
	}
	start.Done()
	succeeded := 0
	for range 2 {
		if <-results == nil {
			succeeded++
		}
	}
	require.Equal(t, 1, succeeded, "the existing fence lock must serialize duplicate completion")
	var count int
	require.NoError(t, database.DB.QueryRowContext(t.Context(), "SELECT count(*) FROM device_write_attempt WHERE device_id=$1", deviceID).Scan(&count))
	require.Zero(t, count)
	require.Error(t, attempt.Recheck(t.Context()))
	called := false
	require.Error(t, devicewrite.Dispatch(ctx, func(context.Context) error { called = true; return nil }))
	require.False(t, called, "a removed completed admission is not authority for another physical call")
	require.Error(t, attempt.Finish(t.Context(), nil))
}

func TestObservationCleanupFailureRollsBackCompletion(t *testing.T) {
	database, orgID, deviceID, _, _ := queuedFenceFixture(t)
	service := devicewrite.NewService(database.DB)
	attempt, err := service.BeginHost(t.Context(), orgID, deviceID, 0)
	require.NoError(t, err)
	require.NoError(t, devicewrite.Observe(attempt.Context(t.Context()), func(context.Context) error { return nil }))
	_, err = database.DB.ExecContext(t.Context(), `CREATE FUNCTION reject_observation_cleanup() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN RAISE EXCEPTION 'injected observation cleanup failure'; END $$;
 CREATE TRIGGER reject_observation_cleanup BEFORE DELETE ON device_write_attempt
 FOR EACH ROW EXECUTE FUNCTION reject_observation_cleanup()`)
	require.NoError(t, err)
	err = attempt.Finish(t.Context(), nil)
	require.ErrorContains(t, err, "injected observation cleanup failure")
	var status string
	var unfinished bool
	require.NoError(t, database.DB.QueryRowContext(t.Context(), "SELECT status,finished_at IS NULL FROM device_write_attempt WHERE device_id=$1", deviceID).Scan(&status, &unfinished))
	require.Equal(t, "running", status)
	require.True(t, unfinished)
	epoch, err := service.Reserve(t.Context(), orgID, deviceID)
	require.NoError(t, err)
	drained, err := service.Drained(t.Context(), orgID, deviceID, epoch)
	require.NoError(t, err)
	require.False(t, drained)
	_, err = database.DB.ExecContext(t.Context(), "DROP TRIGGER reject_observation_cleanup ON device_write_attempt")
	require.NoError(t, err)
	require.NoError(t, attempt.Finish(t.Context(), nil), "the same owning process may finish its already-ended observation; no device call is retried")
}
