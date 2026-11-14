package main

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"connectrpc.com/connect"
	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	"github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2/mdkv2connect"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// readLogTail reads until the stream stays quiet, like the ProtoOS client.
func readLogTail(t *testing.T, client mdkv2connect.SystemServiceClient, req *mdk.GetLogsRequest) ([]*mdk.GetLogsResponse, error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream, err := client.GetLogs(ctx, connect.NewRequest(req))
	if err != nil {
		return nil, err
	}
	type result struct {
		entry *mdk.GetLogsResponse
		ok    bool
	}
	results := make(chan result)
	go func() {
		for stream.Receive() {
			results <- result{stream.Msg(), true}
		}
		close(results)
	}()
	var entries []*mdk.GetLogsResponse
	for {
		select {
		case r, open := <-results:
			if !open || !r.ok {
				return entries, stream.Err()
			}
			entries = append(entries, r.entry)
		case <-time.After(300 * time.Millisecond):
			return entries, nil
		}
	}
}

func TestGetLogsReturnsBoundedFilteredTail(t *testing.T) {
	srv, _ := newConnectTestServer(t)
	system := mdkv2connect.NewSystemServiceClient(http.DefaultClient, srv.URL)

	entries, err := readLogTail(t, system, &mdk.GetLogsRequest{Tail: 50, Level: mdk.LogLevel_LOG_LEVEL_WARNING, Component: "pool"})
	if err != nil {
		t.Fatalf("GetLogs: %v", err)
	}
	if len(entries) == 0 || len(entries) > 50 {
		t.Fatalf("entries = %d, want 1..50", len(entries))
	}
	for i, entry := range entries {
		if entry.GetComponent() != "pool" || entry.GetLevel() < mdk.LogLevel_LOG_LEVEL_WARNING {
			t.Fatalf("entry %d = %s/%s, want pool at warning or above", i, entry.GetComponent(), entry.GetLevel())
		}
		if i > 0 && entry.GetTimestamp().AsTime().Before(entries[i-1].GetTimestamp().AsTime()) {
			t.Fatalf("entries are not oldest first at %d", i)
		}
	}

	all, err := readLogTail(t, system, &mdk.GetLogsRequest{Tail: 5000, Level: mdk.LogLevel_LOG_LEVEL_DEBUG})
	if err != nil {
		t.Fatalf("GetLogs: %v", err)
	}
	if len(all) != maxLogTail {
		t.Fatalf("debug tail = %d, want the %d cap", len(all), maxLogTail)
	}
	defaults, err := readLogTail(t, system, &mdk.GetLogsRequest{})
	if err != nil {
		t.Fatalf("GetLogs: %v", err)
	}
	if len(defaults) != defaultLogTail {
		t.Fatalf("default tail = %d, want %d", len(defaults), defaultLogTail)
	}
}

func TestGetLogsKeepsOnlyTheRing(t *testing.T) {
	state := newContainerState(testConfig(), time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC))
	entries, latest := state.logTail(&mdk.GetLogsRequest{Tail: maxLogTail, Level: mdk.LogLevel_LOG_LEVEL_ERROR}, state.StartTime)
	if len(entries) == 0 || len(entries) >= maxLogTail {
		t.Fatalf("error entries = %d, want a partial ring", len(entries))
	}
	if oldest := entries[0].sequence; oldest <= latest-logRingCapacity {
		t.Fatalf("oldest sequence %d predates the ring ending at %d", oldest, latest)
	}
}

func TestGetLogsScenarios(t *testing.T) {
	srv, _ := newConnectTestServer(t)
	system := mdkv2connect.NewSystemServiceClient(http.DefaultClient, srv.URL)

	t.Setenv("CT1_LOG_SCENARIO", "logs-unavailable")
	if _, err := readLogTail(t, system, &mdk.GetLogsRequest{Tail: 10}); connect.CodeOf(err) != connect.CodeUnavailable {
		t.Fatalf("unavailable scenario error = %v, want Unavailable", err)
	}

	t.Setenv("CT1_LOG_SCENARIO", "logs-interrupted")
	entries, err := readLogTail(t, system, &mdk.GetLogsRequest{Tail: 20})
	if connect.CodeOf(err) != connect.CodeUnavailable || len(entries) != 10 {
		t.Fatalf("interrupted scenario = %d entries, %v; want 10 entries then Unavailable", len(entries), err)
	}
}

func TestListAuditEventsBoundsWindowAndLimit(t *testing.T) {
	srv, state := newConnectTestServer(t)
	system := mdkv2connect.NewSystemServiceClient(http.DefaultClient, srv.URL)
	ctx := context.Background()
	end := state.StartTime

	week, err := system.ListAuditEvents(ctx, connect.NewRequest(&mdk.ListAuditEventsRequest{
		Range: &mdk.TimeRange{Start: timestamppb.New(end.Add(-maxAuditRange)), End: timestamppb.New(end)},
		Limit: 50,
	}))
	if err != nil {
		t.Fatalf("ListAuditEvents: %v", err)
	}
	events := week.Msg.GetEvents()
	if len(events) != 50 {
		t.Fatalf("events = %d, want the requested limit", len(events))
	}
	for i, event := range events {
		if event.GetNode() != state.ServerID || event.GetActor() == "" {
			t.Fatalf("event %d lacks attribution: %+v", i, event)
		}
		if i > 0 && event.GetTime().AsTime().After(events[i-1].GetTime().AsTime()) {
			t.Fatalf("events are not newest first at %d", i)
		}
	}

	failures, err := system.ListAuditEvents(ctx, connect.NewRequest(&mdk.ListAuditEventsRequest{Result: "error"}))
	if err != nil {
		t.Fatalf("ListAuditEvents: %v", err)
	}
	for _, event := range failures.Msg.GetEvents() {
		if event.GetResult() != "error" || event.GetTime().AsTime().Before(end.Add(-defaultAuditRange)) {
			t.Fatalf("filtered event outside result or default window: %+v", event)
		}
	}

	_, err = system.ListAuditEvents(ctx, connect.NewRequest(&mdk.ListAuditEventsRequest{
		Range: &mdk.TimeRange{Start: timestamppb.New(end.Add(-8 * 24 * time.Hour)), End: timestamppb.New(end)},
	}))
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("eight-day window error = %v, want InvalidArgument", err)
	}

	t.Setenv("CT1_LOG_SCENARIO", "audit-unavailable")
	_, err = system.ListAuditEvents(ctx, connect.NewRequest(&mdk.ListAuditEventsRequest{}))
	var connectErr *connect.Error
	if !errors.As(err, &connectErr) || connectErr.Code() != connect.CodeUnavailable {
		t.Fatalf("audit-unavailable error = %v, want Unavailable", err)
	}
}
