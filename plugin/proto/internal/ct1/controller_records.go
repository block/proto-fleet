package ct1

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// GetLogs follows the log after sending its retained tail and has no
// end-of-tail marker, so a read ends on the tail limit, an idle gap, or a
// deadline. These match the ProtoOS reader so both products report the same
// coverage for the same controller. Variables only so tests can shorten them.
var (
	logTailOpenTimeout = 2 * time.Second
	logTailIdleGap     = 750 * time.Millisecond
	logTailDeadline    = 5 * time.Second
	auditReadTimeout   = 10 * time.Second
)

var _ sdk.DeviceControllerRecords = (*Device)(nil)

// ControllerRecords reads one controller's own logs or audit events. With no
// controller ID it reads the controller HA selection prefers for reads.
func (d *Device) ControllerRecords(ctx context.Context, req sdk.ControllerRecordsRequest) (sdk.ControllerRecordsResult, error) {
	var result sdk.ControllerRecordsResult
	if err := req.Validate(); err != nil {
		return result, sdk.InvalidWriteRequest(err)
	}
	for _, c := range d.clients {
		if c.memberID != "" {
			result.ControllerIDs = append(result.ControllerIDs, c.memberID)
		}
	}
	c, err := d.recordsClient(ctx, req.ControllerID)
	if err != nil {
		return result, err
	}
	// Identity is best effort: records from an unconfirmed controller are still
	// evidence, so a stalled status call never holds them back. A controller
	// that reports a different identity than the one addressed is refused.
	identityCtx, cancelIdentity := context.WithTimeout(ctx, logTailDeadline)
	defer cancelIdentity()
	identity := make(chan string, 1)
	go func() {
		response, err := c.system.GetSystemStatus(identityCtx, &mdk.GetSystemStatusRequest{})
		if err != nil {
			identity <- ""
			return
		}
		identity <- response.GetStatus().GetServerId()
	}()

	switch req.Kind {
	case sdk.ControllerRecordsKindLogs:
		logs, err := readLogTail(ctx, c.system, req.Logs)
		if err != nil {
			return result, err
		}
		result.Logs = &logs
	case sdk.ControllerRecordsKindAudit:
		audit, err := listAuditEvents(ctx, c.system, req.Audit)
		if err != nil {
			return result, err
		}
		result.Audit = &audit
	}
	var id string
	select {
	case id = <-identity:
	case <-ctx.Done():
		return sdk.ControllerRecordsResult{}, fmt.Errorf("read CT1 controller identity: %w", ctx.Err())
	}
	if id != "" && c.memberID != "" && id != c.memberID {
		return sdk.ControllerRecordsResult{}, fmt.Errorf("read CT1 controller records: %w", status.Error(codes.FailedPrecondition, "CT1 controller identity changed"))
	}
	result.ControllerID = id
	return sdk.BoundControllerRecords(result), nil
}

func (d *Device) recordsClient(ctx context.Context, id string) (*Client, error) {
	if id == "" {
		return d.selectClient(ctx, false)
	}
	for _, c := range d.clients {
		if c.memberID == id {
			return c, nil
		}
	}
	return nil, sdk.InvalidWriteRequest(status.Error(codes.InvalidArgument, "unknown CT1 controller"))
}

var wireLevels = map[sdk.ControllerLogSeverity]mdk.LogLevel{
	sdk.ControllerLogDebug: mdk.LogLevel_LOG_LEVEL_DEBUG,
	sdk.ControllerLogInfo:  mdk.LogLevel_LOG_LEVEL_INFO,
	sdk.ControllerLogWarn:  mdk.LogLevel_LOG_LEVEL_WARNING,
	sdk.ControllerLogError: mdk.LogLevel_LOG_LEVEL_ERROR,
}

func toSeverity(level mdk.LogLevel) sdk.ControllerLogSeverity {
	switch level {
	case mdk.LogLevel_LOG_LEVEL_DEBUG:
		return sdk.ControllerLogDebug
	case mdk.LogLevel_LOG_LEVEL_WARNING:
		return sdk.ControllerLogWarn
	case mdk.LogLevel_LOG_LEVEL_ERROR:
		return sdk.ControllerLogError
	case mdk.LogLevel_LOG_LEVEL_UNSPECIFIED, mdk.LogLevel_LOG_LEVEL_INFO:
		return sdk.ControllerLogInfo
	default:
		return sdk.ControllerLogInfo
	}
}

type logStreamEnd int

const (
	logStreamLimit logStreamEnd = iota
	logStreamClosed
	logStreamPaused
	logStreamDeadline
	logStreamUnopened
)

type logRecv struct {
	entry *mdk.GetLogsResponse
	err   error
}

type logRecord struct {
	entry sdk.ControllerLogEntry
	size  int
}

// logCollection holds what a read has received, already bounded: each entry
// is truncated into its own storage as it arrives, so the controller's
// message is released, and the oldest entries are dropped once the total
// would exceed the result budget.
type logCollection struct {
	records  []logRecord
	size     int
	received int
	dropped  bool
}

func (c *logCollection) add(e *mdk.GetLogsResponse) {
	entry := sdk.ControllerLogEntry{Severity: toSeverity(e.GetLevel()), Component: e.GetComponent(), Message: e.GetMessage()}
	if e.GetTimestamp().IsValid() {
		at := e.GetTimestamp().AsTime().UTC()
		entry.Time = &at
	}
	var r logRecord
	r.entry, r.size = sdk.BoundControllerLogEntry(entry)
	c.records = append(c.records, r)
	c.size += r.size
	c.received++
	for c.size > sdk.ControllerRecordsBudget && len(c.records) > 1 {
		c.size -= c.records[0].size
		c.records[0] = logRecord{}
		c.records = c.records[1:]
		c.dropped = true
	}
}

func (c *logCollection) newest() *sdk.ControllerLogEntry {
	if len(c.records) == 0 {
		return nil
	}
	return &c.records[len(c.records)-1].entry
}

// readLogStream reads one GetLogs stream into got until it pauses, ends,
// fills the tail or reaches the deadline. Entries stay in got if the stream
// fails; the stream is always closed on return.
func readLogStream(ctx context.Context, system mdk.SystemServiceClient, req *mdk.GetLogsRequest, got *logCollection, openWait time.Duration, deadline time.Time) (logStreamEnd, error) {
	streamCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	stream, err := system.GetLogs(streamCtx, req)
	if err != nil {
		return 0, err
	}
	received := make(chan logRecv, 1)
	go func() {
		for {
			entry, err := stream.Recv()
			select {
			case received <- logRecv{entry, err}:
			case <-streamCtx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()
	limit := int(req.GetTail())
	// Headers arrive with the first entry, so a silent stream is "unopened"
	// rather than proof that nothing matches.
	wait := openWait
	first := true
	for limit <= 0 || got.received < limit {
		remaining := time.Until(deadline)
		timeout := min(wait, remaining)
		if timeout <= 0 {
			return logStreamDeadline, nil
		}
		timer := time.NewTimer(timeout)
		select {
		case <-ctx.Done():
			timer.Stop()
			return 0, fmt.Errorf("read CT1 controller logs: %w", ctx.Err())
		case <-timer.C:
			switch {
			case first:
				return logStreamUnopened, nil
			case timeout < wait:
				return logStreamDeadline, nil
			default:
				return logStreamPaused, nil
			}
		case r := <-received:
			timer.Stop()
			if errors.Is(r.err, io.EOF) {
				return logStreamClosed, nil
			}
			if r.err != nil {
				if streamCtx.Err() != nil && ctx.Err() == nil {
					// Our own deadline cancelled the stream mid-message.
					return logStreamDeadline, nil
				}
				return 0, r.err
			}
			got.add(r.entry)
		}
		first = false
		wait = logTailIdleGap
	}
	return logStreamLimit, nil
}

// sameLogEntry compares bounded entries: two entries that differ only past
// the message limit at the same time, severity and component look the same.
func sameLogEntry(a, b *sdk.ControllerLogEntry) bool {
	if a == nil || b == nil {
		return false
	}
	sameTime := a.Time == nil && b.Time == nil || a.Time != nil && b.Time != nil && a.Time.Equal(*b.Time)
	return sameTime && a.Severity == b.Severity && a.Component == b.Component && a.Message == b.Message && a.MessageTruncated == b.MessageTruncated
}

const missedNewest = "the controller's newest matching entry was not received"

// readLogTail reads a bounded tail. GetLogs has no end-of-tail marker, and a
// stall mid-tail looks like its end, so after an idle gap the tail counts as
// complete only if it reaches the controller's newest matching entry,
// confirmed by a tail-1 read. Otherwise it is read once more.
func readLogTail(ctx context.Context, system mdk.SystemServiceClient, q *sdk.ControllerLogQuery) (sdk.ControllerLogSnapshot, error) {
	req := &mdk.GetLogsRequest{Tail: q.Limit, Component: q.Component}
	if q.MinSeverity != "" {
		req.Level = wireLevels[q.MinSeverity]
	}
	deadline := time.Now().Add(logTailDeadline)
	var entries logCollection
	snapshot := func(got *logCollection, interruption string) sdk.ControllerLogSnapshot {
		s := sdk.ControllerLogSnapshot{Entries: make([]sdk.ControllerLogEntry, 0, len(got.records)), Interruption: interruption}
		for _, r := range got.records {
			s.Entries = append(s.Entries, r.entry)
		}
		switch {
		case interruption != "":
			s.Coverage = sdk.ControllerLogInterrupted
		// Dropped entries were the oldest, as when the tail fills.
		case got.received >= int(q.Limit) || got.dropped:
			s.Coverage = sdk.ControllerLogLimitReached
		default:
			s.Coverage = sdk.ControllerLogRetained
		}
		return s
	}
	failed := func(err error) (sdk.ControllerLogSnapshot, error) {
		if ctx.Err() != nil {
			return sdk.ControllerLogSnapshot{}, ctx.Err()
		}
		// With no entries there is nothing partial to show.
		if len(entries.records) == 0 {
			return sdk.ControllerLogSnapshot{}, err
		}
		return snapshot(&entries, interruptionReason(err)), nil
	}

	end, err := readLogStream(ctx, system, req, &entries, logTailOpenTimeout, deadline)
	if err != nil {
		return failed(err)
	}
	switch end {
	case logStreamUnopened:
		return sdk.ControllerLogSnapshot{Entries: []sdk.ControllerLogEntry{}, Coverage: sdk.ControllerLogNoResponse}, nil
	case logStreamDeadline:
		return snapshot(&entries, fmt.Sprintf("the controller was still sending after %d seconds", int(logTailDeadline/time.Second))), nil
	case logStreamLimit, logStreamClosed:
		return snapshot(&entries, ""), nil
	case logStreamPaused:
		// The stream went quiet: confirm below that it sent the newest entry.
	}
	// Follow-up reads get whatever time is left rather than the open timeout.
	var latest logCollection
	confirm := &mdk.GetLogsRequest{Tail: 1, Level: req.Level, Component: req.Component}
	if _, err := readLogStream(ctx, system, confirm, &latest, time.Until(deadline), deadline); err != nil {
		return failed(err)
	}
	newest := latest.newest()
	if newest == nil {
		return snapshot(&entries, "the controller did not confirm its newest entry"), nil
	}
	if sameLogEntry(newest, entries.newest()) {
		return snapshot(&entries, ""), nil
	}
	// A stall, or entries logged since the first read: read the tail again.
	var again logCollection
	if _, err := readLogStream(ctx, system, req, &again, time.Until(deadline), deadline); err != nil {
		return failed(err)
	}
	for i := range again.records {
		if sameLogEntry(&again.records[i].entry, newest) {
			return snapshot(&again, ""), nil
		}
	}
	if len(again.records) > 0 {
		return snapshot(&again, missedNewest), nil
	}
	return snapshot(&entries, missedNewest), nil
}

func interruptionReason(err error) string {
	if s, ok := status.FromError(err); ok && s.Message() != "" {
		reason, _ := sdk.TruncateJSONText("the stream failed: "+s.Message(), sdk.ControllerAuditErrorMaxBytes)
		return reason
	}
	return "the stream failed"
}

func listAuditEvents(ctx context.Context, system mdk.SystemServiceClient, q *sdk.ControllerAuditQuery) (sdk.ControllerAuditPage, error) {
	ctx, cancel := context.WithTimeout(ctx, auditReadTimeout)
	defer cancel()
	response, err := system.ListAuditEvents(ctx, &mdk.ListAuditEventsRequest{
		Range:  &mdk.TimeRange{Start: timestamppb.New(q.Start), End: timestamppb.New(q.End)},
		Limit:  q.Limit,
		Result: q.Result,
	})
	if err != nil {
		return sdk.ControllerAuditPage{}, err
	}
	wire := response.GetEvents()
	page := sdk.ControllerAuditPage{Events: make([]sdk.ControllerAuditEvent, 0, min(len(wire), int(q.Limit))), LimitReached: len(wire) >= int(q.Limit)}
	for _, e := range wire[:min(len(wire), int(q.Limit))] {
		event := sdk.ControllerAuditEvent{ID: e.GetId(), Actor: e.GetActor(), Source: e.GetSource(), Node: e.GetNode(), Action: e.GetAction(), Target: e.GetTarget(), Result: e.GetResult(), Error: e.GetError(), DetailsJSON: e.GetDetailsJson()}
		if e.GetTime().IsValid() {
			at := e.GetTime().AsTime().UTC()
			event.Time = &at
		}
		page.Events = append(page.Events, event)
	}
	return page, nil
}
