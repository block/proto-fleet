package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/block/proto-fleet/sdk/mdk/firmware/firmwaretest"
	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	"github.com/block/proto-fleet/sdk/mdk/psucap/psucaptest"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

const (
	psuJobQueued    = mdk.PSUFirmwareUpdateState_PSU_FIRMWARE_UPDATE_STATE_QUEUED
	psuJobPreparing = mdk.PSUFirmwareUpdateState_PSU_FIRMWARE_UPDATE_STATE_PREPARING
	psuJobApplying  = mdk.PSUFirmwareUpdateState_PSU_FIRMWARE_UPDATE_STATE_APPLYING
	psuJobSucceeded = mdk.PSUFirmwareUpdateState_PSU_FIRMWARE_UPDATE_STATE_SUCCEEDED
	psuJobFailed    = mdk.PSUFirmwareUpdateState_PSU_FIRMWARE_UPDATE_STATE_FAILED
	psuJobCancelled = mdk.PSUFirmwareUpdateState_PSU_FIRMWARE_UPDATE_STATE_CANCELLED

	simulatedPSUTotal = simulatedPSUPrepare + simulatedPSUStage + simulatedPSUValidate + simulatedPSUApply
)

type psuSimulator struct {
	t       *testing.T
	state   *ContainerState
	clock   time.Time
	conn    *grpc.ClientConn
	psu     mdk.PSUFirmwareServiceClient
	modules []string
}

func newPSUSimulator(t *testing.T, cfg ContainerConfig) *psuSimulator {
	t.Helper()
	cfg.ServerID, cfg.Tanks, cfg.SlotsPerModule, cfg.Seed = "psu-sim", 1, 3, 1
	if cfg.ModulesPerTank == 0 {
		cfg.ModulesPerTank = 2
	}
	sim := &psuSimulator{t: t, state: NewContainerState(cfg), clock: time.Now()}
	sim.state.now = func() time.Time { return sim.clock }
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := newGRPCServer(sim.state)
	t.Cleanup(server.Stop)
	go func() { _ = server.Serve(listener) }()
	sim.conn, err = grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sim.conn.Close() })
	sim.psu = mdk.NewPSUFirmwareServiceClient(sim.conn)
	for _, module := range sim.state.Modules {
		sim.modules = append(sim.modules, module.ModuleID)
	}
	return sim
}

func (sim *psuSimulator) advance(d time.Duration) {
	sim.state.mu.Lock()
	sim.clock = sim.clock.Add(d)
	sim.state.mu.Unlock()
}

func (sim *psuSimulator) upload(options psucaptest.Options) *mdk.PSUFirmwareCapsule {
	sim.t.Helper()
	data, err := psucaptest.Build(options)
	if err != nil {
		sim.t.Fatal(err)
	}
	capsule, err := sim.uploadBytes(data, fmt.Sprintf("%x", sha256.Sum256(data)))
	if err != nil {
		sim.t.Fatal(err)
	}
	return capsule
}

func (sim *psuSimulator) uploadBytes(data []byte, digest string) (*mdk.PSUFirmwareCapsule, error) {
	stream, err := sim.psu.UploadPSUFirmwareCapsule(sim.t.Context())
	if err != nil {
		return nil, err
	}
	meta := &mdk.PSUFirmwareCapsuleUploadMetadata{FileName: "psu.psucap", SizeBytes: int64(len(data)), Sha256: digest}
	if err := stream.Send(&mdk.UploadPSUFirmwareCapsuleRequest{Payload: &mdk.UploadPSUFirmwareCapsuleRequest_Metadata{Metadata: meta}}); err != nil {
		return nil, err
	}
	if len(data) > 0 {
		if err := stream.Send(&mdk.UploadPSUFirmwareCapsuleRequest{Payload: &mdk.UploadPSUFirmwareCapsuleRequest_Chunk{Chunk: data}}); err != nil {
			return nil, err
		}
	}
	response, err := stream.CloseAndRecv()
	return response.GetCapsule(), err
}

func (sim *psuSimulator) start(request *mdk.StartPSUFirmwareUpdateRequest) *mdk.PSUFirmwareUpdate {
	sim.t.Helper()
	response, err := sim.psu.StartPSUFirmwareUpdate(sim.t.Context(), request)
	if err != nil {
		sim.t.Fatal(err)
	}
	return response.GetUpdate()
}

func (sim *psuSimulator) job(taskID string) *mdk.PSUFirmwareUpdate {
	sim.t.Helper()
	response, err := sim.psu.GetPSUFirmwareUpdate(sim.t.Context(), &mdk.GetPSUFirmwareUpdateRequest{TaskId: taskID})
	if err != nil {
		sim.t.Fatal(err)
	}
	return response.GetUpdate()
}

// mining reports how many of a module's boards are hashing.
func (sim *psuSimulator) mining(moduleID string) int {
	sim.state.mu.Lock()
	defer sim.state.mu.Unlock()
	count := 0
	for _, board := range moduleBoards(sim.state.modulesByID[moduleID]) {
		if board.isMining() {
			count++
		}
	}
	return count
}

func (sim *psuSimulator) version(moduleID string) string {
	sim.state.mu.Lock()
	defer sim.state.mu.Unlock()
	return sim.state.modulesByID[moduleID].PSUFirmwareVersion
}

func TestPSUFirmwareSimulatorRunsOneModuleAtATime(t *testing.T) {
	sim := newPSUSimulator(t, ContainerConfig{})
	capsule := sim.upload(psucaptest.Options{Version: "app=1410;bl=1110"})
	if capsule.GetVendor() != "BOCO" || capsule.GetManufacturerRevision() != defaultPSURevision || capsule.GetFileName() != "psu.psucap" ||
		len(capsule.GetComponents()) != 1 || capsule.GetComponents()[0].GetRole() != mdk.PSUFirmwareComponentRole_PSU_FIRMWARE_COMPONENT_ROLE_SECONDARY {
		t.Fatalf("unexpected capsule: %v", capsule)
	}

	first := sim.start(&mdk.StartPSUFirmwareUpdateRequest{CapsuleId: capsule.CapsuleId, ModuleId: sim.modules[0]})
	second := sim.start(&mdk.StartPSUFirmwareUpdateRequest{CapsuleId: capsule.CapsuleId, ModuleId: sim.modules[1]})
	if first.GetState() != psuJobQueued || !strings.HasPrefix(first.GetTaskId(), "psufw-") || first.GetServerId() != "psu-sim" || len(first.GetMembers()) != 3 {
		t.Fatalf("unexpected start response: %v", first)
	}
	if got := sim.job(first.TaskId); got.GetState() != psuJobPreparing || sim.mining(sim.modules[0]) != 0 {
		t.Fatalf("first module should stop mining while it updates: %v", got)
	}
	if got := sim.job(second.TaskId); got.GetState() != psuJobQueued || got.StartedAt != nil || sim.mining(sim.modules[1]) != 3 {
		t.Fatalf("second module must keep mining while it waits: %v", got)
	}
	if _, err := sim.psu.StartPSUFirmwareUpdate(t.Context(), &mdk.StartPSUFirmwareUpdateRequest{CapsuleId: capsule.CapsuleId, ModuleId: sim.modules[0]}); status.Code(err) != codes.AlreadyExists || !strings.Contains(err.Error(), first.TaskId) {
		t.Fatalf("a busy module must name its task, got %v", err)
	}

	sim.advance(simulatedPSUPrepare + simulatedPSUStage + simulatedPSUValidate + simulatedPSUApply/2)
	applying := sim.job(first.TaskId)
	if applying.GetState() != psuJobApplying || !applying.GetApplyRequested() || applying.GetLeaderStatus().GetProgrammedBytes() == 0 ||
		applying.GetLeaderStatus().GetPhase() != mdk.PSUFirmwarePhase_PSU_FIRMWARE_PHASE_PROGRAMMING {
		t.Fatalf("unexpected applying job: %v", applying)
	}
	if _, err := sim.psu.CancelPSUFirmwareUpdate(t.Context(), &mdk.CancelPSUFirmwareUpdateRequest{TaskId: first.TaskId}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("an applying job cannot be cancelled, got %v", err)
	}

	// The second job starts when the first ends, however late it is observed.
	sim.advance(simulatedPSUTotal)
	done := sim.job(first.TaskId)
	if done.GetState() != psuJobSucceeded || done.GetError() != "" || sim.version(sim.modules[0]) != "app=1410;bl=1110" || sim.mining(sim.modules[0]) != 3 {
		t.Fatalf("unexpected finished job: %v", done)
	}
	running := sim.job(second.TaskId)
	if !running.GetStartedAt().AsTime().Equal(done.GetCompletedAt().AsTime()) || running.GetState() != psuJobApplying {
		t.Fatalf("second job should have started at %v: %v", done.GetCompletedAt().AsTime(), running)
	}
	sim.advance(simulatedPSUTotal)
	if got := sim.job(second.TaskId); got.GetState() != psuJobSucceeded || sim.version(sim.modules[1]) != "app=1410;bl=1110" {
		t.Fatalf("unexpected second job: %v", got)
	}

	// A capsule the PSU already runs ends without apply unless forced.
	again := sim.start(&mdk.StartPSUFirmwareUpdateRequest{CapsuleId: capsule.CapsuleId, ModuleId: sim.modules[0]})
	sim.advance(simulatedPSUTotal)
	if got := sim.job(again.TaskId); got.GetState() != psuJobSucceeded || got.GetApplyRequested() {
		t.Fatalf("a current capsule must not be applied again: %v", got)
	}
	forced := sim.start(&mdk.StartPSUFirmwareUpdateRequest{CapsuleId: capsule.CapsuleId, ModuleId: sim.modules[0], Force: true})
	sim.advance(simulatedPSUTotal)
	if got := sim.job(forced.TaskId); got.GetState() != psuJobSucceeded || !got.GetApplyRequested() {
		t.Fatalf("a forced job must apply: %v", got)
	}

	list, err := sim.psu.ListPSUFirmwareUpdates(t.Context(), &mdk.ListPSUFirmwareUpdatesRequest{})
	if err != nil || len(list.GetUpdates()) != 4 || list.GetUpdates()[0].GetTaskId() != forced.TaskId {
		t.Fatalf("jobs must list newest first: %v, %v", list, err)
	}
	layout, err := mdk.NewHashboardServiceClient(sim.conn).GetContainerMap(t.Context(), &mdk.GetContainerMapRequest{})
	if err != nil {
		t.Fatal(err)
	}
	identity := layout.GetContainerMap().GetTanks()[0].GetModules()[0].GetSlots()[0].GetHashboard().GetPsuIdentity()
	if identity.GetFirmwareVersion() != "app=1410;bl=1110" || identity.GetRevision() != defaultPSURevision {
		t.Fatalf("the container map must report the PSU firmware: %v", identity)
	}
}

func TestPSUFirmwareSimulatorRecoversAFailedProgram(t *testing.T) {
	sim := newPSUSimulator(t, ContainerConfig{PSUFirmwareFaults: [][2]int{{1, 1}}})
	capsule := sim.upload(psucaptest.Options{})
	module := sim.modules[0]

	failed := sim.start(&mdk.StartPSUFirmwareUpdateRequest{CapsuleId: capsule.CapsuleId, ModuleId: module})
	sim.advance(simulatedPSUTotal)
	job := sim.job(failed.TaskId)
	if job.GetState() != psuJobFailed || !strings.Contains(job.GetError(), "RECOVERY_REQUIRED") || !job.GetLeaderStatus().GetRecoveryRequired() ||
		job.GetLeaderStatus().GetPhase() != mdk.PSUFirmwarePhase_PSU_FIRMWARE_PHASE_RECOVERY_REQUIRED || job.GetMembers()[0].GetRestoreError() != psuRecoveryHold {
		t.Fatalf("unexpected failed job: %v", job)
	}
	if sim.mining(module) != 0 || sim.version(module) != defaultPSUFirmwareVersion {
		t.Fatal("a PSU that needs recovery must stay stopped on its old version")
	}
	started, err := mdk.NewMiningServiceClient(sim.conn).StartMiningScoped(t.Context(), &mdk.StartMiningScopedRequest{Scope: &mdk.BoardScope{Identifiers: []string{job.GetMembers()[0].GetMacAddress()}}})
	if err != nil || started.GetBoardsStarted() != 0 || sim.mining(module) != 0 {
		t.Fatalf("a held module must refuse to mine: %v, %v", started, err)
	}
	boardStatus, err := sim.psu.GetPSUFirmwareBoardStatus(t.Context(), &mdk.GetPSUFirmwareBoardStatusRequest{MacAddress: job.GetLeaderMacAddress()})
	if err != nil || !boardStatus.GetStatus().GetRecoveryRequired() || boardStatus.GetStatus().GetCapsuleId() != capsule.CapsuleId ||
		!boardStatus.GetStatus().GetPsuLeader() || boardStatus.GetStatus().GetCurrentAppRevision() != "1310" || boardStatus.GetStatus().GetCurrentBootRevision() != "1110" {
		t.Fatalf("unexpected board status: %v, %v", boardStatus, err)
	}

	// A plain job is refused before it touches the module; recover finishes the update.
	plain := sim.start(&mdk.StartPSUFirmwareUpdateRequest{CapsuleId: capsule.CapsuleId, ModuleId: module})
	if got := sim.job(plain.TaskId); got.GetState() != psuJobFailed || !strings.Contains(got.GetError(), "re-run with recover") || got.GetApplyRequested() {
		t.Fatalf("unexpected plain job: %v", got)
	}
	other := sim.upload(psucaptest.Options{Version: "app=1510;bl=1110"})
	wrong := sim.start(&mdk.StartPSUFirmwareUpdateRequest{CapsuleId: other.CapsuleId, ModuleId: module, Recover: true})
	if got := sim.job(wrong.TaskId); got.GetState() != psuJobFailed || !strings.Contains(got.GetError(), "expected "+other.CapsuleId) {
		t.Fatalf("recovery needs the capsule that failed: %v", got)
	}
	recovered := sim.start(&mdk.StartPSUFirmwareUpdateRequest{CapsuleId: capsule.CapsuleId, ModuleId: module, Recover: true})
	sim.advance(simulatedPSUTotal)
	if got := sim.job(recovered.TaskId); got.GetState() != psuJobSucceeded || got.GetMembers()[0].GetRestoreError() != "" {
		t.Fatalf("unexpected recover job: %v", got)
	}
	if sim.mining(module) != 3 || sim.version(module) != psucaptest.DefaultVersion {
		t.Fatal("a recovered module must resume mining on the new version")
	}
	unneeded := sim.start(&mdk.StartPSUFirmwareUpdateRequest{CapsuleId: capsule.CapsuleId, ModuleId: module, Recover: true})
	if got := sim.job(unneeded.TaskId); got.GetState() != psuJobFailed || !strings.Contains(got.GetError(), "recovery was requested") || sim.mining(module) != 3 {
		t.Fatalf("recover on a healthy PSU must fail without stopping it: %v", got)
	}
}

func TestPSUFirmwareSimulatorRefusals(t *testing.T) {
	sim := newPSUSimulator(t, ContainerConfig{})
	capsule := sim.upload(psucaptest.Options{})
	if again := sim.upload(psucaptest.Options{}); again.GetCapsuleId() == capsule.GetCapsuleId() {
		t.Fatal("each built capsule is signed with its own key")
	}
	data, err := psucaptest.Build(psucaptest.Options{})
	if err != nil {
		t.Fatal(err)
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(data))
	stored, err := sim.uploadBytes(data, digest)
	if err != nil {
		t.Fatal(err)
	}
	sim.advance(time.Minute)
	if repeat, err := sim.uploadBytes(data, ""); err != nil || !repeat.GetUploadedAt().AsTime().Equal(stored.GetUploadedAt().AsTime()) {
		t.Fatalf("uploading a cached capsule must return it unchanged: %v, %v", repeat, err)
	}
	damaged := append([]byte(nil), data...)
	damaged[600] ^= 1
	for name, upload := range map[string]func() (*mdk.PSUFirmwareCapsule, error){
		"damaged":      func() (*mdk.PSUFirmwareCapsule, error) { return sim.uploadBytes(damaged, "") },
		"wrong digest": func() (*mdk.PSUFirmwareCapsule, error) { return sim.uploadBytes(data, strings.Repeat("0", 64)) },
		"not hex":      func() (*mdk.PSUFirmwareCapsule, error) { return sim.uploadBytes(data, "digest") },
		"too small":    func() (*mdk.PSUFirmwareCapsule, error) { return sim.uploadBytes([]byte("PSUCAP1"), "") },
	} {
		if _, err := upload(); status.Code(err) != codes.InvalidArgument {
			t.Fatalf("%s upload: got %v", name, err)
		}
	}
	capsules, err := sim.psu.ListPSUFirmwareCapsules(t.Context(), &mdk.ListPSUFirmwareCapsulesRequest{})
	if err != nil || len(capsules.GetCapsules()) != 3 {
		t.Fatalf("unexpected capsule list: %v, %v", capsules, err)
	}

	for name, tt := range map[string]struct {
		request *mdk.StartPSUFirmwareUpdateRequest
		want    codes.Code
	}{
		"no module":         {&mdk.StartPSUFirmwareUpdateRequest{CapsuleId: capsule.CapsuleId}, codes.InvalidArgument},
		"no capsule":        {&mdk.StartPSUFirmwareUpdateRequest{ModuleId: sim.modules[0]}, codes.InvalidArgument},
		"unknown module":    {&mdk.StartPSUFirmwareUpdateRequest{CapsuleId: capsule.CapsuleId, ModuleId: "0123456789abcdef"}, codes.NotFound},
		"unknown capsule":   {&mdk.StartPSUFirmwareUpdateRequest{CapsuleId: strings.Repeat("0", 64), ModuleId: sim.modules[0]}, codes.NotFound},
		"force and recover": {&mdk.StartPSUFirmwareUpdateRequest{CapsuleId: capsule.CapsuleId, ModuleId: sim.modules[0], Force: true, Recover: true}, codes.InvalidArgument},
	} {
		if _, err := sim.psu.StartPSUFirmwareUpdate(t.Context(), tt.request); status.Code(err) != tt.want {
			t.Fatalf("%s: got %v, want %s", name, err, tt.want)
		}
	}
	for name, call := range map[string]func() error{
		"get": func() error {
			_, err := sim.psu.GetPSUFirmwareUpdate(t.Context(), &mdk.GetPSUFirmwareUpdateRequest{TaskId: "psufw-missing"})
			return err
		},
		"cancel": func() error {
			_, err := sim.psu.CancelPSUFirmwareUpdate(t.Context(), &mdk.CancelPSUFirmwareUpdateRequest{TaskId: "psufw-missing"})
			return err
		},
		"board status": func() error {
			_, err := sim.psu.GetPSUFirmwareBoardStatus(t.Context(), &mdk.GetPSUFirmwareBoardStatusRequest{MacAddress: "02:00:00:00:00:01"})
			return err
		},
	} {
		if err := call(); status.Code(err) != codes.NotFound {
			t.Fatalf("%s: got %v", name, err)
		}
	}
}

func TestPSUFirmwareSimulatorCancelAndTargetMismatch(t *testing.T) {
	sim := newPSUSimulator(t, ContainerConfig{})
	capsule := sim.upload(psucaptest.Options{})
	running := sim.start(&mdk.StartPSUFirmwareUpdateRequest{CapsuleId: capsule.CapsuleId, ModuleId: sim.modules[0]})
	queued := sim.start(&mdk.StartPSUFirmwareUpdateRequest{CapsuleId: capsule.CapsuleId, ModuleId: sim.modules[1]})

	cancelled, err := sim.psu.CancelPSUFirmwareUpdate(t.Context(), &mdk.CancelPSUFirmwareUpdateRequest{TaskId: queued.TaskId})
	if err != nil || cancelled.GetUpdate().GetState() != psuJobCancelled || cancelled.GetUpdate().GetError() != "cancelled before start" || sim.mining(sim.modules[1]) != 3 {
		t.Fatalf("a queued job must cancel without touching its module: %v, %v", cancelled, err)
	}
	sim.advance(simulatedPSUPrepare + simulatedPSUStage/2)
	cancelled, err = sim.psu.CancelPSUFirmwareUpdate(t.Context(), &mdk.CancelPSUFirmwareUpdateRequest{TaskId: running.TaskId})
	if err != nil || cancelled.GetUpdate().GetState() != psuJobCancelled || cancelled.GetUpdate().GetError() != "staging capsule: cancelled" ||
		sim.mining(sim.modules[0]) != 3 || sim.version(sim.modules[0]) != defaultPSUFirmwareVersion {
		t.Fatalf("a staging job must cancel and resume mining: %v, %v", cancelled, err)
	}
	if again, err := sim.psu.CancelPSUFirmwareUpdate(t.Context(), &mdk.CancelPSUFirmwareUpdateRequest{TaskId: running.TaskId}); err != nil || again.GetUpdate().GetState() != psuJobCancelled {
		t.Fatalf("cancelling a finished job must return it unchanged: %v, %v", again, err)
	}

	other := sim.upload(psucaptest.Options{Revision: "HB0000001"})
	mismatch := sim.start(&mdk.StartPSUFirmwareUpdateRequest{CapsuleId: other.CapsuleId, ModuleId: sim.modules[0]})
	sim.advance(simulatedPSUTotal)
	job := sim.job(mismatch.TaskId)
	if job.GetState() != psuJobFailed || !strings.Contains(job.GetError(), "TARGET_MISMATCH") ||
		job.GetLeaderStatus().GetError() != mdk.PSUFirmwareError_PSU_FIRMWARE_ERROR_TARGET_MISMATCH || job.GetLeaderStatus().GetRecoveryRequired() {
		t.Fatalf("unexpected mismatch job: %v", job)
	}
	if sim.mining(sim.modules[0]) != 3 || sim.version(sim.modules[0]) != defaultPSUFirmwareVersion {
		t.Fatal("a capsule for another PSU must leave the module mining on its version")
	}
}

func TestPSUAndHashboardFirmwareJobsExcludeEachOther(t *testing.T) {
	sim := newPSUSimulator(t, ContainerConfig{})
	capsule := sim.upload(psucaptest.Options{})
	firmware := mdk.NewFirmwareServiceClient(sim.conn)
	image := firmwaretest.Image(t, "prod", "f1", 9)
	stream, err := firmware.UploadFirmwareArtifact(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	meta := &mdk.FirmwareUploadMetadata{Images: []*mdk.FirmwareUploadImage{{Name: "f1-app-prod.bin", SizeBytes: int64(len(image)), Sha256: fmt.Sprintf("%x", sha256.Sum256(image))}}}
	if err := stream.Send(&mdk.UploadFirmwareArtifactRequest{Payload: &mdk.UploadFirmwareArtifactRequest_Metadata{Metadata: meta}}); err != nil {
		t.Fatal(err)
	}
	if err := stream.Send(&mdk.UploadFirmwareArtifactRequest{Payload: &mdk.UploadFirmwareArtifactRequest_Chunk{Chunk: &mdk.FirmwareUploadChunk{Data: image}}}); err != nil {
		t.Fatal(err)
	}
	artifact, err := stream.CloseAndRecv()
	if err != nil {
		t.Fatal(err)
	}
	boards := func(moduleID string) []string {
		sim.state.mu.Lock()
		defer sim.state.mu.Unlock()
		var macs []string
		for _, board := range moduleBoards(sim.state.modulesByID[moduleID]) {
			macs = append(macs, board.MAC)
		}
		return macs
	}
	flash := func(ctx context.Context, moduleID string) error {
		_, err := firmware.StartFirmwareUpdate(ctx, &mdk.StartFirmwareUpdateRequest{Artifact: artifact.Artifact, Scope: &mdk.BoardScope{Identifiers: boards(moduleID)}, Options: &mdk.FirmwareUpdateOptions{Build: "prod"}})
		return err
	}

	if err := flash(t.Context(), sim.modules[1]); err != nil {
		t.Fatal(err)
	}
	blocked := sim.start(&mdk.StartPSUFirmwareUpdateRequest{CapsuleId: capsule.CapsuleId, ModuleId: sim.modules[1]})
	if got := sim.job(blocked.TaskId); got.GetState() != psuJobFailed || !strings.Contains(got.GetError(), "reserving module members") || sim.mining(sim.modules[1]) != 3 {
		t.Fatalf("a module being flashed must not start a PSU update: %v", got)
	}

	psuJob := sim.start(&mdk.StartPSUFirmwareUpdateRequest{CapsuleId: capsule.CapsuleId, ModuleId: sim.modules[0]})
	if err := flash(t.Context(), sim.modules[0]); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("a board in a PSU update must not be flashed, got %v", err)
	}
	sim.advance(simulatedPSUTotal)
	if got := sim.job(psuJob.TaskId); got.GetState() != psuJobSucceeded {
		t.Fatalf("unexpected PSU job: %v", got)
	}
	if err := flash(t.Context(), sim.modules[0]); err != nil {
		t.Fatalf("a finished PSU update must release its boards: %v", err)
	}
}

func TestWritePSUCapsuleProducesAnUploadableCapsule(t *testing.T) {
	sim := newPSUSimulator(t, ContainerConfig{})
	path := t.TempDir() + "/placeholder.psucap"
	if err := writePSUCapsule(path, "app=1510;bl=1110", defaultPSURevision); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	capsule, err := sim.uploadBytes(data, "")
	if err != nil {
		t.Fatal(err)
	}
	job := sim.start(&mdk.StartPSUFirmwareUpdateRequest{CapsuleId: capsule.CapsuleId, ModuleId: sim.modules[0]})
	sim.advance(simulatedPSUTotal)
	if got := sim.job(job.TaskId); got.GetState() != psuJobSucceeded || sim.version(sim.modules[0]) != "app=1510;bl=1110" {
		t.Fatalf("unexpected job: %v", got)
	}
}
