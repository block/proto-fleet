package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

type fakeHTTPAdmission struct {
	ctx context.Context //nolint:containedctx // Supplies the admitted lifetime to the middleware.
	err error
}

func (a fakeHTTPAdmission) Admit(context.Context) (context.Context, func(), error) {
	if a.err != nil {
		return nil, nil, a.err
	}
	return a.ctx, func() {}, nil
}

func TestActiveMiddlewareRejectsPassiveHTTPWithMachineReadableBody(t *testing.T) {
	middleware := NewActiveMiddleware(fakeHTTPAdmission{err: errors.New("not active")})
	nextCalled := false
	handler := middleware.Wrap(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		nextCalled = true
	}))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/product", nil))

	require.False(t, nextCalled)
	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	require.JSONEq(t, `{"error":"Fleet is not active","code":"not-active"}`, recorder.Body.String())
}

func TestActiveMiddlewareBindsRequestToActiveLifetime(t *testing.T) {
	activeCtx, cancelActive := context.WithCancel(t.Context())
	middleware := NewActiveMiddleware(fakeHTTPAdmission{ctx: activeCtx})
	handlerStarted := make(chan struct{})
	handler := middleware.Wrap(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		close(handlerStarted)
		<-r.Context().Done()
	}))
	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/product", nil))
		close(done)
	}()

	<-handlerStarted
	cancelActive()
	<-done
}
