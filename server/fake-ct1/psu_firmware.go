package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	"github.com/block/proto-fleet/sdk/mdk/psucap"
	"github.com/block/proto-fleet/sdk/mdk/psucap/psucaptest"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// A simulated module update moves through Frigate's job states on this
// timeline. Jobs run one module at a time, as on a Frigate node.
const (
	simulatedPSUPrepare  = time.Second
	simulatedPSUStage    = time.Second
	simulatedPSUValidate = 500 * time.Millisecond
	simulatedPSUApply    = 1500 * time.Millisecond
	// simulatedPSUFault is how far into programming a configured fault occurs.
	simulatedPSUFault = 750 * time.Millisecond

	maxSimulatedCapsules = 32
	maxSimulatedPSUJobs  = 100

	defaultPSURevision        = psucaptest.DefaultRevision
	defaultPSUFirmwareVersion = "app=1310;bl=1110"
	psuRecoveryHold           = "PSU recovery is required; mining was left stopped"
)

type simulatedPSUFirmware struct {
	capsules map[string]*simulatedCapsule
	jobs     map[string]*mdk.PSUFirmwareUpdate
	// order lists task IDs, oldest first.
	order []string
	// faults counts the programming failures left for a module ID.
	faults map[string]int
}

type simulatedCapsule struct {
	info *mdk.PSUFirmwareCapsule
	// version is the firmware version the capsule installs.
	version string
}

// modulePSUFirmware is the update journal a module's PSU leader keeps.
type modulePSUFirmware struct {
	capsuleID        string
	phase            mdk.PSUFirmwarePhase
	failure          mdk.PSUFirmwareError
	recoveryRequired bool
	// hold keeps the module out of mining until a later job succeeds.
	hold bool
	// miningBefore records which boards were mining before the job that set hold.
	miningBefore map[string]bool
}

func newSimulatedPSUFirmware() *simulatedPSUFirmware {
	return &simulatedPSUFirmware{capsules: map[string]*simulatedCapsule{}, jobs: map[string]*mdk.PSUFirmwareUpdate{}, faults: map[string]int{}}
}

// parsePSUFirmwareFaults reads CT1_PSU_FIRMWARE_FAULT_MODULES, a comma-separated
// list of tank/module coordinates whose next PSU programming attempt fails.
func parsePSUFirmwareFaults(value string) ([][2]int, error) {
	var out [][2]int
	for _, entry := range strings.Split(value, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		tank, module, ok := strings.Cut(entry, "/")
		t, tankErr := strconv.Atoi(tank)
		m, moduleErr := strconv.Atoi(module)
		if !ok || tankErr != nil || moduleErr != nil || t < 1 || m < 1 {
			return nil, fmt.Errorf("invalid PSU firmware fault module %q; expected tank/module, for example 1/2", entry)
		}
		out = append(out, [2]int{t, m})
	}
	return out, nil
}

// writePSUCapsule writes a capsule the simulator accepts, for local runs.
// It carries placeholder images and must never be sent to real hardware.
func writePSUCapsule(path, version, revision string) error {
	data, err := psucaptest.Build(psucaptest.Options{Version: version, Revision: revision})
	if err != nil {
		return fmt.Errorf("build PSU firmware capsule: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write PSU firmware capsule: %w", err)
	}
	return nil
}

func (a *grpcAPI) UploadPSUFirmwareCapsule(stream mdk.PSUFirmwareService_UploadPSUFirmwareCapsuleServer) error {
	first, err := stream.Recv()
	if errors.Is(err, io.EOF) {
		return status.Error(codes.InvalidArgument, "upload metadata is required")
	}
	if err != nil {
		return err
	}
	meta := first.GetMetadata()
	if meta == nil {
		return status.Error(codes.InvalidArgument, "first upload message must contain metadata")
	}
	name := strings.TrimSpace(meta.GetFileName())
	if len(name) > 255 || strings.ContainsAny(name, "/\\\x00") || name == "." || name == ".." {
		return status.Error(codes.InvalidArgument, "invalid PSU firmware capsule: file_name must be a single path component of at most 255 bytes")
	}
	if meta.GetSizeBytes() < psucap.MinSize || meta.GetSizeBytes() > psucap.MaxSize {
		return status.Errorf(codes.InvalidArgument, "invalid PSU firmware capsule: size_bytes is %d; expected %d..%d", meta.GetSizeBytes(), psucap.MinSize, psucap.MaxSize)
	}
	expected := strings.ToLower(strings.TrimSpace(meta.GetSha256()))
	if decoded, err := hex.DecodeString(expected); expected != "" && (err != nil || len(decoded) != sha256.Size) {
		return status.Error(codes.InvalidArgument, "invalid PSU firmware capsule: sha256 must be 64 lowercase hexadecimal characters")
	}
	data := make([]byte, 0, meta.GetSizeBytes())
	for {
		message, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		if message.GetMetadata() != nil {
			return status.Error(codes.InvalidArgument, "upload metadata must appear exactly once")
		}
		chunk := message.GetChunk()
		if len(chunk) == 0 {
			return status.Error(codes.InvalidArgument, "upload chunks must not be empty")
		}
		if len(chunk) > psucap.ChunkBytes {
			return status.Errorf(codes.ResourceExhausted, "upload chunk exceeds %d-byte limit", psucap.ChunkBytes)
		}
		if int64(len(data)+len(chunk)) > meta.GetSizeBytes() {
			return status.Errorf(codes.InvalidArgument, "invalid PSU firmware capsule: upload exceeds declared size of %d bytes", meta.GetSizeBytes())
		}
		data = append(data, chunk...)
	}
	if int64(len(data)) != meta.GetSizeBytes() {
		return status.Errorf(codes.InvalidArgument, "invalid PSU firmware capsule: received %d of %d declared bytes", len(data), meta.GetSizeBytes())
	}
	capsule, err := psucap.Verify(data)
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "invalid PSU firmware capsule: %v", err)
	}
	if expected != "" && expected != capsule.ID {
		return status.Errorf(codes.InvalidArgument, "invalid PSU firmware capsule: SHA-256 is %s; expected %s", capsule.ID, expected)
	}
	// Frigate accepts BOCO capsules signed by a firmware signing leaf only.
	if capsule.Vendor != "BOCO" || (capsule.SignerSubject != "prod-leaf" && capsule.SignerSubject != "dev-leaf") {
		return status.Errorf(codes.InvalidArgument, "invalid PSU firmware capsule: unsupported vendor %s or signer %q", capsule.Vendor, capsule.SignerSubject)
	}

	a.state.mu.Lock()
	defer a.state.mu.Unlock()
	fw := a.state.psuFirmware
	cached := fw.capsules[capsule.ID]
	if cached == nil {
		if len(fw.capsules) >= maxSimulatedCapsules {
			return status.Errorf(codes.ResourceExhausted, "PSU firmware capsule cache is full: at most %d capsules may be cached", maxSimulatedCapsules)
		}
		info := &mdk.PSUFirmwareCapsule{
			CapsuleId: capsule.ID, SizeBytes: capsule.Size, Vendor: capsule.Vendor,
			ManufacturerId: capsule.ManufacturerID, ManufacturerModel: capsule.ManufacturerModel, ManufacturerRevision: capsule.ManufacturerRevision,
			SignerSubject: capsule.SignerSubject, FileName: name, UploadedAt: timestamppb.New(a.state.now()),
		}
		last := capsule.Components[len(capsule.Components)-1]
		for _, component := range capsule.Components {
			info.Components = append(info.Components, &mdk.PSUFirmwareComponent{
				Role:     mdk.PSUFirmwareComponentRole(mdk.PSUFirmwareComponentRole_value["PSU_FIRMWARE_COMPONENT_ROLE_"+strings.ToUpper(component.Role)]),
				TargetId: uint32(component.TargetID), ImageSizeBytes: uint32(len(component.Image)), Sha256: component.SHA256,
			})
		}
		// Real images name no version. Placeholder images do; any other image
		// installs a version derived from its digest.
		version := psucaptest.SimulatedVersion(last.Image)
		if version == "" {
			version = "app=" + last.SHA256[:4] + ";bl=1110"
		}
		cached = &simulatedCapsule{info: info, version: version}
		fw.capsules[capsule.ID] = cached
	}
	return stream.SendAndClose(&mdk.UploadPSUFirmwareCapsuleResponse{Capsule: proto.Clone(cached.info).(*mdk.PSUFirmwareCapsule)})
}

func (a *grpcAPI) ListPSUFirmwareCapsules(context.Context, *mdk.ListPSUFirmwareCapsulesRequest) (*mdk.ListPSUFirmwareCapsulesResponse, error) {
	a.state.mu.Lock()
	defer a.state.mu.Unlock()
	out := &mdk.ListPSUFirmwareCapsulesResponse{}
	for _, capsule := range a.state.psuFirmware.capsules {
		out.Capsules = append(out.Capsules, proto.Clone(capsule.info).(*mdk.PSUFirmwareCapsule))
	}
	sort.Slice(out.Capsules, func(i, j int) bool {
		if a, b := out.Capsules[i].GetUploadedAt().AsTime(), out.Capsules[j].GetUploadedAt().AsTime(); !a.Equal(b) {
			return a.After(b)
		}
		return out.Capsules[i].GetCapsuleId() < out.Capsules[j].GetCapsuleId()
	})
	return out, nil
}

func (a *grpcAPI) GetPSUFirmwareBoardStatus(_ context.Context, req *mdk.GetPSUFirmwareBoardStatusRequest) (*mdk.GetPSUFirmwareBoardStatusResponse, error) {
	a.state.mu.Lock()
	defer a.state.mu.Unlock()
	s := a.state
	s.advancePSUFirmware()
	board := s.boardsByMAC[strings.ToUpper(strings.TrimSpace(req.GetMacAddress()))]
	if board == nil || board.State == miningStateDisconnected {
		return nil, status.Error(codes.NotFound, "hashboard is not managed by this Frigate node")
	}
	module := s.modulesByID[board.ModuleID]
	return &mdk.GetPSUFirmwareBoardStatusResponse{Status: s.psuBoardStatus(module, board, s.psuLeader(module) == board)}, nil
}

func (a *grpcAPI) StartPSUFirmwareUpdate(_ context.Context, req *mdk.StartPSUFirmwareUpdateRequest) (*mdk.StartPSUFirmwareUpdateResponse, error) {
	a.state.mu.Lock()
	defer a.state.mu.Unlock()
	s := a.state
	fw := s.psuFirmware
	// Real module IDs are 16 hexadecimal characters; the simulator names
	// modules by position.
	moduleID := strings.TrimSpace(req.GetModuleId())
	if moduleID == "" {
		return nil, status.Error(codes.InvalidArgument, "module_id is required")
	}
	capsuleID := strings.ToLower(strings.TrimSpace(req.GetCapsuleId()))
	if capsuleID == "" {
		return nil, status.Error(codes.InvalidArgument, "capsule_id is required")
	}
	if fw.capsules[capsuleID] == nil {
		return nil, status.Error(codes.NotFound, "PSU firmware capsule not found")
	}
	if req.GetForce() && req.GetRecover() {
		return nil, status.Error(codes.InvalidArgument, "force and recover are mutually exclusive")
	}
	module := s.modulesByID[moduleID]
	if module == nil || len(moduleBoards(module)) == 0 {
		return nil, status.Error(codes.NotFound, "PSU module not found")
	}
	s.advancePSUFirmware()
	for _, id := range fw.order {
		if job := fw.jobs[id]; job.GetModuleId() == moduleID && !psuJobTerminal(job.GetState()) {
			return nil, status.Errorf(codes.AlreadyExists, "PSU module already has an active PSU firmware update: task %s", id)
		}
	}
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return nil, status.Errorf(codes.Unavailable, "generating task ID: %v", err)
	}
	job := &mdk.PSUFirmwareUpdate{
		TaskId: "psufw-" + hex.EncodeToString(raw[:]), ServerId: s.ServerID, ModuleId: moduleID, CapsuleId: capsuleID,
		Force: req.GetForce(), Recover: req.GetRecover(), State: mdk.PSUFirmwareUpdateState_PSU_FIRMWARE_UPDATE_STATE_QUEUED,
		CreatedAt: timestamppb.New(s.now()),
	}
	for _, board := range moduleBoards(module) {
		job.Members = append(job.Members, &mdk.PSUFirmwareUpdateMember{MacAddress: strings.ToLower(board.MAC)})
	}
	fw.jobs[job.TaskId] = job
	fw.order = append(fw.order, job.TaskId)
	for i := 0; len(fw.order) > maxSimulatedPSUJobs && i < len(fw.order); {
		if id := fw.order[i]; psuJobTerminal(fw.jobs[id].GetState()) {
			delete(fw.jobs, id)
			fw.order = append(fw.order[:i], fw.order[i+1:]...)
			continue
		}
		i++
	}
	queued := proto.Clone(job).(*mdk.PSUFirmwareUpdate)
	s.advancePSUFirmware()
	return &mdk.StartPSUFirmwareUpdateResponse{Update: queued}, nil
}

func (a *grpcAPI) GetPSUFirmwareUpdate(_ context.Context, req *mdk.GetPSUFirmwareUpdateRequest) (*mdk.GetPSUFirmwareUpdateResponse, error) {
	a.state.mu.Lock()
	defer a.state.mu.Unlock()
	a.state.advancePSUFirmware()
	job := a.state.psuFirmware.jobs[req.GetTaskId()]
	if job == nil {
		return nil, status.Error(codes.NotFound, "PSU firmware update not found")
	}
	return &mdk.GetPSUFirmwareUpdateResponse{Update: proto.Clone(job).(*mdk.PSUFirmwareUpdate)}, nil
}

func (a *grpcAPI) ListPSUFirmwareUpdates(context.Context, *mdk.ListPSUFirmwareUpdatesRequest) (*mdk.ListPSUFirmwareUpdatesResponse, error) {
	a.state.mu.Lock()
	defer a.state.mu.Unlock()
	a.state.advancePSUFirmware()
	fw := a.state.psuFirmware
	out := &mdk.ListPSUFirmwareUpdatesResponse{}
	for i := len(fw.order) - 1; i >= 0; i-- {
		out.Updates = append(out.Updates, proto.Clone(fw.jobs[fw.order[i]]).(*mdk.PSUFirmwareUpdate))
	}
	return out, nil
}

func (a *grpcAPI) CancelPSUFirmwareUpdate(_ context.Context, req *mdk.CancelPSUFirmwareUpdateRequest) (*mdk.CancelPSUFirmwareUpdateResponse, error) {
	a.state.mu.Lock()
	defer a.state.mu.Unlock()
	s := a.state
	s.advancePSUFirmware()
	job := s.psuFirmware.jobs[req.GetTaskId()]
	if job == nil {
		return nil, status.Error(codes.NotFound, "PSU firmware update not found")
	}
	if !psuJobTerminal(job.GetState()) {
		if job.GetApplyRequested() {
			return nil, status.Error(codes.FailedPrecondition, "PSU firmware update can no longer be cancelled")
		}
		message := "cancelled before start"
		switch job.GetState() { //nolint:exhaustive // Only pre-apply states can be cancelled.
		case mdk.PSUFirmwareUpdateState_PSU_FIRMWARE_UPDATE_STATE_PREPARING:
			message = "stopping module mining: cancelled"
		case mdk.PSUFirmwareUpdateState_PSU_FIRMWARE_UPDATE_STATE_STAGING:
			message = "staging capsule: cancelled"
		case mdk.PSUFirmwareUpdateState_PSU_FIRMWARE_UPDATE_STATE_VALIDATING:
			message = "validating capsule: cancelled"
		}
		s.finishPSUJob(job, s.now(), mdk.PSUFirmwareUpdateState_PSU_FIRMWARE_UPDATE_STATE_CANCELLED, message, "")
		s.advancePSUFirmware()
	}
	return &mdk.CancelPSUFirmwareUpdateResponse{Update: proto.Clone(job).(*mdk.PSUFirmwareUpdate)}, nil
}

func psuJobTerminal(state mdk.PSUFirmwareUpdateState) bool {
	return state == mdk.PSUFirmwareUpdateState_PSU_FIRMWARE_UPDATE_STATE_SUCCEEDED ||
		state == mdk.PSUFirmwareUpdateState_PSU_FIRMWARE_UPDATE_STATE_FAILED ||
		state == mdk.PSUFirmwareUpdateState_PSU_FIRMWARE_UPDATE_STATE_CANCELLED
}

// psuLeader is the connected board that controls the module's PSU.
func (s *ContainerState) psuLeader(module *Module) *Hashboard {
	for _, board := range moduleBoards(module) {
		if board.State != miningStateDisconnected {
			return board
		}
	}
	return nil
}

func (s *ContainerState) psuBoardStatus(module *Module, board *Hashboard, leader bool) *mdk.PSUFirmwareBoardStatus {
	journal := &module.psu
	out := &mdk.PSUFirmwareBoardStatus{
		MacAddress: strings.ToLower(board.MAC), Phase: journal.phase, Error: journal.failure, CapsuleId: journal.capsuleID,
		RecoveryRequired: journal.recoveryRequired, PsuLeader: leader,
	}
	if out.Phase == mdk.PSUFirmwarePhase_PSU_FIRMWARE_PHASE_UNSPECIFIED {
		out.Phase = mdk.PSUFirmwarePhase_PSU_FIRMWARE_PHASE_EMPTY
	}
	if out.Error == mdk.PSUFirmwareError_PSU_FIRMWARE_ERROR_UNSPECIFIED {
		out.Error = mdk.PSUFirmwareError_PSU_FIRMWARE_ERROR_NONE
	}
	if capsule := s.psuFirmware.capsules[journal.capsuleID]; capsule != nil {
		out.TotalSizeBytes = uint32(capsule.info.GetSizeBytes())
	}
	for _, part := range strings.Split(module.PSUFirmwareVersion, ";") {
		if revision, ok := strings.CutPrefix(part, "app="); ok {
			out.CurrentAppRevision = revision
		} else if revision, ok := strings.CutPrefix(part, "bl="); ok {
			out.CurrentBootRevision = revision
		}
	}
	return out
}

// advancePSUFirmware moves every job to its state at the current time. One
// job runs at a time; the next starts when the one before it ends. Caller
// holds mu for writing.
func (s *ContainerState) advancePSUFirmware() {
	fw := s.psuFirmware
	now := s.now()
	var free time.Time
	for _, id := range fw.order {
		job := fw.jobs[id]
		if !psuJobTerminal(job.GetState()) {
			if job.StartedAt == nil {
				start := job.GetCreatedAt().AsTime()
				if free.After(start) {
					start = free
				}
				s.startPSUJob(job, start)
			}
			if !psuJobTerminal(job.GetState()) {
				s.runPSUJob(job, now)
			}
			if !psuJobTerminal(job.GetState()) {
				return
			}
		}
		if job.StartedAt != nil && job.GetCompletedAt().AsTime().After(free) {
			free = job.GetCompletedAt().AsTime()
		}
	}
}

// startPSUJob reserves the module, runs the checks Frigate makes while the
// module still mines, and then stops mining.
func (s *ContainerState) startPSUJob(job *mdk.PSUFirmwareUpdate, start time.Time) {
	failed := mdk.PSUFirmwareUpdateState_PSU_FIRMWARE_UPDATE_STATE_FAILED
	module := s.modulesByID[job.GetModuleId()]
	job.StartedAt = timestamppb.New(start)
	job.State = mdk.PSUFirmwareUpdateState_PSU_FIRMWARE_UPDATE_STATE_PREPARING
	for _, member := range job.Members {
		board := s.boardsByMAC[strings.ToUpper(member.GetMacAddress())]
		if board == nil || board.State == miningStateDisconnected {
			continue
		}
		if s.boardBeingFlashed(board.MAC) {
			for _, reserved := range job.Members {
				reserved.Reserved = false
			}
			s.finishPSUJob(job, start, failed, fmt.Sprintf("reserving module members: %s is reserved by a hashboard firmware update", member.GetMacAddress()), "")
			return
		}
		member.Reserved = true
		member.MiningBefore = board.isMining() || module.psu.miningBefore[board.MAC]
	}
	leader := s.psuLeader(module)
	if leader == nil {
		s.finishPSUJob(job, start, failed, "resolving PSU leader: no reserved member reported valid PSU leadership", "")
		return
	}
	job.LeaderMacAddress = strings.ToLower(leader.MAC)
	journal := &module.psu
	switch {
	case journal.recoveryRequired && !job.GetRecover():
		s.finishPSUJob(job, start, failed, job.LeaderMacAddress+" reports a PSU that needs recovery; re-run with recover", psuRecoveryHold)
		return
	case job.GetRecover() && !journal.recoveryRequired:
		job.LeaderStatus = s.psuBoardStatus(module, leader, true)
		s.finishPSUJob(job, start, failed, "recovery was requested, but the leader is "+job.LeaderStatus.GetPhase().String(), "")
		return
	case job.GetRecover() && journal.capsuleID != job.GetCapsuleId():
		job.LeaderStatus = s.psuBoardStatus(module, leader, true)
		s.finishPSUJob(job, start, failed, fmt.Sprintf("leader status is for capsule %s; expected %s", journal.capsuleID, job.GetCapsuleId()), psuRecoveryHold)
		return
	}
	for _, board := range moduleBoards(module) {
		board.PSUFirmwareHold = true
		if board.State == miningStateMining {
			board.State = miningStateIdle
		}
	}
}

func (s *ContainerState) runPSUJob(job *mdk.PSUFirmwareUpdate, now time.Time) {
	module := s.modulesByID[job.GetModuleId()]
	capsule := s.psuFirmware.capsules[job.GetCapsuleId()]
	journal := &module.psu
	leader := s.boardsByMAC[strings.ToUpper(job.GetLeaderMacAddress())]
	started := job.GetStartedAt().AsTime()
	elapsed := now.Sub(started)
	total := uint32(capsule.info.GetSizeBytes())
	leaderStatus := func(phase mdk.PSUFirmwarePhase, staged, programmed uint32) {
		job.LeaderStatus = s.psuBoardStatus(module, leader, true)
		job.LeaderStatus.Phase, job.LeaderStatus.CapsuleId = phase, job.GetCapsuleId()
		job.LeaderStatus.TotalSizeBytes, job.LeaderStatus.StagedBytes, job.LeaderStatus.ProgrammedBytes = total, staged, programmed
	}
	fraction := func(since, duration time.Duration) uint32 {
		return uint32(float64(total) * min(1, max(0, float64(elapsed-since)/float64(duration))))
	}
	// CT1_PSU_FIRMWARE_SPEED shortens every phase by the same factor.
	speed := time.Duration(max(1, s.Config.PSUFirmwareSpeed))
	prepare, stage, apply, fault := simulatedPSUPrepare/speed, simulatedPSUStage/speed, simulatedPSUApply/speed, simulatedPSUFault/speed
	validated := prepare + stage + simulatedPSUValidate/speed
	switch {
	case elapsed < prepare:
		return
	case elapsed < prepare+stage:
		job.State = mdk.PSUFirmwareUpdateState_PSU_FIRMWARE_UPDATE_STATE_STAGING
		leaderStatus(mdk.PSUFirmwarePhase_PSU_FIRMWARE_PHASE_STAGING, fraction(prepare, stage), 0)
		return
	case elapsed < validated:
		job.State = mdk.PSUFirmwareUpdateState_PSU_FIRMWARE_UPDATE_STATE_VALIDATING
		leaderStatus(mdk.PSUFirmwarePhase_PSU_FIRMWARE_PHASE_VALIDATING, total, 0)
		return
	}

	succeeded := mdk.PSUFirmwareUpdateState_PSU_FIRMWARE_UPDATE_STATE_SUCCEEDED
	failed := mdk.PSUFirmwareUpdateState_PSU_FIRMWARE_UPDATE_STATE_FAILED
	current := journal.capsuleID == job.GetCapsuleId() && journal.phase == mdk.PSUFirmwarePhase_PSU_FIRMWARE_PHASE_SUCCEEDED
	if current && !job.GetForce() && !job.GetRecover() {
		// The PSU already runs this capsule, so the job ends without apply.
		leaderStatus(mdk.PSUFirmwarePhase_PSU_FIRMWARE_PHASE_SUCCEEDED, total, 0)
		s.finishPSUJob(job, started.Add(validated), succeeded, "", "")
		return
	}
	job.ApplyRequested = true
	job.State = mdk.PSUFirmwareUpdateState_PSU_FIRMWARE_UPDATE_STATE_APPLYING
	journal.capsuleID = job.GetCapsuleId()
	if capsule.info.GetManufacturerRevision() != module.PSURevision {
		// Board firmware refuses a capsule for another PSU before changing it.
		journal.phase, journal.failure = mdk.PSUFirmwarePhase_PSU_FIRMWARE_PHASE_FAILED, mdk.PSUFirmwareError_PSU_FIRMWARE_ERROR_TARGET_MISMATCH
		leaderStatus(journal.phase, total, 0)
		s.finishPSUJob(job, started.Add(validated), failed, "PSU firmware update failed in PSU_FW_PHASE_FAILED: PSU_FW_ERROR_TARGET_MISMATCH", "")
		return
	}
	if s.psuFirmware.faults[module.ModuleID] > 0 {
		if elapsed < validated+fault {
			leaderStatus(mdk.PSUFirmwarePhase_PSU_FIRMWARE_PHASE_PROGRAMMING, total, fraction(validated, apply))
			return
		}
		s.psuFirmware.faults[module.ModuleID]--
		journal.phase, journal.failure, journal.recoveryRequired = mdk.PSUFirmwarePhase_PSU_FIRMWARE_PHASE_RECOVERY_REQUIRED, mdk.PSUFirmwareError_PSU_FIRMWARE_ERROR_I2C, true
		leaderStatus(journal.phase, total, total/2)
		s.finishPSUJob(job, started.Add(validated+fault), failed, "PSU firmware update failed in PSU_FW_PHASE_RECOVERY_REQUIRED: PSU_FW_ERROR_I2C", psuRecoveryHold)
		return
	}
	if elapsed < validated+apply {
		leaderStatus(mdk.PSUFirmwarePhase_PSU_FIRMWARE_PHASE_PROGRAMMING, total, fraction(validated, apply))
		return
	}
	module.PSUFirmwareVersion = capsule.version
	journal.phase, journal.failure, journal.recoveryRequired = mdk.PSUFirmwarePhase_PSU_FIRMWARE_PHASE_SUCCEEDED, mdk.PSUFirmwareError_PSU_FIRMWARE_ERROR_NONE, false
	leaderStatus(journal.phase, total, total)
	s.finishPSUJob(job, started.Add(validated+apply), succeeded, "", "")
}

// finishPSUJob records a terminal state and settles the module's mining
// hold: a nonempty hold keeps the module stopped, a success lifts an earlier
// hold, and any other outcome leaves earlier holds in place.
func (s *ContainerState) finishPSUJob(job *mdk.PSUFirmwareUpdate, at time.Time, state mdk.PSUFirmwareUpdateState, message, hold string) {
	job.State, job.Error, job.CompletedAt = state, message, timestamppb.New(at)
	module := s.modulesByID[job.GetModuleId()]
	journal := &module.psu
	if hold != "" {
		if journal.miningBefore == nil {
			journal.miningBefore = map[string]bool{}
		}
		journal.hold = true
	} else if state == mdk.PSUFirmwareUpdateState_PSU_FIRMWARE_UPDATE_STATE_SUCCEEDED {
		journal.hold, journal.miningBefore = false, nil
	}
	for _, member := range job.Members {
		board := s.boardsByMAC[strings.ToUpper(member.GetMacAddress())]
		if board == nil {
			continue
		}
		board.PSUFirmwareHold = journal.hold
		if !member.GetReserved() || !member.GetMiningBefore() {
			continue
		}
		if journal.hold {
			if hold != "" {
				journal.miningBefore[board.MAC] = true
			}
			member.RestoreError = psuRecoveryHold
			continue
		}
		if !board.Isolated && board.State == miningStateIdle {
			board.State = miningStateMining
		}
	}
}

// boardBeingFlashed reports whether a running hashboard firmware job targets mac.
func (s *ContainerState) boardBeingFlashed(mac string) bool {
	for _, job := range s.firmware.jobs {
		s.advanceFirmware(job)
		if job.CompletedAt != nil {
			continue
		}
		for _, target := range job.Targets {
			if target.MacAddress == mac {
				return true
			}
		}
	}
	return false
}

// boardInPSUUpdate reports whether a module update that has not ended holds mac.
func (s *ContainerState) boardInPSUUpdate(mac string) bool {
	s.advancePSUFirmware()
	for _, job := range s.psuFirmware.jobs {
		if psuJobTerminal(job.GetState()) || job.StartedAt == nil {
			continue
		}
		for _, member := range job.Members {
			if strings.EqualFold(member.GetMacAddress(), mac) {
				return true
			}
		}
	}
	return false
}
