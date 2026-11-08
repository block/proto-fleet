package ct1

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	hbfirmware "github.com/block/proto-fleet/sdk/mdk/firmware"
	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/google/uuid"
	"google.golang.org/grpc/metadata"
)

type firmwareAttempt struct {
	taskID string
	owners map[string][]string
}

var _ sdk.FirmwareUpdateStatusProvider = (*Device)(nil)

func (d *Device) FirmwareUpdate(ctx context.Context, file sdk.FirmwareFile) error {
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	// Never replay a start whose outcome is still unknown.
	d.firmwareMu.Lock()
	previous := d.firmwareAttempt
	d.firmwareMu.Unlock()
	if previous != nil {
		status, err := d.GetFirmwareUpdateStatus(ctx)
		if err != nil || (status.State != "completed" && status.State != "error") {
			return sdk.WithWriteOutcome(fmt.Errorf("previous CT1 firmware job is unfinished or unavailable"), sdk.WriteUnknown, sdk.WriteStageDispatch, sdk.WriteReasonTransportFailed)
		}
	}
	if file.Reader == nil {
		return sdk.InvalidWriteRequest(fmt.Errorf("firmware reader is required"))
	}
	// Work from the pinned reader, not an unverified path supplied by a caller.
	f, err := os.CreateTemp("", "ct1-firmware-*.zip")
	if err != nil {
		return fmt.Errorf("stage CT1 firmware: %w", err)
	}
	defer os.Remove(f.Name())
	defer f.Close()
	n, err := io.Copy(f, io.LimitReader(file.Reader, 500<<20+1))
	if err != nil {
		return fmt.Errorf("copy CT1 firmware: %w", err)
	}
	if n > 500<<20 {
		return sdk.InvalidWriteRequest(fmt.Errorf("firmware archive exceeds 500 MiB"))
	}
	archive, err := hbfirmware.Read(f.Name(), "")
	if err != nil {
		return sdk.InvalidWriteRequest(err)
	}
	c, err := d.selectClient(ctx, true)
	if err != nil {
		return sdk.WriteRefused(err, sdk.WriteStageBackendSelection, sdk.WriteReasonTransportFailed)
	}
	wire, err := c.boards.GetContainerMap(ctx, &mdk.GetContainerMapRequest{})
	if err != nil {
		return sdk.WriteRefused(err, sdk.WriteStageConfigurationRead, sdk.WriteReasonReadFailed)
	}
	attempt := &firmwareAttempt{taskID: uuid.NewString(), owners: map[string][]string{}}
	var macs []string
	seen := map[string]bool{}
	for _, tank := range wire.GetContainerMap().GetTanks() {
		for _, module := range tank.GetModules() {
			for _, slot := range module.GetSlots() {
				if !slot.GetOccupied() {
					continue
				}
				board := slot.GetHashboard()
				mac := normalizedBoardMAC(board.GetMacAddress())
				if mac == "" || seen[mac] {
					return sdk.InvalidWriteRequest(fmt.Errorf("missing or duplicate firmware board identity"))
				}
				seen[mac] = true
				owner := board.GetOwnerServerId()
				if owner == "" {
					owner = c.memberID
				}
				attempt.owners[owner] = append(attempt.owners[owner], mac)
				macs = append(macs, mac)
			}
		}
	}
	if len(macs) == 0 {
		return sdk.InvalidWriteRequest(fmt.Errorf("container has no hashboards to update"))
	}
	artifact, err := uploadFirmware(ctx, c, archive, file.Filename)
	if err != nil {
		return sdk.WriteRefused(err, sdk.WriteStageConfigurationRead, sdk.WriteReasonReadFailed)
	}
	d.firmwareMu.Lock()
	d.firmwareAttempt = attempt
	d.firmwareMu.Unlock()
	_, err = c.firmware.StartFirmwareUpdate(ctx, &mdk.StartFirmwareUpdateRequest{
		Artifact: artifact, TaskId: attempt.taskID, Scope: &mdk.BoardScope{Identifiers: macs},
		Options: &mdk.FirmwareUpdateOptions{Build: archive.Build, Force: true},
	})
	if err != nil {
		return sdk.WithWriteOutcome(fmt.Errorf("CT1 firmware start outcome unconfirmed: %w", err), sdk.WriteUnknown, sdk.WriteStageDispatch, sdk.WriteReasonTransportFailed)
	}
	return nil
}

func uploadFirmware(ctx context.Context, c *Client, archive *hbfirmware.Archive, filename string) (*mdk.FirmwareArtifactRef, error) {
	ctx, cancel := context.WithCancel(metadata.AppendToOutgoingContext(ctx, "x-frigate-actor", "proto-fleet"))
	defer cancel()
	stream, err := c.firmware.UploadFirmwareArtifact(ctx)
	if err != nil {
		return nil, err
	}
	meta := &mdk.FirmwareUploadMetadata{Source: &mdk.FirmwareArtifactRef{Source: mdk.FirmwareArtifactSource_FIRMWARE_ARTIFACT_SOURCE_RELEASE, ReleaseTag: archive.Version, AssetName: filename}}
	for _, image := range archive.Images {
		meta.Images = append(meta.Images, &mdk.FirmwareUploadImage{Name: image.Name, SizeBytes: int64(len(image.Data)), Sha256: image.SHA256()})
	}
	if err := stream.Send(&mdk.UploadFirmwareArtifactRequest{Payload: &mdk.UploadFirmwareArtifactRequest_Metadata{Metadata: meta}}); err != nil {
		return nil, err
	}
	for index := uint32(0); index < uint32(hbfirmware.MaxImages) && int(index) < len(archive.Images); index++ {
		image := archive.Images[index]
		for offset := 0; offset < len(image.Data); offset += hbfirmware.ChunkBytes {
			chunk := &mdk.FirmwareUploadChunk{ImageIndex: index, Data: image.Data[offset:min(offset+hbfirmware.ChunkBytes, len(image.Data))]}
			if err := stream.Send(&mdk.UploadFirmwareArtifactRequest{Payload: &mdk.UploadFirmwareArtifactRequest_Chunk{Chunk: chunk}}); err != nil {
				return nil, err
			}
		}
	}
	response, err := stream.CloseAndRecv()
	if err != nil {
		return nil, err
	}
	if response.GetArtifact().GetUploadId() == "" || len(response.GetImages()) != len(archive.Images) {
		return nil, fmt.Errorf("controller did not confirm uploaded images")
	}
	confirmed := map[string]bool{}
	for _, image := range response.Images {
		// Frigate may canonicalize ordering; identify by name, not index.
		if confirmed[image.Name] {
			return nil, fmt.Errorf("controller returned duplicate image metadata")
		}
		confirmed[image.Name] = true
		matched := false
		for _, expected := range archive.Images {
			if image.Name == expected.Name && image.Sha256 == expected.SHA256() && image.Build == archive.Build && image.Version == archive.Version {
				matched = true
				break
			}
		}
		if !matched {
			return nil, fmt.Errorf("controller returned different firmware image metadata")
		}
	}
	return response.Artifact, nil
}

func (d *Device) GetFirmwareUpdateStatus(ctx context.Context) (*sdk.FirmwareUpdateStatus, error) {
	d.firmwareMu.Lock()
	attempt := d.firmwareAttempt
	d.firmwareMu.Unlock()
	if attempt == nil {
		return nil, fmt.Errorf("no CT1 firmware job is associated with this device handle")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	c, err := d.selectClient(ctx, false)
	if err != nil {
		return nil, err
	}
	result := &sdk.FirmwareUpdateStatus{State: "installing"}
	finished, expected := 0, 0
	var failures []string
	allTerminal := true
	for owner, macs := range attempt.owners {
		expected += len(macs)
		response, err := c.firmware.GetFirmwareUpdate(ctx, &mdk.GetFirmwareUpdateRequest{TaskId: attempt.taskID, ServerId: owner})
		if err != nil {
			return nil, err
		}
		job := response.GetUpdate()
		if job.GetStatus() != mdk.FirmwareUpdateStatus_FIRMWARE_UPDATE_STATUS_COMPLETED && !firmwareJobFailed(job.GetStatus()) {
			allTerminal = false
		}
		if job.GetTaskId() != attempt.taskID || owner != "" && job.GetServerId() != owner {
			return nil, fmt.Errorf("firmware job identity mismatch")
		}
		if firmwareJobFailed(job.Status) {
			failures = append(failures, fmt.Sprintf("controller %s: %s %s", owner, job.Status, job.Error))
		}
		boards := map[string]*mdk.FirmwareUpdateTarget{}
		for _, board := range job.Targets {
			boards[normalizedBoardMAC(board.MacAddress)] = board
		}
		for _, mac := range macs {
			board := boards[mac]
			if board == nil {
				return nil, fmt.Errorf("firmware job missing hashboard %s on %s", mac, owner)
			}
			switch board.Status {
			case mdk.FirmwareTargetStatus_FIRMWARE_TARGET_STATUS_SUCCEEDED, mdk.FirmwareTargetStatus_FIRMWARE_TARGET_STATUS_SKIPPED_UP_TO_DATE:
				finished++
			case mdk.FirmwareTargetStatus_FIRMWARE_TARGET_STATUS_FAILED:
				failures = append(failures, fmt.Sprintf("%s (%s): %s %s", mac, owner, board.Reason, board.Message))
			case mdk.FirmwareTargetStatus_FIRMWARE_TARGET_STATUS_UNSPECIFIED,
				mdk.FirmwareTargetStatus_FIRMWARE_TARGET_STATUS_QUEUED,
				mdk.FirmwareTargetStatus_FIRMWARE_TARGET_STATUS_STOPPING_MINING,
				mdk.FirmwareTargetStatus_FIRMWARE_TARGET_STATUS_PREFLIGHT,
				mdk.FirmwareTargetStatus_FIRMWARE_TARGET_STATUS_FLASHING,
				mdk.FirmwareTargetStatus_FIRMWARE_TARGET_STATUS_REBOOTING,
				mdk.FirmwareTargetStatus_FIRMWARE_TARGET_STATUS_VERIFYING,
				mdk.FirmwareTargetStatus_FIRMWARE_TARGET_STATUS_RESTORING_MINING:
				// Still pending, including missing status evidence.
			}
		}
	}
	if expected == 0 {
		return nil, fmt.Errorf("firmware job has no targets")
	}
	progress := finished * 100 / expected
	result.Progress = &progress
	if len(failures) > 0 && allTerminal {
		sort.Strings(failures)
		message := strings.Join(failures, "; ")
		result.State, result.Error = "error", &message
	} else if finished == expected && allTerminal {
		// Frigate already rebooted and verified every board. Never reboot CT1.
		result.State = "completed"
	}
	return result, nil
}

func firmwareJobFailed(state mdk.FirmwareUpdateStatus) bool {
	switch state {
	case mdk.FirmwareUpdateStatus_FIRMWARE_UPDATE_STATUS_COMPLETED_WITH_ERRORS, mdk.FirmwareUpdateStatus_FIRMWARE_UPDATE_STATUS_FAILED, mdk.FirmwareUpdateStatus_FIRMWARE_UPDATE_STATUS_CANCELED, mdk.FirmwareUpdateStatus_FIRMWARE_UPDATE_STATUS_INTERRUPTED:
		return true
	case mdk.FirmwareUpdateStatus_FIRMWARE_UPDATE_STATUS_UNSPECIFIED,
		mdk.FirmwareUpdateStatus_FIRMWARE_UPDATE_STATUS_PENDING,
		mdk.FirmwareUpdateStatus_FIRMWARE_UPDATE_STATUS_DOWNLOADING,
		mdk.FirmwareUpdateStatus_FIRMWARE_UPDATE_STATUS_RUNNING,
		mdk.FirmwareUpdateStatus_FIRMWARE_UPDATE_STATUS_COMPLETED:
		return false
	}
	return false
}
