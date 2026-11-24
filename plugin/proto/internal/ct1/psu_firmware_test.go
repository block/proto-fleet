package ct1

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	"github.com/block/proto-fleet/sdk/mdk/psucap"
	"github.com/block/proto-fleet/sdk/mdk/psucap/psucaptest"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	psuSucceeded = mdk.PSUFirmwareUpdateState_PSU_FIRMWARE_UPDATE_STATE_SUCCEEDED
	psuFailed    = mdk.PSUFirmwareUpdateState_PSU_FIRMWARE_UPDATE_STATE_FAILED
	psuApplying  = mdk.PSUFirmwareUpdateState_PSU_FIRMWARE_UPDATE_STATE_APPLYING
	psuRecovery  = mdk.PSUFirmwarePhase_PSU_FIRMWARE_PHASE_RECOVERY_REQUIRED
	psuHeld      = "PSU recovery is required; mining was left stopped"
	// The revisions a PSU reports once the test capsule is applied.
	psuUpdatedApp, psuUpdatedBoot = "1410", "1110"
)

type fakePSUStart struct {
	create bool
	err    error
}

// fakePSUController keeps capsules and module jobs the way one Frigate node
// does. Tests end jobs explicitly.
type fakePSUController struct {
	mdk.UnimplementedPSUFirmwareServiceServer
	mdk.UnimplementedHashboardServiceServer
	mdk.UnimplementedSystemServiceServer

	mu           sync.Mutex
	id           string
	wire         *mdk.GetContainerMapResponse
	uploads      []*mdk.PSUFirmwareCapsuleUploadMetadata
	uploadErr    error
	listErr      error
	jobs         []*mdk.PSUFirmwareUpdate
	starts       []*mdk.StartPSUFirmwareUpdateRequest
	startResults map[string][]fakePSUStart
	// boards is the PSU firmware status every board of a module reports.
	boards map[string]*mdk.PSUFirmwareBoardStatus
}

func (f *fakePSUController) GetContainerMap(context.Context, *mdk.GetContainerMapRequest) (*mdk.GetContainerMapResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return proto.CloneOf(f.wire), nil
}

func (f *fakePSUController) GetSystemStatus(context.Context, *mdk.GetSystemStatusRequest) (*mdk.GetSystemStatusResponse, error) {
	return &mdk.GetSystemStatusResponse{Status: &mdk.SystemStatus{ServerId: f.id}}, nil
}

func (f *fakePSUController) UploadPSUFirmwareCapsule(stream mdk.PSUFirmwareService_UploadPSUFirmwareCapsuleServer) error {
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	var data []byte
	for {
		message, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		data = append(data, message.GetChunk()...)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.uploads = append(f.uploads, first.GetMetadata())
	if f.uploadErr != nil {
		return f.uploadErr
	}
	capsule, err := psucap.Verify(data)
	if err != nil || capsule.ID != first.GetMetadata().GetSha256() || capsule.Size != first.GetMetadata().GetSizeBytes() {
		return status.Error(codes.InvalidArgument, "invalid PSU firmware capsule") //nolint:wrapcheck // server must return a gRPC status
	}
	return stream.SendAndClose(&mdk.UploadPSUFirmwareCapsuleResponse{Capsule: &mdk.PSUFirmwareCapsule{CapsuleId: capsule.ID}})
}

func (f *fakePSUController) ListPSUFirmwareUpdates(context.Context, *mdk.ListPSUFirmwareUpdatesRequest) (*mdk.ListPSUFirmwareUpdatesResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listErr != nil {
		return nil, f.listErr
	}
	out := &mdk.ListPSUFirmwareUpdatesResponse{}
	for i := len(f.jobs) - 1; i >= 0; i-- {
		out.Updates = append(out.Updates, proto.CloneOf(f.jobs[i]))
	}
	return out, nil
}

func (f *fakePSUController) members(moduleID string) []*mdk.PSUFirmwareUpdateMember {
	var out []*mdk.PSUFirmwareUpdateMember
	for _, tank := range f.wire.GetContainerMap().GetTanks() {
		for _, module := range tank.GetModules() {
			for _, slot := range module.GetSlots() {
				if slot.GetHashboard().GetModuleId() == moduleID {
					out = append(out, &mdk.PSUFirmwareUpdateMember{MacAddress: slot.GetHashboard().GetMacAddress(), Reserved: true, MiningBefore: true})
				}
			}
		}
	}
	return out
}

func (f *fakePSUController) addJob(moduleID, capsuleID string, recovery bool) *mdk.PSUFirmwareUpdate {
	job := &mdk.PSUFirmwareUpdate{
		TaskId: fmt.Sprintf("psufw-%s-%d", f.id, len(f.jobs)+1), ServerId: f.id, ModuleId: moduleID, CapsuleId: capsuleID, Recover: recovery,
		State: psuApplying, CreatedAt: timestamppb.New(time.Unix(1_800_000_000+int64(len(f.jobs)), 0)), Members: f.members(moduleID),
	}
	f.jobs = append(f.jobs, job)
	return job
}

func (f *fakePSUController) StartPSUFirmwareUpdate(_ context.Context, req *mdk.StartPSUFirmwareUpdateRequest) (*mdk.StartPSUFirmwareUpdateResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.starts = append(f.starts, req)
	result := fakePSUStart{create: true}
	if scripted := f.startResults[req.GetModuleId()]; len(scripted) > 0 {
		result, f.startResults[req.GetModuleId()] = scripted[0], scripted[1:]
	}
	var job *mdk.PSUFirmwareUpdate
	if result.create {
		for _, existing := range f.jobs {
			if existing.GetModuleId() == req.GetModuleId() && !psuJobTerminal(existing.GetState()) {
				return nil, status.Errorf(codes.AlreadyExists, "PSU module already has an active PSU firmware update: task %s", existing.GetTaskId()) //nolint:wrapcheck // server must return a gRPC status
			}
		}
		job = f.addJob(req.GetModuleId(), req.GetCapsuleId(), req.GetRecover())
	}
	if result.err != nil {
		return nil, result.err
	}
	return &mdk.StartPSUFirmwareUpdateResponse{Update: proto.CloneOf(job)}, nil
}

func (f *fakePSUController) GetPSUFirmwareUpdate(_ context.Context, req *mdk.GetPSUFirmwareUpdateRequest) (*mdk.GetPSUFirmwareUpdateResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, job := range f.jobs {
		if job.GetTaskId() == req.GetTaskId() {
			return &mdk.GetPSUFirmwareUpdateResponse{Update: proto.CloneOf(job)}, nil
		}
	}
	return nil, status.Error(codes.NotFound, "PSU firmware update not found") //nolint:wrapcheck // server must return a gRPC status
}

func (f *fakePSUController) GetPSUFirmwareBoardStatus(_ context.Context, req *mdk.GetPSUFirmwareBoardStatusRequest) (*mdk.GetPSUFirmwareBoardStatusResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for moduleID, board := range f.boards {
		for _, member := range f.members(moduleID) {
			if member.GetMacAddress() == req.GetMacAddress() {
				return &mdk.GetPSUFirmwareBoardStatusResponse{Status: proto.CloneOf(board)}, nil
			}
		}
	}
	return nil, status.Error(codes.NotFound, "hashboard is not managed by this Frigate node") //nolint:wrapcheck // server must return a gRPC status
}

// succeed ends a job the way Frigate does after programming: the job keeps
// the leader's last status, and the module's boards report the same capsule
// and PSU revisions from then on. Caller holds f.mu.
func (f *fakePSUController) succeed(job *mdk.PSUFirmwareUpdate) {
	job.State, job.ApplyRequested = psuSucceeded, true
	job.LeaderMacAddress = job.GetMembers()[0].GetMacAddress()
	job.LeaderStatus = &mdk.PSUFirmwareBoardStatus{
		MacAddress: job.LeaderMacAddress, Phase: mdk.PSUFirmwarePhase_PSU_FIRMWARE_PHASE_SUCCEEDED, CapsuleId: job.GetCapsuleId(),
		CurrentAppRevision: psuUpdatedApp, CurrentBootRevision: psuUpdatedBoot, PsuLeader: true,
	}
}

// finish ends a module's newest job, successfully unless change says otherwise.
func (f *fakePSUController) finish(moduleID string, change func(*mdk.PSUFirmwareUpdate)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.jobs) - 1; i >= 0; i-- {
		if job := f.jobs[i]; job.GetModuleId() == moduleID {
			f.succeed(job)
			if change != nil {
				change(job)
			}
			if job.GetState() == psuSucceeded {
				f.boards[moduleID] = proto.CloneOf(job.GetLeaderStatus())
			}
			return
		}
	}
}

// finishRunning ends every running job successfully and returns how many it ended.
func (f *fakePSUController) finishRunning() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	ended := 0
	for _, job := range f.jobs {
		if !psuJobTerminal(job.GetState()) {
			f.succeed(job)
			f.boards[job.GetModuleId()] = proto.CloneOf(job.GetLeaderStatus())
			ended++
		}
	}
	return ended
}

func (f *fakePSUController) running() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var modules []string
	for _, job := range f.jobs {
		if !psuJobTerminal(job.GetState()) {
			modules = append(modules, job.GetModuleId())
		}
	}
	return modules
}

func (f *fakePSUController) started() []*mdk.StartPSUFirmwareUpdateRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*mdk.StartPSUFirmwareUpdateRequest(nil), f.starts...)
}

func (f *fakePSUController) uploaded() []*mdk.PSUFirmwareCapsuleUploadMetadata {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*mdk.PSUFirmwareCapsuleUploadMetadata(nil), f.uploads...)
}

func (f *fakePSUController) set(change func(*fakePSUController)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	change(f)
}

func psuModuleID(tank, module int32) string {
	return fmt.Sprintf("%08x%08x", tank, module)
}

// psuContainerMap is a complete container whose tanks owner assigns to controllers.
func psuContainerMap(owner func(tank int32) string) *mdk.GetContainerMapResponse {
	wire := firmwareContainerMap(time.Now())
	for _, tank := range wire.ContainerMap.Tanks {
		for _, module := range tank.Modules {
			for _, slot := range module.Slots {
				slot.Hashboard.ModuleId = psuModuleID(tank.Id, module.Id)
				slot.Hashboard.OwnerServerId = owner(tank.Id)
			}
		}
	}
	return wire
}

func oneController(int32) string { return "a" }

// newPSUDevice serves one fake controller per ID; the first is the one a
// write selects.
func newPSUDevice(t *testing.T, wire *mdk.GetContainerMapResponse, ids ...string) (*Device, map[string]*fakePSUController) {
	t.Helper()
	dev := &Device{id: "ct1", info: sdk.DeviceInfo{SerialNumber: "ct1-" + t.Name()}}
	controllers := map[string]*fakePSUController{}
	for _, id := range ids {
		controller := &fakePSUController{id: id, wire: wire, startResults: map[string][]fakePSUStart{}, boards: map[string]*mdk.PSUFirmwareBoardStatus{}}
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)
		server := grpc.NewServer()
		mdk.RegisterPSUFirmwareServiceServer(server, controller)
		mdk.RegisterHashboardServiceServer(server, controller)
		mdk.RegisterSystemServiceServer(server, controller)
		go func() { _ = server.Serve(listener) }()
		t.Cleanup(server.Stop)
		addr, ok := listener.Addr().(*net.TCPAddr)
		require.True(t, ok)
		port := int32(addr.Port) //nolint:gosec // a TCP port fits int32
		client, err := NewClient(sdk.DeviceInfo{Host: "127.0.0.1", Port: port, RPCPort: port, URLScheme: "http"}, sdk.UsernamePassword{})
		require.NoError(t, err)
		t.Cleanup(func() { _ = client.Close() })
		client.memberID = id
		dev.clients = append(dev.clients, client)
		controllers[id] = controller
	}
	return dev, controllers
}

// psuCapsuleData is a capsule that one or more commands send.
type psuCapsuleData []byte

func (c psuCapsuleData) file(requestID string) sdk.FirmwareFile {
	return sdk.FirmwareFile{Reader: bytes.NewReader(c), Filename: "boco-1.4.psucap", RequestID: requestID}
}

func psuCapsuleBytes(t *testing.T) (psuCapsuleData, string) {
	t.Helper()
	data, err := psucaptest.Build(psucaptest.Options{})
	require.NoError(t, err)
	capsule, err := psucap.Verify(data)
	require.NoError(t, err)
	return data, capsule.ID
}

// psuStatus is a bare status poll. It must never change a controller.
func psuStatus(t *testing.T, dev *Device) *sdk.FirmwareUpdateStatus {
	t.Helper()
	current, err := dev.GetFirmwareUpdateStatus(t.Context())
	require.NoError(t, err)
	return current
}

// psuCommand is one Fleet firmware command. Like Fleet's worker, it sends the
// capsule, polls, and sends the same capsule and request ID again whenever a
// poll reports that the update is ready for its next module.
type psuCommand struct {
	dev     *Device
	capsule psuCapsuleData
	request string
}

// newPSUCommand is a command for a new capsule.
func newPSUCommand(t *testing.T, dev *Device) (psuCommand, string) {
	t.Helper()
	data, id := psuCapsuleBytes(t)
	return psuCommand{dev: dev, capsule: data, request: "batch/1"}, id
}

func (c psuCommand) send(t *testing.T) {
	t.Helper()
	require.NoError(t, c.dev.FirmwareUpdate(t.Context(), c.capsule.file(c.request)))
}

// poll is one poll by Fleet's worker and returns what it reported.
func (c psuCommand) poll(t *testing.T) *sdk.FirmwareUpdateStatus {
	t.Helper()
	current := psuStatus(t, c.dev)
	if current.State == sdk.FirmwareStateContinue {
		c.send(t)
	}
	return current
}

// run ends every job successfully until the update is over.
func (c psuCommand) run(t *testing.T, controllers map[string]*fakePSUController) *sdk.FirmwareUpdateStatus {
	t.Helper()
	for range 4 * sdk.CT1TankCount * sdk.CT1ModulesPerTank {
		current := c.poll(t)
		if current.State != firmwareStateInstalling && current.State != sdk.FirmwareStateContinue {
			return current
		}
		for _, controller := range controllers {
			controller.finishRunning()
		}
	}
	t.Fatal("PSU update did not finish")
	return nil
}

func psuStarts(controllers map[string]*fakePSUController) int {
	total := 0
	for _, controller := range controllers {
		total += len(controller.started())
	}
	return total
}

func psuRecoveryFailure(capsuleID string) func(*mdk.PSUFirmwareUpdate) {
	return func(job *mdk.PSUFirmwareUpdate) {
		job.State, job.Error = psuFailed, "PSU firmware update failed in PSU_FW_PHASE_RECOVERY_REQUIRED: PSU_FW_ERROR_I2C"
		job.LeaderStatus = &mdk.PSUFirmwareBoardStatus{Phase: psuRecovery, RecoveryRequired: true, CapsuleId: capsuleID, PsuLeader: true}
		for _, member := range job.Members {
			member.RestoreError = psuHeld
		}
	}
}

func TestPSUFirmwareUpdatesOneModulePerControllerAtATime(t *testing.T) {
	wire := psuContainerMap(func(tank int32) string {
		if tank > 3 {
			return "b"
		}
		return "a"
	})
	dev, controllers := newPSUDevice(t, wire, "a", "b")
	command, capsuleID := newPSUCommand(t, dev)

	command.send(t)
	for id, first := range map[string]string{"a": psuModuleID(1, 1), "b": psuModuleID(4, 1)} {
		uploads := controllers[id].uploaded()
		require.Len(t, uploads, 1, "each owning controller keeps its own capsule cache")
		require.Equal(t, "boco-1.4.psucap", uploads[0].GetFileName())
		require.Equal(t, capsuleID, uploads[0].GetSha256())
		require.Equal(t, []string{first}, controllers[id].running(), "Frigate updates one module at a time, so only one is started")
		start := controllers[id].started()[0]
		require.Equal(t, capsuleID, start.GetCapsuleId())
		require.False(t, start.GetRecover() || start.GetForce())
	}
	current := psuStatus(t, dev)
	require.Equal(t, firmwareStateInstalling, current.State)
	require.Zero(t, *current.Progress)
	require.Len(t, controllers["a"].started(), 1, "a poll must not start a second module while one runs")

	controllers["a"].finish(psuModuleID(1, 1), nil)
	current = psuStatus(t, dev)
	require.Equal(t, sdk.FirmwareStateContinue, current.State, "a poll reports that a controller is ready for its next module")
	require.Equal(t, 1, *current.Progress)
	require.Empty(t, controllers["a"].running(), "a poll does not start that module")
	command.send(t)
	require.Equal(t, []string{psuModuleID(1, 2)}, controllers["a"].running(), "the command sent again starts it")
	require.Equal(t, []string{psuModuleID(4, 1)}, controllers["b"].running(), "controllers advance independently")
	require.Len(t, controllers["b"].started(), 1)
	require.Len(t, controllers["a"].uploaded(), 1, "a continuation does not upload the capsule again")
	require.Equal(t, firmwareStateInstalling, psuStatus(t, dev).State)

	done := command.run(t, controllers)
	require.Equal(t, firmwareStateCompleted, done.State)
	require.Equal(t, 100, *done.Progress)
	require.Nil(t, done.Error)
	total := sdk.CT1TankCount * sdk.CT1ModulesPerTank
	require.Len(t, controllers["a"].started(), total/2)
	require.Len(t, controllers["b"].started(), total/2)
	for index, start := range controllers["a"].started() {
		require.Equal(t, psuModuleID(int32(index/sdk.CT1ModulesPerTank)+1, int32(index%sdk.CT1ModulesPerTank)+1), start.GetModuleId(), "modules update in tank and module order") //nolint:gosec // small indexes
	}
	require.Equal(t, firmwareStateCompleted, psuStatus(t, newHandle(dev)).State, "status is shared by every handle of the container")
}

func TestPSUFirmwareStopsWhenAModuleIsLeftOutOfMining(t *testing.T) {
	dev, controllers := newPSUDevice(t, psuContainerMap(oneController), "a")
	a := controllers["a"]
	command, capsuleID := newPSUCommand(t, dev)
	command.send(t)
	a.finish(psuModuleID(1, 1), nil)
	require.Equal(t, sdk.FirmwareStateContinue, command.poll(t).State)
	a.finish(psuModuleID(1, 2), psuRecoveryFailure(capsuleID))

	current := command.poll(t)
	require.Equal(t, firmwareStateFailed, current.State)
	require.Equal(t, 2, *current.Progress, "two modules finished: one updated, one failed")
	require.Contains(t, *current.Error, "2 PSU module firmware failures")
	require.Contains(t, *current.Error, "tank 1 module 2 ("+psuModuleID(1, 2)+"): PSU firmware update failed in PSU_FW_PHASE_RECOVERY_REQUIRED: PSU_FW_ERROR_I2C; "+psuHeld)
	require.Contains(t, *current.Error, "update stopped because tank 1 module 2 was left out of mining; 94 modules were not attempted")
	require.Len(t, a.started(), 2, "no module may start after one is left stopped")
	command.send(t)
	require.Equal(t, firmwareStateFailed, command.poll(t).State)
	require.Len(t, a.started(), 2, "the stopped command sent again starts nothing")
}

// Frigate records a restore error only for boards that were mining before the
// job. When it stops watching an apply on a module that was not mining, the
// leader's last phase is the only sign that the PSU may still be programming.
func TestPSUFirmwareStopsWhenAnApplyIsUnresolved(t *testing.T) {
	idle := func(member *mdk.PSUFirmwareUpdateMember) { member.MiningBefore = false }
	for name, tc := range map[string]struct {
		leader *mdk.PSUFirmwareBoardStatus
		member func(*mdk.PSUFirmwareUpdateMember)
	}{
		"programming":      {&mdk.PSUFirmwareBoardStatus{Phase: mdk.PSUFirmwarePhase_PSU_FIRMWARE_PHASE_PROGRAMMING, PsuLeader: true}, idle},
		"verifying":        {&mdk.PSUFirmwareBoardStatus{Phase: mdk.PSUFirmwarePhase_PSU_FIRMWARE_PHASE_VERIFYING, PsuLeader: true}, idle},
		"apply not seen":   {&mdk.PSUFirmwareBoardStatus{Phase: mdk.PSUFirmwarePhase_PSU_FIRMWARE_PHASE_READY, PsuLeader: true}, idle},
		"no leader status": {nil, idle},
		// Frigate records nothing about mining on a member it did not reserve.
		"members not reserved": {
			&mdk.PSUFirmwareBoardStatus{Phase: mdk.PSUFirmwarePhase_PSU_FIRMWARE_PHASE_PROGRAMMING, PsuLeader: true},
			func(member *mdk.PSUFirmwareUpdateMember) { member.Reserved = false },
		},
	} {
		t.Run(name, func(t *testing.T) {
			dev, controllers := newPSUDevice(t, psuContainerMap(oneController), "a")
			a := controllers["a"]
			command, _ := newPSUCommand(t, dev)
			command.send(t)
			a.finish(psuModuleID(1, 1), nil)
			require.Equal(t, sdk.FirmwareStateContinue, command.poll(t).State)
			a.finish(psuModuleID(1, 2), func(job *mdk.PSUFirmwareUpdate) {
				job.State, job.Error, job.LeaderStatus = psuFailed, "monitoring PSU programming ended before a terminal phase", tc.leader
				for _, member := range job.Members {
					tc.member(member)
				}
			})

			current := command.poll(t)
			require.Equal(t, firmwareStateFailed, current.State)
			require.Contains(t, *current.Error, "tank 1 module 2 ("+psuModuleID(1, 2)+"): monitoring PSU programming ended before a terminal phase; the PSU may still be programming")
			require.Contains(t, *current.Error, "update stopped because tank 1 module 2 may still be programming its PSU; 94 modules were not attempted")
			require.Len(t, a.started(), 2, "no module may start while a PSU may still be programming")
			command.send(t)
			require.Equal(t, firmwareStateFailed, command.poll(t).State)
			require.Len(t, a.started(), 2)
		})
	}
}

// Frigate restarts the mining a job stopped only when it leaves no hold. A
// failed apply whose members were restarted was therefore resolved: Frigate
// concluded that the apply never started, and the update goes on.
func TestPSUFirmwareContinuesPastAnApplyThatNeverStarted(t *testing.T) {
	dev, controllers := newPSUDevice(t, psuContainerMap(oneController), "a")
	a := controllers["a"]
	command, _ := newPSUCommand(t, dev)
	command.send(t)
	const refused = "apply failed and the leader remained in PSU_FW_PHASE_READY: PSU firmware command rejected"
	a.finish(psuModuleID(1, 1), func(job *mdk.PSUFirmwareUpdate) {
		job.State, job.Error = psuFailed, refused
		job.LeaderStatus = &mdk.PSUFirmwareBoardStatus{Phase: mdk.PSUFirmwarePhase_PSU_FIRMWARE_PHASE_READY, PsuLeader: true}
	})
	done := command.run(t, controllers)
	require.Equal(t, firmwareStateFailed, done.State)
	require.Equal(t, "1 PSU module firmware failures: tank 1 module 1 ("+psuModuleID(1, 1)+"): "+refused, *done.Error)
	require.Len(t, a.started(), sdk.CT1TankCount*sdk.CT1ModulesPerTank)
}

// Frigate addresses a module by the ID its boards report. A module whose
// boards report none cannot be updated, and leaving it out would let the
// command complete with that PSU never updated.
func TestPSUFirmwareFailsAModuleWithoutAnIdentity(t *testing.T) {
	wire := psuContainerMap(oneController)
	for _, slot := range wire.ContainerMap.Tanks[0].Modules[1].Slots {
		slot.Hashboard.ModuleId = ""
	}
	// One board of another module reports no ID; its module is still known.
	wire.ContainerMap.Tanks[0].Modules[2].Slots[0].Hashboard.ModuleId = ""
	dev, controllers := newPSUDevice(t, wire, "a")
	command, _ := newPSUCommand(t, dev)
	command.send(t)

	done := command.run(t, controllers)
	require.Equal(t, firmwareStateFailed, done.State, "an occupied module that was not updated must not report a completed container")
	require.Equal(t, "1 PSU module firmware failures: tank 1 module 2: its boards report no PSU module identity, so Frigate cannot update it", *done.Error)
	require.Equal(t, 100, *done.Progress)
	started := map[string]bool{}
	for _, start := range controllers["a"].started() {
		started[start.GetModuleId()] = true
	}
	require.True(t, started[psuModuleID(1, 3)])
	require.NotContains(t, started, "")
	require.Len(t, started, sdk.CT1TankCount*sdk.CT1ModulesPerTank-1)
}

// A failure that stops the update stops it on every controller, whichever
// controller the plugin happens to read first.
func TestPSUFirmwareStopsOnEveryControllerAtOnce(t *testing.T) {
	wire := psuContainerMap(func(tank int32) string {
		if tank > 3 {
			return "b"
		}
		return "a"
	})
	// Map order decides which controller is visited first, so try several.
	for attempt := range 12 {
		t.Run(fmt.Sprint(attempt), func(t *testing.T) {
			dev, controllers := newPSUDevice(t, wire, "a", "b")
			command, capsuleID := newPSUCommand(t, dev)
			command.send(t)
			controllers["a"].finish(psuModuleID(1, 1), nil)
			controllers["b"].finish(psuModuleID(4, 1), psuRecoveryFailure(capsuleID))

			// The command sent again finds both jobs ended in the same call.
			command.send(t)
			require.Equal(t, 2, psuStarts(controllers), "controller a must not start a module once controller b left one out of mining")
			current := command.poll(t)
			require.Equal(t, firmwareStateFailed, current.State)
			require.Contains(t, *current.Error, "update stopped because tank 4 module 1 was left out of mining; 94 modules were not attempted")
			require.Equal(t, 2, psuStarts(controllers))
		})
	}
}

// Fleet keeps a firmware command open while its progress rises. A container
// whose PSUs refuse the capsule one after another is still being worked
// through, so every refused module raises it; a job that keeps running does not.
func TestPSUFirmwareProgressCountsEveryFinishedModule(t *testing.T) {
	dev, controllers := newPSUDevice(t, psuContainerMap(oneController), "a")
	a := controllers["a"]
	command, _ := newPSUCommand(t, dev)
	command.send(t)
	mismatch := func(job *mdk.PSUFirmwareUpdate) {
		job.State, job.Error = psuFailed, "PSU firmware update failed in PSU_FW_PHASE_FAILED: PSU_FW_ERROR_TARGET_MISMATCH"
		job.LeaderStatus = &mdk.PSUFirmwareBoardStatus{Phase: mdk.PSUFirmwarePhase_PSU_FIRMWARE_PHASE_FAILED, Error: mdk.PSUFirmwareError_PSU_FIRMWARE_ERROR_TARGET_MISMATCH, PsuLeader: true}
	}
	total, progress := sdk.CT1TankCount*sdk.CT1ModulesPerTank, 0
	for module := 1; module <= total; module++ {
		for range 3 {
			running := psuStatus(t, dev)
			require.Equal(t, firmwareStateInstalling, running.State)
			require.Equal(t, progress, *running.Progress, "a job that is still running is not progress")
		}
		a.finish(a.running()[0], mismatch)
		current := command.poll(t)
		require.Greater(t, *current.Progress, progress, "module %d failed without stopping the update, so the command still advances", module)
		progress = *current.Progress
	}
	require.Equal(t, 100, progress)
	require.Equal(t, firmwareStateFailed, psuStatus(t, dev).State)
	require.Len(t, a.started(), total)
}

// Frigate keeps a successful job, and the boards keep their record of the
// capsule, after a PSU is replaced. Only the revisions the PSU reports now
// show whether the module still runs what that job installed.
func TestPSUFirmwareUpdatesAReplacedPSUDespiteAnEarlierSuccess(t *testing.T) {
	dev, controllers := newPSUDevice(t, psuContainerMap(oneController), "a")
	a := controllers["a"]
	command, capsuleID := newPSUCommand(t, dev)
	redo := map[string]string{}
	a.set(func(f *fakePSUController) {
		updated := func(position int32) (string, *mdk.PSUFirmwareUpdate) {
			module := psuModuleID(1, position)
			job := f.addJob(module, capsuleID, false)
			f.succeed(job)
			f.boards[module] = proto.CloneOf(job.GetLeaderStatus())
			return module, job
		}
		updated(1)

		module, _ := updated(2)
		f.boards[module].CurrentAppRevision = "1310"
		redo[module] = "a replacement PSU on other firmware, behind boards that still record the capsule"

		module, _ = updated(3)
		f.boards[module].CurrentBootRevision = "1010"
		redo[module] = "a replacement PSU with another bootloader"

		module, _ = updated(4)
		f.boards[module] = &mdk.PSUFirmwareBoardStatus{Phase: mdk.PSUFirmwarePhase_PSU_FIRMWARE_PHASE_EMPTY, CurrentAppRevision: psuUpdatedApp, CurrentBootRevision: psuUpdatedBoot, PsuLeader: true}
		redo[module] = "replacement boards that record no capsule"

		module, _ = updated(5)
		delete(f.boards, module)
		redo[module] = "boards that do not answer"

		module, job := updated(6)
		job.LeaderStatus.CurrentAppRevision, job.LeaderStatus.CurrentBootRevision = "", ""
		redo[module] = "a job that recorded no PSU revisions"

		module, _ = updated(7)
		f.boards[module].PsuLeader = false
		redo[module] = "a board that no longer leads the PSU"
	})

	command.send(t)
	require.Equal(t, firmwareStateCompleted, command.run(t, controllers).State)

	started := map[string]bool{}
	for _, start := range a.started() {
		require.False(t, start.GetRecover() || start.GetForce(), "Frigate decides whether the PSU already runs the capsule")
		started[start.GetModuleId()] = true
	}
	require.False(t, started[psuModuleID(1, 1)], "a PSU that still reports what the job left on it is not updated again")
	for module, why := range redo {
		require.True(t, started[module], "the earlier success must not stand in for %s", why)
	}
	require.Len(t, started, sdk.CT1TankCount*sdk.CT1ModulesPerTank-1)
}

// Fleet admits FirmwareUpdate as a device write and does not admit a status
// poll, so a poll must leave every controller unchanged.
func TestPSUFirmwareStatusPollsNeverStartAModule(t *testing.T) {
	wire := psuContainerMap(func(tank int32) string {
		if tank > 3 {
			return "b"
		}
		return "a"
	})
	dev, controllers := newPSUDevice(t, wire, "a", "b")
	a := controllers["a"]
	command, capsuleID := newPSUCommand(t, dev)
	// The board of the second module reports that it needs this capsule's
	// recovery, and the third module's start is lost before it creates a job.
	a.set(func(f *fakePSUController) {
		f.boards[psuModuleID(1, 2)] = &mdk.PSUFirmwareBoardStatus{Phase: psuRecovery, RecoveryRequired: true, CapsuleId: capsuleID, PsuLeader: true}
		f.startResults[psuModuleID(1, 3)] = []fakePSUStart{{err: status.Error(codes.DeadlineExceeded, "deadline exceeded")}}
	})
	command.send(t)
	require.Equal(t, 2, psuStarts(controllers))
	polls := func(reason string) {
		t.Helper()
		before, uploads := psuStarts(controllers), len(a.uploaded())+len(controllers["b"].uploaded())
		for _, handle := range []*Device{dev, newHandle(dev), dev} {
			require.Equal(t, sdk.FirmwareStateContinue, psuStatus(t, handle).State)
		}
		require.Equal(t, before, psuStarts(controllers), reason)
		require.Equal(t, uploads, len(a.uploaded())+len(controllers["b"].uploaded()), reason)
	}

	a.finish(psuModuleID(1, 1), nil)
	polls("a finished module does not start the next one")
	command.send(t)
	require.Equal(t, []string{psuModuleID(1, 2)}, a.running())

	a.finish(psuModuleID(1, 2), func(job *mdk.PSUFirmwareUpdate) {
		job.State, job.ApplyRequested, job.Error = psuFailed, false, "PSU needs recovery; re-run with recover"
	})
	polls("a PSU that needs recovery is not recovered by a poll")
	command.send(t)
	starts := a.started()
	require.Equal(t, psuModuleID(1, 2), starts[len(starts)-1].GetModuleId())
	require.True(t, starts[len(starts)-1].GetRecover())

	a.finish(psuModuleID(1, 2), nil)
	polls("a recovered module does not start the next one")
	command.send(t)
	require.Empty(t, a.running(), "the third module's start never reached the controller")
	polls("a lost start is not repeated by a poll")
	command.send(t)
	require.Equal(t, []string{psuModuleID(1, 3)}, a.running())

	a.finish(psuModuleID(1, 3), nil)
	controllers["b"].finish(psuModuleID(4, 1), nil)
	polls("no controller starts a module without the command")
	require.Empty(t, append(a.running(), controllers["b"].running()...))
	require.Equal(t, 6, psuStarts(controllers))
}

func TestPSUFirmwareStopsWhenBoardsRejectTheCapsule(t *testing.T) {
	dev, controllers := newPSUDevice(t, psuContainerMap(oneController), "a")
	command, _ := newPSUCommand(t, dev)
	command.send(t)
	controllers["a"].finish(psuModuleID(1, 1), func(job *mdk.PSUFirmwareUpdate) {
		job.State, job.Error, job.ApplyRequested = psuFailed, "validating capsule: PSU_FW_ERROR_CAPSULE_TRUST", false
		job.LeaderStatus = &mdk.PSUFirmwareBoardStatus{Phase: mdk.PSUFirmwarePhase_PSU_FIRMWARE_PHASE_FAILED, Error: mdk.PSUFirmwareError_PSU_FIRMWARE_ERROR_CAPSULE_TRUST, PsuLeader: true}
	})
	current := psuStatus(t, dev)
	require.Equal(t, firmwareStateFailed, current.State)
	require.Contains(t, *current.Error, "update stopped because board firmware rejected the capsule; 95 modules were not attempted")
	require.Len(t, controllers["a"].started(), 1)
}

func TestPSUFirmwareContinuesPastAModuleThatKeepsMining(t *testing.T) {
	dev, controllers := newPSUDevice(t, psuContainerMap(oneController), "a")
	command, _ := newPSUCommand(t, dev)
	command.send(t)
	// Another PSU model refuses the capsule before anything changes, and
	// Frigate restores mining.
	controllers["a"].finish(psuModuleID(1, 1), func(job *mdk.PSUFirmwareUpdate) {
		job.State, job.Error = psuFailed, "PSU firmware update failed in PSU_FW_PHASE_FAILED: PSU_FW_ERROR_TARGET_MISMATCH"
		job.LeaderStatus = &mdk.PSUFirmwareBoardStatus{Phase: mdk.PSUFirmwarePhase_PSU_FIRMWARE_PHASE_FAILED, Error: mdk.PSUFirmwareError_PSU_FIRMWARE_ERROR_TARGET_MISMATCH, PsuLeader: true}
	})
	done := command.run(t, controllers)
	require.Equal(t, firmwareStateFailed, done.State)
	require.Equal(t, "1 PSU module firmware failures: tank 1 module 1 ("+psuModuleID(1, 1)+"): PSU firmware update failed in PSU_FW_PHASE_FAILED: PSU_FW_ERROR_TARGET_MISMATCH", *done.Error)
	require.Len(t, controllers["a"].started(), sdk.CT1TankCount*sdk.CT1ModulesPerTank)
}

func TestPSUFirmwareSentAgainRecoversAndSkipsUpdatedModules(t *testing.T) {
	dev, controllers := newPSUDevice(t, psuContainerMap(oneController), "a")
	a := controllers["a"]
	first, capsuleID := newPSUCommand(t, dev)
	capsule := first.capsule
	first.send(t)
	a.finish(psuModuleID(1, 1), nil)
	first.poll(t)
	a.finish(psuModuleID(1, 2), psuRecoveryFailure(capsuleID))
	require.Equal(t, firmwareStateFailed, first.poll(t).State)
	a.set(func(f *fakePSUController) {
		f.boards[psuModuleID(1, 2)] = &mdk.PSUFirmwareBoardStatus{Phase: psuRecovery, RecoveryRequired: true, CapsuleId: capsuleID, PsuLeader: true}
	})

	second := psuCommand{dev: dev, capsule: capsule, request: "batch/2"}
	second.send(t)
	require.Len(t, a.uploaded(), 2)
	starts := a.started()
	require.Len(t, starts, 3)
	require.Equal(t, psuModuleID(1, 2), starts[2].GetModuleId(), "the updated module is skipped and the failed one goes first")
	require.True(t, starts[2].GetRecover(), "a PSU the capsule left needing recovery must be recovered, not updated again")
	done := second.run(t, controllers)
	require.Equal(t, firmwareStateCompleted, done.State)
	started := map[string]int{}
	for _, start := range a.started() {
		started[start.GetModuleId()]++
		require.Equal(t, start.GetModuleId() == psuModuleID(1, 2) && started[start.GetModuleId()] == 2, start.GetRecover())
	}
	require.Equal(t, 1, started[psuModuleID(1, 1)], "a module the controller reports on this capsule is not updated again")
	require.Equal(t, 2, started[psuModuleID(1, 2)])
	require.Len(t, started, sdk.CT1TankCount*sdk.CT1ModulesPerTank)

	// Sent once more, nothing is left to do.
	before := len(a.started())
	require.NoError(t, dev.FirmwareUpdate(t.Context(), capsule.file("batch/3")))
	require.Equal(t, firmwareStateCompleted, psuStatus(t, dev).State)
	require.Len(t, a.started(), before)
	require.Len(t, a.uploaded(), 2, "a capsule is not uploaded when no module needs it")
}

func TestPSUFirmwareRecoveryNeedsTheCapsuleThatFailed(t *testing.T) {
	dev, controllers := newPSUDevice(t, psuContainerMap(oneController), "a")
	a := controllers["a"]
	other := strings.Repeat("ab", 32)
	a.set(func(f *fakePSUController) {
		psuRecoveryFailure(other)(f.addJob(psuModuleID(1, 1), other, false))
		// The module's boards are unreachable, so the job's last status decides.
		job := f.addJob(psuModuleID(1, 2), other, false)
		job.State, job.Error = psuFailed, "monitoring PSU programming ended before a terminal phase"
		job.LeaderStatus = &mdk.PSUFirmwareBoardStatus{Phase: mdk.PSUFirmwarePhase_PSU_FIRMWARE_PHASE_PROGRAMMING, CapsuleId: other, PsuLeader: true}
	})
	command, _ := newPSUCommand(t, dev)
	command.send(t)
	require.Equal(t, []string{psuModuleID(1, 3)}, a.running(), "modules that cannot take this capsule are not started")
	done := command.run(t, controllers)
	require.Equal(t, firmwareStateFailed, done.State)
	require.Contains(t, *done.Error, "tank 1 module 1 ("+psuModuleID(1, 1)+"): its PSU needs recovery with capsule abababababab; send that capsule again")
	require.Contains(t, *done.Error, "tank 1 module 2 ("+psuModuleID(1, 2)+"): an earlier PSU firmware apply is still running; send the update again once it ends")
	require.Len(t, a.started(), sdk.CT1TankCount*sdk.CT1ModulesPerTank-2)
}

func TestPSUFirmwareRecoversAfterTheControllerForgotTheFailure(t *testing.T) {
	dev, controllers := newPSUDevice(t, psuContainerMap(oneController), "a")
	a := controllers["a"]
	command, capsuleID := newPSUCommand(t, dev)
	// A restarted Frigate has no job history, but the board still journals
	// the failed update.
	a.set(func(f *fakePSUController) {
		f.boards[psuModuleID(1, 1)] = &mdk.PSUFirmwareBoardStatus{Phase: psuRecovery, RecoveryRequired: true, CapsuleId: capsuleID, PsuLeader: true}
		f.boards[psuModuleID(1, 2)] = &mdk.PSUFirmwareBoardStatus{Phase: psuRecovery, RecoveryRequired: true, CapsuleId: strings.Repeat("cd", 32)}
	})
	refused := func(job *mdk.PSUFirmwareUpdate) {
		job.State, job.ApplyRequested, job.Error = psuFailed, false, job.Members[0].GetMacAddress()+" reports a PSU that needs recovery; re-run with recover"
	}
	command.send(t)
	a.finish(psuModuleID(1, 1), refused)
	require.Equal(t, sdk.FirmwareStateContinue, command.poll(t).State)
	starts := a.started()
	require.Len(t, starts, 2)
	require.Equal(t, psuModuleID(1, 1), starts[1].GetModuleId())
	require.True(t, starts[1].GetRecover(), "a refused plain job must be retried as a recovery of this capsule")

	a.finish(psuModuleID(1, 1), nil)
	command.poll(t)
	require.Equal(t, []string{psuModuleID(1, 2)}, a.running())
	a.finish(psuModuleID(1, 2), refused)
	current := command.poll(t)
	require.Equal(t, firmwareStateFailed, current.State)
	require.Contains(t, *current.Error, "its PSU needs recovery with capsule cdcdcdcdcdcd; send that capsule again")
	require.Contains(t, *current.Error, "update stopped because tank 1 module 2 was left out of mining; 94 modules were not attempted")
	require.Len(t, a.started(), 3)
}

func TestPSUFirmwareResumesInsteadOfStartingAgain(t *testing.T) {
	dev, controllers := newPSUDevice(t, psuContainerMap(oneController), "a")
	a := controllers["a"]
	first, _ := newPSUCommand(t, dev)
	capsule := first.capsule
	first.send(t)

	// An automatic retry of the command resumes its update.
	psuCommand{dev: newHandle(dev), capsule: capsule, request: first.request}.send(t)
	require.Len(t, a.uploaded(), 1)
	require.Len(t, a.started(), 1)

	// Another command is refused while a command still polls the update.
	requireWriteOutcome(t, dev.FirmwareUpdate(t.Context(), capsule.file("batch/2")), sdk.WriteNotDispatched)
	requireWriteOutcome(t, dev.FirmwareUpdate(t.Context(), firmwareFile(t)), sdk.WriteNotDispatched)
	require.Len(t, a.started(), 1)

	// An update nobody polls has stopped; a new command takes over its
	// running job instead of starting the module again.
	dev.currentPSUAttempt().polledAt = time.Now().Add(-2 * psuAttemptLiveWindow)
	second := psuCommand{dev: dev, capsule: capsule, request: "batch/2"}
	second.send(t)
	require.Len(t, a.started(), 1)
	require.Len(t, a.uploaded(), 2)
	a.finish(psuModuleID(1, 1), nil)
	second.poll(t)
	require.Equal(t, []string{psuModuleID(1, 2)}, a.running())

	// A restarted plugin finds the running job the same way.
	restarted := &Device{id: "restarted", info: sdk.DeviceInfo{SerialNumber: dev.info.SerialNumber + "-restarted"}, clients: dev.clients}
	second.dev = restarted
	second.send(t)
	require.Len(t, a.started(), 2)
	require.Equal(t, firmwareStateCompleted, second.run(t, controllers).State)
	require.Len(t, a.started(), sdk.CT1TankCount*sdk.CT1ModulesPerTank)
}

func TestPSUFirmwareWaitsForAHashboardUpdate(t *testing.T) {
	dev, controllers := newPSUDevice(t, psuContainerMap(oneController), "a")
	jobs := map[string]*mdk.FirmwareUpdate{"a": {TaskId: "task", ServerId: "a", Status: mdk.FirmwareUpdateStatus_FIRMWARE_UPDATE_STATUS_RUNNING}}
	dev.clients[0].firmware = firmwareStatusClient{jobs: jobs}
	dev.setFirmwareAttempt(&firmwareAttempt{taskID: "task", owners: map[string][]string{"a": {"board"}}})
	command, _ := newPSUCommand(t, dev)
	requireWriteOutcome(t, dev.FirmwareUpdate(t.Context(), command.capsule.file(command.request)), sdk.WriteNotDispatched)
	require.Empty(t, controllers["a"].uploaded())

	jobs["a"].Status = mdk.FirmwareUpdateStatus_FIRMWARE_UPDATE_STATUS_COMPLETED
	jobs["a"].Targets = []*mdk.FirmwareUpdateTarget{{MacAddress: "board", Status: mdk.FirmwareTargetStatus_FIRMWARE_TARGET_STATUS_SUCCEEDED}}
	command.send(t)
	require.Equal(t, firmwareStateInstalling, psuStatus(t, dev).State, "status now reports the PSU update")
}

func TestPSUFirmwareRefusesBeforeStarting(t *testing.T) {
	valid, _ := psuCapsuleBytes(t)
	damaged := bytes.Clone(valid)
	damaged[600] ^= 1
	other := strings.Repeat("ab", 32)
	for _, tc := range []struct {
		name    string
		capsule []byte
		wire    func(*mdk.GetContainerMapResponse)
		setup   func(*fakePSUController)
		outcome string
		message string
	}{
		{name: "damaged capsule", capsule: damaged, outcome: sdk.WriteNotDispatched, message: "invalid PSU firmware capsule"},
		{name: "partial map", wire: func(w *mdk.GetContainerMapResponse) { w.ContainerMap.Tanks = w.ContainerMap.Tanks[1:] }, outcome: sdk.WriteNotDispatched, message: "container map is incomplete"},
		{name: "no module identities", wire: func(w *mdk.GetContainerMapResponse) {
			for _, tank := range w.ContainerMap.Tanks {
				for _, module := range tank.Modules {
					for _, slot := range module.Slots {
						slot.Hashboard.ModuleId = ""
					}
				}
			}
		}, outcome: sdk.WriteNotDispatched, message: "no PSU modules to update"},
		{name: "unpaired owner", wire: func(w *mdk.GetContainerMapResponse) {
			for _, slot := range w.ContainerMap.Tanks[5].Modules[0].Slots {
				slot.Hashboard.OwnerServerId = "b"
			}
		}, outcome: sdk.WriteNotDispatched, message: "tank 6 module 1 is owned by controller b, which is not paired with Fleet"},
		{name: "older Frigate", setup: func(f *fakePSUController) { f.listErr = status.Error(codes.Unimplemented, "unknown service") }, outcome: sdk.WriteNotDispatched, message: "does not support PSU firmware updates"},
		{name: "unreadable jobs", setup: func(f *fakePSUController) { f.listErr = status.Error(codes.Unavailable, "restarting") }, outcome: sdk.WriteNotDispatched, message: "read PSU firmware jobs"},
		{name: "another capsule running", setup: func(f *fakePSUController) { f.addJob(psuModuleID(2, 3), other, false) }, outcome: sdk.WriteNotDispatched, message: "tank 2 module 3 is being updated with another PSU firmware capsule"},
		{name: "capsule refused", setup: func(f *fakePSUController) {
			f.uploadErr = status.Error(codes.ResourceExhausted, "PSU firmware capsule cache is full")
		}, outcome: sdk.WriteNotDispatched, message: "CT1 refused the PSU firmware capsule"},
		{name: "upload interrupted", setup: func(f *fakePSUController) { f.uploadErr = status.Error(codes.Unavailable, "restarting") }, outcome: sdk.WriteNotDispatched, message: "upload PSU firmware capsule"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wire := psuContainerMap(oneController)
			if tc.wire != nil {
				tc.wire(wire)
			}
			dev, controllers := newPSUDevice(t, wire, "a")
			if tc.setup != nil {
				controllers["a"].set(tc.setup)
			}
			capsule := tc.capsule
			if capsule == nil {
				capsule = valid
			}
			err := dev.FirmwareUpdate(t.Context(), psuCapsuleData(capsule).file("batch/1"))
			requireWriteOutcome(t, err, tc.outcome)
			require.Contains(t, err.Error(), tc.message)
			require.Empty(t, controllers["a"].started())
			require.Nil(t, dev.currentPSUAttempt())
		})
	}
}

func TestPSUFirmwareStartOutcomes(t *testing.T) {
	t.Run("an unconfirmed start is looked up, not repeated", func(t *testing.T) {
		dev, controllers := newPSUDevice(t, psuContainerMap(oneController), "a")
		a := controllers["a"]
		a.set(func(f *fakePSUController) {
			f.startResults[psuModuleID(1, 1)] = []fakePSUStart{{create: true, err: status.Error(codes.Unavailable, "connection reset")}}
			f.startResults[psuModuleID(1, 2)] = []fakePSUStart{{err: status.Error(codes.DeadlineExceeded, "deadline exceeded")}}
		})
		command, _ := newPSUCommand(t, dev)
		command.send(t)
		require.Equal(t, firmwareStateInstalling, command.poll(t).State)
		require.Len(t, a.started(), 1, "the job the lost response created is adopted")

		a.finish(psuModuleID(1, 1), nil)
		require.Equal(t, sdk.FirmwareStateContinue, command.poll(t).State)
		require.Empty(t, a.running(), "the second start never reached the controller")
		require.Len(t, a.started(), 2)
		require.Equal(t, sdk.FirmwareStateContinue, command.poll(t).State)
		require.Equal(t, []string{psuModuleID(1, 2)}, a.running(), "a start that created no job is sent again")
		require.Len(t, a.started(), 3)
	})

	t.Run("a refused start moves to the next module", func(t *testing.T) {
		wire := psuContainerMap(oneController)
		wire.ContainerMap.Tanks[0].Modules[1].Slots[0].Hashboard.OwnerServerId = "b"
		dev, controllers := newPSUDevice(t, wire, "a", "b")
		a := controllers["a"]
		a.set(func(f *fakePSUController) {
			f.startResults[psuModuleID(1, 1)] = []fakePSUStart{{err: status.Error(codes.FailedPrecondition, "PSU module member is assigned to another Frigate node")}}
		})
		command, _ := newPSUCommand(t, dev)
		command.send(t)
		require.Equal(t, []string{psuModuleID(1, 3)}, a.running(), "the refused and the split module are passed over")
		require.Empty(t, controllers["b"].uploaded(), "a controller with nothing to update gets no capsule")
		done := command.run(t, controllers)
		require.Equal(t, firmwareStateFailed, done.State)
		require.Contains(t, *done.Error, "tank 1 module 1 ("+psuModuleID(1, 1)+"): controller refused the update: PSU module member is assigned to another Frigate node")
		require.Contains(t, *done.Error, "tank 1 module 2 ("+psuModuleID(1, 2)+"): its boards are owned by different controllers; Frigate cannot update a split module")
		require.Equal(t, 100, *done.Progress, "every module finished, updated or failed")
	})

	t.Run("a job the controller forgot has an unknown outcome", func(t *testing.T) {
		dev, controllers := newPSUDevice(t, psuContainerMap(oneController), "a")
		a := controllers["a"]
		command, _ := newPSUCommand(t, dev)
		command.send(t)
		a.set(func(f *fakePSUController) { f.jobs = nil })
		current := psuStatus(t, dev)
		require.Equal(t, firmwareStateFailed, current.State)
		require.Contains(t, *current.Error, "the controller no longer has its update job; the PSU may still be programming or need recovery")
		require.Contains(t, *current.Error, "update stopped because tank 1 module 1 has an unknown outcome; 95 modules were not attempted")
		require.Len(t, a.started(), 1)
	})

	t.Run("an unreachable controller is not a result", func(t *testing.T) {
		dev, controllers := newPSUDevice(t, psuContainerMap(oneController), "a")
		command, _ := newPSUCommand(t, dev)
		command.send(t)
		unreachable, err := NewClient(sdk.DeviceInfo{Host: "127.0.0.1", Port: 1, RPCPort: 1, URLScheme: "http"}, sdk.UsernamePassword{})
		require.NoError(t, err)
		t.Cleanup(func() { _ = unreachable.Close() })
		unreachable.memberID = "a"
		reachable := dev.clients[0]
		dev.clients[0] = unreachable
		require.Equal(t, firmwareStateInstalling, psuStatus(t, dev).State)
		dev.clients[0] = reachable
		require.Equal(t, firmwareStateCompleted, command.run(t, controllers).State)
	})
}

func TestPSUFirmwareEvidenceIsAttachedToEveryBoardOfAModule(t *testing.T) {
	now := time.Now().UTC()
	job := func(task, owner, module string, created time.Time, state mdk.PSUFirmwareUpdateState) *mdk.PSUFirmwareUpdate {
		return &mdk.PSUFirmwareUpdate{TaskId: task, ServerId: owner, ModuleId: module, CapsuleId: "capsule", State: state, CreatedAt: timestamppb.New(created)}
	}
	old := job("old", "a", "m1", now.Add(-time.Hour), psuSucceeded)
	failed := job("failed", "b", "m1", now, psuFailed)
	psuRecoveryFailure("capsule")(failed)
	failed.Members = []*mdk.PSUFirmwareUpdateMember{{MacAddress: "aa:bb:cc:dd:ee:01", RestoreError: psuHeld}}
	failed.Recover = true
	programming := job("programming", "a", "m2", now, psuApplying)
	programming.LeaderStatus = &mdk.PSUFirmwareBoardStatus{Phase: mdk.PSUFirmwarePhase_PSU_FIRMWARE_PHASE_PROGRAMMING, TotalSizeBytes: 1000, ProgrammedBytes: 500}

	selected := &controllerProbe{
		client:  &Client{psu: psuJobsClient{jobs: []*mdk.PSUFirmwareUpdate{old, programming}}},
		standby: &controllerProbe{client: &Client{psu: psuJobsClient{jobs: []*mdk.PSUFirmwareUpdate{failed}}}},
	}
	response := <-observePSUFirmwareJobs(t.Context(), selected)
	require.Len(t, response.GetUpdates(), 3)

	board := func(mac, module string) sdk.ContainerHashboard {
		return sdk.ContainerHashboard{MACAddress: mac, MetadataJSON: []byte(`{"serial":"` + mac + `","moduleId":"` + module + `"}`)}
	}
	snapshot := &sdk.ContainerSnapshot{
		Tanks:               []sdk.ContainerTank{{Modules: []sdk.ContainerModule{{Hashboards: []sdk.ContainerHashboard{board("aa:bb:cc:dd:ee:01", "m1"), board("aa:bb:cc:dd:ee:02", "m1")}}, {Hashboards: []sdk.ContainerHashboard{board("aa:bb:cc:dd:ee:03", "m2")}}}}},
		UnlocatedHashboards: []sdk.ContainerHashboard{board("aa:bb:cc:dd:ee:04", "m3")},
	}
	enrichFirmwareEvidence(snapshot, nil, response, now)
	for _, hashboard := range snapshot.Tanks[0].Modules[0].Hashboards {
		metadata := string(hashboard.MetadataJSON)
		require.Contains(t, metadata, `"psuFirmwareUpdate":{"taskId":"failed","serverId":"b","capsuleId":"capsule","state":"PSU_FIRMWARE_UPDATE_STATE_FAILED","phase":"PSU_FIRMWARE_PHASE_RECOVERY_REQUIRED","progressPercent":0,"recover":true,"recoveryRequired":true,"miningHeld":true,"message":"PSU firmware update failed in PSU_FW_PHASE_RECOVERY_REQUIRED: PSU_FW_ERROR_I2C"`)
		require.Contains(t, metadata, `"serial":"`+hashboard.MACAddress+`"`)
		require.NoError(t, sdk.ValidateHashboardJSON(hashboard.MetadataJSON))
	}
	require.Contains(t, string(snapshot.Tanks[0].Modules[1].Hashboards[0].MetadataJSON), `"state":"PSU_FIRMWARE_UPDATE_STATE_APPLYING","phase":"PSU_FIRMWARE_PHASE_PROGRAMMING","progressPercent":65`)
	require.NotContains(t, string(snapshot.UnlocatedHashboards[0].MetadataJSON), "psuFirmwareUpdate", "a module without a job gets no evidence")
}

type psuJobsClient struct {
	mdk.PSUFirmwareServiceClient
	jobs []*mdk.PSUFirmwareUpdate
}

func (c psuJobsClient) ListPSUFirmwareUpdates(context.Context, *mdk.ListPSUFirmwareUpdatesRequest, ...grpc.CallOption) (*mdk.ListPSUFirmwareUpdatesResponse, error) {
	return &mdk.ListPSUFirmwareUpdatesResponse{Updates: c.jobs}, nil
}
