package minerproxy

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/handlers/middleware"
	"github.com/block/proto-fleet/server/internal/testutil/dbtest"
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
			dbtest.ObserveDatabaseWaits(t, database.DB)
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
func TestProxyDeadlineCleanupJoinsCancellationBeforeReturn(t *testing.T) {
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
	case <-done:
		t.Fatal("cleanup returned while cancellation still held the writer")
	case <-time.After(20 * time.Millisecond):
	}
	close(writer.release)
	awaitProxyBarrier(t, done)
	select {
	case <-writer.reset:
		t.Fatal("cleanup cleared the deadline before HTTP server finalization")
	default:
	}
}

type proxyPipeListener struct {
	connections chan net.Conn
	closed      chan struct{}
	once        sync.Once
	address     net.Addr
}

func (l *proxyPipeListener) Accept() (net.Conn, error) {
	select {
	case conn := <-l.connections:
		return conn, nil
	case <-l.closed:
		return nil, net.ErrClosed
	}
}
func (l *proxyPipeListener) Close() error   { l.once.Do(func() { close(l.closed) }); return nil }
func (l *proxyPipeListener) Addr() net.Addr { return l.address }

// A pipe has no TCP send buffer: even a tiny response must finish its buffered
// flush before admission ends, and a client that reads nothing must time out.
func TestFiniteProxyResponseFlushIsBoundedInsideAdmission(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		t.Run(method, func(t *testing.T) {
			database, _, deviceID, h, target := proxyFenceFixture(t)
			entered, done := make(chan struct{}), make(chan struct{})
			h.httpClient = &http.Client{Timeout: 300 * time.Millisecond, Transport: proxyRoundTripFunc(func(*http.Request) (*http.Response, error) {
				close(entered)
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("{}"))}, nil
			})}
			template := authenticatedProxyRequest(t, database, h, target)
			if method == http.MethodHead {
				template.SetPathValue("apiVersion", "v1")
				template.SetPathValue("rest", "system")
			}
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				for _, key := range []string{"deviceIdentifier", "apiVersion", "rest"} {
					r.SetPathValue(key, template.PathValue(key))
				}
				h.ServeHTTP(w, r)
				close(done)
			})
			serverConn, clientConn := net.Pipe()
			listener := &proxyPipeListener{connections: make(chan net.Conn, 1), closed: make(chan struct{}), address: serverConn.LocalAddr()}
			listener.connections <- serverConn
			server := &http.Server{ReadHeaderTimeout: time.Second, Handler: middleware.TelemetryMiddleware{}.Wrap(handler)}
			served := make(chan error, 1)
			go func() { served <- server.Serve(listener) }()
			t.Cleanup(func() {
				_ = clientConn.Close()
				_ = serverConn.Close()
				require.NoError(t, server.Close())
				require.ErrorIs(t, <-served, http.ErrServerClosed)
			})
			request := template.Clone(t.Context())
			request.Method = method
			request.URL.Scheme, request.URL.Host, request.RequestURI = "", "", ""
			require.NoError(t, request.Write(clientConn))
			awaitProxyBarrier(t, entered)
			select {
			case <-done:
				t.Fatal("finite response retired admission before the buffered flush")
			case <-time.After(30 * time.Millisecond):
			}
			require.Equal(t, "running", proxyAttemptStatus(t, database, deviceID))
			awaitProxyBarrier(t, done)
			assertNoProxyAttempt(t, database, deviceID)
		})
	}
}
