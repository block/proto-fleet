package ct1

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	"github.com/block/proto-fleet/sdk/mdk/psucap"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	psuFirmwareCallTimeout = 10 * time.Second
	psuBoardStatusTimeout  = 5 * time.Second
	// psuAttemptLiveWindow is how recently a status poll must have driven an
	// unfinished update for another command to be refused. Fleet polls every
	// ten seconds; an update nobody polls has stopped and can be replaced.
	psuAttemptLiveWindow = time.Minute
	maxPSUStartRefusals  = 3
)

type psuModuleState int

const (
	psuModulePending psuModuleState = iota
	// psuModuleStarting has a start request whose outcome is not confirmed.
	psuModuleStarting
	psuModuleRunning
	// psuModuleSucceeded also covers a module already on the capsule.
	psuModuleSucceeded
	psuModuleFailed
)

// psuModule is one PSU module of a container update.
type psuModule struct {
	id    string
	label string
	// owner is the controller that owns the module's boards. Frigate keeps
	// capsules, jobs, and job history on that controller only.
	owner   string
	recover bool
	// recoverChecked records that a refused plain job was already checked
	// for a PSU that needs recovery.
	recoverChecked bool
	state          psuModuleState
	taskID         string
	// seenTaskID is the module's newest job when the update was planned.
	seenTaskID string
	failure    string
}

// psuAttempt is a container's PSU firmware update. Frigate runs one module
// job at a time per controller, so the plugin starts a module only after the
// one before it on that controller ended, and stops starting modules once one
// is left out of mining or may still be programming.
//
// Only FirmwareUpdate starts a module, because Fleet admits that call as a
// device write and does not admit a status poll. A status poll reads the
// jobs and reports sdk.FirmwareStateContinue when a controller is ready for
// its next module; Fleet then sends the same command again. An update whose
// command stops therefore stops after its running jobs end.
type psuAttempt struct {
	mu        sync.Mutex
	requestID string
	capsuleID string
	modules   []*psuModule
	// halted is why no further module is started.
	halted   string
	polledAt time.Time
}

func (a *psuAttempt) finished() bool {
	for _, m := range a.modules {
		if m.state == psuModuleRunning || m.state == psuModuleStarting || m.state == psuModulePending && a.halted == "" {
			return false
		}
	}
	return true
}

// startable reports whether some controller has no job running and a module
// waiting, so the next FirmwareUpdate call would start one.
func (a *psuAttempt) startable() bool {
	if a.halted != "" {
		return false
	}
	for _, modules := range a.byOwner() {
		busy, pending := false, false
		for _, m := range modules {
			busy = busy || m.state == psuModuleRunning || m.state == psuModuleStarting
			pending = pending || m.state == psuModulePending
		}
		if pending && !busy {
			return true
		}
	}
	return false
}

// live reports whether a command is still driving an unfinished update.
func (a *psuAttempt) live(now time.Time) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return !a.finished() && now.Sub(a.polledAt) < psuAttemptLiveWindow
}

func (d *Device) currentPSUAttempt() *psuAttempt {
	state := d.firmwareShared()
	state.mu.Lock()
	defer state.mu.Unlock()
	return state.psu
}

func (d *Device) setPSUAttempt(attempt *psuAttempt) {
	state := d.firmwareShared()
	state.mu.Lock()
	defer state.mu.Unlock()
	state.psu, state.attempt = attempt, nil
}

func (d *Device) controllerClient(owner string) *Client {
	for _, c := range d.clients {
		if c.memberID == owner {
			return c
		}
	}
	return nil
}

// psuFirmwareUpdate brings every PSU module of the container to the capsule.
// It is safe to send again: modules whose controller reports the capsule as
// applied are left alone, a running job for the capsule is resumed, and a PSU
// the capsule left needing recovery is recovered.
func (d *Device) psuFirmwareUpdate(ctx context.Context, reader io.Reader, file sdk.FirmwareFile) error {
	if current := d.currentPSUAttempt(); current != nil {
		if file.RequestID != "" && current.requestID == file.RequestID {
			// The same command again: a retry, or the continuation a status
			// poll asked for. Either way this admitted call starts what is next.
			d.advancePSUAttempt(ctx, current)
			return nil
		}
		if current.live(time.Now()) {
			return sdk.WriteRefused(fmt.Errorf("another CT1 PSU firmware update is running"), sdk.WriteStageValidation, sdk.WriteReasonRejected)
		}
	}
	if previous := d.currentFirmwareAttempt(); previous != nil {
		current, lost, err := d.firmwareAttemptStatus(ctx, previous)
		if err != nil || !lost && current.State != firmwareStateCompleted && current.State != firmwareStateFailed {
			return sdk.WriteRefused(fmt.Errorf("a CT1 hashboard firmware update is unfinished or unavailable"), sdk.WriteStageValidation, sdk.WriteReasonRejected)
		}
	}
	data, err := io.ReadAll(io.LimitReader(reader, psucap.MaxSize+1))
	if err != nil {
		return fmt.Errorf("read PSU firmware capsule: %w", err)
	}
	capsule, err := psucap.Verify(data)
	if err != nil {
		return sdk.InvalidWriteRequest(fmt.Errorf("invalid PSU firmware capsule: %w", err))
	}
	c, err := d.selectClient(ctx, true)
	if err != nil {
		return sdk.WriteRefused(err, sdk.WriteStageBackendSelection, sdk.WriteReasonTransportFailed)
	}
	wire, err := c.boards.GetContainerMap(ctx, &mdk.GetContainerMapRequest{})
	if err != nil {
		return sdk.WriteRefused(err, sdk.WriteStageConfigurationRead, sdk.WriteReasonReadFailed)
	}
	attempt := &psuAttempt{requestID: file.RequestID, capsuleID: capsule.ID, polledAt: time.Now()}
	if attempt.modules, err = planPSUModules(wire, c.memberID); err != nil {
		return err
	}
	// Check every controller and plan every module before changing anything.
	owners := attempt.byOwner()
	for owner, modules := range owners {
		client := d.controllerClient(owner)
		if client == nil {
			return sdk.WriteRefused(fmt.Errorf("%s is owned by controller %s, which is not paired with Fleet; scan and pair both controllers", modules[0].label, owner), sdk.WriteStageBackendSelection, sdk.WriteReasonRejected)
		}
		if client != c {
			if err := verifyControllerIdentity(ctx, client); err != nil {
				return sdk.WriteRefused(err, sdk.WriteStageBackendSelection, sdk.WriteReasonTransportFailed)
			}
		}
		jobs, err := latestPSUJobs(ctx, client, owner)
		if status.Code(err) == codes.Unimplemented {
			return sdk.InvalidWriteRequest(fmt.Errorf("this controller's Frigate does not support PSU firmware updates"))
		}
		if err != nil {
			return sdk.WriteRefused(fmt.Errorf("read PSU firmware jobs: %w", err), sdk.WriteStageConfigurationRead, sdk.WriteReasonReadFailed)
		}
		for _, m := range modules {
			if err := d.planPSUModule(ctx, client, attempt, m, jobs[m.id]); err != nil {
				return err
			}
		}
	}
	// A controller caches capsules for itself only, so each one with a
	// module to start needs the capsule.
	for owner, modules := range owners {
		pending := false
		for _, m := range modules {
			pending = pending || m.state == psuModulePending
		}
		if !pending {
			continue
		}
		if err := uploadPSUCapsule(ctx, d.controllerClient(owner), data, capsule, file.Filename); err != nil {
			if firmwareStartRefused(err) {
				return sdk.InvalidWriteRequest(fmt.Errorf("CT1 refused the PSU firmware capsule: %w", err))
			}
			return sdk.WriteRefused(fmt.Errorf("upload PSU firmware capsule: %w", err), sdk.WriteStageConfigurationWrite, sdk.WriteReasonTransportFailed)
		}
	}
	d.setPSUAttempt(attempt)
	d.advancePSUAttempt(ctx, attempt)
	return nil
}

func verifyControllerIdentity(ctx context.Context, c *Client) error {
	ctx, cancel := context.WithTimeout(ctx, firmwarePeerLookupTimeout)
	defer cancel()
	response, err := c.system.GetSystemStatus(ctx, &mdk.GetSystemStatusRequest{})
	if err != nil {
		return fmt.Errorf("reach controller %s: %w", c.memberID, err)
	}
	if response.GetStatus().GetServerId() != c.memberID {
		return fmt.Errorf("CT1 status identity mismatch")
	}
	return nil
}

func (a *psuAttempt) byOwner() map[string][]*psuModule {
	out := map[string][]*psuModule{}
	for _, m := range a.modules {
		out[m.owner] = append(out[m.owner], m)
	}
	return out
}

// planPSUModules lists the container's PSU modules in tank and module order.
// A module whose boards two controllers own is refused up front, because
// Frigate does not coordinate a split module. So is a position whose boards
// report no module ID: Frigate addresses a module by that ID, and leaving the
// position out would let the command complete with its PSU never updated.
func planPSUModules(wire *mdk.GetContainerMapResponse, memberID string) ([]*psuModule, error) {
	topology, err := snapshot(mapResponse(wire), time.Now().UTC())
	if err != nil {
		return nil, sdk.WriteRefused(err, sdk.WriteStageConfigurationRead, sdk.WriteReasonReadFailed)
	}
	if !locatedInventoryComplete(topology) {
		return nil, sdk.WriteRefused(fmt.Errorf("CT1 container map is incomplete; retry when every tank and module is reported"), sdk.WriteStageConfigurationRead, sdk.WriteReasonReadFailed)
	}
	var modules []*psuModule
	byID := map[string]*psuModule{}
	for _, tank := range wire.GetContainerMap().GetTanks() {
		if tank.GetId() == 0 {
			continue
		}
		for _, module := range tank.GetModules() {
			label := fmt.Sprintf("tank %d module %d", tank.GetId(), module.GetId())
			identified, unidentifiedOwner := false, ""
			for _, slot := range module.GetSlots() {
				if !slot.GetOccupied() {
					continue
				}
				owner := slot.GetHashboard().GetOwnerServerId()
				if owner == "" {
					owner = memberID
				}
				id := strings.TrimSpace(slot.GetHashboard().GetModuleId())
				if id == "" {
					if unidentifiedOwner == "" {
						unidentifiedOwner = owner
					}
					continue
				}
				identified = true
				m := byID[id]
				if m == nil {
					m = &psuModule{id: id, label: label, owner: owner}
					byID[id] = m
					modules = append(modules, m)
				} else if m.owner != owner && m.state != psuModuleFailed {
					m.state, m.failure = psuModuleFailed, "its boards are owned by different controllers; Frigate cannot update a split module"
				}
			}
			if !identified && unidentifiedOwner != "" {
				modules = append(modules, &psuModule{
					label: label, owner: unidentifiedOwner, state: psuModuleFailed,
					failure: "its boards report no PSU module identity, so Frigate cannot update it",
				})
			}
		}
	}
	if len(byID) == 0 {
		return nil, sdk.InvalidWriteRequest(fmt.Errorf("container reports no PSU modules to update"))
	}
	return modules, nil
}

// latestPSUJobs returns each module's newest retained job on one controller.
func latestPSUJobs(ctx context.Context, c *Client, owner string) (map[string]*mdk.PSUFirmwareUpdate, error) {
	ctx, cancel := context.WithTimeout(ctx, psuFirmwareCallTimeout)
	defer cancel()
	response, err := c.psu.ListPSUFirmwareUpdates(ctx, &mdk.ListPSUFirmwareUpdatesRequest{})
	if err != nil {
		return nil, err
	}
	jobs := append([]*mdk.PSUFirmwareUpdate(nil), response.GetUpdates()...)
	sort.SliceStable(jobs, func(i, j int) bool {
		return jobs[i].GetCreatedAt().AsTime().After(jobs[j].GetCreatedAt().AsTime())
	})
	latest := map[string]*mdk.PSUFirmwareUpdate{}
	for _, job := range jobs {
		if owner != "" && job.GetServerId() != owner {
			return nil, fmt.Errorf("PSU firmware job identity mismatch")
		}
		if _, seen := latest[job.GetModuleId()]; !seen {
			latest[job.GetModuleId()] = job
		}
	}
	return latest, nil
}

func psuJobTerminal(state mdk.PSUFirmwareUpdateState) bool {
	return state == mdk.PSUFirmwareUpdateState_PSU_FIRMWARE_UPDATE_STATE_SUCCEEDED ||
		state == mdk.PSUFirmwareUpdateState_PSU_FIRMWARE_UPDATE_STATE_FAILED ||
		state == mdk.PSUFirmwareUpdateState_PSU_FIRMWARE_UPDATE_STATE_CANCELLED
}

// planPSUModule decides what a module needs from its newest retained job.
// An error refuses the whole update before anything is started.
func (d *Device) planPSUModule(ctx context.Context, c *Client, a *psuAttempt, m *psuModule, job *mdk.PSUFirmwareUpdate) error {
	if m.state == psuModuleFailed || job == nil {
		return nil
	}
	m.seenTaskID = job.GetTaskId()
	switch {
	case !psuJobTerminal(job.GetState()):
		if job.GetCapsuleId() != a.capsuleID {
			return sdk.WriteRefused(fmt.Errorf("%s is being updated with another PSU firmware capsule", m.label), sdk.WriteStageValidation, sdk.WriteReasonRejected)
		}
		m.state, m.taskID = psuModuleRunning, job.GetTaskId()
	case job.GetState() == mdk.PSUFirmwareUpdateState_PSU_FIRMWARE_UPDATE_STATE_SUCCEEDED:
		if job.GetCapsuleId() == a.capsuleID && psuStillUpdated(ctx, c, job) {
			m.state = psuModuleSucceeded
		}
	case job.GetState() == mdk.PSUFirmwareUpdateState_PSU_FIRMWARE_UPDATE_STATE_FAILED:
		d.planPSURecovery(ctx, c, a, m, job)
	}
	return nil
}

// psuStillUpdated reports whether the PSU a successful job updated is still in
// the module and still runs what that job left on it. Frigate keeps the job,
// and the boards keep their record of the capsule, after a PSU is replaced, so
// neither shows which PSU is installed now. The revisions the PSU reports do:
// the module is left alone only when its leader still reports this capsule as
// applied and the same revisions the job recorded. Without that evidence the
// module gets a new job, which Frigate ends without applying if the PSU
// already runs the capsule.
func psuStillUpdated(ctx context.Context, c *Client, job *mdk.PSUFirmwareUpdate) bool {
	recorded := job.GetLeaderStatus()
	if recorded.GetCurrentAppRevision() == "" || recorded.GetCurrentBootRevision() == "" || job.GetLeaderMacAddress() == "" {
		return false
	}
	current := psuBoardStatus(ctx, c, job.GetLeaderMacAddress())
	return current.GetPsuLeader() &&
		current.GetPhase() == mdk.PSUFirmwarePhase_PSU_FIRMWARE_PHASE_SUCCEEDED &&
		current.GetCapsuleId() == job.GetCapsuleId() &&
		current.GetCurrentAppRevision() == recorded.GetCurrentAppRevision() &&
		current.GetCurrentBootRevision() == recorded.GetCurrentBootRevision()
}

// planPSURecovery checks a module whose last job failed. A PSU left needing
// recovery can only be recovered with the capsule that failed, and a PSU
// still being programmed must not be touched.
func (d *Device) planPSURecovery(ctx context.Context, c *Client, a *psuAttempt, m *psuModule, job *mdk.PSUFirmwareUpdate) {
	// The board that leads the PSU journals the update, and that record
	// outlives Frigate's job history. Any member can report a PSU that needs
	// recovery or is still being programmed. The job's last leader status is
	// the fallback when no board answers.
	evidence := job.GetLeaderStatus()
	for _, member := range job.GetMembers() {
		current := psuBoardStatus(ctx, c, member.GetMacAddress())
		if current != nil && (current.GetPsuLeader() || psuNeedsRecovery(current) || psuApplyRunning(current.GetPhase())) {
			evidence = current
			break
		}
	}
	switch {
	case psuApplyRunning(evidence.GetPhase()):
		m.state, m.failure = psuModuleFailed, "an earlier PSU firmware apply is still running; send the update again once it ends"
	case !psuNeedsRecovery(evidence):
	case evidence.GetCapsuleId() == a.capsuleID:
		m.recover = true
	default:
		m.state, m.failure = psuModuleFailed, fmt.Sprintf("its PSU needs recovery with capsule %s; send that capsule again", shortCapsuleID(evidence.GetCapsuleId()))
	}
}

func psuBoardStatus(ctx context.Context, c *Client, mac string) *mdk.PSUFirmwareBoardStatus {
	ctx, cancel := context.WithTimeout(ctx, psuBoardStatusTimeout)
	defer cancel()
	response, err := c.psu.GetPSUFirmwareBoardStatus(ctx, &mdk.GetPSUFirmwareBoardStatusRequest{MacAddress: mac})
	if err != nil {
		return nil
	}
	return response.GetStatus()
}

func psuNeedsRecovery(board *mdk.PSUFirmwareBoardStatus) bool {
	return board.GetRecoveryRequired() || board.GetPhase() == mdk.PSUFirmwarePhase_PSU_FIRMWARE_PHASE_RECOVERY_REQUIRED
}

// psuApplyRunning reports the phases in which board firmware is changing the PSU.
func psuApplyRunning(phase mdk.PSUFirmwarePhase) bool {
	return phase >= mdk.PSUFirmwarePhase_PSU_FIRMWARE_PHASE_FENCING && phase <= mdk.PSUFirmwarePhase_PSU_FIRMWARE_PHASE_VERIFYING
}

// psuApplyOutcomeKnown reports whether the leader's last status settles an
// apply: the PSU finished, failed without needing recovery, or needs recovery.
// Any other phase, or no status at all, leaves the PSU's state unknown.
func psuApplyOutcomeKnown(board *mdk.PSUFirmwareBoardStatus) bool {
	switch board.GetPhase() { //nolint:exhaustive // Every other phase leaves the outcome unknown.
	case mdk.PSUFirmwarePhase_PSU_FIRMWARE_PHASE_SUCCEEDED,
		mdk.PSUFirmwarePhase_PSU_FIRMWARE_PHASE_FAILED,
		mdk.PSUFirmwarePhase_PSU_FIRMWARE_PHASE_RECOVERY_REQUIRED:
		return true
	}
	return false
}

// psuCapsuleRejected reports an error board firmware raises about the capsule
// itself, which every other module would raise too.
func psuCapsuleRejected(failure mdk.PSUFirmwareError) bool {
	return failure >= mdk.PSUFirmwareError_PSU_FIRMWARE_ERROR_CAPSULE_FORMAT && failure <= mdk.PSUFirmwareError_PSU_FIRMWARE_ERROR_CAPSULE_TRUST
}

func shortCapsuleID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	if id == "" {
		return "unknown"
	}
	return id
}

func uploadPSUCapsule(ctx context.Context, c *Client, data []byte, capsule *psucap.Capsule, filename string) error {
	ctx, cancel := context.WithTimeout(ctx, firmwareUploadTimeout)
	defer cancel()
	stream, err := c.psu.UploadPSUFirmwareCapsule(ctx)
	if err != nil {
		return err
	}
	name := filepath.Base(strings.TrimSpace(filename))
	if name == "." || name == ".." || name == string(filepath.Separator) || len(name) > 255 || strings.ContainsAny(name, "/\\\x00") {
		name = capsule.ID + psucap.Extension
	}
	meta := &mdk.PSUFirmwareCapsuleUploadMetadata{FileName: name, SizeBytes: capsule.Size, Sha256: capsule.ID}
	if err := stream.Send(&mdk.UploadPSUFirmwareCapsuleRequest{Payload: &mdk.UploadPSUFirmwareCapsuleRequest_Metadata{Metadata: meta}}); err != nil {
		return err
	}
	for offset := 0; offset < len(data); offset += psucap.ChunkBytes {
		chunk := data[offset:min(offset+psucap.ChunkBytes, len(data))]
		if err := stream.Send(&mdk.UploadPSUFirmwareCapsuleRequest{Payload: &mdk.UploadPSUFirmwareCapsuleRequest_Chunk{Chunk: chunk}}); err != nil {
			return err
		}
	}
	response, err := stream.CloseAndRecv()
	if err != nil {
		return err
	}
	if response.GetCapsule().GetCapsuleId() != capsule.ID {
		return fmt.Errorf("controller did not confirm the uploaded capsule")
	}
	return nil
}

// refreshPSUAttempt reads the controllers' jobs and settles each module that
// is starting or running. It only reads from the controllers; it never starts
// or changes a job. Caller holds a.mu.
func (d *Device) refreshPSUAttempt(ctx context.Context, a *psuAttempt) {
	for owner, modules := range a.byOwner() {
		c := d.controllerClient(owner)
		if c == nil {
			continue
		}
		for _, m := range modules {
			if m.state == psuModuleStarting {
				d.confirmPSUStart(ctx, c, a, m)
			}
			if m.state == psuModuleRunning {
				d.refreshPSUModule(ctx, c, a, m)
			}
		}
	}
}

// advancePSUAttempt reads the jobs and, on each controller with none running,
// starts the next module. It writes to the controllers, so only FirmwareUpdate
// calls it.
func (d *Device) advancePSUAttempt(ctx context.Context, a *psuAttempt) {
	a.mu.Lock()
	defer a.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, psuFirmwareCallTimeout)
	defer cancel()
	d.refreshPSUAttempt(ctx, a)
	for owner, modules := range a.byOwner() {
		c := d.controllerClient(owner)
		if c == nil {
			continue
		}
		busy := false
		for _, m := range modules {
			busy = busy || m.state == psuModuleRunning || m.state == psuModuleStarting
		}
		refused := 0
		for _, m := range modules {
			// A refused start moves on to the next module; a controller that
			// refuses several in a row is left for the next call.
			if busy || a.halted != "" || refused == maxPSUStartRefusals {
				break
			}
			if m.state == psuModulePending {
				d.startPSUModule(ctx, c, a, m)
				if busy = m.state != psuModuleFailed; !busy {
					refused++
				}
			}
		}
	}
}

func (d *Device) startPSUModule(ctx context.Context, c *Client, a *psuAttempt, m *psuModule) {
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	response, err := c.psu.StartPSUFirmwareUpdate(ctx, &mdk.StartPSUFirmwareUpdateRequest{CapsuleId: a.capsuleID, ModuleId: m.id, Recover: m.recover})
	switch {
	case err == nil && response.GetUpdate().GetTaskId() != "" && (m.owner == "" || response.GetUpdate().GetServerId() == m.owner):
		m.state, m.taskID = psuModuleRunning, response.GetUpdate().GetTaskId()
	case err == nil:
		m.state, m.failure = psuModuleFailed, "the controller answered as a different controller"
		a.halted = "a controller's identity changed"
	case firmwareStartRefused(err):
		m.state, m.failure = psuModuleFailed, "controller refused the update: "+status.Convert(err).Message()
	default:
		// The job may exist. A later read looks for it before it is started again.
		m.state = psuModuleStarting
	}
}

// confirmPSUStart settles a start whose outcome was not confirmed by looking
// for a job created since the update was planned.
func (d *Device) confirmPSUStart(ctx context.Context, c *Client, a *psuAttempt, m *psuModule) {
	jobs, err := latestPSUJobs(ctx, c, m.owner)
	if err != nil {
		return
	}
	job := jobs[m.id]
	switch {
	case job == nil || job.GetTaskId() == m.seenTaskID:
		m.state = psuModulePending
	case job.GetCapsuleId() != a.capsuleID:
		m.state, m.failure = psuModuleFailed, "it is being updated with another PSU firmware capsule"
	default:
		m.state, m.taskID = psuModuleRunning, job.GetTaskId()
	}
}

func (d *Device) refreshPSUModule(ctx context.Context, c *Client, a *psuAttempt, m *psuModule) {
	response, err := c.psu.GetPSUFirmwareUpdate(ctx, &mdk.GetPSUFirmwareUpdateRequest{TaskId: m.taskID})
	if status.Code(err) == codes.NotFound {
		// Frigate keeps jobs in memory, so a restart forgets one mid-update.
		m.state, m.failure = psuModuleFailed, "the controller no longer has its update job; the PSU may still be programming or need recovery"
		a.halted = m.label + " has an unknown outcome"
		return
	}
	if err != nil {
		return
	}
	job := response.GetUpdate()
	if job.GetTaskId() != m.taskID || m.owner != "" && job.GetServerId() != m.owner {
		m.state, m.failure = psuModuleFailed, "the controller answered as a different controller"
		a.halted = "a controller's identity changed"
		return
	}
	if !psuJobTerminal(job.GetState()) {
		return
	}
	if job.GetState() == mdk.PSUFirmwareUpdateState_PSU_FIRMWARE_UPDATE_STATE_SUCCEEDED {
		m.state = psuModuleSucceeded
		return
	}
	// Frigate refuses a plain job for a PSU that needs recovery before it
	// changes anything. Recover it with this capsule if that is what failed.
	failed := job.GetState() == mdk.PSUFirmwareUpdateState_PSU_FIRMWARE_UPDATE_STATE_FAILED
	if failed && !job.GetApplyRequested() && !m.recover && !m.recoverChecked {
		m.recoverChecked = true
		d.planPSURecovery(ctx, c, a, m, job)
		if m.recover {
			m.state, m.seenTaskID = psuModulePending, job.GetTaskId()
			return
		}
		if m.state == psuModuleFailed {
			a.halted = m.label + " was left out of mining"
			return
		}
	}
	m.state, m.failure = psuModuleFailed, strings.TrimSpace(job.GetError())
	if m.failure == "" {
		m.failure = "update " + strings.ToLower(strings.TrimPrefix(job.GetState().String(), "PSU_FIRMWARE_UPDATE_STATE_"))
	}
	// A member whose mining was not restored means the module is left stopped.
	// Frigate restarts a member that was mining only when it leaves no hold.
	held, restored := psuNeedsRecovery(job.GetLeaderStatus()), false
	for _, member := range job.GetMembers() {
		reason := strings.TrimSpace(member.GetRestoreError())
		if reason == "" {
			restored = restored || member.GetReserved() && member.GetMiningBefore()
			continue
		}
		held = true
		if !strings.Contains(m.failure, reason) {
			m.failure += "; " + reason
		}
	}
	switch {
	case held:
		a.halted = m.label + " was left out of mining"
	case job.GetApplyRequested() && !restored && !psuApplyOutcomeKnown(job.GetLeaderStatus()):
		// The apply may have reached the PSU and the leader's last status
		// does not settle it. Frigate holds such a module out of mining, but
		// records that only on members that were mining before the job, and
		// this module had none.
		m.failure += "; the PSU may still be programming"
		a.halted = m.label + " may still be programming its PSU"
	case psuCapsuleRejected(job.GetLeaderStatus().GetError()):
		a.halted = "board firmware rejected the capsule"
	}
}

// status summarizes the update. Progress is the share of modules that
// finished, updated or failed: Fleet keeps the command open while it rises,
// and an update that works through failing modules is still advancing.
// Caller holds a.mu.
func (a *psuAttempt) status() *sdk.FirmwareUpdateStatus {
	finished, pending := 0, 0
	var failures []string
	for _, m := range a.modules {
		switch m.state {
		case psuModuleSucceeded:
			finished++
		case psuModuleFailed:
			finished++
			name := m.label
			if m.id != "" {
				name += " (" + m.id + ")"
			}
			failures = append(failures, name+": "+m.failure)
		case psuModulePending:
			pending++
		case psuModuleStarting, psuModuleRunning:
		}
	}
	progress := finished * 100 / len(a.modules)
	result := &sdk.FirmwareUpdateStatus{State: firmwareStateInstalling, Progress: &progress}
	if !a.finished() {
		if a.startable() {
			result.State = sdk.FirmwareStateContinue
		}
		return result
	}
	if a.halted != "" && pending > 0 {
		failures = append(failures, fmt.Sprintf("update stopped because %s; %d modules were not attempted", a.halted, pending))
	}
	if len(failures) == 0 {
		result.State = firmwareStateCompleted
		return result
	}
	message := firmwareFailureMessage("PSU module", failures)
	result.State, result.Error = firmwareStateFailed, &message
	return result
}

// psuFirmwareStatus reads the update's jobs and reports where it stands. It
// never starts a module: Fleet does not admit a status poll as a device write.
func (d *Device) psuFirmwareStatus(ctx context.Context, a *psuAttempt) *sdk.FirmwareUpdateStatus {
	a.mu.Lock()
	defer a.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, psuFirmwareCallTimeout)
	defer cancel()
	d.refreshPSUAttempt(ctx, a)
	a.polledAt = time.Now()
	return a.status()
}
