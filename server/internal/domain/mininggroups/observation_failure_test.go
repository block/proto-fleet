package mininggroups

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

type canceledObservationMiner struct {
	interfaces.Miner
	entered       chan struct{}
	returnSuccess bool
}

func (m *canceledObservationMiner) GetMinerForCommand(context.Context, int64) (interfaces.Miner, error) {
	return m, nil
}

func (m *canceledObservationMiner) MiningGroups(ctx context.Context, request sdk.MiningGroupRequest) (sdk.MiningGroupResult, error) {
	if request.Action != "observe" {
		return sdk.MiningGroupResult{}, fmt.Errorf("unexpected mining-group action: %s", request.Action)
	}
	close(m.entered)
	<-ctx.Done()
	if m.returnSuccess {
		return sdk.MiningGroupResult{Supported: true, Enabled: false, Revision: 2}, nil
	}
	return sdk.MiningGroupResult{}, fmt.Errorf("controller observation failed: %w", ctx.Err())
}

func observationFailureFixture(t *testing.T) (context.Context, retryFixture, sqlc.ContainerMiningGroupState) {
	t.Helper()
	ctx, f := deactivationFixture(t)
	_, err := f.conn.ExecContext(ctx, `UPDATE device_write_fence SET updated_at=clock_timestamp()-interval '2 minutes' WHERE device_id=$1`, f.target.DeviceID)
	require.NoError(t, err)
	_, err = f.conn.ExecContext(ctx, `UPDATE container_mining_group_state SET observed_at=clock_timestamp()-interval '65 seconds' WHERE device_id=$1`, f.target.DeviceID)
	require.NoError(t, err)
	requireAllocationReadiness(t, ctx, f, true)
	return ctx, f, f.state(t)
}

func requireAllocationReadiness(t *testing.T, ctx context.Context, f retryFixture, expected bool) {
	t.Helper()
	ready, err := f.service.queries.CT1HashboardAllocationReady(ctx, sqlc.CT1HashboardAllocationReadyParams{OrganizationID: f.target.OrganizationID, DeviceID: f.target.DeviceID})
	require.NoError(t, err)
	require.Equal(t, expected, ready)
}

func TestFailedObservationInvalidatesCachedAllocationAuthority(t *testing.T) {
	for _, path := range []string{"dialog", "background"} {
		for _, failure := range []string{"deadline", "canceled", "canceled before save"} {
			t.Run(path+"/"+failure, func(t *testing.T) {
				ctx, f, cached := observationFailureFixture(t)
				miner := &canceledObservationMiner{entered: make(chan struct{}), returnSuccess: failure == "canceled before save"}
				f.service.miners = miner
				var callCtx context.Context
				var cancel context.CancelFunc
				if failure == "deadline" {
					callCtx, cancel = context.WithTimeout(ctx, time.Second)
				} else {
					callCtx, cancel = context.WithCancel(ctx)
				}
				defer cancel()
				type result struct {
					state State
					err   error
				}
				done := make(chan result, 1)
				go func() {
					if path == "dialog" {
						state, err := f.service.GetState(callCtx, f.target.DeviceIdentifier)
						done <- result{state: state, err: err}
						return
					}
					done <- result{err: f.service.refreshAllocationReadiness(callCtx, f.target.OrganizationID, f.target.DeviceID, f.target.DeviceIdentifier)}
				}()
				select {
				case <-miner.entered:
				case <-time.After(3 * time.Second):
					t.Fatal("controller observation did not start")
				}
				if failure != "deadline" {
					cancel()
				}
				var outcome result
				select {
				case outcome = <-done:
				case <-time.After(5 * time.Second):
					t.Fatal("failed observation did not finish")
				}
				if miner.returnSuccess {
					require.ErrorIs(t, outcome.err, context.Canceled, "a disabled read cannot be saved after cancellation")
				} else if path == "dialog" {
					require.NoError(t, outcome.err)
					require.Contains(t, outcome.state.DeactivationBlockedReason, "Reconnect")
					require.Equal(t, cached.Observed, outcome.state.Record.Observed, "the dialog keeps the last display snapshot")
				} else if failure == "deadline" {
					require.ErrorIs(t, outcome.err, context.DeadlineExceeded)
				} else {
					require.ErrorIs(t, outcome.err, context.Canceled)
				}
				after := f.state(t)
				require.False(t, after.ObservedAt.Valid, "failed reads must revoke cached allocation authority")
				require.Equal(t, cached.Observed, after.Observed)
				requireAllocationReadiness(t, ctx, f, false)
			})
		}
	}
}

func TestScheduledObservationTimeoutRetainsMissingStateForBackoff(t *testing.T) {
	ctx, f := deactivationFixture(t)
	_, err := f.conn.ExecContext(ctx, `DELETE FROM container_mining_group_state WHERE device_id=$1`, f.target.DeviceID)
	require.NoError(t, err)
	miner := &canceledObservationMiner{entered: make(chan struct{})}
	f.service.miners = miner
	done := make(chan error, 1)
	go func() {
		done <- f.service.reconcileScheduledWithTimeout(ctx, sqlc.ListMiningGroupControllersRow{
			OrganizationID:   f.target.OrganizationID,
			DeviceID:         f.target.DeviceID,
			DeviceIdentifier: f.target.DeviceIdentifier,
			Readiness:        true,
		}, time.Second)
	}()
	select {
	case <-miner.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("controller observation did not start")
	}
	select {
	case err = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("failed observation did not finish")
	}
	require.ErrorIs(t, err, context.DeadlineExceeded)
	state := f.state(t)
	require.False(t, state.ObservedAt.Valid)
	require.EqualValues(t, 1, state.RetryCount)
	require.True(t, state.NextReconcileAt.After(time.Now()))
	requireAllocationReadiness(t, ctx, f, false)
}
