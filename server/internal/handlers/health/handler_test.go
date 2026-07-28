package health

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

type fakePinger struct {
	err error
}

func (f fakePinger) PingContext(context.Context) error { return f.err }

type fakeActiveState bool

func (s fakeActiveState) Active() bool { return bool(s) }

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
	NewReadyHandler(fakePinger{})(recorder, httptest.NewRequest(http.MethodGet, "/health/ready", nil))

	// Assert
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "ok", recorder.Body.String())
}

func TestReadyHandlerServiceUnavailableWhenPingFails(t *testing.T) {
	// Arrange
	recorder := httptest.NewRecorder()
	pinger := fakePinger{err: errors.New("connection refused")}

	// Act
	NewReadyHandler(pinger)(recorder, httptest.NewRequest(http.MethodGet, "/health/ready", nil))

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
	handler := NewReadyHandler(pinger)

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
	NewReadyHandler(fakePinger{})(recorder, httptest.NewRequest(http.MethodPost, "/health/ready", nil))

	// Assert
	require.Equal(t, http.StatusMethodNotAllowed, recorder.Code)
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
