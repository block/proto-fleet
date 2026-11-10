package ct1

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	validate "buf.build/gen/go/bufbuild/protovalidate/protocolbuffers/go/buf/validate"
	hbfirmware "github.com/block/proto-fleet/sdk/mdk/firmware"
	"github.com/block/proto-fleet/sdk/mdk/firmware/firmwaretest"
	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type firmwareStatusClient struct {
	mdk.FirmwareServiceClient
	jobs map[string]*mdk.FirmwareUpdate
}

func (c firmwareStatusClient) ListFirmwareUpdates(_ context.Context, req *mdk.ListFirmwareUpdatesRequest, _ ...grpc.CallOption) (*mdk.ListFirmwareUpdatesResponse, error) {
	if !req.LocalOnly {
		return nil, fmt.Errorf("cluster lists erase job ownership")
	}
	response := &mdk.ListFirmwareUpdatesResponse{}
	for _, job := range c.jobs {
		response.Updates = append(response.Updates, job)
	}
	return response, nil
}

func (c firmwareStatusClient) GetFirmwareUpdate(_ context.Context, req *mdk.GetFirmwareUpdateRequest, _ ...grpc.CallOption) (*mdk.GetFirmwareUpdateResponse, error) {
	job := c.jobs[req.ServerId]
	if job == nil {
		return nil, fmt.Errorf("controller unavailable")
	}
	return &mdk.GetFirmwareUpdateResponse{Update: job}, nil
}

func TestFirmwareObservationRetainsLatestJobOwner(t *testing.T) {
	now := time.Now()
	job := func(task, owner string, started time.Time) *mdk.FirmwareUpdate {
		return &mdk.FirmwareUpdate{TaskId: task, ServerId: owner, StartedAt: timestamppb.New(started), Targets: []*mdk.FirmwareUpdateTarget{{MacAddress: "aa:bb:cc:dd:ee:01", Status: mdk.FirmwareTargetStatus_FIRMWARE_TARGET_STATUS_FLASHING, ProgressPercent: 42}}}
	}
	selected := &controllerProbe{client: &Client{firmware: firmwareStatusClient{jobs: map[string]*mdk.FirmwareUpdate{"old": job("old", "a", now.Add(-time.Hour))}}}, standby: &controllerProbe{client: &Client{firmware: firmwareStatusClient{jobs: map[string]*mdk.FirmwareUpdate{"new": job("new", "b", now)}}}}}
	response := <-observeFirmwareJobs(t.Context(), selected)
	require.Len(t, response.Updates, 2)
	snapshot := &sdk.ContainerSnapshot{UnlocatedHashboards: []sdk.ContainerHashboard{{MACAddress: "aa:bb:cc:dd:ee:01", MetadataJSON: []byte(`{"serial":"board"}`)}}}
	enrichFirmwareBoards(snapshot, response, now)
	require.Contains(t, string(snapshot.UnlocatedHashboards[0].MetadataJSON), `"serverId":"b"`)
	require.Contains(t, string(snapshot.UnlocatedHashboards[0].MetadataJSON), `"taskId":"new"`)
	require.Contains(t, string(snapshot.UnlocatedHashboards[0].MetadataJSON), `"serial":"board"`)
}

func boardMAC(tank, module int32, slot uint32) string {
	return fmt.Sprintf("02:00:00:%02x:%02x:%02x", tank, module, slot)
}

func firmwareContainerMap(now time.Time) *mdk.GetContainerMapResponse {
	out := &mdk.GetContainerMapResponse{ContainerMap: &mdk.ContainerMap{}}
	for tank := int32(1); tank <= sdk.CT1TankCount; tank++ {
		t := &mdk.Tank{Id: tank}
		for module := int32(1); module <= sdk.CT1ModulesPerTank; module++ {
			m := &mdk.Module{Id: module}
			for slot := uint32(1); slot <= sdk.CT1HashboardsPerModule; slot++ {
				m.Slots = append(m.Slots, &mdk.Slot{Occupied: true, Hashboard: &mdk.Hashboard{
					MacAddress: boardMAC(tank, module, slot), FirmwareVersion: "v2.0.0",
					Location: &mdk.HashboardLocation{HashboardPosition: proto.Uint32(slot)},
					Health:   &mdk.HashboardHealth{Timestamp: timestamppb.New(now)},
				}})
			}
			t.Modules = append(t.Modules, m)
		}
		out.ContainerMap.Tanks = append(out.ContainerMap.Tanks, t)
	}
	return out
}

func firstBoard(wire *mdk.GetContainerMapResponse) *mdk.Hashboard {
	return wire.ContainerMap.Tanks[0].Modules[0].Slots[0].Hashboard
}

func unlocatedTank(boards ...*mdk.Hashboard) *mdk.Tank {
	module := &mdk.Module{}
	for _, board := range boards {
		module.Slots = append(module.Slots, &mdk.Slot{Occupied: true, Hashboard: board})
	}
	return &mdk.Tank{Modules: []*mdk.Module{module}}
}

func TestObservedHashboardFirmwareRequiresCompleteFreshInventory(t *testing.T) {
	now := time.Now().UTC()
	unlocated := func(version string) *mdk.Hashboard {
		return &mdk.Hashboard{MacAddress: "aa:bb:cc:dd:ee:99", FirmwareVersion: version, Health: &mdk.HashboardHealth{Timestamp: timestamppb.New(now)}}
	}
	for _, tc := range []struct {
		name        string
		change      func(*mdk.GetContainerMapResponse)
		wantVersion string
		wantMixed   bool
	}{
		{"uniform", func(*mdk.GetContainerMapResponse) {}, "2.0.0", false},
		{"uniform with unlocated", func(w *mdk.GetContainerMapResponse) {
			w.ContainerMap.Tanks = append(w.ContainerMap.Tanks, unlocatedTank(unlocated("2.0.0")))
		}, "2.0.0", false},
		{"mixed", func(w *mdk.GetContainerMapResponse) { firstBoard(w).FirmwareVersion = "3.0.0" }, "", true},
		{"mixed unlocated", func(w *mdk.GetContainerMapResponse) {
			w.ContainerMap.Tanks = append(w.ContainerMap.Tanks, unlocatedTank(unlocated("1.0.0")))
		}, "", true},
		{"missing version", func(w *mdk.GetContainerMapResponse) { firstBoard(w).FirmwareVersion = "" }, "", false},
		{"missing board", func(w *mdk.GetContainerMapResponse) { w.ContainerMap.Tanks[0].Modules[0].Slots[0].Hashboard = nil }, "", false},
		{"missing health", func(w *mdk.GetContainerMapResponse) { firstBoard(w).Health = nil }, "", false},
		{"stale", func(w *mdk.GetContainerMapResponse) {
			firstBoard(w).Health.Timestamp = timestamppb.New(now.Add(-healthMaxAge - time.Second))
		}, "", false},
		{"future", func(w *mdk.GetContainerMapResponse) {
			firstBoard(w).Health.Timestamp = timestamppb.New(now.Add(clockSkewAllowance + time.Second))
		}, "", false},
		{"duplicate identity", func(w *mdk.GetContainerMapResponse) { firstBoard(w).MacAddress = boardMAC(1, 1, 2) }, "", false},
		{"missing module", func(w *mdk.GetContainerMapResponse) {
			w.ContainerMap.Tanks[2].Modules = w.ContainerMap.Tanks[2].Modules[1:]
		}, "", false},
		{"missing tank", func(w *mdk.GetContainerMapResponse) { w.ContainerMap.Tanks = w.ContainerMap.Tanks[1:] }, "", false},
		{"unlocated only", func(w *mdk.GetContainerMapResponse) {
			w.ContainerMap.Tanks = []*mdk.Tank{unlocatedTank(unlocated("2.0.0"))}
		}, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wire := firmwareContainerMap(now)
			tc.change(wire)
			topology, err := snapshot(mapResponse(wire), now)
			if err != nil {
				topology = nil
			}
			version, mixed := observedHashboardFirmware(wire, topology, now)
			require.Equal(t, tc.wantVersion, version)
			require.Equal(t, tc.wantMixed, mixed)
		})
	}
	version, mixed := observedHashboardFirmware(nil, nil, now)
	require.Empty(t, version)
	require.False(t, mixed)
}

func TestFirmwareStatusRequiresEveryOwnerAndBoard(t *testing.T) {
	jobs := map[string]*mdk.FirmwareUpdate{}
	for _, owner := range []string{"a", "b"} {
		jobs[owner] = &mdk.FirmwareUpdate{TaskId: "task", ServerId: owner, Status: mdk.FirmwareUpdateStatus_FIRMWARE_UPDATE_STATUS_COMPLETED, Targets: []*mdk.FirmwareUpdateTarget{{MacAddress: owner, Status: mdk.FirmwareTargetStatus_FIRMWARE_TARGET_STATUS_SUCCEEDED}}}
	}
	d := &Device{clients: []*Client{{firmware: firmwareStatusClient{jobs: jobs}}}, firmware: &firmwareState{attempt: &firmwareAttempt{taskID: "task", owners: map[string][]string{"a": {"a"}, "b": {"b"}}}}}
	result, err := d.GetFirmwareUpdateStatus(t.Context())
	require.NoError(t, err)
	require.Equal(t, "completed", result.State)
	peer := jobs["b"]
	delete(jobs, "b")
	_, err = d.GetFirmwareUpdateStatus(t.Context())
	require.Error(t, err)
	jobs["b"] = peer
	peer.Targets = nil
	_, err = d.GetFirmwareUpdateStatus(t.Context())
	require.ErrorContains(t, err, "missing hashboard")
	peer.Targets = []*mdk.FirmwareUpdateTarget{{MacAddress: "b", Status: mdk.FirmwareTargetStatus_FIRMWARE_TARGET_STATUS_FAILED, Reason: "verify_failed"}}
	peer.Status = mdk.FirmwareUpdateStatus_FIRMWARE_UPDATE_STATUS_COMPLETED_WITH_ERRORS
	jobs["a"].Status = mdk.FirmwareUpdateStatus_FIRMWARE_UPDATE_STATUS_RUNNING
	err = d.FirmwareUpdate(t.Context(), sdk.FirmwareFile{Reader: strings.NewReader("")})
	require.ErrorContains(t, err, "previous CT1 firmware job is unfinished")
	result, err = d.GetFirmwareUpdateStatus(t.Context())
	require.NoError(t, err)
	require.Equal(t, "installing", result.State, "a failed board cannot release another owner's running job")
	jobs["a"].Status = mdk.FirmwareUpdateStatus_FIRMWARE_UPDATE_STATUS_COMPLETED
	result, err = d.GetFirmwareUpdateStatus(t.Context())
	require.NoError(t, err)
	require.Equal(t, "failed", result.State)
	require.Contains(t, *result.Error, "verify_failed")
}

func TestFirmwareFailureMessageFitsStatusBound(t *testing.T) {
	var failures []string
	for i := range 288 {
		failures = append(failures, fmt.Sprintf("%s (owner): verify_failed %d %s\xff", boardMAC(1, 1, 1), i, strings.Repeat("é", 400)))
	}
	field := (&gatewaypb.FirmwareUpdateStatusResult{}).ProtoReflect().Descriptor().Fields().ByName("error")
	rules, ok := proto.GetExtension(field.Options(), validate.E_Field).(*validate.FieldRules)
	require.True(t, ok)
	require.Equal(t, uint64(maxFirmwareStatusErrorBytes), rules.GetString().GetMaxLen(), "Fleet Node rejects longer status errors")
	message := firmwareFailureMessage(failures)
	require.LessOrEqual(t, len(message), maxFirmwareStatusErrorBytes)
	require.True(t, utf8.ValidString(message))
	require.True(t, strings.HasPrefix(message, "288 hashboard firmware failures: "))
	require.Regexp(t, `; \.\.\. and \d+ more$`, message)
	require.Equal(t, "1 hashboard firmware failures: one", firmwareFailureMessage([]string{"one"}))
}

type fakeStart struct {
	create bool
	err    error
}

type fakeFirmwareController struct {
	mdk.UnimplementedFirmwareServiceServer
	mdk.UnimplementedHashboardServiceServer

	mu            sync.Mutex
	wire          *mdk.GetContainerMapResponse
	starts        []*mdk.StartFirmwareUpdateRequest
	startResults  []fakeStart
	jobs          map[string]*mdk.FirmwareUpdate
	statusReads   map[string]bool
	uploads       int
	uploadActors  []string
	uploadStarted chan struct{}
	uploadGate    chan struct{}
	corruptUpload bool
	skipOwners    map[string]bool
	unreachable   map[string]bool
}

func (f *fakeFirmwareController) GetContainerMap(context.Context, *mdk.GetContainerMapRequest) (*mdk.GetContainerMapResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return proto.CloneOf(f.wire), nil
}

func (f *fakeFirmwareController) UploadFirmwareArtifact(stream mdk.FirmwareService_UploadFirmwareArtifactServer) error {
	md, _ := metadata.FromIncomingContext(stream.Context())
	f.mu.Lock()
	f.uploads++
	f.uploadActors = append(f.uploadActors, md.Get("x-frigate-actor")...)
	started, gate, corrupt := f.uploadStarted, f.uploadGate, f.corruptUpload
	f.mu.Unlock()
	if started != nil {
		close(started)
	}
	if gate != nil {
		<-gate
	}
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	data := make([][]byte, len(first.GetMetadata().GetImages()))
	for {
		message, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		chunk := message.GetChunk()
		data[chunk.ImageIndex] = append(data[chunk.ImageIndex], chunk.Data...)
	}
	response := &mdk.UploadFirmwareArtifactResponse{Artifact: &mdk.FirmwareArtifactRef{Source: mdk.FirmwareArtifactSource_FIRMWARE_ARTIFACT_SOURCE_UPLOAD, UploadId: "upload"}}
	for i, image := range first.GetMetadata().GetImages() {
		meta, err := hbfirmware.DecodeMetadata(data[i])
		if err != nil {
			return status.Error(codes.InvalidArgument, err.Error()) //nolint:wrapcheck // server must return a gRPC status
		}
		checksum := image.Sha256
		if corrupt {
			checksum = strings.Repeat("0", 64)
		}
		response.Images = append(response.Images, &mdk.FirmwareImage{Name: image.Name, Version: meta.Version(), Build: meta.Build, Sha256: checksum})
	}
	slices.Reverse(response.Images)
	return stream.SendAndClose(response)
}

func (f *fakeFirmwareController) StartFirmwareUpdate(_ context.Context, req *mdk.StartFirmwareUpdateRequest) (*mdk.StartFirmwareUpdateResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.starts = append(f.starts, req)
	result := fakeStart{create: true}
	if len(f.startResults) > 0 {
		result, f.startResults = f.startResults[0], f.startResults[1:]
	}
	if result.create {
		owners := map[string]string{}
		for _, tank := range f.wire.GetContainerMap().GetTanks() {
			for _, module := range tank.GetModules() {
				for _, slot := range module.GetSlots() {
					owner := slot.GetHashboard().GetOwnerServerId()
					if owner == "" {
						owner = "self"
					}
					owners[normalizedBoardMAC(slot.GetHashboard().GetMacAddress())] = owner
				}
			}
		}
		for _, mac := range req.Scope.Identifiers {
			if f.skipOwners[owners[mac]] {
				continue
			}
			key := req.TaskId + "/" + owners[mac]
			job := f.jobs[key]
			if job == nil {
				job = &mdk.FirmwareUpdate{TaskId: req.TaskId, ServerId: owners[mac], Status: mdk.FirmwareUpdateStatus_FIRMWARE_UPDATE_STATUS_RUNNING}
				f.jobs[key] = job
			}
			job.Targets = append(job.Targets, &mdk.FirmwareUpdateTarget{MacAddress: mac, Status: mdk.FirmwareTargetStatus_FIRMWARE_TARGET_STATUS_QUEUED})
		}
	}
	if result.err != nil {
		return nil, result.err
	}
	return &mdk.StartFirmwareUpdateResponse{}, nil
}

func (f *fakeFirmwareController) GetFirmwareUpdate(_ context.Context, req *mdk.GetFirmwareUpdateRequest) (*mdk.GetFirmwareUpdateResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.statusReads[req.ServerId] = true
	server := req.ServerId
	if server == "" {
		server = "self"
	}
	if f.unreachable[server] {
		return nil, status.Error(codes.Unavailable, "peer controller unreachable") //nolint:wrapcheck // server must return a gRPC status
	}
	job := f.jobs[req.TaskId+"/"+server]
	if job == nil {
		return nil, status.Error(codes.NotFound, "firmware task not found on controller") //nolint:wrapcheck // server must return a gRPC status
	}
	return &mdk.GetFirmwareUpdateResponse{Update: proto.CloneOf(job)}, nil
}

func (f *fakeFirmwareController) finish(failed ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	task := f.starts[len(f.starts)-1].TaskId
	for key, job := range f.jobs {
		if !strings.HasPrefix(key, task+"/") {
			continue
		}
		job.Status = mdk.FirmwareUpdateStatus_FIRMWARE_UPDATE_STATUS_COMPLETED
		for _, target := range job.Targets {
			target.Status = mdk.FirmwareTargetStatus_FIRMWARE_TARGET_STATUS_SUCCEEDED
			for _, mac := range failed {
				if target.MacAddress == mac {
					target.Status = mdk.FirmwareTargetStatus_FIRMWARE_TARGET_STATUS_FAILED
					target.Reason = "verify_failed"
					job.Status = mdk.FirmwareUpdateStatus_FIRMWARE_UPDATE_STATUS_COMPLETED_WITH_ERRORS
				}
			}
		}
	}
}

func (f *fakeFirmwareController) dropJob(owner string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.jobs, f.starts[len(f.starts)-1].TaskId+"/"+owner)
}

func (f *fakeFirmwareController) startCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.starts)
}

func (f *fakeFirmwareController) uploadCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.uploads
}

func newFirmwareDevice(t *testing.T, wire *mdk.GetContainerMapResponse) (*Device, *fakeFirmwareController) {
	t.Helper()
	controller := &fakeFirmwareController{wire: wire, jobs: map[string]*mdk.FirmwareUpdate{}, statusReads: map[string]bool{}}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	server := grpc.NewServer()
	mdk.RegisterFirmwareServiceServer(server, controller)
	mdk.RegisterHashboardServiceServer(server, controller)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	addr, ok := listener.Addr().(*net.TCPAddr)
	require.True(t, ok)
	port := int32(addr.Port) //nolint:gosec // a TCP port fits int32
	client, err := NewClient(sdk.DeviceInfo{Host: "127.0.0.1", Port: port, RPCPort: port, URLScheme: "http"}, sdk.UsernamePassword{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })
	client.memberID = "self"
	return &Device{id: "ct1", info: sdk.DeviceInfo{SerialNumber: "ct1-" + t.Name()}, clients: []*Client{client}}, controller
}

func newHandle(dev *Device) *Device {
	return &Device{id: "command-handle", info: dev.info, clients: dev.clients}
}

func firmwareFile(t *testing.T) sdk.FirmwareFile {
	t.Helper()
	archive := firmwaretest.Archive(t, map[string][]byte{"a.bin": firmwaretest.Image(t, "prod", "f1", 2), "b.bin": firmwaretest.Image(t, "prod", "f2", 2)})
	f, err := os.Open(archive)
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.Close() })
	return sdk.FirmwareFile{Reader: f, Filename: "release.zip"}
}

func requireWriteOutcome(t *testing.T, err error, outcome string) {
	t.Helper()
	require.Error(t, err)
	require.Equal(t, outcome, sdk.WriteEvidence(err).GetReason(), err.Error())
}

func TestFirmwareUpdateTargetsEveryOwner(t *testing.T) {
	wire := firmwareContainerMap(time.Now())
	for i, module := range wire.ContainerMap.Tanks[0].Modules {
		for _, slot := range module.Slots {
			if i%2 == 0 {
				slot.Hashboard.OwnerServerId = "peer"
			}
		}
	}
	dev, controller := newFirmwareDevice(t, wire)
	require.NoError(t, dev.FirmwareUpdate(t.Context(), firmwareFile(t)))
	require.Len(t, controller.starts[0].Scope.Identifiers, sdk.CT1TankCount*sdk.CT1ModulesPerTank*sdk.CT1HashboardsPerModule)
	require.Equal(t, "prod", controller.starts[0].Options.Build)
	require.Equal(t, []string{"proto-fleet"}, controller.uploadActors, "the upload stream carries audit attribution once")
	controller.finish()
	result, err := dev.GetFirmwareUpdateStatus(t.Context())
	require.NoError(t, err)
	require.Equal(t, "completed", result.State)
	require.Equal(t, map[string]bool{"peer": true, "self": true}, controller.statusReads)
}

func splitOwnerMap() (*mdk.GetContainerMapResponse, []string) {
	wire := firmwareContainerMap(time.Now())
	var peer []string
	for i, module := range wire.ContainerMap.Tanks[0].Modules {
		for _, slot := range module.Slots {
			if i%2 == 0 {
				slot.Hashboard.OwnerServerId = "peer"
				peer = append(peer, normalizedBoardMAC(slot.Hashboard.MacAddress))
			}
		}
	}
	return wire, peer
}

func TestFirmwareMissingOwnerJobIsTerminal(t *testing.T) {
	t.Run("record lost after completion", func(t *testing.T) {
		wire, peer := splitOwnerMap()
		dev, controller := newFirmwareDevice(t, wire)
		require.NoError(t, dev.FirmwareUpdate(t.Context(), firmwareFile(t)))
		controller.finish()
		controller.dropJob("peer")
		result, err := dev.GetFirmwareUpdateStatus(t.Context())
		require.NoError(t, err)
		require.Equal(t, "failed", result.State)
		require.Contains(t, *result.Error, "controller peer: firmware task not found")
		require.NoError(t, dev.FirmwareUpdate(t.Context(), firmwareFile(t)))
		require.Len(t, controller.starts[1].Scope.Identifiers, sdk.CT1TankCount*sdk.CT1ModulesPerTank*sdk.CT1HashboardsPerModule)
		require.Subset(t, controller.starts[1].Scope.Identifiers, peer)
	})
	t.Run("every owner lost the job", func(t *testing.T) {
		dev, controller := newFirmwareDevice(t, firmwareContainerMap(time.Now()))
		require.NoError(t, dev.FirmwareUpdate(t.Context(), requestFile(t, "batch/1")))
		controller.dropJob("self")
		result, err := dev.GetFirmwareUpdateStatus(t.Context())
		require.NoError(t, err, "a lost task is terminal, not a transient poll error")
		require.Equal(t, "failed", result.State)
		require.Contains(t, *result.Error, "controller self: firmware task not found")
		require.NoError(t, dev.FirmwareUpdate(t.Context(), requestFile(t, "batch/2")))
		require.Equal(t, 2, controller.startCount(), "a new command updates again")
	})
	t.Run("job created on one owner", func(t *testing.T) {
		wire, peer := splitOwnerMap()
		dev, controller := newFirmwareDevice(t, wire)
		controller.skipOwners = map[string]bool{"peer": true}
		require.NoError(t, dev.FirmwareUpdate(t.Context(), firmwareFile(t)))
		result, err := dev.GetFirmwareUpdateStatus(t.Context())
		require.NoError(t, err)
		require.Equal(t, "installing", result.State)
		requireWriteOutcome(t, dev.FirmwareUpdate(t.Context(), firmwareFile(t)), sdk.WriteUnknown)
		controller.finish()
		result, err = dev.GetFirmwareUpdateStatus(t.Context())
		require.NoError(t, err)
		require.Equal(t, "failed", result.State)
		controller.skipOwners = nil
		require.NoError(t, dev.FirmwareUpdate(t.Context(), firmwareFile(t)))
		require.Subset(t, controller.starts[1].Scope.Identifiers, peer)
	})
}

func TestFirmwareStartRefusalReleasesReplayGuard(t *testing.T) {
	dev, controller := newFirmwareDevice(t, firmwareContainerMap(time.Now()))
	controller.startResults = []fakeStart{{err: status.Error(codes.FailedPrecondition, "hashboard update already running")}}
	requireWriteOutcome(t, dev.FirmwareUpdate(t.Context(), requestFile(t, "batch/1")), sdk.WriteNotDispatched)
	require.NoError(t, dev.FirmwareUpdate(t.Context(), requestFile(t, "batch/1")))
	require.Equal(t, 2, controller.startCount(), "a retry of a refused start dispatches it again")
	require.Equal(t, controller.starts[0].TaskId, controller.starts[1].TaskId, "a refused start created no job, so its task ID is reused")
}

func TestFirmwareUncertainStartIsNeverReplayed(t *testing.T) {
	t.Run("job created", func(t *testing.T) {
		dev, controller := newFirmwareDevice(t, firmwareContainerMap(time.Now()))
		controller.startResults = []fakeStart{{create: true, err: status.Error(codes.Unavailable, "response lost")}}
		requireWriteOutcome(t, dev.FirmwareUpdate(t.Context(), firmwareFile(t)), sdk.WriteUnknown)
		requireWriteOutcome(t, dev.FirmwareUpdate(t.Context(), firmwareFile(t)), sdk.WriteUnknown)
		require.Equal(t, 1, controller.uploadCount(), "a running job blocks a second upload")
		require.Equal(t, 1, controller.startCount())
	})
	t.Run("task already exists", func(t *testing.T) {
		dev, controller := newFirmwareDevice(t, firmwareContainerMap(time.Now()))
		controller.startResults = []fakeStart{{create: true, err: status.Error(codes.AlreadyExists, "task already exists")}}
		requireWriteOutcome(t, dev.FirmwareUpdate(t.Context(), firmwareFile(t)), sdk.WriteUnknown)
		requireWriteOutcome(t, dev.FirmwareUpdate(t.Context(), firmwareFile(t)), sdk.WriteUnknown)
		require.Equal(t, 1, controller.startCount())
	})
	t.Run("job never created", func(t *testing.T) {
		dev, controller := newFirmwareDevice(t, firmwareContainerMap(time.Now()))
		controller.startResults = []fakeStart{{err: status.Error(codes.Unavailable, "connection reset")}}
		requireWriteOutcome(t, dev.FirmwareUpdate(t.Context(), firmwareFile(t)), sdk.WriteUnknown)
		require.NoError(t, dev.FirmwareUpdate(t.Context(), firmwareFile(t)), "no owner knows the task, so it never started")
		require.Equal(t, 2, controller.startCount())
	})
}

func TestFirmwareRequestsFlashEveryBoard(t *testing.T) {
	all := sdk.CT1TankCount * sdk.CT1ModulesPerTank * sdk.CT1HashboardsPerModule
	dev, controller := newFirmwareDevice(t, firmwareContainerMap(time.Now()))
	failed := boardMAC(2, 3, 1)
	require.NoError(t, dev.FirmwareUpdate(t.Context(), firmwareFile(t)))
	controller.finish(failed)
	result, err := dev.GetFirmwareUpdateStatus(t.Context())
	require.NoError(t, err)
	require.Equal(t, "failed", result.State)
	require.Contains(t, *result.Error, failed)

	require.NoError(t, dev.FirmwareUpdate(t.Context(), firmwareFile(t)))
	require.Len(t, controller.starts[1].Scope.Identifiers, all, "earlier success cannot prove a board still runs the artifact")
	controller.finish()
	result, err = dev.GetFirmwareUpdateStatus(t.Context())
	require.NoError(t, err)
	require.Equal(t, "completed", result.State)

	t.Run("reinstalls when boards already report the version", func(t *testing.T) {
		require.NoError(t, dev.FirmwareUpdate(t.Context(), firmwareFile(t)))
		require.Len(t, controller.starts[2].Scope.Identifiers, all)
		require.Equal(t, 3, controller.uploadCount())
	})
}

func TestFirmwareRetryOfCommandResumesItsJob(t *testing.T) {
	dev, controller := newFirmwareDevice(t, firmwareContainerMap(time.Now()))
	file := func(request string) sdk.FirmwareFile {
		f := firmwareFile(t)
		f.RequestID = request
		return f
	}
	require.NoError(t, dev.FirmwareUpdate(t.Context(), file("queue-message-1")))
	controller.finish()
	require.NoError(t, newHandle(dev).FirmwareUpdate(t.Context(), file("queue-message-1")))
	require.Equal(t, 1, controller.startCount(), "a retried command resumes its job instead of reflashing")
	require.Equal(t, 1, controller.uploadCount())
	result, err := newHandle(dev).GetFirmwareUpdateStatus(t.Context())
	require.NoError(t, err)
	require.Equal(t, "completed", result.State)
	require.NoError(t, dev.FirmwareUpdate(t.Context(), file("queue-message-2")))
	require.Equal(t, 2, controller.startCount(), "a new command updates again")
}

func restartPlugin(dev *Device) {
	firmwareStates.Lock()
	defer firmwareStates.Unlock()
	delete(firmwareStates.byContainer, firmwareContainerIdentity(dev.info))
}

func TestFirmwareRetryAfterPluginRestartResumesItsJob(t *testing.T) {
	for _, tc := range []struct {
		name            string
		finished        bool
		changeWhileDown func(*mdk.GetContainerMapResponse)
		untargeted      string
	}{
		{name: "running"},
		{name: "finished", finished: true},
		{name: "board replaced", changeWhileDown: func(w *mdk.GetContainerMapResponse) {
			firstBoard(w).MacAddress = "02:ff:00:00:00:01"
		}, untargeted: "02:ff:00:00:00:01"},
		{name: "ownership moved", changeWhileDown: func(w *mdk.GetContainerMapResponse) {
			for _, module := range w.ContainerMap.Tanks[0].Modules {
				for _, slot := range module.Slots {
					slot.Hashboard.OwnerServerId = ""
				}
			}
		}, untargeted: boardMAC(1, 1, 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wire, _ := splitOwnerMap()
			dev, controller := newFirmwareDevice(t, wire)
			require.NoError(t, dev.FirmwareUpdate(t.Context(), requestFile(t, "batch/1")))
			if tc.finished {
				controller.finish()
			}
			if tc.changeWhileDown != nil {
				controller.mu.Lock()
				tc.changeWhileDown(controller.wire)
				controller.mu.Unlock()
			}
			restartPlugin(dev)
			_, err := newHandle(dev).GetFirmwareUpdateStatus(t.Context())
			require.Error(t, err, "a restarted plugin knows no job until the command retries")
			require.NoError(t, newHandle(dev).FirmwareUpdate(t.Context(), requestFile(t, "batch/1")))
			require.Equal(t, 1, controller.startCount(), "the retry finds its job instead of reflashing")
			require.Equal(t, 1, controller.uploadCount())
			controller.finish()
			result, err := newHandle(dev).GetFirmwareUpdateStatus(t.Context())
			require.NoError(t, err)
			if tc.untargeted == "" {
				require.Equal(t, "completed", result.State)
			} else {
				require.Equal(t, "failed", result.State, "a board outside the resumed task was not updated")
				require.Contains(t, *result.Error, tc.untargeted)
			}

			restartPlugin(dev)
			require.NoError(t, newHandle(dev).FirmwareUpdate(t.Context(), requestFile(t, "batch/2")))
			require.Equal(t, 2, controller.startCount(), "a new command updates again")
			require.NotEqual(t, controller.starts[0].TaskId, controller.starts[1].TaskId)
		})
	}
}

func requestFile(t *testing.T, request string) sdk.FirmwareFile {
	t.Helper()
	f := firmwareFile(t)
	f.RequestID = request
	return f
}

func TestFirmwareRecoveryFindsJobOnPreviousOwner(t *testing.T) {
	dev, controller := newFirmwareDevice(t, firmwareContainerMap(time.Now()))
	require.NoError(t, dev.FirmwareUpdate(t.Context(), requestFile(t, "batch/1")))
	controller.mu.Lock()
	for _, tank := range controller.wire.ContainerMap.Tanks {
		for _, module := range tank.Modules {
			for _, slot := range module.Slots {
				slot.Hashboard.OwnerServerId = "peer"
			}
		}
	}
	controller.mu.Unlock()
	restartPlugin(dev)
	require.NoError(t, newHandle(dev).FirmwareUpdate(t.Context(), requestFile(t, "batch/1")))
	require.Equal(t, 1, controller.startCount(), "the previous owner's job is resumed, not replayed")
	controller.finish()
	result, err := newHandle(dev).GetFirmwareUpdateStatus(t.Context())
	require.NoError(t, err)
	require.Equal(t, "completed", result.State)
}

func TestFirmwareRecoveryToleratesUnnamedOwner(t *testing.T) {
	dev, controller := newFirmwareDevice(t, firmwareContainerMap(time.Now()))
	dev.clients[0].memberID = ""
	require.NoError(t, dev.FirmwareUpdate(t.Context(), requestFile(t, "batch/1")))
	restartPlugin(dev)
	require.NoError(t, newHandle(dev).FirmwareUpdate(t.Context(), requestFile(t, "batch/1")))
	require.Equal(t, 1, controller.startCount(), "the connected controller's job is resumed")
	controller.finish()
	result, err := newHandle(dev).GetFirmwareUpdateStatus(t.Context())
	require.NoError(t, err)
	require.Equal(t, "completed", result.State)
}

func TestFirmwareRecoveryIgnoresUnreachablePeer(t *testing.T) {
	dev, controller := newFirmwareDevice(t, firmwareContainerMap(time.Now()))
	self := dev.clients[0]
	dev.clients = append(dev.clients, &Client{memberID: "gone", conn: self.conn, boards: self.boards, firmware: self.firmware})
	controller.unreachable = map[string]bool{"gone": true}
	require.NoError(t, dev.FirmwareUpdate(t.Context(), requestFile(t, "batch/1")), "a down peer does not block a new command")
	require.Equal(t, 1, controller.startCount())
	restartPlugin(dev)
	require.NoError(t, newHandle(dev).FirmwareUpdate(t.Context(), requestFile(t, "batch/1")))
	require.Equal(t, 1, controller.startCount(), "a down peer does not prevent resuming the job")
	controller.unreachable = map[string]bool{"self": true}
	restartPlugin(dev)
	requireWriteOutcome(t, newHandle(dev).FirmwareUpdate(t.Context(), requestFile(t, "batch/1")), sdk.WriteNotDispatched)
	require.Equal(t, 1, controller.startCount(), "an unreachable planned owner refuses rather than replays")
}

func TestFirmwareArchiveWaitIsRetryable(t *testing.T) {
	archive := firmwaretest.Archive(t, map[string][]byte{"board.bin": firmwaretest.Image(t, "prod", "f1", 2)})
	for range hbfirmware.MaxConcurrentReads {
		held, err := hbfirmware.Read(t.Context(), archive, "")
		require.NoError(t, err)
		t.Cleanup(held.Release)
	}
	dev, controller := newFirmwareDevice(t, firmwareContainerMap(time.Now()))
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	requireWriteOutcome(t, dev.FirmwareUpdate(ctx, firmwareFile(t)), sdk.WriteNotDispatched)
	require.Zero(t, controller.uploadCount())
	require.Nil(t, dev.currentFirmwareAttempt())
}

func TestFirmwareAttemptOutlivesCommandHandle(t *testing.T) {
	dev, controller := newFirmwareDevice(t, firmwareContainerMap(time.Now()))
	require.NoError(t, dev.FirmwareUpdate(t.Context(), firmwareFile(t)))
	poller := newHandle(dev)
	result, err := poller.GetFirmwareUpdateStatus(t.Context())
	require.NoError(t, err)
	require.Equal(t, "installing", result.State)
	requireWriteOutcome(t, newHandle(dev).FirmwareUpdate(t.Context(), firmwareFile(t)), sdk.WriteUnknown)
	require.Equal(t, 1, controller.startCount(), "another handle cannot replay the running job")
	controller.finish()
	result, err = poller.GetFirmwareUpdateStatus(t.Context())
	require.NoError(t, err)
	require.Equal(t, "completed", result.State)
}

func TestFirmwareContainerIdentity(t *testing.T) {
	endpoint := sdk.DeviceEndpoint{ID: "a", DeviceKey: "container-key", Host: "10.0.0.1", Port: 80, URLScheme: "http"}
	peer := endpoint
	peer.ID, peer.Host = "b", "10.0.0.2"
	paired := firmwareContainerIdentity(sdk.DeviceInfo{SerialNumber: "x", Endpoints: []sdk.DeviceEndpoint{endpoint, peer}})
	require.Equal(t, paired, firmwareContainerIdentity(sdk.DeviceInfo{SerialNumber: "y", Endpoints: []sdk.DeviceEndpoint{endpoint, peer}}), "the endpoint key outranks the serial")
	require.Equal(t, firmwareContainerIdentity(sdk.DeviceInfo{Endpoints: []sdk.DeviceEndpoint{endpoint}}), firmwareContainerIdentity(sdk.DeviceInfo{Endpoints: []sdk.DeviceEndpoint{peer}}), "either controller names the same container")
	require.Equal(t, firmwareContainerIdentity(sdk.DeviceInfo{SerialNumber: "ct1-serial"}), firmwareContainerIdentity(sdk.DeviceInfo{SerialNumber: " ct1-serial "}))
	require.NotEqual(t, paired, firmwareContainerIdentity(sdk.DeviceInfo{SerialNumber: "container-key"}), "a serial never collides with an endpoint key")
	require.NotEqual(t, firmwareContainerIdentity(sdk.DeviceInfo{Host: "10.0.0.1", Port: 80}), firmwareContainerIdentity(sdk.DeviceInfo{Host: "10.0.0.1", Port: 81}))
}

func TestFirmwareUpdateRefusesIncompleteMap(t *testing.T) {
	wire := firmwareContainerMap(time.Now())
	wire.ContainerMap.Tanks[4].Modules = wire.ContainerMap.Tanks[4].Modules[:10]
	dev, controller := newFirmwareDevice(t, wire)
	requireWriteOutcome(t, dev.FirmwareUpdate(t.Context(), firmwareFile(t)), sdk.WriteNotDispatched)
	require.Zero(t, controller.uploadCount())
	require.Nil(t, dev.currentFirmwareAttempt())
}

func TestFirmwareUploadRejectsUnconfirmedImages(t *testing.T) {
	dev, controller := newFirmwareDevice(t, firmwareContainerMap(time.Now()))
	controller.corruptUpload = true
	err := dev.FirmwareUpdate(t.Context(), firmwareFile(t))
	requireWriteOutcome(t, err, sdk.WriteNotDispatched)
	require.ErrorContains(t, err, "different firmware image metadata")
	require.Zero(t, controller.startCount())
}

func TestFirmwareUploadDoesNotHoldWriteLock(t *testing.T) {
	dev, controller := newFirmwareDevice(t, firmwareContainerMap(time.Now()))
	controller.uploadStarted, controller.uploadGate = make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	file := firmwareFile(t)
	go func() { done <- dev.FirmwareUpdate(t.Context(), file) }()
	<-controller.uploadStarted
	require.True(t, dev.writeMu.TryLock(), "other container writes proceed during the upload")
	dev.writeMu.Unlock()
	requireWriteOutcome(t, dev.FirmwareUpdate(t.Context(), firmwareFile(t)), sdk.WriteNotDispatched)
	close(controller.uploadGate)
	require.NoError(t, <-done)
	require.Equal(t, 1, controller.startCount())
}

func testFirmwareSimulator(t *testing.T, dev *Device, info sdk.DeviceInfo) {
	archive := firmwaretest.Archive(t, map[string][]byte{"renamed.bin": firmwaretest.Image(t, "dev", "f1", 42)})
	f, err := os.Open(archive)
	require.NoError(t, err)
	defer f.Close()
	require.NoError(t, dev.FirmwareUpdate(t.Context(), sdk.FirmwareFile{Reader: f, Filename: "release.zip"}))
	poller, err := NewDevice(t.Context(), "command-status", info, sdk.UsernamePassword{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = poller.Close(t.Context()) })
	initial, err := poller.GetFirmwareUpdateStatus(t.Context())
	require.NoError(t, err)
	require.Equal(t, "installing", initial.State)
	require.Eventually(t, func() bool {
		result, err := poller.GetFirmwareUpdateStatus(t.Context())
		return err == nil && result.State == "completed"
	}, 10*time.Second, 100*time.Millisecond)
	metrics, err := dev.Status(t.Context())
	require.NoError(t, err)
	require.Equal(t, "42.0.0", metrics.Container.HashboardFirmwareVersion)
	require.False(t, metrics.Container.HashboardFirmwareMixed)
	require.NotEqual(t, "42.0.0", metrics.FirmwareVersion, "controller firmware is independent")
	for _, tank := range metrics.Container.Tanks {
		for _, module := range tank.Modules {
			for _, board := range module.Hashboards {
				if board.MACAddress != "" {
					require.Contains(t, string(board.MetadataJSON), "FIRMWARE_TARGET_STATUS_SUCCEEDED")
				}
			}
		}
	}
}
