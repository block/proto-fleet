package middleware

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
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

func NewActiveMiddleware(gate activeAdmissionGate) ActiveMiddleware {
	return ActiveMiddleware{gate: gate}
}

func (m ActiveMiddleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		activeCtx, release, err := m.gate.Admit(r.Context())
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			if encodeErr := json.NewEncoder(w).Encode(notActiveResponse{
				Error: "Fleet is not active",
				Code:  "not-active",
			}); encodeErr != nil {
				slog.Error("failed to write not-active response", "error", encodeErr)
			}
			return
		}
		defer release()
		next.ServeHTTP(w, r.WithContext(activeCtx))
	})
}
