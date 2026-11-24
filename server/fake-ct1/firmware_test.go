package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/block/proto-fleet/sdk/mdk/firmware/firmwaretest"
	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func TestFirmwareSimulatorStreamsAndReportsPartialFailure(t *testing.T) {
	s := NewContainerState(ContainerConfig{ServerID: "firmware-sim", Tanks: 1, ModulesPerTank: 1, SlotsPerModule: 3, Seed: 1})
	clock := time.Now()
	s.now = func() time.Time { return clock }
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := newGRPCServer(s)
	t.Cleanup(server.Stop)
	go func() { _ = server.Serve(l) }()
	conn, err := grpc.NewClient(l.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	client := mdk.NewFirmwareServiceClient(conn)
	data := firmwaretest.Image(t, "mfgtest", "f1", 9)
	stream, err := client.UploadFirmwareArtifact(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	meta := &mdk.FirmwareUploadMetadata{Source: &mdk.FirmwareArtifactRef{Source: mdk.FirmwareArtifactSource_FIRMWARE_ARTIFACT_SOURCE_RELEASE, ReleaseTag: "9.0.0", AssetName: "release.zip"}, Images: []*mdk.FirmwareUploadImage{{Name: "app.bin", SizeBytes: int64(len(data)), Sha256: fmt.Sprintf("%x", sha256.Sum256(data))}}}
	if err := stream.Send(&mdk.UploadFirmwareArtifactRequest{Payload: &mdk.UploadFirmwareArtifactRequest_Metadata{Metadata: meta}}); err != nil {
		t.Fatal(err)
	}
	if err := stream.Send(&mdk.UploadFirmwareArtifactRequest{Payload: &mdk.UploadFirmwareArtifactRequest_Chunk{Chunk: &mdk.FirmwareUploadChunk{Data: data}}}); err != nil {
		t.Fatal(err)
	}
	upload, err := stream.CloseAndRecv()
	if err != nil {
		t.Fatal(err)
	}
	var macs []string
	s.mu.Lock()
	for mac, board := range s.boardsByMAC {
		macs = append(macs, mac)
		board.State = miningStateMining
		board.Isolated = false
	}
	s.boardsByMAC[macs[0]].State = miningStateDisconnected
	s.mu.Unlock()
	job, err := client.StartFirmwareUpdate(t.Context(), &mdk.StartFirmwareUpdateRequest{Artifact: upload.Artifact, Scope: &mdk.BoardScope{Identifiers: macs}, Options: &mdk.FirmwareUpdateOptions{Build: "mfgtest"}})
	if err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	clock = clock.Add(5 * time.Second)
	s.mu.Unlock()
	result, err := client.GetFirmwareUpdate(t.Context(), &mdk.GetFirmwareUpdateRequest{TaskId: job.Update.TaskId, ServerId: s.ServerID})
	if err != nil {
		t.Fatal(err)
	}
	if result.Update.Status != mdk.FirmwareUpdateStatus_FIRMWARE_UPDATE_STATUS_COMPLETED_WITH_ERRORS {
		t.Fatal(result.Update)
	}
	failures := 0
	for _, target := range result.Update.Targets {
		if target.Status == mdk.FirmwareTargetStatus_FIRMWARE_TARGET_STATUS_FAILED {
			failures++
			if target.Reason != "board_unavailable" {
				t.Fatal(target)
			}
		}
	}
	if failures != 1 {
		t.Fatalf("want one board failure, got %d", failures)
	}
	if s.Version == "9.0.0" {
		t.Fatal("changed controller firmware")
	}
	_, err = client.GetFirmwareUpdate(context.Background(), &mdk.GetFirmwareUpdateRequest{TaskId: job.Update.TaskId, ServerId: "other"})
	if err == nil {
		t.Fatal("accepted wrong owner")
	}
}
