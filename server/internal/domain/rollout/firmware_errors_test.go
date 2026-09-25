package rollout

import (
	"context"
	"fmt"
	"math"
	"testing"

	"connectrpc.com/connect"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

type firmwareLookupOperation struct {
	name      string
	channelID bool
	run       func(context.Context, *Service, int64, int64) error
}

var firmwareLookupOperations = []firmwareLookupOperation{
	{"apply", true, func(ctx context.Context, s *Service, orgID, id int64) error {
		_, err := s.ApplyFirmware(ctx, orgID, testActor, id, nil, nil)
		return err
	}},
	{"preview", true, func(ctx context.Context, s *Service, orgID, id int64) error {
		_, err := s.PreviewFirmware(ctx, orgID, id, nil, nil)
		return err
	}},
	{"rollback", false, func(ctx context.Context, s *Service, orgID, id int64) error {
		_, _, err := s.RollbackFirmware(ctx, orgID, id, byOperator)
		return err
	}},
	{"pause", false, func(ctx context.Context, s *Service, orgID, id int64) error {
		_, err := s.PauseRollout(ctx, orgID, id, byOperator)
		return err
	}},
	{"get", false, func(ctx context.Context, s *Service, orgID, id int64) error {
		_, err := s.GetRollout(ctx, orgID, id)
		return err
	}},
	{"devices", false, func(ctx context.Context, s *Service, orgID, id int64) error {
		_, _, err := s.ListRolloutDevices(ctx, orgID, id, 0, "")
		return err
	}},
	{"retry", false, func(ctx context.Context, s *Service, orgID, id int64) error {
		_, err := s.RetryFailedDevices(ctx, orgID, id, byOperator)
		return err
	}},
}

func TestFirmwareLookupErrorsDistinguishMissingRows(t *testing.T) {
	for _, operation := range firmwareLookupOperations {
		t.Run(operation.name, func(t *testing.T) {
			f := newFixture(t, 1)
			ctx := t.Context()
			f.channel(t, allAtOnce, f.allMiners()...)
			rollout := f.apply(t, "fw-1")
			id := rollout.ID
			if operation.channelID {
				id = f.channelID
			}
			var otherOrgID int64
			require.NoError(t, f.conn.QueryRowContext(ctx, `INSERT INTO organization (org_id, name) VALUES ('other-org', 'Other org') RETURNING id`).Scan(&otherOrgID))
			require.NoError(t, operation.run(ctx, f.svc, f.orgID, id))
			assert.True(t, fleeterror.IsNotFoundError(operation.run(ctx, f.svc, f.orgID, math.MaxInt64)))
			assert.True(t, fleeterror.IsNotFoundError(operation.run(ctx, f.svc, otherOrgID, id)))

			canceledCtx, cancel := context.WithCancel(ctx)
			cancel()
			err := operation.run(canceledCtx, f.svc, f.orgID, id)
			assert.False(t, fleeterror.IsNotFoundError(err))
			require.ErrorIs(t, err, context.Canceled)

			current, err := f.svc.GetRollout(ctx, f.orgID, rollout.ID)
			require.NoError(t, err)
			if current.Status == StatusActive {
				_, _, err = f.svc.CancelRollout(ctx, f.orgID, rollout.ID, byOperator)
				require.NoError(t, err)
			}
			require.NoError(t, f.svc.DeleteChannel(ctx, f.orgID, f.channelID))
			assert.True(t, fleeterror.IsNotFoundError(operation.run(ctx, f.svc, f.orgID, id)))
		})
	}
}

func TestFirmwareLookupDatabaseFailuresPreserveCause(t *testing.T) {
	for _, operation := range firmwareLookupOperations {
		for _, table := range []string{"release_channel", "firmware_rollout"} {
			if operation.channelID && table == "firmware_rollout" {
				continue
			}
			t.Run(operation.name+"/"+table, func(t *testing.T) {
				f := newFixture(t, 1)
				ctx := t.Context()
				f.channel(t, allAtOnce, f.allMiners()...)
				rollout := f.apply(t, "fw-1")
				id := rollout.ID
				if operation.channelID {
					id = f.channelID
				}
				// A broken lookup in this disposable database reaches the real
				// query inside transaction callbacks as well as direct reads.
				statement := `ALTER TABLE release_channel RENAME COLUMN name TO hidden_name`
				if table == "firmware_rollout" {
					statement = `ALTER TABLE firmware_rollout RENAME COLUMN firmware_checksum TO hidden_checksum`
				}
				_, err := f.conn.ExecContext(ctx, statement)
				require.NoError(t, err)
				assertFirmwareDatabaseError(t, operation.run(ctx, f.svc, f.orgID, id), "42703")
			})
		}
	}
}

func TestAssignmentLookupErrorsAreNotStaleGeneration(t *testing.T) {
	for _, operation := range []string{"rollback", "retry"} {
		t.Run(operation, func(t *testing.T) {
			f := newFixture(t, 1)
			ctx := t.Context()
			f.channel(t, allAtOnce, f.allMiners()...)
			rollout := f.apply(t, "fw-1")
			if operation == "retry" {
				_, _, err := f.svc.CancelRollout(ctx, f.orgID, rollout.ID, byOperator)
				require.NoError(t, err)
			}
			_, err := f.conn.ExecContext(ctx, `ALTER TABLE release_channel_firmware RENAME COLUMN firmware_checksum TO hidden_checksum`)
			require.NoError(t, err)
			if operation == "rollback" {
				_, _, err = f.svc.RollbackFirmware(ctx, f.orgID, rollout.ID, byOperator)
			} else {
				_, err = f.svc.RetryFailedDevices(ctx, f.orgID, rollout.ID, byOperator)
			}
			assertFirmwareDatabaseError(t, err, "42703")
			_, hasReason := ReasonOf(err)
			assert.False(t, hasReason, "a database failure must not masquerade as a stale assignment")
		})
	}
}

func TestFirmwareWritesRetryPostgresFailures(t *testing.T) {
	for _, table := range []string{"release_channel_firmware", "firmware_rollout", "firmware_rollout_device"} {
		for _, code := range []string{"40001", "40P01"} {
			t.Run(table+"/"+code, func(t *testing.T) {
				f := newFixture(t, 1)
				ctx := t.Context()
				f.channel(t, allAtOnce, f.allMiners()...)
				// The sequence survives rollback. Fail one real write after
				// channel lookup, then let the normal transaction retry finish.
				_, err := f.conn.ExecContext(ctx, fmt.Sprintf(`
					CREATE SEQUENCE firmware_retry_attempt;
					CREATE FUNCTION fail_first_firmware_write() RETURNS trigger AS $$
					BEGIN
						IF nextval('firmware_retry_attempt') = 1 THEN
							RAISE EXCEPTION 'injected retryable firmware write failure' USING ERRCODE = '%s';
						END IF;
						RETURN NEW;
					END;
					$$ LANGUAGE plpgsql;
					CREATE TRIGGER firmware_retry BEFORE INSERT ON %s
					FOR EACH ROW EXECUTE FUNCTION fail_first_firmware_write();`, code, table))
				require.NoError(t, err)
				observer := &channelRetryObserver{Transactor: f.svc.tx}
				f.svc.tx = observer
				started, err := f.svc.ApplyFirmware(ctx, f.orgID, testActor, f.channelID, []Assignment{rigAssignment("fw-1")}, nil)
				require.NoError(t, err)
				require.Len(t, started, 1)
				assert.Equal(t, 2, observer.attempts)
				assertFirmwareDatabaseError(t, observer.firstErr, code)
				rollouts, _, _, err := f.svc.ListRollouts(ctx, f.orgID, RolloutFilter{})
				require.NoError(t, err)
				require.Len(t, rollouts, 1, "the aborted attempt must not leave a rollout behind")
				assert.Equal(t, started[0].ID, rollouts[0].ID)
				devices, _, err := f.svc.ListRolloutDevices(ctx, f.orgID, started[0].ID, 0, "")
				require.NoError(t, err)
				assert.Len(t, devices, 1)
			})
		}
	}
}

func assertFirmwareDatabaseError(t *testing.T, err error, sqlstate string) {
	t.Helper()
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodeInternal, fleetErr.GRPCCode)
	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr)
	assert.Equal(t, sqlstate, pgErr.Code)
}
