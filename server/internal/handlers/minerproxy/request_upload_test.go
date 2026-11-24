package minerproxy

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
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
	"github.com/stretchr/testify/require"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

type uploadDeadlineWriter struct {
	http.ResponseWriter
	started chan<- struct{}
	once    sync.Once
}

func (w *uploadDeadlineWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *uploadDeadlineWriter) SetReadDeadline(deadline time.Time) error {
	err := http.NewResponseController(w.ResponseWriter).SetReadDeadline(deadline)
	if err == nil && !deadline.IsZero() {
		w.once.Do(func() { w.started <- struct{}{} })
	}
	if err != nil {
		return fmt.Errorf("set upload test read deadline: %w", err)
	}
	return nil
}

// Incomplete uploads must release the proxy's limited shared capacity under both
// protocols served by Fleet. HTTP server header timeouts do not bound these reads.
func TestIncompleteProxyUploadsReleaseCapacity(t *testing.T) {
	for _, protocol := range []string{"http1", "h2c"} {
		for _, interrupt := range []string{"timeout", "cancellation"} {
			t.Run(protocol+"/"+interrupt, func(t *testing.T) {
				database, _, deviceID, h, target := proxyFenceFixture(t)
				h.config.MaxConcurrentRequests = 2
				timeout := time.Second
				if interrupt == "cancellation" {
					timeout = time.Minute
				}
				transport := &countingProxyTransport{}
				h.httpClient = &http.Client{Timeout: timeout, Transport: transport}
				template := authenticatedProxyRequest(t, database, h, target)
				started, done := make(chan struct{}, 2), make(chan struct{}, 2)
				controlDone := make(chan struct{}, 2)
				cancels := make(chan context.CancelFunc, 2)
				handler := middleware.TelemetryMiddleware{}.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					for _, key := range []string{"deviceIdentifier", "apiVersion", "rest"} {
						r.SetPathValue(key, template.PathValue(key))
					}
					if r.URL.Path == "/incomplete" {
						ctx, cancel := context.WithCancel(r.Context())
						defer cancel()
						cancels <- cancel
						h.ServeHTTP(&uploadDeadlineWriter{ResponseWriter: w, started: started}, r.WithContext(ctx))
						done <- struct{}{}
						return
					}
					h.ServeHTTP(w, r)
					controlDone <- struct{}{}
				}))
				if protocol == "h2c" {
					handler = h2c.NewHandler(handler, &http2.Server{})
				}
				server := httptest.NewServer(handler)
				t.Cleanup(server.Close)
				var clientTransport http.RoundTripper
				if protocol == "h2c" {
					clientTransport = &http2.Transport{AllowHTTP: true, DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
						return (&net.Dialer{}).DialContext(ctx, network, addr)
					}}
				} else {
					clientTransport = &http.Transport{DisableKeepAlives: true}
				}
				client := &http.Client{Transport: clientTransport, Timeout: 5 * time.Second}
				t.Cleanup(client.CloseIdleConnections)
				request := func(path string, body io.Reader) *http.Request {
					r, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+path, body)
					require.NoError(t, err)
					r.Header = template.Header.Clone()
					return r
				}
				type result struct {
					status int
					body   string
					err    error
				}
				results := make(chan result, 2)
				for range 2 {
					if protocol == "http1" {
						conn, err := net.Dial("tcp", server.Listener.Addr().String())
						require.NoError(t, err)
						t.Cleanup(func() { _ = conn.Close() })
						require.NoError(t, conn.SetDeadline(time.Now().Add(5*time.Second)))
						_, err = fmt.Fprintf(conn, "GET /incomplete HTTP/1.1\r\nHost: fleet.test\r\nContent-Length: %d\r\nConnection: close\r\n", 1<<20)
						require.NoError(t, err)
						require.NoError(t, template.Header.Write(conn))
						_, err = io.WriteString(conn, "\r\n{")
						require.NoError(t, err)
						r := request("/incomplete", nil)
						go func() {
							response, err := http.ReadResponse(bufio.NewReader(conn), r)
							if response == nil {
								results <- result{err: err}
								return
							}
							body, readErr := io.ReadAll(response.Body)
							closeErr := response.Body.Close()
							results <- result{status: response.StatusCode, body: string(body), err: errors.Join(err, readErr, closeErr)}
						}()
					} else {
						reader, writer := io.Pipe()
						t.Cleanup(func() { _ = writer.Close(); _ = reader.Close() })
						r := request("/incomplete", reader)
						r.ContentLength = 1 << 20
						go func() {
							response, err := client.Do(r)
							if response == nil {
								results <- result{err: err}
								return
							}
							body, readErr := io.ReadAll(response.Body)
							closeErr := response.Body.Close()
							results <- result{status: response.StatusCode, body: string(body), err: errors.Join(err, readErr, closeErr)}
						}()
						_, err := writer.Write([]byte("{"))
						require.NoError(t, err)
					}
					awaitProxyBarrier(t, started)
				}
				response, err := client.Do(request("/busy", nil))
				require.NoError(t, err)
				require.Equal(t, http.StatusServiceUnavailable, response.StatusCode)
				require.NoError(t, response.Body.Close())
				awaitProxyBarrier(t, controlDone)
				require.Zero(t, transport.calls.Load())
				assertNoProxyAttempt(t, database, deviceID)
				if interrupt == "cancellation" {
					for range 2 {
						(<-cancels)()
					}
				}
				for range 2 {
					awaitProxyBarrier(t, done)
				}
				for range 2 {
					select {
					case result := <-results:
						if interrupt == "timeout" {
							require.NoError(t, result.err, "body timeout must allow an independently bounded error response")
							require.Equal(t, http.StatusBadGateway, result.status)
							require.Contains(t, result.body, "timed out")
						}
					case <-time.After(5 * time.Second):
						t.Fatal("incomplete upload client did not finish after handler interruption")
					}
				}
				require.Zero(t, transport.calls.Load())
				assertNoProxyAttempt(t, database, deviceID)
				response, err = client.Do(request("/next", nil))
				require.NoError(t, err)
				require.Equal(t, http.StatusOK, response.StatusCode)
				require.NoError(t, response.Body.Close())
				awaitProxyBarrier(t, controlDone)
				require.Equal(t, int64(1), transport.calls.Load(), "a subsequent request must be admitted after the uploads release capacity")
			})
		}
	}
}

func TestProxyUploadWithoutReadDeadlineSupportRejectsBeforeAdmission(t *testing.T) {
	database, _, deviceID, h, target := proxyFenceFixture(t)
	transport := &countingProxyTransport{}
	h.httpClient = &http.Client{Transport: transport}
	r := authenticatedProxyRequest(t, database, h, target)
	r.Body = io.NopCloser(strings.NewReader("{}"))
	r.ContentLength = 2
	response := newDeadlineProxyRecorder() // Write deadlines only; upload support is absent.
	h.ServeHTTP(response, r)
	require.Equal(t, http.StatusBadGateway, response.Code)
	require.Contains(t, response.Body.String(), "read deadline could not be enforced")
	require.Zero(t, transport.calls.Load())
	assertNoProxyAttempt(t, database, deviceID)
}

type uploadCleanupWriter struct {
	*httptest.ResponseRecorder
	entered, release, reset chan struct{}
}

func (w *uploadCleanupWriter) SetReadDeadline(deadline time.Time) error {
	if deadline.IsZero() {
		close(w.reset)
	} else if deadline.Before(time.Now()) {
		close(w.entered)
		<-w.release
	}
	return nil
}

func TestProxyUploadCleanupKeepsDeadlineThroughCloseAndJoinsCancellation(t *testing.T) {
	writer := &uploadCleanupWriter{ResponseRecorder: httptest.NewRecorder(), entered: make(chan struct{}), release: make(chan struct{}), reset: make(chan struct{})}
	ctx, cancel := context.WithTimeout(t.Context(), time.Hour)
	defer cancel()
	request := httptest.NewRequest(http.MethodGet, "http://fleet.test/", nil).WithContext(ctx)
	body := &proxyBodyBarrier{reader: strings.NewReader("{}"), closeStarted: make(chan struct{}), closeRelease: make(chan struct{})}
	request.Body = body
	finishClose := sync.OnceFunc(func() { close(body.closeRelease) })
	finishDeadline := sync.OnceFunc(func() { close(writer.release) })
	t.Cleanup(finishClose)
	t.Cleanup(finishDeadline)
	done := make(chan struct{})
	go func() { _, _ = readProxyBody(writer, request); close(done) }()
	awaitProxyBarrier(t, body.closeStarted)
	cancel()
	awaitProxyBarrier(t, writer.entered)
	select {
	case <-writer.reset:
		t.Fatal("read deadline cleared before body Close completed")
	default:
	}
	finishClose()
	select {
	case <-done:
		t.Fatal("cleanup returned while cancellation still held the writer")
	case <-time.After(20 * time.Millisecond):
	}
	finishDeadline()
	awaitProxyBarrier(t, done)
	awaitProxyBarrier(t, writer.reset)
}

func TestProxyUploadErrorsPreserveClassification(t *testing.T) {
	for _, test := range []struct {
		name      string
		body      io.ReadCloser
		oversized bool
		code      int
	}{
		{name: "too large", body: io.NopCloser(io.LimitReader(repeatedProxyBytes{}, maxProxyBodyBytes+1)), oversized: true, code: http.StatusRequestEntityTooLarge},
		{name: "read failure", body: &proxyBodyBarrier{reader: errorUploadReader{}}, code: http.StatusBadRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), time.Hour)
			defer cancel()
			request := httptest.NewRequest(http.MethodGet, "http://fleet.test/", nil).WithContext(ctx)
			request.Body = test.body
			writer := &uploadCleanupWriter{ResponseRecorder: httptest.NewRecorder(), entered: make(chan struct{}), release: make(chan struct{}), reset: make(chan struct{})}
			_, err := readProxyBody(writer, request)
			require.Error(t, err)
			require.Equal(t, test.code, httpStatusForError(err))
			if test.oversized {
				var limitErr *http.MaxBytesError
				require.ErrorAs(t, err, &limitErr)
			} else {
				require.NotContains(t, clientMessageForError(err), "exceeds")
			}
		})
	}
}

type errorUploadReader struct{}

func (errorUploadReader) Read([]byte) (int, error) { return 0, errors.New("fixture upload error") }
