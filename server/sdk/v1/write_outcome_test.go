package sdk

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync/atomic"
	"testing"
	"time"

	pb "github.com/block/proto-fleet/server/sdk/v1/pb/generated"
	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"
)

func outcomeRPCDevice(t *testing.T, device Device) *DeviceGRPCClient {
	t.Helper()
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	pb.RegisterDriverServer(server, &DriverGRPCServer{devices: map[string]Device{"device": device}})
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	conn, err := grpc.NewClient("passthrough:///write-outcome", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })
	return &DeviceGRPCClient{client: pb.NewDriverClient(conn), deviceID: "device"}
}

func TestWriteRefusalHelpersPreserveCause(t *testing.T) {
	for _, tc := range []struct {
		name string
		wrap func(error) error
	}{
		{name: "write refused", wrap: func(err error) error { return WriteRefused(err, WriteStageValidation, WriteReasonInvalidRequest) }},
		{name: "invalid request", wrap: InvalidWriteRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cause := errors.New("configuration rejected")
			err := tc.wrap(NewErrorInvalidConfig("device", cause))
			require.ErrorIs(t, err, cause)
			require.EqualError(t, err, "invalid device configuration: device")
			var sdkErr SDKError
			require.ErrorAs(t, err, &sdkErr)
			require.Equal(t, ErrCodeInvalidConfig, sdkErr.Code)
			require.Equal(t, codes.InvalidArgument, status.Code(err))
			evidence := WriteEvidence(err)
			require.Equal(t, WriteNotDispatched, evidence.GetReason())
			require.Equal(t, string(WriteStageValidation), evidence.Metadata["stage"])
			require.Equal(t, string(WriteReasonInvalidRequest), evidence.Metadata["reason"])
			require.NoError(t, tc.wrap(nil))
		})
	}
}

func TestUnsupportedWritePreservesCapabilityStatusAcrossRPC(t *testing.T) {
	err := UnsupportedWrite("reboot")
	require.EqualError(t, err, "unsupported capability: reboot")
	var sdkErr SDKError
	require.ErrorAs(t, err, &sdkErr)
	require.Equal(t, ErrCodeUnsupportedCapability, sdkErr.Code)
	device := outcomeRPCDevice(t, fakeDevice{startMiningFunc: func(context.Context) error { return err }})
	err = device.StartMining(t.Context())
	require.Equal(t, codes.Unimplemented, status.Code(err))
	require.Contains(t, status.Convert(err).Message(), "unsupported capability: reboot")
	evidence := WriteEvidence(err)
	require.Equal(t, WriteNotDispatched, evidence.GetReason())
	require.Equal(t, string(WriteStageCapability), evidence.Metadata["stage"])
	require.Equal(t, string(WriteReasonUnsupported), evidence.Metadata["reason"])
}

func TestDeviceProbeEvidenceRequiresPluginOrigin(t *testing.T) {
	downstream, err := status.New(codes.Unavailable, "controller rejected request").WithDetails(
		&errdetails.ErrorInfo{Domain: deviceProbeDomain, Reason: deviceProbeReason},
	)
	require.NoError(t, err)
	for _, test := range []struct {
		name      string
		failure   error
		confirmed bool
	}{
		{"unmarked network error", &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}, false},
		{"locally marked controller error", WithDeviceProbeFailure(status.Error(codes.Unavailable, "connection refused")), true},
		{"downstream detail without plugin evidence", downstream.Err(), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			device := outcomeRPCDevice(t, fakeDevice{statusFunc: func(context.Context) (DeviceMetrics, error) {
				return DeviceMetrics{}, test.failure
			}})
			_, err := device.Status(t.Context())
			require.Error(t, err)
			require.Equal(t, test.confirmed, DeviceProbeFailed(err))
		})
	}
}

func TestWriteEvidenceSurvivesActualRPCAndErrorMapping(t *testing.T) {
	for _, outcome := range []string{WriteNotDispatched, WriteCompleted, WriteUnknown} {
		t.Run(outcome, func(t *testing.T) {
			device := outcomeRPCDevice(t, fakeDevice{startMiningFunc: func(context.Context) error {
				return fmt.Errorf("driver context: %w", WithWriteOutcome(NewErrorDeviceUnavailable("device"), outcome, WriteStageDispatch, WriteReasonRejected))
			}})
			err := device.StartMining(t.Context())
			require.Error(t, err)
			require.Equal(t, codes.Unavailable, status.Code(err))
			evidence := WriteEvidence(err)
			require.NotNil(t, evidence)
			require.Equal(t, outcome, evidence.Reason)
			require.Equal(t, "dispatch", evidence.Metadata["stage"])
		})
	}
}

func TestDriverRPCDoesNotTrustDownstreamWriteEvidence(t *testing.T) {
	for _, tc := range []struct {
		name   string
		code   codes.Code
		detail *errdetails.ErrorInfo
	}{
		{name: WriteNotDispatched, code: codes.Unavailable, detail: NewWriteEvidence(WriteNotDispatched, WriteStageDispatch, WriteReasonFinished)},
		{name: WriteCompleted, code: codes.Unavailable, detail: NewWriteEvidence(WriteCompleted, WriteStageDispatch, WriteReasonFinished)},
		{name: WriteUnknown, code: codes.Unavailable, detail: NewWriteEvidence(WriteUnknown, WriteStageDispatch, WriteReasonFinished)},
		{name: "backend generation", code: codes.FailedPrecondition, detail: &errdetails.ErrorInfo{Domain: "protofleet.sdk", Reason: backendGenerationMismatch}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			firmwareDetail := &errdetails.ErrorInfo{Domain: "firmware", Reason: "CONTROLLER_FAILURE"}
			downstream, err := status.New(tc.code, "controller response lost").WithDetails(tc.detail, firmwareDetail)
			require.NoError(t, err)
			var accepted atomic.Int32
			device := outcomeRPCDevice(t, fakeDevice{startMiningFunc: func(context.Context) error {
				accepted.Add(1)
				return fmt.Errorf("downstream RPC: %w", downstream.Err())
			}})
			err = device.StartMining(t.Context())
			require.EqualValues(t, 1, accepted.Load())
			require.Equal(t, tc.code, status.Code(err))
			require.Equal(t, "downstream RPC: "+downstream.Err().Error(), status.Convert(err).Message())
			require.Nil(t, WriteEvidence(err), "a downstream status cannot certify SDK execution")
			require.False(t, IsBackendGenerationRejected(err))
			details := status.Convert(err).Details()
			require.Len(t, details, 1)
			info, ok := details[0].(*errdetails.ErrorInfo)
			require.True(t, ok)
			require.True(t, proto.Equal(firmwareDetail, info))
		})
	}
}

type outcomeReadDevice struct {
	fakeDevice
	err error
}

func (d outcomeReadDevice) GetMiningPools(context.Context) ([]ConfiguredPool, error) {
	return nil, d.err
}

func TestDriverReadRPCDoesNotForwardReservedEvidence(t *testing.T) {
	downstream, err := status.New(codes.FailedPrecondition, "controller read failed").WithDetails(
		NewWriteEvidence(WriteNotDispatched, WriteStageDispatch, WriteReasonRejected),
		&errdetails.ErrorInfo{Domain: "protofleet.sdk", Reason: backendGenerationMismatch},
		&errdetails.ErrorInfo{Domain: "protofleet.sdk", Reason: "OTHER_DETAIL"},
	)
	require.NoError(t, err)
	device := outcomeRPCDevice(t, outcomeReadDevice{err: downstream.Err()})
	_, err = device.GetMiningPools(t.Context())
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
	require.Equal(t, "controller read failed", status.Convert(err).Message())
	require.Nil(t, WriteEvidence(err))
	require.False(t, IsBackendGenerationRejected(err))
	require.Len(t, status.Convert(err).Details(), 1)
}

func TestSDKServerFilteringPreservesCauseAndLocalEvidence(t *testing.T) {
	downstream, err := status.New(codes.FailedPrecondition, "controller failed").WithDetails(
		NewWriteEvidence(WriteNotDispatched, WriteStageDispatch, WriteReasonRejected),
		&errdetails.ErrorInfo{Domain: "protofleet.sdk", Reason: backendGenerationMismatch},
	)
	require.NoError(t, err)
	wrapped := fmt.Errorf("%w: %w", downstream.Err(), context.DeadlineExceeded)
	filtered := sdkErrorToGRPCStatus(wrapped)
	require.ErrorIs(t, filtered, wrapped)
	require.ErrorIs(t, filtered, context.DeadlineExceeded)
	require.Equal(t, codes.FailedPrecondition, status.Code(filtered))
	require.Nil(t, WriteEvidence(filtered))
	require.False(t, IsBackendGenerationRejected(filtered))

	device := outcomeRPCDevice(t, fakeDevice{startMiningFunc: func(context.Context) error {
		return WithWriteOutcome(wrapped, WriteCompleted, WriteStageDispatch, WriteReasonFinished)
	}})
	err = device.StartMining(t.Context())
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
	require.Equal(t, WriteCompleted, WriteEvidence(err).GetReason())
	require.False(t, IsBackendGenerationRejected(err), "local completion does not endorse a downstream generation marker")

	lookup := sdkErrorToGRPCStatus(WriteRefused(backendGenerationRejected(), WriteStageBackendSelection, WriteReasonStaleBinding))
	require.True(t, IsBackendGenerationRejected(lookup))
	require.Equal(t, WriteNotDispatched, WriteEvidence(lookup).GetReason())
}

func TestWriteEvidencePreservesContextAndStatusCodesAcrossRPC(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		code codes.Code
	}{
		{name: "canceled", err: context.Canceled, code: codes.Canceled},
		{name: "wrapped canceled", err: fmt.Errorf("configuration read: %w", context.Canceled), code: codes.Canceled},
		{name: "deadline exceeded", err: context.DeadlineExceeded, code: codes.DeadlineExceeded},
		{name: "wrapped deadline exceeded", err: fmt.Errorf("configuration read: %w", context.DeadlineExceeded), code: codes.DeadlineExceeded},
		{name: "explicit internal with canceled cause", err: fmt.Errorf("%w: %w", status.Error(codes.Internal, "read failed"), context.Canceled), code: codes.Internal},
		{name: "explicit internal with deadline cause", err: fmt.Errorf("%w: %w", status.Error(codes.Internal, "read failed"), context.DeadlineExceeded), code: codes.Internal},
	} {
		t.Run(tc.name, func(t *testing.T) {
			device := outcomeRPCDevice(t, fakeDevice{startMiningFunc: func(context.Context) error {
				return WithWriteOutcome(tc.err, WriteNotDispatched, WriteStageConfigurationRead, WriteReasonReadFailed)
			}})
			err := device.StartMining(t.Context())
			require.Error(t, err)
			require.Equal(t, tc.code, status.Code(err))
			require.Equal(t, WriteNotDispatched, WriteEvidence(err).GetReason())
		})
	}
}

func TestUnsupportedCurtailmentConfigHasPreDispatchEvidence(t *testing.T) {
	device := outcomeRPCDevice(t, fakeDevice{})
	err := device.ApplyCurtailmentConfig(t.Context(), CurtailmentConfig{})
	require.Equal(t, codes.Unimplemented, status.Code(err))
	require.Equal(t, WriteNotDispatched, WriteEvidence(err).GetReason())
}

func TestCanceledRPCDoesNotClaimCompletionOfHandlerThatOutlivesCaller(t *testing.T) {
	entered, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	device := outcomeRPCDevice(t, fakeDevice{startMiningFunc: func(context.Context) error {
		close(entered)
		<-release
		defer close(finished)
		return WithWriteOutcome(errors.New("eventually finished"), WriteCompleted, WriteStageDispatch, WriteReasonFinished)
	}})
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	done := make(chan error, 1)
	go func() { done <- device.StartMining(ctx) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("handler did not start")
	}
	cancel()
	err := <-done
	require.Equal(t, codes.Canceled, status.Code(err))
	require.Nil(t, WriteEvidence(err), "caller cancellation carries no backend completion receipt")
	close(release)
	<-finished
	require.Nil(t, WriteEvidence(err), "later handler completion must not retroactively certify the lost response")
}

func TestWriteEvidenceRejectsCodesTextAndUnboundedMetadata(t *testing.T) {
	for _, code := range []codes.Code{codes.Unimplemented, codes.InvalidArgument, codes.FailedPrecondition, codes.DeadlineExceeded} {
		require.Nil(t, WriteEvidence(status.Error(code, WriteNotDispatched)))
	}
	info := NewWriteEvidence(WriteNotDispatched, WriteStageCredentials, WriteReasonMissingCredentials)
	info.Metadata["password"] = "must-not-persist"
	wire, err := status.New(codes.Internal, "failed").WithDetails(info)
	require.NoError(t, err)
	require.Nil(t, WriteEvidence(wire.Err()))
	require.Nil(t, NewWriteEvidence(WriteCompleted, "untrusted stage", WriteReasonFinished))
	wire, err = status.New(codes.Internal, "failed").WithDetails(&errdetails.ErrorInfo{Reason: WriteCompleted, Domain: "firmware"})
	require.NoError(t, err)
	require.Nil(t, WriteEvidence(wire.Err()))
}
