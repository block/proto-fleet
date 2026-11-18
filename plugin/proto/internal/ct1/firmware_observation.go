package ct1

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

func observeFirmwareJobs(ctx context.Context, selected *controllerProbe) <-chan *mdk.ListFirmwareUpdatesResponse {
	result := make(chan *mdk.ListFirmwareUpdatesResponse, 1)
	go func() {
		ctx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		clients := []*Client{selected.client}
		if selected.standby != nil {
			clients = append(clients, selected.standby.client)
		}
		reads := make(chan *mdk.ListFirmwareUpdatesResponse, len(clients))
		for _, client := range clients {
			go func() {
				response, _ := client.firmware.ListFirmwareUpdates(ctx, &mdk.ListFirmwareUpdatesRequest{LocalOnly: true})
				reads <- response
			}()
		}
		out := &mdk.ListFirmwareUpdatesResponse{}
		for range clients {
			response := <-reads
			out.Updates = append(out.Updates, response.GetUpdates()...)
		}
		sort.SliceStable(out.Updates, func(i, j int) bool {
			return out.Updates[i].GetStartedAt().AsTime().After(out.Updates[j].GetStartedAt().AsTime())
		})
		result <- out
	}()
	return result
}

// observePSUFirmwareJobs reads each controller's module update jobs. Frigate
// keeps a job only on the controller that ran it.
func observePSUFirmwareJobs(ctx context.Context, selected *controllerProbe) <-chan *mdk.ListPSUFirmwareUpdatesResponse {
	result := make(chan *mdk.ListPSUFirmwareUpdatesResponse, 1)
	go func() {
		ctx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		clients := []*Client{selected.client}
		if selected.standby != nil {
			clients = append(clients, selected.standby.client)
		}
		reads := make(chan *mdk.ListPSUFirmwareUpdatesResponse, len(clients))
		for _, client := range clients {
			go func() {
				response, _ := client.psu.ListPSUFirmwareUpdates(ctx, &mdk.ListPSUFirmwareUpdatesRequest{})
				reads <- response
			}()
		}
		out := &mdk.ListPSUFirmwareUpdatesResponse{}
		for range clients {
			response := <-reads
			out.Updates = append(out.Updates, response.GetUpdates()...)
		}
		sort.SliceStable(out.Updates, func(i, j int) bool {
			return out.Updates[i].GetCreatedAt().AsTime().After(out.Updates[j].GetCreatedAt().AsTime())
		})
		result <- out
	}()
	return result
}

type firmwareBoard struct {
	mac   string
	board *mdk.Hashboard
}

func occupiedFirmwareBoards(wire *mdk.GetContainerMapResponse) ([]firmwareBoard, error) {
	var boards []firmwareBoard
	seen := map[string]bool{}
	for _, tank := range wire.GetContainerMap().GetTanks() {
		for _, module := range tank.GetModules() {
			for _, slot := range module.GetSlots() {
				if !slot.GetOccupied() {
					continue
				}
				mac := normalizedBoardMAC(slot.GetHashboard().GetMacAddress())
				if mac == "" || seen[mac] {
					return nil, fmt.Errorf("missing or duplicate firmware board identity")
				}
				seen[mac] = true
				boards = append(boards, firmwareBoard{mac: mac, board: slot.GetHashboard()})
			}
		}
	}
	return boards, nil
}

func locatedInventoryComplete(topology *sdk.ContainerSnapshot) bool {
	if topology == nil || len(topology.Tanks) != sdk.CT1TankCount {
		return false
	}
	for _, tank := range topology.Tanks {
		if !tank.TopologyComplete {
			return false
		}
	}
	return true
}

func boardFirmwareVersion(board *mdk.Hashboard) string {
	return strings.TrimPrefix(strings.TrimSpace(board.GetFirmwareVersion()), "v")
}

func observedHashboardFirmware(wire *mdk.GetContainerMapResponse, topology *sdk.ContainerSnapshot, now time.Time) (version string, mixed bool) {
	if !locatedInventoryComplete(topology) {
		return "", false
	}
	boards, err := occupiedFirmwareBoards(wire)
	if err != nil || len(boards) == 0 {
		return "", false
	}
	for _, b := range boards {
		v := boardFirmwareVersion(b.board)
		if v == "" || !sdk.FreshContainerReading(sourceTime(b.board.GetHealth().GetTimestamp()), now) {
			return "", false
		}
		if version == "" {
			version = v
		} else if version != v {
			mixed = true
		}
	}
	if mixed {
		return "", true
	}
	return version, false
}

// psuFirmwareEvidence is a module's latest PSU update job. Every board of the
// module carries it, like the module's other PSU readings.
type psuFirmwareEvidence struct {
	TaskID    string  `json:"taskId"`
	ServerID  string  `json:"serverId"`
	CapsuleID string  `json:"capsuleId"`
	State     string  `json:"state"`
	Phase     string  `json:"phase,omitempty"`
	Progress  float64 `json:"progressPercent"`
	Recover   bool    `json:"recover,omitempty"`
	// RecoveryRequired means the PSU must be recovered with the same capsule.
	RecoveryRequired bool `json:"recoveryRequired,omitempty"`
	// MiningHeld means the job left the module out of mining.
	MiningHeld bool      `json:"miningHeld,omitempty"`
	Message    string    `json:"message,omitempty"`
	ObservedAt time.Time `json:"observedAt"`
}

func psuFirmwareJobEvidence(job *mdk.PSUFirmwareUpdate, observed time.Time) psuFirmwareEvidence {
	leader := job.GetLeaderStatus()
	out := psuFirmwareEvidence{
		TaskID: job.GetTaskId(), ServerID: job.GetServerId(), CapsuleID: job.GetCapsuleId(), State: job.GetState().String(),
		Recover: job.GetRecover(), RecoveryRequired: psuNeedsRecovery(leader), ObservedAt: observed,
		Message: truncateUTF8(strings.ToValidUTF8(job.GetError(), ""), maxFirmwareFailureBytes),
	}
	if leader != nil {
		out.Phase = leader.GetPhase().String()
	}
	for _, member := range job.GetMembers() {
		out.MiningHeld = out.MiningHeld || member.GetRestoreError() != ""
	}
	part := func(done uint32) float64 {
		if leader.GetTotalSizeBytes() == 0 {
			return 0
		}
		return min(1, float64(done)/float64(leader.GetTotalSizeBytes()))
	}
	// Staging the capsule is quick; programming the PSU is most of a job.
	progress := map[mdk.PSUFirmwareUpdateState]float64{
		mdk.PSUFirmwareUpdateState_PSU_FIRMWARE_UPDATE_STATE_PREPARING:  5,
		mdk.PSUFirmwareUpdateState_PSU_FIRMWARE_UPDATE_STATE_STAGING:    5 + 20*part(leader.GetStagedBytes()),
		mdk.PSUFirmwareUpdateState_PSU_FIRMWARE_UPDATE_STATE_VALIDATING: 30,
		mdk.PSUFirmwareUpdateState_PSU_FIRMWARE_UPDATE_STATE_APPLYING:   35 + 60*part(leader.GetProgrammedBytes()),
		mdk.PSUFirmwareUpdateState_PSU_FIRMWARE_UPDATE_STATE_RESTORING:  97,
		mdk.PSUFirmwareUpdateState_PSU_FIRMWARE_UPDATE_STATE_SUCCEEDED:  100,
	}
	out.Progress = progress[job.GetState()]
	return out
}

func enrichFirmwareBoards(snapshot *sdk.ContainerSnapshot, response *mdk.ListFirmwareUpdatesResponse, observed time.Time) {
	enrichFirmwareEvidence(snapshot, response, nil, observed)
}

// enrichFirmwareEvidence attaches each board's latest hashboard firmware job
// and its module's latest PSU firmware job to the board's metadata.
func enrichFirmwareEvidence(snapshot *sdk.ContainerSnapshot, response *mdk.ListFirmwareUpdatesResponse, psu *mdk.ListPSUFirmwareUpdatesResponse, observed time.Time) {
	modules := map[string]psuFirmwareEvidence{}
	for _, job := range psu.GetUpdates() {
		if _, exists := modules[job.GetModuleId()]; !exists && job.GetModuleId() != "" {
			modules[job.GetModuleId()] = psuFirmwareJobEvidence(job, observed)
		}
	}
	type evidence struct {
		TaskID     string    `json:"taskId"`
		ServerID   string    `json:"serverId"`
		Status     string    `json:"status"`
		Progress   float64   `json:"progressPercent"`
		Reason     string    `json:"reason,omitempty"`
		Message    string    `json:"message,omitempty"`
		ObservedAt time.Time `json:"observedAt"`
	}
	latest := map[string]evidence{}
	for _, job := range response.GetUpdates() {
		for _, target := range job.GetTargets() {
			mac := normalizedBoardMAC(target.GetMacAddress())
			if _, exists := latest[mac]; exists {
				continue
			}
			latest[mac] = evidence{TaskID: job.TaskId, ServerID: job.ServerId, Status: target.Status.String(), Progress: target.ProgressPercent, Reason: target.Reason, Message: target.Message, ObservedAt: observed}
		}
	}
	attach := func(board *sdk.ContainerHashboard) {
		value, ok := latest[board.MACAddress]
		if !ok && len(modules) == 0 {
			return
		}
		var fields map[string]json.RawMessage
		if json.Unmarshal(board.MetadataJSON, &fields) != nil || fields == nil {
			return
		}
		var moduleID string
		_ = json.Unmarshal(fields["moduleId"], &moduleID)
		module, hasModule := modules[strings.TrimSpace(moduleID)]
		if !ok && !hasModule {
			return
		}
		if ok {
			encoded, err := json.Marshal(value)
			if err != nil {
				return
			}
			fields["firmwareUpdate"] = encoded
		}
		if hasModule {
			encoded, err := json.Marshal(module)
			if err != nil {
				return
			}
			fields["psuFirmwareUpdate"] = encoded
		}
		if data, err := json.Marshal(fields); err == nil && sdk.ValidateHashboardJSON(data) == nil {
			board.MetadataJSON = data
		}
	}
	for ti := range snapshot.Tanks {
		for mi := range snapshot.Tanks[ti].Modules {
			for bi := range snapshot.Tanks[ti].Modules[mi].Hashboards {
				attach(&snapshot.Tanks[ti].Modules[mi].Hashboards[bi])
			}
		}
	}
	for i := range snapshot.UnlocatedHashboards {
		attach(&snapshot.UnlocatedHashboards[i])
	}
}
