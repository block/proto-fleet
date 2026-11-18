package main

import (
	"context"
	"fmt"
	"net/http"
	"time"

	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Mapping uses the shared simulation clock. Each phase takes two seconds;
// no background worker or status read is required to complete a run.
const mappingPhaseDuration = 2 * time.Second

type simulatedFeedMapping struct {
	run         *mdk.FeedMappingRun
	sequence    int
	boardStates map[string]string
	mappedFeeds map[string]bool
	canceledAt  time.Time
}

func (s *ContainerState) mappingActive() bool {
	return s.feedMapping.run.GetStatus() == mdk.FeedMappingRunStatus_FEED_MAPPING_RUN_STATUS_RUNNING
}

func (s *ContainerState) mappingWriteGuard() error {
	if s.mappingActive() {
		return &apiError{http.StatusPreconditionFailed, codes.FailedPrecondition, "hashboard mapping is running; wait until mapping completes"}
	}
	return nil
}

func (s *ContainerState) mappingPlan(config *mdk.FeedMappingRunConfig) (*mdk.FeedMappingRun, error) {
	available := map[string]bool{}
	var ids []string
	for _, module := range s.Modules {
		for _, slot := range module.Slots {
			if id := slot.PowerFeedID; id != "" && !available[id] {
				available[id] = true
				ids = append(ids, id)
			}
		}
	}
	if len(config.GetFeedIds()) > 0 {
		ids = config.GetFeedIds()
	}
	run := &mdk.FeedMappingRun{RunId: "dry-run", Status: mdk.FeedMappingRunStatus_FEED_MAPPING_RUN_STATUS_PLANNED, Phase: "PLAN", DryRun: true, RestoreAllOn: config.GetRestoreAllOn()}
	seen := map[string]bool{}
	for _, id := range ids {
		if !available[id] {
			return nil, status.Errorf(codes.InvalidArgument, "unknown power feed %q", id)
		}
		if !seen[id] {
			seen[id] = true
			run.Feeds = append(run.Feeds, &mdk.FeedMappingFeedResult{FeedId: id, Status: mdk.FeedMappingFeedStatus_FEED_MAPPING_FEED_STATUS_PENDING})
		}
	}
	if len(run.Feeds) == 0 {
		return nil, status.Error(codes.FailedPrecondition, "no power feeds configured")
	}
	return run, nil
}

// Mapping disables every feed during isolation, including feeds outside the
// selected set. Restoration enables every feed; board mining intent is retained.
func (s *ContainerState) mappingFeedEnabled(id string) bool {
	run := s.feedMapping.run
	if run == nil || run.DryRun {
		return true
	}
	switch run.Phase {
	case "STARTING", "RESTORING", "CANCELING":
		return true
	case "COMPLETED", "CANCELED":
		return run.RestoreAllOn
	case "WAITING_ONLINE":
		return run.CurrentFeedId == id
	default:
		return false
	}
}

func (s *ContainerState) advanceFeedMapping(now time.Time) {
	if !s.mappingActive() {
		return
	}
	m := &s.feedMapping
	run := m.run
	step := int(now.Sub(run.StartedAt.AsTime()) / mappingPhaseDuration)
	run.UpdatedAt = timestamppb.New(now)
	run.CurrentFeedId = ""
	if !m.canceledAt.IsZero() {
		run.Phase = "CANCELING"
		if now.Sub(m.canceledAt) >= mappingPhaseDuration {
			run.Phase = "CANCELED"
			run.Status = mdk.FeedMappingRunStatus_FEED_MAPPING_RUN_STATUS_CANCELED
		}
	} else {
		switch {
		case step < 1:
			run.Phase = "STARTING"
		case step < 2:
			run.Phase = "ISOLATING"
		case step < 2+len(run.Feeds)*2:
			index := (step - 2) / 2
			run.CurrentFeedId = run.Feeds[index].FeedId
			run.Phase = "WAITING_ONLINE"
			if step%2 == 1 {
				run.Phase = "WAITING_OFFLINE"
			}
		case step == 2+len(run.Feeds)*2:
			run.Phase = "PERSISTING"
		case step == 3+len(run.Feeds)*2:
			run.Phase = "RESTORING"
		default:
			run.Phase = "COMPLETED"
			run.Status = mdk.FeedMappingRunStatus_FEED_MAPPING_RUN_STATUS_COMPLETED
		}
		for i, feed := range run.Feeds {
			start := 2 + i*2
			if step >= start && feed.StartedAt == nil {
				feed.StartedAt = timestamppb.New(run.StartedAt.AsTime().Add(time.Duration(start) * mappingPhaseDuration))
				feed.Status = mdk.FeedMappingFeedStatus_FEED_MAPPING_FEED_STATUS_RUNNING
			}
			if step >= start+2 && feed.CompletedAt == nil {
				feed.Status = mdk.FeedMappingFeedStatus_FEED_MAPPING_FEED_STATUS_COMPLETED
				feed.CompletedAt = timestamppb.New(run.StartedAt.AsTime().Add(time.Duration(start+2) * mappingPhaseDuration))
				m.mappedFeeds[feed.FeedId] = true
				for _, board := range s.eachBoard() {
					if board.PowerFeedID == feed.FeedId {
						feed.Boards = append(feed.Boards, &mdk.FeedMappingDetectedBoard{MacAddress: board.MAC, Serial: board.Serial, HwRevision: board.HWRevision, FirmwareVersion: board.FirmwareVersion, FirstSeenAt: feed.StartedAt, LastSeenAt: feed.CompletedAt, VerifiedAt: feed.CompletedAt, DetectedBy: s.ServerID})
					}
				}
			}
		}
	}
	if !s.mappingActive() {
		run.CompletedAt = timestamppb.New(now)
	}
	for _, board := range s.eachBoard() {
		if s.mappingFeedEnabled(board.PowerFeedID) {
			board.State = m.boardStates[board.MAC]
		} else {
			board.State = miningStateDisconnected
		}
	}
}

func (a *grpcAPI) PlanFeedMappingRun(_ context.Context, in *mdk.PlanFeedMappingRunRequest) (*mdk.PlanFeedMappingRunResponse, error) {
	a.state.mu.Lock()
	defer a.state.mu.Unlock()
	plan, err := a.state.mappingPlan(in.GetConfig())
	if err != nil {
		return nil, err
	}
	return &mdk.PlanFeedMappingRunResponse{Plan: plan}, nil
}

func (a *grpcAPI) StartFeedMappingRun(_ context.Context, in *mdk.StartFeedMappingRunRequest) (*mdk.StartFeedMappingRunResponse, error) {
	s := a.state
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.mappingWriteGuard(); err != nil {
		return nil, err
	}
	config := in.GetConfig()
	run, err := s.mappingPlan(config)
	if err != nil {
		return nil, err
	}
	if config.GetDryRun() {
		return &mdk.StartFeedMappingRunResponse{RunId: run.RunId, Run: run}, nil
	}
	if !config.GetConfirmPowerCycle() {
		return nil, status.Error(codes.InvalidArgument, "confirm_power_cycle is required")
	}
	// The simulator supports the restoration policy used by ProtoOS. It does
	// not expose manual feed controls to recover from a leave-off run.
	if !config.GetRestoreAllOn() {
		return nil, status.Error(codes.InvalidArgument, "simulator requires restore_all_on")
	}
	if s.ha != nil {
		return nil, status.Error(codes.Unimplemented, "simulator mapping requires a standalone controller")
	}
	if s.miningGroups != nil && s.miningGroups.capability.Enabled {
		return nil, status.Error(codes.FailedPrecondition, "simulator mapping requires legacy mining mode")
	}
	m := &s.feedMapping
	m.sequence++
	m.run = run
	m.boardStates = map[string]string{}
	if m.mappedFeeds == nil {
		m.mappedFeeds = map[string]bool{}
	}
	m.canceledAt = time.Time{}
	for _, board := range s.eachBoard() {
		m.boardStates[board.MAC] = board.State
	}
	run.RunId = fmt.Sprintf("mapping-%d", m.sequence)
	run.DryRun = false
	run.Status = mdk.FeedMappingRunStatus_FEED_MAPPING_RUN_STATUS_RUNNING
	run.Phase = "STARTING"
	run.StartedAt = timestamppb.New(s.now())
	run.UpdatedAt = run.StartedAt
	return &mdk.StartFeedMappingRunResponse{RunId: run.RunId, Run: proto.Clone(run).(*mdk.FeedMappingRun)}, nil
}

func (a *grpcAPI) GetActiveFeedMappingRun(context.Context, *mdk.GetActiveFeedMappingRunRequest) (*mdk.GetActiveFeedMappingRunResponse, error) {
	a.state.mu.RLock()
	defer a.state.mu.RUnlock()
	if !a.state.mappingActive() {
		return nil, status.Error(codes.NotFound, "no active mapping run")
	}
	return &mdk.GetActiveFeedMappingRunResponse{Run: proto.Clone(a.state.feedMapping.run).(*mdk.FeedMappingRun)}, nil
}

func (a *grpcAPI) GetFeedMappingRun(_ context.Context, in *mdk.GetFeedMappingRunRequest) (*mdk.GetFeedMappingRunResponse, error) {
	a.state.mu.RLock()
	defer a.state.mu.RUnlock()
	run := a.state.feedMapping.run
	if run == nil || run.RunId != in.GetRunId() {
		return nil, status.Error(codes.NotFound, "mapping run not found")
	}
	return &mdk.GetFeedMappingRunResponse{Run: proto.Clone(run).(*mdk.FeedMappingRun)}, nil
}

func (a *grpcAPI) CancelFeedMappingRun(_ context.Context, in *mdk.CancelFeedMappingRunRequest) (*mdk.CancelFeedMappingRunResponse, error) {
	s := a.state
	s.mu.Lock()
	defer s.mu.Unlock()
	run := s.feedMapping.run
	if run == nil || run.RunId != in.GetRunId() {
		return nil, status.Error(codes.NotFound, "mapping run not found")
	}
	if s.mappingActive() && s.feedMapping.canceledAt.IsZero() {
		s.feedMapping.canceledAt = s.now()
		s.advanceFeedMapping(s.now())
	}
	return &mdk.CancelFeedMappingRunResponse{Run: proto.Clone(run).(*mdk.FeedMappingRun)}, nil
}

func (a *grpcAPI) GetFeedMappingReport(context.Context, *mdk.GetFeedMappingReportRequest) (*mdk.GetFeedMappingReportResponse, error) {
	s := a.state
	s.mu.RLock()
	defer s.mu.RUnlock()
	report := &mdk.FeedMappingRun{RunId: "report", Status: mdk.FeedMappingRunStatus_FEED_MAPPING_RUN_STATUS_COMPLETED}
	// Report only assigned locations, including results persisted before cancel.
	feeds := map[string]*mdk.FeedMappingFeedResult{}
	for _, tank := range s.containerMapDTO().ContainerMap.Tanks {
		if tank.ID == 0 {
			continue
		}
		for _, module := range tank.Modules {
			for _, slot := range module.Slots {
				if slot.Hashboard == nil {
					continue
				}
				feed := feeds[slot.PowerFeedID]
				if feed == nil {
					feed = &mdk.FeedMappingFeedResult{FeedId: slot.PowerFeedID, Status: mdk.FeedMappingFeedStatus_FEED_MAPPING_FEED_STATUS_COMPLETED}
					feeds[slot.PowerFeedID] = feed
					report.Feeds = append(report.Feeds, feed)
				}
				feed.Boards = append(feed.Boards, &mdk.FeedMappingDetectedBoard{MacAddress: slot.Hashboard.MACAddress, Serial: slot.Hashboard.Serial})
			}
		}
	}
	return &mdk.GetFeedMappingReportResponse{Report: report, Wiring: &mdk.WiringReport{Mode: "manual"}}, nil
}
