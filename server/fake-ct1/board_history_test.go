package main

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	"github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2/mdkv2connect"
)

func firstBoard(t *testing.T, state *ContainerState) *Hashboard {
	t.Helper()
	for _, module := range state.Modules {
		for _, slot := range module.Slots {
			if slot.Occupied && slot.Board != nil {
				return slot.Board
			}
		}
	}
	t.Fatal("no occupied slot")
	return nil
}

func TestBoardEventsFollowFrigateWindowsAndAggregation(t *testing.T) {
	srv, state := newConnectTestServer(t)
	board := firstBoard(t, state)
	client := mdkv2connect.NewHashboardServiceClient(http.DefaultClient, srv.URL, connect.WithGRPCWeb())
	ctx := context.Background()

	resp, err := client.GetHashboardEvents(ctx, connect.NewRequest(&mdk.GetHashboardEventsRequest{Identifier: board.MAC}))
	if err != nil {
		t.Fatalf("default window: %v", err)
	}
	window := resp.Msg.GetEffectiveWindow()
	if got := window.GetEnd().AsTime().Sub(window.GetStart().AsTime()); got != 24*time.Hour || !window.GetEnd().AsTime().Equal(state.StartTime) {
		t.Fatalf("default window = %v ending %v, want 24h ending now", got, window.GetEnd().AsTime())
	}
	if resp.Msg.GetMacAddress() != board.MAC || len(resp.Msg.GetEvents()) == 0 {
		t.Fatalf("expected events for %s, got %d", board.MAC, len(resp.Msg.GetEvents()))
	}
	errorsByCause := map[mdk.HashboardErrorCause]int{}
	var previous time.Time
	for _, event := range resp.Msg.GetEvents() {
		at := event.GetTime().AsTime()
		if at.Before(previous) || at.Before(window.GetStart().AsTime()) || !at.Before(window.GetEnd().AsTime()) {
			t.Fatalf("event %v out of order or outside the window", event)
		}
		previous = at
		if event.GetType() == mdk.HashboardEventType_HASHBOARD_EVENT_TYPE_ERROR {
			errorsByCause[event.GetCause()]++
			if event.GetCount() < 1 || event.GetLastSeen().AsTime().Before(at) {
				t.Fatalf("aggregated error needs a count and last seen after first seen: %v", event)
			}
		}
	}
	for cause, rows := range errorsByCause {
		if rows != 1 {
			t.Fatalf("cause %s has %d rows, want one aggregated row per window", cause, rows)
		}
	}

	week, err := client.GetHashboardEvents(ctx, connect.NewRequest(&mdk.GetHashboardEventsRequest{Identifier: board.MAC, Window: &mdk.TimeRange{
		Start: timestamppb.New(state.StartTime.Add(-30 * 24 * time.Hour)), End: timestamppb.New(state.StartTime),
	}}))
	if err != nil {
		t.Fatalf("long window: %v", err)
	}
	if got := week.Msg.GetEffectiveWindow().GetStart().AsTime(); !got.Equal(state.StartTime.Add(-7 * 24 * time.Hour)) {
		t.Fatalf("a window over seven days keeps its end and starts seven days before it, got %v", got)
	}

	_, err = client.GetHashboardEvents(ctx, connect.NewRequest(&mdk.GetHashboardEventsRequest{Identifier: board.MAC, Window: &mdk.TimeRange{
		Start: timestamppb.New(state.StartTime), End: timestamppb.New(state.StartTime),
	}}))
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("empty window: got %v, want InvalidArgument", err)
	}
	_, err = client.GetHashboardEvents(ctx, connect.NewRequest(&mdk.GetHashboardEventsRequest{Identifier: "02:00:00:00:00:99"}))
	if connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("unknown board: got %v, want NotFound", err)
	}

	state.mu.Lock()
	board.Isolated = true
	state.mu.Unlock()
	isolated, err := client.GetHashboardEvents(ctx, connect.NewRequest(&mdk.GetHashboardEventsRequest{Identifier: board.MAC}))
	if err != nil {
		t.Fatalf("isolated board: %v", err)
	}
	events := isolated.Msg.GetEvents()
	if last := events[len(events)-1]; last.GetType() != mdk.HashboardEventType_HASHBOARD_EVENT_TYPE_ISOLATION {
		t.Fatalf("the current isolation record is appended last, got %v", last.GetType())
	}
}

func TestRecoveryEvidenceIsRetainedAndReadable(t *testing.T) {
	srv, state := newConnectTestServer(t)
	board := firstBoard(t, state)
	client := mdkv2connect.NewHashboardServiceClient(http.DefaultClient, srv.URL, connect.WithGRPCWeb())
	ctx := context.Background()

	list, err := client.ListRecoveryEvidence(ctx, connect.NewRequest(&mdk.ListRecoveryEvidenceRequest{Identifier: board.MAC}))
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	evidence := list.Msg.GetEvidence()
	if len(evidence) == 0 || len(evidence) > evidencePerBoard {
		t.Fatalf("got %d snapshots, want 1..%d", len(evidence), evidencePerBoard)
	}
	for i, summary := range evidence {
		captured := summary.GetCapturedAt().AsTime()
		if captured.Before(state.StartTime.Add(-evidenceRetention)) || captured.After(state.StartTime) {
			t.Fatalf("snapshot %s captured %v outside retention", summary.GetEvidenceId(), captured)
		}
		if i > 0 && captured.After(evidence[i-1].GetCapturedAt().AsTime()) {
			t.Fatal("snapshots are newest first")
		}
		if summary.GetMacAddress() != board.MAC || summary.GetEpisodeId() == "" || len(summary.GetCauses()) == 0 {
			t.Fatalf("incomplete summary %v", summary)
		}
	}

	detail, err := client.GetRecoveryEvidence(ctx, connect.NewRequest(&mdk.GetRecoveryEvidenceRequest{EvidenceId: evidence[0].GetEvidenceId()}))
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	var document struct {
		Version int                       `json:"version"`
		Sources map[string]map[string]any `json:"sources"`
	}
	if err := json.Unmarshal([]byte(detail.Msg.GetEvidenceJson()), &document); err != nil || document.Version != 1 || len(document.Sources) == 0 {
		t.Fatalf("evidence payload %q: %v", detail.Msg.GetEvidenceJson(), err)
	}
	if detail.Msg.GetSummary().GetPayloadBytes() != uint32(len(detail.Msg.GetEvidenceJson())) {
		t.Fatal("payload_bytes is the stored payload size")
	}

	_, err = client.GetRecoveryEvidence(ctx, connect.NewRequest(&mdk.GetRecoveryEvidenceRequest{EvidenceId: "missing"}))
	if connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("unknown evidence: got %v, want NotFound", err)
	}

	t.Setenv("CT1_BOARD_HISTORY_SCENARIO", "evidence-unavailable")
	_, err = client.ListRecoveryEvidence(ctx, connect.NewRequest(&mdk.ListRecoveryEvidenceRequest{Identifier: board.MAC}))
	if connect.CodeOf(err) != connect.CodeUnavailable {
		t.Fatalf("scenario: got %v, want Unavailable", err)
	}
}
