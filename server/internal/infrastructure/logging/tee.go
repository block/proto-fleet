package logging

import (
	"context"
	"errors"
	"log/slog"
)

type teeHandler struct {
	handlers []slog.Handler
}

func newTeeHandler(handlers ...slog.Handler) slog.Handler {
	filtered := make([]slog.Handler, 0, len(handlers))
	for _, h := range handlers {
		if h != nil {
			filtered = append(filtered, h)
		}
	}
	return &teeHandler{handlers: filtered}
}

func (t *teeHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, h := range t.handlers {
		if h.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (t *teeHandler) Handle(ctx context.Context, r slog.Record) error {
	var errs []error
	for _, h := range t.handlers {
		if !h.Enabled(ctx, r.Level) {
			continue
		}
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
