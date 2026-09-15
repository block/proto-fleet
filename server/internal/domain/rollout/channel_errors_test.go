package rollout

import (
	"context"
	"fmt"
	"testing"

	"connectrpc.com/connect"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Observe the real transactor without replacing its queries or retry policy.
type channelRetryObserver struct {
	interfaces.Transactor
	attempts int
	firstErr error
}

func (tx *channelRetryObserver) RunInTx(ctx context.Context, action func(context.Context) error) error {
	return tx.Transactor.RunInTx(ctx, func(ctx context.Context) error {
		tx.attempts++
		err := action(ctx)
		if tx.attempts == 1 {
			tx.firstErr = err
		}
		return err
	})
}

func TestChannelWritesRetryPostgresFailures(t *testing.T) {
	for _, operation := range []string{"create", "update"} {
		for _, table := range []string{"release_channel", "release_channel_target"} {
			for _, code := range []string{"40001", "40P01"} {
				t.Run(operation+"/"+table+"/"+code, func(t *testing.T) {
					f := newFixture(t, 2)
					ctx := t.Context()
					var channelID int64
					if operation == "update" {
						channel, err := f.svc.CreateChannel(ctx, f.orgID, 1, ChannelSpec{
							Name: "Original", Scope: Scope{DeviceIdentifiers: []string{"miner-0"}},
						})
						require.NoError(t, err)
						channelID = channel.ID
					}

					// The sequence survives rollback, so the first write fails and
					// the retry succeeds in this test's disposable database.
					_, err := f.conn.ExecContext(ctx, fmt.Sprintf(`
						CREATE SEQUENCE channel_retry_attempt;
						CREATE FUNCTION fail_first_channel_write() RETURNS trigger AS $$
						BEGIN
							IF nextval('channel_retry_attempt') = 1 THEN
								RAISE EXCEPTION 'injected retryable channel write failure' USING ERRCODE = '%s';
							END IF;
							RETURN NEW;
						END;
						$$ LANGUAGE plpgsql;
						CREATE TRIGGER channel_retry BEFORE INSERT OR UPDATE ON %s
						FOR EACH ROW EXECUTE FUNCTION fail_first_channel_write();`, code, table))
					require.NoError(t, err)
					observer := &channelRetryObserver{Transactor: f.svc.tx}
					f.svc.tx = observer
					spec := ChannelSpec{Name: "Saved", Scope: Scope{DeviceIdentifiers: []string{"miner-1"}}}
					var channel *Channel
					if operation == "create" {
						channel, err = f.svc.CreateChannel(ctx, f.orgID, 1, spec)
					} else {
						channel, err = f.svc.UpdateChannel(ctx, f.orgID, channelID, spec)
					}
					require.NoError(t, err, "retryable causes must reach the real transaction retry loop")
					assert.Equal(t, 2, observer.attempts)
					var pgErr *pgconn.PgError
					require.ErrorAs(t, observer.firstErr, &pgErr)
					assert.Equal(t, code, pgErr.Code)
					assert.Equal(t, spec.Name, channel.Name)
					assert.Equal(t, spec.Scope.DeviceIdentifiers, channel.Scope.DeviceIdentifiers)
					channels, _, err := f.svc.ListChannels(ctx, f.orgID, 0, "")
					require.NoError(t, err)
					require.Len(t, channels, 1, "a failed attempt must not leave another channel behind")
					assert.Equal(t, int32(1), channels[0].MinerCount)
				})
			}
		}
	}
}

func TestChannelLookupErrorsDistinguishMissingRows(t *testing.T) {
	f := newFixture(t, 0)
	ctx := t.Context()
	channel, err := f.svc.CreateChannel(ctx, f.orgID, 1, ChannelSpec{Name: "Existing"})
	require.NoError(t, err)
	operations := []struct {
		name string
		run  func(context.Context, int64, int64) error
	}{
		{"get", func(ctx context.Context, orgID, channelID int64) error {
			_, err := f.svc.GetChannel(ctx, orgID, channelID)
			return err
		}},
		{"update", func(ctx context.Context, orgID, channelID int64) error {
			_, err := f.svc.UpdateChannel(ctx, orgID, channelID, ChannelSpec{Name: "Updated"})
			return err
		}},
		{"miners", func(ctx context.Context, orgID, channelID int64) error {
			_, _, err := f.svc.ListChannelMiners(ctx, orgID, channelID, "", "", 0, "")
			return err
		}},
		{"model groups", func(ctx context.Context, orgID, channelID int64) error {
			_, _, err := f.svc.ListChannelModelGroups(ctx, orgID, channelID, 0, "")
			return err
		}},
	}
	for _, operation := range operations {
		t.Run(operation.name+"/missing", func(t *testing.T) {
			assert.True(t, fleeterror.IsNotFoundError(operation.run(ctx, f.orgID, channel.ID+1)))
			assert.True(t, fleeterror.IsNotFoundError(operation.run(ctx, f.orgID+1, channel.ID)), "other orgs cannot see the channel")
		})
		t.Run(operation.name+"/canceled", func(t *testing.T) {
			canceledCtx, cancel := context.WithCancel(ctx)
			cancel()
			err := operation.run(canceledCtx, f.orgID, channel.ID)
			assert.False(t, fleeterror.IsNotFoundError(err))
			require.ErrorIs(t, err, context.Canceled)
		})
	}

	// Break the prepared lookup in the disposable database. The scope lock
	// still succeeds, so UpdateChannel must classify the lookup error inside
	// its transaction rather than treating a database failure as a missing row.
	_, err = f.conn.ExecContext(ctx, `ALTER TABLE release_channel RENAME COLUMN description TO hidden_description`)
	require.NoError(t, err)
	for _, operation := range operations {
		t.Run(operation.name+"/database failure", func(t *testing.T) {
			err := operation.run(ctx, f.orgID, channel.ID)
			var fleetErr fleeterror.FleetError
			require.ErrorAs(t, err, &fleetErr)
			assert.Equal(t, connect.CodeInternal, fleetErr.GRPCCode)
			var pgErr *pgconn.PgError
			require.ErrorAs(t, err, &pgErr)
			assert.Equal(t, "42703", pgErr.Code)
		})
	}
}
