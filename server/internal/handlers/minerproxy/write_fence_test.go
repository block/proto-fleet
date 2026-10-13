package minerproxy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/require"
)

type countingProxyTransport struct{ calls atomic.Int64 }

func (t *countingProxyTransport) RoundTrip(*http.Request) (*http.Response, error) {
	t.calls.Add(1)
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("{}"))}, nil
}

func TestResolvedProxyTargetCannotCrossReservation(t *testing.T) {
	database, orgID, deviceID, h, target := proxyFenceFixture(t)
	transport := &countingProxyTransport{}
	h.httpClient = &http.Client{Transport: transport}
	_, err := devicewrite.NewService(database.DB).Reserve(t.Context(), orgID, deviceID)
	require.NoError(t, err)
	source, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://fleet.test/", nil)
	require.NoError(t, err)
	resp, err := h.forward(t.Context(), source, target, "/api/v2/system/info", nil, false)
	if resp != nil {
		require.NoError(t, resp.Body.Close())
	}
	require.Error(t, err, "a target resolved before reservation must not reach the raw miner API")
	require.Zero(t, transport.calls.Load(), "no raw request may cross the ownership epoch")
}

type proxyBodyBarrier struct {
	reader                                               io.Reader
	readStarted, readRelease, closeStarted, closeRelease chan struct{}
	readOnce, closeOnce                                  sync.Once
	closeErr                                             error
}

func (b *proxyBodyBarrier) Read(p []byte) (int, error) {
	if b.readStarted != nil {
		b.readOnce.Do(func() { close(b.readStarted); <-b.readRelease })
	}
	n, err := b.reader.Read(p)
	if err != nil && err != io.EOF {
		err = fmt.Errorf("proxy fixture response read: %w", err)
	}
	return n, err
}
func (b *proxyBodyBarrier) Close() error {
	if b.closeStarted != nil {
		b.closeOnce.Do(func() { close(b.closeStarted); <-b.closeRelease })
	}
	return b.closeErr
}

type proxyResponseTransport struct {
	calls  atomic.Int64
	status int
	body   io.ReadCloser
}

func (p *proxyResponseTransport) RoundTrip(*http.Request) (*http.Response, error) {
	p.calls.Add(1)
	return &http.Response{StatusCode: p.status, Header: make(http.Header), Body: p.body}, nil
}
func proxyFenceFixture(t *testing.T) (*testutil.DatabaseService, int64, int64, *Handler, proxyTarget) {
	t.Helper()
	cfg, err := testutil.GetTestConfig()
	require.NoError(t, err)
	database := testutil.NewDatabaseService(t, cfg)
	user := database.CreateSuperAdminUser()
	device := database.CreateDevice(user.OrganizationID, "proto")
	q := sqlc.New(database.DB)
	_, err = q.UpsertDevicePairing(t.Context(), sqlc.UpsertDevicePairingParams{DeviceID: device.DatabaseID, PairingStatus: sqlc.PairingStatusEnumPAIRED})
	require.NoError(t, err)
	require.NoError(t, q.UpdateDeviceIPAssignment(t.Context(), sqlc.UpdateDeviceIPAssignmentParams{ID: device.DatabaseID, IpAddress: "192.0.2.18", Port: "8080", UrlScheme: "http"}))
	h := newForwardTestHandler(t)
	h.conn = database.DB
	h.queries = q
	h.writeService = devicewrite.NewService(database.DB)
	h.sessionService = session.NewService(session.Config{Duration: time.Minute, IDBytes: 32, CookieName: "fleet_session"}, sqlstores.NewSQLSessionStore(database.DB))
	h.userStore = sqlstores.NewSQLUserStore(database.DB)
	h.permissionResolver = authz.NewPermissionResolver(database.DB)
	target, err := h.resolveTarget(t.Context(), device.ID, user.OrganizationID)
	require.NoError(t, err)
	return database, user.OrganizationID, device.DatabaseID, h, target
}
func awaitProxyBarrier(t *testing.T, barrier <-chan struct{}) {
	t.Helper()
	select {
	case <-barrier:
	case <-time.After(5 * time.Second):
		t.Fatal("proxy operation did not reach response barrier")
	}
}
func TestProxyAttemptRetainsResponseReadAndCloseLifetime(t *testing.T) {
	database, orgID, deviceID, h, target := proxyFenceFixture(t)
	body := &proxyBodyBarrier{reader: strings.NewReader("{}"), readStarted: make(chan struct{}), readRelease: make(chan struct{}), closeStarted: make(chan struct{}), closeRelease: make(chan struct{})}
	releaseRead := sync.OnceFunc(func() { close(body.readRelease) })
	releaseClose := sync.OnceFunc(func() { close(body.closeRelease) })
	t.Cleanup(releaseRead)
	t.Cleanup(releaseClose)
	transport := &proxyResponseTransport{status: http.StatusOK, body: body}
	h.httpClient = &http.Client{Transport: transport}
	source := authenticatedProxyRequest(t, database, h, target)
	response := newDeadlineProxyRecorder()
	done := make(chan struct{})
	go func() {
		h.ServeHTTP(response, source)
		close(done)
	}()
	assertBindingLocked := func() {
		ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
		defer cancel()
		_, err := database.DB.ExecContext(ctx, "UPDATE discovered_device SET port='8081' WHERE id=(SELECT discovered_device_id FROM device WHERE id=$1)", deviceID)
		require.Error(t, err, "response consumption and Close must retain the captured binding")
	}
	awaitProxyBarrier(t, body.readStarted)
	assertBindingLocked()
	require.Equal(t, "running", proxyAttemptStatus(t, database, deviceID))
	fences := devicewrite.NewService(database.DB)
	epoch, err := fences.Reserve(t.Context(), orgID, deviceID)
	require.NoError(t, err)
	drained, err := fences.Drained(t.Context(), orgID, deviceID, epoch)
	require.NoError(t, err)
	require.False(t, drained, "HTTP headers do not complete the admitted operation")
	releaseRead()
	awaitProxyBarrier(t, body.closeStarted)
	assertBindingLocked()
	require.Equal(t, "running", proxyAttemptStatus(t, database, deviceID))
	drained, err = fences.Drained(t.Context(), orgID, deviceID, epoch)
	require.NoError(t, err)
	require.False(t, drained, "completion must wait for response close")
	releaseClose()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("proxy did not complete after close")
	}
	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, "{}", response.Body.String())
	require.Equal(t, int64(1), transport.calls.Load())
	drained, err = fences.Drained(t.Context(), orgID, deviceID, epoch)
	require.NoError(t, err)
	require.True(t, drained)
	assertNoProxyAttempt(t, database, deviceID)
}
func TestProxyReservationBefore401RetrySendsNoLoginOrResend(t *testing.T) {
	database, orgID, deviceID, h, target := proxyFenceFixture(t)
	encrypted, err := h.encryptService.Encrypt([]byte("proxy-fixture-password"))
	require.NoError(t, err)
	require.NoError(t, sqlc.New(database.DB).UpsertMinerCredentials(t.Context(), sqlc.UpsertMinerCredentialsParams{DeviceID: deviceID, UsernameEnc: encrypted, PasswordEnc: encrypted}))
	target, err = h.resolveTarget(t.Context(), target.deviceIdentifier, orgID)
	require.NoError(t, err)
	h.storeToken(target.cacheKey(), "fixture-expired-token")
	body := &proxyBodyBarrier{reader: strings.NewReader("{}"), closeStarted: make(chan struct{}), closeRelease: make(chan struct{})}
	release := sync.OnceFunc(func() { close(body.closeRelease) })
	t.Cleanup(release)
	transport := &proxyResponseTransport{status: http.StatusUnauthorized, body: body}
	h.httpClient = &http.Client{Transport: transport}
	source := httptest.NewRequest(http.MethodPost, "http://fleet.test/", nil)
	path := "/api/v1/system"
	require.True(t, requiresMinerAuthentication(path))
	response := newDeadlineProxyRecorder()
	started := false
	done := make(chan error, 1)
	go func() {
		done <- h.writeService.WithHostAttempt(t.Context(), orgID, deviceID, target.binding.WriteEpoch, commandtype.Type(-1), func(ctx context.Context) error {
			return h.forwardAndCopy(ctx, response, source, target, path, nil, &started)
		})
	}()
	awaitProxyBarrier(t, body.closeStarted)
	_, err = devicewrite.NewService(database.DB).Reserve(t.Context(), orgID, deviceID)
	require.NoError(t, err)
	release()
	select {
	case err := <-done:
		require.Error(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("proxy did not reject superseded retry")
	}
	require.False(t, started)
	require.Equal(t, int64(1), transport.calls.Load(), "neither a credential login nor a command retry may cross reservation")
	_, cached := h.lookupToken(target.cacheKey())
	require.False(t, cached)
}

type proxyTruncatedReader struct{}

func (proxyTruncatedReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

type proxyErrorReader struct{}

func (proxyErrorReader) Read([]byte) (int, error) { return 0, errors.New("fixture-read-secret") }
func TestProxyResponseFailuresRetainUnknownOutcome(t *testing.T) {
	for _, failure := range []string{"read", "close"} {
		t.Run(failure, func(t *testing.T) {
			database, orgID, deviceID, h, target := proxyFenceFixture(t)
			body := &proxyBodyBarrier{reader: strings.NewReader("{}")}
			if failure == "read" {
				body.reader = proxyErrorReader{}
			} else {
				body.closeErr = errors.New("fixture-close-secret")
			}
			transport := &proxyResponseTransport{status: http.StatusOK, body: body}
			h.httpClient = &http.Client{Transport: transport}
			source := httptest.NewRequest(http.MethodPost, "http://fleet.test/", nil)
			response := newDeadlineProxyRecorder()
			started := false
			err := h.writeService.WithHostAttempt(t.Context(), orgID, deviceID, target.binding.WriteEpoch, commandtype.Type(-1), func(ctx context.Context) error {
				return h.forwardAndCopy(ctx, response, source, target, "/api/v2/mining/start", nil, &started)
			})
			require.Error(t, err)
			require.NotContains(t, err.Error(), "fixture-")
			require.True(t, started)
			var status string
			require.NoError(t, database.DB.QueryRowContext(t.Context(), "SELECT status FROM device_write_attempt WHERE device_id=$1", deviceID).Scan(&status))
			require.Equal(t, "unknown", status)
			_, err = devicewrite.NewService(database.DB).Reserve(t.Context(), orgID, deviceID)
			require.True(t, fleeterror.IsFailedPreconditionError(err))
		})
	}
}
func TestProxyRejectsAmbientTransactionBeforeTransport(t *testing.T) {
	database, orgID, deviceID, h, target := proxyFenceFixture(t)
	transport := &countingProxyTransport{}
	h.httpClient = &http.Client{Transport: transport}
	source := httptest.NewRequest(http.MethodGet, "http://fleet.test/", nil)
	err := db.WithTransactionNoResult(t.Context(), database.DB, func(q sqlc.Querier) error {
		return h.writeService.WithHostAttempt(db.WithTxQueries(t.Context(), q), orgID, deviceID, target.binding.WriteEpoch, commandtype.Type(-1), func(ctx context.Context) error {
			response, err := h.forward(ctx, source, target, "/api/v2/system/info", nil, false)
			if response != nil {
				require.NoError(t, response.Body.Close())
			}
			return err
		})
	})
	require.Error(t, err)
	require.Zero(t, transport.calls.Load())
	var attempts int
	require.NoError(t, database.DB.QueryRowContext(t.Context(), "SELECT count(*) FROM device_write_attempt").Scan(&attempts))
	require.Zero(t, attempts)
}

func TestProxyFailedLoginResponseCannotCacheTokenOrForward(t *testing.T) {
	for _, failure := range []string{"read", "close", "truncated-tail", "oversized", "network", "unauthorized", "unavailable-status"} {
		t.Run(failure, func(t *testing.T) {
			database, orgID, deviceID, h, target := proxyFenceFixture(t)
			encrypted, err := h.encryptService.Encrypt([]byte("proxy-fixture-password"))
			require.NoError(t, err)
			require.NoError(t, sqlc.New(database.DB).UpsertMinerCredentials(t.Context(), sqlc.UpsertMinerCredentialsParams{DeviceID: deviceID, UsernameEnc: encrypted, PasswordEnc: encrypted}))
			target, err = h.resolveTarget(t.Context(), target.deviceIdentifier, orgID)
			require.NoError(t, err)
			body := &proxyBodyBarrier{reader: strings.NewReader(`{"access_token":"fixture-login-token"}`)}
			if failure == "read" {
				body.reader = proxyErrorReader{}
			} else if failure == "truncated-tail" {
				body.reader = io.MultiReader(body.reader, proxyTruncatedReader{})
			} else if failure == "oversized" {
				body.reader = strings.NewReader(`{"access_token":"fixture-login-token"}` + strings.Repeat(" ", maxLoginResponseBytes))
			} else if failure == "close" {
				body.closeErr = errors.New("fixture-close-secret")
			}
			responseStatus := http.StatusOK
			if failure == "unauthorized" {
				responseStatus = http.StatusUnauthorized
			} else if failure == "unavailable-status" {
				responseStatus = http.StatusServiceUnavailable
			}
			transport := &proxyResponseTransport{status: responseStatus, body: body}
			h.httpClient = &http.Client{Transport: transport}
			if failure == "network" {
				h.httpClient.Transport = proxyRoundTripFunc(func(*http.Request) (*http.Response, error) {
					transport.calls.Add(1)
					return nil, errors.New("fixture-network-secret")
				})
			}
			response := newDeadlineProxyRecorder()
			started := false
			err = h.writeService.WithHostAttempt(t.Context(), orgID, deviceID, target.binding.WriteEpoch, commandtype.Type(-1), func(ctx context.Context) error {
				return h.forwardAndCopy(ctx, response, httptest.NewRequest(http.MethodPost, "http://fleet.test/", nil), target, "/api/v1/system", nil, &started)
			})
			require.Error(t, err)
			require.NotContains(t, err.Error(), "fixture-")
			require.False(t, started)
			require.Equal(t, int64(1), transport.calls.Load(), "failed login response must not lead to a control request")
			_, cached := h.lookupToken(target.cacheKey())
			require.False(t, cached)
			assertNoProxyAttempt(t, database, deviceID)
			epoch, err := h.writeService.Reserve(t.Context(), orgID, deviceID)
			require.NoError(t, err)
			drained, err := h.writeService.Drained(t.Context(), orgID, deviceID, epoch)
			require.NoError(t, err)
			require.True(t, drained)
		})
	}
}

type proxyWriteFailureConn struct {
	net.Conn
	failNext *atomic.Bool
}

func (c *proxyWriteFailureConn) Write(p []byte) (int, error) {
	if c.failNext.Swap(false) {
		return 0, errors.New("fixture-write-secret")
	}
	n, err := c.Conn.Write(p)
	if err != nil {
		return n, fmt.Errorf("proxy fixture connection write: %w", err)
	}
	return n, nil
}
func TestProxyTransportCannotReplayAcrossReservation(t *testing.T) {
	_, orgID, deviceID, h, target := proxyFenceFixture(t)
	var controls atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			controls.Add(1)
		}
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = w.Write([]byte("{}"))
	}))
	t.Cleanup(upstream.Close)
	h.httpClient = newProxyHTTPClient(false)
	transport, ok := h.httpClient.Transport.(*http.Transport)
	require.True(t, ok)
	t.Cleanup(transport.CloseIdleConnections)
	var dials atomic.Int64
	var failNext atomic.Bool
	redialStarted, redialRelease := make(chan struct{}), make(chan struct{})
	release := sync.OnceFunc(func() { close(redialRelease) })
	t.Cleanup(release)
	transport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		if dials.Add(1) == 2 {
			close(redialStarted)
			select {
			case <-redialRelease:
			case <-ctx.Done():
				return nil, fmt.Errorf("proxy fixture dial canceled: %w", ctx.Err())
			}
		}
		conn, err := (&net.Dialer{}).DialContext(ctx, network, upstream.Listener.Addr().String())
		if err != nil {
			return nil, fmt.Errorf("proxy fixture dial: %w", err)
		}
		return &proxyWriteFailureConn{Conn: conn, failNext: &failNext}, nil
	}
	warm := newDeadlineProxyRecorder()
	started := false
	require.NoError(t, h.writeService.WithHostAttempt(t.Context(), orgID, deviceID, target.binding.WriteEpoch, commandtype.Type(-1), func(ctx context.Context) error {
		return h.forwardAndCopy(ctx, warm, httptest.NewRequest(http.MethodGet, "http://fleet.test/", nil), target, "/api/v2/system/info", nil, &started)
	}))
	require.Equal(t, "{}", warm.Body.String())
	failNext.Store(true)
	done := make(chan error, 1)
	go func() {
		response := newDeadlineProxyRecorder()
		started := false
		done <- h.writeService.WithHostAttempt(t.Context(), orgID, deviceID, target.binding.WriteEpoch, commandtype.Type(-1), func(ctx context.Context) error {
			return h.forwardAndCopy(ctx, response, httptest.NewRequest(http.MethodPost, "http://fleet.test/", nil), target, "/api/v2/mining/start", []byte("{}"), &started)
		})
	}()
	awaitProxyBarrier(t, redialStarted)
	epoch, err := h.writeService.Reserve(t.Context(), orgID, deviceID)
	require.NoError(t, err)
	release()
	select {
	case err := <-done:
		require.Error(t, err)
		require.NotContains(t, err.Error(), "fixture-")
	case <-time.After(5 * time.Second):
		t.Fatal("proxy transport did not complete")
	}
	require.Zero(t, controls.Load(), "transparent replay must not send the buffered control request")
	require.Equal(t, int64(2), dials.Load())
	drained, err := h.writeService.Drained(t.Context(), orgID, deviceID, epoch)
	require.NoError(t, err)
	require.False(t, drained, "a dispatched transport failure has an unknown outcome")
}
func TestProxyTLSUsesOneFreshHTTP1ConnectionPerRequest(t *testing.T) {
	var mu sync.Mutex
	var peers []string
	var protocols []int
	upstream := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		peers = append(peers, r.RemoteAddr)
		protocols = append(protocols, r.ProtoMajor)
		mu.Unlock()
		_, _ = w.Write([]byte("{}"))
	}))
	upstream.EnableHTTP2 = true
	upstream.StartTLS()
	t.Cleanup(upstream.Close)
	client := newProxyHTTPClient(true)
	t.Cleanup(client.CloseIdleConnections)
	for range 2 {
		request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, upstream.URL, bytes.NewReader(nil))
		require.NoError(t, err)
		response, err := client.Do(request)
		require.NoError(t, err)
		body, readErr := io.ReadAll(response.Body)
		closeErr := response.Body.Close()
		require.NoError(t, readErr)
		require.NoError(t, closeErr)
		require.Equal(t, "{}", string(body))
	}
	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, []int{1, 1}, protocols)
	require.Len(t, peers, 2)
	require.NotEqual(t, peers[0], peers[1])
}

func authenticatedProxyRequest(t *testing.T, database *testutil.DatabaseService, h *Handler, target proxyTarget) *http.Request {
	t.Helper()
	var userID int64
	require.NoError(t, database.DB.QueryRowContext(t.Context(), "SELECT user_id FROM user_organization WHERE organization_id=$1 LIMIT 1", target.binding.OrgID).Scan(&userID))
	sess, err := h.sessionService.Create(t.Context(), userID, target.binding.OrgID, "proxy-fixture", "127.0.0.1")
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodGet, "http://fleet.test/miners/fixture/api/v2/system/info", nil)
	request.AddCookie(h.sessionService.CreateCookie(sess.SessionID))
	request.SetPathValue("deviceIdentifier", target.deviceIdentifier)
	request.SetPathValue("apiVersion", "v2")
	request.SetPathValue("rest", "system/info")
	return request
}
func proxyAttemptStatus(t *testing.T, database *testutil.DatabaseService, deviceID int64) string {
	t.Helper()
	var status string
	require.NoError(t, database.DB.QueryRowContext(t.Context(), "SELECT status FROM device_write_attempt WHERE device_id=$1 ORDER BY created_at DESC LIMIT 1", deviceID).Scan(&status))
	return status
}

func assertNoProxyAttempt(t *testing.T, database *testutil.DatabaseService, deviceID int64) {
	t.Helper()
	var count int
	require.NoError(t, database.DB.QueryRowContext(t.Context(), "SELECT count(*) FROM device_write_attempt WHERE device_id=$1", deviceID).Scan(&count))
	require.Zero(t, count, "a completed undispatched direct observation should retire its admission row")
}
func TestProxyServeHTTPResponseFailureDoesNotAppendError(t *testing.T) {
	for _, failure := range []string{"read", "close"} {
		t.Run(failure, func(t *testing.T) {
			database, orgID, deviceID, h, target := proxyFenceFixture(t)
			body := &proxyBodyBarrier{reader: strings.NewReader("{}")}
			if failure == "read" {
				body.reader = io.MultiReader(body.reader, proxyErrorReader{})
			} else {
				body.closeErr = errors.New("fixture-close-secret")
			}
			transport := &proxyResponseTransport{status: http.StatusOK, body: body}
			h.httpClient = &http.Client{Transport: transport}
			response := newDeadlineProxyRecorder()
			h.ServeHTTP(response, authenticatedProxyRequest(t, database, h, target))
			require.Equal(t, http.StatusOK, response.Code)
			require.Equal(t, "{}", response.Body.String(), "an error must not be appended after miner response headers")
			assertNoProxyAttempt(t, database, deviceID)
			epoch, err := h.writeService.Reserve(t.Context(), orgID, deviceID)
			require.NoError(t, err)
			drained, err := h.writeService.Drained(t.Context(), orgID, deviceID, epoch)
			require.NoError(t, err)
			require.True(t, drained)
		})
	}
}
func TestProxyStaleBindingWithoutEpochChangeSendsNoRequest(t *testing.T) {
	changes := map[string]string{
		"credentials":        "UPDATE miner_credentials SET password_enc='different', updated_at=updated_at+interval '1 second' WHERE device_id=$1",
		"endpoint":           "UPDATE discovered_device SET port='8081' WHERE id=(SELECT discovered_device_id FROM device WHERE id=$1)",
		"inactive-discovery": "UPDATE discovered_device SET is_active=false WHERE id=(SELECT discovered_device_id FROM device WHERE id=$1)",
		"deleted-discovery":  "UPDATE discovered_device SET deleted_at=now() WHERE id=(SELECT discovered_device_id FROM device WHERE id=$1)",
		"pairing":            "UPDATE device_pairing SET pairing_status='UNPAIRED' WHERE device_id=$1",
		"deleted-device":     "UPDATE device SET deleted_at=now() WHERE id=$1",
		"fleet-node":         "WITH node AS (INSERT INTO fleet_node(org_id,name,identity_pubkey,encryption_pubkey) SELECT org_id,'proxy-node','\\x01'::bytea,decode(repeat('01',32),'hex') FROM device WHERE id=$1 RETURNING id,org_id) INSERT INTO fleet_node_device(fleet_node_id,device_id,org_id) SELECT id,$1,org_id FROM node",
	}
	for name, change := range changes {
		for _, operation := range []string{"login", "raw", "mutation"} {
			t.Run(name+"/"+operation, func(t *testing.T) {
				database, orgID, deviceID, h, target := proxyFenceFixture(t)
				encrypted, err := h.encryptService.Encrypt([]byte("proxy-fixture-password"))
				require.NoError(t, err)
				require.NoError(t, sqlc.New(database.DB).UpsertMinerCredentials(t.Context(), sqlc.UpsertMinerCredentialsParams{DeviceID: deviceID, UsernameEnc: encrypted, PasswordEnc: encrypted}))
				target, err = h.resolveTarget(t.Context(), target.deviceIdentifier, orgID)
				require.NoError(t, err)
				transport := &countingProxyTransport{}
				h.httpClient = &http.Client{Transport: transport}
				forwardAfterBindingChange := func(ctx context.Context) error {
					// Change the captured binding only after durable admission, so
					// even device deletion exercises dispatch-time revalidation.
					require.Equal(t, "running", proxyAttemptStatus(t, database, deviceID))
					_, err := database.DB.ExecContext(ctx, change, deviceID)
					require.NoError(t, err)
					var epoch int64
					require.NoError(t, database.DB.QueryRowContext(ctx, "SELECT epoch FROM device_write_fence WHERE device_id=$1", deviceID).Scan(&epoch))
					require.Equal(t, target.binding.WriteEpoch, epoch)
					if operation == "login" {
						_, err := h.loginTarget(ctx, target, "proxy-fixture-password")
						return err
					}
					method, path := http.MethodGet, "/api/v2/system/info"
					if operation == "mutation" {
						method, path = http.MethodPost, "/api/v2/mining/start"
					}
					response, err := h.forward(ctx, httptest.NewRequest(method, "http://fleet.test/", nil), target, path, nil, false)
					if response != nil {
						require.NoError(t, response.Body.Close())
					}
					return err
				}
				if operation == "mutation" {
					err = h.writeService.WithHostAttempt(t.Context(), orgID, deviceID, target.binding.WriteEpoch, commandtype.StartMining, forwardAfterBindingChange)
				} else {
					err = h.writeService.WithHostObservation(t.Context(), orgID, deviceID, target.binding.WriteEpoch, forwardAfterBindingChange)
				}
				require.Error(t, err)
				require.True(t, fleeterror.IsFailedPreconditionError(err), "binding invalidation must fail closed: %v", err)
				require.Zero(t, transport.calls.Load(), "stale bindings must be rejected independently of write epochs")
			})
		}
	}
}
func TestProxyBindingChangeWaitsThroughLoginAndControlResponse(t *testing.T) {
	database, orgID, deviceID, h, target := proxyFenceFixture(t)
	encrypted, err := h.encryptService.Encrypt([]byte("proxy-fixture-password"))
	require.NoError(t, err)
	require.NoError(t, sqlc.New(database.DB).UpsertMinerCredentials(t.Context(), sqlc.UpsertMinerCredentialsParams{DeviceID: deviceID, UsernameEnc: encrypted, PasswordEnc: encrypted}))
	target, err = h.resolveTarget(t.Context(), target.deviceIdentifier, orgID)
	require.NoError(t, err)
	body := &proxyBodyBarrier{reader: strings.NewReader(`{"access_token":"fixture-login-token"}`), closeStarted: make(chan struct{}), closeRelease: make(chan struct{})}
	release := sync.OnceFunc(func() { close(body.closeRelease) })
	t.Cleanup(release)
	transport := &proxyResponseTransport{status: http.StatusOK, body: body}
	h.httpClient = &http.Client{Transport: transport}
	done := make(chan error, 1)
	go func() {
		started := false
		done <- h.writeService.WithHostAttempt(t.Context(), orgID, deviceID, target.binding.WriteEpoch, commandtype.Type(-1), func(ctx context.Context) error {
			return h.forwardAndCopy(ctx, newDeadlineProxyRecorder(), httptest.NewRequest(http.MethodPost, "http://fleet.test/", nil), target, "/api/v1/system", nil, &started)
		})
	}()
	awaitProxyBarrier(t, body.closeStarted)
	require.Equal(t, "running", proxyAttemptStatus(t, database, deviceID))
	updated := make(chan error, 1)
	go func() {
		_, updateErr := database.DB.ExecContext(t.Context(), "UPDATE miner_credentials SET updated_at=updated_at+interval '1 second' WHERE device_id=$1", deviceID)
		updated <- updateErr
	}()
	select {
	case updateErr := <-updated:
		require.NoError(t, updateErr)
		t.Fatal("credential change did not wait for the active login to close")
	case <-time.After(50 * time.Millisecond):
	}
	release()
	require.NoError(t, <-updated)
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("proxy did not finish after login close")
	}
	require.Equal(t, int64(2), transport.calls.Load(), "login and control use one locked binding until the response closes")
	require.Equal(t, "unknown", proxyAttemptStatus(t, database, deviceID))
	var epoch int64
	require.NoError(t, database.DB.QueryRowContext(t.Context(), "SELECT epoch FROM device_write_fence WHERE device_id=$1", deviceID).Scan(&epoch))
	require.Equal(t, target.binding.WriteEpoch, epoch)
}
