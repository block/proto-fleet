package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
)

// Board history follows Frigate's HashboardService event and recovery
// evidence reads. Each board has a deterministic fault episode every
// boardEpisodeInterval, offset per slot, so history is stable across restarts
// and different boards show different times.
const (
	boardEpisodeInterval   = 9 * time.Hour
	boardEpisodeBackfill   = 40 * 24 * time.Hour
	boardEpisodeFault      = 18 * time.Minute
	boardHealthSample      = 30 * time.Second
	defaultBoardEventRange = 24 * time.Hour
	maxBoardEventRange     = 7 * 24 * time.Hour
	evidenceRetention      = 30 * 24 * time.Hour
	evidencePerBoard       = 10
	maxEvidenceList        = 100
)

var boardEpisodeCauses = []mdk.HashboardErrorCause{
	mdk.HashboardErrorCause_HASHBOARD_ERROR_CAUSE_ASIC_OVER_TEMPERATURE,
	mdk.HashboardErrorCause_HASHBOARD_ERROR_CAUSE_BOARD_UNDER_VOLTAGE,
	mdk.HashboardErrorCause_HASHBOARD_ERROR_CAUSE_MC3_NO_STACK_RUNNING,
	mdk.HashboardErrorCause_HASHBOARD_ERROR_CAUSE_BOARD_POWER_SUPPLY,
}

var evidenceTriggers = []string{"power_recovery", "ready_recovery", "power_recovery_capture_only"}

func boardHistoryScenario() string { return os.Getenv("CT1_BOARD_HISTORY_SCENARIO") }

type boardEpisode struct {
	index int
	start time.Time
	cause mdk.HashboardErrorCause
}

func (s *ContainerState) boardEpisodeAnchor(board *Hashboard) time.Time {
	position := (board.TankID*100+board.ModuleNum)*3 + board.SlotIndex
	offset := time.Duration(position) * 47 * time.Minute % boardEpisodeInterval
	return s.StartTime.Add(-boardEpisodeBackfill).Add(offset)
}

func (s *ContainerState) boardEpisode(board *Hashboard, index int) boardEpisode {
	return boardEpisode{
		index: index,
		start: s.boardEpisodeAnchor(board).Add(time.Duration(index) * boardEpisodeInterval),
		cause: boardEpisodeCauses[index%len(boardEpisodeCauses)],
	}
}

// boardEpisodes returns the episodes that started before end and could still
// overlap [start, end), oldest first.
func (s *ContainerState) boardEpisodes(board *Hashboard, start, end time.Time) []boardEpisode {
	anchor := s.boardEpisodeAnchor(board)
	if !end.After(anchor) {
		return nil
	}
	var out []boardEpisode
	first := max(0, int(start.Add(-boardEpisodeInterval).Sub(anchor)/boardEpisodeInterval))
	for index := first; ; index++ {
		episode := s.boardEpisode(board, index)
		if !episode.start.Before(end) {
			return out
		}
		out = append(out, episode)
	}
}

// boardEventWindow mirrors Frigate's eventWindow: a missing bound is filled
// from the default span, a span over seven days keeps the end and moves the
// start, and the window is half-open.
func boardEventWindow(window *mdk.TimeRange, now time.Time) (time.Time, time.Time, error) {
	start, end := window.GetStart(), window.GetEnd()
	switch {
	case start == nil && end == nil:
		return now.Add(-defaultBoardEventRange), now, nil
	case end == nil:
		return start.AsTime(), start.AsTime().Add(defaultBoardEventRange), nil
	case start == nil:
		return end.AsTime().Add(-defaultBoardEventRange), end.AsTime(), nil
	}
	from, to := start.AsTime(), end.AsTime()
	if !from.Before(to) {
		return time.Time{}, time.Time{}, status.Error(codes.InvalidArgument, "window start must be before end")
	}
	if to.Sub(from) > maxBoardEventRange {
		from = to.Add(-maxBoardEventRange)
	}
	return from, to, nil
}

func inWindow(at, start, end time.Time) bool { return !at.Before(start) && at.Before(end) }

// boardEvents builds Frigate's merged event list. Like Frigate, error and
// last-shutdown causes aggregate per cause within the window, counting health
// samples, and the current isolation record is appended whatever the window.
func (s *ContainerState) boardEvents(board *Hashboard, start, end time.Time) []*mdk.HashboardEvent {
	now := s.now()
	end = minTime(end, now)
	var events []*mdk.HashboardEvent
	type aggregate struct {
		first, last time.Time
		count       int32
	}
	errors := map[mdk.HashboardErrorCause]*aggregate{}
	for _, episode := range s.boardEpisodes(board, start, end) {
		faultEnd := episode.start.Add(boardEpisodeFault)
		for at := episode.start; !at.After(faultEnd); at = at.Add(boardHealthSample) {
			if !inWindow(at, start, end) {
				continue
			}
			agg := errors[episode.cause]
			if agg == nil {
				agg = &aggregate{first: at}
				errors[episode.cause] = agg
			}
			agg.last, agg.count = at, agg.count+1
		}
		for _, change := range []struct {
			at       time.Time
			from, to mdk.MiningState
		}{
			{episode.start.Add(boardHealthSample), mdk.MiningState_MINING_STATE_MINING, mdk.MiningState_MINING_STATE_ERROR},
			{faultEnd.Add(2 * time.Minute), mdk.MiningState_MINING_STATE_ERROR, mdk.MiningState_MINING_STATE_MINING},
		} {
			if inWindow(change.at, start, end) {
				events = append(events, &mdk.HashboardEvent{
					Time: timestamppb.New(change.at), Type: mdk.HashboardEventType_HASHBOARD_EVENT_TYPE_MINING_STATE_CHANGE,
					FromState: change.from, ToState: change.to,
					Detail: fmt.Sprintf("mining state %s -> %s", change.from, change.to),
				})
			}
		}
		if episode.index%4 == 0 {
			stack := uint32(episode.index % 6)
			detected := episode.start.Add(5 * time.Minute)
			if inWindow(detected, start, end) {
				events = append(events, &mdk.HashboardEvent{
					Time: timestamppb.New(detected), Type: mdk.HashboardEventType_HASHBOARD_EVENT_TYPE_STACK_DROPOUT,
					LastSeen: timestamppb.New(detected.Add(-time.Second)), Count: 1, Stack: &stack,
					Detail: fmt.Sprintf("stack %d stopped hashing; recovered", stack),
				})
			}
		}
		if episode.index%5 == 0 {
			shutdown := faultEnd.Add(time.Minute)
			if inWindow(shutdown, start, end) {
				events = append(events, &mdk.HashboardEvent{
					Time: timestamppb.New(shutdown), Type: mdk.HashboardEventType_HASHBOARD_EVENT_TYPE_LAST_SHUTDOWN,
					Cause: episode.cause, Count: 1, LastSeen: timestamppb.New(shutdown),
					Detail: "historical shutdown cause; not an active fault",
				})
			}
		}
	}
	for cause, agg := range errors {
		events = append(events, &mdk.HashboardEvent{
			Time: timestamppb.New(agg.first), Type: mdk.HashboardEventType_HASHBOARD_EVENT_TYPE_ERROR,
			Cause: cause, Count: agg.count, LastSeen: timestamppb.New(agg.last),
			Detail: fmt.Sprintf("error %s reported %d times", cause, agg.count),
		})
	}
	sort.SliceStable(events, func(i, j int) bool { return events[i].GetTime().AsTime().Before(events[j].GetTime().AsTime()) })
	if board.Isolated {
		events = append(events, &mdk.HashboardEvent{
			Time: timestamppb.New(end), Type: mdk.HashboardEventType_HASHBOARD_EVENT_TYPE_ISOLATION,
			IsolationReason: "isolated by operator", IsolatedBy: "proto-os", Detail: "hashboard is isolated",
		})
	}
	return events
}

func (s *ContainerState) getHashboardEvents(req *mdk.GetHashboardEventsRequest) (*mdk.GetHashboardEventsResponse, error) {
	if req.GetIdentifier() == "" {
		return nil, status.Error(codes.InvalidArgument, "identifier is required")
	}
	if boardHistoryScenario() == "events-unavailable" {
		return nil, status.Error(codes.Unavailable, "hashboard event history requires a configured database")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	board := s.findBoard(req.GetIdentifier())
	if board == nil {
		return nil, notFoundf("unknown hashboard %q", req.GetIdentifier())
	}
	start, end, err := boardEventWindow(req.GetWindow(), s.now())
	if err != nil {
		return nil, err
	}
	return &mdk.GetHashboardEventsResponse{
		MacAddress:      board.MAC,
		Events:          s.boardEvents(board, start, end),
		EffectiveWindow: &mdk.TimeRange{Start: timestamppb.New(start), End: timestamppb.New(end)},
	}, nil
}

// boardEvidence returns the board's retained snapshots, newest first. Every
// third episode captured one; Frigate keeps 30 days and ten per board.
func (s *ContainerState) boardEvidence(board *Hashboard) []*mdk.GetRecoveryEvidenceResponse {
	now := s.now()
	var out []*mdk.GetRecoveryEvidenceResponse
	episodes := s.boardEpisodes(board, now.Add(-evidenceRetention), now)
	for i := len(episodes) - 1; i >= 0 && len(out) < evidencePerBoard; i-- {
		episode := episodes[i]
		captured := episode.start.Add(boardEpisodeFault).Add(30 * time.Second)
		if episode.index%3 != 0 || captured.After(now) || captured.Before(now.Add(-evidenceRetention)) {
			continue
		}
		out = append(out, s.evidenceSnapshot(board, episode, captured))
	}
	return out
}

func (s *ContainerState) evidenceSnapshot(board *Hashboard, episode boardEpisode, captured time.Time) *mdk.GetRecoveryEvidenceResponse {
	digest := sha256.Sum256([]byte(fmt.Sprintf("%s/evidence/%d", board.MAC, episode.index)))
	id := fmt.Sprintf("%x-%x-%x-%x-%x", digest[0:4], digest[4:6], digest[6:8], digest[8:10], digest[10:16])
	// Only every third episode captures evidence, so rotate by capture.
	trigger := evidenceTriggers[(episode.index/3)%len(evidenceTriggers)]
	truncated := episode.index%7 == 0
	stack := episode.index % 6
	dropoutDetected := episode.start.Add(5 * time.Minute)
	document := map[string]any{
		"version":     1,
		"mac_address": board.MAC,
		"server_id":   s.ServerID,
		"captured_at": captured.UTC().Format(time.RFC3339),
		"trigger":     trigger,
		"health": map[string]any{
			"board_voltage_v": 12.1, "board_current_a": 31.4, "board_temperature_c": 71.5,
			"hashrail_ok": episode.cause != mdk.HashboardErrorCause_HASHBOARD_ERROR_CAUSE_BOARD_UNDER_VOLTAGE, "refclks_ok": true,
		},
		"asic_frames": []map[string]any{{"asic": 4, "voltage_mv": 312, "temperature_dc": 812, "frequency_mhz": 500, "pass_rate_bp": 9800, "hash_rate_ghs10": 9000}},
		"can_status":  map[string]any{"tx_errors": 0, "rx_errors": 2, "bus_off": false},
		"events": map[string]any{"stack_dropouts": []map[string]any{{
			"stack": stack, "detected_at": dropoutDetected.UTC().Format(time.RFC3339), "cleared_at": dropoutDetected.Add(4 * time.Minute).UTC().Format(time.RFC3339),
		}}},
		"logs": []string{fmt.Sprintf("hashboard %s: %s", board.MAC, episode.cause), fmt.Sprintf("hashboard %s: recovery %s", board.MAC, trigger)},
		"sources": map[string]any{
			"health":      map[string]any{"status": "ok", "count": 1},
			"asic_frames": map[string]any{"status": "ok", "count": 1},
			"can_status":  map[string]any{"status": "ok", "count": 1},
			"logs":        map[string]any{"status": "ok", "count": 2},
		},
	}
	if truncated {
		document["omitted"] = []string{"asic_frames"}
		document["sources"].(map[string]any)["asic_frames"] = map[string]any{"status": "error", "detail": "frame read timed out"}
	}
	payload, _ := json.Marshal(document)
	return &mdk.GetRecoveryEvidenceResponse{
		Summary: &mdk.RecoveryEvidenceSummary{
			EvidenceId: id, EpisodeId: fmt.Sprintf("episode-%d", episode.index), MacAddress: board.MAC,
			Trigger: trigger, Causes: []mdk.HashboardErrorCause{episode.cause}, CapturedAt: timestamppb.New(captured),
			CapturedBy: s.ServerID, Truncated: truncated, PayloadBytes: uint32(len(payload)),
		},
		EvidenceJson: string(payload),
	}
}

func (s *ContainerState) listRecoveryEvidence(req *mdk.ListRecoveryEvidenceRequest) (*mdk.ListRecoveryEvidenceResponse, error) {
	if boardHistoryScenario() == "evidence-unavailable" {
		return nil, status.Error(codes.Unavailable, "recovery evidence requires a configured database")
	}
	limit := int(req.GetLimit())
	if limit <= 0 || limit > maxEvidenceList {
		limit = maxEvidenceList
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	boards := make([]*Hashboard, 0, len(s.boardsByMAC))
	if req.GetIdentifier() != "" {
		board := s.findBoard(req.GetIdentifier())
		if board == nil {
			return nil, notFoundf("unknown hashboard %q", req.GetIdentifier())
		}
		boards = append(boards, board)
	} else {
		for _, board := range s.boardsByMAC {
			boards = append(boards, board)
		}
	}
	var all []*mdk.RecoveryEvidenceSummary
	for _, board := range boards {
		for _, snapshot := range s.boardEvidence(board) {
			if req.GetEpisodeId() == "" || snapshot.GetSummary().GetEpisodeId() == req.GetEpisodeId() {
				all = append(all, snapshot.GetSummary())
			}
		}
	}
	sort.Slice(all, func(i, j int) bool {
		a, b := all[i].GetCapturedAt().AsTime(), all[j].GetCapturedAt().AsTime()
		if !a.Equal(b) {
			return a.After(b)
		}
		return all[i].GetEvidenceId() > all[j].GetEvidenceId()
	})
	return &mdk.ListRecoveryEvidenceResponse{Evidence: all[:min(limit, len(all))]}, nil
}

func (s *ContainerState) getRecoveryEvidence(req *mdk.GetRecoveryEvidenceRequest) (*mdk.GetRecoveryEvidenceResponse, error) {
	if req.GetEvidenceId() == "" {
		return nil, status.Error(codes.InvalidArgument, "evidence_id is required")
	}
	if boardHistoryScenario() == "evidence-unavailable" {
		return nil, status.Error(codes.Unavailable, "recovery evidence requires a configured database")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, board := range s.boardsByMAC {
		for _, snapshot := range s.boardEvidence(board) {
			if snapshot.GetSummary().GetEvidenceId() == req.GetEvidenceId() {
				return snapshot, nil
			}
		}
	}
	return nil, notFoundf("recovery evidence %q not found", req.GetEvidenceId())
}

func (a *grpcAPI) GetHashboardEvents(_ context.Context, req *mdk.GetHashboardEventsRequest) (*mdk.GetHashboardEventsResponse, error) {
	return a.state.getHashboardEvents(req)
}

func (a *grpcAPI) ListRecoveryEvidence(_ context.Context, req *mdk.ListRecoveryEvidenceRequest) (*mdk.ListRecoveryEvidenceResponse, error) {
	return a.state.listRecoveryEvidence(req)
}

func (a *grpcAPI) GetRecoveryEvidence(_ context.Context, req *mdk.GetRecoveryEvidenceRequest) (*mdk.GetRecoveryEvidenceResponse, error) {
	return a.state.getRecoveryEvidence(req)
}

func (a *connectAPI) GetHashboardEvents(ctx context.Context, req *connect.Request[mdk.GetHashboardEventsRequest]) (*connect.Response[mdk.GetHashboardEventsResponse], error) {
	return connectResult(a.native.GetHashboardEvents(ctx, req.Msg))
}

func (a *connectAPI) ListRecoveryEvidence(ctx context.Context, req *connect.Request[mdk.ListRecoveryEvidenceRequest]) (*connect.Response[mdk.ListRecoveryEvidenceResponse], error) {
	return connectResult(a.native.ListRecoveryEvidence(ctx, req.Msg))
}

func (a *connectAPI) GetRecoveryEvidence(ctx context.Context, req *connect.Request[mdk.GetRecoveryEvidenceRequest]) (*connect.Response[mdk.GetRecoveryEvidenceResponse], error) {
	return connectResult(a.native.GetRecoveryEvidence(ctx, req.Msg))
}
