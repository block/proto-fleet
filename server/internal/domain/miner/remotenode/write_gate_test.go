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
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	"github.com/block/proto-fleet/server/internal/domain/miner/dto"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
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
		{commandtype.SetModuleMining, func(m *Miner) error {
			return m.SetModuleMining(t.Context(), sdk.ModuleMiningRequest{Modules: []sdk.ModuleMiningTarget{{Tank: 1, Module: 1, ReportedID: "module-a"}}, Mining: new(false)})
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
			err := tc.run(m)
			require.ErrorIs(t, err, denied)
			require.True(t, devicewrite.IsNotDispatched(err), "ordinary and artifact admission denials are pre-dispatch")
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
	denial := m.Reboot(t.Context())
	require.ErrorIs(t, denial, denied)
	require.True(t, devicewrite.IsNotDispatched(denial))
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
				require.False(t, devicewrite.IsNotDispatched(err), "generic errors after send must retain an unknown outcome")
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

func TestPhysicalWriteKindCoversEveryProtocolAction(t *testing.T) {
	readAndIndicator := map[protoreflect.Name]bool{
		"get_errors": true, "get_cooling_mode": true, "get_mining_pools": true,
		"get_firmware_update_status": true, "download_logs": true, "blink_led": true,
	}
	mutationKinds := map[protoreflect.Name]commandtype.Type{
		"reboot": commandtype.Reboot, "start_mining": commandtype.StartMining, "stop_mining": commandtype.StopMining,
		"curtail": commandtype.Curtail, "uncurtail": commandtype.Uncurtail, "apply_curtailment_config": commandtype.ApplyCurtailmentConfig,
		"set_cooling_mode": commandtype.SetCoolingMode, "set_power_target": commandtype.SetPowerTarget,
		"update_mining_pools": commandtype.UpdateMiningPools, "update_miner_password": commandtype.UpdateMinerPassword,
		"firmware_update": commandtype.FirmwareUpdate, "set_container_control": commandtype.SetContainerControl,
		"set_tank_mining":   commandtype.SetTankMining,
		"set_module_mining": commandtype.SetModuleMining,
		"mining_groups":     commandtype.Type(-1),
		"container_mining":  commandtype.ContainerMining,
	}
	actions := (&gatewaypb.MinerCommand{}).ProtoReflect().Descriptor().Oneofs().ByName("action").Fields()
	require.Equal(t, len(readAndIndicator)+len(mutationKinds), actions.Len(), "classify new protocol actions explicitly")
	for i := range actions.Len() {
		field := actions.Get(i)
		t.Run(string(field.Name()), func(t *testing.T) {
			command := &gatewaypb.MinerCommand{}
			message := command.ProtoReflect()
			message.Set(field, message.NewField(field))
			kind, physical := physicalWriteKind(command)
			if readAndIndicator[field.Name()] {
				require.False(t, physical, "only explicit reads and indicator control may bypass admission")
				return
			}
			expected, classified := mutationKinds[field.Name()]
			require.True(t, classified, "new mutations must receive an explicit command kind")
			require.True(t, physical)
			require.Equal(t, expected, kind)
		})
	}
}

func TestPhysicalWriteKindFencesAbsentAndUnknownActions(t *testing.T) {
	unknown := &gatewaypb.MinerCommand{}
	// Unknown field 127 with an empty message represents a future wire action.
	require.NoError(t, proto.Unmarshal([]byte{0xfa, 0x07, 0x00}, unknown))
	for _, command := range []*gatewaypb.MinerCommand{nil, {}, unknown} {
		kind, physical := physicalWriteKind(command)
		require.True(t, physical)
		require.Equal(t, commandtype.Type(-1), kind)
	}
}

func TestRemoteReadAndIndicatorEntrypointsBypassWriteAdmission(t *testing.T) {
	for _, tc := range []struct {
		name    string
		payload proto.Message
		run     func(*Miner) error
		partial bool
	}{
		{"GetErrors", &gatewaypb.GetErrorsResult{DeviceId: "dev-1"}, func(m *Miner) error { _, err := m.GetErrors(t.Context()); return err }, false},
		{"GetCoolingMode", &gatewaypb.GetCoolingModeResult{Mode: commonpb.CoolingMode_COOLING_MODE_AIR_COOLED}, func(m *Miner) error { _, err := m.GetCoolingMode(t.Context()); return err }, false},
		{"GetMiningPools", &gatewaypb.GetMiningPoolsResult{}, func(m *Miner) error { _, err := m.GetMiningPools(t.Context()); return err }, false},
		{"GetFirmwareUpdateStatus", nil, func(m *Miner) error { _, err := m.GetFirmwareUpdateStatus(t.Context()); return err }, false},
		{"DownloadLogs", nil, func(m *Miner) error { return m.DownloadLogs(t.Context(), "batch-1") }, false},
		{"DownloadLogs/PARTIAL", nil, func(m *Miner) error { return m.DownloadLogs(t.Context(), "batch-1") }, true},
		{"BlinkLED", nil, func(m *Miner) error { return m.BlinkLED(t.Context()) }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sender := okSender()
			if tc.payload != nil {
				var err error
				sender.ack.Payload, err = proto.Marshal(tc.payload)
				require.NoError(t, err)
			}
			sender.refs = []*gatewaypb.CommandArtifactRef{{
				ArtifactId: "artifact-1", Purpose: gatewaypb.CommandArtifactPurpose_COMMAND_ARTIFACT_PURPOSE_MINER_LOGS,
				Filename: "logs.csv", SizeBytes: 123, Sha256: "3a6eb0790f39ac87c94f3856b2dd2c5d110e6811602261a9a923d3bb23adc8b7",
			}}
			if tc.partial {
				sender.ack.Code = gatewaypb.AckCode_ACK_CODE_PARTIAL
				sender.ack.Succeeded = false
				sender.ack.ErrorMessage = "uploaded partial miner log data"
			}
			m := newTestMiner(t, sender)
			saver := &fakeLogArtifactSaver{}
			m.logArtifacts = saver
			m.writeGate = func(context.Context, commandtype.Type, func(context.Context) error) error {
				t.Fatalf("read or indicator entrypoint must not enter physical-write admission")
				return nil
			}
			err := tc.run(m)
			if tc.partial {
				require.True(t, fleeterror.IsFailedPreconditionError(err))
				require.Contains(t, err.Error(), "uploaded partial miner log data")
			} else {
				require.NoError(t, err)
			}
			require.NotNil(t, sender.cmd)
			if strings.HasPrefix(tc.name, "DownloadLogs") {
				require.Equal(t, "artifact-1", saver.artifactID, "partial artifacts must still be materialized")
			}
		})
	}
}

func TestRemoteGenericAckCodesRetainUnknownDispatchOutcome(t *testing.T) {
	for _, code := range []gatewaypb.AckCode{gatewaypb.AckCode_ACK_CODE_BAD_REQUEST, gatewaypb.AckCode_ACK_CODE_AGENT_INCAPABLE, gatewaypb.AckCode_ACK_CODE_BUSY} {
		for _, artifact := range []bool{false, true} {
			name := code.String()
			if artifact {
				name += "/artifact"
			}
			t.Run(name, func(t *testing.T) {
				sender := &fakeSender{ack: &gatewaypb.ControlAck{Code: code}}
				m := newTestMiner(t, sender)
				var observed error
				m.writeGate = func(ctx context.Context, _ commandtype.Type, call func(context.Context) error) error {
					observed = call(ctx)
					return observed
				}
				var err error
				if artifact {
					err = m.FirmwareUpdate(t.Context(), sdk.FirmwareFile{ID: "11111111-1111-1111-1111-111111111111", Filename: "update.swu", Size: 42, SHA256: strings.Repeat("a", 64)})
				} else {
					err = m.Reboot(t.Context())
				}
				require.Error(t, err)
				require.ErrorIs(t, err, observed)
				require.NotNil(t, sender.cmd)
				require.Equal(t, code == gatewaypb.AckCode_ACK_CODE_BUSY, devicewrite.IsNotDispatched(err), "generic ACK codes cannot prove whether device I/O began")
			})
		}
	}
}

func TestRemoteInvalidTankRequestInsideOuterDispatchIsNotDispatched(t *testing.T) {
	for _, tc := range []struct {
		name    string
		request sdk.TankMiningRequest
	}{
		{"missing mining state", sdk.TankMiningRequest{Positions: []int32{1}}},
		{"missing positions", sdk.TankMiningRequest{Mining: new(false)}},
		{"nonpositive position", sdk.TankMiningRequest{Positions: []int32{0}, Mining: new(false)}},
		{"duplicate positions", sdk.TankMiningRequest{Positions: []int32{1, 1}, Mining: new(false)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sender := okSender()
			miner := newTestMiner(t, sender)
			miner.writeGate = func(context.Context, commandtype.Type, func(context.Context) error) error {
				t.Fatalf("invalid tank request must fail before write admission")
				return nil
			}
			validation := tc.request.Validate()
			require.Error(t, validation)
			outcome := devicewrite.Dispatch(t.Context(), func(ctx context.Context) error {
				return miner.SetTankMining(ctx, tc.request)
			})
			require.Error(t, outcome)
			require.EqualError(t, outcome, validation.Error())
			require.True(t, devicewrite.IsNotDispatched(outcome), "outer dispatch must see that local validation prevented transport")
			require.Nil(t, sender.cmd)
		})
	}
}

func TestMiningGroupReadAdmissionRequiresAValidReadRequest(t *testing.T) {
	for _, tc := range []struct {
		payload  string
		physical bool
	}{
		{`{"action":"observe"}`, false},
		{`{"action":"operation","group_id":"receipt"}`, false},
		{`{"action":"start","group_id":"g","revision":1,"key":"k"}`, true},
		{`{"action":"observe","pools":[{"password":"secret"}]}`, true},
	} {
		_, physical := physicalWriteKind(&gatewaypb.MinerCommand{Action: &gatewaypb.MinerCommand_MiningGroups{MiningGroups: &gatewaypb.MiningGroupsAction{Request: []byte(tc.payload)}}})
		require.Equal(t, tc.physical, physical)
	}
}
