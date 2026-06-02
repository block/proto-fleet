package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1/fleetnodegatewayv1connect"
	"github.com/block/proto-fleet/server/internal/fleetnode/bootstrap"
	"github.com/block/proto-fleet/server/internal/testutil"
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

	heartbeatMu          sync.Mutex
	heartbeatsReceived   []heartbeatRecord
	expectedSessionToken string
	onHeartbeat          func(count int)
}

type heartbeatRecord struct {
	authHeader string
	sentAt     time.Time
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
		IdentityFingerprint: bootstrap.IdentityFingerprint(req.Msg.GetIdentityPubkey()),
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

func (f *fakeFleetNodeGateway) UploadHeartbeat(_ context.Context, req *connect.Request[pb.UploadHeartbeatRequest]) (*connect.Response[pb.UploadHeartbeatResponse], error) {
	auth := req.Header().Get("Authorization")
	if f.expectedSessionToken != "" && auth != "Bearer "+f.expectedSessionToken {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("invalid session token"))
	}
	f.heartbeatMu.Lock()
	f.heartbeatsReceived = append(f.heartbeatsReceived, heartbeatRecord{authHeader: auth, sentAt: req.Msg.GetSentAt().AsTime()})
	count := len(f.heartbeatsReceived)
	cb := f.onHeartbeat
	f.heartbeatMu.Unlock()
	if cb != nil {
		cb(count)
	}
	return connect.NewResponse(&pb.UploadHeartbeatResponse{ReceivedAt: timestamppb.Now()}), nil
}

func (f *fakeFleetNodeGateway) heartbeatCount() int {
	f.heartbeatMu.Lock()
	defer f.heartbeatMu.Unlock()
	return len(f.heartbeatsReceived)
}

func (f *fakeFleetNodeGateway) heartbeats() []heartbeatRecord {
	f.heartbeatMu.Lock()
	defer f.heartbeatMu.Unlock()
	out := make([]heartbeatRecord, len(f.heartbeatsReceived))
	copy(out, f.heartbeatsReceived)
	return out
}

func newFakeServer(t *testing.T, fake *fakeFleetNodeGateway) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	path, h := fleetnodegatewayv1connect.NewFleetNodeGatewayServiceHandler(fake)
	mux.Handle(path, h)
	return testutil.NewH2CServer(t, mux)
}
