package logging

import (
	"context"
	"fmt"
	"log/slog"
	"runtime"
	"strings"
	"sync"
	"time"
)

// DefaultBufferCapacity is the number of records retained in the in-process
// ring buffer when Init constructs a default Buffer. 1000 is enough for
// minutes of logs at typical fleetd verbosity without measurable memory cost.
const DefaultBufferCapacity = 1000

// BufferedRecord is a captured slog.Record snapshot, suitable for serving to
// log-viewer clients without holding any references back into slog internals.
type BufferedRecord struct {
	// ID is monotonic and strictly increasing for the lifetime of the
	// process. The ring buffer uses it as both an identifier and a cursor.
	ID    uint64
	Time  time.Time
	Level slog.Level
	// Message is the record's free-text body (slog's first positional arg).
	Message string
	// Attrs are pre-rendered as ordered key/value strings. We flatten any
	// nested Groups so the wire format stays trivially serializable.
	Attrs []KeyValue
	// Source is "file:line" if the original handler captured it; otherwise "".
	Source string
}

// KeyValue is one rendered attribute on a buffered record.
type KeyValue struct {
	Key   string
	Value string
}

// Buffer is a thread-safe ring buffer of recent slog records.
//
// It satisfies slog.Handler so it can be installed alongside (or instead of)
// the regular text/JSON handler via slog.New. In normal use the package
// composes both via teeHandler so writes to stdout still happen.
//
// Lookups (Snapshot / SnapshotSince) take a read-lock and copy out matching
// records. The buffer is not designed for high-throughput querying — it's
// intended for occasional polling from a single operator UI.
type Buffer struct {
	mu       sync.RWMutex
	capacity int
	records  []BufferedRecord // length grows up to capacity, then overwrites
	head     int              // index of the next slot to write
	full     bool             // true once we've wrapped at least once

	// nextID is the ID assigned to the next inserted record. Held under
	// mu (not atomic) so ID assignment and physical insertion stay in
	// lockstep — important so iterating the ring in physical order also
	// yields IDs in monotonic order.
	nextID uint64

	// minLevel is the inclusive floor below which records are dropped.
	minLevel slog.Level
}

// NewBuffer returns an empty buffer with the given capacity.
//
// capacity must be > 0; values <= 0 fall back to DefaultBufferCapacity.
// minLevel filters out records below it before they even hit the ring; this
// keeps Debug noise from displacing useful entries when the operator has the
// global level set higher.
func NewBuffer(capacity int, minLevel slog.Level) *Buffer {
	if capacity <= 0 {
		capacity = DefaultBufferCapacity
	}
	return &Buffer{
		capacity: capacity,
		records:  make([]BufferedRecord, 0, capacity),
		minLevel: minLevel,
	}
}

// Capacity returns the configured ring size.
func (b *Buffer) Capacity() int { return b.capacity }

// Enabled reports whether the buffer captures records at the given level.
func (b *Buffer) Enabled(_ context.Context, level slog.Level) bool {
	return level >= b.minLevel
}

// Handle stores the record in the ring buffer. It never returns an error —
// dropping a log record into an in-memory buffer can't fail in any way the
// caller can act on.
//
// We do the (potentially slow) attribute rendering before acquiring the
// lock so concurrent loggers don't serialize on each other. ID assignment
// and the physical insert both happen under the lock so they stay paired.
func (b *Buffer) Handle(_ context.Context, r slog.Record) error {
	rec := BufferedRecord{
		Time:    r.Time,
		Level:   r.Level,
		Message: r.Message,
		Attrs:   collectAttrs(r),
		Source:  formatSource(r),
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	b.nextID++
	rec.ID = b.nextID

	if !b.full && len(b.records) < b.capacity {
		b.records = append(b.records, rec)
		b.head = len(b.records) % b.capacity
		if len(b.records) == b.capacity {
			b.full = true
		}
		return nil
	}

	// Wrapped: overwrite the oldest slot.
	b.records[b.head] = rec
	b.head = (b.head + 1) % b.capacity
	return nil
}

// WithAttrs and WithGroup return the same buffer — the buffer doesn't carry
// per-logger state, since the records it captures already include any attrs
// added upstream by the time slog calls Handle.
func (b *Buffer) WithAttrs(_ []slog.Attr) slog.Handler { return b }
func (b *Buffer) WithGroup(_ string) slog.Handler      { return b }

// SnapshotOptions narrows what Snapshot returns.
type SnapshotOptions struct {
	// SinceID returns only records whose ID is strictly greater than this.
	// Zero means "from the start of what's still buffered."
	SinceID uint64

	// MinLevel inclusive. If equal to slog.LevelDebug-1 (i.e. zero default
	// is fine for "everything"), no filter is applied.
	MinLevel slog.Level

	// Search is a case-insensitive substring match against Message.
	// Empty disables the filter.
	Search string

	// Limit caps the number of records returned (oldest-first).
	// Zero means "no cap"; callers should always pass a sane upper bound.
	Limit int
}

// SnapshotResult bundles the matching records with cursor info the client
// needs to keep tailing.
type SnapshotResult struct {
	Records []BufferedRecord
	// LatestID is the ID of the most recent buffered record at snapshot
	// time, regardless of whether it matched the filter. Clients use this
	// as the next SinceID to avoid missing records that didn't pass the
	// filter but still advanced the cursor.
	LatestID uint64
	// Size is the count of records currently in the ring (filter-agnostic).
	Size int
	// Truncated is true if Limit clipped the result.
	Truncated bool
}

// Snapshot returns matching records oldest-first.
//
// We materialize a copy under the lock so callers can't observe in-progress
// writes from the slog goroutines.
func (b *Buffer) Snapshot(opts SnapshotOptions) SnapshotResult {
	b.mu.RLock()
	defer b.mu.RUnlock()

	res := SnapshotResult{Size: len(b.records)}
	if len(b.records) == 0 {
		return res
	}

	// Iterate oldest-to-newest. When the ring has wrapped, that means
	// starting at head; otherwise records are in insertion order.
	start := 0
	if b.full {
		start = b.head
	}
	needle := strings.ToLower(opts.Search)

	for i := range len(b.records) {
		idx := (start + i) % len(b.records)
		r := b.records[idx]
		if r.ID > res.LatestID {
			res.LatestID = r.ID
		}
		if r.ID <= opts.SinceID {
			continue
		}
		if r.Level < opts.MinLevel {
			continue
		}
		if needle != "" && !strings.Contains(strings.ToLower(r.Message), needle) {
			continue
		}
		if opts.Limit > 0 && len(res.Records) == opts.Limit {
			res.Truncated = true
			// Don't break — we still want LatestID to reflect the true
			// newest record so the client cursor advances correctly even
			// when its limit clips the visible window.
			continue
		}
		res.Records = append(res.Records, r)
	}
	return res
}

// collectAttrs flattens slog's Attrs (including Group nesting) into a flat
// list of "key=value" strings. We render values via fmt to avoid sending
// arbitrary types over the wire.
func collectAttrs(r slog.Record) []KeyValue {
	if r.NumAttrs() == 0 {
		return nil
	}
	out := make([]KeyValue, 0, r.NumAttrs())
	r.Attrs(func(a slog.Attr) bool {
		appendAttr(&out, "", a)
		return true
	})
	return out
}

func appendAttr(out *[]KeyValue, prefix string, a slog.Attr) {
	a.Value = a.Value.Resolve()
	key := a.Key
	if prefix != "" {
		key = prefix + "." + a.Key
	}
	if a.Value.Kind() == slog.KindGroup {
		for _, sub := range a.Value.Group() {
			appendAttr(out, key, sub)
		}
		return
	}
	*out = append(*out, KeyValue{Key: key, Value: fmt.Sprintf("%v", a.Value.Any())})
}

// formatSource returns "file:line" if the slog record carries a PC, else "".
//
// We deliberately don't follow slog.Source's full format (which includes the
// function name) — for an operator log viewer, file:line is enough and the
// shorter string keeps the response small.
func formatSource(r slog.Record) string {
	if r.PC == 0 {
		return ""
	}
	frames := runtime.CallersFrames([]uintptr{r.PC})
	frame, _ := frames.Next()
	if frame.File == "" {
		return ""
	}
	// Trim to a short suffix (last two path components) to keep the wire
	// representation compact while staying useful for navigation.
	file := frame.File
	if i := strings.LastIndexByte(file, '/'); i >= 0 {
		if j := strings.LastIndexByte(file[:i], '/'); j >= 0 {
			file = file[j+1:]
		}
	}
	return fmt.Sprintf("%s:%d", file, frame.Line)
}
