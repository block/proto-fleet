package gateway

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1/fleetnodegatewayv1connect"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
)

func requireBootstrapThrottle(t *testing.T, err error, seconds string) {
	t.Helper()
	require.Equal(t, connect.CodeResourceExhausted, connect.CodeOf(err))
	var coded *connect.Error
	require.ErrorAs(t, err, &coded)
	require.Equal(t, seconds, coded.Meta().Get("Retry-After"))
}

func TestBootstrapRateBudgetWindow(t *testing.T) {
	b := &bootstrapRateBudget{limit: 2}
	now := time.Now()
	require.NoError(t, b.check("192.0.2.1", now))
	require.NoError(t, b.check("192.0.2.1", now.Add(time.Second)))
	requireBootstrapThrottle(t, b.check("192.0.2.1", now.Add(1500*time.Millisecond)), "59")
	requireBootstrapThrottle(t, b.check("192.0.2.1", now.Add(time.Minute-time.Nanosecond)), "1")
	require.NoError(t, b.check("2001:db8::1", now))
	require.NoError(t, b.check("192.0.2.1", now.Add(time.Minute)))
	require.Equal(t, 1, b.entries["192.0.2.1"].count)
}

func TestBootstrapRateBudgetConcurrent(t *testing.T) {
	b := &bootstrapRateBudget{limit: bootstrapHandshakeLimit}
	now := time.Now()
	var accepted atomic.Int64
	var wg sync.WaitGroup
	for range 1000 {
		wg.Go(func() {
			if b.check("192.0.2.1", now) == nil {
				accepted.Add(1)
			}
		})
	}
	wg.Wait()
	require.EqualValues(t, 600, accepted.Load())
}

func TestBootstrapRateBudgetCapacity(t *testing.T) {
	b := &bootstrapRateBudget{limit: 2}
	now := time.Now()
	require.NoError(t, b.check("oldest", now))
	require.NoError(t, b.check("oldest", now))
	for i := 1; i < bootstrapRateMaxIPs; i++ {
		require.NoError(t, b.check(strconv.Itoa(i), now.Add(10*time.Second)))
	}
	requireBootstrapThrottle(t, b.check("new", now.Add(30*time.Second)), "30")
	require.Len(t, b.entries, bootstrapRateMaxIPs)
	requireBootstrapThrottle(t, b.check("oldest", now.Add(30*time.Second)), "30")
	// Existing clients retain their unspent budget even when the map is full.
	require.NoError(t, b.check("1", now.Add(30*time.Second)))
	require.NoError(t, b.check("new", now.Add(time.Minute)))
	require.Len(t, b.entries, bootstrapRateMaxIPs)
	require.NotContains(t, b.entries, "oldest")
	requireBootstrapThrottle(t, b.check("another", now.Add(time.Minute)), "10")
	// All expired entries are reclaimed, without a background goroutine.
	require.NoError(t, b.check("another", now.Add(2*time.Minute)))
	require.Len(t, b.entries, 1)
}

func bootstrapRejectingInterceptor(calls *atomic.Int64) connect.Interceptor {
	return connect.UnaryInterceptorFunc(func(_ connect.UnaryFunc) connect.UnaryFunc {
		return func(_ctx context.Context, _ connect.AnyRequest) (connect.AnyResponse, error) {
			calls.Add(1)
			return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("test authentication rejected"))
		}
	})
}

func TestBootstrapRateGateProtocolsAndSharedHandshakeBudget(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts []connect.ClientOption
	}{
		{name: "connect-protobuf"},
		{name: "connect-json", opts: []connect.ClientOption{connect.WithProtoJSON()}},
		{name: "grpc", opts: []connect.ClientOption{connect.WithGRPC()}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Now()
			var clock atomic.Int64
			clock.Store(now.UnixNano())
			var calls atomic.Int64
			ip, err := middleware.NewClientIPMiddleware("")
			require.NoError(t, err)
			_, h := fleetnodegatewayv1connect.NewFleetNodeGatewayServiceHandler(&Handler{},
				bootstrapRateLimitOption(func() time.Time { return time.Unix(0, clock.Load()) }),
				connect.WithInterceptors(bootstrapRejectingInterceptor(&calls)))
			srv := httptest.NewUnstartedServer(ip.Wrap(h))
			srv.EnableHTTP2 = true
			srv.StartTLS()
			t.Cleanup(srv.Close)
			client := fleetnodegatewayv1connect.NewFleetNodeGatewayServiceClient(srv.Client(), srv.URL, tc.opts...)
			// Failed authentication also spends the budget, independently of codec.
			for range bootstrapRegistrationLimit {
				_, err = client.Register(t.Context(), connect.NewRequest(&pb.RegisterRequest{}))
				require.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))
			}
			_, err = client.Register(t.Context(), connect.NewRequest(&pb.RegisterRequest{}))
			requireBootstrapThrottle(t, err, "60")
			for range bootstrapHandshakeLimit / 2 {
				_, err = client.BeginAuthHandshake(t.Context(), connect.NewRequest(&pb.BeginAuthHandshakeRequest{}))
				require.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))
				_, err = client.CompleteAuthHandshake(t.Context(), connect.NewRequest(&pb.CompleteAuthHandshakeRequest{}))
				require.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))
			}
			_, err = client.BeginAuthHandshake(t.Context(), connect.NewRequest(&pb.BeginAuthHandshakeRequest{}))
			requireBootstrapThrottle(t, err, "60")
			_, err = client.CompleteAuthHandshake(t.Context(), connect.NewRequest(&pb.CompleteAuthHandshakeRequest{}))
			requireBootstrapThrottle(t, err, "60")
			require.EqualValues(t, 720, calls.Load(), "throttled calls must never reach interceptors or database-backed handlers")
			clock.Store(now.Add(time.Minute).UnixNano())
			_, err = client.Register(t.Context(), connect.NewRequest(&pb.RegisterRequest{}))
			require.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))
			_, err = client.BeginAuthHandshake(t.Context(), connect.NewRequest(&pb.BeginAuthHandshakeRequest{}))
			require.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))
		})
	}
}

type bootstrapUnreadBody struct{ reads int }

func (b *bootstrapUnreadBody) Read([]byte) (int, error) {
	b.reads++
	return 0, errors.New("bootstrap body must not be decoded")
}

func (*bootstrapUnreadBody) Close() error { return nil }

func TestBootstrapRateGateRejectsBeforeDecodingAndIgnoresSpoofedIP(t *testing.T) {
	now := time.Now()
	var calls atomic.Int64
	gate := bootstrapRateLimitOption(func() time.Time { return now })
	h := connect.NewUnaryHandler(fleetnodegatewayv1connect.FleetNodeGatewayServiceRegisterProcedure,
		func(context.Context, *connect.Request[pb.RegisterRequest]) (*connect.Response[pb.RegisterResponse], error) {
			t.Fatal("database-backed handler must not run")
			return nil, nil
		}, gate, connect.WithInterceptors(bootstrapRejectingInterceptor(&calls)))
	for _, tc := range []struct {
		name, trusted, peer, forwarded string
	}{
		{name: "untrusted peer", peer: "192.0.2.1:1234"},
		{name: "trusted proxy chain", trusted: "127.0.0.1/32,10.0.0.0/24", peer: "127.0.0.1:1234", forwarded: ", 192.0.2.1, 10.0.0.1"},
		{name: "missing address", peer: "invalid"},
		{name: "ipv6", peer: "[2001:db8::1]:1234"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Give each scenario a fresh quota window.
			now = now.Add(time.Minute)
			ip, err := middleware.NewClientIPMiddleware(tc.trusted)
			require.NoError(t, err)
			wrapped := ip.Wrap(h)
			for i := range bootstrapRegistrationLimit {
				req := httptest.NewRequest(http.MethodPost, fleetnodegatewayv1connect.FleetNodeGatewayServiceRegisterProcedure, strings.NewReader("{}"))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("X-Forwarded-For", fmt.Sprintf("198.51.100.%d%s", i, tc.forwarded))
				req.RemoteAddr = tc.peer
				rr := httptest.NewRecorder()
				wrapped.ServeHTTP(rr, req)
				require.Equal(t, http.StatusUnauthorized, rr.Code)
			}
			body := &bootstrapUnreadBody{}
			req := httptest.NewRequest(http.MethodPost, fleetnodegatewayv1connect.FleetNodeGatewayServiceRegisterProcedure, body)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Forwarded-For", "203.0.113.1"+tc.forwarded)
			req.RemoteAddr = tc.peer
			rr := httptest.NewRecorder()
			wrapped.ServeHTTP(rr, req)
			require.Equal(t, http.StatusTooManyRequests, rr.Code)
			require.Equal(t, "60", rr.Header().Get("Retry-After"))
			require.Zero(t, body.reads)
		})
	}
	// This same option must leave every operational procedure outside the quotas.
	for _, procedure := range []string{
		fleetnodegatewayv1connect.FleetNodeGatewayServiceUploadHeartbeatProcedure,
		fleetnodegatewayv1connect.FleetNodeGatewayServiceControlStreamProcedure,
		fleetnodegatewayv1connect.FleetNodeGatewayServiceUploadCommandArtifactProcedure,
		fleetnodegatewayv1connect.FleetNodeGatewayServiceDownloadCommandArtifactProcedure,
		fleetnodegatewayv1connect.FleetNodeGatewayServiceReportDiscoveredDevicesProcedure,
		fleetnodegatewayv1connect.FleetNodeGatewayServiceReportPairedDevicesProcedure,
	} {
		t.Run(procedure, func(t *testing.T) {
			handler := connect.NewUnaryHandler(procedure,
				func(context.Context, *connect.Request[pb.RegisterRequest]) (*connect.Response[pb.RegisterResponse], error) {
					return connect.NewResponse(&pb.RegisterResponse{}), nil
				}, gate)
			// No IP middleware: bootstrap calls would use the exhausted fallback
			// bucket, but operational procedures must still be admitted.
			for range bootstrapHandshakeLimit + 1 {
				req := httptest.NewRequest(http.MethodPost, procedure, bytes.NewReader(nil))
				req.Header.Set("Content-Type", "application/proto")
				rr := httptest.NewRecorder()
				handler.ServeHTTP(rr, req)
				require.Equal(t, http.StatusOK, rr.Code)
			}
		})
	}
}
