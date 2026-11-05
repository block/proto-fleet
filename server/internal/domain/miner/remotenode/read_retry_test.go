package remotenode

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

type readRetrySender func(context.Context, *gatewaypb.ControlCommand) (*gatewaypb.ControlAck, error)

func (f readRetrySender) SendCommand(ctx context.Context, _ int64, _ gatewaypb.CommandProtocolVersion, cmd *gatewaypb.ControlCommand) (*gatewaypb.ControlAck, error) {
	return f(ctx, cmd)
}

type readRetryGate struct {
	held        bool
	releasedAt  time.Time
	acquired    int
	onRelease   func()
	onReacquire func(context.Context) error
}

func (g *readRetryGate) Acquire(ctx context.Context, _ int64) (func(), error) {
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

func TestMinerGetErrorsRetriesRejectedBusy(t *testing.T) {
	for _, allBusy := range []bool{false, true} {
		t.Run(map[bool]string{false: "recovers", true: "capped"}[allBusy], func(t *testing.T) {
			payload, err := proto.Marshal(&gatewaypb.GetErrorsResult{DeviceId: "dev-1"})
			require.NoError(t, err)
			gate := &readRetryGate{}
			var ids []string
			var deadline time.Time
			sender := readRetrySender(func(ctx context.Context, cmd *gatewaypb.ControlCommand) (*gatewaypb.ControlAck, error) {
				require.True(t, gate.held)
				if len(ids) > 0 {
					require.GreaterOrEqual(t, time.Since(gate.releasedAt), 20*time.Millisecond, "gate must be released throughout backoff")
				}
				got, ok := ctx.Deadline()
				require.True(t, ok)
				if deadline.IsZero() {
					deadline = got
				} else {
					require.Equal(t, deadline, got, "retry extended original deadline")
				}
				require.NotContains(t, ids, cmd.GetCommandId())
				ids = append(ids, cmd.GetCommandId())
				if allBusy || len(ids) == 1 {
					return &gatewaypb.ControlAck{Code: gatewaypb.AckCode_ACK_CODE_BUSY}, nil
				}
				return &gatewaypb.ControlAck{Code: gatewaypb.AckCode_ACK_CODE_OK, Succeeded: true, Payload: payload}, nil
			})
			m := newTestMinerWithGate(t, sender, gate)
			result, err := m.GetErrors(t.Context())
			if allBusy {
				require.True(t, fleeterror.IsResourceExhaustedError(err))
				require.Len(t, ids, 3)
			} else {
				require.NoError(t, err)
				require.Equal(t, "dev-1", result.DeviceID)
				require.Len(t, ids, 2)
			}
			require.False(t, gate.held)
			require.Equal(t, len(ids), gate.acquired)
		})
	}
}

func TestMinerReadRetryCancellationAndDeadline(t *testing.T) {
	for _, cancelOnRelease := range []bool{true, false} {
		t.Run(map[bool]string{true: "cancel during backoff", false: "deadline during backoff"}[cancelOnRelease], func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			gate := &readRetryGate{}
			if cancelOnRelease {
				gate.onRelease = cancel
			}
			calls := 0
			m := newTestMinerWithGate(t, readRetrySender(func(context.Context, *gatewaypb.ControlCommand) (*gatewaypb.ControlAck, error) {
				calls++
				return &gatewaypb.ControlAck{Code: gatewaypb.AckCode_ACK_CODE_BUSY}, nil
			}), gate)
			_, err := m.sendWithCommandTimeout(ctx, 10*time.Millisecond, &gatewaypb.MinerCommand{Action: &gatewaypb.MinerCommand_GetErrors{GetErrors: &gatewaypb.GetErrorsAction{}}})
			if cancelOnRelease {
				require.ErrorIs(t, err, context.Canceled)
			} else {
				require.ErrorIs(t, err, context.DeadlineExceeded)
			}
			require.Equal(t, 1, calls)
			require.False(t, gate.held)
		})
	}
}

func TestMinerReadRetryNeverRetriesOtherFailuresOrWrites(t *testing.T) {
	for _, tc := range []struct {
		name        string
		ack         *gatewaypb.ControlAck
		err         error
		write       bool
		generalRead bool
	}{
		{name: "transport", err: errors.New("transport failed")},
		{name: "timeout", err: context.DeadlineExceeded},
		{name: "no stream", err: control.ErrNoActiveStream},
		{name: "resource exhausted", err: connect.NewError(connect.CodeResourceExhausted, errors.New("capacity"))},
		{name: "unknown ACK", ack: &gatewaypb.ControlAck{Code: gatewaypb.AckCode_ACK_CODE_UNSPECIFIED}},
		{name: "failed OK", ack: &gatewaypb.ControlAck{Code: gatewaypb.AckCode_ACK_CODE_OK}},
		{name: "inconsistent BUSY", ack: &gatewaypb.ControlAck{Code: gatewaypb.AckCode_ACK_CODE_BUSY, Succeeded: true}},
		{name: "general read BUSY", ack: &gatewaypb.ControlAck{Code: gatewaypb.AckCode_ACK_CODE_BUSY}, generalRead: true},
		{name: "write BUSY", ack: &gatewaypb.ControlAck{Code: gatewaypb.AckCode_ACK_CODE_BUSY}, write: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			m := newTestMiner(t, readRetrySender(func(context.Context, *gatewaypb.ControlCommand) (*gatewaypb.ControlAck, error) {
				calls++
				return tc.ack, tc.err
			}))
			if tc.write {
				require.Error(t, m.Reboot(t.Context()))
			} else if tc.generalRead {
				_, err := m.GetFirmwareUpdateStatus(t.Context())
				require.Error(t, err)
			} else {
				_, err := m.GetErrors(t.Context())
				require.Error(t, err)
			}
			require.Equal(t, 1, calls)
		})
	}
}

func TestMinerReadRetryReacquiresWithinOriginalDeadline(t *testing.T) {
	var deadline time.Time
	gate := &readRetryGate{onReacquire: func(ctx context.Context) error {
		got, ok := ctx.Deadline()
		require.True(t, ok)
		require.Equal(t, deadline, got)
		<-ctx.Done()
		return ctx.Err()
	}}
	calls := 0
	m := newTestMinerWithGate(t, readRetrySender(func(ctx context.Context, _ *gatewaypb.ControlCommand) (*gatewaypb.ControlAck, error) {
		calls++
		deadline, _ = ctx.Deadline()
		return &gatewaypb.ControlAck{Code: gatewaypb.AckCode_ACK_CODE_BUSY}, nil
	}), gate)
	started := time.Now()
	_, err := m.sendWithCommandTimeout(t.Context(), 60*time.Millisecond, &gatewaypb.MinerCommand{Action: &gatewaypb.MinerCommand_GetErrors{GetErrors: &gatewaypb.GetErrorsAction{}}})
	require.Error(t, err)
	require.Equal(t, 1, calls)
	require.False(t, gate.held)
	require.Less(t, time.Since(started), time.Second)
}
