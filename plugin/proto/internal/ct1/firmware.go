package ct1

import (
	"context"
	"errors"
	"fmt"
	"io"
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
	// firmwarePeerLookupTimeout bounds each best-effort job lookup on a paired
	// controller that owns no boards in the current plan.
	firmwarePeerLookupTimeout = 2 * time.Second
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
// started it. State is lost when the plugin process restarts; a retried
// command then finds its job by the task ID derived from its request ID.
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
	taskID string
	owners map[string][]string
	// requestID identifies the Fleet command that started the attempt.
	requestID string
	// untracked lists occupied boards a recovered task never targeted.
	untracked []string
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
	// An automatic retry of the command that started the current attempt
	// resumes it: status polling reports that job instead of a new flash.
	if current := d.currentFirmwareAttempt(); current != nil && file.RequestID != "" && current.requestID == file.RequestID {
		return nil
	}
	previous, err := d.settledFirmwareAttempt(ctx)
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
	attempt, macs, err := planFirmwareAttempt(wire, c.memberID, file.RequestID)
	if err != nil {
		return err
	}
	if file.RequestID != "" {
		recovered, err := d.recoverFirmwareAttempt(ctx, c, attempt)
		if err != nil {
			return sdk.WriteRefused(fmt.Errorf("check for this command's CT1 firmware job: %w", err), sdk.WriteStageConfigurationRead, sdk.WriteReasonReadFailed)
		}
		if recovered != nil {
			d.setFirmwareAttempt(recovered)
			return nil
		}
	}
	archive, err := stageFirmwareArchive(ctx, file.Reader)
	if err != nil {
		return err
	}
	uploadCtx, cancel := context.WithTimeout(ctx, firmwareUploadTimeout)
	artifact, err := uploadFirmware(uploadCtx, c, archive, file.Filename)
	cancel()
	archive.Release()
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

// firmwareStartRefused reports a status that proves no job was created. A task
// ID belongs to one command, so AlreadyExists means its job may be running.
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
// terminal. An unfinished or unreadable attempt refuses a new start because it
// may still change the boards.
func (d *Device) settledFirmwareAttempt(ctx context.Context) (*firmwareAttempt, error) {
	previous := d.currentFirmwareAttempt()
	if previous == nil {
		return nil, nil
	}
	current, err := d.firmwareAttemptStatus(ctx, previous)
	if errors.Is(err, errFirmwareTaskNotFound) {
		// The start never took effect, or its finished job is no longer retained.
		d.setFirmwareAttempt(nil)
		return nil, nil
	}
	if err != nil || (current.State != firmwareStateCompleted && current.State != firmwareStateFailed) {
		return nil, sdk.WithWriteOutcome(fmt.Errorf("previous CT1 firmware job is unfinished or unavailable"), sdk.WriteUnknown, sdk.WriteStageDispatch, sdk.WriteReasonTransportFailed)
	}
	return previous, nil
}

// stageFirmwareArchive works from the pinned reader, never an unverified path
// supplied by a caller. The caller must release the returned archive.
func stageFirmwareArchive(ctx context.Context, reader io.Reader) (*hbfirmware.Archive, error) {
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
	archive, err := hbfirmware.Read(ctx, f.Name(), "")
	if err != nil {
		if ctx.Err() != nil {
			return nil, sdk.WriteRefused(err, sdk.WriteStageValidation, sdk.WriteReasonRejected)
		}
		return nil, sdk.InvalidWriteRequest(err)
	}
	return archive, nil
}

// firmwareTaskID names the controller job for a Fleet request, so a retry can
// find the job its command started even after the plugin restarts.
func firmwareTaskID(requestID string) string {
	if requestID == "" {
		return uuid.NewString()
	}
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte("urn:proto-fleet:ct1-firmware-request:"+requestID)).String()
}

// recoverFirmwareAttempt rebuilds a retried command's attempt from the jobs its
// task created, or returns nil when no controller has one. Board ownership may
// have changed since the task started, so every paired controller is asked
// through the selected one; planned owners must answer, while other peers are
// best effort so an unreachable peer cannot block updates. Each job's own
// targets define what it must finish, and current boards no found job
// targeted report as not updated.
func (d *Device) recoverFirmwareAttempt(ctx context.Context, c *Client, planned *firmwareAttempt) (*firmwareAttempt, error) {
	jobs := map[string][]string{}
	lookup := func(ctx context.Context, owner string) error {
		response, err := c.firmware.GetFirmwareUpdate(ctx, &mdk.GetFirmwareUpdateRequest{TaskId: planned.taskID, ServerId: owner})
		if status.Code(err) == codes.NotFound {
			return nil
		}
		if err != nil {
			return err
		}
		job := response.GetUpdate()
		if job.GetTaskId() != planned.taskID || job.GetServerId() != owner {
			return fmt.Errorf("firmware job identity mismatch")
		}
		targets := []string{}
		for _, target := range job.GetTargets() {
			targets = append(targets, normalizedBoardMAC(target.GetMacAddress()))
		}
		jobs[owner] = targets
		return nil
	}
	for owner := range planned.owners {
		if err := lookup(ctx, owner); err != nil {
			return nil, err
		}
	}
	for _, peer := range d.clients {
		if _, owns := planned.owners[peer.memberID]; owns || peer.memberID == "" {
			continue
		}
		peerCtx, cancel := context.WithTimeout(ctx, firmwarePeerLookupTimeout)
		_ = lookup(peerCtx, peer.memberID)
		cancel()
	}
	if len(jobs) == 0 {
		return nil, nil
	}
	recovered := &firmwareAttempt{taskID: planned.taskID, requestID: planned.requestID, owners: jobs}
	tracked := map[string]bool{}
	for _, targets := range jobs {
		for _, mac := range targets {
			tracked[mac] = true
		}
	}
	for _, macs := range planned.owners {
		for _, mac := range macs {
			if !tracked[mac] {
				recovered.untracked = append(recovered.untracked, mac)
			}
		}
	}
	sort.Strings(recovered.untracked)
	return recovered, nil
}

// planFirmwareAttempt targets every occupied board. Boards report a version
// but not their installed build, so earlier success cannot prove a board
// still runs the artifact. Partial located inventory refuses the update so
// omitted boards cannot be reported as updated.
func planFirmwareAttempt(wire *mdk.GetContainerMapResponse, memberID, requestID string) (*firmwareAttempt, []string, error) {
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
	attempt := &firmwareAttempt{taskID: firmwareTaskID(requestID), owners: map[string][]string{}, requestID: requestID}
	var macs []string
	for _, b := range boards {
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
	return d.firmwareAttemptStatus(ctx, attempt)
}

// firmwareAttemptStatus reports "completed" only when every target on every
// owning controller succeeded or was current, and "failed" once every
// existing owner job is terminal with any failure, including owners that no
// longer know the task and boards a resumed task never targeted. Frigate restores boards either way, so Fleet
// never reboots CT1.
func (d *Device) firmwareAttemptStatus(ctx context.Context, attempt *firmwareAttempt) (*sdk.FirmwareUpdateStatus, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	c, err := d.selectClient(ctx, false)
	if err != nil {
		return nil, err
	}
	finished, expected, missing := 0, len(attempt.untracked), 0
	var failures []string
	for _, mac := range attempt.untracked {
		failures = append(failures, mac+": not targeted by the resumed task")
	}
	allTerminal := true
	for owner, macs := range attempt.owners {
		expected += len(macs)
		response, err := c.firmware.GetFirmwareUpdate(ctx, &mdk.GetFirmwareUpdateRequest{TaskId: attempt.taskID, ServerId: owner})
		if status.Code(err) == codes.NotFound {
			// That controller has no job for the task: it is not running there,
			// and its boards were not proven updated.
			missing++
			failures = append(failures, fmt.Sprintf("controller %s: firmware task not found", owner))
			continue
		}
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
	if missing == len(attempt.owners) {
		return nil, errFirmwareTaskNotFound
	}
	progress := 0
	if expected > 0 {
		progress = finished * 100 / expected
	}
	result := &sdk.FirmwareUpdateStatus{State: firmwareStateInstalling, Progress: &progress}
	if len(failures) > 0 && allTerminal {
		message := firmwareFailureMessage(failures)
		result.State, result.Error = firmwareStateFailed, &message
	} else if finished == expected && allTerminal {
		result.State = firmwareStateCompleted
	}
	return result, nil
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
