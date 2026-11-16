package ct1

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// recordsFrigate scripts GetLogs per call: each call gets the next script,
// and the last one repeats.
type recordsFrigate struct {
	mdk.UnimplementedSystemServiceServer
	mu       sync.Mutex
	serverID string
	scripts  []func(*mdk.GetLogsRequest, mdk.SystemService_GetLogsServer) error
	requests []*mdk.GetLogsRequest
	audit    *mdk.ListAuditEventsRequest
	events   []*mdk.AuditEvent
}

func (f *recordsFrigate) GetSystemStatus(context.Context, *mdk.GetSystemStatusRequest) (*mdk.GetSystemStatusResponse, error) {
	return &mdk.GetSystemStatusResponse{Status: &mdk.SystemStatus{ServerId: f.serverID}}, nil
}

func (f *recordsFrigate) GetLogs(req *mdk.GetLogsRequest, stream mdk.SystemService_GetLogsServer) error {
	f.mu.Lock()
	f.requests = append(f.requests, req)
	script := f.scripts[min(len(f.requests), len(f.scripts))-1]
	f.mu.Unlock()
	return script(req, stream)
}

func (f *recordsFrigate) ListAuditEvents(_ context.Context, req *mdk.ListAuditEventsRequest) (*mdk.ListAuditEventsResponse, error) {
	f.audit = req
	return &mdk.ListAuditEventsResponse{Events: f.events}, nil
}

func (f *recordsFrigate) logRequests() []*mdk.GetLogsRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*mdk.GetLogsRequest(nil), f.requests...)
}

var logEpoch = time.Date(2026, 11, 14, 8, 0, 0, 0, time.UTC)

func logEntry(i int) *mdk.GetLogsResponse {
	return &mdk.GetLogsResponse{Timestamp: timestamppb.New(logEpoch.Add(time.Duration(i) * time.Second)), Level: mdk.LogLevel_LOG_LEVEL_INFO, Component: "plant", Message: fmt.Sprintf("entry %d", i)}
}

// sendThenFollow sends the entries, pausing before the indexes in stallBefore,
// then follows forever like Frigate.
func sendThenFollow(entries []int, stallBefore map[int]time.Duration) func(*mdk.GetLogsRequest, mdk.SystemService_GetLogsServer) error {
	return func(req *mdk.GetLogsRequest, stream mdk.SystemService_GetLogsServer) error {
		tail := entries
		if n := int(req.GetTail()); n > 0 && n < len(tail) {
			tail = tail[len(tail)-n:]
		}
		for i, e := range tail {
			if d := stallBefore[i]; d > 0 && req.GetTail() != 1 {
				select {
				case <-time.After(d):
				case <-stream.Context().Done():
					return nil
				}
			}
			if err := stream.Send(logEntry(e)); err != nil {
				return err
			}
		}
		<-stream.Context().Done()
		return nil
	}
}

func rangeOf(n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = i
	}
	return out
}

func newRecordsDevice(t *testing.T, frigates ...*recordsFrigate) *Device {
	t.Helper()
	d := &Device{id: "ct1"}
	for _, f := range frigates {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)
		server := grpc.NewServer()
		mdk.RegisterSystemServiceServer(server, f)
		go func() { _ = server.Serve(listener) }()
		t.Cleanup(server.Stop)
		conn, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
		require.NoError(t, err)
		t.Cleanup(func() { _ = conn.Close() })
		d.clients = append(d.clients, &Client{conn: conn, memberID: f.serverID, system: mdk.NewSystemServiceClient(conn)})
	}
	return d
}

func shortenLogTiming(t *testing.T) {
	open, idle, deadline := logTailOpenTimeout, logTailIdleGap, logTailDeadline
	logTailOpenTimeout, logTailIdleGap, logTailDeadline = 300*time.Millisecond, 150*time.Millisecond, 1500*time.Millisecond
	t.Cleanup(func() { logTailOpenTimeout, logTailIdleGap, logTailDeadline = open, idle, deadline })
}

func readLogs(t *testing.T, d *Device, q sdk.ControllerLogQuery, controller string) (sdk.ControllerRecordsResult, error) {
	t.Helper()
	return d.ControllerRecords(t.Context(), sdk.ControllerRecordsRequest{Kind: sdk.ControllerRecordsKindLogs, ControllerID: controller, Logs: &q})
}

func messages(s *sdk.ControllerLogSnapshot) []string {
	out := make([]string, len(s.Entries))
	for i, e := range s.Entries {
		out[i] = e.Message
	}
	return out
}

func TestControllerLogsConfirmedTailIsRetainedWithIdentityAndFilters(t *testing.T) {
	shortenLogTiming(t)
	f := &recordsFrigate{serverID: "ct1-a", scripts: []func(*mdk.GetLogsRequest, mdk.SystemService_GetLogsServer) error{sendThenFollow(rangeOf(3), nil)}}
	result, err := readLogs(t, newRecordsDevice(t, f), sdk.ControllerLogQuery{Limit: 10, MinSeverity: sdk.ControllerLogWarn, Component: "plant"}, "")
	require.NoError(t, err)
	require.Equal(t, "ct1-a", result.ControllerID)
	require.Equal(t, []string{"ct1-a"}, result.ControllerIDs)
	require.Equal(t, sdk.ControllerLogRetained, result.Logs.Coverage)
	require.Equal(t, []string{"entry 0", "entry 1", "entry 2"}, messages(result.Logs))
	first := result.Logs.Entries[0]
	require.Equal(t, logEpoch, *first.Time, "source time is preserved")
	require.Equal(t, sdk.ControllerLogInfo, first.Severity)
	require.Equal(t, "plant", first.Component)

	requests := f.logRequests()
	require.Len(t, requests, 2, "the pause is confirmed with a tail-1 read")
	require.Equal(t, mdk.LogLevel_LOG_LEVEL_WARNING, requests[0].GetLevel())
	require.Equal(t, "plant", requests[0].GetComponent())
	require.EqualValues(t, 10, requests[0].GetTail())
	require.EqualValues(t, 1, requests[1].GetTail())
	require.Equal(t, mdk.LogLevel_LOG_LEVEL_WARNING, requests[1].GetLevel())
}

func TestControllerLogsFullTailIsLimitReachedWithoutConfirmation(t *testing.T) {
	shortenLogTiming(t)
	f := &recordsFrigate{serverID: "ct1-a", scripts: []func(*mdk.GetLogsRequest, mdk.SystemService_GetLogsServer) error{sendThenFollow(rangeOf(8), nil)}}
	result, err := readLogs(t, newRecordsDevice(t, f), sdk.ControllerLogQuery{Limit: 5}, "")
	require.NoError(t, err)
	require.Equal(t, sdk.ControllerLogLimitReached, result.Logs.Coverage)
	require.Equal(t, []string{"entry 3", "entry 4", "entry 5", "entry 6", "entry 7"}, messages(result.Logs))
	require.Len(t, f.logRequests(), 1)
}

// Large controller messages are bounded as they arrive, and the oldest are
// dropped once the read holds a full result's worth, so a read never holds
// a tail of whole messages.
func TestControllerLogsLargeMessagesAreBoundedWhileCollected(t *testing.T) {
	shortenLogTiming(t)
	const sent = 900
	large := func(i int) *mdk.GetLogsResponse {
		e := logEntry(i)
		e.Message = fmt.Sprintf("entry %d ", i) + strings.Repeat("x", 64<<10)
		return e
	}
	script := func(req *mdk.GetLogsRequest, stream mdk.SystemService_GetLogsServer) error {
		first := 0
		if n := int(req.GetTail()); n > 0 && n < sent {
			first = sent - n
		}
		for i := first; i < sent; i++ {
			if err := stream.Send(large(i)); err != nil {
				return err
			}
		}
		<-stream.Context().Done()
		return nil
	}
	f := &recordsFrigate{serverID: "ct1-a", scripts: []func(*mdk.GetLogsRequest, mdk.SystemService_GetLogsServer) error{script}}

	result, err := readLogs(t, newRecordsDevice(t, f), sdk.ControllerLogQuery{Limit: sdk.ControllerRecordsLimit}, "")
	require.NoError(t, err)
	logs := result.Logs
	require.Equal(t, sdk.ControllerLogLimitReached, logs.Coverage, "dropped entries mean older records were not returned")
	require.NotEmpty(t, logs.Entries)
	require.Less(t, len(logs.Entries), sent)
	require.True(t, strings.HasPrefix(logs.Entries[len(logs.Entries)-1].Message, fmt.Sprintf("entry %d ", sent-1)), "the newest entries are kept")
	for _, e := range logs.Entries {
		require.True(t, e.MessageTruncated)
		require.LessOrEqual(t, len(e.Message), sdk.ControllerLogMessageMaxBytes)
	}
	encoded, err := sdk.EncodeControllerRecords(logs.Entries)
	require.NoError(t, err)
	require.LessOrEqual(t, len(encoded), sdk.ControllerRecordsBudget)
	require.Len(t, f.logRequests(), 2, "the tail is confirmed, not re-read")
}

func TestLogCollectionReleasesDroppedEntries(t *testing.T) {
	var c logCollection
	message := strings.Repeat("y", 8<<20)
	for i := range sdk.ControllerRecordsLimit {
		e := logEntry(i)
		e.Message = message
		c.add(e)
	}
	require.Equal(t, sdk.ControllerRecordsLimit, c.received)
	require.True(t, c.dropped)
	require.LessOrEqual(t, c.size, sdk.ControllerRecordsBudget)
	total := 0
	for _, r := range c.records {
		total += r.size
		require.Len(t, r.entry.Message, sdk.ControllerLogMessageMaxBytes)
	}
	require.Equal(t, c.size, total)
	require.Equal(t, logEpoch.Add((sdk.ControllerRecordsLimit-1)*time.Second), *c.newest().Time)
}

func TestControllerLogsStallMidTailIsReread(t *testing.T) {
	shortenLogTiming(t)
	stalled := sendThenFollow(rangeOf(5), map[int]time.Duration{2: 400 * time.Millisecond})
	whole := sendThenFollow(rangeOf(5), nil)
	f := &recordsFrigate{serverID: "ct1-a", scripts: []func(*mdk.GetLogsRequest, mdk.SystemService_GetLogsServer) error{stalled, whole}}
	result, err := readLogs(t, newRecordsDevice(t, f), sdk.ControllerLogQuery{Limit: 10}, "")
	require.NoError(t, err)
	require.Equal(t, sdk.ControllerLogRetained, result.Logs.Coverage)
	require.Equal(t, []string{"entry 0", "entry 1", "entry 2", "entry 3", "entry 4"}, messages(result.Logs))
	require.Len(t, f.logRequests(), 3)
}

func TestControllerLogsStallOnEveryReadIsInterrupted(t *testing.T) {
	shortenLogTiming(t)
	stalled := sendThenFollow(rangeOf(5), map[int]time.Duration{2: 2 * time.Second})
	f := &recordsFrigate{serverID: "ct1-a", scripts: []func(*mdk.GetLogsRequest, mdk.SystemService_GetLogsServer) error{stalled}}
	result, err := readLogs(t, newRecordsDevice(t, f), sdk.ControllerLogQuery{Limit: 10}, "")
	require.NoError(t, err)
	require.Equal(t, sdk.ControllerLogInterrupted, result.Logs.Coverage)
	require.Equal(t, missedNewest, result.Logs.Interruption)
	require.Equal(t, []string{"entry 0", "entry 1"}, messages(result.Logs))
}

func TestControllerLogsSilentStreamIsNoResponse(t *testing.T) {
	shortenLogTiming(t)
	silent := func(_ *mdk.GetLogsRequest, stream mdk.SystemService_GetLogsServer) error {
		<-stream.Context().Done()
		return nil
	}
	f := &recordsFrigate{serverID: "ct1-a", scripts: []func(*mdk.GetLogsRequest, mdk.SystemService_GetLogsServer) error{silent}}
	result, err := readLogs(t, newRecordsDevice(t, f), sdk.ControllerLogQuery{Limit: 10}, "")
	require.NoError(t, err)
	require.Equal(t, sdk.ControllerLogNoResponse, result.Logs.Coverage)
	require.Empty(t, result.Logs.Entries)
	require.NoError(t, result.Validate(sdk.ControllerRecordsRequest{Kind: sdk.ControllerRecordsKindLogs, Logs: &sdk.ControllerLogQuery{Limit: 10}}))
}

func TestControllerLogsStillSendingAtDeadlineIsInterrupted(t *testing.T) {
	shortenLogTiming(t)
	flood := func(_ *mdk.GetLogsRequest, stream mdk.SystemService_GetLogsServer) error {
		for i := 0; ; i++ {
			if err := stream.Send(logEntry(i)); err != nil {
				return err
			}
			select {
			case <-time.After(50 * time.Millisecond):
			case <-stream.Context().Done():
				return nil
			}
		}
	}
	f := &recordsFrigate{serverID: "ct1-a", scripts: []func(*mdk.GetLogsRequest, mdk.SystemService_GetLogsServer) error{flood}}
	result, err := readLogs(t, newRecordsDevice(t, f), sdk.ControllerLogQuery{Limit: 1000}, "")
	require.NoError(t, err)
	require.Equal(t, sdk.ControllerLogInterrupted, result.Logs.Coverage)
	require.Contains(t, result.Logs.Interruption, "still sending")
	require.NotEmpty(t, result.Logs.Entries)
}

func TestControllerLogsStreamFailure(t *testing.T) {
	shortenLogTiming(t)
	failAfter := func(n int) func(*mdk.GetLogsRequest, mdk.SystemService_GetLogsServer) error {
		return func(_ *mdk.GetLogsRequest, stream mdk.SystemService_GetLogsServer) error {
			for i := range n {
				if err := stream.Send(logEntry(i)); err != nil {
					return err
				}
			}
			return status.Error(codes.Unavailable, "log buffer reset")
		}
	}
	partial := &recordsFrigate{serverID: "ct1-a", scripts: []func(*mdk.GetLogsRequest, mdk.SystemService_GetLogsServer) error{failAfter(2)}}
	result, err := readLogs(t, newRecordsDevice(t, partial), sdk.ControllerLogQuery{Limit: 10}, "")
	require.NoError(t, err)
	require.Equal(t, sdk.ControllerLogInterrupted, result.Logs.Coverage)
	require.Equal(t, "the stream failed: log buffer reset", result.Logs.Interruption)
	require.Len(t, result.Logs.Entries, 2)

	none := &recordsFrigate{serverID: "ct1-a", scripts: []func(*mdk.GetLogsRequest, mdk.SystemService_GetLogsServer) error{failAfter(0)}}
	_, err = readLogs(t, newRecordsDevice(t, none), sdk.ControllerLogQuery{Limit: 10}, "")
	require.Equal(t, codes.Unavailable, status.Code(err), "nothing partial to show, so the logs are unavailable")
}

func TestControllerRecordsAddressesOneHAControllerByIdentity(t *testing.T) {
	shortenLogTiming(t)
	a := &recordsFrigate{serverID: "ct1-a", scripts: []func(*mdk.GetLogsRequest, mdk.SystemService_GetLogsServer) error{sendThenFollow(rangeOf(1), nil)}}
	b := &recordsFrigate{serverID: "ct1-b", scripts: []func(*mdk.GetLogsRequest, mdk.SystemService_GetLogsServer) error{sendThenFollow(rangeOf(2), nil)}}
	d := newRecordsDevice(t, a, b)
	result, err := readLogs(t, d, sdk.ControllerLogQuery{Limit: 10}, "ct1-b")
	require.NoError(t, err)
	require.Equal(t, "ct1-b", result.ControllerID)
	require.Equal(t, []string{"ct1-a", "ct1-b"}, result.ControllerIDs)
	require.Len(t, result.Logs.Entries, 2)
	require.Empty(t, a.logRequests())

	_, err = readLogs(t, d, sdk.ControllerLogQuery{Limit: 10}, "ct1-c")
	require.Error(t, err)
	require.Equal(t, sdk.WriteNotDispatched, sdk.WriteEvidence(err).GetReason())

	// The address now answers as another controller: never attribute its logs.
	b.serverID = "ct1-replacement"
	_, err = readLogs(t, d, sdk.ControllerLogQuery{Limit: 10}, "ct1-b")
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
}

func TestControllerAuditEventsMapAttributionAndBounds(t *testing.T) {
	at := time.Date(2026, 11, 14, 9, 30, 0, 0, time.UTC)
	f := &recordsFrigate{serverID: "ct1-a", events: []*mdk.AuditEvent{
		{Id: 9, Time: timestamppb.New(at), Actor: "operator@fleet", Source: "grpc:10.0.0.5:51000", Node: "ct1-a", Action: "mining.stop", Target: "tank/2", Result: "error", Error: "interlock", DetailsJson: `{"reason":"maintenance"}`},
		{Id: 8, Actor: "system", Source: "system", Node: "ct1-b", Action: "ha.failover", Result: "ok"},
		{Id: 7, Actor: "system", Result: "ok"},
	}}
	start, end := at.Add(-time.Hour), at.Add(time.Hour)
	result, err := newRecordsDevice(t, f).ControllerRecords(t.Context(), sdk.ControllerRecordsRequest{Kind: sdk.ControllerRecordsKindAudit, Audit: &sdk.ControllerAuditQuery{Start: start, End: end, Limit: 2, Result: "error"}})
	require.NoError(t, err)
	require.True(t, f.audit.GetRange().GetStart().AsTime().Equal(start))
	require.True(t, f.audit.GetRange().GetEnd().AsTime().Equal(end))
	require.EqualValues(t, 2, f.audit.GetLimit())
	require.Equal(t, "error", f.audit.GetResult())

	page := result.Audit
	require.True(t, page.LimitReached)
	require.Len(t, page.Events, 2, "a controller that returns more than the limit is trimmed")
	require.Equal(t, sdk.ControllerAuditEvent{ID: 9, Time: &at, Actor: "operator@fleet", Source: "grpc:10.0.0.5:51000", Node: "ct1-a", Action: "mining.stop", Target: "tank/2", Result: "error", Error: "interlock", DetailsJSON: `{"reason":"maintenance"}`}, page.Events[0])
	require.Nil(t, page.Events[1].Time, "a missing source time is not invented")
	require.Equal(t, "ct1-b", page.Events[1].Node)
}
