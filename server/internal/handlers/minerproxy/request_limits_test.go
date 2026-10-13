package minerproxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/stretchr/testify/require"
)

func TestProxyReadClassificationCoversAllowlist(t *testing.T) {
	for _, endpoint := range v2AllowlistedEndpoints {
		t.Run(endpoint.method+"/"+endpoint.rest, func(t *testing.T) {
			wantMutation := endpoint.method != http.MethodGet && endpoint.method != http.MethodHead
			require.Equal(t, wantMutation, proxyMutatesMiner(endpoint.method, endpoint.proxyPath()))
		})
	}
	for _, endpoint := range []string{"/api/v1/timeseries", minerLoginPath} {
		for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
			wantMutation := method != http.MethodGet && method != http.MethodHead && method != http.MethodPost
			require.Equal(t, wantMutation, proxyMutatesMiner(method, endpoint), "%s %s", method, endpoint)
		}
	}
}

func TestProxyInternalErrorsAreNotDisclosed(t *testing.T) {
	for _, err := range []error{
		errors.New("record direct device write: pq: fixture-secret-schema"),
		fleeterror.NewInternalError("acquire dispatch connection: fixture-secret-driver"),
		fmt.Errorf("wrapped: %w", fleeterror.NewInternalError("fixture-secret")),
	} {
		require.Equal(t, http.StatusInternalServerError, httpStatusForError(err))
		require.Equal(t, "miner proxy request failed", clientMessageForError(err))
	}
	require.Equal(t, "miner proxy request timed out", clientMessageForError(fmt.Errorf("driver: %w", context.DeadlineExceeded)))
}

// Test through a real HTTP server: the upstream operation's deadline must not
// prevent the independently bounded downstream 502 from being delivered.
func TestProxyTimeoutDeliversBadGatewayResponse(t *testing.T) {
	for _, http2 := range []bool{false, true} {
		t.Run(fmt.Sprintf("http2=%t", http2), func(t *testing.T) {
			database, _, deviceID, h, target := proxyFenceFixture(t)
			h.httpClient = &http.Client{Timeout: 500 * time.Millisecond, Transport: proxyRoundTripFunc(func(r *http.Request) (*http.Response, error) {
				<-r.Context().Done()
				return nil, r.Context().Err()
			})}
			template := authenticatedProxyRequest(t, database, h, target)
			done := make(chan struct{})
			server := httptest.NewUnstartedServer(middleware.TelemetryMiddleware{}.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				for _, key := range []string{"deviceIdentifier", "apiVersion", "rest"} {
					r.SetPathValue(key, template.PathValue(key))
				}
				h.ServeHTTP(w, r)
				close(done)
			})))
			server.EnableHTTP2 = http2
			server.StartTLS()
			t.Cleanup(server.Close)
			request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL, nil)
			require.NoError(t, err)
			request.Header = template.Header.Clone()
			response, err := server.Client().Do(request)
			require.NoError(t, err, "an upstream timeout must deliver a response rather than drop the connection")
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			require.NoError(t, err)
			require.Equal(t, http.StatusBadGateway, response.StatusCode)
			require.Contains(t, string(body), "timed out")
			awaitProxyBarrier(t, done)
			assertNoProxyAttempt(t, database, deviceID)

		})
	}
}

func TestProxyBindingWaitExpiresBeforeControlDispatch(t *testing.T) {
	database, _, deviceID, h, target := proxyFenceFixture(t)
	locked := make(chan struct{})
	release := make(chan struct{})
	finish := sync.OnceFunc(func() { close(release) })
	t.Cleanup(finish)
	lockDone := make(chan error, 1)
	go func() {
		lockDone <- db.WithTransactionNoResult(t.Context(), database.DB, func(q sqlc.Querier) error {
			if _, err := q.LockMinerBindingDevice(t.Context(), deviceID); err != nil {
				return err
			}
			close(locked)
			<-release
			return nil
		})
	}()
	awaitProxyBarrier(t, locked)
	transport := &countingProxyTransport{}
	h.httpClient = &http.Client{Timeout: 500 * time.Millisecond, Transport: transport}
	request := authenticatedProxyRequest(t, database, h, target)
	request.Method = http.MethodPost
	request.SetPathValue("rest", "mining/start")
	response := newDeadlineProxyRecorder()
	done := make(chan struct{})
	go func() { h.ServeHTTP(response, request); close(done) }()
	require.Eventually(t, func() bool {
		var waiting bool
		err := database.DB.QueryRowContext(t.Context(), `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%SELECT discovered_device_id FROM device%FOR NO KEY UPDATE%')`).Scan(&waiting)
		return err == nil && waiting
	}, 400*time.Millisecond, 5*time.Millisecond, "the request must reach its binding lock before expiration")
	awaitProxyBarrier(t, done)
	require.Equal(t, http.StatusBadGateway, response.Code)
	require.Zero(t, transport.calls.Load())
	assertNoProxyAttempt(t, database, deviceID)
	finish()
	require.NoError(t, <-lockDone)
	// Clearing contention cannot cause the expired request to send later.
	require.Zero(t, transport.calls.Load())
	require.NoError(t, db.WithDispatchTransaction(t.Context(), database.DB, func(sqlc.Querier) error { return nil }))
}

func TestProxyCapacityLeavesCommandDispatchAvailable(t *testing.T) {
	database, orgID, deviceID, h, target := proxyFenceFixture(t)
	database.DB.SetMaxOpenConns(4)       // Shared dispatch capacity two; proxy gets at most one.
	h.config.MaxConcurrentRequests = 100 // Clamp operator configuration below shared capacity.
	entered := make(chan struct{})
	release := make(chan struct{})
	finish := sync.OnceFunc(func() { close(release) })
	t.Cleanup(finish)
	var calls atomic.Int64
	h.httpClient = &http.Client{Transport: proxyRoundTripFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		close(entered)
		<-release // Retain the slot until the backend actually returns after cancellation.
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("{}"))}, nil
	})}
	firstRequest := authenticatedProxyRequest(t, database, h, target)
	secondRequest := authenticatedProxyRequest(t, database, h, target)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan struct{})
	go func() { h.ServeHTTP(newDeadlineProxyRecorder(), firstRequest.WithContext(ctx)); close(done) }()
	t.Cleanup(func() { finish(); awaitProxyBarrier(t, done) })
	awaitProxyBarrier(t, entered)
	cancel()
	rejected := newDeadlineProxyRecorder()
	h.ServeHTTP(rejected, secondRequest)
	require.Equal(t, http.StatusServiceUnavailable, rejected.Code)
	require.Equal(t, int64(1), calls.Load())
	var count int
	require.NoError(t, database.DB.QueryRowContext(t.Context(), "SELECT count(*) FROM device_write_attempt WHERE device_id=$1", deviceID).Scan(&count))
	require.Equal(t, 1, count, "busy rejection must not create another admission")
	otherDevice := database.CreateDevice(orgID, "proto")
	commandCtx, stop := context.WithTimeout(t.Context(), time.Second)
	defer stop()
	invoked := false
	require.NoError(t, h.writeService.WithHostAttempt(commandCtx, orgID, otherDevice.DatabaseID, 0, commandtype.SetPowerTarget, func(ctx context.Context) error {
		return h.writeService.WithBindingDispatch(ctx, otherDevice.DatabaseID, func(sqlc.Querier) error { return nil }, func(context.Context) error { invoked = true; return nil })
	}))
	require.True(t, invoked, "proxy saturation must leave command dispatch available")
	finish()
	awaitProxyBarrier(t, done)
	assertNoProxyAttempt(t, database, deviceID)
}

func TestProxyCapacityRequiresCommandHeadroom(t *testing.T) {
	database, _, deviceID, h, target := proxyFenceFixture(t)
	database.DB.SetMaxOpenConns(2) // Only one shared dispatch slot, reserved for commands.
	transport := &countingProxyTransport{}
	h.httpClient = &http.Client{Transport: transport}
	response := newDeadlineProxyRecorder()
	h.ServeHTTP(response, authenticatedProxyRequest(t, database, h, target))
	require.Equal(t, http.StatusServiceUnavailable, response.Code)
	require.Zero(t, transport.calls.Load())
	assertNoProxyAttempt(t, database, deviceID)
}
