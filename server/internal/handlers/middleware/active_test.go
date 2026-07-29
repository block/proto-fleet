package middleware

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type fakeHTTPAdmission struct {
	ctx context.Context //nolint:containedctx // Supplies the admitted lifetime to the middleware.
	err error
}

type blockingRequestBody struct {
	closed chan struct{}
	once   sync.Once
}

func (b *blockingRequestBody) Read([]byte) (int, error) {
	<-b.closed
	return 0, errors.New("request body closed")
}

func (b *blockingRequestBody) Close() error {
	b.once.Do(func() {
		close(b.closed)
	})
	return nil
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

func TestActiveMiddlewareClosesRequestBodyWhenActiveLifetimeEnds(t *testing.T) {
	activeCtx, cancelActive := context.WithCancel(t.Context())
	middleware := NewActiveMiddleware(fakeHTTPAdmission{ctx: activeCtx})
	handlerStarted := make(chan struct{})
	handler := middleware.Wrap(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		close(handlerStarted)
		_, _ = io.ReadAll(r.Body)
	}))
	body := &blockingRequestBody{closed: make(chan struct{})}
	request := httptest.NewRequest(http.MethodPost, "/product", body)
	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(httptest.NewRecorder(), request)
		close(done)
	}()

	<-handlerStarted
	cancelActive()
	requireReceiveSignal(t, body.closed)
	requireReceiveSignal(t, done)
}

func requireReceiveSignal(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for signal")
	}
}
