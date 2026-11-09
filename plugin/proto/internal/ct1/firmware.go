package ct1

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	hbfirmware "github.com/block/proto-fleet/sdk/mdk/firmware"
	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const (
	maxFirmwareArchiveBytes = 500 << 20
	firmwareUploadTimeout   = 5 * time.Minute
	// FleetNode rejects FirmwareUpdateStatusResult.error beyond this length.
	maxFirmwareStatusErrorBytes = 4096
	maxFirmwareFailureBytes     = 256
)

// Firmware states reported to the server for this self-managed updater.
const (
	firmwareStateInstalling = "installing"
	firmwareStateCompleted  = "completed"
	firmwareStateFailed     = "failed"
)

var errFirmwareTaskNotFound = errors.New("no owning CT1 controller knows the firmware task")

// firmwareState is shared by every handle for one container: Fleet Node opens
// a new handle for each command, so an attempt must outlive the handle that
// started it. State is lost when the plugin process restarts.
type firmwareState struct {
	mu      sync.Mutex
	busy    bool
	attempt *firmwareAttempt
}

var firmwareStates = struct {
	sync.Mutex
	byContainer map[string]*firmwareState
}{byContainer: map[string]*firmwareState{}}

func firmwareStateFor(container string) *firmwareState {
	firmwareStates.Lock()
	defer firmwareStates.Unlock()
	state := firmwareStates.byContainer[container]
	if state == nil {
		state = &firmwareState{}
		firmwareStates.byContainer[container] = state
	}
	return state
}

// firmwareContainerIdentity names the physical container across handles: the
// paired endpoint key, else the controller-verified serial, else its address.
func firmwareContainerIdentity(info sdk.DeviceInfo) string {
	if key, err := sdk.DeviceEndpointKey(info.Endpoints); err == nil && key != "" {
		return "endpoints:" + key
	}
	if serial := strings.TrimSpace(info.SerialNumber); serial != "" {
		return "serial:" + serial
	}
	return "address:" + net.JoinHostPort(info.Host, strconv.Itoa(int(info.Port)))
}

type firmwareAttempt struct {
	taskID   string
	artifact string
	owners   map[string][]string
	// updated holds boards an earlier attempt for the same artifact finished
	// that still report its version. A retry flashes only the rest.
	updated map[string]bool
}

type firmwareAttemptResult struct {
	status  *sdk.FirmwareUpdateStatus
	updated map[string]bool
}

var _ sdk.FirmwareUpdateStatusProvider = (*Device)(nil)

func (d *Device) FirmwareUpdate(ctx context.Context, file sdk.FirmwareFile) error {
	if file.Reader == nil {
		return sdk.InvalidWriteRequest(fmt.Errorf("firmware reader is required"))
	}
	if !d.claimFirmware() {
		return sdk.WriteRefused(fmt.Errorf("another CT1 firmware update is being prepared"), sdk.WriteStageValidation, sdk.WriteReasonRejected)
	}
	defer d.releaseFirmware()
	previous, err := d.settledFirmwareAttempt(ctx)
	if err != nil {
		return err
	}
	archive, err := stageFirmwareArchive(file.Reader)
	if err != nil {
		return err
	}
	c, err := d.selectClient(ctx, true)
	if err != nil {
		return sdk.WriteRefused(err, sdk.WriteStageBackendSelection, sdk.WriteReasonTransportFailed)
	}
	wire, err := c.boards.GetContainerMap(ctx, &mdk.GetContainerMapRequest{})
	if err != nil {
		return sdk.WriteRefused(err, sdk.WriteStageConfigurationRead, sdk.WriteReasonReadFailed)
	}
	attempt, macs, err := planFirmwareAttempt(wire, c.memberID, archive, previous, time.Now().UTC())
	if err != nil {
		return err
	}
	if len(macs) == 0 {
		d.setFirmwareAttempt(attempt)
		return nil
	}
	uploadCtx, cancel := context.WithTimeout(ctx, firmwareUploadTimeout)
	artifact, err := uploadFirmware(uploadCtx, c, archive, file.Filename)
	cancel()
	if err != nil {
		return sdk.WriteRefused(err, sdk.WriteStageConfigurationWrite, sdk.WriteReasonTransportFailed)
	}
	// Only the start is a board write. Holding writeMu through staging and
	// upload would stall breaker, pool, and mining writes for the upload.
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	// Never replay a start whose outcome is still unknown.
	d.setFirmwareAttempt(attempt)
	_, err = c.firmware.StartFirmwareUpdate(ctx, &mdk.StartFirmwareUpdateRequest{
		Artifact: artifact, TaskId: attempt.taskID, Scope: &mdk.BoardScope{Identifiers: macs},
		Options: &mdk.FirmwareUpdateOptions{Build: archive.Build, Force: true},
	})
	if err == nil {
		return nil
	}
	if firmwareStartRefused(err) {
		d.setFirmwareAttempt(previous)
		return sdk.WriteRefused(fmt.Errorf("CT1 refused firmware start: %w", err), sdk.WriteStageDispatch, sdk.WriteReasonRejected)
	}
	return sdk.WithWriteOutcome(fmt.Errorf("CT1 firmware start outcome unconfirmed: %w", err), sdk.WriteUnknown, sdk.WriteStageDispatch, sdk.WriteReasonTransportFailed)
}

// firmwareStartRefused reports a status that proves no job was created. A
// fresh task ID cannot collide, so AlreadyExists stays ambiguous.
func firmwareStartRefused(err error) bool {
	switch status.Code(err) {
	case codes.InvalidArgument, codes.FailedPrecondition, codes.NotFound, codes.OutOfRange,
		codes.PermissionDenied, codes.Unauthenticated, codes.ResourceExhausted, codes.Unimplemented:
		return true
	case codes.OK, codes.Canceled, codes.Unknown, codes.DeadlineExceeded, codes.AlreadyExists,
		codes.Aborted, codes.Internal, codes.Unavailable, codes.DataLoss:
		return false
	}
	return false
}

func (d *Device) firmwareShared() *firmwareState {
	d.firmwareOnce.Do(func() {
		if d.firmware == nil {
			d.firmware = firmwareStateFor(firmwareContainerIdentity(d.info))
		}
	})
	return d.firmware
}

func (d *Device) claimFirmware() bool {
	state := d.firmwareShared()
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.busy {
		return false
	}
	state.busy = true
	return true
}

func (d *Device) releaseFirmware() {
	state := d.firmwareShared()
	state.mu.Lock()
	defer state.mu.Unlock()
	state.busy = false
}

func (d *Device) currentFirmwareAttempt() *firmwareAttempt {
	state := d.firmwareShared()
	state.mu.Lock()
	defer state.mu.Unlock()
	return state.attempt
}

func (d *Device) setFirmwareAttempt(attempt *firmwareAttempt) {
	state := d.firmwareShared()
	state.mu.Lock()
	defer state.mu.Unlock()
	state.attempt = attempt
}

// settledFirmwareAttempt returns the container's previous attempt once it is
// terminal, with every board it finished. An unfinished or unreadable attempt
// refuses a new start because it may still change the boards.
func (d *Device) settledFirmwareAttempt(ctx context.Context) (*firmwareAttempt, error) {
	previous := d.currentFirmwareAttempt()
	if previous == nil {
		return nil, nil
	}
	result, err := d.firmwareAttemptStatus(ctx, previous)
	if errors.Is(err, errFirmwareTaskNotFound) {
		// The start never took effect, or its finished job is no longer retained.
		d.setFirmwareAttempt(nil)
		return nil, nil
	}
	if err != nil || (result.status.State != firmwareStateCompleted && result.status.State != firmwareStateFailed) {
		return nil, sdk.WithWriteOutcome(fmt.Errorf("previous CT1 firmware job is unfinished or unavailable"), sdk.WriteUnknown, sdk.WriteStageDispatch, sdk.WriteReasonTransportFailed)
	}
	settled := *previous
	settled.updated = result.updated
	return &settled, nil
}

// stageFirmwareArchive works from the pinned reader, never an unverified path
// supplied by a caller.
func stageFirmwareArchive(reader io.Reader) (*hbfirmware.Archive, error) {
	f, err := os.CreateTemp("", "ct1-firmware-*.zip")
	if err != nil {
		return nil, fmt.Errorf("stage CT1 firmware: %w", err)
	}
	defer os.Remove(f.Name())
	defer f.Close()
	n, err := io.Copy(f, io.LimitReader(reader, maxFirmwareArchiveBytes+1))
	if err != nil {
		return nil, fmt.Errorf("copy CT1 firmware: %w", err)
	}
	if n > maxFirmwareArchiveBytes {
		return nil, sdk.InvalidWriteRequest(fmt.Errorf("firmware archive exceeds 500 MiB"))
	}
	archive, err := hbfirmware.Read(f.Name(), "")
	if err != nil {
		return nil, sdk.InvalidWriteRequest(err)
	}
	return archive, nil
}

// archiveIdentity names the selected images, independent of the archive's
// container bytes.
func archiveIdentity(archive *hbfirmware.Archive) string {
	digest := sha256.New()
	_, _ = fmt.Fprintf(digest, "%s\n%s\n", archive.Build, archive.Version)
	for _, image := range archive.Images {
		_, _ = fmt.Fprintf(digest, "%s:%s\n", image.Name, image.SHA256())
	}
	return fmt.Sprintf("%x", digest.Sum(nil))
}

// planFirmwareAttempt targets every occupied board, except boards an earlier
// attempt for the same artifact finished that freshly report its version; a
// board that drifted since is targeted again. Partial located inventory
// refuses the update so omitted boards cannot be reported as updated.
func planFirmwareAttempt(wire *mdk.GetContainerMapResponse, memberID string, archive *hbfirmware.Archive, previous *firmwareAttempt, now time.Time) (*firmwareAttempt, []string, error) {
	boards, err := occupiedFirmwareBoards(wire)
	if err != nil {
		return nil, nil, sdk.InvalidWriteRequest(err)
	}
	if len(boards) == 0 {
		return nil, nil, sdk.InvalidWriteRequest(fmt.Errorf("container has no hashboards to update"))
	}
	topology, err := snapshot(mapResponse(wire), time.Now().UTC())
	if err != nil {
		return nil, nil, sdk.WriteRefused(err, sdk.WriteStageConfigurationRead, sdk.WriteReasonReadFailed)
	}
	if len(topology.Tanks) > 0 && !locatedInventoryComplete(topology) {
		return nil, nil, sdk.WriteRefused(fmt.Errorf("CT1 container map is incomplete; retry when every tank and module is reported"), sdk.WriteStageConfigurationRead, sdk.WriteReasonReadFailed)
	}
	artifact := archiveIdentity(archive)
	attempt := &firmwareAttempt{taskID: uuid.NewString(), artifact: artifact, owners: map[string][]string{}, updated: map[string]bool{}}
	sameArtifact := previous != nil && previous.artifact == artifact
	var macs []string
	for _, b := range boards {
		if sameArtifact && previous.updated[b.mac] && boardReportsFirmware(b.board, archive.Version, now) {
			attempt.updated[b.mac] = true
			continue
		}
		owner := b.board.GetOwnerServerId()
		if owner == "" {
			owner = memberID
		}
		attempt.owners[owner] = append(attempt.owners[owner], b.mac)
		macs = append(macs, b.mac)
	}
	return attempt, macs, nil
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
	attempt := d.currentFirmwareAttempt()
	if attempt == nil {
		return nil, fmt.Errorf("no CT1 firmware job is associated with this device handle")
	}
	result, err := d.firmwareAttemptStatus(ctx, attempt)
	if err != nil {
		return nil, err
	}
	return result.status, nil
}

// firmwareAttemptStatus reports "completed" only when every target on every
// owning controller succeeded or was current, and "failed" once all owner jobs
// are terminal with any failure. Frigate restores boards either way, so Fleet
// never reboots CT1.
func (d *Device) firmwareAttemptStatus(ctx context.Context, attempt *firmwareAttempt) (firmwareAttemptResult, error) {
	updated := maps.Clone(attempt.updated)
	if updated == nil {
		updated = map[string]bool{}
	}
	if len(attempt.owners) == 0 {
		progress := 100
		return firmwareAttemptResult{status: &sdk.FirmwareUpdateStatus{State: firmwareStateCompleted, Progress: &progress}, updated: updated}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	c, err := d.selectClient(ctx, false)
	if err != nil {
		return firmwareAttemptResult{}, err
	}
	finished, expected, missing := 0, 0, 0
	var failures []string
	allTerminal := true
	for owner, macs := range attempt.owners {
		expected += len(macs)
		response, err := c.firmware.GetFirmwareUpdate(ctx, &mdk.GetFirmwareUpdateRequest{TaskId: attempt.taskID, ServerId: owner})
		if status.Code(err) == codes.NotFound {
			missing++
			continue
		}
		if err != nil {
			return firmwareAttemptResult{}, err
		}
		job := response.GetUpdate()
		if job.GetStatus() != mdk.FirmwareUpdateStatus_FIRMWARE_UPDATE_STATUS_COMPLETED && !firmwareJobFailed(job.GetStatus()) {
			allTerminal = false
		}
		if job.GetTaskId() != attempt.taskID || owner != "" && job.GetServerId() != owner {
			return firmwareAttemptResult{}, fmt.Errorf("firmware job identity mismatch")
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
				return firmwareAttemptResult{}, fmt.Errorf("firmware job missing hashboard %s on %s", mac, owner)
			}
			switch board.Status {
			case mdk.FirmwareTargetStatus_FIRMWARE_TARGET_STATUS_SUCCEEDED, mdk.FirmwareTargetStatus_FIRMWARE_TARGET_STATUS_SKIPPED_UP_TO_DATE:
				finished++
				updated[mac] = true
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
	if missing == len(attempt.owners) {
		return firmwareAttemptResult{}, errFirmwareTaskNotFound
	}
	if missing > 0 {
		return firmwareAttemptResult{}, fmt.Errorf("firmware task missing on %d of %d owning controllers", missing, len(attempt.owners))
	}
	progress := finished * 100 / expected
	result := &sdk.FirmwareUpdateStatus{State: firmwareStateInstalling, Progress: &progress}
	if len(failures) > 0 && allTerminal {
		message := firmwareFailureMessage(failures)
		result.State, result.Error = firmwareStateFailed, &message
	} else if finished == expected && allTerminal {
		result.State = firmwareStateCompleted
	}
	return firmwareAttemptResult{status: result, updated: updated}, nil
}

// firmwareFailureMessage keeps every failure it can within the status error
// bound and counts the rest.
func firmwareFailureMessage(failures []string) string {
	sort.Strings(failures)
	var b strings.Builder
	_, _ = fmt.Fprintf(&b, "%d hashboard firmware failures", len(failures))
	for i, failure := range failures {
		separator := "; "
		if i == 0 {
			separator = ": "
		}
		entry := separator + truncateUTF8(strings.ToValidUTF8(failure, ""), maxFirmwareFailureBytes)
		reserve := 0
		if remaining := len(failures) - i - 1; remaining > 0 {
			reserve = len(fmt.Sprintf("; ... and %d more", remaining))
		}
		if b.Len()+len(entry)+reserve > maxFirmwareStatusErrorBytes {
			_, _ = fmt.Fprintf(&b, "; ... and %d more", len(failures)-i)
			break
		}
		b.WriteString(entry)
	}
	return b.String()
}

func truncateUTF8(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	for limit > 0 && !utf8.RuneStart(value[limit]) {
		limit--
	}
	return value[:limit]
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
