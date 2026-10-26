package main

import (
	"context"
	"math"
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
	buckets := series.Msg.GetBuckets()
	if series.Msg.GetUnit() != "TH/s" || len(buckets) == 0 || buckets[0].GetAvg() <= 0 {
		t.Fatalf("unexpected time series: unit=%q buckets=%v", series.Msg.GetUnit(), buckets)
	}
}

func TestConnectGatewayPreservesStatusCodes(t *testing.T) {
	srv, _ := newConnectTestServer(t)
	ctx := context.Background()
	hashboards := mdkv2connect.NewHashboardServiceClient(http.DefaultClient, srv.URL, connect.WithGRPCWeb())
	runIn := mdkv2connect.NewRunInServiceClient(http.DefaultClient, srv.URL, connect.WithGRPCWeb())
	telemetry := mdkv2connect.NewTelemetryServiceClient(http.DefaultClient, srv.URL, connect.WithGRPCWeb())

	for _, tc := range []struct {
		name string
		call func() error
		want connect.Code
	}{
		{"unknown hashboard", func() error {
			_, err := hashboards.GetHashboardASICs(ctx, connect.NewRequest(&mdk.GetHashboardASICsRequest{Identifier: "missing"}))
			return err
		}, connect.CodeNotFound},
		{"run-in without config", func() error {
			_, err := runIn.StartModuleRunIn(ctx, connect.NewRequest(&mdk.StartModuleRunInRequest{ModuleId: "T1-M01"}))
			return err
		}, connect.CodeInvalidArgument},
		{"time series without range", func() error {
			_, err := telemetry.QueryTimeSeries(ctx, connect.NewRequest(&mdk.QueryTimeSeriesRequest{
				Metric:     mdk.TimeSeriesMetric_TIME_SERIES_METRIC_HASHRATE,
				BucketSize: mdk.BucketSize_BUCKET_SIZE_5M,
			}))
			return err
		}, connect.CodeInvalidArgument},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := connect.CodeOf(tc.call()); got != tc.want {
				t.Fatalf("code = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestConnectRejectsNonFinitePowerTargets(t *testing.T) {
	srv, state := newConnectTestServer(t)
	ctx := context.Background()
	mining := mdkv2connect.NewMiningServiceClient(http.DefaultClient, srv.URL, connect.WithGRPCWeb())
	before, err := mining.GetTargetPower(ctx, connect.NewRequest(&mdk.GetTargetPowerRequest{}))
	if err != nil {
		t.Fatalf("GetTargetPower: %v", err)
	}

	for _, watts := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		_, err := mining.SetTargetPower(ctx, connect.NewRequest(&mdk.SetTargetPowerRequest{Watts: watts}))
		if got := connect.CodeOf(err); got != connect.CodeInvalidArgument {
			t.Fatalf("SetTargetPower(%v) code = %v, want invalid_argument", watts, got)
		}
		_, err = mining.SetPowerTargetScoped(ctx, connect.NewRequest(&mdk.SetPowerTargetScopedRequest{
			Scope: &mdk.BoardScope{Modules: []*mdk.ModuleSelector{{Tank: 1, Module: 1}}},
			Watts: watts,
		}))
		if got := connect.CodeOf(err); got != connect.CodeInvalidArgument {
			t.Fatalf("SetPowerTargetScoped(%v) code = %v, want invalid_argument", watts, got)
		}
	}

	after, err := mining.GetTargetPower(ctx, connect.NewRequest(&mdk.GetTargetPowerRequest{}))
	if err != nil {
		t.Fatalf("GetTargetPower after rejected writes: %v", err)
	}
	if after.Msg.GetWatts() != before.Msg.GetWatts() {
		t.Fatalf("target watts = %v, want unchanged %v", after.Msg.GetWatts(), before.Msg.GetWatts())
	}
	if _, err := mining.GetMiningStatus(ctx, connect.NewRequest(&mdk.GetMiningStatusRequest{})); err != nil {
		t.Fatalf("GetMiningStatus after rejected writes: %v", err)
	}
	state.mu.RLock()
	defer state.mu.RUnlock()
	for _, board := range state.eachBoard() {
		if board.ScopedWattsSet {
			t.Fatalf("board %s has a scoped power target after rejected writes", board.MAC)
		}
	}
}

func TestConnectWritesRecordHistoryLikeREST(t *testing.T) {
	srv, state := newConnectTestServer(t)
	mining := mdkv2connect.NewMiningServiceClient(http.DefaultClient, srv.URL, connect.WithGRPCWeb())
	if _, err := mining.StopMining(context.Background(), connect.NewRequest(&mdk.StopMiningRequest{})); err != nil {
		t.Fatalf("StopMining: %v", err)
	}

	state.mu.RLock()
	defer state.mu.RUnlock()
	latest := state.history.Samples[len(state.history.Samples)-1]
	for _, board := range latest.Boards {
		if board.Mining {
			t.Fatal("latest history sample still reports a mining board after StopMining")
		}
	}
}

func TestBoundToGETFollowsRESTBindings(t *testing.T) {
	for procedure, want := range map[string]bool{
		mdkv2connect.SystemServiceGetSystemInfoProcedure:               true,
		mdkv2connect.TelemetryServiceQueryTimeSeriesProcedure:          true,
		mdkv2connect.MiningServiceStopMiningScopedProcedure:            false,
		mdkv2connect.MiningServiceSetTargetPowerProcedure:              false,
		mdkv2connect.RunInServiceStartModuleRunInProcedure:             false,
		"/mdk.v2.SystemService/NoSuchMethod":                           false,
		mdkv2connect.TelemetryServiceQueryShareQualityHistoryProcedure: true,
	} {
		if got := boundToGET(procedure); got != want {
			t.Errorf("boundToGET(%q) = %v, want %v", procedure, got, want)
		}
	}
}
