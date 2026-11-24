package sdk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"
)

// CapabilityControllerRecords marks drivers that read a controller's own logs
// and audit events on demand.
const CapabilityControllerRecords = "controller_records"

const (
	ControllerRecordsKindLogs  = "logs"
	ControllerRecordsKindAudit = "audit"

	// ControllerRecordsLimit is the controller's own cap on a log tail and an
	// audit page.
	ControllerRecordsLimit = 1000
	// ControllerAuditWindowMax is the longest audit window a controller accepts.
	ControllerAuditWindowMax = 7 * 24 * time.Hour

	// Relayed text is bounded by its JSON-encoded length so a result always
	// fits a plugin and Fleet Node response, whatever the controller sends.
	ControllerLogMessageMaxBytes  = 4096
	ControllerAuditDetailMaxBytes = 4096
	ControllerAuditErrorMaxBytes  = 1024
	ControllerRecordFieldMaxBytes = 256
	// controllerRecordsBudget leaves headroom under the 4 MiB gRPC message
	// default between Fleet and a plugin.
	controllerRecordsBudget       = 3 << 20
	controllerRecordsPayloadLimit = controllerRecordsBudget + 64<<10
	controllerRecordsRequestLimit = 4096
	controllerIDMaxBytes          = 255
	controllerComponentMaxBytes   = 128
)

type ControllerLogSeverity string

const (
	ControllerLogDebug ControllerLogSeverity = "debug"
	ControllerLogInfo  ControllerLogSeverity = "info"
	ControllerLogWarn  ControllerLogSeverity = "warn"
	ControllerLogError ControllerLogSeverity = "error"
)

func (s ControllerLogSeverity) valid() bool {
	switch s {
	case ControllerLogDebug, ControllerLogInfo, ControllerLogWarn, ControllerLogError:
		return true
	}
	return false
}

// ControllerLogCoverage says how much of the controller's retained log a read
// covers. A controller keeps only a bounded in-memory tail, so no read is a
// complete history.
type ControllerLogCoverage string

const (
	// Fewer than the limit came back: everything retained for the filters.
	ControllerLogRetained ControllerLogCoverage = "retained"
	// The limit was reached, or older entries were dropped to fit the
	// response; older retained entries were not returned.
	ControllerLogLimitReached ControllerLogCoverage = "limit_reached"
	// The read stopped before the tail was known to be finished.
	ControllerLogInterrupted ControllerLogCoverage = "interrupted"
	// The controller sent nothing, so an empty log and a slow controller look
	// the same.
	ControllerLogNoResponse ControllerLogCoverage = "no_response"
)

// ControllerRecordsRequest is one closed, bounded read of a controller's own
// records; never a raw controller call.
type ControllerRecordsRequest struct {
	Kind string `json:"kind"`
	// Server ID of one of the device's controllers; empty selects the
	// controller the driver prefers for reads.
	ControllerID string                `json:"controller_id,omitempty"`
	Logs         *ControllerLogQuery   `json:"logs,omitempty"`
	Audit        *ControllerAuditQuery `json:"audit,omitempty"`
}

type ControllerLogQuery struct {
	Limit int32 `json:"limit"`
	// Lowest severity to include; empty applies the controller default (info).
	MinSeverity ControllerLogSeverity `json:"min_severity,omitempty"`
	// Exact component name; empty means every component.
	Component string `json:"component,omitempty"`
}

type ControllerAuditQuery struct {
	Start  time.Time `json:"start"`
	End    time.Time `json:"end"`
	Limit  int32     `json:"limit"`
	Result string    `json:"result,omitempty"`
}

type ControllerRecordsResult struct {
	// Server ID of the controller that answered, when it reported one.
	ControllerID string `json:"controller_id,omitempty"`
	// Server IDs of every controller of the device.
	ControllerIDs []string               `json:"controller_ids,omitempty"`
	Logs          *ControllerLogSnapshot `json:"logs,omitempty"`
	Audit         *ControllerAuditPage   `json:"audit,omitempty"`
}

type ControllerLogSnapshot struct {
	// Oldest first, as the controller sent them.
	Entries      []ControllerLogEntry  `json:"entries"`
	Coverage     ControllerLogCoverage `json:"coverage"`
	Interruption string                `json:"interruption,omitempty"`
}

type ControllerLogEntry struct {
	// Controller-recorded time; nil when the controller sent none.
	Time             *time.Time            `json:"time,omitempty"`
	Severity         ControllerLogSeverity `json:"severity"`
	Component        string                `json:"component,omitempty"`
	Message          string                `json:"message"`
	MessageTruncated bool                  `json:"message_truncated,omitempty"`
}

type ControllerAuditPage struct {
	// Newest first, as the controller sent them.
	Events []ControllerAuditEvent `json:"events"`
	// The page is full, or older events were dropped to fit the response.
	LimitReached bool `json:"limit_reached"`
}

type ControllerAuditEvent struct {
	ID          int64      `json:"id"`
	Time        *time.Time `json:"time,omitempty"`
	Actor       string     `json:"actor,omitempty"`
	Source      string     `json:"source,omitempty"`
	Node        string     `json:"node,omitempty"`
	Action      string     `json:"action,omitempty"`
	Target      string     `json:"target,omitempty"`
	Result      string     `json:"result,omitempty"`
	Error       string     `json:"error,omitempty"`
	DetailsJSON string     `json:"details_json,omitempty"`
	Truncated   bool       `json:"truncated,omitempty"`
}

// DeviceControllerRecords reads a controller's own logs or audit events. A
// result is never presented as more complete than the read was; drivers bound
// it with BoundControllerRecords before returning it.
type DeviceControllerRecords interface {
	ControllerRecords(ctx context.Context, req ControllerRecordsRequest) (ControllerRecordsResult, error)
}

func (r ControllerRecordsRequest) Validate() error {
	if len(r.ControllerID) > controllerIDMaxBytes {
		return fmt.Errorf("controller id too long")
	}
	switch r.Kind {
	case ControllerRecordsKindLogs:
		if r.Logs == nil || r.Audit != nil {
			return fmt.Errorf("log query required")
		}
		q := r.Logs
		if q.Limit < 1 || q.Limit > ControllerRecordsLimit {
			return fmt.Errorf("log limit must be between 1 and %d", ControllerRecordsLimit)
		}
		if q.MinSeverity != "" && !q.MinSeverity.valid() {
			return fmt.Errorf("unsupported log severity")
		}
		if len(q.Component) > controllerComponentMaxBytes {
			return fmt.Errorf("component too long")
		}
	case ControllerRecordsKindAudit:
		if r.Audit == nil || r.Logs != nil {
			return fmt.Errorf("audit query required")
		}
		q := r.Audit
		if q.Start.IsZero() || q.End.IsZero() || !q.Start.Before(q.End) {
			return fmt.Errorf("audit window must start before it ends")
		}
		if q.End.Sub(q.Start) > ControllerAuditWindowMax {
			return fmt.Errorf("audit window must be at most seven days")
		}
		if q.Limit < 1 || q.Limit > ControllerRecordsLimit {
			return fmt.Errorf("audit limit must be between 1 and %d", ControllerRecordsLimit)
		}
		if q.Result != "" && q.Result != "ok" && q.Result != "error" {
			return fmt.Errorf("audit result filter must be ok or error")
		}
	default:
		return fmt.Errorf("unsupported controller records operation")
	}
	return nil
}

// Validate checks a result against its request, so a misbehaving driver cannot
// relay an unbounded or mislabeled read.
func (r ControllerRecordsResult) Validate(req ControllerRecordsRequest) error {
	if len(r.ControllerID) > controllerIDMaxBytes || len(r.ControllerIDs) > 2 {
		return fmt.Errorf("invalid controller identity")
	}
	for _, id := range r.ControllerIDs {
		if id == "" || len(id) > controllerIDMaxBytes {
			return fmt.Errorf("invalid controller identity")
		}
	}
	if req.ControllerID != "" && r.ControllerID != "" && r.ControllerID != req.ControllerID {
		return fmt.Errorf("controller records came from another controller")
	}
	switch req.Kind {
	case ControllerRecordsKindLogs:
		if r.Logs == nil || r.Audit != nil {
			return fmt.Errorf("invalid controller log result")
		}
		return r.Logs.validate(req.Logs.Limit)
	case ControllerRecordsKindAudit:
		if r.Audit == nil || r.Logs != nil {
			return fmt.Errorf("invalid controller audit result")
		}
		return r.Audit.validate(req.Audit.Limit)
	}
	return fmt.Errorf("unsupported controller records result")
}

func (s ControllerLogSnapshot) validate(limit int32) error {
	if len(s.Entries) > int(limit) {
		return fmt.Errorf("controller log result exceeds its limit")
	}
	switch s.Coverage {
	case ControllerLogRetained, ControllerLogLimitReached, ControllerLogNoResponse:
		if s.Interruption != "" {
			return fmt.Errorf("interruption reason without an interrupted read")
		}
	case ControllerLogInterrupted:
		if s.Interruption == "" || len(s.Interruption) > ControllerAuditErrorMaxBytes {
			return fmt.Errorf("interrupted read needs a bounded reason")
		}
	default:
		return fmt.Errorf("invalid controller log coverage")
	}
	if s.Coverage == ControllerLogNoResponse && len(s.Entries) > 0 {
		return fmt.Errorf("no-response read with entries")
	}
	for _, e := range s.Entries {
		if !e.Severity.valid() || jsonTextLen(e.Message) > ControllerLogMessageMaxBytes || jsonTextLen(e.Component) > controllerComponentMaxBytes {
			return fmt.Errorf("invalid controller log entry")
		}
	}
	return nil
}

func (p ControllerAuditPage) validate(limit int32) error {
	if len(p.Events) > int(limit) {
		return fmt.Errorf("controller audit result exceeds its limit")
	}
	for _, e := range p.Events {
		for _, field := range []string{e.Actor, e.Source, e.Node, e.Action, e.Target, e.Result} {
			if jsonTextLen(field) > ControllerRecordFieldMaxBytes {
				return fmt.Errorf("invalid controller audit event")
			}
		}
		if jsonTextLen(e.Error) > ControllerAuditErrorMaxBytes || jsonTextLen(e.DetailsJSON) > ControllerAuditDetailMaxBytes {
			return fmt.Errorf("invalid controller audit event")
		}
	}
	return nil
}

// ControllerRecordsBudget is the encoded size a result's records may take.
// Readers that collect records over time hold at most this much.
const ControllerRecordsBudget = controllerRecordsBudget

// BoundControllerLogEntry truncates one entry's relayed text into its own
// storage, so the controller's original message can be released, and returns
// the entry's encoded size.
func BoundControllerLogEntry(e ControllerLogEntry) (ControllerLogEntry, int) {
	var cut bool
	e.Message, cut = TruncateJSONText(e.Message, ControllerLogMessageMaxBytes)
	e.Message = strings.Clone(e.Message)
	e.MessageTruncated = e.MessageTruncated || cut
	e.Component, _ = TruncateJSONText(e.Component, controllerComponentMaxBytes)
	e.Component = strings.Clone(e.Component)
	return e, encodedLen(e)
}

// BoundControllerRecords truncates relayed text and then drops the oldest
// records until the encoded result fits the response budget. Dropped records
// are reported the way a full page is: older records were not returned.
func BoundControllerRecords(r ControllerRecordsResult) ControllerRecordsResult {
	if r.Logs != nil {
		logs := *r.Logs
		logs.Entries = make([]ControllerLogEntry, len(r.Logs.Entries))
		size := 0
		for i, e := range r.Logs.Entries {
			var n int
			logs.Entries[i], n = BoundControllerLogEntry(e)
			size += n
		}
		drop := 0
		for size > controllerRecordsBudget && drop < len(logs.Entries) {
			size -= encodedLen(logs.Entries[drop])
			drop++
		}
		if drop > 0 {
			logs.Entries = logs.Entries[drop:]
			if logs.Coverage == ControllerLogRetained {
				logs.Coverage = ControllerLogLimitReached
			}
		}
		r.Logs = &logs
	}
	if r.Audit != nil {
		audit := *r.Audit
		audit.Events = make([]ControllerAuditEvent, len(r.Audit.Events))
		size := 0
		for i, e := range r.Audit.Events {
			cut := false
			for _, field := range []*string{&e.Actor, &e.Source, &e.Node, &e.Action, &e.Target, &e.Result} {
				var c bool
				*field, c = TruncateJSONText(*field, ControllerRecordFieldMaxBytes)
				cut = cut || c
			}
			var c1, c2 bool
			e.Error, c1 = TruncateJSONText(e.Error, ControllerAuditErrorMaxBytes)
			e.DetailsJSON, c2 = TruncateJSONText(e.DetailsJSON, ControllerAuditDetailMaxBytes)
			e.Truncated = e.Truncated || cut || c1 || c2
			audit.Events[i] = e
			size += encodedLen(e)
		}
		keep := len(audit.Events)
		for size > controllerRecordsBudget && keep > 0 {
			keep--
			size -= encodedLen(audit.Events[keep])
		}
		if keep < len(audit.Events) {
			audit.Events = audit.Events[:keep]
			audit.LimitReached = true
		}
		r.Audit = &audit
	}
	return r
}

// TruncateJSONText cuts s at a rune boundary so its JSON string encoding
// (without quotes or HTML escaping) is at most limit bytes.
func TruncateJSONText(s string, limit int) (string, bool) {
	n := 0
	for i, r := range s {
		w := jsonRuneLen(r, s[i:])
		if n+w > limit {
			return s[:i], true
		}
		n += w
	}
	return s, false
}

func jsonTextLen(s string) int {
	n := 0
	for i, r := range s {
		n += jsonRuneLen(r, s[i:])
	}
	return n
}

func jsonRuneLen(r rune, rest string) int {
	switch {
	case r == '"' || r == '\\' || r == '\n' || r == '\r' || r == '\t' || r == '\b' || r == '\f':
		return 2
	case r < 0x20 || r == '\u2028' || r == '\u2029':
		return 6
	case r == utf8.RuneError:
		if _, size := utf8.DecodeRuneInString(rest); size == 1 {
			return 6 // invalid byte, encoded as �
		}
	}
	return utf8.RuneLen(r)
}

// encodedLen treats a value that cannot be encoded as over every budget.
func encodedLen(v any) int {
	data, err := EncodeControllerRecords(v)
	if err != nil {
		return controllerRecordsPayloadLimit + 1
	}
	return len(data)
}

// EncodeControllerRecords encodes a request or result without HTML escaping,
// matching the bounds TruncateJSONText enforces.
func EncodeControllerRecords(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, fmt.Errorf("encode controller records: %w", err)
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// decodeControllerRecords rejects unknown fields only when strict. Requests are
// strict so a filter an older driver does not understand fails rather than
// being ignored; results are lenient so a newer driver or Fleet Node that adds
// a field does not break reads on an older server. Size and Validate bound
// both either way.
func decodeControllerRecords(data []byte, limit int, strict bool, out any) error {
	if len(data) > limit {
		return fmt.Errorf("controller records payload too large")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	if strict {
		dec.DisallowUnknownFields()
	}
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("invalid controller records payload: %w", err)
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("trailing controller records payload")
	}
	return nil
}

func DecodeControllerRecordsRequest(data []byte) (ControllerRecordsRequest, error) {
	var r ControllerRecordsRequest
	if err := decodeControllerRecords(data, controllerRecordsRequestLimit, true, &r); err != nil {
		return r, err
	}
	return r, r.Validate()
}

func DecodeControllerRecordsResult(data []byte, req ControllerRecordsRequest) (ControllerRecordsResult, error) {
	var r ControllerRecordsResult
	if err := decodeControllerRecords(data, controllerRecordsPayloadLimit, false, &r); err != nil {
		return r, err
	}
	return r, r.Validate(req)
}
