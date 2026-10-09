package gateway

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
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
	// Arrange
	b := &bootstrapRateBudget{limit: 2}
	now := time.Now()

	// Act
	require.NoError(t, b.check("192.0.2.1", now))
	require.NoError(t, b.check("192.0.2.1", now.Add(time.Second)))
	requireBootstrapThrottle(t, b.check("192.0.2.1", now.Add(1500*time.Millisecond)), "59")
	requireBootstrapThrottle(t, b.check("192.0.2.1", now.Add(time.Minute-time.Nanosecond)), "1")
	require.NoError(t, b.check("2001:db8::1", now))
	require.NoError(t, b.check("192.0.2.1", now.Add(time.Minute)))

	// Assert
	require.Equal(t, 1, b.entries["192.0.2.1"].count)
}

func TestBootstrapRateBudgetConcurrent(t *testing.T) {
	// Arrange
	b := &bootstrapRateBudget{limit: bootstrapHandshakeLimit}
	now := time.Now()
	var accepted atomic.Int64
	var wg sync.WaitGroup

	// Act
	for range 1000 {
		wg.Go(func() {
			if b.check("192.0.2.1", now) == nil {
				accepted.Add(1)
			}
		})
	}
	wg.Wait()

	// Assert
	require.EqualValues(t, 600, accepted.Load())
}

func TestBootstrapRateBudgetCapacity(t *testing.T) {
	// Arrange
	b := &bootstrapRateBudget{limit: 2}
	now := time.Now()
	require.NoError(t, b.check("oldest", now))
	require.NoError(t, b.check("oldest", now))
	for i := 1; i < bootstrapRateMaxIPs; i++ {
		require.NoError(t, b.check(strconv.Itoa(i), now.Add(10*time.Second)))
	}

	// Act
	// A full table must not lock out untracked IPs: they share one overflow budget.
	require.NoError(t, b.check("overflow-1", now.Add(30*time.Second)))
	require.NoError(t, b.check("overflow-2", now.Add(35*time.Second)))
	overflowErr := b.check("overflow-3", now.Add(40*time.Second))

	// Assert
	requireBootstrapThrottle(t, overflowErr, "50")
	require.Len(t, b.entries, bootstrapRateMaxIPs)
	require.NotContains(t, b.entries, "overflow-1")
	requireBootstrapThrottle(t, b.check("oldest", now.Add(30*time.Second)), "30")
	// Existing clients retain their unspent budget even when the map is full.
	require.NoError(t, b.check("1", now.Add(30*time.Second)))

	// Act
	// The oldest entry expires, which frees a slot for the next new IP.
	require.NoError(t, b.check("new", now.Add(time.Minute)))

	// Assert
	require.Len(t, b.entries, bootstrapRateMaxIPs)
	require.NotContains(t, b.entries, "oldest")
	requireBootstrapThrottle(t, b.check("another", now.Add(time.Minute)), "30")

	// Act
	// All expired entries are reclaimed, without a background goroutine.
	require.NoError(t, b.check("another", now.Add(2*time.Minute)))

	// Assert
	require.Len(t, b.entries, 1)
}

func TestBootstrapRateBudgetOverflowWindowResets(t *testing.T) {
	// Arrange
	b := &bootstrapRateBudget{limit: 1}
	now := time.Now()
	for i := range bootstrapRateMaxIPs {
		require.NoError(t, b.check(strconv.Itoa(i), now))
	}
	require.NoError(t, b.check("overflow-1", now))
	requireBootstrapThrottle(t, b.check("overflow-2", now.Add(30*time.Second)), "30")

	// Act
	// Renew every entry as its window ends so the table stays full.
	later := now.Add(bootstrapRateWindow)
	for i := range bootstrapRateMaxIPs {
		require.NoError(t, b.check(strconv.Itoa(i), later))
	}
	err := b.check("overflow-3", later)

	// Assert
	require.NoError(t, err)
	require.Len(t, b.entries, bootstrapRateMaxIPs)
	require.NotContains(t, b.entries, "overflow-3")
	requireBootstrapThrottle(t, b.check("overflow-4", later), "60")
}

func TestBootstrapRateKey(t *testing.T) {
	tests := []struct {
		name, ip, expected string
	}{
		{name: "ipv4", ip: "192.0.2.1", expected: "192.0.2.1"},
		{name: "ipv6 masked to /64", ip: "2001:db8:1:2:3:4:5:6", expected: "2001:db8:1:2::/64"},
		{name: "ipv4-mapped ipv6", ip: "::ffff:192.0.2.1", expected: "192.0.2.1"},
		{name: "unresolved", ip: "", expected: ""},
		{name: "unparseable", ip: "not-an-ip", expected: "not-an-ip"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Act
			key := bootstrapRateKey(tc.ip)

			// Assert
			require.Equal(t, tc.expected, key)
		})
	}
}

func TestBootstrapRateBudgetSharesIPv6Prefix(t *testing.T) {
	// Arrange
	b := &bootstrapRateBudget{limit: 1}
	now := time.Now()

	// Act
	require.NoError(t, b.check(bootstrapRateKey("2001:db8:1:2::1"), now))
	sameErr := b.check(bootstrapRateKey("2001:db8:1:2:ffff::2"), now)
	otherErr := b.check(bootstrapRateKey("2001:db8:1:3::1"), now)

	// Assert
	requireBootstrapThrottle(t, sameErr, "60")
	require.NoError(t, otherErr)
}

type bootstrapLogRecorder struct {
	mu      sync.Mutex
	records []slog.Record
}

func (*bootstrapLogRecorder) Enabled(context.Context, slog.Level) bool { return true }
func (r *bootstrapLogRecorder) Handle(_ context.Context, rec slog.Record) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records = append(r.records, rec)
	return nil
}
func (r *bootstrapLogRecorder) WithAttrs([]slog.Attr) slog.Handler { return r }
func (r *bootstrapLogRecorder) WithGroup(string) slog.Handler      { return r }

func TestBootstrapRateBudgetLogsOncePerWindowWithoutIP(t *testing.T) {
	// Arrange
	recorder := &bootstrapLogRecorder{}
	previous := slog.Default()
	slog.SetDefault(slog.New(recorder))
	t.Cleanup(func() { slog.SetDefault(previous) })
	const ip = "192.0.2.1"
	b := &bootstrapRateBudget{limit: 1}
	now := time.Now()
	require.NoError(t, b.check(ip, now))

	// Act
	for range 3 {
		requireBootstrapThrottle(t, b.check(ip, now), "60")
	}

	// Assert
	require.Len(t, recorder.records, 1)
	rec := recorder.records[0]
	require.Equal(t, "fleet node bootstrap throttled", rec.Message)
	attrs := map[string]string{}
	rec.Attrs(func(a slog.Attr) bool {
		attrs[a.Key] = a.Value.String()
		return true
	})
	require.Equal(t, "1", attrs["requests_per_minute"])
	require.Equal(t, "ip_quota", attrs["reason"])
	require.NotContains(t, fmt.Sprint(rec.Message, attrs), ip)

	// Act
	later := now.Add(bootstrapRateWindow)
	require.NoError(t, b.check(ip, later))
	requireBootstrapThrottle(t, b.check(ip, later), "60")

	// Assert
	require.Len(t, recorder.records, 2)
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
			// Arrange
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

			// Act
			// Failed authentication also spends the budget, independently of codec.
			for range bootstrapRegistrationLimit {
				_, err = client.Register(t.Context(), connect.NewRequest(&pb.RegisterRequest{}))
				require.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))
			}

			// Assert
			_, err = client.Register(t.Context(), connect.NewRequest(&pb.RegisterRequest{}))
			requireBootstrapThrottle(t, err, "60")

			// Act
			for range bootstrapHandshakeLimit / 2 {
				_, err = client.BeginAuthHandshake(t.Context(), connect.NewRequest(&pb.BeginAuthHandshakeRequest{}))
				require.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))
				_, err = client.CompleteAuthHandshake(t.Context(), connect.NewRequest(&pb.CompleteAuthHandshakeRequest{}))
				require.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))
			}

			// Assert
			_, err = client.BeginAuthHandshake(t.Context(), connect.NewRequest(&pb.BeginAuthHandshakeRequest{}))
			requireBootstrapThrottle(t, err, "60")
			_, err = client.CompleteAuthHandshake(t.Context(), connect.NewRequest(&pb.CompleteAuthHandshakeRequest{}))
			requireBootstrapThrottle(t, err, "60")
			require.EqualValues(t, 720, calls.Load(), "throttled calls must never reach interceptors or database-backed handlers")

			// Act
			clock.Store(now.Add(time.Minute).UnixNano())

			// Assert
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
	// Arrange
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
			// Arrange
			// Give each scenario a fresh quota window.
			now = now.Add(time.Minute)
			ip, err := middleware.NewClientIPMiddleware(tc.trusted)
			require.NoError(t, err)
			wrapped := ip.Wrap(h)

			// Act
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

			// Assert
			require.Equal(t, http.StatusTooManyRequests, rr.Code)
			require.Equal(t, "60", rr.Header().Get("Retry-After"))
			require.Zero(t, body.reads)
		})
	}

	// Arrange
	// Without IP middleware, bootstrap calls share the unresolved-address
	// fallback bucket. Exhaust it at the current clock for both budgets.
	serve := func(procedure string) *httptest.ResponseRecorder {
		handler := connect.NewUnaryHandler(procedure,
			func(context.Context, *connect.Request[pb.RegisterRequest]) (*connect.Response[pb.RegisterResponse], error) {
				return connect.NewResponse(&pb.RegisterResponse{}), nil
			}, gate)
		req := httptest.NewRequest(http.MethodPost, procedure, bytes.NewReader(nil))
		req.Header.Set("Content-Type", "application/proto")
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		return rr
	}
	for _, procedure := range []string{
		fleetnodegatewayv1connect.FleetNodeGatewayServiceRegisterProcedure,
		fleetnodegatewayv1connect.FleetNodeGatewayServiceBeginAuthHandshakeProcedure,
	} {
		for range bootstrapHandshakeLimit {
			serve(procedure)
		}
		require.Equal(t, http.StatusTooManyRequests, serve(procedure).Code)
	}

	// Act
	// This same option must leave every operational procedure outside the quotas.
	for _, procedure := range []string{
		fleetnodegatewayv1connect.FleetNodeGatewayServiceUploadHeartbeatProcedure,
		fleetnodegatewayv1connect.FleetNodeGatewayServiceControlStreamProcedure,
		fleetnodegatewayv1connect.FleetNodeGatewayServiceUploadCommandArtifactProcedure,
		fleetnodegatewayv1connect.FleetNodeGatewayServiceDownloadCommandArtifactProcedure,
		fleetnodegatewayv1connect.FleetNodeGatewayServiceReportDiscoveredDevicesProcedure,
		fleetnodegatewayv1connect.FleetNodeGatewayServiceReportPairedDevicesProcedure,
		fleetnodegatewayv1connect.FleetNodeGatewayServiceUploadTelemetryProcedure,
		fleetnodegatewayv1connect.FleetNodeGatewayServiceUploadEventsProcedure,
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

				// Assert
				require.Equal(t, http.StatusOK, rr.Code)
			}
		})
	}
}
