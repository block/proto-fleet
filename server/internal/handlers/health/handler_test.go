package health

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type fakePinger struct {
	err error
}

func (f fakePinger) PingContext(context.Context) error { return f.err }

type fakeActiveState bool

func (s fakeActiveState) Active() bool { return bool(s) }

type mutableActiveState struct {
	active atomic.Bool
}

func (s *mutableActiveState) Active() bool { return s.active.Load() }

func TestLivenessHandlerStaysStatic(t *testing.T) {
	// Arrange
	recorder := httptest.NewRecorder()

	// Act
	NewHandler()(recorder, httptest.NewRequest(http.MethodGet, "/health", nil))

	// Assert
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "ok", recorder.Body.String())
}

func TestReadyHandlerOKWhenDBReachable(t *testing.T) {
	// Arrange
	recorder := httptest.NewRecorder()

	// Act
	NewReadyHandler(fakePinger{}, fakeActiveState(true))(
		recorder,
		httptest.NewRequest(http.MethodGet, "/health/ready", nil),
	)

	// Assert
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "ok", recorder.Body.String())
}

func TestReadyHandlerServiceUnavailableWhenPingFails(t *testing.T) {
	// Arrange
	recorder := httptest.NewRecorder()
	pinger := fakePinger{err: errors.New("connection refused")}

	// Act
	NewReadyHandler(pinger, fakeActiveState(true))(
		recorder,
		httptest.NewRequest(http.MethodGet, "/health/ready", nil),
	)

	// Assert
	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
}

type countingPinger struct {
	pings int
}

func (c *countingPinger) PingContext(context.Context) error {
	c.pings++
	return nil
}

func TestReadyHandlerCachesPingResults(t *testing.T) {
	// Arrange
	pinger := &countingPinger{}
	handler := NewReadyHandler(pinger, fakeActiveState(true))

	// Act
	for range 3 {
		recorder := httptest.NewRecorder()
		handler(recorder, httptest.NewRequest(http.MethodGet, "/health/ready", nil))
		require.Equal(t, http.StatusOK, recorder.Code)
	}

	// Assert
	require.Equal(t, 1, pinger.pings, "a request flood must not amplify into per-request DB pings")
}

func TestReadyHandlerRejectsNonGet(t *testing.T) {
	// Arrange
	recorder := httptest.NewRecorder()

	// Act
	NewReadyHandler(fakePinger{}, fakeActiveState(true))(
		recorder,
		httptest.NewRequest(http.MethodPost, "/health/ready", nil),
	)

	// Assert
	require.Equal(t, http.StatusMethodNotAllowed, recorder.Code)
}

func TestReadyHandlerServiceUnavailableWhilePassive(t *testing.T) {
	pinger := &countingPinger{}
	recorder := httptest.NewRecorder()

	NewReadyHandler(pinger, fakeActiveState(false))(
		recorder,
		httptest.NewRequest(http.MethodGet, "/health/ready", nil),
	)

	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	require.Zero(t, pinger.pings, "a passive process must not report traffic readiness or ping the database")
}

type blockingPinger struct {
	started chan struct{}
	release chan struct{}
}

func (p *blockingPinger) PingContext(ctx context.Context) error {
	close(p.started)
	select {
	case <-p.release:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("ping canceled: %w", ctx.Err())
	}
}

func TestReadyHandlerRechecksActiveStateAfterPing(t *testing.T) {
	active := &mutableActiveState{}
	active.active.Store(true)
	pinger := &blockingPinger{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	handler := NewReadyHandler(pinger, active)
	result := make(chan *httptest.ResponseRecorder, 1)

	go func() {
		recorder := httptest.NewRecorder()
		handler(recorder, httptest.NewRequest(http.MethodGet, "/health/ready", nil))
		result <- recorder
	}()

	select {
	case <-pinger.started:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for readiness ping")
	}
	active.active.Store(false)
	close(pinger.release)

	select {
	case recorder := <-result:
		require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for readiness response")
	}
}

func TestActiveHandlerReflectsStrictActiveState(t *testing.T) {
	for _, test := range []struct {
		name       string
		active     bool
		statusCode int
	}{
		{name: "active", active: true, statusCode: http.StatusOK},
		{name: "passive", active: false, statusCode: http.StatusServiceUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()

			NewActiveHandler(fakeActiveState(test.active))(
				recorder,
				httptest.NewRequest(http.MethodGet, "/health/active", nil),
			)

			require.Equal(t, test.statusCode, recorder.Code)
		})
	}
}
