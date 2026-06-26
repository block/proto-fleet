package remotenode

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	curtailmentpb "github.com/block/proto-fleet/server/generated/grpc/curtailment/v1"
	errorspb "github.com/block/proto-fleet/server/generated/grpc/errors/v1"
	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	minercommandpb "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/passwordupdate"
	"github.com/block/proto-fleet/server/internal/domain/miner/dto"
	"github.com/block/proto-fleet/server/internal/domain/miner/logformat"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

type fakeSender struct {
	cmd       *gatewaypb.ControlCommand
	ack       *gatewaypb.ControlAck
	err       error
	artifacts []control.ArtifactExpectation
	refs      []*gatewaypb.CommandArtifactRef
}

func (f *fakeSender) SendCommand(_ context.Context, _ int64, cmd *gatewaypb.ControlCommand) (*gatewaypb.ControlAck, error) {
	f.cmd = cmd
	return f.ack, f.err
}

func (f *fakeSender) SendCommandWithArtifactResults(_ context.Context, _ int64, cmd *gatewaypb.ControlCommand, artifacts []control.ArtifactExpectation) (*gatewaypb.ControlAck, []*gatewaypb.CommandArtifactRef, error) {
	f.cmd = cmd
	f.artifacts = artifacts
	return f.ack, f.refs, f.err
}

type fakeLogArtifactSaver struct {
	batchLogUUID string
	macAddress   string
	artifactID   string
	err          error
}

func (f *fakeLogArtifactSaver) SaveCommandArtifactLog(batchLogUUID string, macAddress string, artifactID string) (string, error) {
	f.batchLogUUID = batchLogUUID
	f.macAddress = macAddress
	f.artifactID = artifactID
	if f.err != nil {
		return "", f.err
	}
	return "logs/" + batchLogUUID + "/miner-logs.csv", nil
}

type blockingSender struct {
	started chan struct{}
}

func (s *blockingSender) SendCommand(ctx context.Context, _ int64, _ *gatewaypb.ControlCommand) (*gatewaypb.ControlAck, error) {
	close(s.started)
	<-ctx.Done()
	return nil, fmt.Errorf("wait for ack: %w", ctx.Err())
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

func newTestMinerWithLogSaver(t *testing.T, s CommandSender, saver LogArtifactSaver) *Miner {
	t.Helper()
	m, err := New(Config{
		Sender: s, FleetNodeID: 7, OrgID: 1,
		LogArtifacts:     saver,
		DeviceIdentifier: "dev-1", DriverName: "virtual",
		IPAddress: "10.0.0.5", Port: "4028", URLScheme: "http",
		SerialNumber: "SN1", MacAddress: "AA:BB:CC:DD:EE:FF",
	})
	require.NoError(t, err)
	return m
}

func newTestMinerWithGate(t *testing.T, s CommandSender, gate Gate) *Miner {
	t.Helper()
	m, err := New(Config{
		Sender: s, Gate: gate, FleetNodeID: 7, OrgID: 1,
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
		call  func(*Miner, *fakeSender) error
		check func(*testing.T, *gatewaypb.MinerCommand)
	}{
		{"reboot", func(m *Miner, _ *fakeSender) error { return m.Reboot(ctx) }, func(t *testing.T, mc *gatewaypb.MinerCommand) {
			assert.NotNil(t, mc.GetReboot())
		}},
		{"start", func(m *Miner, _ *fakeSender) error { return m.StartMining(ctx) }, func(t *testing.T, mc *gatewaypb.MinerCommand) {
			assert.NotNil(t, mc.GetStartMining())
		}},
		{"stop", func(m *Miner, _ *fakeSender) error { return m.StopMining(ctx) }, func(t *testing.T, mc *gatewaypb.MinerCommand) {
			assert.NotNil(t, mc.GetStopMining())
		}},
		{"blink", func(m *Miner, _ *fakeSender) error { return m.BlinkLED(ctx) }, func(t *testing.T, mc *gatewaypb.MinerCommand) {
			assert.NotNil(t, mc.GetBlinkLed())
		}},
		{"uncurtail", func(m *Miner, _ *fakeSender) error { return m.Uncurtail(ctx, sdk.UncurtailRequest{}) }, func(t *testing.T, mc *gatewaypb.MinerCommand) {
			assert.NotNil(t, mc.GetUncurtail())
		}},
		{"curtail full", func(m *Miner, _ *fakeSender) error {
			return m.Curtail(ctx, sdk.CurtailRequest{Level: sdk.CurtailLevelFull})
		}, func(t *testing.T, mc *gatewaypb.MinerCommand) {
			assert.Equal(t, curtailmentpb.CurtailmentLevel_CURTAILMENT_LEVEL_FULL, mc.GetCurtail().GetLevel())
		}},
		{"cooling immersion", func(m *Miner, _ *fakeSender) error {
			return m.SetCoolingMode(ctx, dto.CoolingModePayload{Mode: commonpb.CoolingMode_COOLING_MODE_IMMERSION_COOLED})
		}, func(t *testing.T, mc *gatewaypb.MinerCommand) {
			assert.Equal(t, commonpb.CoolingMode_COOLING_MODE_IMMERSION_COOLED, mc.GetSetCoolingMode().GetMode())
		}},
		{"power efficiency", func(m *Miner, _ *fakeSender) error {
			return m.SetPowerTarget(ctx, dto.PowerTargetPayload{PerformanceMode: minercommandpb.PerformanceMode_PERFORMANCE_MODE_EFFICIENCY})
		}, func(t *testing.T, mc *gatewaypb.MinerCommand) {
			assert.Equal(t, minercommandpb.PerformanceMode_PERFORMANCE_MODE_EFFICIENCY, mc.GetSetPowerTarget().GetPerformanceMode())
		}},
		{"update mining pools", func(m *Miner, _ *fakeSender) error {
			return m.UpdateMiningPools(ctx, dto.UpdateMiningPoolsPayload{
				DefaultPool: dto.MiningPool{
					Priority: 0,
					URL:      "stratum+tcp://pool1.example.com:3333",
					Username: "worker1",
				},
				Backup1Pool: &dto.MiningPool{
					Priority: 1,
					URL:      "stratum+tcp://pool2.example.com:3333",
					Username: "worker2",
				},
				Backup2Pool: &dto.MiningPool{
					Priority: 2,
					URL:      "stratum+tcp://pool4.example.com:3333",
					Username: "worker4",
				},
			})
		}, func(t *testing.T, mc *gatewaypb.MinerCommand) {
			pools := mc.GetUpdateMiningPools().GetPools()
			require.Len(t, pools, 3)
			assert.Equal(t, int32(0), pools[0].GetPriority())
			assert.Equal(t, "stratum+tcp://pool1.example.com:3333", pools[0].GetUrl())
			assert.Equal(t, "worker1", pools[0].GetUsername())
			assert.Equal(t, int32(1), pools[1].GetPriority())
			assert.Equal(t, "stratum+tcp://pool2.example.com:3333", pools[1].GetUrl())
			assert.Equal(t, "worker2", pools[1].GetUsername())
			assert.Equal(t, int32(2), pools[2].GetPriority())
			assert.Equal(t, "stratum+tcp://pool4.example.com:3333", pools[2].GetUrl())
			assert.Equal(t, "worker4", pools[2].GetUsername())
		}},
		{"update miner password", func(m *Miner, s *fakeSender) error {
			payload, err := proto.Marshal(&gatewaypb.UpdateMinerPasswordResult{
				EncryptedCredentials: &gatewaypb.EncryptedCredentials{
					Username: []byte("node-user"),
					Password: []byte("node-pass"),
				},
			})
			require.NoError(t, err)
			s.ack.Payload = payload
			return m.UpdateMinerPassword(ctx, dto.UpdateMinerPasswordPayload{
				EncryptedPasswordUpdate: testEncryptedPasswordUpdatePayload(),
			})
		}, func(t *testing.T, mc *gatewaypb.MinerCommand) {
			action := mc.GetUpdateMinerPassword()
			require.NotNil(t, action)
			assert.Equal(t, passwordupdate.Algorithm, action.GetEncryptedPasswordUpdate().GetAlgorithm())
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			s := okSender()
			m := newTestMiner(t, s)

			// Act
			err := tc.call(m, s)

			// Assert
			require.NoError(t, err)
			mc := decodeSent(t, s)
			assert.Equal(t, "dev-1", mc.GetTarget().GetDeviceIdentifier())
			assert.Equal(t, "10.0.0.5", mc.GetTarget().GetIpAddress())
			tc.check(t, mc)
		})
	}
}

func TestMiner_UpdateMinerPasswordWithCredentials_DecodesEncryptedCredentials(t *testing.T) {
	// Arrange
	payload, err := proto.Marshal(&gatewaypb.UpdateMinerPasswordResult{
		EncryptedCredentials: &gatewaypb.EncryptedCredentials{
			Username: []byte("node-user"),
			Password: []byte("node-pass"),
		},
	})
	require.NoError(t, err)
	s := &fakeSender{ack: &gatewaypb.ControlAck{
		Succeeded: true,
		Code:      gatewaypb.AckCode_ACK_CODE_OK,
		Payload:   payload,
	}}
	m := newTestMiner(t, s)

	// Act
	creds, err := m.UpdateMinerPasswordWithCredentials(context.Background(), dto.UpdateMinerPasswordPayload{
		EncryptedPasswordUpdate: testEncryptedPasswordUpdatePayload(),
	})

	// Assert
	require.NoError(t, err)
	assert.Equal(t, []byte("node-user"), creds.GetUsername())
	assert.Equal(t, []byte("node-pass"), creds.GetPassword())
}

func testEncryptedPasswordUpdatePayload() *dto.NodeEncryptedPayload {
	return &dto.NodeEncryptedPayload{
		Algorithm:       passwordupdate.Algorithm,
		EphemeralPubkey: bytes.Repeat([]byte{1}, 32),
		Nonce:           bytes.Repeat([]byte{2}, 12),
		Ciphertext:      bytes.Repeat([]byte{3}, 17),
	}
}

func TestMiner_UpdateMinerPassword_RejectsInvalidPayloadData(t *testing.T) {
	cases := []struct {
		name    string
		payload []byte
	}{
		{"malformed", []byte{0xff}},
		{"missing credentials", mustMarshalUpdateMinerPasswordResult(t, &gatewaypb.UpdateMinerPasswordResult{})},
		{"empty username", mustMarshalUpdateMinerPasswordResult(t, &gatewaypb.UpdateMinerPasswordResult{
			EncryptedCredentials: &gatewaypb.EncryptedCredentials{Password: []byte("node-pass")},
		})},
		{"empty password", mustMarshalUpdateMinerPasswordResult(t, &gatewaypb.UpdateMinerPasswordResult{
			EncryptedCredentials: &gatewaypb.EncryptedCredentials{Username: []byte("node-user")},
		})},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			s := &fakeSender{ack: &gatewaypb.ControlAck{
				Succeeded: true,
				Code:      gatewaypb.AckCode_ACK_CODE_OK,
				Payload:   tc.payload,
			}}
			m := newTestMiner(t, s)

			// Act
			err := m.UpdateMinerPassword(context.Background(), dto.UpdateMinerPasswordPayload{
				EncryptedPasswordUpdate: testEncryptedPasswordUpdatePayload(),
			})

			// Assert
			require.Error(t, err)
			require.NotNil(t, s.cmd)
			assert.True(t, fleeterror.IsFailedPreconditionError(err), "expected FailedPrecondition, got %v", err)
		})
	}
}

func mustMarshalUpdateMinerPasswordResult(t *testing.T, result *gatewaypb.UpdateMinerPasswordResult) []byte {
	t.Helper()
	payload, err := proto.Marshal(result)
	require.NoError(t, err)
	return payload
}

func TestMiner_GetMiningPools_DecodesPayload(t *testing.T) {
	// Arrange
	payload, err := proto.Marshal(&gatewaypb.GetMiningPoolsResult{
		Pools: []*gatewaypb.MiningPoolConfig{
			{Priority: 0, Url: "stratum+tcp://pool1.example.com:3333", Username: "worker1"},
			{Priority: 2, Url: "stratum+tcp://pool4.example.com:3333", Username: "worker4"},
		},
	})
	require.NoError(t, err)
	s := &fakeSender{ack: &gatewaypb.ControlAck{
		Succeeded: true,
		Code:      gatewaypb.AckCode_ACK_CODE_OK,
		Payload:   payload,
	}}
	m := newTestMiner(t, s)

	// Act
	pools, err := m.GetMiningPools(context.Background())

	// Assert
	require.NoError(t, err)
	require.Len(t, pools, 2)
	assert.Equal(t, int32(0), pools[0].Priority)
	assert.Equal(t, "stratum+tcp://pool1.example.com:3333", pools[0].URL)
	assert.Equal(t, "worker1", pools[0].Username)
	assert.Equal(t, int32(2), pools[1].Priority)
	assert.Equal(t, "stratum+tcp://pool4.example.com:3333", pools[1].URL)
	assert.Equal(t, "worker4", pools[1].Username)
	assert.NotNil(t, decodeSent(t, s).GetGetMiningPools())
}

func TestMiner_GetErrors_DecodesPayload(t *testing.T) {
	// Arrange
	now := time.Now().UTC().Truncate(time.Millisecond)
	closedAt := now.Add(time.Hour)
	componentID := "psu-0"
	payload, err := proto.Marshal(&gatewaypb.GetErrorsResult{
		DeviceId:           "dev-1",
		Truncated:          true,
		OmittedReportCount: 3,
		Errors: []*gatewaypb.MinerErrorReport{{
			MinerError:        errorspb.MinerError_MINER_ERROR_PSU_FAULT_GENERIC,
			CauseSummary:      "PSU fault",
			RecommendedAction: "Replace PSU",
			Severity:          errorspb.Severity_SEVERITY_CRITICAL,
			FirstSeenAt:       timestamppb.New(now),
			LastSeenAt:        timestamppb.New(now.Add(time.Minute)),
			ClosedAt:          timestamppb.New(closedAt),
			VendorAttributes: map[string]string{
				"vendor_code": "PSU_001",
				"firmware":    "v1.2.3",
			},
			DeviceId:      "dev-1",
			ComponentId:   &componentID,
			Impact:        "Stops mining",
			Summary:       "Power supply fault detected",
			ComponentType: errorspb.ComponentType_COMPONENT_TYPE_PSU,
		}},
	})
	require.NoError(t, err)
	s := &fakeSender{ack: &gatewaypb.ControlAck{
		Succeeded: true,
		Code:      gatewaypb.AckCode_ACK_CODE_OK,
		Payload:   payload,
	}}
	m := newTestMiner(t, s)

	// Act
	deviceErrors, err := m.GetErrors(context.Background())

	// Assert
	require.NoError(t, err)
	assert.Equal(t, "dev-1", deviceErrors.DeviceID)
	assert.True(t, deviceErrors.Partial)
	assert.Equal(t, uint32(3), deviceErrors.OmittedReportCount)
	require.Len(t, deviceErrors.Errors, 1)
	got := deviceErrors.Errors[0]
	assert.Equal(t, "PSU fault", got.CauseSummary)
	assert.Equal(t, "Replace PSU", got.RecommendedAction)
	assert.Equal(t, "Power supply fault detected", got.Summary)
	assert.Equal(t, "PSU_001", got.VendorCode)
	assert.Equal(t, "v1.2.3", got.Firmware)
	assert.Equal(t, &componentID, got.ComponentID)
	assert.NotZero(t, got.MinerError)
	assert.NotZero(t, got.Severity)
	assert.NotZero(t, got.ComponentType)
	assert.Equal(t, now, got.FirstSeenAt)
	assert.Equal(t, now.Add(time.Minute), got.LastSeenAt)
	require.NotNil(t, got.ClosedAt)
	assert.Equal(t, closedAt, *got.ClosedAt)
	assert.NotNil(t, decodeSent(t, s).GetGetErrors())
}

func TestMiner_GetErrors_ClampsDerivedDatabaseColumns(t *testing.T) {
	// Arrange
	overlongVendorCode := strings.Repeat("v", maxErrorColumnStringLen+1)
	overlongFirmware := strings.Repeat("f", maxErrorColumnStringLen+1)
	payload, err := proto.Marshal(&gatewaypb.GetErrorsResult{
		DeviceId: "dev-1",
		Errors: []*gatewaypb.MinerErrorReport{{
			DeviceId:      "dev-1",
			MinerError:    errorspb.MinerError_MINER_ERROR_PSU_FAULT_GENERIC,
			Severity:      errorspb.Severity_SEVERITY_CRITICAL,
			ComponentType: errorspb.ComponentType_COMPONENT_TYPE_PSU,
			VendorAttributes: map[string]string{
				"vendor_code": overlongVendorCode,
				"firmware":    overlongFirmware,
			},
		}},
	})
	require.NoError(t, err)
	m := newTestMiner(t, &fakeSender{ack: &gatewaypb.ControlAck{
		Succeeded: true,
		Code:      gatewaypb.AckCode_ACK_CODE_OK,
		Payload:   payload,
	}})

	// Act
	deviceErrors, err := m.GetErrors(context.Background())

	// Assert
	require.NoError(t, err)
	require.Len(t, deviceErrors.Errors, 1)
	got := deviceErrors.Errors[0]
	assert.Len(t, got.VendorCode, maxErrorColumnStringLen)
	assert.Len(t, got.Firmware, maxErrorColumnStringLen)
	assert.Equal(t, overlongVendorCode, got.VendorAttributes["vendor_code"])
	assert.Equal(t, overlongFirmware, got.VendorAttributes["firmware"])
}

func TestMiner_GetErrors_MalformedPayloadReturnsInternal(t *testing.T) {
	// Arrange
	m := newTestMiner(t, &fakeSender{ack: &gatewaypb.ControlAck{
		Succeeded: true,
		Code:      gatewaypb.AckCode_ACK_CODE_OK,
		Payload:   []byte{0xff},
	}})

	// Act
	deviceErrors, err := m.GetErrors(context.Background())

	// Assert
	require.Error(t, err)
	assert.Empty(t, deviceErrors.Errors)
	assert.Contains(t, err.Error(), "unmarshal get errors result")
}

type releasableGate struct {
	acquired chan int64
	release  chan struct{}
}

func (g *releasableGate) Acquire(ctx context.Context, fleetNodeID int64) (func(), error) {
	g.acquired <- fleetNodeID
	select {
	case <-g.release:
		return func() {}, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("waiting for release: %w", ctx.Err())
	}
}

func TestMiner_GetErrors_UsesBoundedCommandContext(t *testing.T) {
	// Arrange
	oldTimeout := remoteGetErrorsCommandTimeout
	remoteGetErrorsCommandTimeout = 25 * time.Millisecond
	t.Cleanup(func() { remoteGetErrorsCommandTimeout = oldTimeout })
	s := &blockingSender{started: make(chan struct{})}
	m := newTestMiner(t, s)

	// Act
	startedAt := time.Now()
	_, err := m.GetErrors(context.Background())

	// Assert
	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.True(t, fleeterror.IsConnectionError(err), "expected connection error, got %v", err)
	assert.Less(t, time.Since(startedAt), time.Second)
	select {
	case <-s.started:
	default:
		t.Fatal("SendCommand was not called")
	}
}

func TestMiner_GetErrors_CommandTimeoutStartsAfterGateAcquisition(t *testing.T) {
	// Arrange
	oldTimeout := remoteGetErrorsCommandTimeout
	remoteGetErrorsCommandTimeout = 25 * time.Millisecond
	t.Cleanup(func() { remoteGetErrorsCommandTimeout = oldTimeout })
	payload, err := proto.Marshal(&gatewaypb.GetErrorsResult{DeviceId: "dev-1"})
	require.NoError(t, err)
	gate := &releasableGate{
		acquired: make(chan int64, 1),
		release:  make(chan struct{}),
	}
	m := newTestMinerWithGate(t, &fakeSender{ack: &gatewaypb.ControlAck{
		Succeeded: true,
		Code:      gatewaypb.AckCode_ACK_CODE_OK,
		Payload:   payload,
	}}, gate)
	resultCh := make(chan error, 1)

	// Act
	go func() {
		_, err := m.GetErrors(context.Background())
		resultCh <- err
	}()
	require.Equal(t, int64(7), <-gate.acquired)
	time.Sleep(2 * remoteGetErrorsCommandTimeout)
	close(gate.release)

	// Assert
	select {
	case err := <-resultCh:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for GetErrors")
	}
}

func TestMiner_GetErrors_RejectsMismatchedResultDeviceID(t *testing.T) {
	// Arrange
	payload, err := proto.Marshal(&gatewaypb.GetErrorsResult{DeviceId: "other-device"})
	require.NoError(t, err)
	m := newTestMiner(t, &fakeSender{ack: &gatewaypb.ControlAck{
		Succeeded: true,
		Code:      gatewaypb.AckCode_ACK_CODE_OK,
		Payload:   payload,
	}})

	// Act
	_, err = m.GetErrors(context.Background())

	// Assert
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not match requested device")
}

func TestMiner_GetErrors_RejectsMismatchedErrorDeviceID(t *testing.T) {
	// Arrange
	payload, err := proto.Marshal(&gatewaypb.GetErrorsResult{
		DeviceId: "dev-1",
		Errors: []*gatewaypb.MinerErrorReport{{
			DeviceId:      "other-device",
			CauseSummary:  "wrong miner",
			ComponentType: errorspb.ComponentType_COMPONENT_TYPE_PSU,
		}},
	})
	require.NoError(t, err)
	m := newTestMiner(t, &fakeSender{ack: &gatewaypb.ControlAck{
		Succeeded: true,
		Code:      gatewaypb.AckCode_ACK_CODE_OK,
		Payload:   payload,
	}})

	// Act
	_, err = m.GetErrors(context.Background())

	// Assert
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not match result device_id")
}

func TestMiner_GetErrors_RejectsInvalidPayloadData(t *testing.T) {
	validReport := func() *gatewaypb.MinerErrorReport {
		return &gatewaypb.MinerErrorReport{
			DeviceId:      "dev-1",
			MinerError:    errorspb.MinerError_MINER_ERROR_PSU_FAULT_GENERIC,
			Severity:      errorspb.Severity_SEVERITY_CRITICAL,
			ComponentType: errorspb.ComponentType_COMPONENT_TYPE_PSU,
			VendorAttributes: map[string]string{
				"vendor_code": "PSU_001",
			},
		}
	}
	tooManyAttributes := make(map[string]string, 33)
	for i := range 33 {
		tooManyAttributes[fmt.Sprintf("key-%d", i)] = "value"
	}

	cases := []struct {
		name   string
		mutate func(*gatewaypb.MinerErrorReport)
	}{
		{"undefined miner error", func(report *gatewaypb.MinerErrorReport) {
			report.MinerError = errorspb.MinerError(123456)
		}},
		{"undefined severity", func(report *gatewaypb.MinerErrorReport) {
			report.Severity = errorspb.Severity(99)
		}},
		{"undefined component type", func(report *gatewaypb.MinerErrorReport) {
			report.ComponentType = errorspb.ComponentType(99)
		}},
		{"too many vendor attributes", func(report *gatewaypb.MinerErrorReport) {
			report.VendorAttributes = tooManyAttributes
		}},
		{"empty vendor attribute key", func(report *gatewaypb.MinerErrorReport) {
			report.VendorAttributes = map[string]string{"": "value"}
		}},
		{"long vendor attribute key", func(report *gatewaypb.MinerErrorReport) {
			report.VendorAttributes = map[string]string{strings.Repeat("k", 129): "value"}
		}},
		{"long vendor attribute value", func(report *gatewaypb.MinerErrorReport) {
			report.VendorAttributes = map[string]string{"key": strings.Repeat("v", 1025)}
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			report := validReport()
			tc.mutate(report)
			payload, err := proto.Marshal(&gatewaypb.GetErrorsResult{
				DeviceId: "dev-1",
				Errors:   []*gatewaypb.MinerErrorReport{report},
			})
			require.NoError(t, err)
			m := newTestMiner(t, &fakeSender{ack: &gatewaypb.ControlAck{
				Succeeded: true,
				Code:      gatewaypb.AckCode_ACK_CODE_OK,
				Payload:   payload,
			}})

			// Act
			_, err = m.GetErrors(context.Background())

			// Assert
			require.Error(t, err)
			assert.Contains(t, err.Error(), "invalid get errors result")
		})
	}
}

func TestMiner_GetMiningPools_EmptyPayloadReturnsEmptyList(t *testing.T) {
	// Arrange
	m := newTestMiner(t, okSender())

	// Act
	pools, err := m.GetMiningPools(context.Background())

	// Assert
	require.NoError(t, err)
	assert.Empty(t, pools)
}

func TestMiner_GetMiningPools_MalformedPayloadReturnsInternal(t *testing.T) {
	// Arrange
	m := newTestMiner(t, &fakeSender{ack: &gatewaypb.ControlAck{
		Succeeded: true,
		Code:      gatewaypb.AckCode_ACK_CODE_OK,
		Payload:   []byte{0xff},
	}})

	// Act
	pools, err := m.GetMiningPools(context.Background())

	// Assert
	require.Error(t, err)
	assert.Nil(t, pools)
	assert.Contains(t, err.Error(), "unmarshal get mining pools result")
}

func TestMiner_GetMiningPools_RejectsInvalidPayloadData(t *testing.T) {
	validPool := func(priority int32) *gatewaypb.MiningPoolConfig {
		return &gatewaypb.MiningPoolConfig{
			Priority: priority,
			Url:      fmt.Sprintf("stratum+tcp://pool%d.example.com:3333", priority),
			Username: "worker",
		}
	}

	cases := []struct {
		name   string
		result *gatewaypb.GetMiningPoolsResult
	}{
		{"too many pools", &gatewaypb.GetMiningPoolsResult{
			Pools: []*gatewaypb.MiningPoolConfig{
				validPool(0),
				validPool(1),
				validPool(2),
				{Priority: 0, Url: "stratum+tcp://overflow.example.com:3333", Username: "worker"},
			},
		}},
		{"priority beyond slots", &gatewaypb.GetMiningPoolsResult{
			Pools: []*gatewaypb.MiningPoolConfig{
				{Priority: 3, Url: "stratum+tcp://pool3.example.com:3333", Username: "worker"},
			},
		}},
		{"invalid URL shape", &gatewaypb.GetMiningPoolsResult{
			Pools: []*gatewaypb.MiningPoolConfig{
				{Priority: 0, Url: "https://pool.example.com", Username: "worker"},
			},
		}},
		{"invalid SV2 authority key", &gatewaypb.GetMiningPoolsResult{
			Pools: []*gatewaypb.MiningPoolConfig{
				{Priority: 0, Url: "stratum2+tcp://pool.example.com:3333/not_base58", Username: "worker"},
			},
		}},
		{"username too long", &gatewaypb.GetMiningPoolsResult{
			Pools: []*gatewaypb.MiningPoolConfig{
				{Priority: 0, Url: "stratum+tcp://pool.example.com:3333", Username: strings.Repeat("x", 513)},
			},
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			payload, err := proto.Marshal(tc.result)
			require.NoError(t, err)
			m := newTestMiner(t, &fakeSender{ack: &gatewaypb.ControlAck{
				Succeeded: true,
				Code:      gatewaypb.AckCode_ACK_CODE_OK,
				Payload:   payload,
			}})

			// Act
			pools, err := m.GetMiningPools(context.Background())

			// Assert
			require.Error(t, err)
			assert.Nil(t, pools)
			assert.Contains(t, err.Error(), "invalid get mining pools result")
		})
	}
}

func TestMiner_UpdateMiningPools_RejectsPriorityBeyondSDKRange(t *testing.T) {
	// Arrange
	s := okSender()
	m := newTestMiner(t, s)

	// Act
	err := m.UpdateMiningPools(context.Background(), dto.UpdateMiningPoolsPayload{
		DefaultPool: dto.MiningPool{
			Priority: uint32(math.MaxInt32) + 1,
			URL:      "stratum+tcp://pool1.example.com:3333",
			Username: "worker1",
		},
	})

	// Assert
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
	assert.Nil(t, s.cmd, "invalid payload should not be sent to the node")
}

func TestMiner_UpdateMiningPools_RejectsPriorityBeyondWireSlots(t *testing.T) {
	// Arrange
	s := okSender()
	m := newTestMiner(t, s)

	// Act
	err := m.UpdateMiningPools(context.Background(), dto.UpdateMiningPoolsPayload{
		DefaultPool: dto.MiningPool{
			Priority: 3,
			URL:      "stratum+tcp://pool1.example.com:3333",
			Username: "worker1",
		},
	})

	// Assert
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
	assert.Nil(t, s.cmd, "invalid payload should not be sent to the node")
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

func TestMiner_DownloadLogsSendsActionAndMaterializesArtifact(t *testing.T) {
	// Arrange
	s := &fakeSender{
		ack: &gatewaypb.ControlAck{Succeeded: true, Code: gatewaypb.AckCode_ACK_CODE_OK},
		refs: []*gatewaypb.CommandArtifactRef{{
			ArtifactId: "artifact-1",
			Purpose:    gatewaypb.CommandArtifactPurpose_COMMAND_ARTIFACT_PURPOSE_MINER_LOGS,
			Filename:   "logs.csv",
			SizeBytes:  123,
			Sha256:     "3a6eb0790f39ac87c94f3856b2dd2c5d110e6811602261a9a923d3bb23adc8b7",
		}},
	}
	saver := &fakeLogArtifactSaver{}
	m := newTestMinerWithLogSaver(t, s, saver)

	// Act
	err := m.DownloadLogs(context.Background(), "batch-1")

	// Assert
	require.NoError(t, err)
	mc := decodeSent(t, s)
	assert.Equal(t, "batch-1", mc.GetDownloadLogs().GetBatchLogUuid())
	require.Len(t, s.artifacts, 1)
	assert.Equal(t, control.ArtifactDirectionUpload, s.artifacts[0].Direction)
	assert.Equal(t, gatewaypb.CommandArtifactPurpose_COMMAND_ARTIFACT_PURPOSE_MINER_LOGS, s.artifacts[0].Purpose)
	assert.Equal(t, "dev-1", s.artifacts[0].DeviceIdentifier)
	assert.Equal(t, logformat.MaxArtifactBytes, s.artifacts[0].MaxSizeBytes)
	assert.Equal(t, "batch-1", saver.batchLogUUID)
	assert.Equal(t, "AA:BB:CC:DD:EE:FF", saver.macAddress)
	assert.Equal(t, "artifact-1", saver.artifactID)
}

func TestMiner_DownloadLogsRequiresUploadedArtifactRef(t *testing.T) {
	// Arrange
	s := &fakeSender{ack: &gatewaypb.ControlAck{Succeeded: true, Code: gatewaypb.AckCode_ACK_CODE_OK}}
	saver := &fakeLogArtifactSaver{}
	m := newTestMinerWithLogSaver(t, s, saver)

	// Act
	err := m.DownloadLogs(context.Background(), "batch-1")

	// Assert
	require.Error(t, err)
	assert.Contains(t, err.Error(), "without uploaded log artifact")
	assert.Empty(t, saver.artifactID)
}

func TestMiner_DownloadLogsNoActiveStreamIsRetryable(t *testing.T) {
	// Arrange
	m := newTestMinerWithLogSaver(t, &fakeSender{err: control.ErrNoActiveStream}, &fakeLogArtifactSaver{})

	// Act
	err := m.DownloadLogs(context.Background(), "batch-1")

	// Assert
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
	assert.True(t, fleeterror.IsUnimplementedError(m.Unpair(ctx)))
	_, err := m.GetDeviceStatus(ctx)
	assert.True(t, fleeterror.IsUnimplementedError(err))
}
