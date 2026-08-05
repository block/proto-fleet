package updates

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/updaterapi"
)

type executorRequestObservation struct {
	method      string
	path        string
	host        string
	contentType string
	trigger     updaterapi.TriggerRequest
	decodeErr   error
}

type executorRoundTripFunc func(*http.Request) (*http.Response, error)

func (f executorRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func startExecutorTestServer(t *testing.T, handler http.Handler) *unixExecutorClient {
	t.Helper()
	root, err := os.MkdirTemp("/tmp", "pf-executor-test-") //nolint:usetesting // Unix socket paths must stay short on macOS.
	require.NoError(t, err)
	socketPath := filepath.Join(root, "updater.sock")
	listener, err := net.Listen("unix", socketPath)
	require.NoError(t, err)

	server := &http.Server{Handler: handler, ReadHeaderTimeout: time.Second}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()

	client, ok := newExecutorClient(socketPath).(*unixExecutorClient)
	require.True(t, ok)
	t.Cleanup(func() {
		client.http.CloseIdleConnections()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			assert.NoError(t, server.Close())
		}
		assert.ErrorIs(t, <-done, http.ErrServerClosed)
		assert.NoError(t, os.RemoveAll(root))
	})
	return client
}

func TestUnixExecutorClientStatus(t *testing.T) {
	t.Parallel()

	want := updaterapi.Operation{
		ID:            "11111111-1111-4111-8111-111111111111",
		TargetVersion: "v1.2.3",
		Phase:         updaterapi.PhasePreflight,
	}
	observed := make(chan executorRequestObservation, 1)
	client := startExecutorTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observed <- executorRequestObservation{method: r.Method, path: r.URL.Path, host: r.Host}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(updaterapi.StatusResponse{Operation: &want})
	}))

	status, err := client.Status(t.Context())
	require.NoError(t, err)
	require.NotNil(t, status.Operation)
	assert.Equal(t, want, *status.Operation)
	assert.Equal(t, executorRequestObservation{
		method: http.MethodGet,
		path:   "/v1/status",
		host:   "updater",
	}, <-observed)
}

func TestUnixExecutorClientTrigger(t *testing.T) {
	t.Parallel()

	operationID := "11111111-1111-4111-8111-111111111111"
	want := updaterapi.Operation{
		ID:            operationID,
		TargetVersion: "v1.2.3",
		Phase:         updaterapi.PhaseQueued,
	}
	observed := make(chan executorRequestObservation, 1)
	client := startExecutorTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var trigger updaterapi.TriggerRequest
		decoder := json.NewDecoder(r.Body)
		decodeErr := decoder.Decode(&trigger)
		if decodeErr == nil {
			decodeErr = decoder.Decode(&struct{}{})
			if errors.Is(decodeErr, io.EOF) {
				decodeErr = nil
			}
		}
		observed <- executorRequestObservation{
			method:      r.Method,
			path:        r.URL.Path,
			host:        r.Host,
			contentType: r.Header.Get("Content-Type"),
			trigger:     trigger,
			decodeErr:   decodeErr,
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(updaterapi.TriggerResponse{Operation: want})
	}))

	operation, err := client.Trigger(t.Context(), operationID, "v1.2.3")
	require.NoError(t, err)
	assert.Equal(t, want, operation)
	observation := <-observed
	assert.Equal(t, http.MethodPost, observation.method)
	assert.Equal(t, "/v1/upgrade", observation.path)
	assert.Equal(t, "updater", observation.host)
	assert.Equal(t, "application/json", observation.contentType)
	assert.Equal(t, updaterapi.TriggerRequest{OperationID: operationID, TargetVersion: "v1.2.3"}, observation.trigger)
	assert.NoError(t, observation.decodeErr)
}

func TestUnixExecutorClientHTTPFailures(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name       string
		statusCode int
		body       string
		message    string
	}{
		{name: "bad request", statusCode: http.StatusBadRequest, body: `{"error":"invalid target"}`, message: "invalid target"},
		{name: "precondition", statusCode: http.StatusPreconditionFailed, body: `{"error":"target is not newer"}`, message: "target is not newer"},
		{name: "conflict", statusCode: http.StatusConflict, body: `{"error":"upgrade already running"}`, message: "upgrade already running"},
		{name: "unavailable", statusCode: http.StatusServiceUnavailable, body: `{"error":"updater is shutting down"}`, message: "updater is shutting down"},
		{name: "internal", statusCode: http.StatusInternalServerError, body: `{"error":"host updater failed"}`, message: "host updater failed"},
		{name: "malformed body", statusCode: http.StatusBadGateway, body: `{`, message: "502 Bad Gateway"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			client := startExecutorTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.statusCode)
				_, _ = io.WriteString(w, test.body)
			}))

			_, err := client.Status(t.Context())
			require.Error(t, err)
			var httpErr *executorHTTPError
			require.ErrorAs(t, err, &httpErr)
			assert.Equal(t, test.statusCode, httpErr.StatusCode)
			assert.Equal(t, test.message, httpErr.Message)
		})
	}
}

func TestUnixExecutorClientRejectsInvalidSuccessResponses(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name string
		body string
	}{
		{name: "malformed", body: `{"operation":`},
		{name: "second JSON value", body: `{} {}`},
		{name: "oversized", body: `{}` + strings.Repeat(" ", int(maxExecutorSuccessResponseBytes))},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			client := startExecutorTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, test.body)
			}))

			_, err := client.Status(t.Context())
			require.Error(t, err)
			var protocolErr *executorProtocolError
			assert.ErrorAs(t, err, &protocolErr)
		})
	}
}

func TestUnixExecutorClientRejectsMismatchedTriggerIdentity(t *testing.T) {
	t.Parallel()

	client := startExecutorTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(updaterapi.TriggerResponse{Operation: updaterapi.Operation{
			ID:            "22222222-2222-4222-8222-222222222222",
			TargetVersion: "v9.9.9",
		}})
	}))

	_, err := client.Trigger(t.Context(), "11111111-1111-4111-8111-111111111111", "v1.2.3")
	require.Error(t, err)
	var protocolErr *executorProtocolError
	assert.ErrorAs(t, err, &protocolErr)
}

func TestUnixExecutorClientUnavailableSocket(t *testing.T) {
	t.Parallel()

	root, err := os.MkdirTemp("/tmp", "pf-executor-unavailable-") //nolint:usetesting // Unix socket paths must stay short on macOS.
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, os.RemoveAll(root)) })

	missingClient := newExecutorClient(filepath.Join(root, "missing.sock"))
	_, err = missingClient.Status(t.Context())
	assert.ErrorIs(t, err, errExecutorUnavailable)

	stalePath := filepath.Join(root, "stale.sock")
	stale, err := net.ListenUnix("unix", &net.UnixAddr{Name: stalePath, Net: "unix"})
	require.NoError(t, err)
	stale.SetUnlinkOnClose(false)
	require.NoError(t, stale.Close())
	staleClient := newExecutorClient(stalePath)
	require.Eventually(t, func() bool {
		_, statusErr := staleClient.Status(t.Context())
		return errors.Is(statusErr, errExecutorUnavailable)
	}, 2*time.Second, 10*time.Millisecond)
}

func TestUnixExecutorClientHonorsContext(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name       string
		newContext func() (context.Context, context.CancelFunc)
		cancel     bool
		want       error
	}{
		{
			name: "cancellation",
			newContext: func() (context.Context, context.CancelFunc) {
				return context.WithCancel(context.Background())
			},
			cancel: true,
			want:   context.Canceled,
		},
		{
			name: "deadline",
			newContext: func() (context.Context, context.CancelFunc) {
				return context.WithTimeout(context.Background(), 50*time.Millisecond)
			},
			want: context.DeadlineExceeded,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			entered := make(chan struct{}, 1)
			client := startExecutorTestServer(t, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				entered <- struct{}{}
				<-r.Context().Done()
			}))
			ctx, cancel := test.newContext()
			defer cancel()
			done := make(chan error, 1)
			go func() {
				_, statusErr := client.Status(ctx)
				done <- statusErr
			}()
			<-entered
			if test.cancel {
				cancel()
			}
			select {
			case err := <-done:
				assert.ErrorIs(t, err, test.want)
				var transportErr *executorTransportError
				assert.ErrorAs(t, err, &transportErr)
			case <-time.After(time.Second):
				t.Fatal("executor request did not honor its context")
			}
			assert.Equal(t, executorHTTPTimeout, client.http.Timeout)
		})
	}
}

func TestUnixExecutorClientContextWinsOverUnavailableTransportError(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name   string
		cancel bool
		want   error
	}{
		{name: "cancellation", cancel: true, want: context.Canceled},
		{name: "deadline", want: context.DeadlineExceeded},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			entered := make(chan struct{})
			client := &unixExecutorClient{http: &http.Client{Transport: executorRoundTripFunc(
				func(request *http.Request) (*http.Response, error) {
					close(entered)
					<-request.Context().Done()
					return nil, fmt.Errorf("%w: %v", errExecutorUnavailable, request.Context().Err())
				},
			)}}
			var ctx context.Context
			var cancel context.CancelFunc
			if test.cancel {
				ctx, cancel = context.WithCancel(context.Background())
			} else {
				ctx, cancel = context.WithTimeout(context.Background(), 50*time.Millisecond)
			}
			defer cancel()

			done := make(chan error, 1)
			go func() {
				_, statusErr := client.Status(ctx)
				done <- statusErr
			}()
			<-entered
			if test.cancel {
				cancel()
			}

			select {
			case err := <-done:
				require.ErrorIs(t, err, test.want)
				assert.NotErrorIs(t, err, errExecutorUnavailable)
				var transportErr *executorTransportError
				assert.ErrorAs(t, err, &transportErr)
			case <-time.After(time.Second):
				t.Fatal("executor request did not preserve its context error")
			}
		})
	}
}

func TestTriggerUpgradeRetriesLostUnixResponseWithSameOperationID(t *testing.T) {
	t.Parallel()

	var (
		mu          sync.Mutex
		operation   *updaterapi.Operation
		requests    []updaterapi.TriggerRequest
		starts      int
		statusReads int
	)
	retryArrived := make(chan struct{})
	admitted := make(chan struct{})
	handlerErr := make(chan error, 1)
	client := startExecutorTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/status":
			mu.Lock()
			statusReads++
			var snapshot *updaterapi.Operation
			if operation != nil {
				operationSnapshot := *operation
				snapshot = &operationSnapshot
			}
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(updaterapi.StatusResponse{Operation: snapshot})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/upgrade":
			var request updaterapi.TriggerRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				handlerErr <- err
				return
			}
			mu.Lock()
			requests = append(requests, request)
			requestNumber := len(requests)
			mu.Unlock()
			if requestNumber == 1 {
				hijacker, ok := w.(http.Hijacker)
				if !ok {
					handlerErr <- errors.New("test response writer cannot hijack connection")
					return
				}
				connection, _, err := hijacker.Hijack()
				if err != nil {
					handlerErr <- err
					return
				}
				_ = connection.Close() // Lose the first response before admission is visible.
				go func() {
					<-retryArrived
					mu.Lock()
					operation = &updaterapi.Operation{
						ID:            request.OperationID,
						TargetVersion: request.TargetVersion,
						Phase:         updaterapi.PhaseQueued,
					}
					starts++
					mu.Unlock()
					close(admitted)
				}()
				return
			}
			close(retryArrived)
			<-admitted
			mu.Lock()
			response := *operation
			mu.Unlock()
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(updaterapi.TriggerResponse{Operation: response})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))

	svc := newEligibleUpgradeService(t)
	svc.executor = client
	result, err := svc.TriggerUpgrade(context.Background(), 1, "v1.1.0")
	require.NoError(t, err)
	assert.NotEmpty(t, result.ID)
	assert.Equal(t, "v1.1.0", result.TargetVersion)
	assert.Equal(t, updaterapi.PhaseQueued, result.Phase)
	mu.Lock()
	recordedRequests := append([]updaterapi.TriggerRequest(nil), requests...)
	recordedStarts := starts
	recordedStatusReads := statusReads
	mu.Unlock()
	require.Len(t, recordedRequests, 2)
	assert.Equal(t, recordedRequests[0], recordedRequests[1], "the retry must reuse the idempotency key")
	assert.Equal(t, result.ID, recordedRequests[0].OperationID)
	assert.Equal(t, 1, recordedStarts)
	assert.Equal(t, 1, recordedStatusReads,
		"only the availability read should run; a successful idempotent retry needs no reconciliation read")
	select {
	case err := <-handlerErr:
		require.NoError(t, err)
	default:
	}
}
