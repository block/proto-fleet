package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	hbfirmware "github.com/block/proto-fleet/sdk/mdk/firmware"
	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type simulatedFirmware struct {
	artifacts map[string][]*mdk.FirmwareImage
	jobs      map[string]*mdk.FirmwareUpdate
	uploads   map[string]*simulatedFirmwareUpload
}

const simulatedFlashDuration = 4 * time.Second

func newSimulatedFirmware() *simulatedFirmware {
	return &simulatedFirmware{artifacts: map[string][]*mdk.FirmwareImage{}, jobs: map[string]*mdk.FirmwareUpdate{}, uploads: map[string]*simulatedFirmwareUpload{}}
}

func (a *grpcAPI) UploadFirmwareArtifact(stream mdk.FirmwareService_UploadFirmwareArtifactServer) error {
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	meta := first.GetMetadata()
	if meta == nil || len(meta.Images) == 0 || len(meta.Images) > hbfirmware.MaxImages {
		return status.Error(codes.InvalidArgument, "image metadata required")
	}
	data := make([][]byte, len(meta.Images))
	seen := map[string]bool{}
	var total int64
	for _, image := range meta.Images {
		if image.SizeBytes <= 0 || image.SizeBytes > hbfirmware.MaxImageBytes || seen[strings.ToLower(image.Name)] || !strings.HasSuffix(image.Name, ".bin") || strings.ContainsAny(image.Name, "/\\") {
			return status.Error(codes.InvalidArgument, "invalid image metadata")
		}
		seen[strings.ToLower(image.Name)] = true
		total += image.SizeBytes
	}
	if total > hbfirmware.MaxTotalBytes {
		return status.Error(codes.ResourceExhausted, "upload too large")
	}
	for {
		message, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		chunk := message.GetChunk()
		if chunk == nil || int(chunk.ImageIndex) >= len(data) || len(chunk.Data) > hbfirmware.ChunkBytes {
			return status.Error(codes.InvalidArgument, "invalid firmware chunk")
		}
		i := chunk.ImageIndex
		if int64(len(data[i])+len(chunk.Data)) > meta.Images[i].SizeBytes {
			return status.Error(codes.InvalidArgument, "image exceeds declared size")
		}
		data[i] = append(data[i], chunk.Data...)
	}
	a.state.mu.Lock()
	result, err := a.cacheFirmware(meta, data)
	a.state.mu.Unlock()
	if err != nil {
		return err
	}
	return stream.SendAndClose(result)
}

// Caller holds state.mu. Both upload transports share image validation and caching.
func (a *grpcAPI) cacheFirmware(meta *mdk.FirmwareUploadMetadata, data [][]byte) (*mdk.UploadFirmwareArtifactResponse, error) {
	var images []*mdk.FirmwareImage
	digest := sha256.New()
	for i, image := range meta.Images {
		checksum := fmt.Sprintf("%x", sha256.Sum256(data[i]))
		if int64(len(data[i])) != image.SizeBytes || checksum != image.Sha256 {
			return nil, status.Error(codes.InvalidArgument, "firmware checksum or size mismatch")
		}
		decoded, err := hbfirmware.DecodeMetadata(data[i])
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		_, _ = fmt.Fprintf(digest, "%s:%s\n", image.Name, checksum)
		images = append(images, &mdk.FirmwareImage{Name: image.Name, Version: decoded.Version(), Build: decoded.Build, HardwareRevision: decoded.HardwareRevision, Sha256: checksum})
	}
	id := fmt.Sprintf("%x", digest.Sum(nil))
	a.state.firmware.artifacts[id] = images
	return &mdk.UploadFirmwareArtifactResponse{Artifact: &mdk.FirmwareArtifactRef{Source: mdk.FirmwareArtifactSource_FIRMWARE_ARTIFACT_SOURCE_UPLOAD, UploadId: id}, Images: images}, nil
}

func (a *grpcAPI) StartFirmwareUpdate(_ context.Context, req *mdk.StartFirmwareUpdateRequest) (*mdk.StartFirmwareUpdateResponse, error) {
	a.state.mu.Lock()
	defer a.state.mu.Unlock()
	s := a.state
	images := s.firmware.artifacts[req.GetArtifact().GetUploadId()]
	if len(images) == 0 {
		return nil, status.Error(codes.NotFound, "artifact not uploaded")
	}
	if req.Scope == nil || len(req.Scope.Identifiers) == 0 || len(req.Scope.Modules) > 0 || len(req.R2Targets) > 0 {
		return nil, status.Error(codes.InvalidArgument, "explicit CT1 board scope required")
	}
	build, version := "", ""
	for _, image := range images {
		if req.GetOptions().GetBuild() != "" && req.Options.Build != image.Build {
			continue
		}
		if build != "" && (build != image.Build || version != image.Version) {
			return nil, status.Error(codes.InvalidArgument, "ambiguous firmware build")
		}
		build, version = image.Build, image.Version
	}
	if build == "" {
		return nil, status.Error(codes.InvalidArgument, "no images for build")
	}
	id := req.TaskId
	if id == "" {
		id = uuid.NewString()
	}
	if s.firmware.jobs[id] != nil {
		return nil, status.Error(codes.AlreadyExists, "task already exists")
	}
	job := &mdk.FirmwareUpdate{TaskId: id, ServerId: s.ServerID, Artifact: req.Artifact, Options: req.Options, Status: mdk.FirmwareUpdateStatus_FIRMWARE_UPDATE_STATUS_RUNNING, StartedAt: timestamppb.New(s.now())}
	seen := map[string]bool{}
	for _, mac := range req.Scope.Identifiers {
		mac = strings.ToUpper(strings.TrimSpace(mac))
		board := s.boardsByMAC[mac]
		if board == nil || seen[mac] {
			return nil, status.Error(codes.InvalidArgument, "unknown or duplicate hashboard")
		}
		seen[mac] = true
		for _, existing := range s.firmware.jobs {
			if existing.CompletedAt == nil {
				for _, target := range existing.Targets {
					if target.MacAddress == mac {
						return nil, status.Error(codes.FailedPrecondition, "hashboard update already running")
					}
				}
			}
		}
		job.Targets = append(job.Targets, &mdk.FirmwareUpdateTarget{MacAddress: mac, Serial: board.Serial, FromVersion: board.FirmwareVersion, ToVersion: version, Status: mdk.FirmwareTargetStatus_FIRMWARE_TARGET_STATUS_QUEUED, UpdatedAt: timestamppb.New(s.now())})
	}
	s.firmware.jobs[id] = job
	return &mdk.StartFirmwareUpdateResponse{Update: proto.Clone(job).(*mdk.FirmwareUpdate)}, nil
}

func (s *ContainerState) advanceFirmware(job *mdk.FirmwareUpdate) {
	if job.CompletedAt != nil {
		return
	}
	elapsed := s.now().Sub(job.StartedAt.AsTime())
	failed := false
	for _, target := range job.Targets {
		board := s.boardsByMAC[target.MacAddress]
		target.UpdatedAt = timestamppb.New(s.now())
		if elapsed < simulatedFlashDuration {
			target.Status = mdk.FirmwareTargetStatus_FIRMWARE_TARGET_STATUS_FLASHING
			target.ProgressPercent = min(99, max(0, elapsed.Seconds()*25))
			continue
		}
		if board == nil || board.Isolated || board.State == miningStateDisconnected {
			target.Status = mdk.FirmwareTargetStatus_FIRMWARE_TARGET_STATUS_FAILED
			target.Reason = "board_unavailable"
			target.Message = "Simulated hashboard is disconnected or isolated"
			failed = true
		} else {
			target.Status = mdk.FirmwareTargetStatus_FIRMWARE_TARGET_STATUS_SUCCEEDED
			target.ProgressPercent = 100
			board.FirmwareVersion = target.ToVersion
		}
	}
	if elapsed >= simulatedFlashDuration {
		job.Status = mdk.FirmwareUpdateStatus_FIRMWARE_UPDATE_STATUS_COMPLETED
		if failed {
			job.Status = mdk.FirmwareUpdateStatus_FIRMWARE_UPDATE_STATUS_COMPLETED_WITH_ERRORS
		}
		job.CompletedAt = timestamppb.New(s.now())
	}
}

func (a *grpcAPI) GetFirmwareUpdate(_ context.Context, req *mdk.GetFirmwareUpdateRequest) (*mdk.GetFirmwareUpdateResponse, error) {
	a.state.mu.Lock()
	defer a.state.mu.Unlock()
	job := a.state.firmware.jobs[req.TaskId]
	if job == nil || req.ServerId != "" && req.ServerId != a.state.ServerID {
		return nil, status.Error(codes.NotFound, "firmware task not found on controller")
	}
	a.state.advanceFirmware(job)
	return &mdk.GetFirmwareUpdateResponse{Update: proto.Clone(job).(*mdk.FirmwareUpdate)}, nil
}

func (a *grpcAPI) ListFirmwareUpdates(context.Context, *mdk.ListFirmwareUpdatesRequest) (*mdk.ListFirmwareUpdatesResponse, error) {
	a.state.mu.Lock()
	defer a.state.mu.Unlock()
	out := &mdk.ListFirmwareUpdatesResponse{}
	for _, job := range a.state.firmware.jobs {
		a.state.advanceFirmware(job)
		out.Updates = append(out.Updates, proto.Clone(job).(*mdk.FirmwareUpdate))
	}
	sort.Slice(out.Updates, func(i, j int) bool { return out.Updates[i].StartedAt.AsTime().After(out.Updates[j].StartedAt.AsTime()) })
	return out, nil
}

func (a *grpcAPI) CancelFirmwareUpdate(_ context.Context, req *mdk.CancelFirmwareUpdateRequest) (*mdk.CancelFirmwareUpdateResponse, error) {
	a.state.mu.Lock()
	defer a.state.mu.Unlock()
	job := a.state.firmware.jobs[req.TaskId]
	if job == nil || req.ServerId != "" && req.ServerId != a.state.ServerID {
		return nil, status.Error(codes.NotFound, "firmware task not found")
	}
	if job.CompletedAt == nil {
		job.Status = mdk.FirmwareUpdateStatus_FIRMWARE_UPDATE_STATUS_CANCELED
		job.CompletedAt = timestamppb.New(a.state.now())
	}
	return &mdk.CancelFirmwareUpdateResponse{Update: proto.Clone(job).(*mdk.FirmwareUpdate)}, nil
}
