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
	"github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1/fleetnodegatewayv1connect"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	"github.com/block/proto-fleet/server/internal/domain/miner/logformat"
	"github.com/block/proto-fleet/server/internal/handlers/fleetnode/gateway"
	"github.com/block/proto-fleet/server/internal/infrastructure/files"
)

func sha256Hex(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func newArtifactTestClient(t *testing.T, opts ...connect.HandlerOption) (*controlHarness, fleetnodegatewayv1connect.FleetNodeGatewayServiceClient) {
	t.Helper()
	t.Chdir(t.TempDir())
	registry := control.NewRegistry()
	filesService, err := files.NewService(files.Config{})
	require.NoError(t, err)
	h := &controlHarness{
		handler:     gateway.NewHandler(nil, nil, nil, registry, filesService),
		registry:    registry,
		files:       filesService,
		fleetNodeID: 44,
	}
	return h, startControlServer(t, h, opts...)
}

func uploadExpectation() control.ArtifactExpectation {
	return control.ArtifactExpectation{
		Direction:        control.ArtifactDirectionUpload,
		Purpose:          pb.CommandArtifactPurpose_COMMAND_ARTIFACT_PURPOSE_MINER_LOGS,
		DeviceIdentifier: "miner-a",
		MaxSizeBytes:     logformat.MaxArtifactBytes,
	}
}

func downloadExpectation(artifact *pb.CommandArtifactRef) control.ArtifactExpectation {
	return control.ArtifactExpectation{
		Direction:        control.ArtifactDirectionDownload,
		Purpose:          pb.CommandArtifactPurpose_COMMAND_ARTIFACT_PURPOSE_MINER_LOGS,
		ArtifactID:       artifact.GetArtifactId(),
		DeviceIdentifier: "miner-a",
	}
}

func firmwareDownloadExpectation(artifact *pb.CommandArtifactRef) control.ArtifactExpectation {
	return control.ArtifactExpectation{
		Direction:        control.ArtifactDirectionDownload,
		Purpose:          pb.CommandArtifactPurpose_COMMAND_ARTIFACT_PURPOSE_FIRMWARE_PAYLOAD,
		ArtifactID:       artifact.GetArtifactId(),
		DeviceIdentifier: "miner-a",
	}
}

func uploadHeaderRequest(commandID string, payload []byte) *pb.UploadCommandArtifactRequest {
	return &pb.UploadCommandArtifactRequest{Part: &pb.UploadCommandArtifactRequest_Header{
		Header: &pb.CommandArtifactUploadHeader{
			CommandId:        commandID,
			Purpose:          pb.CommandArtifactPurpose_COMMAND_ARTIFACT_PURPOSE_MINER_LOGS,
			Filename:         "miner-a.zip",
			SizeBytes:        int64(len(payload)),
			Sha256:           sha256Hex(payload),
			DeviceIdentifier: "miner-a",
		},
	}}
}

func oversizedUploadHeaderRequest(commandID string) *pb.UploadCommandArtifactRequest {
	req := uploadHeaderRequest(commandID, []byte("x"))
	req.GetHeader().SizeBytes = logformat.MaxArtifactBytes + 1
	return req
}

func uploadChunkRequest(payload []byte) *pb.UploadCommandArtifactRequest {
	return &pb.UploadCommandArtifactRequest{Part: &pb.UploadCommandArtifactRequest_Chunk{
		Chunk: &pb.CommandArtifactChunk{Data: payload},
	}}
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
	h, client := newArtifactTestClient(t)
	payload := []byte("zipped miner logs")
	uploadCommandID := "upload-artifact-command"

	uploadStream, uploadDone := startAckOnlyCommandWithArtifacts(t, h, uploadCommandID, []control.ArtifactExpectation{uploadExpectation()})

	up := client.UploadCommandArtifact(context.Background())
	require.NoError(t, up.Send(uploadHeaderRequest(uploadCommandID, payload)))
	require.NoError(t, up.Send(uploadChunkRequest(payload)))
	uploadResp, err := up.CloseAndReceive()
	require.NoError(t, err)
	artifact := uploadResp.Msg.GetArtifact()
	require.NotNil(t, artifact)
	assert.Equal(t, pb.CommandArtifactPurpose_COMMAND_ARTIFACT_PURPOSE_MINER_LOGS, artifact.GetPurpose())
	assert.Equal(t, "miner-a.zip", artifact.GetFilename())
	assert.Equal(t, int64(len(payload)), artifact.GetSizeBytes())
	assert.Equal(t, sha256Hex(payload), artifact.GetSha256())

	completedRetry := client.UploadCommandArtifact(context.Background())
	require.NoError(t, completedRetry.Send(uploadHeaderRequest(uploadCommandID, payload)))
	require.NoError(t, completedRetry.Send(uploadChunkRequest(payload)))
	completedRetryResp, err := completedRetry.CloseAndReceive()
	require.NoError(t, err)
	assert.Equal(t, artifact.GetArtifactId(), completedRetryResp.Msg.GetArtifact().GetArtifactId())

	finishAckOnlyCommand(t, uploadStream, uploadCommandID, uploadDone)

	duplicate := client.UploadCommandArtifact(context.Background())
	duplicateSendErr := duplicate.Send(uploadHeaderRequest(uploadCommandID, payload))
	if duplicateSendErr != nil {
		require.ErrorContains(t, duplicateSendErr, "EOF")
	} else {
		_, err = duplicate.CloseAndReceive()
		require.Error(t, err)
		assert.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))
	}

	downloadCommandID := "download-artifact-command"
	downloadStream, downloadDone := startAckOnlyCommandWithArtifacts(t, h, downloadCommandID, []control.ArtifactExpectation{downloadExpectation(artifact)})
	staleRef := &pb.CommandArtifactRef{
		ArtifactId: artifact.GetArtifactId(),
		Purpose:    artifact.GetPurpose(),
		Filename:   artifact.GetFilename(),
		SizeBytes:  artifact.GetSizeBytes() + 1,
		Sha256:     artifact.GetSha256(),
	}
	staleDownload, err := client.DownloadCommandArtifact(context.Background(), connect.NewRequest(&pb.DownloadCommandArtifactRequest{
		CommandId:        downloadCommandID,
		Artifact:         staleRef,
		DeviceIdentifier: "miner-a",
	}))
	require.NoError(t, err)
	require.False(t, staleDownload.Receive())
	require.Error(t, staleDownload.Err())
	assert.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(staleDownload.Err()))

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
	defer duplicateDownload.Close()
	got.Reset()
	for duplicateDownload.Receive() {
		msg := duplicateDownload.Msg()
		if msg.GetHeader() != nil {
			continue
		}
		_, err := got.Write(msg.GetChunk().GetData())
		require.NoError(t, err)
	}
	require.NoError(t, duplicateDownload.Err())
	assert.Equal(t, payload, got.Bytes())
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

func TestDownloadCommandArtifactServesFirmwarePayload(t *testing.T) {
	h, client := newArtifactTestClient(t)
	payload := []byte("firmware image bytes")
	fileID, err := h.files.SaveFirmwareFile("update.swu", bytes.NewReader(payload), files.FirmwareMetadata{
		TargetManufacturer: "Proto",
		TargetModel:        "S21",
		FirmwareVersion:    "1.2.3",
	})
	require.NoError(t, err)
	_, info, err := h.files.OpenFirmwareFileWithInfo(fileID)
	require.NoError(t, err)
	ref := &pb.CommandArtifactRef{
		ArtifactId: info.ID,
		Purpose:    pb.CommandArtifactPurpose_COMMAND_ARTIFACT_PURPOSE_FIRMWARE_PAYLOAD,
		Filename:   info.Filename,
		SizeBytes:  info.Size,
		Sha256:     info.SHA256,
	}

	commandID := "download-firmware-command"
	stream, done := startAckOnlyCommandWithArtifacts(t, h, commandID, []control.ArtifactExpectation{firmwareDownloadExpectation(ref)})
	download, err := client.DownloadCommandArtifact(context.Background(), connect.NewRequest(&pb.DownloadCommandArtifactRequest{
		CommandId:        commandID,
		Artifact:         ref,
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
	assert.Equal(t, ref.GetArtifactId(), header.GetArtifactId())
	assert.Equal(t, ref.GetPurpose(), header.GetPurpose())
	assert.Equal(t, ref.GetFilename(), header.GetFilename())
	assert.Equal(t, ref.GetSizeBytes(), header.GetSizeBytes())
	assert.Equal(t, ref.GetSha256(), header.GetSha256())
	assert.Equal(t, payload, got.Bytes())
	finishAckOnlyCommand(t, stream, commandID, done)
}

func TestCommandArtifactUploadTimeoutReleasesSlotAndAllowsRetry(t *testing.T) {
	oldHeaderTimeout := gateway.CommandArtifactUploadHeaderTimeout
	oldChunkTimeout := gateway.CommandArtifactUploadChunkTimeout
	oldTotalTimeout := gateway.CommandArtifactUploadTotalTimeout
	gateway.CommandArtifactUploadHeaderTimeout = time.Second
	gateway.CommandArtifactUploadChunkTimeout = 10 * time.Millisecond
	gateway.CommandArtifactUploadTotalTimeout = time.Second
	t.Cleanup(func() {
		gateway.CommandArtifactUploadHeaderTimeout = oldHeaderTimeout
		gateway.CommandArtifactUploadChunkTimeout = oldChunkTimeout
		gateway.CommandArtifactUploadTotalTimeout = oldTotalTimeout
	})

	h, client := newArtifactTestClient(t)
	payload := []byte("zipped miner logs")
	commandID := "stalled-upload-command"
	uploadStream, uploadDone := startAckOnlyCommandWithArtifacts(t, h, commandID, []control.ArtifactExpectation{uploadExpectation()})

	stalled := client.UploadCommandArtifact(context.Background())
	require.NoError(t, stalled.Send(uploadHeaderRequest(commandID, payload)))

	time.Sleep(5 * gateway.CommandArtifactUploadChunkTimeout)
	_, err := stalled.CloseAndReceive()
	require.Error(t, err)
	assert.Equal(t, connect.CodeDeadlineExceeded, connect.CodeOf(err))

	retry := client.UploadCommandArtifact(context.Background())
	require.NoError(t, retry.Send(uploadHeaderRequest(commandID, payload)))
	require.NoError(t, retry.Send(uploadChunkRequest(payload)))
	uploadResp, err := retry.CloseAndReceive()
	require.NoError(t, err)
	require.NotNil(t, uploadResp.Msg.GetArtifact())

	finishAckOnlyCommand(t, uploadStream, commandID, uploadDone)
}

func TestCommandArtifactUploadReadLimitRejectsOversizedMessageBeforeChunkReader(t *testing.T) {
	h, client := newArtifactTestClient(t, gateway.CommandArtifactUploadReadLimitOption())
	payload := []byte("zipped miner logs")
	commandID := "oversized-upload-message-command"
	uploadStream, uploadDone := startAckOnlyCommandWithArtifacts(t, h, commandID, []control.ArtifactExpectation{uploadExpectation()})

	oversized := client.UploadCommandArtifact(context.Background())
	require.NoError(t, oversized.Send(uploadHeaderRequest(commandID, payload)))
	err := oversized.Send(uploadChunkRequest(bytes.Repeat([]byte("x"), gateway.CommandArtifactUploadReadMaxBytes)))
	if err == nil {
		_, err = oversized.CloseAndReceive()
	}
	require.Error(t, err)
	assert.Equal(t, connect.CodeResourceExhausted, connect.CodeOf(err))

	retry := client.UploadCommandArtifact(context.Background())
	require.NoError(t, retry.Send(uploadHeaderRequest(commandID, payload)))
	require.NoError(t, retry.Send(uploadChunkRequest(payload)))
	uploadResp, err := retry.CloseAndReceive()
	require.NoError(t, err)
	require.NotNil(t, uploadResp.Msg.GetArtifact())

	finishAckOnlyCommand(t, uploadStream, commandID, uploadDone)
}

func TestCommandArtifactUploadRejectsMinerLogsOverExpectedSize(t *testing.T) {
	h, client := newArtifactTestClient(t)
	commandID := "oversized-miner-log-artifact-command"
	uploadStream, uploadDone := startAckOnlyCommandWithArtifacts(t, h, commandID, []control.ArtifactExpectation{uploadExpectation()})

	oversized := client.UploadCommandArtifact(context.Background())
	err := oversized.Send(oversizedUploadHeaderRequest(commandID))
	if err == nil {
		_, err = oversized.CloseAndReceive()
	}
	require.Error(t, err)
	assert.Equal(t, connect.CodeResourceExhausted, connect.CodeOf(err))

	payload := []byte("bounded miner logs")
	retry := client.UploadCommandArtifact(context.Background())
	require.NoError(t, retry.Send(uploadHeaderRequest(commandID, payload)))
	require.NoError(t, retry.Send(uploadChunkRequest(payload)))
	uploadResp, err := retry.CloseAndReceive()
	require.NoError(t, err)
	require.NotNil(t, uploadResp.Msg.GetArtifact())

	finishAckOnlyCommand(t, uploadStream, commandID, uploadDone)
}
