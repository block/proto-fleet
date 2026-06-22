package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/internal/fleetnode/bootstrap"
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

func TestSecretBundleFor(t *testing.T) {
	pw := "secret"
	cases := []struct {
		name     string
		creds    *pairingpb.Credentials
		wantOK   bool
		wantKind any
	}{
		{
			name:     "basic auth uses supplied creds",
			creds:    &pairingpb.Credentials{Username: "root", Password: &pw},
			wantOK:   true,
			wantKind: sdk.UsernamePassword{Username: "root", Password: "secret"},
		},
		{
			name:   "no creds falls through",
			wantOK: false,
		},
		{
			name:   "username without password falls through",
			creds:  &pairingpb.Credentials{Username: "root"},
			wantOK: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Act
			bundle, ok := secretBundleFor(tc.creds)

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

func TestSetPaired_ClampsOversizedIdentityToProtoCaps(t *testing.T) {
	// Arrange: a plugin returns identity fields longer than FleetNodePairResult
	// caps; reporting them unclamped would fail validation for the whole chunk.
	res := &pb.FleetNodePairResult{DeviceIdentifier: "mac:x"}
	long := strings.Repeat("z", 300)
	info := sdk.DeviceInfo{
		SerialNumber:    long,
		MacAddress:      strings.Repeat("a", 100),
		Model:           long,
		Manufacturer:    long,
		FirmwareVersion: long,
	}

	// Act
	setPaired(res, info)

	// Assert: every reported field is within its proto max_len.
	assert.LessOrEqual(t, len(res.GetSerialNumber()), 255)
	assert.LessOrEqual(t, len(res.GetMacAddress()), 64)
	assert.LessOrEqual(t, len(res.GetModel()), 255)
	assert.LessOrEqual(t, len(res.GetManufacturer()), 255)
	assert.LessOrEqual(t, len(res.GetFirmwareVersion()), 255)
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

func TestControlLoop_PairReportFailureAcksReportFailed(t *testing.T) {
	// Arrange: a pairable target, but the gateway rejects the result upload.
	cmd := &RunCmd{pairer: &stubPairer{results: map[string]*pb.FleetNodePairResult{
		"mac:aa": {DeviceIdentifier: "mac:aa", Outcome: pb.PairOutcome_PAIR_OUTCOME_PAIRED, SerialNumber: "SN1"},
	}}}
	fake := &controlFakeGateway{}
	fake.setBehavior(controlFakeBehavior{pairReportErr: connect.NewError(connect.CodeUnavailable, errors.New("upload boom"))})
	fake.queue(pairCmd(t, &pairingpb.FleetNodePairRequest{
		Targets: []*pairingpb.FleetNodePairTarget{{DeviceIdentifier: "mac:aa", IpAddress: "10.0.0.5", Port: "80", DriverName: "antminer"}},
	}))

	// Act
	runControlLoopOnce(t, cmd, fake)

	// Assert: a failed upload acks REPORT_FAILED after attempting the report.
	acks := fake.acksCopy()
	require.Len(t, acks, 1)
	assert.False(t, acks[0].GetSucceeded())
	assert.Equal(t, pb.AckCode_ACK_CODE_REPORT_FAILED, acks[0].GetCode())
	assert.Contains(t, acks[0].GetErrorMessage(), "report paired devices")
	require.Len(t, fake.pairReportsCopy(), 1, "REPORT_FAILED implies the report was attempted")
}

func TestControlLoop_PairSupervisorTruncatedAcksPartial(t *testing.T) {
	// Shrink the supervisor budget into a unit-test window.
	prev := perPairTimeout
	perPairTimeout = 50 * time.Millisecond
	t.Cleanup(func() { perPairTimeout = prev })

	// Arrange: one fast target + one that ignores ctx; the supervisor budget
	// fires before commandTimeout so cmdCtx stays alive and the ack is PARTIAL.
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })
	cmd := &RunCmd{pairer: &ctxIgnoringPairer{
		fast:  map[string]*pb.FleetNodePairResult{"mac:fast": {DeviceIdentifier: "mac:fast", Outcome: pb.PairOutcome_PAIR_OUTCOME_PAIRED}},
		stuck: map[string]bool{"mac:stuck": true},
		block: block,
	}}
	state := &bootstrap.State{FleetNodeID: 7}
	fake := &controlFakeGateway{}
	fake.queueWithID("pair-1", pairCmd(t, &pairingpb.FleetNodePairRequest{
		Targets: []*pairingpb.FleetNodePairTarget{
			{DeviceIdentifier: "mac:fast", IpAddress: "10.0.0.5", Port: "80", DriverName: "antminer"},
			{DeviceIdentifier: "mac:stuck", IpAddress: "10.0.0.6", Port: "80", DriverName: "antminer"},
		},
	}))
	client := newControlClient(t, fake)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Act
	done := make(chan error, 1)
	go func() { done <- cmd.runControlLoop(ctx, client, state, discardLogger(t)) }()
	require.Eventually(t, func() bool { return fake.ackCount() > 0 }, 3*time.Second, 20*time.Millisecond)
	cancel()
	<-done

	// Assert
	acks := fake.acksCopy()
	require.Len(t, acks, 1)
	assert.False(t, acks[0].GetSucceeded())
	assert.Equal(t, pb.AckCode_ACK_CODE_PARTIAL, acks[0].GetCode())
	assert.Contains(t, acks[0].GetErrorMessage(), "supervisor")
}

// Ignores ctx for identifiers in stuck (blocks on `block`); fast for the rest.
type ctxIgnoringPairer struct {
	fast  map[string]*pb.FleetNodePairResult
	stuck map[string]bool
	block chan struct{}
}

func (p *ctxIgnoringPairer) Pair(_ context.Context, target *pairingpb.FleetNodePairTarget, _ *pairingpb.Credentials) *pb.FleetNodePairResult {
	id := target.GetDeviceIdentifier()
	if p.stuck[id] {
		<-p.block
	}
	if r, ok := p.fast[id]; ok {
		return r
	}
	return &pb.FleetNodePairResult{DeviceIdentifier: id, Outcome: pb.PairOutcome_PAIR_OUTCOME_ERROR}
}
