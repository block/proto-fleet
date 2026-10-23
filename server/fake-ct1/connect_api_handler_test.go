package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"
	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	"github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2/mdkv2connect"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func newConnectTestServer(t *testing.T) (*httptest.Server, *ContainerState) {
	t.Helper()
	return newConnectTestServerWithWebRoot(t, "")
}

func newConnectTestServerWithWebRoot(t *testing.T, webRoot string) (*httptest.Server, *ContainerState) {
	t.Helper()
	state := newContainerState(testConfig(), time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC))
	state.now = func() time.Time { return state.StartTime }
	handler, cleanup, err := newHTTPHandler(state, webRoot)
	if err != nil {
		t.Fatalf("new HTTP handler: %v", err)
	}
	t.Cleanup(cleanup)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv, state
}

func TestConnectGatewayBypassesWebRootStaticFallback(t *testing.T) {
	srv, _ := newConnectTestServerWithWebRoot(t, testWebRoot(t))
	system := mdkv2connect.NewSystemServiceClient(http.DefaultClient, srv.URL, connect.WithGRPCWeb())
	info, err := system.GetSystemInfo(context.Background(), connect.NewRequest(&mdk.GetSystemInfoRequest{}))
	if err != nil {
		t.Fatalf("GetSystemInfo through web-root handler: %v", err)
	}
	if info.Msg.GetInfo().GetProduct() != "CT1" {
		t.Fatalf("product = %q, want CT1", info.Msg.GetInfo().GetProduct())
	}
}

func TestConnectGatewayServesBrowserMDKClient(t *testing.T) {
	srv, state := newConnectTestServer(t)
	options := []connect.ClientOption{connect.WithGRPCWeb()}
	ctx := context.Background()

	system := mdkv2connect.NewSystemServiceClient(http.DefaultClient, srv.URL, options...)
	info, err := system.GetSystemInfo(ctx, connect.NewRequest(&mdk.GetSystemInfoRequest{}))
	if err != nil {
		t.Fatalf("GetSystemInfo: %v", err)
	}
	if info.Msg.GetInfo().GetProduct() != "CT1" {
		t.Fatalf("product = %q, want CT1", info.Msg.GetInfo().GetProduct())
	}
	status, err := system.GetSystemStatus(ctx, connect.NewRequest(&mdk.GetSystemStatusRequest{}))
	if err != nil {
		t.Fatalf("GetSystemStatus: %v", err)
	}
	if status.Msg.GetStatus().GetServerId() != state.ServerID {
		t.Fatalf("server id = %q, want %q", status.Msg.GetStatus().GetServerId(), state.ServerID)
	}

	hashboards := mdkv2connect.NewHashboardServiceClient(http.DefaultClient, srv.URL, options...)
	layout, err := hashboards.GetContainerMap(ctx, connect.NewRequest(&mdk.GetContainerMapRequest{}))
	if err != nil {
		t.Fatalf("GetContainerMap: %v", err)
	}
	if got := len(layout.Msg.GetContainerMap().GetTanks()); got != state.Config.Tanks {
		t.Fatalf("tanks = %d, want %d", got, state.Config.Tanks)
	}

	mining := mdkv2connect.NewMiningServiceClient(http.DefaultClient, srv.URL, options...)
	miningStatus, err := mining.GetMiningStatus(ctx, connect.NewRequest(&mdk.GetMiningStatusRequest{}))
	if err != nil {
		t.Fatalf("GetMiningStatus: %v", err)
	}
	if miningStatus.Msg.GetTotalBoards() != int32(state.BoardCount()) {
		t.Fatalf("total boards = %d, want %d", miningStatus.Msg.GetTotalBoards(), state.BoardCount())
	}

	stop, err := mining.StopMiningScoped(ctx, connect.NewRequest(&mdk.StopMiningScopedRequest{
		Scope: &mdk.BoardScope{Modules: []*mdk.ModuleSelector{{Tank: 1, Module: 1}}},
	}))
	if err != nil {
		t.Fatalf("StopMiningScoped: %v", err)
	}
	if stop.Msg.GetBoardsStopped() != int32(state.Config.SlotsPerModule) {
		t.Fatalf("boards stopped = %d, want %d", stop.Msg.GetBoardsStopped(), state.Config.SlotsPerModule)
	}

	telemetry := mdkv2connect.NewTelemetryServiceClient(http.DefaultClient, srv.URL, options...)
	health, err := telemetry.GetContainerHealth(ctx, connect.NewRequest(&mdk.GetContainerHealthRequest{}))
	if err != nil {
		t.Fatalf("GetContainerHealth: %v", err)
	}
	if health.Msg.GetTimestamp() == nil {
		t.Fatal("container health timestamp is nil")
	}
	series, err := telemetry.QueryTimeSeries(ctx, connect.NewRequest(&mdk.QueryTimeSeriesRequest{
		Metric:     mdk.TimeSeriesMetric_TIME_SERIES_METRIC_HASHRATE,
		BucketSize: mdk.BucketSize_BUCKET_SIZE_5M,
		Range: &mdk.TimeRange{
			Start: timestamppb.New(state.StartTime.Add(-time.Hour)),
			End:   timestamppb.New(state.StartTime),
		},
	}))
	if err != nil {
		t.Fatalf("QueryTimeSeries: %v", err)
	}
	if len(series.Msg.GetBuckets()) == 0 {
		t.Fatal("time series returned no buckets")
	}
}
