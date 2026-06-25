package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"buf.build/go/protovalidate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/protobuf/proto"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	curtailmentpb "github.com/block/proto-fleet/server/generated/grpc/curtailment/v1"
	errorspb "github.com/block/proto-fleet/server/generated/grpc/errors/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	minercommandpb "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	sdkerrors "github.com/block/proto-fleet/server/sdk/v1/errors"
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

func TestHandleMinerCommand_UpdatesMiningPools(t *testing.T) {
	// Arrange
	ctrl := gomock.NewController(t)
	dev := mocks.NewMockDevice(ctrl)
	dev.EXPECT().
		UpdateMiningPools(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, pools []sdk.MiningPoolConfig) error {
			require.Len(t, pools, 2)
			assert.Equal(t, int32(0), pools[0].Priority)
			assert.Equal(t, "stratum+tcp://pool1.example.com:3333", pools[0].URL)
			assert.Equal(t, "worker1", pools[0].WorkerName)
			assert.Equal(t, int32(1), pools[1].Priority)
			assert.Equal(t, "stratum+tcp://pool2.example.com:3333", pools[1].URL)
			assert.Equal(t, "worker2", pools[1].WorkerName)
			return nil
		})
	dev.EXPECT().Close(gomock.Any()).Return(nil)
	drv := mocks.NewMockDriver(ctrl)
	drv.EXPECT().NewDevice(gomock.Any(), "dev-1", gomock.Any(), gomock.Any()).Return(sdk.NewDeviceResult{Device: dev}, nil)
	r := &RunCmd{driverGetter: fakeDriverGetter{d: drv}, minerSecrets: nodeSecretProvider{}}
	ack := &captureAcker{}

	// Act
	r.handleMinerCommand(context.Background(), ack, "cmd-1",
		withTarget(&pb.MinerCommand{Action: &pb.MinerCommand_UpdateMiningPools{UpdateMiningPools: &pb.UpdateMiningPoolsAction{
			Pools: []*pb.MiningPoolConfig{
				{Priority: 0, Url: "stratum+tcp://pool1.example.com:3333", Username: "worker1"},
				{Priority: 1, Url: "stratum+tcp://pool2.example.com:3333", Username: "worker2"},
			},
		}}}), discardLogger(t))

	// Assert
	got := ack.only(t)
	assert.Equal(t, pb.AckCode_ACK_CODE_OK, got.GetCode())
	assert.True(t, got.GetSucceeded())
	assert.Empty(t, got.GetPayload())
}

func TestHandleMinerCommand_GetMiningPoolsReturnsPayload(t *testing.T) {
	// Arrange
	ctrl := gomock.NewController(t)
	dev := mocks.NewMockDevice(ctrl)
	dev.EXPECT().GetMiningPools(gomock.Any()).Return([]sdk.ConfiguredPool{
		{Priority: 0, URL: "stratum+tcp://pool1.example.com:3333", Username: "worker1"},
		{Priority: 2, URL: "stratum+tcp://pool4.example.com:3333", Username: "worker4"},
	}, nil)
	dev.EXPECT().Close(gomock.Any()).Return(nil)
	drv := mocks.NewMockDriver(ctrl)
	drv.EXPECT().NewDevice(gomock.Any(), "dev-1", gomock.Any(), gomock.Any()).Return(sdk.NewDeviceResult{Device: dev}, nil)
	r := &RunCmd{driverGetter: fakeDriverGetter{d: drv}, minerSecrets: nodeSecretProvider{}}
	ack := &captureAcker{}

	// Act
	r.handleMinerCommand(context.Background(), ack, "cmd-1",
		withTarget(&pb.MinerCommand{Action: &pb.MinerCommand_GetMiningPools{GetMiningPools: &pb.GetMiningPoolsAction{}}}), discardLogger(t))

	// Assert
	got := ack.only(t)
	assert.Equal(t, pb.AckCode_ACK_CODE_OK, got.GetCode())
	assert.True(t, got.GetSucceeded())
	require.NotEmpty(t, got.GetPayload())
	var result pb.GetMiningPoolsResult
	require.NoError(t, proto.Unmarshal(got.GetPayload(), &result))
	require.Len(t, result.GetPools(), 2)
	assert.Equal(t, int32(0), result.GetPools()[0].GetPriority())
	assert.Equal(t, "stratum+tcp://pool1.example.com:3333", result.GetPools()[0].GetUrl())
	assert.Equal(t, "worker1", result.GetPools()[0].GetUsername())
	assert.Equal(t, int32(2), result.GetPools()[1].GetPriority())
	assert.Equal(t, "stratum+tcp://pool4.example.com:3333", result.GetPools()[1].GetUrl())
	assert.Equal(t, "worker4", result.GetPools()[1].GetUsername())
}

func TestHandleMinerCommand_GetErrorsReturnsPayload(t *testing.T) {
	// Arrange
	ctrl := gomock.NewController(t)
	now := time.Now().UTC().Truncate(time.Millisecond)
	componentID := "psu-0"
	dev := mocks.NewMockDevice(ctrl)
	dev.EXPECT().GetErrors(gomock.Any()).Return(sdk.DeviceErrors{
		DeviceID: "plugin-local-id",
		Errors: []sdk.DeviceError{{
			MinerError:        1003,
			CauseSummary:      "PSU fault",
			RecommendedAction: "Replace PSU",
			Severity:          1,
			FirstSeenAt:       now,
			LastSeenAt:        now.Add(time.Minute),
			VendorAttributes: map[string]string{
				"vendor_code": "PSU_001",
			},
			DeviceID:      "plugin-report-id",
			ComponentID:   &componentID,
			Impact:        "Stops mining",
			Summary:       "Power supply fault detected",
			ComponentType: 1,
		}},
	}, nil)
	dev.EXPECT().Close(gomock.Any()).Return(nil)
	drv := mocks.NewMockDriver(ctrl)
	drv.EXPECT().NewDevice(gomock.Any(), "dev-1", gomock.Any(), gomock.Any()).Return(sdk.NewDeviceResult{Device: dev}, nil)
	r := &RunCmd{driverGetter: fakeDriverGetter{d: drv}, minerSecrets: nodeSecretProvider{}}
	ack := &captureAcker{}

	// Act
	r.handleMinerCommand(context.Background(), ack, "cmd-1",
		withTarget(&pb.MinerCommand{Action: &pb.MinerCommand_GetErrors{GetErrors: &pb.GetErrorsAction{}}}), discardLogger(t))

	// Assert
	got := ack.only(t)
	assert.Equal(t, pb.AckCode_ACK_CODE_OK, got.GetCode())
	assert.True(t, got.GetSucceeded())

	var result pb.GetErrorsResult
	require.NoError(t, proto.Unmarshal(got.GetPayload(), &result))
	assert.Equal(t, "dev-1", result.GetDeviceId())
	require.Len(t, result.GetErrors(), 1)
	errReport := result.GetErrors()[0]
	assert.Equal(t, errorspb.MinerError_MINER_ERROR_PSU_FAULT_GENERIC, errReport.GetMinerError())
	assert.Equal(t, "PSU fault", errReport.GetCauseSummary())
	assert.Equal(t, "Replace PSU", errReport.GetRecommendedAction())
	assert.Equal(t, errorspb.Severity_SEVERITY_CRITICAL, errReport.GetSeverity())
	assert.Equal(t, now, errReport.GetFirstSeenAt().AsTime())
	assert.Equal(t, now.Add(time.Minute), errReport.GetLastSeenAt().AsTime())
	assert.Equal(t, "PSU_001", errReport.GetVendorAttributes()["vendor_code"])
	assert.Equal(t, "dev-1", errReport.GetDeviceId())
	assert.Equal(t, &componentID, errReport.ComponentId)
	assert.Equal(t, "Stops mining", errReport.GetImpact())
	assert.Equal(t, "Power supply fault detected", errReport.GetSummary())
	assert.Equal(t, errorspb.ComponentType_COMPONENT_TYPE_PSU, errReport.GetComponentType())
}

func TestGetErrorsResultFromSDKRejectsInvalidPluginErrorData(t *testing.T) {
	validError := func() sdk.DeviceError {
		return sdk.DeviceError{
			MinerError:       sdkerrors.PSUFaultGeneric,
			Severity:         sdkerrors.SeverityCritical,
			VendorAttributes: map[string]string{"vendor_code": "PSU_001"},
			DeviceID:         "dev-1",
			ComponentType:    sdkerrors.ComponentTypePSU,
		}
	}
	tooManyAttributes := make(map[string]string, 33)
	for i := range 33 {
		tooManyAttributes[fmt.Sprintf("key-%d", i)] = "value"
	}

	cases := []struct {
		name    string
		mutate  func(*sdk.DeviceError)
		wantErr string
	}{
		{"undefined miner error", func(err *sdk.DeviceError) {
			err.MinerError = sdk.MinerError(123456)
		}, "miner_error: value must be one of the defined enum values"},
		{"undefined severity", func(err *sdk.DeviceError) {
			err.Severity = sdk.Severity(99)
		}, "severity: value must be one of the defined enum values"},
		{"undefined component type", func(err *sdk.DeviceError) {
			err.ComponentType = sdk.ComponentType(99)
		}, "component_type: value must be one of the defined enum values"},
		{"too many vendor attributes", func(err *sdk.DeviceError) {
			err.VendorAttributes = tooManyAttributes
		}, "map must be at most 32 entries"},
		{"empty vendor attribute key", func(err *sdk.DeviceError) {
			err.VendorAttributes = map[string]string{"": "value"}
		}, "must be at least 1 characters"},
		{"long vendor attribute key", func(err *sdk.DeviceError) {
			err.VendorAttributes = map[string]string{strings.Repeat("k", 129): "value"}
		}, "must be at most 128 characters"},
		{"long vendor attribute value", func(err *sdk.DeviceError) {
			err.VendorAttributes = map[string]string{"key": strings.Repeat("v", 1025)}
		}, "must be at most 1024 characters"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			sdkErr := validError()
			tc.mutate(&sdkErr)

			// Act
			_, err := getErrorsResultPayload("dev-1", sdk.DeviceErrors{
				DeviceID: "dev-1",
				Errors:   []sdk.DeviceError{sdkErr},
			})

			// Assert
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestGetErrorsResultFromSDKCapsPluginErrorCountBeforeConversion(t *testing.T) {
	// Arrange
	pluginErrors := make([]sdk.DeviceError, maxGetErrorsReports+88)
	for i := range pluginErrors {
		pluginErrors[i] = sdk.DeviceError{
			MinerError:    sdkerrors.PSUFaultGeneric,
			Severity:      sdkerrors.SeverityCritical,
			DeviceID:      "dev-1",
			ComponentType: sdkerrors.ComponentTypePSU,
		}
	}

	// Act
	result, err := getErrorsResultFromSDK("dev-1", sdk.DeviceErrors{
		DeviceID: "dev-1",
		Errors:   pluginErrors,
	})

	// Assert
	require.NoError(t, err)
	assert.True(t, result.GetTruncated())
	assert.Equal(t, uint32(88), result.GetOmittedReportCount())
	require.Len(t, result.GetErrors(), maxGetErrorsReports)
	require.NoError(t, protovalidate.Validate(result))

	payload, err := getErrorsResultPayload("dev-1", sdk.DeviceErrors{
		DeviceID: "dev-1",
		Errors:   pluginErrors,
	})
	require.NoError(t, err)
	var payloadResult pb.GetErrorsResult
	require.NoError(t, proto.Unmarshal(payload, &payloadResult))
	assert.True(t, payloadResult.GetTruncated())
	assert.Equal(t, uint32(88), payloadResult.GetOmittedReportCount())
	require.Len(t, payloadResult.GetErrors(), maxGetErrorsReports)
}

func TestHandleMinerCommand_GetErrorsReturnsTruncatedPayloadForOversizedPayload(t *testing.T) {
	// Arrange
	ctrl := gomock.NewController(t)
	pluginErrors := make([]sdk.DeviceError, 512)
	verboseSummary := strings.Repeat("s", 4096)
	for i := range pluginErrors {
		pluginErrors[i] = sdk.DeviceError{
			MinerError:    sdkerrors.PSUFaultGeneric,
			Severity:      sdkerrors.SeverityCritical,
			DeviceID:      "dev-1",
			Summary:       verboseSummary,
			ComponentType: sdkerrors.ComponentTypePSU,
		}
	}
	dev := mocks.NewMockDevice(ctrl)
	dev.EXPECT().GetErrors(gomock.Any()).Return(sdk.DeviceErrors{
		DeviceID: "dev-1",
		Errors:   pluginErrors,
	}, nil)
	dev.EXPECT().Close(gomock.Any()).Return(nil)
	drv := mocks.NewMockDriver(ctrl)
	drv.EXPECT().NewDevice(gomock.Any(), "dev-1", gomock.Any(), gomock.Any()).Return(sdk.NewDeviceResult{Device: dev}, nil)
	r := &RunCmd{driverGetter: fakeDriverGetter{d: drv}, minerSecrets: nodeSecretProvider{}}
	ack := &captureAcker{}

	// Act
	r.handleMinerCommand(context.Background(), ack, "cmd-1",
		withTarget(&pb.MinerCommand{Action: &pb.MinerCommand_GetErrors{GetErrors: &pb.GetErrorsAction{}}}), discardLogger(t))

	// Assert
	got := ack.only(t)
	assert.Equal(t, pb.AckCode_ACK_CODE_OK, got.GetCode())
	assert.True(t, got.GetSucceeded())
	require.NotEmpty(t, got.GetPayload())
	require.LessOrEqual(t, len(got.GetPayload()), maxAckPayloadBytes)

	var result pb.GetErrorsResult
	require.NoError(t, proto.Unmarshal(got.GetPayload(), &result))
	assert.Equal(t, "dev-1", result.GetDeviceId())
	assert.True(t, result.GetTruncated())
	assert.Greater(t, result.GetOmittedReportCount(), uint32(0))
	assert.Less(t, len(result.GetErrors()), len(pluginErrors))
}

func TestHandleMinerCommand_GetMiningPoolsTrimsUnsupportedPoolSlots(t *testing.T) {
	// Arrange
	ctrl := gomock.NewController(t)
	dev := mocks.NewMockDevice(ctrl)
	dev.EXPECT().GetMiningPools(gomock.Any()).Return([]sdk.ConfiguredPool{
		{Priority: 3, URL: "stratum+tcp://pool3.example.com:3333", Username: "worker3"},
		{Priority: 0, URL: "stratum+tcp://pool0.example.com:3333", Username: "worker0"},
		{Priority: 1, URL: "stratum+tcp://pool1.example.com:3333", Username: "worker1"},
		{Priority: 2, URL: "stratum+tcp://pool2.example.com:3333", Username: "worker2"},
	}, nil)
	dev.EXPECT().Close(gomock.Any()).Return(nil)
	drv := mocks.NewMockDriver(ctrl)
	drv.EXPECT().NewDevice(gomock.Any(), "dev-1", gomock.Any(), gomock.Any()).Return(sdk.NewDeviceResult{Device: dev}, nil)
	r := &RunCmd{driverGetter: fakeDriverGetter{d: drv}, minerSecrets: nodeSecretProvider{}}
	ack := &captureAcker{}

	// Act
	r.handleMinerCommand(context.Background(), ack, "cmd-1",
		withTarget(&pb.MinerCommand{Action: &pb.MinerCommand_GetMiningPools{GetMiningPools: &pb.GetMiningPoolsAction{}}}), discardLogger(t))

	// Assert
	got := ack.only(t)
	assert.Equal(t, pb.AckCode_ACK_CODE_OK, got.GetCode())
	assert.True(t, got.GetSucceeded())
	var result pb.GetMiningPoolsResult
	require.NoError(t, proto.Unmarshal(got.GetPayload(), &result))
	require.Len(t, result.GetPools(), 3)
	assert.Equal(t, int32(0), result.GetPools()[0].GetPriority())
	assert.Equal(t, "stratum+tcp://pool0.example.com:3333", result.GetPools()[0].GetUrl())
	assert.Equal(t, "worker0", result.GetPools()[0].GetUsername())
	assert.Equal(t, int32(1), result.GetPools()[1].GetPriority())
	assert.Equal(t, "stratum+tcp://pool1.example.com:3333", result.GetPools()[1].GetUrl())
	assert.Equal(t, "worker1", result.GetPools()[1].GetUsername())
	assert.Equal(t, int32(2), result.GetPools()[2].GetPriority())
	assert.Equal(t, "stratum+tcp://pool2.example.com:3333", result.GetPools()[2].GetUrl())
	assert.Equal(t, "worker2", result.GetPools()[2].GetUsername())
}

func TestHandleMinerCommand_GetMiningPoolsRejectsInvalidPluginResult(t *testing.T) {
	validPoolURL := "stratum+tcp://pool.example.com:3333"
	cases := []struct {
		name  string
		pools []sdk.ConfiguredPool
	}{
		{"negative priority", []sdk.ConfiguredPool{
			{Priority: -1, URL: validPoolURL, Username: "worker"},
		}},
		{"invalid URL shape", []sdk.ConfiguredPool{
			{Priority: 0, URL: "https://pool.example.com", Username: "worker"},
		}},
		{"invalid SV2 authority key", []sdk.ConfiguredPool{
			{Priority: 0, URL: "stratum2+tcp://pool.example.com:3333/not_base58", Username: "worker"},
		}},
		{"username too long", []sdk.ConfiguredPool{{
			Priority: 0,
			URL:      validPoolURL,
			Username: strings.Repeat("x", 513),
		}}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			ctrl := gomock.NewController(t)
			dev := mocks.NewMockDevice(ctrl)
			dev.EXPECT().GetMiningPools(gomock.Any()).Return(tc.pools, nil)
			dev.EXPECT().Close(gomock.Any()).Return(nil)
			drv := mocks.NewMockDriver(ctrl)
			drv.EXPECT().NewDevice(gomock.Any(), "dev-1", gomock.Any(), gomock.Any()).Return(sdk.NewDeviceResult{Device: dev}, nil)
			r := &RunCmd{driverGetter: fakeDriverGetter{d: drv}, minerSecrets: nodeSecretProvider{}}
			ack := &captureAcker{}

			// Act
			r.handleMinerCommand(context.Background(), ack, "cmd-1",
				withTarget(&pb.MinerCommand{Action: &pb.MinerCommand_GetMiningPools{GetMiningPools: &pb.GetMiningPoolsAction{}}}), discardLogger(t))

			// Assert
			got := ack.only(t)
			assert.Equal(t, pb.AckCode_ACK_CODE_INTERNAL, got.GetCode())
			assert.False(t, got.GetSucceeded())
			assert.Empty(t, got.GetPayload())
			assert.Contains(t, got.GetErrorMessage(), "invalid get mining pools result")
		})
	}
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
