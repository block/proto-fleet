package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/block/proto-fleet/server/generated/grpc/auth/v1/authv1connect"
	authhandler "github.com/block/proto-fleet/server/internal/handlers/auth"
	"github.com/block/proto-fleet/server/internal/handlers/interceptors"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

type shutdownHTTPAdmission struct{}

func (shutdownHTTPAdmission) Admit(ctx context.Context) (context.Context, func(), error) {
	return ctx, func() {}, nil
}

type shutdownUploadBody struct {
	io.ReadCloser
	started chan struct{}
	once    sync.Once
}

func (b *shutdownUploadBody) Read(p []byte) (int, error) {
	b.once.Do(func() { close(b.started) })
	n, err := b.ReadCloser.Read(p)
	if err != nil && err != io.EOF {
		err = fmt.Errorf("read shutdown test upload: %w", err)
	}
	return n, err
}

func TestServeFleetRuntimeClosesIncompleteRPCUploadsBeforeDraining(t *testing.T) {
	for _, protocol := range []string{"http1", "h2c"} {
		t.Run(protocol, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			require.NoError(t, err)
			ctx, cancel := context.WithCancel(t.Context())
			t.Cleanup(cancel)
			_, rpc := authv1connect.NewAuthServiceHandler(authhandler.NewHandler(nil),
				connect.WithInterceptors(interceptors.NewActiveInterceptor(shutdownHTTPAdmission{})))
			started, cleanupStarted, allowCleanup := make(chan struct{}), make(chan struct{}), make(chan struct{})
			release := sync.OnceFunc(func() { close(allowCleanup) })
			t.Cleanup(release)
			drain := &httpRequestDrain{handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				r.Body = &shutdownUploadBody{ReadCloser: r.Body, started: started}
				// Connect reads unary bodies before its active/auth interceptors.
				// No service dependency is used because the upload never completes.
				rpc.ServeHTTP(w, r)
				close(cleanupStarted)
				<-allowCleanup
			})}
			var handler http.Handler = drain
			var transport http.RoundTripper = &http.Transport{DisableKeepAlives: true}
			if protocol == "h2c" {
				handler = h2c.NewHandler(drain, &http2.Server{})
				transport = &http2.Transport{AllowHTTP: true, DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
					return (&net.Dialer{}).DialContext(ctx, network, addr)
				}}
			}
			server := &http.Server{Handler: handler, ReadHeaderTimeout: time.Second,
				ConnState: func(conn net.Conn, state http.ConnState) {
					if state == http.StateHijacked {
						// Also release the socket if an assertion fails before shutdown.
						t.Cleanup(func() { _ = conn.Close() })
					}
				}}
			t.Cleanup(func() { _ = server.Close() })
			runtimeStopped := make(chan struct{})
			runtime := scriptedFleetRuntime{run: func(ctx context.Context) error {
				<-ctx.Done()
				close(runtimeStopped)
				return nil
			}}
			result := make(chan error, 1)
			go func() {
				result <- serveFleetRuntimeWithContext(ctx, server, listener, runtime, 20*time.Millisecond, drain)
			}()
			client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
			t.Cleanup(client.CloseIdleConnections)
			reader, writer := io.Pipe()
			t.Cleanup(func() { _ = writer.Close(); _ = reader.Close() })
			request, err := http.NewRequestWithContext(t.Context(), http.MethodPost,
				"http://"+listener.Addr().String()+authv1connect.AuthServiceAuthenticateProcedure, reader)
			require.NoError(t, err)
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Connect-Protocol-Version", "1")
			request.ContentLength = 128
			clientDone := make(chan struct{})
			go func() {
				defer close(clientDone)
				response, err := client.Do(request)
				if err == nil {
					_ = response.Body.Close()
				}
			}()
			_, err = writer.Write([]byte("{"))
			require.NoError(t, err)
			requireReceive(t, started)
			cancel()
			requireReceive(t, cleanupStarted)
			// The server has already interrupted its read. Release the test
			// client's pipe writer, which HTTP/1 may still be waiting on.
			require.NoError(t, writer.Close())
			requireReceive(t, clientDone)
			select {
			case <-runtimeStopped:
				t.Fatal("runtime stopped before handler cleanup finished")
			default:
			}
			select {
			case <-result:
				t.Fatal("shutdown returned before handler cleanup finished")
			default:
			}
			release()
			select {
			case err := <-result:
				if protocol == "h2c" {
					require.NoError(t, err)
				} else {
					require.ErrorIs(t, err, context.DeadlineExceeded)
				}
			case <-time.After(time.Second):
				t.Fatal("shutdown did not finish after handler cleanup")
			}
			requireReceive(t, runtimeStopped)
		})
	}
}

type delayedHTTPAccept struct {
	net.Listener
	conn    net.Conn
	started chan struct{}
	release chan struct{}
}

func (l delayedHTTPAccept) Accept() (net.Conn, error) {
	close(l.started)
	<-l.release
	return l.conn, nil
}

func TestHTTPConnectionListenerClosesAcceptRacingShutdown(t *testing.T) {
	conn, peer := net.Pipe()
	t.Cleanup(func() { _ = conn.Close(); _ = peer.Close() })
	require.NoError(t, peer.SetReadDeadline(time.Now().Add(time.Second)))
	accept := delayedHTTPAccept{conn: conn, started: make(chan struct{}), release: make(chan struct{})}
	listener := &httpConnectionListener{Listener: accept, conns: make(map[*httpConnection]struct{})}
	result := make(chan error, 1)
	go func() { _, err := listener.Accept(); result <- err }()
	requireReceive(t, accept.started)
	listener.closeConnections()
	close(accept.release)
	require.ErrorIs(t, <-result, net.ErrClosed)
	_, err := peer.Read(make([]byte, 1))
	require.ErrorIs(t, err, io.EOF)
	require.Empty(t, listener.conns)
}
