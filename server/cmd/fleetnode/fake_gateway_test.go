package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1/fleetnodegatewayv1connect"

	"github.com/block/proto-fleet/server/internal/fleetnodebootstrap"
)

type fakeFleetNodeGateway struct {
	fleetnodegatewayv1connect.UnimplementedFleetNodeGatewayServiceHandler

	expectedCode     string
	expectedAPIKey   string
	fleetNodeID      int64
	identityPub      ed25519.PublicKey
	challenge        []byte
	sessionToken     string
	sessionExpiresAt time.Time
	registerError    error
	beginAuthError   error

	registered        bool
	signatureVerified bool
}

func (f *fakeFleetNodeGateway) Register(_ context.Context, req *connect.Request[pb.RegisterRequest]) (*connect.Response[pb.RegisterResponse], error) {
	if f.registerError != nil {
		return nil, f.registerError
	}
	if f.expectedCode != "" && req.Msg.GetEnrollmentToken() != f.expectedCode {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("invalid enrollment code"))
	}
	f.identityPub = ed25519.PublicKey(req.Msg.GetIdentityPubkey())
	f.registered = true
	return connect.NewResponse(&pb.RegisterResponse{
		FleetNodeId:         f.fleetNodeID,
		EnrollmentStatus:    pb.EnrollmentStatus_ENROLLMENT_STATUS_PENDING,
		IdentityFingerprint: fleetnodebootstrap.IdentityFingerprint(req.Msg.GetIdentityPubkey()),
	}), nil
}

func (f *fakeFleetNodeGateway) BeginAuthHandshake(_ context.Context, req *connect.Request[pb.BeginAuthHandshakeRequest]) (*connect.Response[pb.BeginAuthHandshakeResponse], error) {
	if f.beginAuthError != nil {
		return nil, f.beginAuthError
	}
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

func (f *fakeFleetNodeGateway) CompleteAuthHandshake(_ context.Context, req *connect.Request[pb.CompleteAuthHandshakeRequest]) (*connect.Response[pb.CompleteAuthHandshakeResponse], error) {
	if !ed25519.Verify(f.identityPub, req.Msg.GetChallenge(), req.Msg.GetSignature()) {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("bad signature"))
	}
	f.signatureVerified = true
	return connect.NewResponse(&pb.CompleteAuthHandshakeResponse{
		SessionToken: f.sessionToken,
		ExpiresAt:    timestamppb.New(f.sessionExpiresAt),
	}), nil
}

func newFakeServer(t *testing.T, fake *fakeFleetNodeGateway) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	path, h := fleetnodegatewayv1connect.NewFleetNodeGatewayServiceHandler(fake)
	mux.Handle(path, h)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}
