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

func TestBuffer_LimitTruncatesAndAdvancesLatestID(t *testing.T) {
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
	// LatestID should still reflect the newest record (5), even though we
	// only returned the oldest two — that's the cursor invariant the
	// client depends on.
	if res.LatestID != 5 {
		t.Errorf("LatestID=%d want 5", res.LatestID)
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
