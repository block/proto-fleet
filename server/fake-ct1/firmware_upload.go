package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
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

// Mirrors Frigate fc06e9c3's node-local, idle-expiring chunk sessions.
type simulatedFirmwareUpload struct {
	metadata *mdk.FirmwareUploadMetadata
	data     [][]byte
	expires  time.Time
}

func (s *ContainerState) expireFirmwareUploads() {
	for id, upload := range s.firmware.uploads {
		if !s.now().Before(upload.expires) {
			delete(s.firmware.uploads, id)
		}
	}
}

func (s *ContainerState) firmwareUpload(id string) (*simulatedFirmwareUpload, error) {
	s.expireFirmwareUploads()
	upload := s.firmware.uploads[id]
	if upload == nil {
		return nil, status.Error(codes.NotFound, "upload not found or expired")
	}
	upload.expires = s.now().Add(10 * time.Minute)
	return upload, nil
}

func (a *grpcAPI) BeginFirmwareArtifactUpload(_ context.Context, req *mdk.BeginFirmwareArtifactUploadRequest) (*mdk.BeginFirmwareArtifactUploadResponse, error) {
	meta := req.GetMetadata()
	source := meta.GetSource()
	validSource := false
	switch source.GetSource() {
	case mdk.FirmwareArtifactSource_FIRMWARE_ARTIFACT_SOURCE_RELEASE:
		validSource = source.GetReleaseTag() != "" && source.GetAssetName() != ""
	case mdk.FirmwareArtifactSource_FIRMWARE_ARTIFACT_SOURCE_PR:
		validSource = source.GetPrNumber() > 0 && source.GetArtifactId() > 0
	case mdk.FirmwareArtifactSource_FIRMWARE_ARTIFACT_SOURCE_BRANCH:
		validSource = source.GetBranch() != "" && source.GetArtifactId() > 0
	}
	if !validSource || len(meta.GetImages()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "source and image metadata required")
	}
	if len(meta.Images) > hbfirmware.MaxImages {
		return nil, status.Error(codes.ResourceExhausted, "too many images")
	}
	seen := map[string]bool{}
	var total int64
	for _, image := range meta.Images {
		name := image.GetName()
		digest, err := hex.DecodeString(image.GetSha256())
		if image == nil || image.SizeBytes <= 0 || seen[strings.ToLower(name)] || !strings.HasSuffix(name, ".bin") || strings.ContainsAny(name, "/\\\x00") || strings.Contains(name, ".unsigned.") || err != nil || len(digest) != sha256.Size || strings.ToLower(image.Sha256) != image.Sha256 {
			return nil, status.Error(codes.InvalidArgument, "invalid image metadata")
		}
		if image.SizeBytes > hbfirmware.MaxImageBytes {
			return nil, status.Error(codes.ResourceExhausted, "image too large")
		}
		seen[strings.ToLower(name)] = true
		total += image.SizeBytes
	}
	if total > hbfirmware.MaxTotalBytes {
		return nil, status.Error(codes.ResourceExhausted, "upload too large")
	}
	a.state.mu.Lock()
	defer a.state.mu.Unlock()
	a.state.expireFirmwareUploads()
	if len(a.state.firmware.uploads) >= 2 {
		return nil, status.Error(codes.ResourceExhausted, "too many uploads in progress")
	}
	id := uuid.NewString()
	expires := a.state.now().Add(10 * time.Minute)
	a.state.firmware.uploads[id] = &simulatedFirmwareUpload{metadata: proto.Clone(meta).(*mdk.FirmwareUploadMetadata), data: make([][]byte, len(meta.Images)), expires: expires}
	return &mdk.BeginFirmwareArtifactUploadResponse{UploadId: id, MaxChunkBytes: hbfirmware.ChunkBytes, ExpiresAt: timestamppb.New(expires)}, nil
}

func (a *grpcAPI) WriteFirmwareArtifactUploadChunk(_ context.Context, req *mdk.WriteFirmwareArtifactUploadChunkRequest) (*mdk.WriteFirmwareArtifactUploadChunkResponse, error) {
	if req.Offset < 0 || len(req.Data) == 0 {
		return nil, status.Error(codes.InvalidArgument, "nonnegative offset and nonempty data required")
	}
	if len(req.Data) > hbfirmware.ChunkBytes {
		return nil, status.Error(codes.ResourceExhausted, "chunk exceeds 1 MiB")
	}
	a.state.mu.Lock()
	defer a.state.mu.Unlock()
	upload, err := a.state.firmwareUpload(req.UploadId)
	if err != nil {
		return nil, err
	}
	if int(req.ImageIndex) >= len(upload.data) {
		return nil, status.Error(codes.InvalidArgument, "image index out of range")
	}
	i := req.ImageIndex
	image := upload.metadata.Images[i]
	written := int64(len(upload.data[i]))
	if req.Offset > image.SizeBytes || int64(len(req.Data)) > image.SizeBytes-req.Offset {
		return nil, status.Error(codes.InvalidArgument, "image exceeds declared size")
	}
	if req.Offset > written {
		return nil, status.Errorf(codes.FailedPrecondition, "chunk offset is %d; expected %d", req.Offset, written)
	}
	if req.Offset+int64(len(req.Data)) > written {
		upload.data[i] = append(upload.data[i], req.Data[written-req.Offset:]...)
		if int64(len(upload.data[i])) == image.SizeBytes && fmt.Sprintf("%x", sha256.Sum256(upload.data[i])) != image.Sha256 {
			upload.data[i] = nil
			return nil, status.Error(codes.InvalidArgument, "sha256 does not match metadata; resend from offset 0")
		}
	}
	result := &mdk.WriteFirmwareArtifactUploadChunkResponse{ImageIndex: i, NextOffset: int64(len(upload.data[i])), ExpiresAt: timestamppb.New(upload.expires)}
	for j, data := range upload.data {
		result.ReceivedBytes += int64(len(data))
		result.TotalBytes += upload.metadata.Images[j].SizeBytes
	}
	return result, nil
}

func (a *grpcAPI) CommitFirmwareArtifactUpload(_ context.Context, req *mdk.CommitFirmwareArtifactUploadRequest) (*mdk.CommitFirmwareArtifactUploadResponse, error) {
	a.state.mu.Lock()
	defer a.state.mu.Unlock()
	upload, err := a.state.firmwareUpload(req.UploadId)
	if err != nil {
		return nil, err
	}
	for i, image := range upload.metadata.Images {
		if int64(len(upload.data[i])) != image.SizeBytes {
			return nil, status.Error(codes.FailedPrecondition, "upload incomplete")
		}
	}
	delete(a.state.firmware.uploads, req.UploadId)
	result, err := a.cacheFirmware(upload.metadata, upload.data)
	if err != nil {
		return nil, err
	}
	return &mdk.CommitFirmwareArtifactUploadResponse{Artifact: result.Artifact, Images: result.Images}, nil
}

func (a *grpcAPI) AbortFirmwareArtifactUpload(_ context.Context, req *mdk.AbortFirmwareArtifactUploadRequest) (*mdk.AbortFirmwareArtifactUploadResponse, error) {
	a.state.mu.Lock()
	defer a.state.mu.Unlock()
	delete(a.state.firmware.uploads, req.UploadId)
	return &mdk.AbortFirmwareArtifactUploadResponse{}, nil
}
