package remotenode

import (
	"context"
	"errors"
	"strings"
	"testing"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	commandpb "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	"github.com/block/proto-fleet/server/internal/domain/miner/dto"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

func TestRemotePhysicalWritesRequireAdmissionBeforeTransport(t *testing.T) {
	denied := errors.New("operation admission denied")
	for _, tc := range []struct {
		kind commandtype.Type
		run  func(*Miner) error
	}{
		{commandtype.SetTankMining, func(m *Miner) error {
			return m.SetTankMining(t.Context(), sdk.TankMiningRequest{Positions: []int32{1}, Mining: new(false)})
		}},
		{commandtype.Reboot, func(m *Miner) error { return m.Reboot(t.Context()) }},
		{commandtype.StartMining, func(m *Miner) error { return m.StartMining(t.Context()) }},
		{commandtype.StopMining, func(m *Miner) error { return m.StopMining(t.Context()) }},
		{commandtype.Curtail, func(m *Miner) error { return m.Curtail(t.Context(), sdk.CurtailRequest{Level: sdk.CurtailLevelFull}) }},
		{commandtype.Uncurtail, func(m *Miner) error { return m.Uncurtail(t.Context(), sdk.UncurtailRequest{}) }},
		{commandtype.SetCoolingMode, func(m *Miner) error {
			return m.SetCoolingMode(t.Context(), dto.CoolingModePayload{Mode: commonpb.CoolingMode_COOLING_MODE_AIR_COOLED})
		}},
		{commandtype.SetPowerTarget, func(m *Miner) error {
			return m.SetPowerTarget(t.Context(), dto.PowerTargetPayload{PerformanceMode: commandpb.PerformanceMode_PERFORMANCE_MODE_EFFICIENCY})
		}},
		{commandtype.UpdateMiningPools, func(m *Miner) error {
			return m.UpdateMiningPools(t.Context(), dto.UpdateMiningPoolsPayload{DefaultPool: dto.MiningPool{URL: "stratum+tcp://pool.example:3333", Username: "worker"}})
		}},
		{commandtype.UpdateMinerPassword, func(m *Miner) error {
			return m.UpdateMinerPassword(t.Context(), dto.UpdateMinerPasswordPayload{EncryptedPasswordUpdate: testEncryptedPasswordUpdatePayload()})
		}},
		{commandtype.ApplyCurtailmentConfig, func(m *Miner) error {
			return m.ApplyCurtailmentConfig(t.Context(), dto.ApplyCurtailmentConfigPayload{EncryptedConfig: testEncryptedPasswordUpdatePayload()})
		}},
		{commandtype.SetContainerControl, func(m *Miner) error {
			return m.SetContainerControl(t.Context(), sdk.ContainerControlRequest{Name: "dry_cooler", Enabled: new(false)})
		}},
		{commandtype.FirmwareUpdate, func(m *Miner) error {
			return m.FirmwareUpdate(t.Context(), sdk.FirmwareFile{ID: "11111111-1111-1111-1111-111111111111", Filename: "update.swu", Size: 42, SHA256: strings.Repeat("a", 64)})
		}},
	} {
		t.Run(tc.kind.String(), func(t *testing.T) {
			sender := okSender()
			m := newTestMiner(t, sender)
			var admitted commandtype.Type
			m.writeGate = func(_ context.Context, kind commandtype.Type, _ func(context.Context) error) error {
				admitted = kind
				return denied
			}
			require.ErrorIs(t, tc.run(m), denied)
			require.Equal(t, tc.kind, admitted)
			require.Nil(t, sender.cmd, "denied operations must not reach the node")
		})
	}
	sender := okSender()
	m := newTestMiner(t, sender)
	m.writeGate = func(context.Context, commandtype.Type, func(context.Context) error) error { return denied }
	require.NoError(t, m.BlinkLED(t.Context()))
	require.NotNil(t, sender.cmd)
}

func TestRemoteFactoryCarriesPhysicalWriteAdmission(t *testing.T) {
	sender := okSender()
	denied := errors.New("operation admission denied")
	m, err := New(Config{Sender: sender, FleetNodeID: 7, OrgID: 1, DeviceIdentifier: "dev-1", DriverName: "virtual", IPAddress: "10.0.0.5", Port: "4028", URLScheme: "http", WriteGate: func(context.Context, commandtype.Type, func(context.Context) error) error { return denied }})
	require.NoError(t, err)
	require.ErrorIs(t, m.Reboot(t.Context()), denied)
	require.Nil(t, sender.cmd)
}

type observingWriteSender struct {
	CommandSender
	observe func(context.Context) error
}

func (s observingWriteSender) SendCommand(ctx context.Context, _ int64, _ gatewaypb.CommandProtocolVersion, _ *gatewaypb.ControlCommand) (*gatewaypb.ControlAck, error) {
	if err := s.observe(ctx); err != nil {
		return nil, err
	}
	return &gatewaypb.ControlAck{Code: gatewaypb.AckCode_ACK_CODE_OK, Succeeded: true}, nil
}
func TestRemoteAdmissionSpansNodeSendAndForwardsItsContext(t *testing.T) {
	type admissionKey struct{}
	active := false
	m := newTestMiner(t, observingWriteSender{observe: func(ctx context.Context) error {
		require.True(t, active)
		require.Equal(t, "admitted", ctx.Value(admissionKey{}))
		return nil
	}})
	m.writeGate = func(ctx context.Context, kind commandtype.Type, call func(context.Context) error) error {
		require.Equal(t, commandtype.Reboot, kind)
		active = true
		defer func() { active = false }()
		return call(context.WithValue(ctx, admissionKey{}, "admitted"))
	}
	require.NoError(t, m.Reboot(t.Context()))
	require.False(t, active)
}

func TestRemoteAdmissionObservesClassifiedFailures(t *testing.T) {
	for _, operation := range []struct {
		name     string
		run      func(*Miner) error
		artifact bool
	}{
		{"ordinary", func(m *Miner) error { return m.Reboot(t.Context()) }, false},
		{"firmware artifact", func(m *Miner) error {
			return m.FirmwareUpdate(t.Context(), sdk.FirmwareFile{ID: "11111111-1111-1111-1111-111111111111", Filename: "update.swu", Size: 42, SHA256: strings.Repeat("a", 64)})
		}, true},
	} {
		for _, failure := range []struct {
			name       string
			ack        *gatewaypb.ControlAck
			transport  error
			classified func(error) bool
		}{
			{"rejected ack", &gatewaypb.ControlAck{Code: gatewaypb.AckCode_ACK_CODE_UNAUTHENTICATED}, nil, fleeterror.IsAuthenticationError},
			{"transport failure", nil, control.ErrNoActiveStream, fleeterror.IsUnavailableError},
		} {
			t.Run(operation.name+"/"+failure.name, func(t *testing.T) {
				sender := &fakeSender{ack: failure.ack, err: failure.transport}
				miner := newTestMiner(t, sender)
				var observed error
				active := false
				miner.writeGate = func(ctx context.Context, _ commandtype.Type, call func(context.Context) error) error {
					active = true
					defer func() { active = false }()
					observed = call(ctx)
					require.True(t, active)
					require.Error(t, observed)
					require.True(t, failure.classified(observed), "callback must receive the classified failure")
					return observed
				}
				err := operation.run(miner)
				require.ErrorIs(t, err, observed)
				require.NotNil(t, sender.cmd)
				require.Equal(t, operation.artifact, len(sender.artifacts) > 0)
				require.False(t, active)
			})
		}
	}
}

type observingArtifactWriteSender struct {
	CommandSender
	observe func(context.Context)
}

func (s observingArtifactWriteSender) SendCommandWithArtifactResults(ctx context.Context, _ int64, _ gatewaypb.CommandProtocolVersion, _ *gatewaypb.ControlCommand, _ []control.ArtifactExpectation) (*gatewaypb.ControlAck, []*gatewaypb.CommandArtifactRef, error) {
	s.observe(ctx)
	return &gatewaypb.ControlAck{Code: gatewaypb.AckCode_ACK_CODE_OK, Succeeded: true}, nil, nil
}
func TestRemoteAdmissionSpansArtifactSendAndForwardsItsContext(t *testing.T) {
	type admissionKey struct{}
	active := false
	miner := newTestMiner(t, observingArtifactWriteSender{observe: func(ctx context.Context) {
		require.True(t, active)
		require.Equal(t, "admitted", ctx.Value(admissionKey{}))
	}})
	miner.writeGate = func(ctx context.Context, kind commandtype.Type, call func(context.Context) error) error {
		require.Equal(t, commandtype.FirmwareUpdate, kind)
		active = true
		defer func() { active = false }()
		return call(context.WithValue(ctx, admissionKey{}, "admitted"))
	}
	require.NoError(t, miner.FirmwareUpdate(t.Context(), sdk.FirmwareFile{ID: "11111111-1111-1111-1111-111111111111", Filename: "update.swu", Size: 42, SHA256: strings.Repeat("a", 64)}))
	require.False(t, active)
}
