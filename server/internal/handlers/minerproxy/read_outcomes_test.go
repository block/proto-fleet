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
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/require"
)

type proxyReaderFunc func([]byte) (int, error)

func (f proxyReaderFunc) Read(p []byte) (int, error) { return f(p) }

type proxyRoundTripFunc func(*http.Request) (*http.Response, error)

func (f proxyRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type proxyFailingWriter struct{ *deadlineProxyRecorder }

func (w proxyFailingWriter) Write([]byte) (int, error) { return 0, errors.New("fixture-copy-secret") }

func TestRepeatedProxyPollingRetiresCompletedAdmission(t *testing.T) {
	database, _, deviceID, h, target := proxyFenceFixture(t)
	h.httpClient = &http.Client{Transport: proxyRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("{}"))}, nil
	})}
	for range 20 {
		response := newDeadlineProxyRecorder()
		h.ServeHTTP(response, authenticatedProxyRequest(t, database, h, target))
		require.Equal(t, http.StatusOK, response.Code)
		require.Equal(t, "{}", response.Body.String())
	}
	assertNoProxyAttempt(t, database, deviceID)
}

func TestProxyFirmwareCheckDoesNotFenceLaterWrites(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusAccepted, http.StatusServiceUnavailable} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			database, orgID, deviceID, h, target := proxyFenceFixture(t)
			h.storeToken(target.cacheKey(), "fixture-token")
			h.httpClient = &http.Client{Transport: proxyRoundTripFunc(func(r *http.Request) (*http.Response, error) {
				require.Equal(t, "/api/v1/system/update/check", r.URL.Path)
				var kind string
				require.NoError(t, database.DB.QueryRowContext(t.Context(), "SELECT operation_kind FROM device_write_attempt WHERE device_id=$1", deviceID).Scan(&kind))
				require.Equal(t, "observation", kind, "availability checks must declare read-only admission before I/O")
				return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("{}"))}, nil
			})}
			request := authenticatedProxyRequest(t, database, h, target)
			request.Method = http.MethodPost
			request.SetPathValue("apiVersion", "v1")
			request.SetPathValue("rest", "system/update/check")
			response := newDeadlineProxyRecorder()
			h.ServeHTTP(response, request)
			require.Equal(t, status, response.Code)
			assertNoProxyAttempt(t, database, deviceID)
			epoch, err := h.writeService.Reserve(t.Context(), orgID, deviceID)
			require.NoError(t, err)
			drained, err := h.writeService.Drained(t.Context(), orgID, deviceID, epoch)
			require.NoError(t, err)
			require.True(t, drained, "opening the miner page must not leave a configuration write fence")
		})
	}
}

func TestCanceledProxyReadAllowsRetiredIdentityReuse(t *testing.T) {
	database, orgID, deviceID, h, target := proxyFenceFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	body := &proxyBodyBarrier{
		reader:      proxyReaderFunc(func([]byte) (int, error) { return 0, fmt.Errorf("fixture canceled read: %w", ctx.Err()) }),
		readStarted: make(chan struct{}), readRelease: make(chan struct{}),
		closeStarted: make(chan struct{}), closeRelease: make(chan struct{}),
	}
	releaseRead := sync.OnceFunc(func() { close(body.readRelease) })
	releaseClose := sync.OnceFunc(func() { close(body.closeRelease) })
	t.Cleanup(releaseRead)
	t.Cleanup(releaseClose)
	transport := &proxyResponseTransport{status: http.StatusOK, body: body}
	h.httpClient = &http.Client{Transport: transport}
	source := authenticatedProxyRequest(t, database, h, target).WithContext(ctx)
	done := make(chan struct{})
	go func() { h.ServeHTTP(newDeadlineProxyRecorder(), source); close(done) }()
	awaitProxyBarrier(t, body.readStarted)
	cancel()
	require.Equal(t, "running", proxyAttemptStatus(t, database, deviceID))
	drained, err := h.writeService.Drained(t.Context(), orgID, deviceID, target.binding.WriteEpoch)
	require.NoError(t, err)
	require.False(t, drained, "cancellation must not release an active response lifetime")
	releaseRead()
	awaitProxyBarrier(t, body.closeStarted)
	require.Equal(t, "running", proxyAttemptStatus(t, database, deviceID))
	releaseClose()
	awaitProxyBarrier(t, done)
	require.Equal(t, int64(1), transport.calls.Load())
	assertNoProxyAttempt(t, database, deviceID)
	// Retire through the production store, then reuse the exact physical identity.
	successorID := retireAndRediscoverProxyIdentity(t, database, target)
	attempt, err := h.writeService.BeginHost(t.Context(), orgID, successorID, 0)
	require.NoError(t, err, "a canceled dashboard read must not poison write admission for the rediscovered identity")
	require.NoError(t, attempt.Finish(t.Context(), nil))
}

func TestProxyObservationFailuresDoNotPoisonDrain(t *testing.T) {
	shared := newProxyFenceDatabase(t)
	database, orgID := shared.database, shared.org
	for _, operation := range []struct{ name, method, path string }{
		{"GET", http.MethodGet, "/api/v2/system/info"},
		{"HEAD", http.MethodHead, "/api/v1/system"},
		{"timeseries-POST", http.MethodPost, "/api/v1/timeseries"},
		{"logs-GET", http.MethodGet, "/api/v1/system/logs/download"},
		{"auth-login-POST", http.MethodPost, "/api/v1/auth/login"},
		{"firmware-check-POST", http.MethodPost, "/api/v1/system/update/check"},
	} {
		for _, failure := range []string{"read", "close", "copy", "network"} {
			if operation.method == http.MethodHead && (failure == "read" || failure == "copy") {
				continue
			}
			t.Run(operation.name+"/"+failure, func(t *testing.T) {
				deviceID, h, target := shared.pairedDevice(t)
				body := &proxyBodyBarrier{reader: strings.NewReader("{}")}
				switch failure {
				case "read":
					body.reader = proxyErrorReader{}
				case "close":
					body.closeErr = errors.New("fixture-close-secret")
				}
				h.httpClient = &http.Client{Transport: &proxyResponseTransport{status: http.StatusOK, body: body}}
				if failure == "network" {
					h.httpClient.Transport = proxyRoundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("fixture-network-secret") })
				}
				var response http.ResponseWriter = newDeadlineProxyRecorder()
				if failure == "copy" {
					response = proxyFailingWriter{newDeadlineProxyRecorder()}
				}
				started := false
				err := h.writeService.WithHostAttempt(t.Context(), orgID, deviceID, target.binding.WriteEpoch, commandtype.Type(-1), func(ctx context.Context) error {
					return h.forwardAndCopy(ctx, response, httptest.NewRequest(operation.method, "http://fleet.test/", nil), target, operation.path, nil, &started)
				})
				require.Error(t, err)
				require.NotContains(t, err.Error(), "fixture-")
				assertNoProxyAttempt(t, database, deviceID)
				epoch, err := h.writeService.Reserve(t.Context(), orgID, deviceID)
				require.NoError(t, err)
				drained, err := h.writeService.Drained(t.Context(), orgID, deviceID, epoch)
				require.NoError(t, err)
				require.True(t, drained, "failed observation must not create physical-write uncertainty")
			})
		}
	}
}

func TestProxyUnclassifiedMutationFailuresStillPoisonDrain(t *testing.T) {
	shared := newProxyFenceDatabase(t)
	database, orgID := shared.database, shared.org
	for _, operation := range []struct{ name, path string }{
		{"run-in", "/api/v2/modules/T1-M01/run-in"}, {"logs-POST", "/api/v1/system/logs/download"}, {"unknown-POST", "/api/v1/new-setting"},
	} {
		for _, failure := range []string{"read", "close", "copy", "network"} {
			t.Run(operation.name+"/"+failure, func(t *testing.T) {
				deviceID, h, target := shared.pairedDevice(t)
				body := &proxyBodyBarrier{reader: strings.NewReader("{}")}
				if failure == "read" {
					body.reader = proxyErrorReader{}
				}
				if failure == "close" {
					body.closeErr = errors.New("fixture-close-secret")
				}
				h.httpClient = &http.Client{Transport: &proxyResponseTransport{status: http.StatusOK, body: body}}
				if failure == "network" {
					h.httpClient.Transport = proxyRoundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("fixture-network-secret") })
				}
				var response http.ResponseWriter = newDeadlineProxyRecorder()
				if failure == "copy" {
					response = proxyFailingWriter{newDeadlineProxyRecorder()}
				}
				started := false
				err := h.writeService.WithHostAttempt(t.Context(), orgID, deviceID, target.binding.WriteEpoch, commandtype.Type(-1), func(ctx context.Context) error {
					return h.forwardAndCopy(ctx, response, httptest.NewRequest(http.MethodPost, "http://fleet.test/", nil), target, operation.path, nil, &started)
				})
				require.Error(t, err)
				require.Equal(t, "unknown", proxyAttemptStatus(t, database, deviceID))
				_, err = h.writeService.Reserve(t.Context(), orgID, deviceID)
				require.True(t, fleeterror.IsFailedPreconditionError(err))
			})
		}
	}
}

func TestProxyMutation401ThenFailedLoginRetainsUnknownOutcome(t *testing.T) {
	database, orgID, deviceID, h, target := proxyFenceFixture(t)
	encrypted, err := h.encryptService.Encrypt([]byte("proxy-fixture-password"))
	require.NoError(t, err)
	require.NoError(t, sqlc.New(database.DB).UpsertMinerCredentials(t.Context(), sqlc.UpsertMinerCredentialsParams{DeviceID: deviceID, UsernameEnc: encrypted, PasswordEnc: encrypted}))
	target, err = h.resolveTarget(t.Context(), target.deviceIdentifier, orgID)
	require.NoError(t, err)
	h.storeToken(target.cacheKey(), "fixture-expired-token")
	var paths []string
	h.httpClient = &http.Client{Transport: proxyRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		paths = append(paths, r.URL.Path)
		if len(paths) == 1 {
			return &http.Response{StatusCode: http.StatusUnauthorized, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("{}"))}, nil
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(proxyErrorReader{})}, nil
	})}
	started := false
	err = h.writeService.WithHostAttempt(t.Context(), orgID, deviceID, target.binding.WriteEpoch, commandtype.Type(-1), func(ctx context.Context) error {
		return h.forwardAndCopy(ctx, newDeadlineProxyRecorder(), httptest.NewRequest(http.MethodPost, "http://fleet.test/", nil), target, "/api/v1/system", nil, &started)
	})
	require.Error(t, err)
	require.Equal(t, []string{"/api/v1/system", "/api/v1/auth/login"}, paths)
	require.Equal(t, "unknown", proxyAttemptStatus(t, database, deviceID), "observational login must never clear a prior mutation marker")
	_, err = h.writeService.Reserve(t.Context(), orgID, deviceID)
	require.True(t, fleeterror.IsFailedPreconditionError(err))
}

func retireAndRediscoverProxyIdentity(t *testing.T, database *testutil.DatabaseService, target proxyTarget) int64 {
	t.Helper()
	count, err := sqlstores.NewSQLDeviceStore(database.DB).SoftDeleteDevices(t.Context(), []string{target.deviceIdentifier}, target.binding.OrgID)
	require.NoError(t, err)
	require.Equal(t, int64(1), count)
	q := sqlc.New(database.DB)
	discoveryID, err := q.UpsertDiscoveredDevice(t.Context(), sqlc.UpsertDiscoveredDeviceParams{
		OrgID: target.binding.OrgID, DeviceIdentifier: target.deviceIdentifier, IpAddress: target.binding.IpAddress,
		Port: target.binding.Port, UrlScheme: target.binding.UrlScheme, IsActive: true, DriverName: "proto", DirectDiscovery: true,
	})
	require.NoError(t, err)
	successorID, err := q.InsertDevice(t.Context(), sqlc.InsertDeviceParams{
		OrgID: target.binding.OrgID, DiscoveredDeviceID: discoveryID, DeviceIdentifier: target.deviceIdentifier,
		MacAddress: target.binding.MacAddress, SerialNumber: target.binding.SerialNumber,
	})
	require.NoError(t, err)
	require.NotEqual(t, target.binding.ID, successorID)
	return successorID
}

func TestProxyMutationFailureStillBlocksRetiredIdentityReuse(t *testing.T) {
	database, orgID, deviceID, h, target := proxyFenceFixture(t)
	h.httpClient = &http.Client{Transport: proxyRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("fixture-network-secret")
	})}
	started := false
	err := h.writeService.WithHostAttempt(t.Context(), orgID, deviceID, target.binding.WriteEpoch, commandtype.Type(-1), func(ctx context.Context) error {
		return h.forwardAndCopy(ctx, newDeadlineProxyRecorder(), httptest.NewRequest(http.MethodPost, "http://fleet.test/", nil), target, "/api/v2/mining/start", nil, &started)
	})
	require.Error(t, err)
	require.Equal(t, "unknown", proxyAttemptStatus(t, database, deviceID))
	successorID := retireAndRediscoverProxyIdentity(t, database, target)
	_, err = h.writeService.BeginHost(t.Context(), orgID, successorID, 0)
	require.Error(t, err, "the read fix must not release a retired identity with an uncertain mutation")
	var attempts int
	require.NoError(t, database.DB.QueryRowContext(t.Context(), "SELECT count(*) FROM device_write_attempt WHERE device_id=$1", successorID).Scan(&attempts))
	require.Zero(t, attempts, "uncertain physical-write evidence must reject admission before recording a new attempt")
}
