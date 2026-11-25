package sdk

import (
	"context"
	"strings"
	"testing"
	"time"

	pb "github.com/block/proto-fleet/server/sdk/v1/pb/generated"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

func logsRequest(limit int32) ControllerRecordsRequest {
	return ControllerRecordsRequest{Kind: ControllerRecordsKindLogs, Logs: &ControllerLogQuery{Limit: limit}}
}

func auditRequest(start, end time.Time) ControllerRecordsRequest {
	return ControllerRecordsRequest{Kind: ControllerRecordsKindAudit, Audit: &ControllerAuditQuery{Start: start, End: end, Limit: 100}}
}

func TestControllerRecordsRequestValidation(t *testing.T) {
	now := time.Now().UTC()
	valid := []ControllerRecordsRequest{
		logsRequest(1),
		{Kind: ControllerRecordsKindLogs, ControllerID: "ct1-a", Logs: &ControllerLogQuery{Limit: 1000, MinSeverity: ControllerLogWarn, Component: "pump"}},
		auditRequest(now.Add(-ControllerAuditWindowMax), now),
		{Kind: ControllerRecordsKindAudit, Audit: &ControllerAuditQuery{Start: now.Add(-time.Hour), End: now, Limit: 1, Result: "error"}},
	}
	for _, req := range valid {
		require.NoError(t, req.Validate(), "%+v", req)
	}
	invalid := map[string]ControllerRecordsRequest{
		"unknown kind":     {Kind: "sql"},
		"no query":         {Kind: ControllerRecordsKindLogs},
		"both queries":     {Kind: ControllerRecordsKindLogs, Logs: &ControllerLogQuery{Limit: 1}, Audit: &ControllerAuditQuery{}},
		"zero limit":       logsRequest(0),
		"over limit":       logsRequest(1001),
		"bad severity":     {Kind: ControllerRecordsKindLogs, Logs: &ControllerLogQuery{Limit: 1, MinSeverity: "trace"}},
		"long component":   {Kind: ControllerRecordsKindLogs, Logs: &ControllerLogQuery{Limit: 1, Component: strings.Repeat("c", 129)}},
		"long controller":  {Kind: ControllerRecordsKindLogs, ControllerID: strings.Repeat("c", 256), Logs: &ControllerLogQuery{Limit: 1}},
		"inverted window":  auditRequest(now, now.Add(-time.Hour)),
		"window too long":  auditRequest(now.Add(-ControllerAuditWindowMax-time.Second), now),
		"zero window":      auditRequest(time.Time{}, now),
		"bad result":       {Kind: ControllerRecordsKindAudit, Audit: &ControllerAuditQuery{Start: now.Add(-time.Hour), End: now, Limit: 1, Result: "denied"}},
		"audit over limit": {Kind: ControllerRecordsKindAudit, Audit: &ControllerAuditQuery{Start: now.Add(-time.Hour), End: now, Limit: 1001}},
	}
	for name, req := range invalid {
		require.Error(t, req.Validate(), name)
	}
}

func TestControllerRecordsResultValidation(t *testing.T) {
	req := logsRequest(2)
	ok := ControllerRecordsResult{Logs: &ControllerLogSnapshot{Entries: []ControllerLogEntry{{Severity: ControllerLogInfo, Message: "m"}}, Coverage: ControllerLogRetained}}
	require.NoError(t, ok.Validate(req))

	for name, result := range map[string]ControllerRecordsResult{
		"missing logs":           {},
		"audit for logs":         {Logs: ok.Logs, Audit: &ControllerAuditPage{}},
		"over limit":             {Logs: &ControllerLogSnapshot{Entries: make([]ControllerLogEntry, 3), Coverage: ControllerLogRetained}},
		"unknown coverage":       {Logs: &ControllerLogSnapshot{Coverage: "complete"}},
		"interrupted no reason":  {Logs: &ControllerLogSnapshot{Coverage: ControllerLogInterrupted}},
		"reason when complete":   {Logs: &ControllerLogSnapshot{Coverage: ControllerLogRetained, Interruption: "x"}},
		"no response w/ entries": {Logs: &ControllerLogSnapshot{Coverage: ControllerLogNoResponse, Entries: ok.Logs.Entries}},
		"bad severity":           {Logs: &ControllerLogSnapshot{Coverage: ControllerLogRetained, Entries: []ControllerLogEntry{{Severity: "trace"}}}},
		"unbounded message":      {Logs: &ControllerLogSnapshot{Coverage: ControllerLogRetained, Entries: []ControllerLogEntry{{Severity: ControllerLogInfo, Message: strings.Repeat("m", ControllerLogMessageMaxBytes+1)}}}},
		"three controllers":      {ControllerIDs: []string{"a", "b", "c"}, Logs: ok.Logs},
	} {
		require.Error(t, result.Validate(req), name)
	}

	addressed := req
	addressed.ControllerID = "ct1-a"
	require.NoError(t, ControllerRecordsResult{ControllerID: "ct1-a", Logs: ok.Logs}.Validate(addressed))
	require.NoError(t, ControllerRecordsResult{Logs: ok.Logs}.Validate(addressed), "identity is best effort")
	require.Error(t, ControllerRecordsResult{ControllerID: "ct1-b", Logs: ok.Logs}.Validate(addressed), "answered by another controller")

	audit := auditRequest(time.Now().Add(-time.Hour), time.Now())
	require.NoError(t, ControllerRecordsResult{Audit: &ControllerAuditPage{Events: []ControllerAuditEvent{{ID: 1, Actor: "op@fleet"}}}}.Validate(audit))
	require.Error(t, ControllerRecordsResult{Audit: &ControllerAuditPage{Events: []ControllerAuditEvent{{Actor: strings.Repeat("a", 257)}}}}.Validate(audit))
	require.Error(t, ControllerRecordsResult{Logs: ok.Logs}.Validate(audit))
}

func TestTruncateJSONTextBoundsEncodedLength(t *testing.T) {
	for _, s := range []string{
		strings.Repeat("a", 100),
		strings.Repeat("\x01", 100),
		strings.Repeat(`"\`, 100),
		strings.Repeat("é", 100),
		strings.Repeat("\u2028", 100),
		strings.Repeat("\xff", 100),
		"<script>&amp;</script>" + strings.Repeat("\n", 50),
	} {
		cut, truncated := TruncateJSONText(s, 64)
		require.True(t, truncated)
		encoded, err := EncodeControllerRecords(cut)
		require.NoError(t, err)
		require.LessOrEqual(t, len(encoded)-2, 64, "%q", s)
		require.True(t, strings.HasPrefix(s, cut))
	}
	same, truncated := TruncateJSONText("short", 64)
	require.False(t, truncated)
	require.Equal(t, "short", same)
}

func TestBoundControllerRecordsTruncatesFieldsAndDropsOldest(t *testing.T) {
	long := strings.Repeat("x", ControllerLogMessageMaxBytes*2)
	entries := make([]ControllerLogEntry, ControllerRecordsLimit)
	for i := range entries {
		entries[i] = ControllerLogEntry{Severity: ControllerLogInfo, Message: strings.Repeat("\x00", ControllerLogMessageMaxBytes)}
	}
	entries[len(entries)-1].Message = long
	bounded := BoundControllerRecords(ControllerRecordsResult{Logs: &ControllerLogSnapshot{Entries: entries, Coverage: ControllerLogRetained}})
	logs := bounded.Logs
	newest := logs.Entries[len(logs.Entries)-1]
	require.True(t, newest.MessageTruncated)
	require.Len(t, newest.Message, ControllerLogMessageMaxBytes)
	require.Less(t, len(logs.Entries), ControllerRecordsLimit, "control characters expand sixfold, so oldest entries are dropped")
	require.Equal(t, ControllerLogLimitReached, logs.Coverage)
	encoded, err := EncodeControllerRecords(bounded)
	require.NoError(t, err)
	require.LessOrEqual(t, len(encoded), controllerRecordsPayloadLimit)
	require.NoError(t, bounded.Validate(logsRequest(ControllerRecordsLimit)))
	require.Len(t, entries[len(entries)-1].Message, len(long), "the caller's entries are not modified")

	interrupted := BoundControllerRecords(ControllerRecordsResult{Logs: &ControllerLogSnapshot{Entries: entries, Coverage: ControllerLogInterrupted, Interruption: "stream reset"}})
	require.Equal(t, ControllerLogInterrupted, interrupted.Logs.Coverage, "an interrupted read stays interrupted")

	events := make([]ControllerAuditEvent, ControllerRecordsLimit)
	for i := range events {
		events[i] = ControllerAuditEvent{ID: int64(ControllerRecordsLimit - i), DetailsJSON: strings.Repeat("\x00", ControllerAuditDetailMaxBytes*2), Actor: strings.Repeat("a", 300)}
	}
	page := BoundControllerRecords(ControllerRecordsResult{Audit: &ControllerAuditPage{Events: events}}).Audit
	require.True(t, page.LimitReached)
	require.Less(t, len(page.Events), ControllerRecordsLimit)
	require.EqualValues(t, ControllerRecordsLimit, page.Events[0].ID, "the newest events are kept")
	require.True(t, page.Events[0].Truncated)
	require.Len(t, page.Events[0].Actor, ControllerRecordFieldMaxBytes)
}

type controllerRecordsResponseClient struct {
	pb.DriverClient
	payload []byte
}

func (c controllerRecordsResponseClient) ControllerRecords(context.Context, *pb.ControllerRecordsRequest, ...grpc.CallOption) (*pb.ControllerRecordsResponse, error) {
	return &pb.ControllerRecordsResponse{Payload: c.payload}, nil
}

type controllerRecordsDevice struct {
	Device
	result ControllerRecordsResult
	got    *ControllerRecordsRequest
}

func (d controllerRecordsDevice) ControllerRecords(_ context.Context, req ControllerRecordsRequest) (ControllerRecordsResult, error) {
	*d.got = req
	return d.result, nil
}

func TestControllerRecordsPluginBoundaries(t *testing.T) {
	at := time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC)
	req := ControllerRecordsRequest{Kind: ControllerRecordsKindLogs, ControllerID: "ct1-b", Logs: &ControllerLogQuery{Limit: 5, MinSeverity: ControllerLogWarn, Component: "plant"}}
	result := ControllerRecordsResult{ControllerID: "ct1-b", ControllerIDs: []string{"ct1-a", "ct1-b"}, Logs: &ControllerLogSnapshot{
		Entries:  []ControllerLogEntry{{Time: &at, Severity: ControllerLogWarn, Component: "plant", Message: "pump <slow> & noisy"}},
		Coverage: ControllerLogInterrupted, Interruption: "stream reset",
	}}

	var got ControllerRecordsRequest
	server := &DriverGRPCServer{devices: map[string]Device{"ct1": controllerRecordsDevice{result: result, got: &got}}}
	data, err := EncodeControllerRecords(req)
	require.NoError(t, err)
	response, err := server.ControllerRecords(t.Context(), &pb.ControllerRecordsRequest{Ref: &pb.DeviceRef{DeviceId: "ct1"}, Payload: data})
	require.NoError(t, err)
	require.Equal(t, req, got)
	require.Contains(t, string(response.Payload), "pump <slow> & noisy", "HTML is not escaped, so the bounds hold")

	client := &DeviceGRPCClient{client: controllerRecordsResponseClient{payload: response.Payload}}
	decoded, err := client.ControllerRecords(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, result, decoded)

	_, err = client.ControllerRecords(t.Context(), logsRequest(0))
	require.Error(t, err)

	wrongKind := &DeviceGRPCClient{client: controllerRecordsResponseClient{payload: []byte(`{"audit":{"events":[],"limit_reached":false}}`)}}
	_, err = wrongKind.ControllerRecords(t.Context(), req)
	require.Error(t, err)

	// A newer driver's extra result field is ignored; requests stay strict.
	newerResult := &DeviceGRPCClient{client: controllerRecordsResponseClient{payload: []byte(`{"logs":{"entries":[],"coverage":"retained"},"source_clock":"ntp"}`)}}
	_, err = newerResult.ControllerRecords(t.Context(), req)
	require.NoError(t, err)
	_, err = DecodeControllerRecordsRequest([]byte(`{"kind":"logs","logs":{"limit":10},"sql":"select 1"}`))
	require.Error(t, err)

	_, err = server.ControllerRecords(t.Context(), &pb.ControllerRecordsRequest{Ref: &pb.DeviceRef{DeviceId: "ct1"}, Payload: []byte(`{"kind":"logs","logs":{"limit":0}}`)})
	require.Error(t, err)
}
