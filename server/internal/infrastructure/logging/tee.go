package logging

import (
	"context"
	"errors"
	"log/slog"
)

// teeHandler fans a slog.Record out to multiple handlers.
//
// We use it to keep the existing stdout (text/JSON) handler in place while
// also feeding records into the in-memory ring buffer that backs the log
// viewer UI.
type teeHandler struct {
	handlers []slog.Handler
}

// newTeeHandler returns a handler that delegates to each of the given
// handlers in order. Any nil entries are skipped.
func newTeeHandler(handlers ...slog.Handler) slog.Handler {
	filtered := make([]slog.Handler, 0, len(handlers))
	for _, h := range handlers {
		if h != nil {
			filtered = append(filtered, h)
		}
	}
	return &teeHandler{handlers: filtered}
}

// Enabled returns true if any wrapped handler would accept the level. We
// can't pre-filter here because each handler may have a different floor.
func (t *teeHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, h := range t.handlers {
		if h.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

// Handle dispatches the record to every enabled wrapped handler. We collect
// all errors rather than short-circuiting so a flaky downstream can't
// silently drop records on its peers.
func (t *teeHandler) Handle(ctx context.Context, r slog.Record) error {
	var errs []error
	for _, h := range t.handlers {
		if !h.Enabled(ctx, r.Level) {
			continue
		}
		// Each handler is allowed to mutate its own copy of the record.
		if err := h.Handle(ctx, r.Clone()); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (t *teeHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	clones := make([]slog.Handler, len(t.handlers))
	for i, h := range t.handlers {
		clones[i] = h.WithAttrs(attrs)
	}
	return &teeHandler{handlers: clones}
}

func (t *teeHandler) WithGroup(name string) slog.Handler {
	clones := make([]slog.Handler, len(t.handlers))
	for i, h := range t.handlers {
		clones[i] = h.WithGroup(name)
	}
	return &teeHandler{handlers: clones}
}
