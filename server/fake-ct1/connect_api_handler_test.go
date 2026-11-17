package main

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"
	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	"github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2/mdkv2connect"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
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

func TestConnectMiningGroupInspectionSharesNativeState(t *testing.T) {
	srv, state := newConnectTestServer(t)
	ctx := t.Context()
	client := mdkv2connect.NewMiningGroupServiceClient(http.DefaultClient, srv.URL)
	native := &grpcAPI{state: state}
	mode, err := client.GetMiningGroupCapability(ctx, connect.NewRequest(&mdk.GetMiningGroupCapabilityRequest{}))
	if err != nil || mode.Msg.GetCapability().GetEnabled() {
		t.Fatalf("initial mode = %v, %v", mode, err)
	}
	if _, err := native.StopMining(ctx, &mdk.StopMiningRequest{}); err != nil {
		t.Fatal(err)
	}
	if _, err := native.ActivateMiningGroups(ctx, &mdk.ActivateMiningGroupsRequest{ExpectedStateRevision: mode.Msg.Capability.StateRevision, IdempotencyKey: "inspection"}); err != nil {
		t.Fatal(err)
	}
	mode, err = client.GetMiningGroupCapability(ctx, connect.NewRequest(&mdk.GetMiningGroupCapabilityRequest{}))
	if err != nil || !mode.Msg.GetCapability().GetEnabled() {
		t.Fatalf("group mode = %v, %v", mode, err)
	}
	groups, err := client.ListMiningGroups(ctx, connect.NewRequest(&mdk.ListMiningGroupsRequest{}))
	if err != nil || len(groups.Msg.GetGroups()) != 1 || groups.Msg.Groups[0].Id != "default" {
		t.Fatalf("groups = %v, %v", groups, err)
	}
	members, err := client.ListMiningGroupMemberships(ctx, connect.NewRequest(&mdk.ListMiningGroupMembershipsRequest{}))
	if err != nil || len(members.Msg.GetMemberships()) != state.BoardCount() {
		t.Fatalf("members = %v, %v", members, err)
	}
	group, err := client.GetMiningGroup(ctx, connect.NewRequest(&mdk.GetMiningGroupRequest{Id: "default"}))
	if err != nil {
		t.Fatal(err)
	}
	username := "account.region.worker-01"
	password := "inspection-secret"
	_, err = native.ReplaceMiningGroupPools(ctx, &mdk.ReplaceMiningGroupPoolsRequest{GroupId: "default", ExpectedRevision: group.Msg.Group.Revision, IdempotencyKey: "inspection-pools", Pools: []*mdk.MiningGroupPoolInput{
		{Pool: &mdk.MiningGroupPoolRecord{Id: 1, Name: "Inspection", Url: "stratum+tcp://pool.example:3333", Username: username, Enabled: true}, Password: &password},
	}})
	if err != nil {
		t.Fatal(err)
	}
	pools, err := client.ListMiningGroupPools(ctx, connect.NewRequest(&mdk.ListMiningGroupPoolsRequest{GroupId: "default"}))
	if err != nil || len(pools.Msg.GetPools()) != 1 || pools.Msg.Pools[0].Username != username {
		t.Fatalf("pools = %v, %v", pools, err)
	}
	// Inspection must not accidentally expose the native mutation workflow.
	_, err = client.CreateMiningGroup(ctx, connect.NewRequest(&mdk.CreateMiningGroupRequest{IdempotencyKey: "not-exposed"}))
	if connect.CodeOf(err) != connect.CodeUnimplemented {
		t.Fatalf("CreateMiningGroup = %v, want Unimplemented", err)
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
	for protocol, options := range map[string][]connect.ClientOption{
		"connect":  nil,
		"grpc-web": {connect.WithGRPCWeb()},
	} {
		hashboards := mdkv2connect.NewHashboardServiceClient(http.DefaultClient, srv.URL, options...)
		runIn := mdkv2connect.NewRunInServiceClient(http.DefaultClient, srv.URL, options...)
		telemetry := mdkv2connect.NewTelemetryServiceClient(http.DefaultClient, srv.URL, options...)

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
			t.Run(protocol+"/"+tc.name, func(t *testing.T) {
				if got := connect.CodeOf(tc.call()); got != tc.want {
					t.Fatalf("code = %v, want %v", got, tc.want)
				}
			})
		}
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

func TestRPCWritesRecordHistoryLikeREST(t *testing.T) {
	scope := &mdk.BoardScope{Modules: []*mdk.ModuleSelector{{Tank: 1, Module: 1}}}
	for _, tc := range []struct {
		name string
		stop func(*testing.T, *httptest.Server, *ContainerState) error
	}{
		{"connect", func(t *testing.T, srv *httptest.Server, _ *ContainerState) error {
			mining := mdkv2connect.NewMiningServiceClient(http.DefaultClient, srv.URL)
			_, err := mining.StopMiningScoped(context.Background(), connect.NewRequest(&mdk.StopMiningScopedRequest{Scope: scope}))
			return err
		}},
		{"native gRPC", func(t *testing.T, _ *httptest.Server, state *ContainerState) error {
			_, err := mdk.NewMiningServiceClient(newNativeTestConn(t, state)).StopMiningScoped(context.Background(), &mdk.StopMiningScopedRequest{Scope: scope})
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, state := newConnectTestServer(t)
			state.mu.RLock()
			module := state.Tanks[0].Modules[0]
			miningBefore := 0
			for _, slot := range module.Slots {
				if slot.Board != nil && slot.Board.isMining() {
					miningBefore++
				}
			}
			state.mu.RUnlock()
			if miningBefore == 0 {
				t.Fatal("precondition: tank 1 module 1 has no mining boards to stop")
			}

			if err := tc.stop(t, srv, state); err != nil {
				t.Fatalf("StopMiningScoped: %v", err)
			}

			state.mu.RLock()
			defer state.mu.RUnlock()
			latest := state.history.Samples[len(state.history.Samples)-1]
			for i, board := range state.eachBoard() {
				if latest.Boards[i].Mining != board.isMining() {
					t.Fatalf("latest history sample board %s mining = %v, want %v", board.MAC, latest.Boards[i].Mining, board.isMining())
				}
			}
		})
	}
}

func TestSetTargetPowerKeepsOmittedFlags(t *testing.T) {
	on, off := true, false
	for _, tc := range []struct {
		name string
		set  func(t *testing.T, srv *httptest.Server, balanceBays, hashOnDisconnect *bool)
	}{
		{"REST", func(t *testing.T, srv *httptest.Server, balanceBays, hashOnDisconnect *bool) {
			body, err := json.Marshal(targetPowerRequest{Watts: 2800, BalanceBays: balanceBays, HashOnDisconnect: hashOnDisconnect})
			if err != nil {
				t.Fatal(err)
			}
			req, err := http.NewRequest(http.MethodPut, srv.URL+"/api/v2/mining/target-power", bytes.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Content-Type", "application/json")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("PUT target-power status = %d", resp.StatusCode)
			}
		}},
		{"Connect", func(t *testing.T, srv *httptest.Server, balanceBays, hashOnDisconnect *bool) {
			mining := mdkv2connect.NewMiningServiceClient(http.DefaultClient, srv.URL)
			if _, err := mining.SetTargetPower(context.Background(), connect.NewRequest(&mdk.SetTargetPowerRequest{
				Watts:            2800,
				BalanceBays:      balanceBays,
				HashOnDisconnect: hashOnDisconnect,
			})); err != nil {
				t.Fatalf("SetTargetPower: %v", err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, state := newConnectTestServer(t)
			flags := func() (bool, bool) {
				state.mu.RLock()
				defer state.mu.RUnlock()
				return state.BalanceBays, state.HashOnDisconnect
			}

			tc.set(t, srv, &on, &on)
			tc.set(t, srv, nil, nil)
			if balanceBays, hashOnDisconnect := flags(); !balanceBays || !hashOnDisconnect {
				t.Fatalf("after omitting both flags: balanceBays=%v hashOnDisconnect=%v, want both still true", balanceBays, hashOnDisconnect)
			}

			tc.set(t, srv, &off, nil)
			if balanceBays, hashOnDisconnect := flags(); balanceBays || !hashOnDisconnect {
				t.Fatalf("after clearing only balanceBays: balanceBays=%v hashOnDisconnect=%v, want false and true", balanceBays, hashOnDisconnect)
			}
		})
	}
}

func newNativeTestConn(t *testing.T, state *ContainerState) *grpc.ClientConn {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := newGRPCServer(state)
	t.Cleanup(server.Stop)
	go func() { _ = server.Serve(listener) }()
	conn, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
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

func TestConnectGatewayServesHAStatusWithUnreachablePeer(t *testing.T) {
	var states [2]*ContainerState
	for i, id := range []string{"ct1-a", "ct1-b"} {
		states[i] = NewContainerState(ContainerConfig{ServerID: id, Hostname: "fake-ct1-" + id, Tanks: 1, ModulesPerTank: 1, SlotsPerModule: 1, AsicsPerBoard: 4, Seed: 1})
	}
	sim := newHAPair(states[0], states[1], haPairSettings{Update: updateSimConfig{Primary: "ct1-a"}, ClusterID: testHAClusterID, PeerUnreachable: true})
	t.Cleanup(sim.close)
	handler, cleanup, err := newHTTPHandler(states[0], "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	system := mdkv2connect.NewSystemServiceClient(http.DefaultClient, srv.URL, connect.WithGRPCWeb())
	response, err := system.GetHAStatus(context.Background(), connect.NewRequest(&mdk.GetHAStatusRequest{}))
	if err != nil {
		t.Fatalf("GetHAStatus over Connect: %v", err)
	}
	report := response.Msg.GetStatus()
	if report.GetReportingServerId() != "ct1-a" || report.GetPeerServerId() != "ct1-b" || len(report.GetMembers()) != 2 {
		t.Fatalf("unexpected HA report: %v", report)
	}
	local, peer := report.GetMembers()[0], report.GetMembers()[1]
	if !local.GetLocal() || !local.GetFrigateReachable() || local.GetHostname() != "fake-ct1-ct1-a" ||
		local.GetRoleObservation().GetFreshness() != mdk.HAObservationFreshness_HA_OBSERVATION_FRESHNESS_FRESH {
		t.Fatalf("local member = %v", local)
	}
	if peer.GetLocal() || peer.FrigateReachable == nil || peer.GetFrigateReachable() || peer.GetRecovery() != nil ||
		peer.GetHostname() != "fake-ct1-ct1-b" ||
		peer.GetFrigateObservation().GetFreshness() != mdk.HAObservationFreshness_HA_OBSERVATION_FRESHNESS_STALE ||
		peer.GetRoleObservation().GetFreshness() != mdk.HAObservationFreshness_HA_OBSERVATION_FRESHNESS_STALE ||
		peer.GetFrigateObservation().GetLastSuccessfulAt() == nil {
		t.Fatalf("peer member = %v", peer)
	}
}

func TestConnectServesModulePowerFeedReads(t *testing.T) {
	srv, _ := newConnectTestServer(t)
	ctx := context.Background()
	power := mdkv2connect.NewPowerServiceClient(http.DefaultClient, srv.URL)
	feeds, err := power.ListPowerFeeds(ctx, connect.NewRequest(&mdk.ListPowerFeedsRequest{}))
	if err != nil {
		t.Fatalf("ListPowerFeeds: %v", err)
	}
	if !feeds.Msg.GetSnapshotReady() || len(feeds.Msg.GetPowerFeeds()) == 0 || feeds.Msg.GetPowerFeeds()[0].GetPowerFeedId() != "pdu-1-outlet-01" {
		t.Fatalf("unexpected feeds: ready=%v feeds=%d", feeds.Msg.GetSnapshotReady(), len(feeds.Msg.GetPowerFeeds()))
	}
	cycle, err := power.GetActivePowerRecoveryCycle(ctx, connect.NewRequest(&mdk.GetActivePowerRecoveryCycleRequest{}))
	if err != nil {
		t.Fatalf("GetActivePowerRecoveryCycle: %v", err)
	}
	if cycle.Msg.GetCycle() != nil {
		t.Fatalf("unexpected recovery cycle: %v", cycle.Msg.GetCycle())
	}
}
