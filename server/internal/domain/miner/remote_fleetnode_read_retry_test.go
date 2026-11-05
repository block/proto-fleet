package miner

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"connectrpc.com/connect"
	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

type telemetryRetrySender func(context.Context, *gatewaypb.ControlCommand) (*gatewaypb.ControlAck, error)

func (f telemetryRetrySender) SendCommand(ctx context.Context, _ int64, _ gatewaypb.CommandProtocolVersion, cmd *gatewaypb.ControlCommand) (*gatewaypb.ControlAck, error) {
	return f(ctx, cmd)
}

type telemetryRetryGate struct {
	held        bool
	releasedAt  time.Time
	acquired    int
	onRelease   func()
	onReacquire func(context.Context) error
}

func (g *telemetryRetryGate) Acquire(ctx context.Context, _ int64) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("read retry gate context: %w", err)
	}
	if g.held {
		return nil, errors.New("previous attempt retained gate")
	}
	if g.acquired > 0 {
		if time.Since(g.releasedAt) < 20*time.Millisecond {
			return nil, errors.New("gate reacquired before backoff elapsed")
		}
		if g.onReacquire != nil {
			if err := g.onReacquire(ctx); err != nil {
				return nil, err
			}
		}
	}
	g.held = true
	g.acquired++
	return func() {
		g.held = false
		g.releasedAt = time.Now()
		if g.onRelease != nil {
			g.onRelease()
		}
	}, nil
}

func TestRemoteTelemetryRetriesRejectedBusy(t *testing.T) {
	for _, allBusy := range []bool{false, true} {
		t.Run(map[bool]string{false: "recovers", true: "capped"}[allBusy], func(t *testing.T) {
			payload, err := proto.Marshal(telemetryResult("node-device"))
			require.NoError(t, err)
			gate := &telemetryRetryGate{}
			var ids []string
			var deadline time.Time
			var previousTimeout time.Duration
			m := newTestRemoteFleetNodeMiner(t, control.NewRegistry())
			m.gate = gate
			m.sender = telemetryRetrySender(func(ctx context.Context, cmd *gatewaypb.ControlCommand) (*gatewaypb.ControlAck, error) {
				require.True(t, gate.held)
				if len(ids) > 0 {
					require.GreaterOrEqual(t, time.Since(gate.releasedAt), 20*time.Millisecond, "gate must be released throughout backoff")
				}
				got, ok := ctx.Deadline()
				require.True(t, ok)
				if deadline.IsZero() {
					deadline = got
				} else {
					require.Equal(t, deadline, got)
				}
				var env gatewaypb.AgentCommand
				require.NoError(t, proto.Unmarshal(cmd.GetPayload(), &env))
				timeout := env.GetTelemetry().GetTimeout().AsDuration()
				if previousTimeout > 0 {
					require.Less(t, timeout, previousTimeout, "node timeout must consume original remaining budget")
				}
				previousTimeout = timeout
				require.NotContains(t, ids, cmd.GetCommandId())
				ids = append(ids, cmd.GetCommandId())
				if allBusy || len(ids) == 1 {
					return &gatewaypb.ControlAck{Code: gatewaypb.AckCode_ACK_CODE_BUSY}, nil
				}
				return &gatewaypb.ControlAck{Code: gatewaypb.AckCode_ACK_CODE_OK, Succeeded: true, Payload: payload}, nil
			})
			_, err = m.GetDeviceMetrics(t.Context())
			if allBusy {
				require.True(t, fleeterror.IsResourceExhaustedError(err))
				require.Len(t, ids, 3)
			} else {
				require.NoError(t, err)
				require.Len(t, ids, 2)
			}
			require.False(t, gate.held)
			require.Equal(t, len(ids), gate.acquired)
		})
	}
}

func TestRemoteTelemetryRetryCancellationAndDeadline(t *testing.T) {
	for _, cancelOnRelease := range []bool{true, false} {
		t.Run(map[bool]string{true: "cancel during backoff", false: "deadline during backoff"}[cancelOnRelease], func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
			defer cancel()
			gate := &telemetryRetryGate{}
			if cancelOnRelease {
				gate.onRelease = cancel
			}
			m := newTestRemoteFleetNodeMiner(t, control.NewRegistry())
			m.gate = gate
			calls := 0
			m.sender = telemetryRetrySender(func(context.Context, *gatewaypb.ControlCommand) (*gatewaypb.ControlAck, error) {
				calls++
				return &gatewaypb.ControlAck{Code: gatewaypb.AckCode_ACK_CODE_BUSY}, nil
			})
			_, err := m.GetDeviceMetrics(ctx)
			require.Error(t, err)
			require.Equal(t, 1, calls)
			require.False(t, gate.held)
			if cancelOnRelease {
				require.ErrorIs(t, err, context.Canceled)
			} else {
				require.ErrorIs(t, err, context.DeadlineExceeded)
			}
		})
	}
}

func TestRemoteTelemetryRetryNeverRetriesOtherFailures(t *testing.T) {
	for _, tc := range []struct {
		name string
		ack  *gatewaypb.ControlAck
		err  error
	}{
		{name: "transport", err: errors.New("transport failed")},
		{name: "timeout", err: context.DeadlineExceeded},
		{name: "no stream", err: control.ErrNoActiveStream},
		{name: "resource exhausted", err: connect.NewError(connect.CodeResourceExhausted, errors.New("capacity"))},
		{name: "unknown ACK", ack: &gatewaypb.ControlAck{Code: gatewaypb.AckCode_ACK_CODE_UNSPECIFIED}},
		{name: "failed OK", ack: &gatewaypb.ControlAck{Code: gatewaypb.AckCode_ACK_CODE_OK}},
		{name: "inconsistent BUSY", ack: &gatewaypb.ControlAck{Code: gatewaypb.AckCode_ACK_CODE_BUSY, Succeeded: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newTestRemoteFleetNodeMiner(t, control.NewRegistry())
			calls := 0
			m.sender = telemetryRetrySender(func(context.Context, *gatewaypb.ControlCommand) (*gatewaypb.ControlAck, error) {
				calls++
				return tc.ack, tc.err
			})
			_, err := m.GetDeviceMetrics(t.Context())
			require.Error(t, err)
			require.Equal(t, 1, calls)
		})
	}
}

func TestRemoteTelemetryRetryReacquiresWithinOriginalDeadline(t *testing.T) {
	var deadline time.Time
	gate := &telemetryRetryGate{onReacquire: func(ctx context.Context) error {
		got, ok := ctx.Deadline()
		require.True(t, ok)
		require.Equal(t, deadline, got)
		<-ctx.Done()
		return ctx.Err()
	}}
	calls := 0
	m := newTestRemoteFleetNodeMiner(t, control.NewRegistry())
	m.gate = gate
	m.sender = telemetryRetrySender(func(ctx context.Context, _ *gatewaypb.ControlCommand) (*gatewaypb.ControlAck, error) {
		calls++
		deadline, _ = ctx.Deadline()
		return &gatewaypb.ControlAck{Code: gatewaypb.AckCode_ACK_CODE_BUSY}, nil
	})
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Millisecond)
	defer cancel()
	_, err := m.GetDeviceMetrics(ctx)
	require.Error(t, err)
	require.Equal(t, 1, calls)
	require.False(t, gate.held)
}
