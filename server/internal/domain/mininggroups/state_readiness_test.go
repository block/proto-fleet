package mininggroups

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestStateReadsDeactivationBlockersOnlyWhenEnabled(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() { otel.SetTracerProvider(previous); require.NoError(t, provider.Shutdown(context.Background())) })
	for _, enabled := range []bool{false, true} {
		name := "disabled"
		if enabled {
			name = "enabled"
		}
		t.Run(name, func(t *testing.T) {
			ctx, f := deactivationFixture(t)
			f.miner.enabled = enabled
			ctx, span := provider.Tracer("mining-group-state-test").Start(ctx, "read")
			defer span.End()
			state, err := f.service.GetState(ctx, f.target.DeviceIdentifier)
			require.NoError(t, err)
			require.True(t, state.Observed.Supported)
			require.Equal(t, enabled, state.Observed.Enabled)
			require.True(t, state.Record.ObservedAt.Valid)
			require.Empty(t, state.DeactivationBlockedReason)
			counts := map[string]int{}
			for _, recorded := range recorder.Ended() {
				if recorded.SpanContext().TraceID() == span.SpanContext().TraceID() {
					counts[recorded.Name()]++
				}
			}
			want := 0
			if enabled {
				want = 1
			}
			for _, query := range []string{"GetMiningGroupDeactivationBlocker", "GetBlockingFrigateUpdateJobForDevice", "ListUnresolvedDeviceWriteAttempts", "ListActivePoolRemovals", "ListUnfinishedDeviceWriteCommands"} {
				require.Equal(t, want, counts["db."+query], query)
			}
		})
	}
}

func TestModeChangeStaleObservationUsesNeutralMessage(t *testing.T) {
	for _, kind := range []string{operationActivate, operationDeactivate} {
		t.Run(kind, func(t *testing.T) {
			ctx, f := newIdleRetryFixture(t)
			_, err := f.conn.ExecContext(ctx, `UPDATE container_mining_group_state SET observed_at=NULL WHERE device_id=$1`, f.target.DeviceID)
			require.NoError(t, err)
			_, err = f.service.changeMode(ctx, f.target.DeviceIdentifier, uuid.NewString(), kind)
			require.ErrorContains(t, err, "refresh the container before changing mining-group mode")
		})
	}
}

type failingDeactivationChecker struct {
	*retryMiner
}

func (m failingDeactivationChecker) CheckMiningGroupDeactivation(context.Context) error {
	return errors.New("node connection closed after observe")
}

type failingDeactivationQueries struct {
	sqlc.Querier
}

func (q failingDeactivationQueries) GetMiningGroupDeactivationBlocker(context.Context, sqlc.GetMiningGroupDeactivationBlockerParams) (string, error) {
	return "", errors.New("readiness database unavailable")
}

func TestStateCommitsObservationWhenAdvisoryReadinessFails(t *testing.T) {
	for _, failure := range []string{"node check", "database check"} {
		t.Run(failure, func(t *testing.T) {
			ctx, f := deactivationFixture(t)
			if failure == "node check" {
				miner := failingDeactivationChecker{retryMiner: f.miner}
				f.service.miners = callMinerGetter(func(context.Context, int64) (interfaces.Miner, error) { return miner, nil })
			} else {
				f.service.queries = failingDeactivationQueries{Querier: f.service.queries}
			}
			_, err := f.conn.ExecContext(ctx, `UPDATE container_mining_group_state SET observed_at=NULL WHERE device_id=$1`, f.target.DeviceID)
			require.NoError(t, err)
			state, err := f.service.GetState(ctx, f.target.DeviceIdentifier)
			require.NoError(t, err)
			require.True(t, state.Observed.Enabled)
			require.True(t, state.Record.ObservedAt.Valid)
			require.Contains(t, state.DeactivationBlockedReason, "cannot confirm")
			require.False(t, state.CanReturnToDefault)
			stored := f.state(t)
			require.True(t, stored.ObservedAt.Valid)
			require.WithinDuration(t, state.Record.ObservedAt.Time, stored.ObservedAt.Time, time.Microsecond)
			require.JSONEq(t, string(state.Record.Observed), string(stored.Observed))
			if failure == "node check" {
				_, err = f.service.Deactivate(ctx, f.target.DeviceIdentifier, uuid.NewString())
				require.ErrorContains(t, err, "node connection closed")
				require.Equal(t, "idle", f.state(t).Status, "advisory failures must still block mutation admission")
			}
		})
	}
}
