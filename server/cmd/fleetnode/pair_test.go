package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/internal/domain/plugins"
	"github.com/block/proto-fleet/server/internal/fleetnode/bootstrap"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

// pairCmd wraps a FleetNodePairRequest in the AgentCommand envelope the node
// expects in ControlCommand.payload.
func pairCmd(t *testing.T, req *pairingpb.FleetNodePairRequest) []byte {
	t.Helper()
	return mustMarshal(t, &pb.AgentCommand{Command: &pb.AgentCommand_Pair{Pair: req}})
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
		caps     sdk.Capabilities
		creds    *pairingpb.Credentials
		wantOK   bool
		wantKind any
	}{
		{
			name:     "basic auth uses supplied creds",
			caps:     sdk.Capabilities{},
			creds:    &pairingpb.Credentials{Username: "root", Password: &pw},
			wantOK:   true,
			wantKind: sdk.UsernamePassword{Username: "root", Password: "secret"},
		},
		{
			name:   "no creds falls through",
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
			bundle, ok := secretBundleFor(tc.caps, tc.creds)

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

func TestControlLoop_PairPartialPersistAcksPartial(t *testing.T) {
	// Arrange: the gateway accepts the upload but reports it failed to persist one
	// paired miner (RejectedCount > 0).
	cmd := &RunCmd{pairer: &stubPairer{results: map[string]*pb.FleetNodePairResult{
		"mac:aa": {DeviceIdentifier: "mac:aa", Outcome: pb.PairOutcome_PAIR_OUTCOME_PAIRED, SerialNumber: "SN1"},
	}}}
	fake := &controlFakeGateway{}
	fake.setBehavior(controlFakeBehavior{pairRejected: 1})
	fake.queue(pairCmd(t, &pairingpb.FleetNodePairRequest{
		Targets: []*pairingpb.FleetNodePairTarget{{DeviceIdentifier: "mac:aa", IpAddress: "10.0.0.5", Port: "80", DriverName: "antminer"}},
	}))

	// Act
	runControlLoopOnce(t, cmd, fake)

	// Assert: a rejected result acks PARTIAL, not OK, so the cloud isn't told the
	// command fully succeeded when a paired miner wasn't stored.
	acks := fake.acksCopy()
	require.Len(t, acks, 1)
	assert.False(t, acks[0].GetSucceeded())
	assert.Equal(t, pb.AckCode_ACK_CODE_PARTIAL, acks[0].GetCode())
	assert.Contains(t, acks[0].GetErrorMessage(), "did not persist")
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

// recordingAcker captures acks for direct handlePairCommand tests.
type recordingAcker struct {
	mu   sync.Mutex
	acks []*pb.ControlAck
}

func (a *recordingAcker) Send(req *pb.ControlStreamRequest) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if ack := req.GetAck(); ack != nil {
		a.acks = append(a.acks, ack)
	}
	return nil
}

func TestHandlePairCommand_BusyWhileAbandonedWorkersStillRunning(t *testing.T) {
	// Shrink the supervisor budget into a unit-test window.
	prev := perPairTimeout
	perPairTimeout = 50 * time.Millisecond
	t.Cleanup(func() { perPairTimeout = prev })

	// Arrange: a worker that ignores ctx and stays stuck past the supervisor
	// budget, so the first command acks PARTIAL with the worker abandoned but
	// still running. This exercises the window AFTER the handler returns (the
	// receive loop's exclusive lane is already released) where only the pair
	// gate prevents a second command from racing the mutating worker.
	block := make(chan struct{})
	cmd := &RunCmd{pairer: &ctxIgnoringPairer{
		stuck: map[string]bool{"mac:stuck": true},
		block: block,
	}}
	client := newControlClient(t, &controlFakeGateway{})
	acks := &recordingAcker{}
	target := func(id, ip string) *pairingpb.FleetNodePairRequest {
		return &pairingpb.FleetNodePairRequest{
			Targets: []*pairingpb.FleetNodePairTarget{{DeviceIdentifier: id, IpAddress: ip, Port: "80", DriverName: "antminer"}},
		}
	}

	// Act: the first command truncates and returns; the second arrives while the
	// abandoned worker is still running.
	cmd.handlePairCommand(context.Background(), client, acks, "pair-1", target("mac:stuck", "10.0.0.6"), discardLogger(t))
	cmd.handlePairCommand(context.Background(), client, acks, "pair-2", target("mac:other", "10.0.0.7"), discardLogger(t))

	// Assert
	require.Len(t, acks.acks, 2)
	assert.Equal(t, pb.AckCode_ACK_CODE_PARTIAL, acks.acks[0].GetCode())
	assert.Equal(t, pb.AckCode_ACK_CODE_BUSY, acks.acks[1].GetCode())

	// Releasing the stuck worker frees the gate for the next command.
	close(block)
	require.Eventually(t, func() bool {
		if cmd.pairMu.TryLock() {
			cmd.pairMu.Unlock()
			return true
		}
		return false
	}, 3*time.Second, 10*time.Millisecond, "gate must release once all workers exit")
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

// fakePairDriver is a minimal sdk.Driver for exercising pluginPairer.Pair: it
// records the bundles PairDevice was called with and returns a configurable
// result, and (as a DefaultCredentialsProvider) yields the configured defaults.
type fakePairDriver struct {
	pairResult sdk.DeviceInfo
	pairErr    error
	defaults   []sdk.UsernamePassword
	gotBundles []sdk.SecretBundle
}

func (d *fakePairDriver) Handshake(context.Context) (sdk.DriverIdentifier, error) {
	return sdk.DriverIdentifier{}, nil
}

func (d *fakePairDriver) DescribeDriver(context.Context) (sdk.DriverIdentifier, sdk.Capabilities, error) {
	return sdk.DriverIdentifier{}, sdk.Capabilities{}, nil
}

func (d *fakePairDriver) DiscoverDevice(context.Context, string, string) (sdk.DeviceInfo, error) {
	return sdk.DeviceInfo{}, nil
}

func (d *fakePairDriver) PairDevice(_ context.Context, _ sdk.DeviceInfo, access sdk.SecretBundle) (sdk.DeviceInfo, error) {
	d.gotBundles = append(d.gotBundles, access)
	return d.pairResult, d.pairErr
}

func (d *fakePairDriver) NewDevice(context.Context, string, sdk.DeviceInfo, sdk.SecretBundle) (sdk.NewDeviceResult, error) {
	return sdk.NewDeviceResult{}, nil
}

func (d *fakePairDriver) GetDefaultCredentials(context.Context, string, string) []sdk.UsernamePassword {
	return d.defaults
}

func newTestPairer(t *testing.T, caps sdk.Capabilities, driver sdk.Driver) *pluginPairer {
	return newTestPairerWithCredentials(t, caps, driver, nil)
}

func newTestPairerWithCredentials(t *testing.T, caps sdk.Capabilities, driver sdk.Driver, credentials credentialSealer) *pluginPairer {
	t.Helper()
	p := newPluginPairer(plugins.NewManager(&plugins.Config{}), credentials)
	require.NoError(t, p.manager.RegisterPluginForTest(&plugins.LoadedPlugin{
		Name:       "fake",
		Identifier: sdk.DriverIdentifier{DriverName: "fakedrv"},
		Driver:     driver,
		Caps:       caps,
	}))
	return p
}

type failingCredentialSealer struct{}

func (failingCredentialSealer) Seal(sdk.SecretBundle) (*pb.EncryptedCredentials, error) {
	return nil, errors.New("seal failed")
}

func fakePairTarget() *pairingpb.FleetNodePairTarget {
	return &pairingpb.FleetNodePairTarget{DeviceIdentifier: "mac:aa", IpAddress: "10.0.0.5", Port: "80", DriverName: "fakedrv"}
}

func TestPluginPairer_BasicAuthRejectsUnreportableCredentials(t *testing.T) {
	longPw := strings.Repeat("p", maxUsedPasswordBytes+1)
	longUser := strings.Repeat("u", maxPairIdentityBytes+1)
	shortPw := "pw"
	cases := []struct {
		name  string
		creds *pairingpb.Credentials
	}{
		{name: "password exceeds cap", creds: &pairingpb.Credentials{Username: "root", Password: &longPw}},
		{name: "username exceeds cap", creds: &pairingpb.Credentials{Username: longUser, Password: &shortPw}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			drv := &fakePairDriver{pairResult: sdk.DeviceInfo{SerialNumber: "SN1"}}
			p := newTestPairer(t, sdk.Capabilities{sdk.CapabilityPairing: true}, drv)

			// Act
			res := p.Pair(context.Background(), fakePairTarget(), tc.creds)

			// Assert: refused before any pair attempt so the cloud never stores an
			// unusable secret.
			assert.Equal(t, pb.PairOutcome_PAIR_OUTCOME_ERROR, res.GetOutcome())
			assert.Contains(t, res.GetErrorMessage(), "exceed the maximum reportable size")
			assert.Empty(t, drv.gotBundles)
		})
	}
}

func TestPluginPairer_BasicAuthReportsEncryptedCredentials(t *testing.T) {
	// Arrange
	drv := &fakePairDriver{pairResult: sdk.DeviceInfo{SerialNumber: "SN1"}}
	codec := &credentialCodec{key: bytes.Repeat([]byte{1}, credentialKeySize)}
	p := newTestPairerWithCredentials(t, sdk.Capabilities{sdk.CapabilityPairing: true}, drv, codec)
	pw := "hunter2"

	// Act
	res := p.Pair(context.Background(), fakePairTarget(), &pairingpb.Credentials{Username: "root", Password: &pw})

	// Assert: production fleet-node pairing reports only node-owned ciphertext.
	assert.Equal(t, pb.PairOutcome_PAIR_OUTCOME_PAIRED, res.GetOutcome())
	require.NotEmpty(t, res.GetEncryptedCredentials())
	require.NotEmpty(t, res.GetEncryptedCredentials().GetUsername())
	require.NotEmpty(t, res.GetEncryptedCredentials().GetPassword())
	bundle, err := codec.Open(res.GetEncryptedCredentials())
	require.NoError(t, err)
	assert.Equal(t, sdk.UsernamePassword{Username: "root", Password: "hunter2"}, bundle.Kind)
}

func TestPluginPairer_BasicAuthRequiresCredentialSealer(t *testing.T) {
	// Arrange
	drv := &fakePairDriver{pairResult: sdk.DeviceInfo{SerialNumber: "SN1"}}
	p := newTestPairer(t, sdk.Capabilities{sdk.CapabilityPairing: true}, drv)
	pw := "hunter2"

	// Act
	res := p.Pair(context.Background(), fakePairTarget(), &pairingpb.Credentials{Username: "root", Password: &pw})

	// Assert: refuse before pairing so credentials cannot authenticate the miner
	// without also producing node-owned ciphertext for future commands.
	assert.Equal(t, pb.PairOutcome_PAIR_OUTCOME_ERROR, res.GetOutcome())
	assert.Contains(t, res.GetErrorMessage(), "credential sealer is not configured")
	assert.Empty(t, drv.gotBundles)
}

func TestPluginPairer_CredentialSealErrorSkipsPairAttempt(t *testing.T) {
	// Arrange
	drv := &fakePairDriver{pairResult: sdk.DeviceInfo{SerialNumber: "SN1"}}
	p := newTestPairerWithCredentials(t, sdk.Capabilities{sdk.CapabilityPairing: true}, drv, failingCredentialSealer{})
	pw := "hunter2"

	// Act
	res := p.Pair(context.Background(), fakePairTarget(), &pairingpb.Credentials{Username: "root", Password: &pw})

	// Assert: refuse before pairing so a local reporting failure cannot follow
	// a successful miner-side pair.
	assert.Equal(t, pb.PairOutcome_PAIR_OUTCOME_ERROR, res.GetOutcome())
	assert.Contains(t, res.GetErrorMessage(), "encrypt credentials")
	assert.Empty(t, drv.gotBundles)
}

func TestPluginPairer_DefaultCredentialsReportsEncryptedCredentials(t *testing.T) {
	// Arrange: no operator creds; the driver provides a working default.
	active := true
	drv := &fakePairDriver{
		pairResult: sdk.DeviceInfo{SerialNumber: "SN1", DefaultPasswordActive: &active},
		defaults:   []sdk.UsernamePassword{{Username: "admin", Password: "admin"}},
	}
	codec := &credentialCodec{key: bytes.Repeat([]byte{2}, credentialKeySize)}
	p := newTestPairerWithCredentials(t, sdk.Capabilities{sdk.CapabilityPairing: true}, drv, codec)

	// Act
	res := p.Pair(context.Background(), fakePairTarget(), nil)

	// Assert: the default that worked is sealed so the cloud can store it without
	// seeing plaintext.
	assert.Equal(t, pb.PairOutcome_PAIR_OUTCOME_PAIRED, res.GetOutcome())
	require.NotEmpty(t, res.GetEncryptedCredentials())
	bundle, err := codec.Open(res.GetEncryptedCredentials())
	require.NoError(t, err)
	assert.Equal(t, sdk.UsernamePassword{Username: "admin", Password: "admin"}, bundle.Kind)
	require.NotNil(t, res.DefaultPasswordActive)
	assert.True(t, res.GetDefaultPasswordActive())
}

func TestPluginPairer_DefaultCredentialsSkipsUnreportable(t *testing.T) {
	// Arrange: the first default is unreportable (oversized), the second is usable.
	drv := &fakePairDriver{
		pairResult: sdk.DeviceInfo{SerialNumber: "SN1"},
		defaults: []sdk.UsernamePassword{
			{Username: "big", Password: strings.Repeat("p", maxUsedPasswordBytes+1)},
			{Username: "admin", Password: "admin"},
		},
	}
	codec := &credentialCodec{key: bytes.Repeat([]byte{3}, credentialKeySize)}
	p := newTestPairerWithCredentials(t, sdk.Capabilities{sdk.CapabilityPairing: true}, drv, codec)

	// Act
	res := p.Pair(context.Background(), fakePairTarget(), nil)

	// Assert: the oversized default is skipped without a pair attempt; the usable one pairs.
	assert.Equal(t, pb.PairOutcome_PAIR_OUTCOME_PAIRED, res.GetOutcome())
	require.NotEmpty(t, res.GetEncryptedCredentials())
	bundle, err := codec.Open(res.GetEncryptedCredentials())
	require.NoError(t, err)
	assert.Equal(t, sdk.UsernamePassword{Username: "admin", Password: "admin"}, bundle.Kind)
	require.Len(t, drv.gotBundles, 1)
	up, ok := drv.gotBundles[0].Kind.(sdk.UsernamePassword)
	require.True(t, ok)
	assert.Equal(t, "admin", up.Username)
}

func TestPluginPairer_NoCredentialsNoDefaultsAuthNeeded(t *testing.T) {
	// Arrange: basic-auth driver, no operator creds, no usable defaults.
	drv := &fakePairDriver{}
	p := newTestPairer(t, sdk.Capabilities{sdk.CapabilityPairing: true}, drv)

	// Act
	res := p.Pair(context.Background(), fakePairTarget(), nil)

	// Assert
	assert.Equal(t, pb.PairOutcome_PAIR_OUTCOME_AUTH_NEEDED, res.GetOutcome())
	assert.Empty(t, drv.gotBundles)
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
