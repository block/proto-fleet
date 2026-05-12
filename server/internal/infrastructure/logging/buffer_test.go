package logging

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// emit pushes a record through the buffer the way slog would.
func emit(t *testing.T, b *Buffer, level slog.Level, msg string, attrs ...slog.Attr) {
	t.Helper()
	r := slog.NewRecord(time.Now(), level, msg, 0)
	r.AddAttrs(attrs...)
	if err := b.Handle(context.Background(), r); err != nil {
		t.Fatalf("Handle returned err: %v", err)
	}
}

func TestBuffer_KeepsOrderUntilWrap(t *testing.T) {
	b := NewBuffer(3, slog.LevelDebug)
	emit(t, b, slog.LevelInfo, "one")
	emit(t, b, slog.LevelInfo, "two")
	emit(t, b, slog.LevelInfo, "three")

	res := b.Snapshot(SnapshotOptions{Limit: 10})
	if got, want := len(res.Records), 3; got != want {
		t.Fatalf("len=%d want %d", got, want)
	}
	if res.Records[0].Message != "one" || res.Records[2].Message != "three" {
		t.Fatalf("unexpected order: %+v", res.Records)
	}
	if res.LatestID != 3 {
		t.Errorf("LatestID=%d want 3", res.LatestID)
	}
}

func TestBuffer_WrapsAndDropsOldest(t *testing.T) {
	b := NewBuffer(2, slog.LevelDebug)
	emit(t, b, slog.LevelInfo, "one")
	emit(t, b, slog.LevelInfo, "two")
	emit(t, b, slog.LevelInfo, "three")

	res := b.Snapshot(SnapshotOptions{Limit: 10})
	if got, want := len(res.Records), 2; got != want {
		t.Fatalf("len=%d want %d", got, want)
	}
	if res.Records[0].Message != "two" || res.Records[1].Message != "three" {
		t.Errorf("expected newest two records, got %+v", res.Records)
	}
}

func TestBuffer_SinceIDFiltersAlreadySeen(t *testing.T) {
	b := NewBuffer(10, slog.LevelDebug)
	emit(t, b, slog.LevelInfo, "one")
	emit(t, b, slog.LevelInfo, "two")
	emit(t, b, slog.LevelInfo, "three")

	res := b.Snapshot(SnapshotOptions{SinceID: 2, Limit: 10})
	if got, want := len(res.Records), 1; got != want {
		t.Fatalf("len=%d want %d", got, want)
	}
	if res.Records[0].Message != "three" {
		t.Errorf("got %q want three", res.Records[0].Message)
	}
}

func TestBuffer_LevelFilterDropsBelowMinimum(t *testing.T) {
	b := NewBuffer(10, slog.LevelDebug)
	emit(t, b, slog.LevelDebug, "dbg")
	emit(t, b, slog.LevelInfo, "info")
	emit(t, b, slog.LevelWarn, "warn")

	res := b.Snapshot(SnapshotOptions{MinLevel: slog.LevelInfo, Limit: 10})
	if got, want := len(res.Records), 2; got != want {
		t.Fatalf("len=%d want %d", got, want)
	}
	for _, r := range res.Records {
		if r.Message == "dbg" {
			t.Errorf("debug record leaked through Info filter")
		}
	}
}

func TestBuffer_SearchIsCaseInsensitive(t *testing.T) {
	b := NewBuffer(10, slog.LevelDebug)
	emit(t, b, slog.LevelInfo, "Pairing succeeded")
	emit(t, b, slog.LevelInfo, "telemetry tick")

	res := b.Snapshot(SnapshotOptions{Search: "PAIRING", Limit: 10})
	if got, want := len(res.Records), 1; got != want {
		t.Fatalf("len=%d want %d", got, want)
	}
	if !strings.Contains(res.Records[0].Message, "Pairing") {
		t.Errorf("unexpected match: %q", res.Records[0].Message)
	}
}

func TestBuffer_LimitTruncatesAndCapsLatestID(t *testing.T) {
	b := NewBuffer(10, slog.LevelDebug)
	for range 5 {
		emit(t, b, slog.LevelInfo, "msg")
	}

	res := b.Snapshot(SnapshotOptions{Limit: 2})
	if got, want := len(res.Records), 2; got != want {
		t.Fatalf("len=%d want %d", got, want)
	}
	if !res.Truncated {
		t.Errorf("expected Truncated=true")
	}
	// LatestID must NOT advance past the records we actually returned,
	// otherwise a resume poll with since_id=LatestID would silently skip
	// the matches that were clipped by the limit (records 3..5 here).
	if got, want := res.LatestID, uint64(2); got != want {
		t.Errorf("LatestID=%d want %d (capped at last returned record)", got, want)
	}
	// Sanity: LatestID equals the ID of the last returned record.
	if last := res.Records[len(res.Records)-1].ID; res.LatestID != last {
		t.Errorf("LatestID=%d does not match last returned record ID=%d", res.LatestID, last)
	}
}

// TestBuffer_TruncatedResumeReturnsClippedMatches exercises the contract
// the proto documents: poll once with a Limit, get truncated=true, then poll
// again with since_id=latest_id and receive every matching record that didn't
// fit the first time. This is a regression test for the bug where LatestID
// was advanced past clipped matches, causing them to be silently skipped on
// resume.
func TestBuffer_TruncatedResumeReturnsClippedMatches(t *testing.T) {
	b := NewBuffer(20, slog.LevelDebug)
	for range 5 {
		emit(t, b, slog.LevelInfo, "msg")
	}

	// Poll #1: ask for 2, get the oldest two and Truncated=true.
	first := b.Snapshot(SnapshotOptions{Limit: 2})
	if !first.Truncated {
		t.Fatalf("expected first poll to be truncated")
	}
	if got := len(first.Records); got != 2 {
		t.Fatalf("first poll len=%d want 2", got)
	}
	if first.Records[0].ID != 1 || first.Records[1].ID != 2 {
		t.Fatalf("first poll IDs want [1 2]")
	}

	// Poll #2: resume from the cursor the server told us to use. We must
	// see records 3, 4, 5 in order — none of them silently dropped.
	second := b.Snapshot(SnapshotOptions{SinceID: first.LatestID, Limit: 10})
	if got := len(second.Records); got != 3 {
		t.Fatalf("second poll len=%d want 3 (records 3,4,5)", got)
	}
	for i, want := range []uint64{3, 4, 5} {
		if got := second.Records[i].ID; got != want {
			t.Errorf("second poll record %d: ID=%d want %d", i, got, want)
		}
	}
	if second.Truncated {
		t.Errorf("second poll should not be truncated")
	}
}

// TestBuffer_TruncatedLatestIDSkipsFilteredOutTail verifies that when the
// limit is hit, LatestID may include trailing non-matching records that
// preceded the clipped match (which is harmless — the client filters them
// out anyway), but never advances past the clipped match itself.
func TestBuffer_TruncatedLatestIDSkipsFilteredOutTail(t *testing.T) {
	b := NewBuffer(10, slog.LevelDebug)
	emit(t, b, slog.LevelInfo, "match")  // id=1
	emit(t, b, slog.LevelInfo, "match")  // id=2
	emit(t, b, slog.LevelDebug, "noise") // id=3 — filtered out
	emit(t, b, slog.LevelInfo, "match")  // id=4 — clipped by limit

	res := b.Snapshot(SnapshotOptions{MinLevel: slog.LevelInfo, Limit: 2})
	if !res.Truncated {
		t.Fatalf("expected Truncated=true")
	}
	// LatestID can be 2 (last returned) or 3 (the filtered-out record
	// AFTER the last returned match) — both leave the client able to see
	// record 4 on resume. It must NOT be 4, which would skip the clipped
	// match.
	if res.LatestID >= 4 {
		t.Errorf("LatestID=%d advanced past clipped match (id=4)", res.LatestID)
	}
}

func TestBuffer_AttrsAreFlattened(t *testing.T) {
	b := NewBuffer(5, slog.LevelDebug)
	emit(t, b, slog.LevelInfo, "with attrs",
		slog.String("user", "alice"),
		slog.Group("req", slog.Int("status", 200)),
	)

	res := b.Snapshot(SnapshotOptions{Limit: 10})
	if got, want := len(res.Records), 1; got != want {
		t.Fatalf("len=%d want %d", got, want)
	}
	got := res.Records[0].Attrs
	want := map[string]string{"user": "alice", "req.status": "200"}
	if len(got) != len(want) {
		t.Fatalf("attrs=%v want %v", got, want)
	}
	for _, kv := range got {
		if want[kv.Key] != kv.Value {
			t.Errorf("attr %q=%q, want %q", kv.Key, kv.Value, want[kv.Key])
		}
	}
}
