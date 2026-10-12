package minerproxy

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/stretchr/testify/require"
)

func TestProxyBindingChangeWaitsForTransportReturn(t *testing.T) {
	for _, operation := range []string{"read", "timeseries", "login", "mutation", "canceled read"} {
		t.Run(operation, func(t *testing.T) {
			database, orgID, deviceID, h, target := proxyFenceFixture(t)
			entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
			finish := sync.OnceFunc(func() { close(release) })
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			h.httpClient = &http.Client{Transport: proxyRoundTripFunc(func(*http.Request) (*http.Response, error) {
				close(entered)
				<-release // Model a transport that remains active after cancellation.
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("{}"))}, nil
			})}
			request := authenticatedProxyRequest(t, database, h, target).WithContext(ctx)
			expectedKind := "observation"
			if operation == "timeseries" || operation == "login" {
				request.Method = http.MethodPost
				request.SetPathValue("apiVersion", "v1")
				request.SetPathValue("rest", map[string]string{"timeseries": "timeseries", "login": "auth/login"}[operation])
			}
			if operation == "mutation" {
				request.Method = http.MethodPost
				request.SetPathValue("apiVersion", "v1")
				request.SetPathValue("rest", "system")
				expectedKind = "mutation"
			}
			response := newDeadlineProxyRecorder()
			go func() { h.ServeHTTP(response, request); close(done) }()
			t.Cleanup(func() { finish(); awaitProxyBarrier(t, done) })
			awaitProxyBarrier(t, entered)
			var kind string
			require.NoError(t, database.DB.QueryRowContext(t.Context(), "SELECT operation_kind FROM device_write_attempt WHERE device_id=$1", deviceID).Scan(&kind))
			require.Equal(t, expectedKind, kind, "ServeHTTP must persist the endpoint's admission classification before I/O")
			if operation == "canceled read" {
				cancel()
			}
			updateCtx, stop := context.WithTimeout(t.Context(), 200*time.Millisecond)
			defer stop()
			_, err := database.DB.ExecContext(updateCtx, "UPDATE discovered_device SET port='8081' WHERE id=(SELECT discovered_device_id FROM device WHERE id=$1)", deviceID)
			require.Error(t, err, "binding changes must wait until the transport returns, including after cancellation")
			finish()
			awaitProxyBarrier(t, done)
			_, err = database.DB.ExecContext(t.Context(), "UPDATE discovered_device SET port='8081' WHERE id=(SELECT discovered_device_id FROM device WHERE id=$1)", deviceID)
			require.NoError(t, err, "binding locks must release after the call actually returns")
			if operation == "mutation" {
				require.Equal(t, "unknown", proxyAttemptStatus(t, database, deviceID))
				_, err = h.writeService.Reserve(t.Context(), orgID, deviceID)
				require.Error(t, err)
			} else {
				assertNoProxyAttempt(t, database, deviceID)
			}
		})
	}
}

type proxyFlushBoundaryWriter struct {
	*deadlineProxyRecorder
	entered, release chan struct{}
	err              error
}

func (w *proxyFlushBoundaryWriter) FlushError() error {
	close(w.entered)
	<-w.release
	return w.err
}

func TestProxyAdmissionIncludesFinalResponseFlush(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		for _, failed := range []bool{false, true} {
			name := method + "/success"
			if failed {
				name = method + "/failure"
			}
			t.Run(name, func(t *testing.T) {
				database, orgID, deviceID, h, target := proxyFenceFixture(t)
				h.httpClient = &http.Client{Transport: proxyRoundTripFunc(func(*http.Request) (*http.Response, error) {
					return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("{}"))}, nil
				})}
				writer := &proxyFlushBoundaryWriter{deadlineProxyRecorder: newDeadlineProxyRecorder(), entered: make(chan struct{}), release: make(chan struct{})}
				if failed {
					writer.err = errors.New("fixture-flush-secret")
				}
				release := sync.OnceFunc(func() { close(writer.release) })
				done := make(chan struct{})
				request := authenticatedProxyRequest(t, database, h, target)
				request.Method = method
				if method == http.MethodHead {
					request.SetPathValue("apiVersion", "v1")
					request.SetPathValue("rest", "system")
				}
				go func() { h.ServeHTTP(writer, request); close(done) }()
				t.Cleanup(func() { release(); awaitProxyBarrier(t, done) })
				select {
				case <-writer.entered:
				case <-done:
					t.Fatal("proxy retired admission without flushing the final response")
				case <-time.After(5 * time.Second):
					t.Fatal("proxy did not reach the final response flush")
				}
				require.Equal(t, "running", proxyAttemptStatus(t, database, deviceID))
				epoch, err := h.writeService.Reserve(t.Context(), orgID, deviceID)
				require.NoError(t, err)
				drained, err := devicewrite.NewService(database.DB).Drained(t.Context(), orgID, deviceID, epoch)
				require.NoError(t, err)
				require.False(t, drained, "a buffered response flush is still inside admission")
				release()
				awaitProxyBarrier(t, done)
				assertNoProxyAttempt(t, database, deviceID)
				require.Equal(t, http.StatusOK, writer.Code)
				require.NotContains(t, writer.Body.String(), "fixture-flush-secret")
			})
		}
	}
}
