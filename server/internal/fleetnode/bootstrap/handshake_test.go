package bootstrap

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1/fleetnodegatewayv1connect"
	"github.com/block/proto-fleet/server/internal/testutil"
)

type fakeAgentGateway struct {
	fleetnodegatewayv1connect.UnimplementedFleetNodeGatewayServiceHandler

	expectedCode     string
	expectedAPIKey   string
	agentID          int64
	identityPub      ed25519.PublicKey
	challenge        []byte
	sessionToken     string
	sessionExpiresAt time.Time
	registerError    error

	registered        bool
	signatureVerified bool
}

func (f *fakeAgentGateway) Register(_ context.Context, req *connect.Request[pb.RegisterRequest]) (*connect.Response[pb.RegisterResponse], error) {
	if f.registerError != nil {
		return nil, f.registerError
	}
	if f.expectedCode != "" && req.Msg.GetEnrollmentToken() != f.expectedCode {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("invalid enrollment code"))
	}
	f.identityPub = ed25519.PublicKey(req.Msg.GetIdentityPubkey())
	f.registered = true
	return connect.NewResponse(&pb.RegisterResponse{
		FleetNodeId:         f.agentID,
		EnrollmentStatus:    pb.EnrollmentStatus_ENROLLMENT_STATUS_PENDING,
		IdentityFingerprint: IdentityFingerprint(req.Msg.GetIdentityPubkey()),
	}), nil
}

func (f *fakeAgentGateway) BeginAuthHandshake(_ context.Context, req *connect.Request[pb.BeginAuthHandshakeRequest]) (*connect.Response[pb.BeginAuthHandshakeResponse], error) {
	if req.Msg.GetApiKey() != f.expectedAPIKey {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("invalid api_key"))
	}
	if !bytes.Equal(req.Msg.GetIdentityPubkey(), f.identityPub) {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("identity_pubkey mismatch"))
	}
	return connect.NewResponse(&pb.BeginAuthHandshakeResponse{
		Challenge: f.challenge,
		ExpiresAt: timestamppb.New(time.Now().Add(30 * time.Second)),
	}), nil
}

func (f *fakeAgentGateway) CompleteAuthHandshake(_ context.Context, req *connect.Request[pb.CompleteAuthHandshakeRequest]) (*connect.Response[pb.CompleteAuthHandshakeResponse], error) {
	if !ed25519.Verify(f.identityPub, req.Msg.GetChallenge(), req.Msg.GetSignature()) {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("bad signature"))
	}
	f.signatureVerified = true
	return connect.NewResponse(&pb.CompleteAuthHandshakeResponse{
		SessionToken: f.sessionToken,
		ExpiresAt:    timestamppb.New(f.sessionExpiresAt),
	}), nil
}

func newFakeServer(t *testing.T, fake *fakeAgentGateway) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	path, h := fleetnodegatewayv1connect.NewFleetNodeGatewayServiceHandler(fake)
	mux.Handle(path, h)
	return testutil.NewH2CServer(t, mux)
}

func TestRunHandshake_RejectsNilState(t *testing.T) {
	t.Parallel()

	// Act
	err := RunHandshake(t.Context(), nil, nil)

	// Assert
	require.Error(t, err)
	assert.Contains(t, err.Error(), "state is required")
}

func TestRunHandshake_RejectsNilClient(t *testing.T) {
	t.Parallel()

	// Arrange
	pub, priv, err := GenerateKeypair()
	require.NoError(t, err)
	state := &State{
		APIKey:                "k",
		IdentityPrivateKeyHex: hex.EncodeToString(priv),
		IdentityPublicKeyHex:  hex.EncodeToString(pub),
	}

	// Act
	err = RunHandshake(t.Context(), nil, state)

	// Assert
	require.Error(t, err)
	assert.Contains(t, err.Error(), "client is required")
}

func TestRunHandshake_HappyPath(t *testing.T) {
	t.Parallel()

	// Arrange
	pub, priv, err := GenerateKeypair()
	require.NoError(t, err)
	expiresAt := time.Now().Add(24 * time.Hour).UTC().Truncate(time.Second)
	fake := &fakeAgentGateway{
		expectedAPIKey:   "fleet_aabbccdd_zzz",
		identityPub:      pub,
		challenge:        bytes.Repeat([]byte{0x42}, 32),
		sessionToken:     "session-token-abc",
		sessionExpiresAt: expiresAt,
	}
	srv := newFakeServer(t, fake)
	state := &State{
		APIKey:                fake.expectedAPIKey,
		IdentityPrivateKeyHex: hex.EncodeToString(priv),
		IdentityPublicKeyHex:  hex.EncodeToString(pub),
	}
	client, err := NewGatewayClient(srv.URL)
	require.NoError(t, err)

	// Act
	err = RunHandshake(t.Context(), client, state)

	// Assert
	require.NoError(t, err)
	assert.Equal(t, "session-token-abc", state.SessionToken)
	assert.WithinDuration(t, expiresAt, state.SessionExpiresAt, time.Second)
	assert.True(t, fake.signatureVerified)
}

func TestRunHandshake_WrongAPIKey(t *testing.T) {
	t.Parallel()

	// Arrange
	pub, priv, err := GenerateKeypair()
	require.NoError(t, err)
	fake := &fakeAgentGateway{
		expectedAPIKey: "right-key",
		identityPub:    pub,
		challenge:      bytes.Repeat([]byte{0x01}, 32),
	}
	srv := newFakeServer(t, fake)
	state := &State{
		APIKey:                "wrong-key",
		IdentityPrivateKeyHex: hex.EncodeToString(priv),
		IdentityPublicKeyHex:  hex.EncodeToString(pub),
	}
	client, err := NewGatewayClient(srv.URL)
	require.NoError(t, err)

	// Act
	err = RunHandshake(t.Context(), client, state)

	// Assert
	require.Error(t, err)
	require.ErrorIs(t, err, ErrBeginAuthRejected)
	var connErr *connect.Error
	require.ErrorAs(t, err, &connErr)
	assert.Equal(t, connect.CodeUnauthenticated, connErr.Code())
	assert.Empty(t, state.SessionToken)
}

func TestRunHandshake_MalformedKeys(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		privHex string
		pubHex  string
		wantErr string
	}{
		{
			name:    "non-hex private key",
			privHex: "not-hex",
			pubHex:  hex.EncodeToString(make([]byte, ed25519.PublicKeySize)),
			wantErr: "decode identity private key",
		},
		{
			name:    "non-hex public key",
			privHex: hex.EncodeToString(make([]byte, ed25519.PrivateKeySize)),
			pubHex:  "not-hex",
			wantErr: "decode identity public key",
		},
		{
			name:    "wrong-length private key",
			privHex: hex.EncodeToString(make([]byte, 8)),
			pubHex:  hex.EncodeToString(make([]byte, ed25519.PublicKeySize)),
			wantErr: "private key has wrong length",
		},
		{
			name:    "wrong-length public key",
			privHex: hex.EncodeToString(make([]byte, ed25519.PrivateKeySize)),
			pubHex:  hex.EncodeToString(make([]byte, 8)),
			wantErr: "public key has wrong length",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// Arrange
			state := &State{IdentityPrivateKeyHex: tc.privHex, IdentityPublicKeyHex: tc.pubHex}

			// Act
			err := RunHandshake(t.Context(), nil, state)

			// Assert
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestRunHandshake_BadSignature(t *testing.T) {
	t.Parallel()

	// Arrange
	pub, _, err := GenerateKeypair()
	require.NoError(t, err)
	_, otherPriv, err := GenerateKeypair()
	require.NoError(t, err)
	fake := &fakeAgentGateway{
		expectedAPIKey: "k",
		identityPub:    pub,
		challenge:      bytes.Repeat([]byte{0x09}, 32),
	}
	srv := newFakeServer(t, fake)
	state := &State{
		APIKey:                "k",
		IdentityPrivateKeyHex: hex.EncodeToString(otherPriv),
		IdentityPublicKeyHex:  hex.EncodeToString(pub),
	}
	client, err := NewGatewayClient(srv.URL)
	require.NoError(t, err)

	// Act
	err = RunHandshake(t.Context(), client, state)

	// Assert
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrBeginAuthRejected, "signature failure must not surface as api_key rejection")
	var connErr *connect.Error
	require.ErrorAs(t, err, &connErr)
	assert.Equal(t, connect.CodeUnauthenticated, connErr.Code())
	assert.False(t, fake.signatureVerified)
}
