package main

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	curtailmentpb "github.com/block/proto-fleet/server/generated/grpc/curtailment/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	minercommandpb "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/block/proto-fleet/server/sdk/v1/mocks"
)

type captureAcker struct {
	mu   sync.Mutex
	acks []*pb.ControlAck
}

func (c *captureAcker) Send(req *pb.ControlStreamRequest) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if a := req.GetAck(); a != nil {
		c.acks = append(c.acks, a)
	}
	return nil
}

func (c *captureAcker) only(t *testing.T) *pb.ControlAck {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	require.Len(t, c.acks, 1, "expected exactly one ack")
	return c.acks[0]
}

type fakeDriverGetter struct {
	d   sdk.Driver
	err error
}

func (f fakeDriverGetter) GetDriverByDriverName(string) (sdk.Driver, error) { return f.d, f.err }

// withTarget stamps a standard descriptor onto a command built with just an action.
func withTarget(mc *pb.MinerCommand) *pb.MinerCommand {
	mc.Target = &pb.MinerConnectionDescriptor{
		DeviceIdentifier: "dev-1", DriverName: "virtual",
		IpAddress: "10.0.0.5", Port: "4028", UrlScheme: "http",
	}
	return mc
}

func TestHandleMinerCommand_ExecutesAndAcksOK(t *testing.T) {
	// Arrange
	ctrl := gomock.NewController(t)
	dev := mocks.NewMockDevice(ctrl)
	dev.EXPECT().Reboot(gomock.Any()).Return(nil)
	dev.EXPECT().Close(gomock.Any()).Return(nil)
	drv := mocks.NewMockDriver(ctrl)
	drv.EXPECT().NewDevice(gomock.Any(), "dev-1", gomock.Any(), gomock.Any()).Return(sdk.NewDeviceResult{Device: dev}, nil)
	r := &RunCmd{driverGetter: fakeDriverGetter{d: drv}, minerSecrets: nodeSecretProvider{}}
	ack := &captureAcker{}

	// Act
	r.handleMinerCommand(context.Background(), ack, "cmd-1",
		withTarget(&pb.MinerCommand{Action: &pb.MinerCommand_Reboot{Reboot: &pb.RebootAction{}}}), discardLogger(t))

	// Assert
	got := ack.only(t)
	assert.Equal(t, "cmd-1", got.GetCommandId())
	assert.Equal(t, pb.AckCode_ACK_CODE_OK, got.GetCode())
	assert.True(t, got.GetSucceeded())
}

func TestHandleMinerCommand_DecryptsTargetCredential(t *testing.T) {
	// Arrange
	ctrl := gomock.NewController(t)
	dev := mocks.NewMockDevice(ctrl)
	dev.EXPECT().Reboot(gomock.Any()).Return(nil)
	dev.EXPECT().Close(gomock.Any()).Return(nil)
	codec := &credentialCodec{key: bytes.Repeat([]byte{2}, credentialKeySize)}
	encrypted, err := codec.Seal(sdk.SecretBundle{
		Version: "v1",
		Kind:    sdk.UsernamePassword{Username: "root", Password: "hunter2"},
	})
	require.NoError(t, err)
	drv := mocks.NewMockDriver(ctrl)
	drv.EXPECT().NewDevice(gomock.Any(), "dev-1", gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, _ sdk.DeviceInfo, secret sdk.SecretBundle) (sdk.NewDeviceResult, error) {
			assert.Equal(t, sdk.UsernamePassword{Username: "root", Password: "hunter2"}, secret.Kind)
			return sdk.NewDeviceResult{Device: dev}, nil
		})
	r := &RunCmd{driverGetter: fakeDriverGetter{d: drv}, minerSecrets: codec}
	ack := &captureAcker{}
	mc := withTarget(&pb.MinerCommand{Action: &pb.MinerCommand_Reboot{Reboot: &pb.RebootAction{}}})
	mc.Target.CredentialUsername = encrypted.GetUsername()
	mc.Target.CredentialPassword = encrypted.GetPassword()

	// Act
	r.handleMinerCommand(context.Background(), ack, "cmd-1", mc, discardLogger(t))

	// Assert
	assert.Equal(t, pb.AckCode_ACK_CODE_OK, ack.only(t).GetCode())
}

func TestHandleMinerCommand_InvalidTargetCredentialAcksUnauthenticated(t *testing.T) {
	// Arrange: the credential bytes cannot be decrypted with this node's key.
	ctrl := gomock.NewController(t)
	r := &RunCmd{
		driverGetter: fakeDriverGetter{d: mocks.NewMockDriver(ctrl)},
		minerSecrets: &credentialCodec{key: bytes.Repeat([]byte{3}, credentialKeySize)},
	}
	ack := &captureAcker{}
	mc := withTarget(&pb.MinerCommand{Action: &pb.MinerCommand_Reboot{Reboot: &pb.RebootAction{}}})
	mc.Target.CredentialUsername = []byte("not-a-valid-credential")
	mc.Target.CredentialPassword = []byte("not-a-valid-credential")

	// Act
	r.handleMinerCommand(context.Background(), ack, "cmd-1", mc, discardLogger(t))

	// Assert: rejected before the driver is dialed.
	assert.Equal(t, pb.AckCode_ACK_CODE_UNAUTHENTICATED, ack.only(t).GetCode())
}

func TestHandleMinerCommand_WrongKeyTargetCredentialAcksUnauthenticated(t *testing.T) {
	// Arrange: the credential bytes are well-formed, but sealed by another node key.
	ctrl := gomock.NewController(t)
	sealingCodec := &credentialCodec{key: bytes.Repeat([]byte{4}, credentialKeySize)}
	encrypted, err := sealingCodec.Seal(sdk.SecretBundle{
		Version: "v1",
		Kind:    sdk.UsernamePassword{Username: "root", Password: "hunter2"},
	})
	require.NoError(t, err)
	r := &RunCmd{
		driverGetter: fakeDriverGetter{d: mocks.NewMockDriver(ctrl)},
		minerSecrets: &credentialCodec{key: bytes.Repeat([]byte{5}, credentialKeySize)},
	}
	ack := &captureAcker{}
	mc := withTarget(&pb.MinerCommand{Action: &pb.MinerCommand_Reboot{Reboot: &pb.RebootAction{}}})
	mc.Target.CredentialUsername = encrypted.GetUsername()
	mc.Target.CredentialPassword = encrypted.GetPassword()

	// Act
	r.handleMinerCommand(context.Background(), ack, "cmd-1", mc, discardLogger(t))

	// Assert: rejected before the driver is dialed.
	assert.Equal(t, pb.AckCode_ACK_CODE_UNAUTHENTICATED, ack.only(t).GetCode())
}

func TestHandleMinerCommand_ConvertsCoolingMode(t *testing.T) {
	// Arrange: the proto cooling enum must map to the matching SDK value.
	ctrl := gomock.NewController(t)
	dev := mocks.NewMockDevice(ctrl)
	dev.EXPECT().SetCoolingMode(gomock.Any(), sdk.CoolingModeImmersionCooled).Return(nil)
	dev.EXPECT().Close(gomock.Any()).Return(nil)
	drv := mocks.NewMockDriver(ctrl)
	drv.EXPECT().NewDevice(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(sdk.NewDeviceResult{Device: dev}, nil)
	r := &RunCmd{driverGetter: fakeDriverGetter{d: drv}, minerSecrets: nodeSecretProvider{}}
	ack := &captureAcker{}

	// Act
	r.handleMinerCommand(context.Background(), ack, "cmd-1",
		withTarget(&pb.MinerCommand{Action: &pb.MinerCommand_SetCoolingMode{SetCoolingMode: &pb.SetCoolingModeAction{Mode: commonpb.CoolingMode_COOLING_MODE_IMMERSION_COOLED}}}), discardLogger(t))

	// Assert
	assert.Equal(t, pb.AckCode_ACK_CODE_OK, ack.only(t).GetCode())
}

func TestHandleMinerCommand_DeviceErrorClassifiesToUnimplemented(t *testing.T) {
	// Arrange: the device reports an unsupported capability.
	ctrl := gomock.NewController(t)
	dev := mocks.NewMockDevice(ctrl)
	dev.EXPECT().Reboot(gomock.Any()).Return(sdk.NewErrUnsupportedCapability("reboot"))
	dev.EXPECT().Close(gomock.Any()).Return(nil)
	drv := mocks.NewMockDriver(ctrl)
	drv.EXPECT().NewDevice(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(sdk.NewDeviceResult{Device: dev}, nil)
	r := &RunCmd{driverGetter: fakeDriverGetter{d: drv}, minerSecrets: nodeSecretProvider{}}
	ack := &captureAcker{}

	// Act
	r.handleMinerCommand(context.Background(), ack, "cmd-1",
		withTarget(&pb.MinerCommand{Action: &pb.MinerCommand_Reboot{Reboot: &pb.RebootAction{}}}), discardLogger(t))

	// Assert
	got := ack.only(t)
	assert.Equal(t, pb.AckCode_ACK_CODE_UNIMPLEMENTED, got.GetCode())
	assert.False(t, got.GetSucceeded())
}

func TestValidateDialTarget(t *testing.T) {
	cases := []struct {
		name    string
		desc    *pb.MinerConnectionDescriptor
		wantErr bool
	}{
		{"private http", &pb.MinerConnectionDescriptor{IpAddress: "10.0.0.5", UrlScheme: "http"}, false},
		{"loopback virtual (dev path)", &pb.MinerConnectionDescriptor{IpAddress: "127.0.0.1", UrlScheme: "virtual"}, false},
		{"private tcp", &pb.MinerConnectionDescriptor{IpAddress: "192.168.1.9", UrlScheme: "tcp"}, false},
		{"public ip", &pb.MinerConnectionDescriptor{IpAddress: "8.8.8.8", UrlScheme: "http"}, true},
		{"not an ip", &pb.MinerConnectionDescriptor{IpAddress: "miner.local", UrlScheme: "http"}, true},
		{"empty scheme", &pb.MinerConnectionDescriptor{IpAddress: "10.0.0.5", UrlScheme: ""}, true},
		{"unknown scheme", &pb.MinerConnectionDescriptor{IpAddress: "10.0.0.5", UrlScheme: "file"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Act
			err := validateDialTarget(tc.desc)

			// Assert
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestHandleMinerCommand_RejectsUndiallableTarget(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*pb.MinerConnectionDescriptor)
	}{
		{"public ip", func(d *pb.MinerConnectionDescriptor) { d.IpAddress = "8.8.8.8" }},
		{"not an ip", func(d *pb.MinerConnectionDescriptor) { d.IpAddress = "miner.example.com" }},
		{"non-web scheme", func(d *pb.MinerConnectionDescriptor) { d.UrlScheme = "file" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange: a driver mock with no expectations, so any dial attempt fails the test.
			r := &RunCmd{driverGetter: fakeDriverGetter{d: mocks.NewMockDriver(gomock.NewController(t))}, minerSecrets: nodeSecretProvider{}}
			mc := withTarget(&pb.MinerCommand{Action: &pb.MinerCommand_Reboot{Reboot: &pb.RebootAction{}}})
			tc.mutate(mc.Target)
			ack := &captureAcker{}

			// Act
			r.handleMinerCommand(context.Background(), ack, "cmd-1", mc, discardLogger(t))

			// Assert: rejected at the control boundary; the driver is never dialed.
			assert.Equal(t, pb.AckCode_ACK_CODE_BAD_REQUEST, ack.only(t).GetCode())
		})
	}
}

func TestHandleMinerCommand_UnknownDriverAcksAgentIncapable(t *testing.T) {
	// Arrange: no plugin loaded for the target's driver.
	r := &RunCmd{driverGetter: fakeDriverGetter{err: errors.New("no plugin")}, minerSecrets: nodeSecretProvider{}}
	ack := &captureAcker{}

	// Act
	r.handleMinerCommand(context.Background(), ack, "cmd-1",
		withTarget(&pb.MinerCommand{Action: &pb.MinerCommand_Reboot{Reboot: &pb.RebootAction{}}}), discardLogger(t))

	// Assert
	assert.Equal(t, pb.AckCode_ACK_CODE_AGENT_INCAPABLE, ack.only(t).GetCode())
}

func TestClassifyMinerCommandError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want pb.AckCode
	}{
		{"auth", sdk.SDKError{Code: sdk.ErrCodeAuthenticationFailed, Message: "bad creds"}, pb.AckCode_ACK_CODE_UNAUTHENTICATED},
		{"unsupported", sdk.NewErrUnsupportedCapability("x"), pb.AckCode_ACK_CODE_UNIMPLEMENTED},
		{"command error carries its code", cmdErr(pb.AckCode_ACK_CODE_BAD_REQUEST, "bad enum"), pb.AckCode_ACK_CODE_BAD_REQUEST},
		{"other", errors.New("boom"), pb.AckCode_ACK_CODE_INTERNAL},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Act
			code, _ := classifyMinerCommandError("execute", tc.err)

			// Assert
			assert.Equal(t, tc.want, code)
		})
	}
}

func TestRunMinerActionEnumConverters(t *testing.T) {
	t.Run("defined values map to the matching SDK value", func(t *testing.T) {
		// Act
		cool, coolErr := toSDKCoolingMode(commonpb.CoolingMode_COOLING_MODE_IMMERSION_COOLED)
		perf, perfErr := toSDKPerformanceMode(minercommandpb.PerformanceMode_PERFORMANCE_MODE_EFFICIENCY)
		lvl, lvlErr := toSDKCurtailLevel(curtailmentpb.CurtailmentLevel_CURTAILMENT_LEVEL_FULL)

		// Assert
		require.NoError(t, coolErr)
		require.NoError(t, perfErr)
		require.NoError(t, lvlErr)
		assert.Equal(t, sdk.CoolingModeImmersionCooled, cool)
		assert.Equal(t, sdk.PerformanceModeEfficiency, perf)
		assert.Equal(t, sdk.CurtailLevelFull, lvl)
	})

	t.Run("undefined values are rejected as BAD_REQUEST", func(t *testing.T) {
		// Act
		_, coolErr := toSDKCoolingMode(commonpb.CoolingMode(99))
		_, perfErr := toSDKPerformanceMode(minercommandpb.PerformanceMode(99))
		_, lvlErr := toSDKCurtailLevel(curtailmentpb.CurtailmentLevel(99))

		// Assert: each maps to a BAD_REQUEST ack instead of casting through to a plugin.
		for _, err := range []error{coolErr, perfErr, lvlErr} {
			require.Error(t, err)
			code, _ := classifyMinerCommandError("execute command", err)
			assert.Equal(t, pb.AckCode_ACK_CODE_BAD_REQUEST, code)
		}
	})
}
