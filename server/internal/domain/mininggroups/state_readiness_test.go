package mininggroups

import (
	"context"
	"testing"

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
