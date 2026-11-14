package ct1

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// Legacy JSON fixtures cross a real gRPC connection. No REST network listener is used.
type grpcFixture struct {
	mdk.UnimplementedSystemServiceServer
	mdk.UnimplementedHashboardServiceServer
	mdk.UnimplementedMiningServiceServer
	mdk.UnimplementedTelemetryServiceServer
	mdk.UnimplementedPowerServiceServer
	tankFixtures bool
	handler      http.Handler
	server       *grpc.Server
	URL          string
}

func newGRPCFixture(t *testing.T, handler http.Handler) *grpcFixture {
	return newConfiguredGRPCFixture(t, handler, false)
}
func newTankGRPCFixture(t *testing.T, handler http.Handler) *grpcFixture {
	return newConfiguredGRPCFixture(t, handler, true)
}
func newConfiguredGRPCFixture(t *testing.T, handler http.Handler, tankFixtures bool) *grpcFixture {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	f := &grpcFixture{tankFixtures: tankFixtures, handler: handler, server: grpc.NewServer(), URL: "http://" + listener.Addr().String()}
	mdk.RegisterSystemServiceServer(f.server, f)
	mdk.RegisterHashboardServiceServer(f.server, f)
	mdk.RegisterMiningServiceServer(f.server, f)
	mdk.RegisterTelemetryServiceServer(f.server, f)
	mdk.RegisterPowerServiceServer(f.server, f)
	go func() { _ = f.server.Serve(listener) }()
	t.Cleanup(f.Close)
	return f
}
func (f *grpcFixture) Close() { f.server.Stop() }
func (f *grpcFixture) call(ctx context.Context, method, path, body string, out proto.Message) error {
	req := httptest.NewRequest(method, path, strings.NewReader(body)).WithContext(ctx)
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		for _, actor := range md.Get("x-frigate-actor") {
			req.Header.Add("X-Frigate-Actor", actor)
		}
	}
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		code := codes.Unavailable
		switch w.Code {
		case 404:
			code = codes.Unimplemented
		case 401:
			code = codes.Unauthenticated
		case 403:
			code = codes.PermissionDenied
		case 409:
			code = codes.FailedPrecondition
		}
		return status.Error(code, "fixture response") //nolint:wrapcheck // server must return a gRPC status
	}
	data := w.Body.Bytes()
	// Give old mining fixtures the explicit power presence introduced in the pinned contract.
	if _, ok := out.(*mdk.GetMiningStatusResponse); ok {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(data, &fields); err != nil {
			return fmt.Errorf("decode fixture: %w", err)
		}
		if power, ok := fields["power"]; ok {
			fields["currentMeasurements"] = json.RawMessage(`{"scope":"MINING_MEASUREMENT_SCOPE_SHARED_ASSIGNMENTS","hashboardPowerW":{"complete":true,"value":` + string(power) + `}}`)
		}
		var err error
		data, err = json.Marshal(fields)
		if err != nil {
			return fmt.Errorf("encode fixture: %w", err)
		}
	}
	if err := protojson.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode protobuf fixture: %w", err)
	}
	return nil
}
func (f *grpcFixture) GetSystemInfo(ctx context.Context, _ *mdk.GetSystemInfoRequest) (*mdk.GetSystemInfoResponse, error) {
	if f.tankFixtures {
		out := new(mdk.GetSystemInfoResponse)
		err := f.call(ctx, "GET", "/api/v2/system/info", "", out)
		return out, err
	}
	return &mdk.GetSystemInfoResponse{Info: &mdk.SystemInfo{Id: "ct1", Product: "CT1", Version: "test"}}, nil
}

func (f *grpcFixture) GetContainerMap(ctx context.Context, _ *mdk.GetContainerMapRequest) (*mdk.GetContainerMapResponse, error) {
	out := new(mdk.GetContainerMapResponse)
	err := f.call(ctx, "GET", "/api/v2/container/map", "", out)
	// These older tank-control fixtures list physical slots in order. Supply
	// the explicit coordinates required by the native MDK contract.
	if f.tankFixtures && err == nil {
		for _, tank := range out.GetContainerMap().GetTanks() {
			for _, module := range tank.GetModules() {
				position := uint32(1)
				for _, slot := range module.GetSlots() {
					slot.ConfiguredLocation = &mdk.HashboardLocation{HashboardPosition: new(position)}
					position++
				}
			}
		}
	}
	return out, err
}
func (f *grpcFixture) GetMiningStatus(ctx context.Context, _ *mdk.GetMiningStatusRequest) (*mdk.GetMiningStatusResponse, error) {
	out := new(mdk.GetMiningStatusResponse)
	err := f.call(ctx, "GET", "/api/v2/mining/status", "", out)
	return out, err
}
func (f *grpcFixture) GetContainerHealth(ctx context.Context, _ *mdk.GetContainerHealthRequest) (*mdk.GetContainerHealthResponse, error) {
	out := new(mdk.GetContainerHealthResponse)
	err := f.call(ctx, "GET", "/api/v2/telemetry/container", "", out)
	return out, err
}
func (f *grpcFixture) GetContainerControls(ctx context.Context, _ *mdk.GetContainerControlsRequest) (*mdk.GetContainerControlsResponse, error) {
	out := new(mdk.GetContainerControlsResponse)
	err := f.call(ctx, "GET", "/api/v2/power/container/controls", "", out)
	return out, err
}
func (f *grpcFixture) SetContainerControl(ctx context.Context, in *mdk.SetContainerControlRequest) (*mdk.SetContainerControlResponse, error) {
	body := fmt.Sprintf(`{"enabled":%t}`, in.Enabled)
	out := new(mdk.SetContainerControlResponse)
	err := f.call(ctx, "POST", "/api/v2/power/container/controls/"+in.Name, body, out)
	return out, err
}
func (f *grpcFixture) SetMainCircuitBreakerState(ctx context.Context, in *mdk.SetMainCircuitBreakerStateRequest) (*mdk.SetMainCircuitBreakerStateResponse, error) {
	body := fmt.Sprintf(`{"closed":%t}`, in.Closed)
	out := new(mdk.SetMainCircuitBreakerStateResponse)
	err := f.call(ctx, "POST", "/api/v2/power/container/main-circuit-breaker", body, out)
	return out, err
}

func (f *grpcFixture) StartMiningScoped(ctx context.Context, in *mdk.StartMiningScopedRequest) (*mdk.StartMiningScopedResponse, error) {
	body, err := protojson.Marshal(in)
	if err != nil {
		return nil, fmt.Errorf("encode scoped mining request: %w", err)
	}
	out := new(mdk.StartMiningScopedResponse)
	err = f.call(ctx, "POST", "/api/v2/mining/scoped/start", string(body), out)
	return out, err
}
func (f *grpcFixture) StopMiningScoped(ctx context.Context, in *mdk.StopMiningScopedRequest) (*mdk.StopMiningScopedResponse, error) {
	body, err := protojson.Marshal(in)
	if err != nil {
		return nil, fmt.Errorf("encode scoped mining request: %w", err)
	}
	out := new(mdk.StopMiningScopedResponse)
	err = f.call(ctx, "POST", "/api/v2/mining/scoped/stop", string(body), out)
	return out, err
}
func (f *grpcFixture) SetIdentifyLED(ctx context.Context, in *mdk.SetIdentifyLEDRequest) (*mdk.SetIdentifyLEDResponse, error) {
	body, err := protojson.Marshal(in)
	if err != nil {
		return nil, fmt.Errorf("encode identify LED request: %w", err)
	}
	out := new(mdk.SetIdentifyLEDResponse)
	err = f.call(ctx, "POST", "/api/v2/hashboards/identify-led", string(body), out)
	return out, err
}
