package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	grpcstatus "google.golang.org/grpc/status"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1/fleetnodegatewayv1connect"
	telemetrypb "github.com/block/proto-fleet/server/generated/grpc/telemetry/v1"
	"github.com/block/proto-fleet/server/internal/domain/plugins"
	modelsV2 "github.com/block/proto-fleet/server/internal/domain/telemetry/models/v2"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/block/proto-fleet/server/sdk/v1/mocks"
)

// Use the real SDK registry: direct driver mocks cannot detect one registration
// replacing another, or Close removing a concurrently registered device.
func newHandleTestGRPCDriver(t *testing.T, impl sdk.Driver, opts ...grpc.ServerOption) sdk.Driver {
	t.Helper()
	plugin := &sdk.DriverPlugin{Impl: impl}
	server := grpc.NewServer(opts...)
	require.NoError(t, plugin.GRPCServer(nil, server))
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	conn, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	client, err := plugin.GRPCClient(t.Context(), nil, conn)
	require.NoError(t, err)
	driver, ok := client.(sdk.Driver)
	require.True(t, ok)
	return driver
}

func newHandleTestTelemetryFetcher(t *testing.T, driver sdk.Driver) *pluginTelemetryFetcher {
	t.Helper()
	manager := plugins.NewManager(&plugins.Config{})
	require.NoError(t, manager.RegisterPluginForTest(&plugins.LoadedPlugin{
		Name: "handle-test", Identifier: sdk.DriverIdentifier{DriverName: "virtual"}, Driver: driver,
		Caps: sdk.Capabilities{sdk.CapabilityRealtimeTelemetry: true},
	}))
	fetcher, err := newPluginTelemetryFetcher(manager, nodeSecretProvider{})
	require.NoError(t, err)
	return fetcher
}

func handleTestTelemetryRequest() *telemetrypb.FleetNodeTelemetryRequest {
	return &telemetrypb.FleetNodeTelemetryRequest{
		DeviceIdentifier: "dev-1", IpAddress: "10.0.0.5", Port: "4028", UrlScheme: "http", DriverName: "virtual",
	}
}

func requireHandleTestSignal(t *testing.T, ctx context.Context, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

func requirePhysicalTelemetryIdentity(t *testing.T, result *telemetrypb.FleetNodeTelemetryResult) {
	t.Helper()
	require.Equal(t, "dev-1", result.GetDeviceIdentifier())
	var metrics modelsV2.DeviceMetrics
	require.NoError(t, json.Unmarshal(result.GetDeviceMetricsJson(), &metrics))
	assert.Equal(t, "dev-1", metrics.DeviceIdentifier)
}

type pausedFirmwareDownloadGateway struct {
	firmwareDownloadGateway
	started chan struct{}
	proceed chan struct{}
}

func (g *pausedFirmwareDownloadGateway) DownloadCommandArtifact(ctx context.Context, req *connect.Request[pb.DownloadCommandArtifactRequest], stream *connect.ServerStream[pb.DownloadCommandArtifactResponse]) error {
	close(g.started)
	select {
	case <-g.proceed:
	case <-ctx.Done():
		return fmt.Errorf("firmware download canceled: %w", ctx.Err())
	}
	return g.firmwareDownloadGateway.DownloadCommandArtifact(ctx, req, stream)
}

func TestMinerCommandHandleSurvivesTelemetryAndConcurrentCommand(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	ctrl := gomock.NewController(t)
	impl := mocks.NewMockDriver(ctrl)
	payload := []byte("firmware payload")
	var commandHandle, telemetryHandle, secondCommandHandle string
	telemetryClosed := make(chan struct{})
	gomock.InOrder(
		impl.EXPECT().NewDevice(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, id string, _ sdk.DeviceInfo, _ sdk.SecretBundle) (sdk.NewDeviceResult, error) {
				commandHandle = id
				dev := mocks.NewMockDevice(ctrl)
				dev.EXPECT().ID().Return(id)
				dev.EXPECT().FirmwareUpdate(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, firmware sdk.FirmwareFile) error {
					got, err := io.ReadAll(firmware.Reader)
					assert.NoError(t, err)
					assert.Equal(t, payload, got)
					return nil
				})
				dev.EXPECT().Close(gomock.Any()).Return(nil)
				return sdk.NewDeviceResult{Device: dev}, nil
			}),
		impl.EXPECT().NewDevice(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, id string, _ sdk.DeviceInfo, _ sdk.SecretBundle) (sdk.NewDeviceResult, error) {
				telemetryHandle = id
				dev := mocks.NewMockDevice(ctrl)
				dev.EXPECT().ID().Return(id)
				dev.EXPECT().Status(gomock.Any()).Return(sdk.DeviceMetrics{DeviceID: id, Timestamp: time.Now(), Health: sdk.HealthHealthyActive, FirmwareVersion: "2.0.0"}, nil)
				dev.EXPECT().Close(gomock.Any()).DoAndReturn(func(context.Context) error { close(telemetryClosed); return nil })
				return sdk.NewDeviceResult{Device: dev}, nil
			}),
		impl.EXPECT().NewDevice(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, id string, _ sdk.DeviceInfo, _ sdk.SecretBundle) (sdk.NewDeviceResult, error) {
				secondCommandHandle = id
				dev := mocks.NewMockDevice(ctrl)
				dev.EXPECT().ID().Return(id)
				dev.EXPECT().Reboot(gomock.Any()).Return(nil)
				dev.EXPECT().Close(gomock.Any()).Return(nil)
				return sdk.NewDeviceResult{Device: dev}, nil
			}),
	)
	driver := newHandleTestGRPCDriver(t, impl)
	ref := firmwareRef("firmware-1", "update.swu", payload)
	gateway := &pausedFirmwareDownloadGateway{
		firmwareDownloadGateway: firmwareDownloadGateway{ref: ref, payload: payload},
		started:                 make(chan struct{}), proceed: make(chan struct{}),
	}
	path, handler := fleetnodegatewayv1connect.NewFleetNodeGatewayServiceHandler(gateway)
	mux := http.NewServeMux()
	mux.Handle(path, handler)
	httpServer := httptest.NewServer(mux)
	defer httpServer.Close()
	gatewayClient := fleetnodegatewayv1connect.NewFleetNodeGatewayServiceClient(httpServer.Client(), httpServer.URL)
	r := &RunCmd{driverGetter: fakeDriverGetter{d: driver}, minerSecrets: nodeSecretProvider{}, firmwareTempRoot: t.TempDir()}
	ack := &captureAcker{}
	commandDone := make(chan struct{})
	go func() {
		defer close(commandDone)
		r.handleMinerCommand(ctx, gatewayClient, ack, "cmd-1", withTarget(&pb.MinerCommand{
			Action: &pb.MinerCommand_FirmwareUpdate{FirmwareUpdate: &pb.FirmwareUpdateAction{Artifact: ref}},
		}), discardLogger(t))
	}()
	requireHandleTestSignal(t, ctx, gateway.started)

	sample, err := newHandleTestTelemetryFetcher(t, driver).Fetch(ctx, handleTestTelemetryRequest())
	require.NoError(t, err)
	requirePhysicalTelemetryIdentity(t, sample)
	assert.Equal(t, "2.0.0", sample.GetFirmwareVersion())
	requireHandleTestSignal(t, ctx, telemetryClosed)

	// A redelivered command ID must also get a fresh registration while the
	// original command is still downloading its artifact.
	secondAck := &captureAcker{}
	r.handleMinerCommand(ctx, nil, secondAck, "cmd-1", withTarget(&pb.MinerCommand{
		Action: &pb.MinerCommand_Reboot{Reboot: &pb.RebootAction{}},
	}), discardLogger(t))
	require.True(t, secondAck.only(t).GetSucceeded())
	close(gateway.proceed)
	requireHandleTestSignal(t, ctx, commandDone)
	require.True(t, ack.only(t).GetSucceeded(), ack.only(t).GetErrorMessage())
	assert.Equal(t, "dev-1", gateway.request.GetDeviceIdentifier())
	assert.NotEmpty(t, commandHandle)
	assert.NotEqual(t, "dev-1", commandHandle)
	assert.NotEqual(t, commandHandle, telemetryHandle)
	assert.NotEqual(t, commandHandle, secondCommandHandle)
	assert.NotEqual(t, telemetryHandle, secondCommandHandle)
}

func TestTelemetryHandleSurvivesPreviousSampleDelayedClose(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	firstCloseStarted, firstCloseReleased := make(chan struct{}), make(chan struct{})
	secondStatusStarted, secondStatusReleased := make(chan struct{}), make(chan struct{})
	var closes, statuses atomic.Int32
	interceptor := func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		var release <-chan struct{}
		switch {
		case strings.HasSuffix(info.FullMethod, "/CloseDevice") && closes.Add(1) == 1:
			close(firstCloseStarted)
			release = firstCloseReleased
		case strings.HasSuffix(info.FullMethod, "/DeviceStatus") && statuses.Add(1) == 2:
			close(secondStatusStarted)
			release = secondStatusReleased
		}
		if release != nil {
			select {
			case <-release:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		return handler(ctx, req)
	}
	ctrl := gomock.NewController(t)
	impl := mocks.NewMockDriver(ctrl)
	closed := []chan struct{}{make(chan struct{}), make(chan struct{})}
	handles := make([]string, 2)
	for i := range 2 {
		impl.EXPECT().NewDevice(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, id string, _ sdk.DeviceInfo, _ sdk.SecretBundle) (sdk.NewDeviceResult, error) {
				handles[i] = id
				dev := mocks.NewMockDevice(ctrl)
				dev.EXPECT().ID().Return(id)
				dev.EXPECT().Status(gomock.Any()).Return(sdk.DeviceMetrics{DeviceID: id, Timestamp: time.Now(), Health: sdk.HealthHealthyActive}, nil)
				dev.EXPECT().Close(gomock.Any()).DoAndReturn(func(context.Context) error { close(closed[i]); return nil })
				return sdk.NewDeviceResult{Device: dev}, nil
			})
	}
	fetcher := newHandleTestTelemetryFetcher(t, newHandleTestGRPCDriver(t, impl, grpc.UnaryInterceptor(interceptor)))
	first, err := fetcher.Fetch(ctx, handleTestTelemetryRequest())
	require.NoError(t, err)
	requirePhysicalTelemetryIdentity(t, first)
	requireHandleTestSignal(t, ctx, firstCloseStarted)
	outcome := make(chan telemetryFetchOutcome, 1)
	go func() {
		result, err := fetcher.Fetch(ctx, handleTestTelemetryRequest())
		outcome <- telemetryFetchOutcome{result: result, err: err}
	}()
	// Pause the next status RPC after registration but before registry lookup,
	// then let the previous sample's asynchronous close reach the SDK.
	requireHandleTestSignal(t, ctx, secondStatusStarted)
	close(firstCloseReleased)
	select {
	case <-closed[0]:
	case <-closed[1]:
		t.Fatal("previous sample closed the next sample's device handle")
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	close(secondStatusReleased)
	select {
	case second := <-outcome:
		require.NoError(t, second.err)
		requirePhysicalTelemetryIdentity(t, second.result)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	requireHandleTestSignal(t, ctx, closed[1])
	assert.NotEmpty(t, handles[0])
	assert.NotEqual(t, "dev-1", handles[0])
	assert.NotEqual(t, handles[0], handles[1])
}

func TestNodeCleansUpUncertainDeviceRegistration(t *testing.T) {
	for _, operation := range []string{"command", "telemetry"} {
		for _, code := range []codes.Code{codes.Canceled, codes.DeadlineExceeded, codes.Unavailable} {
			t.Run(operation+"/"+code.String(), func(t *testing.T) {
				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				defer cancel()
				ctrl := gomock.NewController(t)
				impl := mocks.NewMockDriver(ctrl)
				closed := make(chan struct{})
				impl.EXPECT().NewDevice(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
					func(_ context.Context, id string, _ sdk.DeviceInfo, _ sdk.SecretBundle) (sdk.NewDeviceResult, error) {
						assert.NotEmpty(t, id)
						assert.NotEqual(t, "dev-1", id)
						dev := mocks.NewMockDevice(ctrl)
						dev.EXPECT().ID().Return(id)
						dev.EXPECT().Close(gomock.Any()).DoAndReturn(func(closeCtx context.Context) error {
							assert.NoError(t, closeCtx.Err(), "cleanup must outlive the canceled request")
							close(closed)
							return nil
						})
						return sdk.NewDeviceResult{Device: dev}, nil
					})
				interceptor := func(callCtx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
					response, err := handler(callCtx, req)
					if err == nil && strings.HasSuffix(info.FullMethod, "/NewDevice") {
						// Registration succeeds, but its response never reaches the caller.
						if code == codes.Canceled {
							cancel()
						}
						return nil, grpcstatus.Error(code, "lost NewDevice response")
					}
					return response, err
				}
				driver := newHandleTestGRPCDriver(t, impl, grpc.UnaryInterceptor(interceptor))
				if operation == "command" {
					r := &RunCmd{driverGetter: fakeDriverGetter{d: driver}, minerSecrets: nodeSecretProvider{}}
					ack := &captureAcker{}
					r.handleMinerCommand(ctx, nil, ack, "cmd-1", withTarget(&pb.MinerCommand{
						Action: &pb.MinerCommand_Reboot{Reboot: &pb.RebootAction{}},
					}), discardLogger(t))
					assert.False(t, ack.only(t).GetSucceeded())
				} else {
					_, err := newHandleTestTelemetryFetcher(t, driver).Fetch(ctx, handleTestTelemetryRequest())
					require.Error(t, err)
				}
				select {
				case <-closed:
				default:
					t.Fatal("uncertain registration was not closed")
				}
			})
		}
	}
}
