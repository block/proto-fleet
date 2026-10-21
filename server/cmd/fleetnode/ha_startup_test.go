package main

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"
	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1/fleetnodegatewayv1connect"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/keytransfer"
	"github.com/block/proto-fleet/server/internal/fleetnode/bootstrap"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type startupProvisioningGateway struct {
	fleetnodegatewayv1connect.UnimplementedFleetNodeGatewayServiceHandler
	transfer    *pb.HAKeyTransfer
	fingerprint []byte
	marker      string
	sessions    atomic.Int32
	checked     chan error
}

func (g *startupProvisioningGateway) ControlStream(ctx context.Context, stream *connect.BidiStream[pb.ControlStreamRequest, pb.ControlStreamResponse]) error {
	hello, err := stream.Receive()
	if err != nil {
		return fmt.Errorf("receive hello: %w", err)
	}
	session := g.sessions.Add(1)
	preparing := session == 1
	if preparing == bytes.Equal(hello.GetHello().GetCredentialKeyFingerprint(), g.fingerprint) {
		g.checked <- fmt.Errorf("unexpected installed fingerprint on session %d", session)
		return fmt.Errorf("unexpected fingerprint")
	}
	if err := stream.Send(&pb.ControlStreamResponse{Kind: &pb.ControlStreamResponse_Accepted{Accepted: &pb.ControlAccepted{ControlSessionId: "session", HaOwnerNodeId: 2, OrgId: 1, Preparing: preparing, CredentialKeyFingerprint: g.fingerprint}}}); err != nil {
		return fmt.Errorf("send accepted: %w", err)
	}
	if !preparing {
		<-ctx.Done()
		return nil
	}
	ordinary, err := proto.Marshal(&pb.AgentCommand{Command: &pb.AgentCommand_Telemetry{Telemetry: validTelemetryRequest()}})
	if err != nil {
		return fmt.Errorf("encode ordinary command: %w", err)
	}
	if err := stream.Send(&pb.ControlStreamResponse{Kind: &pb.ControlStreamResponse_Command{Command: &pb.ControlCommand{CommandId: "ordinary", Payload: ordinary}}}); err != nil {
		return fmt.Errorf("send ordinary command: %w", err)
	}
	response, err := stream.Receive()
	if err != nil {
		return fmt.Errorf("receive ordinary command refusal: %w", err)
	}
	if response.GetAck().GetCode() != pb.AckCode_ACK_CODE_HA_NOT_AUTHORIZED {
		g.checked <- fmt.Errorf("preparing node accepted ordinary work or sent a lease: %v", response)
		return fmt.Errorf("unexpected preparing response")
	}
	if _, err := os.Stat(g.marker); !os.IsNotExist(err) {
		g.checked <- fmt.Errorf("plugin started before key installation: %v", err)
		return fmt.Errorf("early plugin startup")
	}
	g.checked <- nil
	payload, err := proto.Marshal(&pb.AgentCommand{Command: &pb.AgentCommand_HaKeyTransfer{HaKeyTransfer: g.transfer}})
	if err != nil {
		return fmt.Errorf("encode key transfer: %w", err)
	}
	if err := stream.Send(&pb.ControlStreamResponse{Kind: &pb.ControlStreamResponse_Command{Command: &pb.ControlCommand{CommandId: "install", Payload: payload, IntendedInstanceId: hello.GetHello().GetInstanceId(), ControlSessionId: "session", HaOwnerNodeId: 2}}}); err != nil {
		return fmt.Errorf("send key transfer: %w", err)
	}
	// The receiver retires this stream after its durable install, including when
	// its final acknowledgement is lost. The replacement Hello proves the key.
	for {
		if _, err := stream.Receive(); err != nil {
			return fmt.Errorf("receive after installation: %w", err)
		}
	}
}

func TestRunLockedPreparesAndReconnectsBeforePluginStartup(t *testing.T) {
	stateDir := t.TempDir()
	st := freshState(t, stateDir, time.Now().Add(24*time.Hour))
	recipient, err := ecdh.X25519().GenerateKey(rand.Reader)
	require.NoError(t, err)
	st.EncryptionPrivateKeyHex = hex.EncodeToString(recipient.Bytes())
	st.EncryptionPublicKeyHex = hex.EncodeToString(recipient.PublicKey().Bytes())
	require.NoError(t, bootstrap.SaveState(bootstrap.StatePath(stateDir), st))
	key := bytes.Repeat([]byte{9}, credentialKeySize)
	fingerprint := sha256.Sum256(key)
	expires := time.Now().Add(time.Minute)
	metadata := keytransfer.Metadata{OperationID: "install", OrgID: 1, OwnerNodeID: 2, SourceNodeID: 2, RecipientNodeID: st.FleetNodeID, RecipientPublicKey: recipient.PublicKey().Bytes(), Fingerprint: fingerprint[:], ExpiresAt: expires.UnixNano()}
	encrypted, err := keytransfer.Encrypt(metadata, key, time.Now())
	require.NoError(t, err)
	pluginsDir := t.TempDir()
	marker := filepath.Join(t.TempDir(), "plugin-started")
	// The plugin intentionally fails after recording that startup was attempted.
	require.NoError(t, os.WriteFile(filepath.Join(pluginsDir, "bad-plugin"), []byte("#!/bin/sh\ntouch '"+marker+"'\nexit 1\n"), 0o755))
	gateway := &startupProvisioningGateway{fingerprint: fingerprint[:], marker: marker, checked: make(chan error, 2), transfer: &pb.HAKeyTransfer{OperationId: "install", OrgId: 1, OwnerNodeId: 2, SourceNodeId: 2, RecipientNodeId: st.FleetNodeID, RecipientPublicKey: recipient.PublicKey().Bytes(), CredentialKeyFingerprint: fingerprint[:], ExpiresAt: timestamppb.New(expires), EncryptedKey: encrypted}}
	mux := http.NewServeMux()
	path, handler := fleetnodegatewayv1connect.NewFleetNodeGatewayServiceHandler(gateway)
	mux.Handle(path, handler)
	server := testutil.NewH2CServer(t, mux)
	client := fleetnodegatewayv1connect.NewFleetNodeGatewayServiceClient(testutil.NewH2CClient(), server.URL, connect.WithGRPC())
	node := &RunCmd{now: time.Now, HeartbeatInterval: defaultHeartbeatInterval, clientFactory: func(string, func() string) (gatewayClient, error) { return client, nil }}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	err = node.runLocked(ctx, &Context{StateDir: stateDir}, pluginsDir, discardLogger(t))
	require.ErrorIs(t, err, errRuntimeInitialization)
	require.ErrorContains(t, err, "bootstrap plugins")
	require.NoError(t, <-gateway.checked)
	require.EqualValues(t, 2, gateway.sessions.Load(), "installation must restart initialization automatically")
	require.FileExists(t, marker, "ready acceptance must allow normal plugin initialization")
	installed, exists, err := bootstrap.LoadState(bootstrap.StatePath(stateDir))
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, hex.EncodeToString(key), installed.CredentialKeyHex)
	require.EqualValues(t, 2, installed.HACredentialOwnerNodeID)
	require.Equal(t, st.IdentityPrivateKeyHex, installed.IdentityPrivateKeyHex)
	require.Equal(t, st.EncryptionPrivateKeyHex, installed.EncryptionPrivateKeyHex)
}
