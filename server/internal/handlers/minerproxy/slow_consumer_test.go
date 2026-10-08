package minerproxy

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/handlers/middleware"
	"github.com/stretchr/testify/require"
)

type repeatedProxyBytes struct{}

func (repeatedProxyBytes) Read(p []byte) (int, error) { clear(p); return len(p), nil }

type responseCloseBarrier struct {
	io.ReadCloser
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (b *responseCloseBarrier) Close() error {
	b.once.Do(func() { close(b.started); <-b.release })
	if err := b.ReadCloser.Close(); err != nil {
		return fmt.Errorf("close fixture upstream response: %w", err)
	}
	return nil
}

// Exercise real HTTP/1 backpressure through the production telemetry wrapper.
// Canceling an upstream request does not interrupt a blocked downstream Write.
func TestStalledProxyConsumerReleasesAdmissionOnlyAfterResponseClose(t *testing.T) {
	for _, interrupt := range []string{"write-deadline", "cancellation"} {
		t.Run(interrupt, func(t *testing.T) {
			database, orgID, deviceID, h, target := proxyFenceFixture(t)
			body := &responseCloseBarrier{ReadCloser: io.NopCloser(io.LimitReader(repeatedProxyBytes{}, 64<<20)), started: make(chan struct{}), release: make(chan struct{})}
			releaseClose := sync.OnceFunc(func() { close(body.release) })
			t.Cleanup(releaseClose)
			timeout := 500 * time.Millisecond
			if interrupt == "cancellation" {
				timeout = 30 * time.Second
			}
			h.httpClient = &http.Client{Timeout: timeout, Transport: proxyRoundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: body}, nil
			})}
			template := authenticatedProxyRequest(t, database, h, target)
			cancels := make(chan context.CancelFunc, 1)
			done := make(chan struct{})
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				ctx, cancel := context.WithCancel(r.Context())
				defer cancel()
				cancels <- cancel
				for _, key := range []string{"deviceIdentifier", "apiVersion", "rest"} {
					r.SetPathValue(key, template.PathValue(key))
				}
				h.ServeHTTP(w, r.WithContext(ctx))
				close(done)
			})
			server := httptest.NewServer(middleware.TelemetryMiddleware{}.Wrap(handler))
			t.Cleanup(func() { releaseClose(); server.Close() })
			conn, err := net.Dial("tcp", server.Listener.Addr().String())
			require.NoError(t, err)
			t.Cleanup(func() { _ = conn.Close() })
			tcpConn, ok := conn.(*net.TCPConn)
			require.True(t, ok)
			require.NoError(t, tcpConn.SetReadBuffer(1024))
			request := template.Clone(t.Context())
			request.URL.Scheme = ""
			request.URL.Host = ""
			request.RequestURI = ""
			require.NoError(t, request.Write(conn))
			require.NoError(t, conn.SetReadDeadline(time.Now().Add(2*time.Second)))
			response, err := http.ReadResponse(bufio.NewReader(conn), request)
			require.NoError(t, err)
			t.Cleanup(func() {
				_ = conn.Close()
				_ = response.Body.Close()
			})
			require.Equal(t, http.StatusOK, response.StatusCode)
			cancel := <-cancels
			if interrupt == "cancellation" {
				cancel()
			}
			// Deliberately consume none of the large response body, leaving the TCP
			// receive window closed. Only route deadline/cancellation can end copying.
			select {
			case <-body.started:
			case <-time.After(2 * time.Second):
				cancel()
				_ = conn.Close()
				releaseClose()
				t.Fatal("stalled downstream write did not reach response cleanup within its bound")
			}
			require.Equal(t, "running", proxyAttemptStatus(t, database, deviceID))
			epoch, err := h.writeService.Reserve(t.Context(), orgID, deviceID)
			require.NoError(t, err)
			drained, err := h.writeService.Drained(t.Context(), orgID, deviceID, epoch)
			require.NoError(t, err)
			require.False(t, drained)
			select {
			case <-done:
				t.Fatal("admission completed before upstream Close returned")
			default:
			}
			releaseClose()
			awaitProxyBarrier(t, done)
			assertNoProxyAttempt(t, database, deviceID)
			drained, err = h.writeService.Drained(t.Context(), orgID, deviceID, epoch)
			require.NoError(t, err)
			require.True(t, drained)
			require.NoError(t, h.writeService.ReleaseHost(t.Context(), orgID, deviceID, epoch))
			successorID := retireAndRediscoverProxyIdentity(t, database, target)
			attempt, err := h.writeService.BeginHost(t.Context(), orgID, successorID, 0)
			require.NoError(t, err)
			require.NoError(t, attempt.Finish(t.Context(), nil))
		})
	}
}

// Buffered test responses cannot block, but implement the same deadline surface
// as a server response. Actual deadline enforcement is exercised over TCP above.
type deadlineProxyRecorder struct{ *httptest.ResponseRecorder }

func (*deadlineProxyRecorder) SetWriteDeadline(time.Time) error { return nil }
func newDeadlineProxyRecorder() *deadlineProxyRecorder {
	return &deadlineProxyRecorder{httptest.NewRecorder()}
}

func TestUnsupportedProxyWriterRejectsBeforeMinerRequest(t *testing.T) {
	database, _, deviceID, h, target := proxyFenceFixture(t)
	transport := &countingProxyTransport{}
	h.httpClient = &http.Client{Transport: transport}
	response := httptest.NewRecorder()
	h.ServeHTTP(response, authenticatedProxyRequest(t, database, h, target))
	require.Equal(t, http.StatusBadGateway, response.Code)
	require.Zero(t, transport.calls.Load())
	assertNoProxyAttempt(t, database, deviceID)
}

type interruptBarrierWriter struct {
	*httptest.ResponseRecorder
	entered, release, reset chan struct{}
}

func (w *interruptBarrierWriter) SetWriteDeadline(deadline time.Time) error {
	if deadline.IsZero() {
		close(w.reset)
	} else if deadline.Before(time.Now()) {
		close(w.entered)
		<-w.release
	}
	return nil
}
func TestProxyDeadlineCleanupJoinsCancellationBeforeReset(t *testing.T) {
	writer := &interruptBarrierWriter{ResponseRecorder: httptest.NewRecorder(), entered: make(chan struct{}), release: make(chan struct{}), reset: make(chan struct{})}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	finish, err := boundProxyResponse(ctx, writer, time.Hour)
	require.NoError(t, err)
	cancel()
	awaitProxyBarrier(t, writer.entered)
	done := make(chan struct{})
	go func() { finish(); close(done) }()
	select {
	case <-writer.reset:
		t.Fatal("deadline reset raced an active cancellation callback")
	case <-time.After(20 * time.Millisecond):
	}
	close(writer.release)
	awaitProxyBarrier(t, done)
	awaitProxyBarrier(t, writer.reset)
}
