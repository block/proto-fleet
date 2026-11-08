package ct1

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/block/proto-fleet/sdk/mdk/firmware/firmwaretest"
	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
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

func TestObservedHashboardFirmwareRequiresUniformFreshInventory(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		name   string
		change func(*mdk.Slot)
		want   string
	}{
		{"uniform", func(*mdk.Slot) {}, "2.0.0"},
		{"mixed", func(s *mdk.Slot) { s.Hashboard.FirmwareVersion = "3.0.0" }, ""},
		{"missing version", func(s *mdk.Slot) { s.Hashboard.FirmwareVersion = "" }, ""},
		{"missing board", func(s *mdk.Slot) { s.Hashboard = nil }, ""},
		{"missing health", func(s *mdk.Slot) { s.Hashboard.Health = nil }, ""},
		{"stale", func(s *mdk.Slot) {
			s.Hashboard.Health.Timestamp = timestamppb.New(now.Add(-healthMaxAge - time.Second))
		}, ""},
		{"future", func(s *mdk.Slot) {
			s.Hashboard.Health.Timestamp = timestamppb.New(now.Add(clockSkewAllowance + time.Second))
		}, ""},
		{"duplicate identity", func(s *mdk.Slot) { s.Hashboard.MacAddress = "aa:bb:cc:dd:ee:01" }, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			board := func(mac string) *mdk.Slot {
				return &mdk.Slot{Occupied: true, Hashboard: &mdk.Hashboard{MacAddress: mac, FirmwareVersion: "v2.0.0", Health: &mdk.HashboardHealth{Timestamp: timestamppb.New(now)}}}
			}
			first, second := board("aa:bb:cc:dd:ee:01"), board("aa:bb:cc:dd:ee:02")
			tc.change(second)
			wire := &mdk.GetContainerMapResponse{ContainerMap: &mdk.ContainerMap{Tanks: []*mdk.Tank{
				{Id: 1, Modules: []*mdk.Module{{Id: 1, Slots: []*mdk.Slot{first}}}},
				// Frigate reports unlocated inventory at tank/module zero.
				{Modules: []*mdk.Module{{Slots: []*mdk.Slot{second}}}},
			}}}
			require.Equal(t, tc.want, observedHashboardFirmware(wire, now))
		})
	}
	require.Empty(t, observedHashboardFirmware(nil, now))
}

func (c firmwareStatusClient) GetFirmwareUpdate(_ context.Context, req *mdk.GetFirmwareUpdateRequest, _ ...grpc.CallOption) (*mdk.GetFirmwareUpdateResponse, error) {
	job := c.jobs[req.ServerId]
	if job == nil {
		return nil, fmt.Errorf("controller unavailable")
	}
	return &mdk.GetFirmwareUpdateResponse{Update: job}, nil
}

func TestFirmwareStatusRequiresEveryOwnerAndBoard(t *testing.T) {
	jobs := map[string]*mdk.FirmwareUpdate{}
	for _, owner := range []string{"a", "b"} {
		jobs[owner] = &mdk.FirmwareUpdate{TaskId: "task", ServerId: owner, Status: mdk.FirmwareUpdateStatus_FIRMWARE_UPDATE_STATUS_COMPLETED, Targets: []*mdk.FirmwareUpdateTarget{{MacAddress: owner, Status: mdk.FirmwareTargetStatus_FIRMWARE_TARGET_STATUS_SUCCEEDED}}}
	}
	d := &Device{clients: []*Client{{firmware: firmwareStatusClient{jobs: jobs}}}, firmwareAttempt: &firmwareAttempt{taskID: "task", owners: map[string][]string{"a": {"a"}, "b": {"b"}}}}
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
	err = d.FirmwareUpdate(t.Context(), sdk.FirmwareFile{})
	require.ErrorContains(t, err, "previous CT1 firmware job is unfinished")
	result, err = d.GetFirmwareUpdateStatus(t.Context())
	require.NoError(t, err)
	require.Equal(t, "installing", result.State, "a failed board cannot release another owner's running job")
	jobs["a"].Status = mdk.FirmwareUpdateStatus_FIRMWARE_UPDATE_STATUS_COMPLETED
	result, err = d.GetFirmwareUpdateStatus(t.Context())
	require.NoError(t, err)
	require.Equal(t, "error", result.State)
	require.Contains(t, *result.Error, "verify_failed")
}

func testFirmwareSimulator(t *testing.T, dev *Device) {
	archive := firmwaretest.Archive(t, map[string][]byte{"renamed.bin": firmwaretest.Image(t, "dev", "f1", 42)})
	f, err := os.Open(archive)
	require.NoError(t, err)
	defer f.Close()
	require.NoError(t, dev.FirmwareUpdate(t.Context(), sdk.FirmwareFile{Reader: f, Filename: "release.zip"}))
	initial, err := dev.GetFirmwareUpdateStatus(t.Context())
	require.NoError(t, err)
	require.Equal(t, "installing", initial.State)
	require.Eventually(t, func() bool {
		result, err := dev.GetFirmwareUpdateStatus(t.Context())
		return err == nil && result.State == "completed"
	}, 10*time.Second, 100*time.Millisecond)
	metrics, err := dev.Status(t.Context())
	require.NoError(t, err)
	require.Equal(t, "42.0.0", metrics.Container.HashboardFirmwareVersion)
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
