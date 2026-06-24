package remotenode

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	curtailmentpb "github.com/block/proto-fleet/server/generated/grpc/curtailment/v1"
	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	minercommandpb "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	"github.com/block/proto-fleet/server/internal/domain/miner/dto"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

type fakeSender struct {
	cmd *gatewaypb.ControlCommand
	ack *gatewaypb.ControlAck
	err error
}

func (f *fakeSender) SendCommand(_ context.Context, _ int64, cmd *gatewaypb.ControlCommand) (*gatewaypb.ControlAck, error) {
	f.cmd = cmd
	return f.ack, f.err
}

func okSender() *fakeSender {
	return &fakeSender{ack: &gatewaypb.ControlAck{Succeeded: true, Code: gatewaypb.AckCode_ACK_CODE_OK}}
}

func newTestMiner(t *testing.T, s CommandSender) *Miner {
	t.Helper()
	m, err := New(Config{
		Sender: s, FleetNodeID: 7, OrgID: 1,
		DeviceIdentifier: "dev-1", DriverName: "virtual",
		IPAddress: "10.0.0.5", Port: "4028", URLScheme: "http",
		SerialNumber: "SN1", MacAddress: "AA:BB:CC:DD:EE:FF",
	})
	require.NoError(t, err)
	return m
}

func decodeSent(t *testing.T, s *fakeSender) *gatewaypb.MinerCommand {
	t.Helper()
	require.NotNil(t, s.cmd, "no command was sent")
	require.NotEmpty(t, s.cmd.GetCommandId())
	var env gatewaypb.AgentCommand
	require.NoError(t, proto.Unmarshal(s.cmd.GetPayload(), &env))
	mc := env.GetMinerCommand()
	require.NotNil(t, mc, "payload was not a MinerCommand")
	return mc
}

func TestMiner_EncodesActionAndTarget(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name  string
		call  func(*Miner) error
		check func(*testing.T, *gatewaypb.MinerCommand)
	}{
		{"reboot", func(m *Miner) error { return m.Reboot(ctx) }, func(t *testing.T, mc *gatewaypb.MinerCommand) {
			assert.NotNil(t, mc.GetReboot())
		}},
		{"start", func(m *Miner) error { return m.StartMining(ctx) }, func(t *testing.T, mc *gatewaypb.MinerCommand) {
			assert.NotNil(t, mc.GetStartMining())
		}},
		{"stop", func(m *Miner) error { return m.StopMining(ctx) }, func(t *testing.T, mc *gatewaypb.MinerCommand) {
			assert.NotNil(t, mc.GetStopMining())
		}},
		{"blink", func(m *Miner) error { return m.BlinkLED(ctx) }, func(t *testing.T, mc *gatewaypb.MinerCommand) {
			assert.NotNil(t, mc.GetBlinkLed())
		}},
		{"uncurtail", func(m *Miner) error { return m.Uncurtail(ctx, sdk.UncurtailRequest{}) }, func(t *testing.T, mc *gatewaypb.MinerCommand) {
			assert.NotNil(t, mc.GetUncurtail())
		}},
		{"curtail full", func(m *Miner) error { return m.Curtail(ctx, sdk.CurtailRequest{Level: sdk.CurtailLevelFull}) }, func(t *testing.T, mc *gatewaypb.MinerCommand) {
			assert.Equal(t, curtailmentpb.CurtailmentLevel_CURTAILMENT_LEVEL_FULL, mc.GetCurtail().GetLevel())
		}},
		{"cooling immersion", func(m *Miner) error {
			return m.SetCoolingMode(ctx, dto.CoolingModePayload{Mode: commonpb.CoolingMode_COOLING_MODE_IMMERSION_COOLED})
		}, func(t *testing.T, mc *gatewaypb.MinerCommand) {
			assert.Equal(t, commonpb.CoolingMode_COOLING_MODE_IMMERSION_COOLED, mc.GetSetCoolingMode().GetMode())
		}},
		{"power efficiency", func(m *Miner) error {
			return m.SetPowerTarget(ctx, dto.PowerTargetPayload{PerformanceMode: minercommandpb.PerformanceMode_PERFORMANCE_MODE_EFFICIENCY})
		}, func(t *testing.T, mc *gatewaypb.MinerCommand) {
			assert.Equal(t, minercommandpb.PerformanceMode_PERFORMANCE_MODE_EFFICIENCY, mc.GetSetPowerTarget().GetPerformanceMode())
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			s := okSender()
			m := newTestMiner(t, s)

			// Act
			err := tc.call(m)

			// Assert
			require.NoError(t, err)
			mc := decodeSent(t, s)
			assert.Equal(t, "dev-1", mc.GetTarget().GetDeviceIdentifier())
			assert.Equal(t, "10.0.0.5", mc.GetTarget().GetIpAddress())
			tc.check(t, mc)
		})
	}
}

func TestMiner_AckMapping(t *testing.T) {
	cases := []struct {
		name   string
		ack    *gatewaypb.ControlAck
		err    error
		expect func(*testing.T, error)
	}{
		{"ok", &gatewaypb.ControlAck{Succeeded: true, Code: gatewaypb.AckCode_ACK_CODE_OK}, nil, func(t *testing.T, err error) {
			assert.NoError(t, err)
		}},
		{"unauthenticated", &gatewaypb.ControlAck{Code: gatewaypb.AckCode_ACK_CODE_UNAUTHENTICATED}, nil, func(t *testing.T, err error) {
			assert.True(t, fleeterror.IsAuthenticationError(err), "should be an auth error so the cache is evicted")
		}},
		{"unimplemented", &gatewaypb.ControlAck{Code: gatewaypb.AckCode_ACK_CODE_UNIMPLEMENTED}, nil, func(t *testing.T, err error) {
			assert.True(t, fleeterror.IsUnimplementedError(err))
		}},
		{"bad request", &gatewaypb.ControlAck{Code: gatewaypb.AckCode_ACK_CODE_BAD_REQUEST}, nil, func(t *testing.T, err error) {
			assert.True(t, fleeterror.IsInvalidArgumentError(err))
		}},
		{"busy", &gatewaypb.ControlAck{Code: gatewaypb.AckCode_ACK_CODE_BUSY}, nil, func(t *testing.T, err error) {
			// BUSY must be retryable, not permanent: the queue only treats
			// Unimplemented/FailedPrecondition as permanent, so neither must hold here
			// or a saturated node would permanently fail a large batch.
			require.Error(t, err)
			assert.False(t, fleeterror.IsFailedPreconditionError(err))
			assert.False(t, fleeterror.IsUnimplementedError(err))
		}},
		{"internal", &gatewaypb.ControlAck{Code: gatewaypb.AckCode_ACK_CODE_INTERNAL}, nil, func(t *testing.T, err error) {
			require.Error(t, err)
			assert.False(t, fleeterror.IsAuthenticationError(err))
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			s := &fakeSender{ack: tc.ack, err: tc.err}
			m := newTestMiner(t, s)

			// Act
			err := m.Reboot(context.Background())

			// Assert
			tc.expect(t, err)
		})
	}
}

func TestMiner_NoActiveStreamIsRetryable(t *testing.T) {
	// Arrange: the registry reports the node offline.
	m := newTestMiner(t, &fakeSender{err: control.ErrNoActiveStream})

	// Act
	err := m.Reboot(context.Background())

	// Assert: a transient disconnect must be retryable, not permanent, so a queued
	// command re-attempts across a reconnect instead of being dropped on the first blip.
	require.Error(t, err)
	assert.True(t, fleeterror.IsUnavailableError(err))
	assert.False(t, fleeterror.IsFailedPreconditionError(err))
}

func TestClampAckReason(t *testing.T) {
	// Arrange: an untrusted oversized ack message (a well-behaved node caps at the limit).
	oversized := strings.Repeat("x", maxAckReasonBytes*2)

	// Act
	clamped := clampAckReason(oversized)

	// Assert: bounded to the cap, still valid UTF-8, and short messages pass through.
	assert.LessOrEqual(t, len(clamped), maxAckReasonBytes)
	assert.True(t, utf8.ValidString(clamped))
	assert.Equal(t, "short reason", clampAckReason("short reason"))
}

func TestMiner_UnsupportedMethodsReturnUnimplemented(t *testing.T) {
	// Arrange
	m := newTestMiner(t, okSender())
	ctx := context.Background()

	// Assert: not-yet-supported methods return Unimplemented.
	assert.True(t, fleeterror.IsUnimplementedError(m.UpdateMiningPools(ctx, dto.UpdateMiningPoolsPayload{})))
	assert.True(t, fleeterror.IsUnimplementedError(m.Unpair(ctx)))
	assert.True(t, fleeterror.IsUnimplementedError(m.DownloadLogs(ctx, "batch")))
	_, err := m.GetDeviceStatus(ctx)
	assert.True(t, fleeterror.IsUnimplementedError(err))
}
