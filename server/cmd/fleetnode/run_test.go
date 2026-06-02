package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"

	"github.com/block/proto-fleet/server/internal/fleetnode/bootstrap"
)

type stubGatewayClient struct {
	mu               sync.Mutex
	calls            int
	authHeaders      []string
	responder        func(call int) error
	cancelAfterCalls int
	cancel           context.CancelFunc
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
		assert.ErrorIs(t, err, bootstrap.ErrBeginAuthRejected)
		assert.Contains(t, err.Error(), "Exiting")
	case <-time.After(3 * time.Second):
		t.Fatal("daemon did not exit within 3s after tick refresh hit ErrBeginAuthRejected")
	}
}
