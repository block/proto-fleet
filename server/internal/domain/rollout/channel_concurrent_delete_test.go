package rollout

import (
	"context"
	"fmt"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateChannelDeletedBeforeWriteReturnsNotFound(t *testing.T) {
	f := newFixture(t, 2)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	channel, err := f.svc.CreateChannel(ctx, f.orgID, 1, ChannelSpec{
		Name: "Original", Scope: Scope{DeviceIdentifiers: []string{"miner-0"}},
	})
	require.NoError(t, err)

	// Stop the UPDATE after the existence/scope checks, before it locks rows.
	// DeleteChannel uses a separate connection and can delete the row while
	// UpdateChannel holds its organization scope lock.
	_, err = f.conn.ExecContext(ctx, `
		CREATE FUNCTION wait_before_channel_update() RETURNS trigger AS $$
		BEGIN
			PERFORM pg_advisory_xact_lock(1016, 1);
			RETURN NULL;
		END;
		$$ LANGUAGE plpgsql;
		CREATE TRIGGER wait_before_channel_update BEFORE UPDATE ON release_channel
		FOR EACH STATEMENT EXECUTE FUNCTION wait_before_channel_update();`)
	require.NoError(t, err)
	blocker, err := f.conn.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = blocker.Rollback() }()
	_, err = blocker.ExecContext(ctx, `SELECT pg_advisory_xact_lock(1016, 1)`)
	require.NoError(t, err)
	var blockerPID int
	require.NoError(t, blocker.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&blockerPID))

	observer := &channelRetryObserver{Transactor: f.svc.tx}
	f.svc.tx = observer
	type updateResult struct {
		channel *Channel
		err     error
	}
	done := make(chan updateResult, 1)
	go func() {
		updated, err := f.svc.UpdateChannel(ctx, f.orgID, channel.ID, ChannelSpec{
			Name: "Updated", Scope: Scope{DeviceIdentifiers: []string{"miner-1"}},
		})
		done <- updateResult{channel: updated, err: err}
	}()
	require.Eventually(t, func() bool {
		var blocked bool
		err := f.conn.QueryRowContext(ctx, `SELECT EXISTS (
			SELECT 1 FROM pg_stat_activity
			WHERE datname = current_database() AND $1 = ANY(pg_blocking_pids(pid))
		)`, blockerPID).Scan(&blocked)
		return err == nil && blocked
	}, 3*time.Second, 10*time.Millisecond, "update must reach its write before deletion")
	require.NoError(t, f.svc.DeleteChannel(ctx, f.orgID, channel.ID))
	require.NoError(t, blocker.Rollback())

	result := <-done
	assert.Nil(t, result.channel)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, result.err, &fleetErr)
	assert.Equal(t, connect.CodeNotFound, fleetErr.GRPCCode)
	assert.Equal(t, fmt.Sprintf("release channel not found: %d", channel.ID), fleetErr.DebugMessage)
	assert.Equal(t, 1, observer.attempts, "a deleted channel is not a retryable database failure")
	var remaining int
	require.NoError(t, f.conn.QueryRowContext(ctx, `
		SELECT (SELECT count(*) FROM release_channel WHERE id = $1)
		     + (SELECT count(*) FROM release_channel_target WHERE channel_id = $1)
	`, channel.ID).Scan(&remaining))
	assert.Zero(t, remaining, "the failed update must not recreate the channel or its targets")
}
