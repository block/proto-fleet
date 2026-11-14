package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sort"
	"time"

	"connectrpc.com/connect"
	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Frigate keeps a bounded in-memory log ring and caps tail and audit reads.
const (
	logRingCapacity   = 1000
	defaultLogTail    = 200
	maxLogTail        = 1000
	defaultAuditLimit = 200
	maxAuditLimit     = 1000
	defaultAuditRange = 24 * time.Hour
	maxAuditRange     = 7 * 24 * time.Hour

	// The simulated process logs one record per logInterval, starting
	// logBackfill before the simulator started so the ring is already full.
	logInterval = 2 * time.Second
	logBackfill = 45 * time.Minute
	// One simulated audit record per auditInterval over auditBackfill.
	auditInterval = 47 * time.Minute
	auditBackfill = 8 * 24 * time.Hour
)

// CT1_LOG_SCENARIO selects a degraded log/audit mode for local verification:
// "logs-unavailable", "logs-interrupted", or "audit-unavailable".
func logScenario() string { return os.Getenv("CT1_LOG_SCENARIO") }

type simulatedLogEntry struct {
	sequence  uint64
	timestamp time.Time
	level     mdk.LogLevel
	component string
	message   string
}

var logComponents = []string{"mining", "pool", "cooling", "power", "api", "flotilla"}

// simulatedLog returns the deterministic record with the given sequence.
func (s *ContainerState) simulatedLog(sequence uint64) simulatedLogEntry {
	component := logComponents[sequence%uint64(len(logComponents))]
	level := mdk.LogLevel_LOG_LEVEL_INFO
	message := fmt.Sprintf("loop tick %d completed", sequence)
	switch {
	case sequence%53 == 0:
		level = mdk.LogLevel_LOG_LEVEL_ERROR
		message = fmt.Sprintf("request failed: deadline exceeded after 5s (attempt %d)", sequence%5+1)
	case sequence%17 == 0:
		level = mdk.LogLevel_LOG_LEVEL_WARNING
		message = fmt.Sprintf("reading late by %dms", 200+sequence%800)
	case sequence%3 == 0:
		level = mdk.LogLevel_LOG_LEVEL_DEBUG
		message = fmt.Sprintf("state snapshot seq=%d", sequence)
	}
	return simulatedLogEntry{
		sequence:  sequence,
		timestamp: s.StartTime.Add(-logBackfill).Add(time.Duration(sequence) * logInterval),
		level:     level,
		component: component,
		message:   message,
	}
}

// latestLogSequence is the newest record logged at or before now; sequences start at 1.
func (s *ContainerState) latestLogSequence(now time.Time) uint64 {
	elapsed := now.Sub(s.StartTime.Add(-logBackfill))
	if elapsed < 0 {
		return 0
	}
	return uint64(elapsed / logInterval)
}

func logMatches(entry simulatedLogEntry, req *mdk.GetLogsRequest) bool {
	minLevel := req.GetLevel()
	if minLevel == mdk.LogLevel_LOG_LEVEL_UNSPECIFIED {
		minLevel = mdk.LogLevel_LOG_LEVEL_INFO
	}
	return entry.level >= minLevel && (req.GetComponent() == "" || entry.component == req.GetComponent())
}

// logTail returns the newest matching records still held in the ring, oldest
// first, and the newest sequence observed.
func (s *ContainerState) logTail(req *mdk.GetLogsRequest, now time.Time) ([]simulatedLogEntry, uint64) {
	limit := int(req.GetTail())
	if limit <= 0 {
		limit = defaultLogTail
	}
	limit = min(limit, maxLogTail)
	latest := s.latestLogSequence(now)
	oldest := uint64(1)
	if latest > logRingCapacity {
		oldest = latest - logRingCapacity + 1
	}
	var out []simulatedLogEntry
	for sequence := latest; sequence >= oldest && sequence > 0 && len(out) < limit; sequence-- {
		if entry := s.simulatedLog(sequence); logMatches(entry, req) {
			out = append(out, entry)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].sequence < out[j].sequence })
	return out, latest
}

func (e simulatedLogEntry) proto() *mdk.GetLogsResponse {
	return &mdk.GetLogsResponse{
		Timestamp: timestamppb.New(e.timestamp),
		Level:     e.level,
		Component: e.component,
		Message:   e.message,
	}
}

// streamLogs mirrors Frigate: send the retained tail, then follow new records
// until the client disconnects.
func (s *ContainerState) streamLogs(ctx context.Context, req *mdk.GetLogsRequest, send func(*mdk.GetLogsResponse) error) error {
	scenario := logScenario()
	if scenario == "logs-unavailable" {
		return status.Error(codes.Unavailable, "process log stream is not configured")
	}
	entries, latest := s.logTail(req, s.now())
	for i, entry := range entries {
		if scenario == "logs-interrupted" && i == len(entries)/2 && i > 0 {
			return status.Error(codes.Unavailable, "log stream reset by controller")
		}
		if err := send(entry.proto()); err != nil {
			return err
		}
	}
	ticker := time.NewTicker(logInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
		next := s.latestLogSequence(s.now())
		for sequence := latest + 1; sequence <= next; sequence++ {
			if entry := s.simulatedLog(sequence); logMatches(entry, req) {
				if err := send(entry.proto()); err != nil {
					return err
				}
			}
		}
		latest = next
	}
}

var auditRecords = []struct {
	actor, source, action, target, result, err, details string
}{
	{"proto-os", "rest:192.168.1.40:51544", "mining.set-target-power", "container", "ok", "", `{"watts":4200}`},
	{"operator@bridge", "grpc:10.0.0.5:40112", "mining.stop", "tank-2/module-11", "ok", "", `{"reason":"maintenance"}`},
	{"system", "system", "failover.promote", "controller", "ok", "", ""},
	{"proto-os", "rest:192.168.1.40:51544", "power.set-container-control", "dry_cooler", "error", "controller register write not confirmed", `{"enabled":false}`},
	{"operator@bridge", "grpc:10.0.0.5:40112", "mining.start", "tank-2/module-11", "ok", "", ""},
	{"fleet", "grpc:10.0.0.2:38810", "pool.update", "pool-1", "ok", "", `{"priority":0}`},
}

func (s *ContainerState) simulatedAudit(index int) *mdk.AuditEvent {
	record := auditRecords[index%len(auditRecords)]
	return &mdk.AuditEvent{
		Id:          int64(index + 1),
		Time:        timestamppb.New(s.StartTime.Add(-auditBackfill).Add(time.Duration(index) * auditInterval)),
		Actor:       record.actor,
		Source:      record.source,
		Node:        s.ServerID,
		Action:      record.action,
		Target:      record.target,
		Result:      record.result,
		Error:       record.err,
		DetailsJson: record.details,
	}
}

func auditWindow(window *mdk.TimeRange, now time.Time) (time.Time, time.Time, error) {
	end := now
	if window.GetEnd() != nil {
		end = window.GetEnd().AsTime()
	}
	start := end.Add(-defaultAuditRange)
	if window.GetStart() != nil {
		start = window.GetStart().AsTime()
	}
	if !start.Before(end) {
		return time.Time{}, time.Time{}, status.Error(codes.InvalidArgument, "range start must be before end")
	}
	if end.Sub(start) > maxAuditRange {
		return time.Time{}, time.Time{}, status.Error(codes.InvalidArgument, "range must not exceed 7 days")
	}
	return start, end, nil
}

func (s *ContainerState) listAuditEvents(req *mdk.ListAuditEventsRequest) (*mdk.ListAuditEventsResponse, error) {
	if logScenario() == "audit-unavailable" {
		return nil, status.Error(codes.Unavailable, "audit log requires a configured database")
	}
	start, end, err := auditWindow(req.GetRange(), s.now())
	if err != nil {
		return nil, err
	}
	limit := int(req.GetLimit())
	if limit <= 0 {
		limit = defaultAuditLimit
	}
	limit = min(limit, maxAuditLimit)
	matches := func(event *mdk.AuditEvent) bool {
		for _, filter := range [][2]string{
			{req.GetAction(), event.GetAction()}, {req.GetActor(), event.GetActor()}, {req.GetSource(), event.GetSource()},
			{req.GetTarget(), event.GetTarget()}, {req.GetResult(), event.GetResult()},
		} {
			if filter[0] != "" && filter[0] != filter[1] {
				return false
			}
		}
		return true
	}
	out := &mdk.ListAuditEventsResponse{}
	newest := int(s.now().Sub(s.StartTime.Add(-auditBackfill)) / auditInterval)
	for index := newest; index >= 0 && len(out.Events) < limit; index-- {
		event := s.simulatedAudit(index)
		at := event.GetTime().AsTime()
		if at.Before(start) {
			break
		}
		if !at.After(end) && matches(event) {
			out.Events = append(out.Events, event)
		}
	}
	return out, nil
}

func (a *grpcAPI) GetLogs(req *mdk.GetLogsRequest, stream mdk.SystemService_GetLogsServer) error {
	return a.state.streamLogs(stream.Context(), req, stream.Send)
}

func (a *grpcAPI) ListAuditEvents(_ context.Context, req *mdk.ListAuditEventsRequest) (*mdk.ListAuditEventsResponse, error) {
	return a.state.listAuditEvents(req)
}

func (a *connectAPI) GetLogs(ctx context.Context, req *connect.Request[mdk.GetLogsRequest], stream *connect.ServerStream[mdk.GetLogsResponse]) error {
	err := a.native.state.streamLogs(ctx, req.Msg, stream.Send)
	if st, ok := status.FromError(err); ok && err != nil {
		return connect.NewError(connect.Code(st.Code()), fmt.Errorf("%s", st.Message()))
	}
	return err
}

func (a *connectAPI) ListAuditEvents(ctx context.Context, req *connect.Request[mdk.ListAuditEventsRequest]) (*connect.Response[mdk.ListAuditEventsResponse], error) {
	return connectResult(a.native.ListAuditEvents(ctx, req.Msg))
}

// Flush lets streaming Connect handlers (GetLogs) send entries as they are
// produced through the JSON error middleware.
func (jw *jsonErrorWriter) Flush() {
	if flusher, ok := jw.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}
