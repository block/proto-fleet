package logging

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
)

const DefaultBufferCapacity = 1000

type BufferedRecord struct {
	ID      uint64
	Time    time.Time
	Level   slog.Level
	Message string
	Attrs   []KeyValue
	Source  string
}

type KeyValue struct {
	Key   string
	Value string
}

type Buffer struct {
	cache    *lru.Cache[uint64, BufferedRecord]
	nextID   atomic.Uint64
	minLevel slog.Level
}

func NewBuffer(capacity int, minLevel slog.Level) *Buffer {
	if capacity <= 0 {
		capacity = DefaultBufferCapacity
	}
	cache, _ := lru.New[uint64, BufferedRecord](capacity)
	return &Buffer{
		cache:    cache,
		minLevel: minLevel,
	}
}

func (b *Buffer) Enabled(_ context.Context, level slog.Level) bool {
	return level >= b.minLevel
}

func (b *Buffer) Handle(_ context.Context, r slog.Record) error {
	rec := BufferedRecord{
		Time:    r.Time,
		Level:   r.Level,
		Message: r.Message,
		Attrs:   collectAttrs(r),
		Source:  formatSource(r.Source()),
	}
	rec.ID = b.nextID.Add(1)
	b.cache.Add(rec.ID, rec)
	return nil
}

func (b *Buffer) WithAttrs(_ []slog.Attr) slog.Handler { return b }
func (b *Buffer) WithGroup(_ string) slog.Handler      { return b }

type SnapshotOptions struct {
	SinceID  uint64
	MinLevel slog.Level
	Search   string
	Limit    int
}

type SnapshotResult struct {
	Records   []BufferedRecord
	LatestID  uint64
	Size      int
	Truncated bool
}

func (b *Buffer) Snapshot(opts SnapshotOptions) SnapshotResult {
	records := b.cache.Values()
	res := SnapshotResult{Size: len(records)}
	search := strings.ToLower(opts.Search)

	sort.SliceStable(records, func(i, j int) bool { return records[i].ID < records[j].ID })

	for _, rec := range records {
		afterId := rec.ID > opts.SinceID
		onLevel := rec.Level >= opts.MinLevel
		matches := search == "" || strings.Contains(strings.ToLower(rec.Message), search)

		if !afterId || !onLevel || !matches {
			continue
		}
		if opts.Limit > 0 && len(res.Records) == opts.Limit {
			res.Truncated = true
			break
		}
		res.LatestID = rec.ID
		res.Records = append(res.Records, rec)
	}
	return res
}

func collectAttrs(r slog.Record) []KeyValue {
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
	// NOTE: this erases type information, but OK for display.
	*out = append(*out, KeyValue{Key: key, Value: fmt.Sprintf("%v", a.Value.Any())})
}

func formatSource(src *slog.Source) string {
	if src == nil || src.File == "" {
		return ""
	}
	return fmt.Sprintf("%s/%s:%d",
		filepath.Base(filepath.Dir(src.File)),
		filepath.Base(src.File),
		src.Line)
}
