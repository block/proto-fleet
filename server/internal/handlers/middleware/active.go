package middleware

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"

	"github.com/felixge/httpsnoop"
)

type activeAdmissionGate interface {
	Admit(ctx context.Context) (context.Context, func(), error)
}

type ActiveMiddleware struct {
	gate activeAdmissionGate
}

type notActiveResponse struct {
	Error string `json:"error"`
	Code  string `json:"code"`
}

type activeResponseWriter struct {
	target    http.ResponseWriter
	demoted   func() bool
	committed bool
	rejected  bool
}

func NewActiveMiddleware(gate activeAdmissionGate) ActiveMiddleware {
	return ActiveMiddleware{gate: gate}
}

func (m ActiveMiddleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		activeCtx, release, err := m.gate.Admit(r.Context())
		if err != nil {
			writeNotActiveResponse(w)
			return
		}
		defer release()
		stopBodyClose := context.AfterFunc(activeCtx, func() {
			if r.Body != nil {
				_ = r.Body.Close()
			}
		})
		defer stopBodyClose()
		activeWriter := &activeResponseWriter{
			target: w,
			demoted: func() bool {
				return r.Context().Err() == nil && activeCtx.Err() != nil
			},
		}
		wrappedWriter := activeWriter.wrap()
		next.ServeHTTP(wrappedWriter, r.WithContext(activeCtx))
		if !activeWriter.committed {
			wrappedWriter.WriteHeader(http.StatusOK)
		}
	})
}

func (w *activeResponseWriter) wrap() http.ResponseWriter {
	// Preserve optional interfaces such as http.Flusher for streaming handlers.
	return httpsnoop.Wrap(w.target, httpsnoop.Hooks{
		WriteHeader: func(next httpsnoop.WriteHeaderFunc) httpsnoop.WriteHeaderFunc {
			return func(statusCode int) {
				if w.allowCommit() {
					next(statusCode)
				}
			}
		},
		Write: func(next httpsnoop.WriteFunc) httpsnoop.WriteFunc {
			return func(body []byte) (int, error) {
				if !w.allowCommit() {
					return len(body), nil
				}
				return next(body)
			}
		},
		ReadFrom: func(next httpsnoop.ReadFromFunc) httpsnoop.ReadFromFunc {
			return func(source io.Reader) (int64, error) {
				if !w.allowCommit() {
					return 0, nil
				}
				return next(source)
			}
		},
		Flush: func(next httpsnoop.FlushFunc) httpsnoop.FlushFunc {
			return func() {
				w.allowCommit()
				next()
			}
		},
	})
}

func (w *activeResponseWriter) allowCommit() bool {
	if w.committed {
		return !w.rejected
	}
	w.committed = true
	if !w.demoted() {
		return true
	}
	w.rejected = true
	writeNotActiveResponse(w.target)
	return false
}

func writeNotActiveResponse(w http.ResponseWriter) {
	header := w.Header()
	header.Del("Content-Length")
	header.Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusServiceUnavailable)
	if err := json.NewEncoder(w).Encode(notActiveResponse{
		Error: "Fleet is not active",
		Code:  "not-active",
	}); err != nil {
		slog.Error("failed to write not-active response", "error", err)
	}
}
