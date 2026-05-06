package main

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

	pb "github.com/block/proto-fleet/server/generated/grpc/agentgateway/v1"
	"github.com/block/proto-fleet/server/generated/grpc/agentgateway/v1/agentgatewayv1connect"
)

type fakeAgentGateway struct {
	agentgatewayv1connect.UnimplementedAgentGatewayServiceHandler

	expectedAPIKey   string
	identityPub      ed25519.PublicKey
	challenge        []byte
	sessionToken     string
	sessionExpiresAt time.Time

	signatureVerified bool
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
	path, h := agentgatewayv1connect.NewAgentGatewayServiceHandler(fake)
	mux.Handle(path, h)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestRunHandshake_HappyPath(t *testing.T) {
	t.Parallel()

	// Arrange
	pub, priv, err := generateKeypair()
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
	client := newGatewayClient(srv.URL)

	// Act
	err = runHandshake(t.Context(), client, state)

	// Assert
	require.NoError(t, err)
	assert.Equal(t, "session-token-abc", state.SessionToken)
	assert.WithinDuration(t, expiresAt, state.SessionExpiresAt, time.Second)
	assert.True(t, fake.signatureVerified)
}

func TestRunHandshake_WrongAPIKey(t *testing.T) {
	t.Parallel()

	// Arrange
	pub, priv, err := generateKeypair()
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
	client := newGatewayClient(srv.URL)

	// Act
	err = runHandshake(t.Context(), client, state)

	// Assert
	require.Error(t, err)
	var connErr *connect.Error
	require.ErrorAs(t, err, &connErr)
	assert.Equal(t, connect.CodeUnauthenticated, connErr.Code())
	assert.Empty(t, state.SessionToken)
}

func TestRunHandshake_BadSignature(t *testing.T) {
	t.Parallel()

	// Arrange
	pub, _, err := generateKeypair()
	require.NoError(t, err)
	_, otherPriv, err := generateKeypair()
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
	client := newGatewayClient(srv.URL)

	// Act
	err = runHandshake(t.Context(), client, state)

	// Assert
	require.Error(t, err)
	var connErr *connect.Error
	require.ErrorAs(t, err, &connErr)
	assert.Equal(t, connect.CodeUnauthenticated, connErr.Code())
	assert.False(t, fake.signatureVerified)
}
