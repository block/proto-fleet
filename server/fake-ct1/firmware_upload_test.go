package main

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/block/proto-fleet/sdk/mdk/firmware/firmwaretest"
	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	"github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2/mdkv2connect"
)

func TestChunkedFirmwareConnect(t *testing.T) {
	server, state := newConnectTestServer(t)
	client := mdkv2connect.NewFirmwareServiceClient(http.DefaultClient, server.URL)
	ctx := t.Context()
	data := firmwaretest.Image(t, "prod", "f1", 9)
	meta := &mdk.FirmwareUploadMetadata{Source: &mdk.FirmwareArtifactRef{Source: mdk.FirmwareArtifactSource_FIRMWARE_ARTIFACT_SOURCE_RELEASE, ReleaseTag: "9.0.0", AssetName: "f1-app-prod.bin"}, Images: []*mdk.FirmwareUploadImage{{Name: "f1-app-prod.bin", SizeBytes: int64(len(data)), Sha256: fmt.Sprintf("%x", sha256.Sum256(data))}}}
	begin := func() string {
		t.Helper()
		response, err := client.BeginFirmwareArtifactUpload(ctx, connect.NewRequest(&mdk.BeginFirmwareArtifactUploadRequest{Metadata: meta}))
		if err != nil {
			t.Fatal(err)
		}
		if response.Msg.MaxChunkBytes != 1<<20 {
			t.Fatal(response.Msg)
		}
		return response.Msg.UploadId
	}
	id := begin()
	write := func(offset int64, chunk []byte) (*connect.Response[mdk.WriteFirmwareArtifactUploadChunkResponse], error) {
		return client.WriteFirmwareArtifactUploadChunk(ctx, connect.NewRequest(&mdk.WriteFirmwareArtifactUploadChunkRequest{UploadId: id, Offset: offset, Data: chunk}))
	}
	if _, err := write(1, data[:1]); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("gap: %v", err)
	}
	if _, err := write(0, make([]byte, (1<<20)+1)); connect.CodeOf(err) != connect.CodeResourceExhausted {
		t.Fatalf("oversize: %v", err)
	}
	if _, err := write(0, nil); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("empty: %v", err)
	}
	if _, err := client.CommitFirmwareArtifactUpload(ctx, connect.NewRequest(&mdk.CommitFirmwareArtifactUploadRequest{UploadId: id})); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("incomplete: %v", err)
	}
	wrong := append([]byte(nil), data...)
	wrong[0] ^= 1
	if _, err := write(0, wrong); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("checksum: %v", err)
	}
	// Digest failure resets the image. An overlapping retry skips received bytes.
	if _, err := write(0, data[:100]); err != nil {
		t.Fatal(err)
	}
	response, err := write(0, data[:100])
	if err != nil || response.Msg.NextOffset != 100 {
		t.Fatalf("retry: %v %v", response, err)
	}
	response, err = write(50, data[50:])
	if err != nil || response.Msg.NextOffset != int64(len(data)) {
		t.Fatalf("overlap: %v %v", response, err)
	}
	committed, err := client.CommitFirmwareArtifactUpload(ctx, connect.NewRequest(&mdk.CommitFirmwareArtifactUploadRequest{UploadId: id}))
	if err != nil {
		t.Fatal(err)
	}
	if committed.Msg.Images[0].Version != "9.0.0" {
		t.Fatal(committed.Msg)
	}
	if _, err := write(0, data); connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("consumed session: %v", err)
	}
	var macs []string
	for mac := range state.boardsByMAC {
		macs = append(macs, mac)
	}
	started, err := client.StartFirmwareUpdate(ctx, connect.NewRequest(&mdk.StartFirmwareUpdateRequest{TaskId: "browser-task", Artifact: committed.Msg.Artifact, Scope: &mdk.BoardScope{Identifiers: macs}, Options: &mdk.FirmwareUpdateOptions{Build: "prod", Force: true}}))
	if err != nil || started.Msg.Update.TaskId != "browser-task" {
		t.Fatalf("start: %v %v", started, err)
	}
	got, err := client.GetFirmwareUpdate(ctx, connect.NewRequest(&mdk.GetFirmwareUpdateRequest{TaskId: "browser-task", ServerId: state.ServerID}))
	if err != nil || len(got.Msg.Update.Targets) != len(macs) {
		t.Fatalf("get: %v %v", got, err)
	}
	listed, err := client.ListFirmwareUpdates(ctx, connect.NewRequest(&mdk.ListFirmwareUpdatesRequest{}))
	if err != nil || len(listed.Msg.Updates) != 1 {
		t.Fatalf("list: %v %v", listed, err)
	}
	canceled, err := client.CancelFirmwareUpdate(ctx, connect.NewRequest(&mdk.CancelFirmwareUpdateRequest{TaskId: "browser-task", ServerId: state.ServerID}))
	if err != nil || canceled.Msg.Update.Status != mdk.FirmwareUpdateStatus_FIRMWARE_UPDATE_STATUS_CANCELED {
		t.Fatalf("cancel: %v %v", canceled, err)
	}

	id = begin()
	second := begin()
	if _, err := client.BeginFirmwareArtifactUpload(ctx, connect.NewRequest(&mdk.BeginFirmwareArtifactUploadRequest{Metadata: meta})); connect.CodeOf(err) != connect.CodeResourceExhausted {
		t.Fatalf("limit: %v", err)
	}
	for range 2 {
		if _, err := client.AbortFirmwareArtifactUpload(ctx, connect.NewRequest(&mdk.AbortFirmwareArtifactUploadRequest{UploadId: second})); err != nil {
			t.Fatal(err)
		}
	}
	state.mu.Lock()
	state.StartTime = state.StartTime.Add(11 * time.Minute)
	state.mu.Unlock()
	if _, err := write(0, data); connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("expired: %v", err)
	}
	begin()
}
