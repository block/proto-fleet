package main

import (
	"net"
	"net/http"
	"testing"
	"time"

	"connectrpc.com/connect"
	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	"github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2/mdkv2connect"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

func mappingConfig() *mdk.FeedMappingRunConfig {
	return &mdk.FeedMappingRunConfig{ConfirmPowerCycle: true, RestoreAllOn: true}
}

func unlocatedCount(s *ContainerState) int {
	count := 0
	for _, tank := range s.containerMapDTO().ContainerMap.Tanks {
		if tank.ID == 0 {
			for _, module := range tank.Modules {
				count += len(module.Slots)
			}
		}
	}
	return count
}

func TestFeedMappingLifecycleAcrossTransports(t *testing.T) {
	srv, s := newConnectTestServer(t)
	s.Config.UnlocatedBoards = -1
	client := mdkv2connect.NewFeedMappingServiceClient(http.DefaultClient, srv.URL)
	now := s.StartTime
	s.now = func() time.Time { return now }
	advance := func(steps int) {
		s.mu.Lock()
		defer s.mu.Unlock()
		now = s.StartTime.Add(time.Duration(steps) * mappingPhaseDuration)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := newGRPCServer(s)
	t.Cleanup(server.Stop)
	go func() { _ = server.Serve(listener) }()
	conn, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	native := mdk.NewFeedMappingServiceClient(conn)
	ctx := t.Context()

	report, err := client.GetFeedMappingReport(ctx, connect.NewRequest(&mdk.GetFeedMappingReportRequest{}))
	if err != nil || report.Msg.GetWiring().GetMode() != "manual" || report.Msg.GetReport() == nil {
		t.Fatalf("report: %v, %v", report, err)
	}
	plan, err := client.PlanFeedMappingRun(ctx, connect.NewRequest(&mdk.PlanFeedMappingRunRequest{Config: mappingConfig()}))
	if err != nil || !plan.Msg.GetPlan().GetDryRun() || len(plan.Msg.Plan.Feeds) != len(s.Modules) {
		t.Fatalf("plan: %v, %v", plan, err)
	}
	if s.mappingActive() || unlocatedCount(s) != s.BoardCount() {
		t.Fatal("planning mutated state")
	}
	_, err = native.GetActiveFeedMappingRun(ctx, &mdk.GetActiveFeedMappingRunRequest{})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("active before start: %v", err)
	}
	started, err := client.StartFeedMappingRun(ctx, connect.NewRequest(&mdk.StartFeedMappingRunRequest{Config: mappingConfig()}))
	if err != nil {
		t.Fatal(err)
	}
	id := started.Msg.RunId
	_, err = client.StartFeedMappingRun(ctx, connect.NewRequest(&mdk.StartFeedMappingRunRequest{Config: mappingConfig()}))
	if connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("duplicate start: %v", err)
	}
	_, err = mdk.NewMiningServiceClient(conn).StartMining(ctx, &mdk.StartMiningRequest{})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("conflicting mining write: %v", err)
	}
	for _, phase := range []struct {
		step int
		name string
	}{{1, "ISOLATING"}, {2, "WAITING_ONLINE"}, {3, "WAITING_OFFLINE"}} {
		advance(phase.step)
		active, err := native.GetActiveFeedMappingRun(ctx, &mdk.GetActiveFeedMappingRunRequest{})
		if err != nil || active.GetRun().GetRunId() != id || active.GetRun().GetPhase() != phase.name {
			t.Fatalf("phase %s: %v, %v", phase.name, active, err)
		}
		if _, err := mdk.NewHashboardServiceClient(conn).GetContainerMap(ctx, &mdk.GetContainerMapRequest{}); err != nil {
			t.Fatalf("topology during %s: %v", phase.name, err)
		}
		if phase.step == 1 {
			mining, err := mdk.NewMiningServiceClient(conn).GetMiningStatus(ctx, &mdk.GetMiningStatusRequest{})
			if err != nil || mining.GetHashrate() != 0 {
				t.Fatalf("mining during isolation: %v, %v", mining, err)
			}
			feeds, err := mdk.NewPowerServiceClient(conn).ListPowerFeeds(ctx, &mdk.ListPowerFeedsRequest{})
			if err != nil {
				t.Fatal(err)
			}
			for _, feed := range feeds.PowerFeeds {
				if feed.GetState() != mdk.PowerFeedState_POWER_FEED_STATE_DISABLED || feed.GetPowerW() != 0 {
					t.Fatalf("isolated feed: %v", feed)
				}
			}
		}
	}
	// Jump past all phases: completion must not depend on polling frequency.
	advance(4 + len(plan.Msg.Plan.Feeds)*2)
	finished, err := native.GetFeedMappingRun(ctx, &mdk.GetFeedMappingRunRequest{RunId: id})
	if err != nil || finished.GetRun().GetStatus() != mdk.FeedMappingRunStatus_FEED_MAPPING_RUN_STATUS_COMPLETED {
		t.Fatalf("complete: %v, %v", finished, err)
	}
	if unlocatedCount(s) != 0 {
		t.Fatal("completed mapping did not assign locations")
	}
	for _, feed := range finished.Run.Feeds {
		if feed.Status != mdk.FeedMappingFeedStatus_FEED_MAPPING_FEED_STATUS_COMPLETED || len(feed.Boards) != s.Config.SlotsPerModule {
			t.Fatalf("feed result: %v", feed)
		}
	}
	mining, err := mdk.NewMiningServiceClient(conn).GetMiningStatus(ctx, &mdk.GetMiningStatusRequest{})
	if err != nil || mining.GetHashrate() <= 0 {
		t.Fatalf("restored mining: %v, %v", mining, err)
	}
	report, err = client.GetFeedMappingReport(ctx, connect.NewRequest(&mdk.GetFeedMappingReportRequest{}))
	if err != nil || len(report.Msg.Report.Feeds) != len(plan.Msg.Plan.Feeds) {
		t.Fatalf("mapped report: %v, %v", report, err)
	}
}

func TestFeedMappingValidationAndCancellation(t *testing.T) {
	_, s := newConnectTestServer(t)
	s.Config.UnlocatedBoards = -1
	api := &grpcAPI{state: s}
	ctx := t.Context()
	for _, cfg := range []*mdk.FeedMappingRunConfig{nil, {RestoreAllOn: true}, {ConfirmPowerCycle: true}, {ConfirmPowerCycle: true, RestoreAllOn: true, FeedIds: []string{"unknown"}}} {
		_, err := api.StartFeedMappingRun(ctx, &mdk.StartFeedMappingRunRequest{Config: cfg})
		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("invalid config accepted: %v, %v", cfg, err)
		}
	}
	started, err := api.StartFeedMappingRun(ctx, &mdk.StartFeedMappingRunRequest{Config: mappingConfig()})
	if err != nil {
		t.Fatal(err)
	}
	canceled, err := api.CancelFeedMappingRun(ctx, &mdk.CancelFeedMappingRunRequest{RunId: started.RunId})
	if err != nil || canceled.GetRun().GetPhase() != "CANCELING" {
		t.Fatalf("cancel: %v, %v", canceled, err)
	}
	s.advance(s.StartTime.Add(mappingPhaseDuration))
	if s.mappingActive() || s.feedMapping.run.Status != mdk.FeedMappingRunStatus_FEED_MAPPING_RUN_STATUS_CANCELED {
		t.Fatal("cancel did not finish")
	}
	if unlocatedCount(s) != s.BoardCount() {
		t.Fatal("early cancellation assigned untested boards")
	}
	for _, b := range s.eachBoard() {
		if !b.isMining() {
			t.Fatal("cancel did not restore mining")
		}
	}
	_, err = api.GetFeedMappingRun(ctx, &mdk.GetFeedMappingRunRequest{RunId: "unknown"})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("unknown run: %v", err)
	}
}

func TestFeedMappingSubsetPreservesUnmappedBoards(t *testing.T) {
	_, s := newConnectTestServer(t)
	s.Config.UnlocatedBoards = 7
	api := &grpcAPI{state: s}
	cfg := mappingConfig()
	cfg.FeedIds = []string{s.Modules[1].Slots[0].PowerFeedID}
	_, err := api.StartFeedMappingRun(t.Context(), &mdk.StartFeedMappingRunRequest{Config: cfg})
	if err != nil {
		t.Fatal(err)
	}
	s.advance(s.StartTime.Add(6 * mappingPhaseDuration))
	if unlocatedCount(s) != 4 {
		t.Fatalf("unlocated = %d, want 4", unlocatedCount(s))
	}
}
