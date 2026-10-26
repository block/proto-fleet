package main

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"buf.build/go/protovalidate"
	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1/fleetnodegatewayv1connect"
	fleetnodecontrol "github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/credentialkey"
	"github.com/block/proto-fleet/server/internal/fleetnode/bootstrap"
	"github.com/block/proto-fleet/server/internal/testutil"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

// haTestNode is a RunCmd with its own credential key, enrollment encryption
// key, and state file, like one half of an HA pair.
type haTestNode struct {
	cmd       *RunCmd
	publicKey []byte
	statePath string
}

func newHANode(t *testing.T) *haTestNode {
	t.Helper()
	private, err := ecdh.X25519().GenerateKey(rand.Reader)
	require.NoError(t, err)
	statePath := bootstrap.StatePath(t.TempDir())
	st := &bootstrap.State{
		FleetNodeID:             7,
		EncryptionPrivateKeyHex: hex.EncodeToString(private.Bytes()),
		EncryptionPublicKeyHex:  hex.EncodeToString(private.PublicKey().Bytes()),
	}
	codec, err := ensureCredentialCodec(statePath, st)
	require.NoError(t, err)
	return &haTestNode{
		cmd: &RunCmd{
			credentials:              codec,
			statePath:                statePath,
			state:                    st,
			passwordUpdatePrivateKey: private.Bytes(),
		},
		publicKey: private.PublicKey().Bytes(),
		statePath: statePath,
	}
}

func (n *haTestNode) persistedKeyHex(t *testing.T) string {
	t.Helper()
	loaded, exists, err := bootstrap.LoadState(n.statePath)
	require.NoError(t, err)
	require.True(t, exists)
	return loaded.CredentialKeyHex
}

func singleAck(t *testing.T, stream *capturingAcker) *pb.ControlAck {
	t.Helper()
	require.Len(t, stream.sent, 1)
	ack := stream.sent[0].GetAck()
	require.NotNil(t, ack)
	return ack
}

func credentialKeyResult(t *testing.T, ack *pb.ControlAck) *pb.CredentialKeyResult {
	t.Helper()
	result := &pb.CredentialKeyResult{}
	require.NoError(t, proto.Unmarshal(ack.GetPayload(), result))
	require.NoError(t, protovalidate.Validate(result))
	return result
}

func TestCredentialKeyTransfer_ExportThenInstallMovesKeyToStandby(t *testing.T) {
	// Arrange
	active := newHANode(t)
	standby := newHANode(t)
	activeKey := active.cmd.credentials.currentKey()
	require.NotEqual(t, activeKey, standby.cmd.credentials.currentKey())
	sealedByActive, err := active.cmd.credentials.Seal(sdk.SecretBundle{
		Version: "v1",
		Kind:    sdk.UsernamePassword{Username: "root", Password: "hunter2"},
	})
	require.NoError(t, err)
	_, err = standby.cmd.credentials.Open(sealedByActive)
	require.Error(t, err, "standby must not open the active node's credentials before the install")
	exported := &capturingAcker{}
	installed := &capturingAcker{}

	// Act
	active.cmd.handleExportCredentialKey(exported, "export-1", &pb.ExportCredentialKey{RecipientPublicKey: standby.publicKey}, discardLogger(t))
	exportResult := credentialKeyResult(t, singleAck(t, exported))
	standby.cmd.handleInstallCredentialKey(installed, "install-1", &pb.InstallCredentialKey{SealedKey: exportResult.GetSealedKey()}, discardLogger(t))

	// Assert
	exportAck := singleAck(t, exported)
	assert.Equal(t, "export-1", exportAck.GetCommandId())
	assert.Equal(t, pb.AckCode_ACK_CODE_OK, exportAck.GetCode())
	assert.True(t, exportAck.GetSucceeded())
	require.NotNil(t, exportResult.GetSealedKey())
	assert.NoError(t, protovalidate.Validate(exportResult.GetSealedKey()))

	installAck := singleAck(t, installed)
	assert.Equal(t, "install-1", installAck.GetCommandId())
	assert.Equal(t, pb.AckCode_ACK_CODE_OK, installAck.GetCode())
	installResult := credentialKeyResult(t, installAck)
	assert.Nil(t, installResult.GetSealedKey(), "install must not echo the sealed key")
	assert.Equal(t, credentialkey.Fingerprint(activeKey), exportResult.GetFingerprint())
	assert.Equal(t, exportResult.GetFingerprint(), installResult.GetFingerprint())

	assert.Equal(t, activeKey, standby.cmd.credentials.currentKey())
	assert.Equal(t, hex.EncodeToString(activeKey), standby.cmd.state.CredentialKeyHex)
	assert.Equal(t, hex.EncodeToString(activeKey), standby.persistedKeyHex(t))
	bundle, err := standby.cmd.credentials.Open(sealedByActive)
	require.NoError(t, err)
	assert.Equal(t, sdk.UsernamePassword{Username: "root", Password: "hunter2"}, bundle.Kind)
	assert.Equal(t, hex.EncodeToString(activeKey), active.persistedKeyHex(t), "export must not change the active node")
}

func TestCredentialKeyTransfer_InstallKeepsOtherPersistedState(t *testing.T) {
	// Arrange
	active := newHANode(t)
	standby := newHANode(t)
	standby.cmd.state.APIKey = "standby-api-key"
	require.NoError(t, bootstrap.SaveState(standby.statePath, standby.cmd.state))
	exported := &capturingAcker{}
	active.cmd.handleExportCredentialKey(exported, "export-1", &pb.ExportCredentialKey{RecipientPublicKey: standby.publicKey}, discardLogger(t))
	sealed := credentialKeyResult(t, singleAck(t, exported)).GetSealedKey()

	// Act
	standby.cmd.handleInstallCredentialKey(&capturingAcker{}, "install-1", &pb.InstallCredentialKey{SealedKey: sealed}, discardLogger(t))

	// Assert
	loaded, _, err := bootstrap.LoadState(standby.statePath)
	require.NoError(t, err)
	assert.Equal(t, "standby-api-key", loaded.APIKey)
	assert.Equal(t, standby.cmd.state.EncryptionPrivateKeyHex, loaded.EncryptionPrivateKeyHex)
	assert.Equal(t, int64(7), loaded.FleetNodeID)
}

func TestHandleExportCredentialKey_Rejections(t *testing.T) {
	tests := []struct {
		name         string
		credentials  bool
		recipientKey []byte
		wantCode     pb.AckCode
	}{
		{"no credential codec is agent incapable", false, make([]byte, 32), pb.AckCode_ACK_CODE_AGENT_INCAPABLE},
		{"short recipient key is bad request", true, []byte("short"), pb.AckCode_ACK_CODE_BAD_REQUEST},
		{"empty recipient key is bad request", true, nil, pb.AckCode_ACK_CODE_BAD_REQUEST},
		{"low order recipient key is bad request", true, make([]byte, 32), pb.AckCode_ACK_CODE_BAD_REQUEST},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			node := newHANode(t)
			if !tc.credentials {
				node.cmd.credentials = nil
			}
			stream := &capturingAcker{}

			// Act
			node.cmd.handleExportCredentialKey(stream, "export-1", &pb.ExportCredentialKey{RecipientPublicKey: tc.recipientKey}, discardLogger(t))

			// Assert
			ack := singleAck(t, stream)
			assert.Equal(t, tc.wantCode, ack.GetCode())
			assert.False(t, ack.GetSucceeded())
			assert.NotEmpty(t, ack.GetErrorMessage())
			assert.Empty(t, ack.GetPayload())
		})
	}
}

func TestHandleInstallCredentialKey_Rejections(t *testing.T) {
	// Arrange
	otherNode := newHANode(t)
	sealedToOther, err := credentialkey.Seal(otherNode.publicKey, make([]byte, credentialKeySize))
	require.NoError(t, err)
	wrongAlgorithm := proto.Clone(sealedToOther).(*pb.NodeEncryptedPayload) //nolint:errcheck,forcetypeassert // clone of the same type
	wrongAlgorithm.Algorithm = "unsupported"
	tests := []struct {
		name       string
		mutateNode func(*RunCmd)
		sealedKey  *pb.NodeEncryptedPayload
		wantCode   pb.AckCode
	}{
		{"no credential codec is agent incapable", func(r *RunCmd) { r.credentials = nil }, sealedToOther, pb.AckCode_ACK_CODE_AGENT_INCAPABLE},
		{"no private key is agent incapable", func(r *RunCmd) { r.passwordUpdatePrivateKey = nil }, sealedToOther, pb.AckCode_ACK_CODE_AGENT_INCAPABLE},
		{"key sealed to another recipient is bad request", func(*RunCmd) {}, sealedToOther, pb.AckCode_ACK_CODE_BAD_REQUEST},
		{"missing sealed key is bad request", func(*RunCmd) {}, nil, pb.AckCode_ACK_CODE_BAD_REQUEST},
		{"wrong algorithm is bad request", func(*RunCmd) {}, wrongAlgorithm, pb.AckCode_ACK_CODE_BAD_REQUEST},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			node := newHANode(t)
			originalKey := node.cmd.credentials.currentKey()
			originalHex := node.cmd.state.CredentialKeyHex
			tc.mutateNode(node.cmd)
			stream := &capturingAcker{}

			// Act
			node.cmd.handleInstallCredentialKey(stream, "install-1", &pb.InstallCredentialKey{SealedKey: tc.sealedKey}, discardLogger(t))

			// Assert
			ack := singleAck(t, stream)
			assert.Equal(t, tc.wantCode, ack.GetCode())
			assert.False(t, ack.GetSucceeded())
			assert.NotEmpty(t, ack.GetErrorMessage())
			assert.Empty(t, ack.GetPayload())
			assert.Equal(t, originalHex, node.cmd.state.CredentialKeyHex)
			assert.Equal(t, originalHex, node.persistedKeyHex(t))
			if node.cmd.credentials != nil {
				assert.Equal(t, originalKey, node.cmd.credentials.currentKey())
			}
		})
	}
}

func TestHandleInstallCredentialKey_PersistFailureLeavesKeyUnchanged(t *testing.T) {
	// Arrange
	active := newHANode(t)
	standby := newHANode(t)
	originalKey := standby.cmd.credentials.currentKey()
	originalHex := standby.cmd.state.CredentialKeyHex
	exported := &capturingAcker{}
	active.cmd.handleExportCredentialKey(exported, "export-1", &pb.ExportCredentialKey{RecipientPublicKey: standby.publicKey}, discardLogger(t))
	sealed := credentialKeyResult(t, singleAck(t, exported)).GetSealedKey()
	// A regular file where the state directory should be makes SaveState fail
	// regardless of the user running the tests.
	blocker := filepath.Join(t.TempDir(), "not-a-dir")
	require.NoError(t, os.WriteFile(blocker, nil, 0o600))
	standby.cmd.statePath = filepath.Join(blocker, "state.yaml")
	stream := &capturingAcker{}

	// Act
	standby.cmd.handleInstallCredentialKey(stream, "install-1", &pb.InstallCredentialKey{SealedKey: sealed}, discardLogger(t))

	// Assert
	ack := singleAck(t, stream)
	assert.Equal(t, pb.AckCode_ACK_CODE_INTERNAL, ack.GetCode())
	assert.False(t, ack.GetSucceeded())
	assert.Contains(t, ack.GetErrorMessage(), "persist credential key")
	assert.Empty(t, ack.GetPayload())
	assert.Equal(t, originalHex, standby.cmd.state.CredentialKeyHex, "in-memory state key must be restored")
	assert.Equal(t, originalKey, standby.cmd.credentials.currentKey(), "codec key must not be swapped when the write failed")
	assert.Equal(t, originalHex, standby.persistedKeyHex(t), "the original state file must be untouched")
}

func TestCredentialCodec_ReplaceKeyChangesWhichCredentialsOpen(t *testing.T) {
	// Arrange
	first := newHANode(t).cmd.credentials
	second := newHANode(t).cmd.credentials
	sealed, err := first.Seal(sdk.SecretBundle{Version: "v1", Kind: sdk.UsernamePassword{Username: "u", Password: "p"}})
	require.NoError(t, err)
	_, err = second.Open(sealed)
	require.Error(t, err)

	// Act
	second.replaceKey(first.currentKey())

	// Assert
	bundle, err := second.Open(sealed)
	require.NoError(t, err)
	assert.Equal(t, sdk.UsernamePassword{Username: "u", Password: "p"}, bundle.Kind)
}

func TestCredentialCodec_CurrentKeyReturnsCopy(t *testing.T) {
	// Arrange
	codec := newHANode(t).cmd.credentials
	original := codec.currentKey()

	// Act
	codec.currentKey()[0] ^= 0xff

	// Assert
	assert.Equal(t, original, codec.currentKey())
}

func TestCredentialCodec_ReplaceKeyCopiesArgument(t *testing.T) {
	// Arrange
	codec := newHANode(t).cmd.credentials
	replacement := make([]byte, credentialKeySize)

	// Act
	codec.replaceKey(replacement)
	replacement[0] ^= 0xff

	// Assert
	assert.Equal(t, make([]byte, credentialKeySize), codec.currentKey())
}

func TestCredentialCodec_ConcurrentSealAndReplaceKey(t *testing.T) {
	// Arrange
	codec := newHANode(t).cmd.credentials
	var wg sync.WaitGroup

	// Act
	for range 4 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for range 50 {
				_, _ = codec.Seal(sdk.SecretBundle{Version: "v1", Kind: sdk.UsernamePassword{Username: "u", Password: "p"}})
			}
		}()
		go func() {
			defer wg.Done()
			for range 50 {
				codec.replaceKey(make([]byte, credentialKeySize))
			}
		}()
	}
	wg.Wait()

	// Assert
	assert.Len(t, codec.currentKey(), credentialKeySize)
}

// pingRecorder is a concurrency-safe acker that records what the keepalive
// goroutines send.
type pingRecorder struct {
	mu   sync.Mutex
	sent []*pb.ControlStreamRequest
	err  error
}

func (a *pingRecorder) Send(req *pb.ControlStreamRequest) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.sent = append(a.sent, req)
	return a.err
}

func (a *pingRecorder) pings() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	n := 0
	for _, req := range a.sent {
		if req.GetPing() != nil {
			n++
		}
	}
	return n
}

func (a *pingRecorder) total() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.sent)
}

// feedReceived simulates server traffic by signaling received every period
// until ctx ends.
func feedReceived(ctx context.Context, received chan<- struct{}, period time.Duration) {
	go func() {
		ticker := time.NewTicker(period)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				select {
				case received <- struct{}{}:
				default:
				}
			}
		}
	}()
}

func TestRunControlKeepalive_SendsPingsEveryInterval(t *testing.T) {
	// Arrange: server traffic keeps the watchdog from retiring the stream mid-test.
	sender := &pingRecorder{}
	received := make(chan struct{}, 1)
	streamCtx, cancelStream := context.WithCancelCause(t.Context())
	defer cancelStream(context.Canceled)
	feedReceived(t.Context(), received, 2*time.Millisecond)

	// Act
	runControlKeepalive(streamCtx, sender, received, 10*time.Millisecond, cancelStream)

	// Assert
	require.Eventually(t, func() bool { return sender.pings() >= 3 }, 5*time.Second, 5*time.Millisecond)
	assert.Equal(t, sender.pings(), sender.total(), "keepalive must only send pings")
}

func TestRunControlKeepalive_CancelsSilentStream(t *testing.T) {
	// Arrange
	const interval = 20 * time.Millisecond
	sender := &pingRecorder{}
	received := make(chan struct{}, 1)
	streamCtx, cancelStream := context.WithCancelCause(t.Context())
	defer cancelStream(context.Canceled)
	start := time.Now()

	// Act
	runControlKeepalive(streamCtx, sender, received, interval, cancelStream)

	// Assert
	select {
	case <-streamCtx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("silent stream was not canceled")
	}
	assert.ErrorIs(t, context.Cause(streamCtx), errControlStreamSilent)
	assert.GreaterOrEqual(t, time.Since(start), fleetnodecontrol.KeepaliveMisses*interval, "must tolerate every allowed missed interval")
}

func TestRunControlKeepalive_ReceivedSignalsKeepStreamAlive(t *testing.T) {
	// Arrange: the 150ms deadline is far above the 10ms feed rate so scheduler
	// jitter cannot starve the feeder.
	const interval = 50 * time.Millisecond
	sender := &pingRecorder{}
	received := make(chan struct{}, 1)
	streamCtx, cancelStream := context.WithCancelCause(t.Context())
	defer cancelStream(context.Canceled)
	feedCtx, stopFeed := context.WithCancel(t.Context())
	defer stopFeed()
	feedReceived(feedCtx, received, 10*time.Millisecond)

	// Act: feed server traffic for ten intervals, then go quiet.
	runControlKeepalive(streamCtx, sender, received, interval, cancelStream)
	select {
	case <-streamCtx.Done():
	case <-time.After(10 * interval):
	}
	causeWhileFed := context.Cause(streamCtx)
	pingsWhileFed := sender.pings()
	stopFeed()
	select {
	case <-streamCtx.Done():
	case <-time.After(5 * time.Second):
	}

	// Assert
	assert.NoError(t, causeWhileFed, "stream canceled despite server traffic")
	assert.GreaterOrEqual(t, pingsWhileFed, 1)
	assert.ErrorIs(t, context.Cause(streamCtx), errControlStreamSilent, "watchdog must fire once traffic stops")
}

func TestRunControlKeepalive_StopsWhenSessionEnds(t *testing.T) {
	// Arrange
	const interval = 10 * time.Millisecond
	sender := &pingRecorder{}
	received := make(chan struct{}, 1)
	streamCtx, cancelStream := context.WithCancelCause(t.Context())
	defer cancelStream(context.Canceled)
	sessionCtx, endSession := context.WithCancel(streamCtx)
	feedCtx, stopFeed := context.WithCancel(t.Context())
	defer stopFeed()
	feedReceived(feedCtx, received, time.Millisecond)
	runControlKeepalive(sessionCtx, sender, received, interval, cancelStream)
	require.Eventually(t, func() bool { return sender.pings() >= 1 }, 5*time.Second, time.Millisecond)

	// Act: with the feed stopped, a surviving watchdog would fire after 3 intervals.
	endSession()
	stopFeed()
	time.Sleep(5 * interval) // let any in-flight tick drain
	settled := sender.total()
	time.Sleep(10 * interval)

	// Assert
	assert.Equal(t, settled, sender.total(), "no pings after the session ended")
	assert.NoError(t, streamCtx.Err(), "watchdog must not retire the stream once the session ended")
}

func TestRunControlKeepalive_StopsPingingAfterSendFailure(t *testing.T) {
	// Arrange
	const interval = 10 * time.Millisecond
	sender := &pingRecorder{err: errControlSenderClosed}
	received := make(chan struct{}, 1)
	streamCtx, cancelStream := context.WithCancelCause(t.Context())
	defer cancelStream(context.Canceled)

	// Act
	runControlKeepalive(streamCtx, sender, received, interval, cancelStream)
	require.Eventually(t, func() bool { return sender.total() >= 1 }, 5*time.Second, time.Millisecond)
	time.Sleep(5 * interval)

	// Assert
	assert.Equal(t, 1, sender.total())
}

// keepaliveControlGateway accepts ControlStreams with a keepalive interval,
// then stays silent so the node's watchdog has to retire the stream.
type keepaliveControlGateway struct {
	fleetnodegatewayv1connect.UnimplementedFleetNodeGatewayServiceHandler

	acceptedInterval time.Duration
	helloVersion     atomic.Int32
	pings            atomic.Int32
}

func (f *keepaliveControlGateway) ControlStream(ctx context.Context, stream *connect.BidiStream[pb.ControlStreamRequest, pb.ControlStreamResponse]) error {
	hello, err := stream.Receive()
	if err != nil {
		return fmt.Errorf("recv hello: %w", err)
	}
	f.helloVersion.Store(int32(hello.GetHello().GetMaxCommandProtocolVersion()))
	accepted := &pb.ControlAccepted{ServerTime: timestamppb.Now(), KeepaliveInterval: durationpb.New(f.acceptedInterval)}
	if err := stream.Send(&pb.ControlStreamResponse{Kind: &pb.ControlStreamResponse_Accepted{Accepted: accepted}}); err != nil {
		return fmt.Errorf("send accepted: %w", err)
	}
	for ctx.Err() == nil {
		msg, err := stream.Receive()
		if err != nil {
			return nil //nolint:nilerr // the node closing the stream is the expected end
		}
		if msg.GetPing() != nil {
			f.pings.Add(1)
		}
	}
	return nil
}

func TestRunControlSession_NegotiatesKeepaliveAndRetiresSilentStream(t *testing.T) {
	// Arrange: a sub-minimum interval must be clamped to minControlKeepaliveInterval,
	// so the 3-miss deadline is 3s rather than 3ms.
	fake := &keepaliveControlGateway{acceptedInterval: time.Millisecond}
	mux := http.NewServeMux()
	path, handler := fleetnodegatewayv1connect.NewFleetNodeGatewayServiceHandler(fake)
	mux.Handle(path, handler)
	srv := testutil.NewH2CServer(t, mux)
	client := fleetnodegatewayv1connect.NewFleetNodeGatewayServiceClient(testutil.NewH2CClient(), srv.URL, connect.WithGRPC())
	cmd := &RunCmd{discoverer: &stubDiscoverer{}}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	start := time.Now()

	// Act
	err := cmd.runControlSession(ctx, discardLogger(t), client, &bootstrap.State{FleetNodeID: 7})

	// Assert
	require.Error(t, err)
	assert.ErrorIs(t, err, errControlStreamSilent)
	assert.GreaterOrEqual(t, time.Since(start), fleetnodecontrol.KeepaliveMisses*minControlKeepaliveInterval)
	assert.Equal(t, int32(pb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V6), fake.helloVersion.Load())
	assert.GreaterOrEqual(t, fake.pings.Load(), int32(1), "node must ping at the negotiated interval")
	assert.NoError(t, ctx.Err(), "the caller's context must still be live")
}
