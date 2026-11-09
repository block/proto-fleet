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

// Read each verified node locally: Frigate's merged cluster list deliberately
// clears server_id, which would lose historical ownership after a board moves.
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

type firmwareBoard struct {
	mac   string
	board *mdk.Hashboard
}

// occupiedFirmwareBoards is the board set both firmware targeting and
// convergence use, located or not. Missing or duplicate identities fail.
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

// locatedInventoryComplete reports whether every CT1 tank and module position
// is reported. Unlocated switch_ports inventory never proves coverage.
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

// observedHashboardFirmware returns the uniform board version, or reports
// mixed versions, only from complete located inventory with fresh health.
// Anything less leaves the version unknown.
func observedHashboardFirmware(wire *mdk.GetContainerMapResponse, topology *sdk.ContainerSnapshot, now time.Time) (version string, mixed bool) {
	boards, err := occupiedFirmwareBoards(wire)
	if err != nil || len(boards) == 0 || !locatedInventoryComplete(topology) {
		return "", false
	}
	for _, b := range boards {
		observed := sourceTime(b.board.GetHealth().GetTimestamp())
		v := boardFirmwareVersion(b.board)
		if v == "" || observed.IsZero() || now.Sub(observed) > healthMaxAge || observed.After(now.Add(clockSkewAllowance)) {
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

// Attach the latest per-board job evidence to the existing diagnostic metadata
// so it follows the same inventory identity and authorization as the board.
func enrichFirmwareBoards(snapshot *sdk.ContainerSnapshot, response *mdk.ListFirmwareUpdatesResponse, observed time.Time) {
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
	// MDK lists newest jobs first. Retain the first observation per identity.
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
		if !ok {
			return
		}
		var fields map[string]json.RawMessage
		if json.Unmarshal(board.MetadataJSON, &fields) != nil || fields == nil {
			return
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			return
		}
		fields["firmwareUpdate"] = encoded
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
