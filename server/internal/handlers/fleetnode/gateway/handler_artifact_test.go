package gateway_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	"github.com/block/proto-fleet/server/internal/handlers/fleetnode/gateway"
	"github.com/block/proto-fleet/server/internal/infrastructure/files"
)

func sha256Hex(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func startAckOnlyCommandWithArtifacts(t *testing.T, h *controlHarness, commandID string, artifacts []control.ArtifactExpectation) (*control.Stream, chan error) {
	t.Helper()
	stream := h.registry.Register(h.fleetNodeID)
	done := make(chan error, 1)
	go func() {
		_, err := h.registry.SendCommandWithArtifacts(context.Background(), h.fleetNodeID, &pb.ControlCommand{CommandId: commandID}, artifacts)
		done <- err
	}()
	select {
	case got := <-stream.Outgoing:
		require.Equal(t, commandID, got.GetCommandId())
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for command %q to enqueue", commandID)
	}
	return stream, done
}

func finishAckOnlyCommand(t *testing.T, stream *control.Stream, commandID string, done <-chan error) {
	t.Helper()
	stream.PublishAck(&pb.ControlAck{
		CommandId: commandID,
		Succeeded: true,
		Code:      pb.AckCode_ACK_CODE_OK,
	})
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for command %q to finish", commandID)
	}
	stream.Unregister()
}

func TestCommandArtifactUploadAndDownloadRequireInFlightExpectation(t *testing.T) {
	t.Chdir(t.TempDir())
	registry := control.NewRegistry()
	filesService, err := files.NewService(files.Config{})
	require.NoError(t, err)
	h := &controlHarness{
		handler:     gateway.NewHandler(nil, nil, nil, registry, filesService),
		registry:    registry,
		fleetNodeID: 44,
	}
	client := startControlServer(t, h)
	payload := []byte("zipped miner logs")
	uploadCommandID := "upload-artifact-command"

	uploadStream, uploadDone := startAckOnlyCommandWithArtifacts(t, h, uploadCommandID, []control.ArtifactExpectation{{
		Direction:        control.ArtifactDirectionUpload,
		Purpose:          pb.CommandArtifactPurpose_COMMAND_ARTIFACT_PURPOSE_MINER_LOGS,
		DeviceIdentifier: "miner-a",
	}})

	up := client.UploadCommandArtifact(context.Background())
	require.NoError(t, up.Send(&pb.UploadCommandArtifactRequest{Part: &pb.UploadCommandArtifactRequest_Header{
		Header: &pb.CommandArtifactUploadHeader{
			CommandId:        uploadCommandID,
			Purpose:          pb.CommandArtifactPurpose_COMMAND_ARTIFACT_PURPOSE_MINER_LOGS,
			Filename:         "miner-a.zip",
			SizeBytes:        int64(len(payload)),
			Sha256:           sha256Hex(payload),
			DeviceIdentifier: "miner-a",
		},
	}}))
	require.NoError(t, up.Send(&pb.UploadCommandArtifactRequest{Part: &pb.UploadCommandArtifactRequest_Chunk{
		Chunk: &pb.CommandArtifactChunk{Data: payload},
	}}))
	uploadResp, err := up.CloseAndReceive()
	require.NoError(t, err)
	artifact := uploadResp.Msg.GetArtifact()
	require.NotNil(t, artifact)
	assert.Equal(t, pb.CommandArtifactPurpose_COMMAND_ARTIFACT_PURPOSE_MINER_LOGS, artifact.GetPurpose())
	assert.Equal(t, "miner-a.zip", artifact.GetFilename())
	assert.Equal(t, int64(len(payload)), artifact.GetSizeBytes())
	assert.Equal(t, sha256Hex(payload), artifact.GetSha256())
	finishAckOnlyCommand(t, uploadStream, uploadCommandID, uploadDone)

	duplicate := client.UploadCommandArtifact(context.Background())
	require.NoError(t, duplicate.Send(&pb.UploadCommandArtifactRequest{Part: &pb.UploadCommandArtifactRequest_Header{
		Header: &pb.CommandArtifactUploadHeader{
			CommandId:        uploadCommandID,
			Purpose:          pb.CommandArtifactPurpose_COMMAND_ARTIFACT_PURPOSE_MINER_LOGS,
			Filename:         "miner-a.zip",
			SizeBytes:        int64(len(payload)),
			Sha256:           sha256Hex(payload),
			DeviceIdentifier: "miner-a",
		},
	}}))
	_, err = duplicate.CloseAndReceive()
	require.Error(t, err)
	assert.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))

	downloadCommandID := "download-artifact-command"
	downloadStream, downloadDone := startAckOnlyCommandWithArtifacts(t, h, downloadCommandID, []control.ArtifactExpectation{{
		Direction:        control.ArtifactDirectionDownload,
		Purpose:          pb.CommandArtifactPurpose_COMMAND_ARTIFACT_PURPOSE_MINER_LOGS,
		ArtifactID:       artifact.GetArtifactId(),
		DeviceIdentifier: "miner-a",
	}})
	download, err := client.DownloadCommandArtifact(context.Background(), connect.NewRequest(&pb.DownloadCommandArtifactRequest{
		CommandId:        downloadCommandID,
		Artifact:         artifact,
		DeviceIdentifier: "miner-a",
	}))
	require.NoError(t, err)
	defer download.Close()

	var got bytes.Buffer
	var header *pb.CommandArtifactRef
	for download.Receive() {
		msg := download.Msg()
		if h := msg.GetHeader(); h != nil {
			header = h.GetArtifact()
			continue
		}
		_, err := got.Write(msg.GetChunk().GetData())
		require.NoError(t, err)
	}
	require.NoError(t, download.Err())
	require.NotNil(t, header)
	assert.Equal(t, artifact.GetArtifactId(), header.GetArtifactId())
	assert.Equal(t, payload, got.Bytes())

	duplicateDownload, err := client.DownloadCommandArtifact(context.Background(), connect.NewRequest(&pb.DownloadCommandArtifactRequest{
		CommandId:        downloadCommandID,
		Artifact:         artifact,
		DeviceIdentifier: "miner-a",
	}))
	require.NoError(t, err)
	require.False(t, duplicateDownload.Receive())
	require.Error(t, duplicateDownload.Err())
	assert.Equal(t, connect.CodeAlreadyExists, connect.CodeOf(duplicateDownload.Err()))
	finishAckOnlyCommand(t, downloadStream, downloadCommandID, downloadDone)

	badDownload, err := client.DownloadCommandArtifact(context.Background(), connect.NewRequest(&pb.DownloadCommandArtifactRequest{
		CommandId:        "not-in-flight",
		Artifact:         artifact,
		DeviceIdentifier: "miner-a",
	}))
	require.NoError(t, err)
	require.False(t, badDownload.Receive())
	require.Error(t, badDownload.Err())
	assert.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(badDownload.Err()))
}
