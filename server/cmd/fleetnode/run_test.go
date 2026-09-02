package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1/fleetnodegatewayv1connect"

	"github.com/block/proto-fleet/server/internal/fleetnode/bootstrap"
	"github.com/block/proto-fleet/server/internal/testutil"
)

type stubGatewayClient struct {
	mu               sync.Mutex
	calls            int
	authHeaders      []string
	responder        func(call int) error
	cancelAfterCalls int
	cancel           context.CancelFunc
}

type authRecordingControlGateway struct {
	controlFakeGateway
	authMu      sync.Mutex
	authHeaders []string
}

func requireOperatorActionExit(t *testing.T, err error) {
	t.Helper()
	var exitCoder interface{ ExitCode() int }
	require.ErrorAs(t, err, &exitCoder)
	assert.Equal(t, operatorActionExitCode, exitCoder.ExitCode())
}

func (f *authRecordingControlGateway) ControlStream(ctx context.Context, stream *connect.BidiStream[pb.ControlStreamRequest, pb.ControlStreamResponse]) error {
	f.authMu.Lock()
	f.authHeaders = append(f.authHeaders, stream.RequestHeader().Get("Authorization"))
	f.authMu.Unlock()
	return f.controlFakeGateway.ControlStream(ctx, stream)
}

func (f *authRecordingControlGateway) controlAuthHeaders() []string {
	f.authMu.Lock()
	defer f.authMu.Unlock()
	return append([]string(nil), f.authHeaders...)
}

func (s *stubGatewayClient) UploadHeartbeat(_ context.Context, req *connect.Request[pb.UploadHeartbeatRequest]) (*connect.Response[pb.UploadHeartbeatResponse], error) {
	s.mu.Lock()
	s.calls++
	s.authHeaders = append(s.authHeaders, req.Header().Get("Authorization"))
	call := s.calls
	cancelAt := s.cancelAfterCalls
	cancel := s.cancel
	resp := s.responder
	s.mu.Unlock()

	var err error
	if resp != nil {
		err = resp(call)
	}
	if cancelAt > 0 && call >= cancelAt && cancel != nil {
		cancel()
	}
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.UploadHeartbeatResponse{}), nil
}

func (s *stubGatewayClient) ReportDiscoveredDevices(_ context.Context, _ *connect.Request[pb.ReportDiscoveredDevicesRequest]) (*connect.Response[pb.ReportDiscoveredDevicesResponse], error) {
	return connect.NewResponse(&pb.ReportDiscoveredDevicesResponse{}), nil
}

func (s *stubGatewayClient) ReportPairedDevices(_ context.Context, _ *connect.Request[pb.ReportPairedDevicesRequest]) (*connect.Response[pb.ReportPairedDevicesResponse], error) {
	return connect.NewResponse(&pb.ReportPairedDevicesResponse{}), nil
}

func (s *stubGatewayClient) UploadCommandArtifact(_ context.Context) *connect.ClientStreamForClient[pb.UploadCommandArtifactRequest, pb.UploadCommandArtifactResponse] {
	return nil
}

func (s *stubGatewayClient) DownloadCommandArtifact(_ context.Context, _ *connect.Request[pb.DownloadCommandArtifactRequest]) (*connect.ServerStreamForClient[pb.DownloadCommandArtifactResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, errors.New("not implemented"))
}

func (s *stubGatewayClient) ControlStream(_ context.Context) *connect.BidiStreamForClient[pb.ControlStreamRequest, pb.ControlStreamResponse] {
	return nil
}

func (s *stubGatewayClient) snapshot() (int, []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.authHeaders))
	copy(out, s.authHeaders)
	return s.calls, out
}

func freshState(t *testing.T, dir string, sessionExpiresAt time.Time) *bootstrap.State {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	st := &bootstrap.State{
		ServerURL:              "http://127.0.0.1:0",
		AllowInsecureTransport: true,
		FleetNodeID:            42,
		IdentityFingerprint:    "0011223344556677",
		IdentityPrivateKeyHex:  hex.EncodeToString(priv),
		IdentityPublicKeyHex:   hex.EncodeToString(pub),
		APIKey:                 "fleet_known_key",
		SessionToken:           "session-1",
		SessionExpiresAt:       sessionExpiresAt,
	}
	require.NoError(t, bootstrap.SaveState(bootstrap.StatePath(dir), st))
	return st
}

func TestRunCmd_HappyPathThreeTicks(t *testing.T) {
	t.Parallel()

	// Arrange
	dir := t.TempDir()
	freshState(t, dir, time.Now().Add(24*time.Hour))

	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	stub := &stubGatewayClient{cancelAfterCalls: 3, cancel: cancel}

	cmd := &RunCmd{
		HeartbeatInterval: 5 * time.Millisecond,
		parentCtx:         parent,
		clientFactory:     func(_ string, _ func() string) (gatewayClient, error) { return stub, nil },
	}

	done := make(chan error, 1)
	go func() { done <- cmd.run(&Context{StateDir: dir}, &bytes.Buffer{}) }()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("daemon did not shut down within 2s after the stub cancelled parent ctx")
	}

	// Assert
	calls, _ := stub.snapshot()
	assert.GreaterOrEqual(t, calls, 3, "daemon must send at least 3 heartbeats before shutdown")
}

func TestRunCmd_NotifiesReadyAfterFirstHeartbeatAttempt(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	freshState(t, dir, time.Now().Add(24*time.Hour))

	parent, cancel := context.WithCancel(context.Background())
	stub := &stubGatewayClient{}
	notified := false
	cmd := &RunCmd{
		parentCtx:     parent,
		clientFactory: func(_ string, _ func() string) (gatewayClient, error) { return stub, nil },
		notifyReady: func() error {
			notified = true
			cancel()
			return nil
		},
	}

	require.NoError(t, cmd.run(&Context{StateDir: dir}, &bytes.Buffer{}))
	assert.True(t, notified)
	calls, _ := stub.snapshot()
	assert.Equal(t, 1, calls, "readiness must follow the first heartbeat attempt")
}

func TestRunCmd_TransientInitialRefreshFailureStillBecomesReady(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	freshState(t, dir, time.Now().Add(30*time.Minute))

	parent, cancel := context.WithCancel(context.Background())
	notified := false
	cmd := &RunCmd{
		parentCtx: parent,
		notifyReady: func() error {
			notified = true
			cancel()
			return nil
		},
	}

	require.NoError(t, cmd.run(&Context{StateDir: dir}, &bytes.Buffer{}))
	assert.True(t, notified, "Fleet Server unavailability must not block local readiness")
}

func TestIsRetryableRefreshError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		err       error
		retryable bool
	}{
		{name: "context canceled", err: context.Canceled, retryable: true},
		{name: "context deadline exceeded", err: context.DeadlineExceeded, retryable: true},
		{name: "connect canceled", err: connect.NewError(connect.CodeCanceled, errors.New("canceled")), retryable: true},
		{name: "connect deadline exceeded", err: connect.NewError(connect.CodeDeadlineExceeded, errors.New("deadline exceeded")), retryable: true},
		{name: "connect resource exhausted", err: connect.NewError(connect.CodeResourceExhausted, errors.New("resource exhausted")), retryable: true},
		{name: "connect aborted", err: connect.NewError(connect.CodeAborted, errors.New("aborted")), retryable: true},
		{name: "connect internal", err: connect.NewError(connect.CodeInternal, errors.New("internal")), retryable: true},
		{name: "wrapped connect unavailable", err: fmt.Errorf("refresh: %w", connect.NewError(connect.CodeUnavailable, errors.New("unavailable"))), retryable: true},
		{name: "connect unauthenticated", err: connect.NewError(connect.CodeUnauthenticated, errors.New("unauthenticated")), retryable: false},
		{name: "connect invalid argument", err: connect.NewError(connect.CodeInvalidArgument, errors.New("invalid argument")), retryable: false},
		{name: "plain error", err: errors.New("local failure"), retryable: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.retryable, isRetryableRefreshError(tt.err))
		})
	}
}

func TestRunCmd_LocalRefreshFailureRequiresOperatorAction(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	state := freshState(t, dir, time.Now().Add(30*time.Minute))
	state.IdentityPrivateKeyHex = "not-hex"
	require.NoError(t, bootstrap.SaveState(bootstrap.StatePath(dir), state))

	notified := false
	parent, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()
	cmd := &RunCmd{parentCtx: parent, notifyReady: func() error {
		notified = true
		return nil
	}}

	err := cmd.run(&Context{StateDir: dir}, &bytes.Buffer{})

	require.Error(t, err)
	requireOperatorActionExit(t, err)
	assert.Contains(t, err.Error(), "decode identity private key")
	assert.False(t, notified, "unrecoverable local state must not report ready")
}

func TestRunCmd_StateSaveFailureRequiresOperatorAction(t *testing.T) {
	t.Parallel()

	pubKey, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	fake := &fakeFleetNodeGateway{
		expectedAPIKey:   "fleet_known_key",
		identityPub:      pubKey,
		challenge:        bytes.Repeat([]byte{0x35}, 32),
		sessionToken:     "session-2",
		sessionExpiresAt: time.Now().Add(24 * time.Hour),
	}
	srv := newFakeServer(t, fake)
	state := &bootstrap.State{
		ServerURL:              srv.URL,
		AllowInsecureTransport: true,
		FleetNodeID:            42,
		IdentityPrivateKeyHex:  hex.EncodeToString(priv),
		IdentityPublicKeyHex:   hex.EncodeToString(pubKey),
		APIKey:                 "fleet_known_key",
		SessionToken:           "session-1",
		SessionExpiresAt:       time.Now().Add(30 * time.Minute),
	}
	blockedParent := filepath.Join(t.TempDir(), "not-a-directory")
	require.NoError(t, os.WriteFile(blockedParent, []byte("blocked"), 0o600))
	statePath := filepath.Join(blockedParent, "state.yaml")
	cmd := &RunCmd{now: func() time.Time { return time.Now().UTC() }}

	err = cmd.tick(t.Context(), &stubGatewayClient{}, state, statePath, discardLogger(t))

	require.Error(t, err)
	requireOperatorActionExit(t, err)
	assert.Contains(t, err.Error(), "save state after refresh")
}

func TestRunLocked_PluginBootstrapFailureRemainsRetryable(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	freshState(t, stateDir, time.Now().Add(24*time.Hour))
	pluginsDir := t.TempDir()
	badPlugin := filepath.Join(pluginsDir, "bad-plugin")
	require.NoError(t, os.WriteFile(badPlugin, []byte("#!/usr/bin/env bash\nexit 1\n"), 0o755))
	cmd := &RunCmd{
		clientFactory: func(_ string, _ func() string) (gatewayClient, error) {
			return &stubGatewayClient{}, nil
		},
	}

	err := cmd.runLocked(t.Context(), &Context{StateDir: stateDir}, pluginsDir, discardLogger(t))

	require.Error(t, err)
	var exitCoder interface{ ExitCode() int }
	assert.False(t, errors.As(err, &exitCoder), "transient plugin startup failures must remain restartable")
}

func TestRunCmd_StateLockErrorClassification(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not use the filesystem state lock")
	}

	t.Run("lock preparation error requires operator action", func(t *testing.T) {
		realStateDir := t.TempDir()
		freshState(t, realStateDir, time.Now().Add(24*time.Hour))
		stateDir := filepath.Join(t.TempDir(), "state")
		require.NoError(t, os.Symlink(realStateDir, stateDir))

		err := (&RunCmd{}).run(&Context{StateDir: stateDir}, &bytes.Buffer{})

		require.Error(t, err)
		requireOperatorActionExit(t, err)
		assert.Contains(t, err.Error(), "symlink")
	})

	t.Run("callback error keeps its original classification", func(t *testing.T) {
		stateDir := t.TempDir()
		freshState(t, stateDir, time.Now().Add(24*time.Hour))
		notifyErr := errors.New("notify failed")
		cmd := &RunCmd{notifyReady: func() error { return notifyErr }}

		err := cmd.run(&Context{StateDir: stateDir}, &bytes.Buffer{})

		require.ErrorIs(t, err, notifyErr)
		var exitCoder interface{ ExitCode() int }
		assert.False(t, errors.As(err, &exitCoder), "callback errors must not inherit lock-error classification")
	})
}

func TestRunCmd_ControlWorkerShutdownWaitIsBounded(t *testing.T) {
	require.Equal(t, 10*time.Second, controlWorkerShutdownTimeout)
	previousTimeout := controlWorkerShutdownTimeout
	controlWorkerShutdownTimeout = 50 * time.Millisecond
	t.Cleanup(func() { controlWorkerShutdownTimeout = previousTimeout })

	cmd := &RunCmd{}
	cmd.initControlConcurrency()
	cmd.controlWorkers.Add(1)
	t.Cleanup(cmd.controlWorkers.Done)

	started := time.Now()
	cmd.waitForControlWorkers(discardLogger(t))
	elapsed := time.Since(started)

	assert.GreaterOrEqual(t, elapsed, controlWorkerShutdownTimeout)
	assert.Less(t, elapsed, 500*time.Millisecond, "daemon shutdown must continue after its worker budget")
}

func TestRunCmd_RefreshesNearExpirySession(t *testing.T) {
	t.Parallel()

	// Arrange
	dir := t.TempDir()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	fake := &fakeFleetNodeGateway{
		expectedAPIKey:   "fleet_known_key",
		identityPub:      pub,
		challenge:        bytes.Repeat([]byte{0x33}, 32),
		sessionToken:     "session-rotated",
		sessionExpiresAt: time.Now().Add(24 * time.Hour).UTC().Truncate(time.Second),
	}
	srv := newFakeServer(t, fake)

	pubKey, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	fake.identityPub = pubKey
	st := &bootstrap.State{
		ServerURL:              srv.URL,
		AllowInsecureTransport: true,
		FleetNodeID:            42,
		IdentityFingerprint:    "0011223344556677",
		IdentityPrivateKeyHex:  hex.EncodeToString(priv),
		IdentityPublicKeyHex:   hex.EncodeToString(pubKey),
		APIKey:                 "fleet_known_key",
		SessionToken:           "session-stale",
		SessionExpiresAt:       time.Now().Add(30 * time.Minute),
	}
	require.NoError(t, bootstrap.SaveState(bootstrap.StatePath(dir), st))

	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	fake.onHeartbeat = func(int) { cancel() }

	cmd := &RunCmd{HeartbeatInterval: 5 * time.Millisecond, parentCtx: parent}

	done := make(chan error, 1)
	go func() { done <- cmd.run(&Context{StateDir: dir}, &bytes.Buffer{}) }()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("daemon did not shut down within 3s of the first heartbeat")
	}

	// Assert
	loaded, _, err := bootstrap.LoadState(bootstrap.StatePath(dir))
	require.NoError(t, err)
	assert.Equal(t, "session-rotated", loaded.SessionToken, "near-expiry session must be refreshed before first heartbeat")
	assert.Equal(t, 1, fake.heartbeatCount(), "exactly one heartbeat before shutdown")
}

func TestRunCmd_SuccessfulRefreshRetiresActiveControlSession(t *testing.T) {
	dir := t.TempDir()
	pubKey, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	fakeAuth := &fakeFleetNodeGateway{
		expectedAPIKey:   "fleet_known_key",
		identityPub:      pubKey,
		challenge:        bytes.Repeat([]byte{0x34}, 32),
		sessionToken:     "session-2",
		sessionExpiresAt: time.Now().Add(24 * time.Hour),
	}
	authServer := newFakeServer(t, fakeAuth)
	state := &bootstrap.State{
		ServerURL:              authServer.URL,
		AllowInsecureTransport: true,
		FleetNodeID:            42,
		IdentityPrivateKeyHex:  hex.EncodeToString(priv),
		IdentityPublicKeyHex:   hex.EncodeToString(pubKey),
		APIKey:                 "fleet_known_key",
		SessionToken:           "session-1",
		SessionExpiresAt:       time.Now().Add(12 * time.Hour),
	}
	statePath := bootstrap.StatePath(dir)
	require.NoError(t, bootstrap.SaveState(statePath, state))

	controlGateway := &authRecordingControlGateway{}
	mux := http.NewServeMux()
	path, handler := fleetnodegatewayv1connect.NewFleetNodeGatewayServiceHandler(controlGateway)
	mux.Handle(path, handler)
	controlServer := testutil.NewH2CServer(t, mux)
	cmd := &RunCmd{}
	client, err := bootstrap.NewAuthenticatedGatewayClient(controlServer.URL, func() string {
		cmd.stateMu.Lock()
		defer cmd.stateMu.Unlock()
		return state.SessionToken
	})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- cmd.runControlLoop(ctx, client, state, discardLogger(t))
	}()
	require.Eventually(t, func() bool {
		headers := controlGateway.controlAuthHeaders()
		return len(headers) == 1 && headers[0] == "Bearer session-1"
	}, time.Second, 10*time.Millisecond)

	require.NoError(t, cmd.refreshAndSave(ctx, state, statePath, discardLogger(t)))
	require.Eventually(t, func() bool {
		headers := controlGateway.controlAuthHeaders()
		return len(headers) == 2 && headers[0] == "Bearer session-1" && headers[1] == "Bearer session-2"
	}, 2*time.Second, 10*time.Millisecond)
	cancel()
	require.NoError(t, <-done)
	assert.Equal(t, "session-2", state.SessionToken)
}

func TestRunCmd_RefreshFailureCannotExtendControlStreamPastExpiry(t *testing.T) {
	dir := t.TempDir()
	pubKey, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	fakeAuth := &fakeFleetNodeGateway{
		expectedAPIKey: "fleet_known_key",
		identityPub:    pubKey,
		beginAuthError: connect.NewError(connect.CodeUnavailable, errors.New("refresh unavailable")),
	}
	authServer := newFakeServer(t, fakeAuth)
	expiresAt := time.Now().Add(2 * time.Second)
	state := &bootstrap.State{
		ServerURL:              authServer.URL,
		AllowInsecureTransport: true,
		FleetNodeID:            42,
		IdentityPrivateKeyHex:  hex.EncodeToString(priv),
		IdentityPublicKeyHex:   hex.EncodeToString(pubKey),
		APIKey:                 "fleet_known_key",
		SessionToken:           "session-1",
		SessionExpiresAt:       expiresAt,
	}
	statePath := bootstrap.StatePath(dir)
	require.NoError(t, bootstrap.SaveState(statePath, state))

	controlGateway := &controlFakeGateway{}
	client := newControlClient(t, controlGateway)
	cmd := &RunCmd{}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- cmd.runControlSession(ctx, discardLogger(t), client, state)
	}()
	require.Eventually(t, func() bool { return controlGateway.helloCount() == 1 }, time.Second, 10*time.Millisecond)

	require.Error(t, cmd.refreshAndSave(ctx, state, statePath, discardLogger(t)))
	select {
	case sessionErr := <-done:
		t.Fatalf("failed refresh retired the stream before token expiry: %v", sessionErr)
	case <-time.After(100 * time.Millisecond):
	}
	select {
	case sessionErr := <-done:
		require.ErrorIs(t, sessionErr, errControlSessionExpired)
	case <-time.After(time.Until(expiresAt) + time.Second):
		t.Fatal("control session survived past the opening token expiry")
	}
	assert.Equal(t, "session-1", state.SessionToken)
}

func TestRunCmd_RefreshesOnUnauthenticatedResponse(t *testing.T) {
	t.Parallel()

	// Arrange
	dir := t.TempDir()
	pubKey, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	fake := &fakeFleetNodeGateway{
		expectedAPIKey:       "fleet_known_key",
		identityPub:          pubKey,
		challenge:            bytes.Repeat([]byte{0x44}, 32),
		sessionToken:         "session-2",
		sessionExpiresAt:     time.Now().Add(24 * time.Hour).UTC().Truncate(time.Second),
		expectedSessionToken: "session-2",
	}
	srv := newFakeServer(t, fake)
	require.NoError(t, bootstrap.SaveState(bootstrap.StatePath(dir), &bootstrap.State{
		ServerURL:              srv.URL,
		AllowInsecureTransport: true,
		FleetNodeID:            42,
		IdentityFingerprint:    "0011223344556677",
		IdentityPrivateKeyHex:  hex.EncodeToString(priv),
		IdentityPublicKeyHex:   hex.EncodeToString(pubKey),
		APIKey:                 "fleet_known_key",
		SessionToken:           "session-1",
		SessionExpiresAt:       time.Now().Add(24 * time.Hour),
	}))

	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	successfulHeartbeats := atomic.Int64{}
	fake.onHeartbeat = func(count int) {
		successfulHeartbeats.Store(int64(count))
		if count >= 3 {
			cancel()
		}
	}

	cmd := &RunCmd{HeartbeatInterval: 5 * time.Millisecond, parentCtx: parent}

	done := make(chan error, 1)
	go func() { done <- cmd.run(&Context{StateDir: dir}, &bytes.Buffer{}) }()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatalf("daemon did not recover and reach 3 successful heartbeats; got %d", successfulHeartbeats.Load())
	}

	// Assert
	loaded, _, _ := bootstrap.LoadState(bootstrap.StatePath(dir))
	assert.Equal(t, "session-2", loaded.SessionToken, "Unauthenticated rejection must trigger a refresh that persists the new token")
}

func TestRunCmd_FailsWhenStateIsMissing(t *testing.T) {
	t.Parallel()

	// Arrange
	parent := t.TempDir()
	stateDir := filepath.Join(parent, "never-existed")
	cmd := &RunCmd{HeartbeatInterval: time.Second}
	var stderr bytes.Buffer

	// Act
	err := cmd.run(&Context{StateDir: stateDir}, &stderr)

	// Assert
	require.Error(t, err)
	requireOperatorActionExit(t, err)
	assert.Contains(t, err.Error(), "fleetnode enroll")
	_, statErr := os.Stat(stateDir)
	assert.True(t, os.IsNotExist(statErr), "state dir must not be created when run bails out on missing state")
}

func TestRunCmd_FailsWhenApiKeyIsMissing(t *testing.T) {
	t.Parallel()

	// Arrange
	dir := t.TempDir()
	pubKey, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	require.NoError(t, bootstrap.SaveState(bootstrap.StatePath(dir), &bootstrap.State{
		ServerURL:              "http://127.0.0.1:1",
		AllowInsecureTransport: true,
		FleetNodeID:            42,
		IdentityFingerprint:    "0011223344556677",
		IdentityPrivateKeyHex:  hex.EncodeToString(priv),
		IdentityPublicKeyHex:   hex.EncodeToString(pubKey),
	}))
	cmd := &RunCmd{HeartbeatInterval: time.Second}

	// Act
	err = cmd.run(&Context{StateDir: dir}, &bytes.Buffer{})

	// Assert
	require.Error(t, err)
	requireOperatorActionExit(t, err)
	assert.Contains(t, err.Error(), "fleetnode refresh")
}

func TestRunCmd_BailsOutWhenInitialRefreshHitsBeginAuthRejected(t *testing.T) {
	t.Parallel()

	// Arrange
	dir := t.TempDir()
	pubKey, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	fake := &fakeFleetNodeGateway{
		expectedAPIKey:   "the-real-key",
		identityPub:      pubKey,
		challenge:        bytes.Repeat([]byte{0x55}, 32),
		sessionToken:     "irrelevant",
		sessionExpiresAt: time.Now().Add(24 * time.Hour),
	}
	srv := newFakeServer(t, fake)
	require.NoError(t, bootstrap.SaveState(bootstrap.StatePath(dir), &bootstrap.State{
		ServerURL:              srv.URL,
		AllowInsecureTransport: true,
		FleetNodeID:            42,
		IdentityFingerprint:    "0011223344556677",
		IdentityPrivateKeyHex:  hex.EncodeToString(priv),
		IdentityPublicKeyHex:   hex.EncodeToString(pubKey),
		APIKey:                 "wrong-key",
		SessionToken:           "",
	}))
	cmd := &RunCmd{HeartbeatInterval: time.Second}

	// Act
	err = cmd.run(&Context{StateDir: dir}, &bytes.Buffer{})

	// Assert
	require.Error(t, err)
	requireOperatorActionExit(t, err)
	assert.ErrorIs(t, err, bootstrap.ErrBeginAuthRejected)
	assert.Contains(t, err.Error(), "local credentials are preserved")
}

func TestRunCmd_ValidatesServerURLBeforeBuildingClient(t *testing.T) {
	t.Parallel()

	// Arrange: state has a fresh session_token but an http:// non-loopback
	// server_url and AllowInsecureTransport=false. The daemon must refuse
	// to start before any heartbeat would leak the bearer to plaintext.
	dir := t.TempDir()
	pubKey, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	require.NoError(t, bootstrap.SaveState(bootstrap.StatePath(dir), &bootstrap.State{
		ServerURL:              "http://fleet.example.com",
		AllowInsecureTransport: false,
		FleetNodeID:            42,
		IdentityFingerprint:    "0011223344556677",
		IdentityPrivateKeyHex:  hex.EncodeToString(priv),
		IdentityPublicKeyHex:   hex.EncodeToString(pubKey),
		APIKey:                 "fleet_known_key",
		SessionToken:           "session-still-fresh",
		SessionExpiresAt:       time.Now().Add(24 * time.Hour),
	}))
	stub := &stubGatewayClient{}
	cmd := &RunCmd{
		HeartbeatInterval: time.Second,
		clientFactory:     func(_ string, _ func() string) (gatewayClient, error) { return stub, nil },
	}

	// Act
	err = cmd.run(&Context{StateDir: dir}, &bytes.Buffer{})

	// Assert
	require.Error(t, err)
	assert.Contains(t, err.Error(), "https")
	calls, _ := stub.snapshot()
	assert.Equal(t, 0, calls, "no heartbeat must be sent when server URL fails validation")
}

func TestRunCmd_ExitsOnCodeNotFoundHeartbeat(t *testing.T) {
	t.Parallel()

	// Arrange
	dir := t.TempDir()
	freshState(t, dir, time.Now().Add(24*time.Hour))
	stub := &stubGatewayClient{
		responder: func(int) error {
			return connect.NewError(connect.CodeNotFound, errors.New("fleet node not found"))
		},
	}
	cmd := &RunCmd{
		HeartbeatInterval: 5 * time.Millisecond,
		clientFactory:     func(_ string, _ func() string) (gatewayClient, error) { return stub, nil },
	}

	// Act
	done := make(chan error, 1)
	go func() { done <- cmd.run(&Context{StateDir: dir}, &bytes.Buffer{}) }()

	// Assert
	select {
	case err := <-done:
		require.Error(t, err)
		requireOperatorActionExit(t, err)
		assert.Equal(t, connect.CodeNotFound, connect.CodeOf(err))
		assert.Contains(t, err.Error(), "re-enroll")
	case <-time.After(2 * time.Second):
		t.Fatal("daemon did not exit within 2s after server returned CodeNotFound")
	}
}

func TestRunCmd_ExitsWhenTickRefreshHitsBeginAuthRejected(t *testing.T) {
	t.Parallel()

	// Arrange: heartbeat returns Unauthenticated which forces a tick
	// refresh; the fake's expectedAPIKey is wrong, so the refresh
	// BeginAuthHandshake also returns Unauthenticated -> ErrBeginAuthRejected.
	// The daemon must exit instead of looping forever.
	dir := t.TempDir()
	pubKey, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	fake := &fakeFleetNodeGateway{
		expectedAPIKey:       "the-key-that-was-revoked",
		identityPub:          pubKey,
		challenge:            bytes.Repeat([]byte{0x99}, 32),
		sessionToken:         "never-issued",
		sessionExpiresAt:     time.Now().Add(24 * time.Hour),
		expectedSessionToken: "different-from-state",
	}
	srv := newFakeServer(t, fake)
	require.NoError(t, bootstrap.SaveState(bootstrap.StatePath(dir), &bootstrap.State{
		ServerURL:              srv.URL,
		AllowInsecureTransport: true,
		FleetNodeID:            42,
		IdentityFingerprint:    "0011223344556677",
		IdentityPrivateKeyHex:  hex.EncodeToString(priv),
		IdentityPublicKeyHex:   hex.EncodeToString(pubKey),
		APIKey:                 "fleet_known_key",
		SessionToken:           "stale-session",
		SessionExpiresAt:       time.Now().Add(24 * time.Hour),
	}))
	cmd := &RunCmd{HeartbeatInterval: 5 * time.Millisecond}

	// Act
	done := make(chan error, 1)
	go func() { done <- cmd.run(&Context{StateDir: dir}, &bytes.Buffer{}) }()

	// Assert
	select {
	case err := <-done:
		require.Error(t, err)
		requireOperatorActionExit(t, err)
		assert.ErrorIs(t, err, bootstrap.ErrBeginAuthRejected)
		assert.Contains(t, err.Error(), "Exiting")
	case <-time.After(3 * time.Second):
		t.Fatal("daemon did not exit within 3s after tick refresh hit ErrBeginAuthRejected")
	}
}
