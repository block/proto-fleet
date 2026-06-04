package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/internal/domain/token"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

// pairCmd wraps a FleetNodePairRequest in the AgentCommand envelope the node
// expects in ControlCommand.payload.
func pairCmd(t *testing.T, req *pairingpb.FleetNodePairRequest) []byte {
	t.Helper()
	return mustMarshal(t, &pairingpb.AgentCommand{Command: &pairingpb.AgentCommand_Pair{Pair: req}})
}

type stubPairer struct {
	results map[string]*pb.FleetNodePairResult
}

func (s *stubPairer) Pair(_ context.Context, target *pairingpb.FleetNodePairTarget, _ *pairingpb.Credentials) *pb.FleetNodePairResult {
	if r, ok := s.results[target.GetDeviceIdentifier()]; ok {
		return r
	}
	return &pb.FleetNodePairResult{
		DeviceIdentifier: target.GetDeviceIdentifier(),
		Outcome:          pb.PairOutcome_PAIR_OUTCOME_ERROR,
		ErrorMessage:     "no stub result",
	}
}

func TestMinerSigningPublicKeySPKIBase64_MatchesTokenService(t *testing.T) {
	// Arrange: a fresh ed25519 key, hex-encoded like bootstrap.State stores it.
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	ts, err := token.NewService(token.Config{
		ClientToken:                token.AuthTokenConfig{SecretKey: "0123456789abcdef0123456789abcdef", ExpirationPeriod: time.Minute},
		MinerTokenExpirationPeriod: time.Minute,
	})
	require.NoError(t, err)
	want, err := ts.ExtractPublicKeyFromPrivateKey(priv)
	require.NoError(t, err)

	// Act
	got, err := minerSigningPublicKeySPKIBase64(hex.EncodeToString(priv))

	// Assert: the node-derived key must equal the server's byte for byte, or a
	// miner paired here would reject the JWTs the node signs at runtime.
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestMinerSigningPublicKeySPKIBase64_RejectsBadKey(t *testing.T) {
	cases := []struct{ name, hexKey string }{
		{name: "not hex", hexKey: "zzzz"},
		{name: "wrong length", hexKey: "abcd"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Act
			_, err := minerSigningPublicKeySPKIBase64(tc.hexKey)

			// Assert
			require.Error(t, err)
		})
	}
}

func TestSecretBundleFor(t *testing.T) {
	pw := "secret"
	cases := []struct {
		name     string
		caps     sdk.Capabilities
		creds    *pairingpb.Credentials
		wantOK   bool
		wantKind any
	}{
		{
			name:     "asymmetric uses node key",
			caps:     sdk.Capabilities{sdk.CapabilityAsymmetricAuth: true},
			wantOK:   true,
			wantKind: sdk.APIKey{Key: "node-pub"},
		},
		{
			name:     "basic auth uses supplied creds",
			caps:     sdk.Capabilities{},
			creds:    &pairingpb.Credentials{Username: "root", Password: &pw},
			wantOK:   true,
			wantKind: sdk.UsernamePassword{Username: "root", Password: "secret"},
		},
		{
			name:   "no creds and not asymmetric falls through",
			caps:   sdk.Capabilities{},
			wantOK: false,
		},
		{
			name:   "username without password falls through",
			caps:   sdk.Capabilities{},
			creds:  &pairingpb.Credentials{Username: "root"},
			wantOK: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Act
			bundle, ok := secretBundleFor(tc.caps, "node-pub", tc.creds)

			// Assert
			assert.Equal(t, tc.wantOK, ok)
			if tc.wantOK {
				assert.Equal(t, tc.wantKind, bundle.Kind)
			}
		})
	}
}

func TestClassifyNodePairError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want pb.PairOutcome
	}{
		{name: "grpc unauthenticated is auth failed", err: status.Error(codes.Unauthenticated, "bad creds"), want: pb.PairOutcome_PAIR_OUTCOME_AUTH_FAILED},
		{name: "sdk auth failure is auth failed", err: sdk.SDKError{Code: sdk.ErrCodeAuthenticationFailed, Message: "rejected"}, want: pb.PairOutcome_PAIR_OUTCOME_AUTH_FAILED},
		{name: "other error is error", err: errors.New("connection refused"), want: pb.PairOutcome_PAIR_OUTCOME_ERROR},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			res := &pb.FleetNodePairResult{DeviceIdentifier: "d1"}

			// Act
			classifyNodePairError(tc.err, res)

			// Assert
			assert.Equal(t, tc.want, res.GetOutcome())
			assert.NotEmpty(t, res.GetErrorMessage())
		})
	}
}

func TestControlLoop_PairAcksAndReportsResults(t *testing.T) {
	// Arrange: a batch of two targets with distinct stubbed outcomes.
	pw := "pw"
	cmd := &RunCmd{pairer: &stubPairer{results: map[string]*pb.FleetNodePairResult{
		"mac:aa": {DeviceIdentifier: "mac:aa", Outcome: pb.PairOutcome_PAIR_OUTCOME_PAIRED, SerialNumber: "SN1", MacAddress: "aa", Model: "S19", FirmwareVersion: "v1"},
		"mac:bb": {DeviceIdentifier: "mac:bb", Outcome: pb.PairOutcome_PAIR_OUTCOME_AUTH_NEEDED, ErrorMessage: "credentials required"},
	}}}
	fake := &controlFakeGateway{}
	fake.queue(pairCmd(t, &pairingpb.FleetNodePairRequest{
		Credentials: &pairingpb.Credentials{Username: "root", Password: &pw},
		Targets: []*pairingpb.FleetNodePairTarget{
			{DeviceIdentifier: "mac:aa", IpAddress: "10.0.0.5", Port: "80", DriverName: "antminer"},
			{DeviceIdentifier: "mac:bb", IpAddress: "10.0.0.6", Port: "80", DriverName: "antminer"},
		},
	}))

	// Act
	runControlLoopOnce(t, cmd, fake)

	// Assert: ack OK, and the report carries both per-device outcomes.
	acks := fake.acksCopy()
	require.Len(t, acks, 1)
	assert.True(t, acks[0].GetSucceeded())
	assert.Equal(t, pb.AckCode_ACK_CODE_OK, acks[0].GetCode())

	reports := fake.pairReportsCopy()
	require.Len(t, reports, 1)
	assert.Equal(t, acks[0].GetCommandId(), reports[0].GetCommandId(), "ack and report must share command_id")
	got := map[string]pb.PairOutcome{}
	for _, r := range reports[0].GetResults() {
		got[r.GetDeviceIdentifier()] = r.GetOutcome()
	}
	assert.Equal(t, pb.PairOutcome_PAIR_OUTCOME_PAIRED, got["mac:aa"])
	assert.Equal(t, pb.PairOutcome_PAIR_OUTCOME_AUTH_NEEDED, got["mac:bb"])
}

func TestControlLoop_PairAgentIncapableWithoutPairer(t *testing.T) {
	// Arrange: no pairer wired (plugins failed to load / discovery-only build).
	cmd := &RunCmd{}
	fake := &controlFakeGateway{}
	fake.queue(pairCmd(t, &pairingpb.FleetNodePairRequest{
		Targets: []*pairingpb.FleetNodePairTarget{{DeviceIdentifier: "mac:aa", IpAddress: "10.0.0.5", Port: "80", DriverName: "antminer"}},
	}))

	// Act
	runControlLoopOnce(t, cmd, fake)

	// Assert
	acks := fake.acksCopy()
	require.Len(t, acks, 1)
	assert.False(t, acks[0].GetSucceeded())
	assert.Equal(t, pb.AckCode_ACK_CODE_AGENT_INCAPABLE, acks[0].GetCode())
	assert.Empty(t, fake.pairReportsCopy())
}

func TestControlLoop_PairEmptyTargetsBadRequest(t *testing.T) {
	// Arrange
	cmd := &RunCmd{pairer: &stubPairer{}}
	fake := &controlFakeGateway{}
	fake.queue(pairCmd(t, &pairingpb.FleetNodePairRequest{}))

	// Act
	runControlLoopOnce(t, cmd, fake)

	// Assert
	acks := fake.acksCopy()
	require.Len(t, acks, 1)
	assert.Equal(t, pb.AckCode_ACK_CODE_BAD_REQUEST, acks[0].GetCode())
	assert.Empty(t, fake.pairReportsCopy())
}
