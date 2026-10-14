// Package ct1 collects Frigate MDK v2 container observations. It never creates
// Fleet miners for the modules behind a controller.
package ct1

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

// Frigate health is sampled independently of RPC responses. A successful poll
// does not make an old board sample fresh. Allow brief replication/poll delays.
const healthMaxAge = sdk.ContainerReadingMaxAge
const clockSkewAllowance = sdk.ContainerClockSkewAllowance
const boardsPerModule = sdk.CT1HashboardsPerModule

const (
	miningStateMining = "MINING_STATE_MINING"
	miningStateError  = "MINING_STATE_ERROR"
	miningStateIdle   = "MINING_STATE_IDLE"
)

type Issue struct {
	Cause string `json:"cause"`
}
type PSUThermal struct {
	ThermalStateValid      bool     `json:"thermalStateValid"`
	ThermalStateAgeMs      *float64 `json:"thermalStateAgeMs"`
	OverTemperatureFault   bool     `json:"overTemperatureFault"`
	OverTemperatureWarning bool     `json:"overTemperatureWarning"`
}
type Health struct {
	Power                   *float64       `json:"power"`
	Hashrate                *float64       `json:"hashrate"`
	AverageAsicTemperatureC *float64       `json:"averageAsicTemperatureC"`
	PSUElectrical           *PSUElectrical `json:"psuElectrical"`
	HashrailOK              *bool          `json:"hashrailOk"`
	RefclksOK               *bool          `json:"refclksOk"`
	Timestamp               time.Time      `json:"timestamp"`
	Errors                  []Issue        `json:"errors"`
	Warnings                []Issue        `json:"warnings"`
	LastShutdownError       *Issue         `json:"lastShutdownError"`
	PSUThermal              *PSUThermal    `json:"psuThermal"`
}
type Board struct {
	Serial      string `json:"serial,omitempty"`
	Source      string `json:"source,omitempty"`
	raw         map[string]json.RawMessage
	MACAddress  string  `json:"macAddress"`
	ModuleID    string  `json:"moduleId"`
	MiningState string  `json:"miningState"`
	Isolated    bool    `json:"isolated"`
	Health      *Health `json:"health"`
}
type Slot struct {
	Feed       *sdk.ContainerFeedEvidence `json:"-"`
	invalid    bool
	unreported bool
	Occupied   bool   `json:"occupied"`
	Hashboard  *Board `json:"hashboard"`
}
type Module struct {
	ID    int32  `json:"id"`
	Slots []Slot `json:"slots"`
}
type Tank struct {
	ExpectedModules int32    `json:"-"`
	ID              int32    `json:"id"`
	Modules         []Module `json:"modules"`
}
type ContainerMap struct {
	Tanks []Tank `json:"tanks"`
}
type MapResponse struct {
	ContainerMap *ContainerMap `json:"containerMap"`
}

func boardState(b Board, now time.Time) sdk.ModuleState {
	// Mining state is a current controller observation even when health is absent.
	if b.MiningState == miningStateError {
		return sdk.ModuleAttention
	}
	fresh := b.Health != nil && !b.Health.Timestamp.IsZero() && now.Sub(b.Health.Timestamp) < healthMaxAge && b.Health.Timestamp.Sub(now) <= clockSkewAllowance
	if fresh {
		if b.MiningState == miningStateMining && ((b.Health.HashrailOK != nil && !*b.Health.HashrailOK) || (b.Health.RefclksOK != nil && !*b.Health.RefclksOK)) {
			return sdk.ModuleAttention
		}
		if len(b.Health.Errors) > 0 || len(b.Health.Warnings) > 0 {
			return sdk.ModuleAttention
		}
		p := b.Health.PSUThermal
		if p != nil && p.ThermalStateValid && (p.ThermalStateAgeMs == nil || (*p.ThermalStateAgeMs >= 0 && *p.ThermalStateAgeMs <= float64(healthMaxAge.Milliseconds()))) && (p.OverTemperatureFault || p.OverTemperatureWarning) {
			return sdk.ModuleAttention
		}
	}
	if b.MiningState == "MINING_STATE_DISCONNECTED" {
		return sdk.ModuleOffline
	}
	if !fresh {
		return sdk.ModuleUnknown
	}
	if b.Isolated && b.Source != "HASHBOARD_SOURCE_REMOTE" {
		return sdk.ModuleIsolated
	}
	switch b.MiningState {
	case miningStateMining:
		return sdk.ModuleHashing
	case miningStateIdle, "MINING_STATE_READY":
		return sdk.ModuleSleeping
	default:
		return sdk.ModuleUnknown
	}
}

func moduleState(boards []Board, now time.Time) sdk.ModuleState {
	counts := map[sdk.ModuleState]int{}
	for _, b := range boards {
		counts[boardState(b, now)]++
	}
	if counts[sdk.ModuleAttention] > 0 {
		return sdk.ModuleAttention
	}
	if counts[sdk.ModuleOffline] > 0 {
		return sdk.ModuleOffline
	}
	if len(boards) != boardsPerModule || counts[sdk.ModuleUnknown] > 0 {
		return sdk.ModuleUnknown
	}
	for _, state := range []sdk.ModuleState{sdk.ModuleHashing, sdk.ModuleSleeping, sdk.ModuleIsolated} {
		if counts[state] == len(boards) {
			return state
		}
	}
	return sdk.ModuleAttention
}

// snapshot rejects ambiguous topology rather than changing the card's denominator
// through duplicate modules or boards. Empty mapped slots are not live readings.
func snapshot(wire MapResponse, now time.Time) (*sdk.ContainerSnapshot, error) {
	if wire.ContainerMap == nil {
		return nil, fmt.Errorf("container map missing")
	}
	result := &sdk.ContainerSnapshot{ObservedAt: now, TopologyComplete: len(wire.ContainerMap.Tanks) == sdk.CT1TankCount, Tanks: []sdk.ContainerTank{}}
	tankIDs := map[int32]bool{}
	identities := collectTopologyIdentities(wire)
	issuesBudget := issueBudget{remaining: maxContainerIssueBytes}
	for _, tank := range wire.ContainerMap.Tanks {
		if tank.ID <= 0 || tankIDs[tank.ID] {
			return nil, fmt.Errorf("invalid or duplicate tank %d", tank.ID)
		}
		tankIDs[tank.ID] = true
		expected := tank.ExpectedModules
		if expected == 0 {
			expected = sdk.CT1ModulesPerTank
		} // CT1 Trident96 layout; absent modules remain unknown.
		out := sdk.ContainerTank{Position: tank.ID, Modules: []sdk.ContainerModule{}, TopologyComplete: len(tank.Modules) == int(expected) && tank.ID <= sdk.CT1TankCount}
		moduleIDs := map[int32]bool{}
		for _, m := range tank.Modules {
			if m.ID <= 0 || moduleIDs[m.ID] {
				return nil, fmt.Errorf("invalid or duplicate module %d in tank %d", m.ID, tank.ID)
			}
			moduleIDs[m.ID] = true
			if m.ID > expected {
				out.TopologyComplete = false
			}
			boards := []Board{}
			reportedID := ""
			identityUnavailable := false
			hashboards := hashboardObservations(m.Slots, now)
			for index := range hashboards {
				observation := &hashboards[index]
				if index < len(m.Slots) {
					observation.Feed = m.Slots[index].Feed
				}
				identityConflict := identities.selectors[observation.MACAddress] > 1
				for _, key := range boardIdentityKeys(observation.MACAddress, observation.Serial) {
					if identities.boards[key] > 1 {
						identityConflict = true
						break
					}
				}
				if identityConflict {
					*observation = unavailableBoard(observation.Position, sdk.HashboardIdentityConflict, m.Slots[index].Hashboard)
					observation.Feed = m.Slots[index].Feed
				}
				if observation.UnavailableReason != "" {
					slog.Warn("CT1 hashboard observation unavailable", "tank", tank.ID, "module", m.ID, "position", observation.Position, "reason", observation.UnavailableReason)
				}
				if observation.Occupancy == "unknown" {
					out.TopologyComplete = false
				}
				if observation.Occupancy != "occupied" {
					if observation.Occupancy != "empty" {
						identityUnavailable = true
					}
					continue
				}
				b := *m.Slots[index].Hashboard

				if strings.TrimSpace(b.ModuleID) == "" {
					identityUnavailable = true
				} else {
					reportedID = b.ModuleID
				}
				boards = append(boards, b)
			}
			state := moduleState(boards, now)
			identityConflict := reportedID != "" && !identities.uniqueModule(tank.ID, m.ID, reportedID)
			if identityConflict {
				state = sdk.ModuleUnknown
				reportedID = ""
				out.TopologyComplete = false
				for i := range hashboards {
					board := hashboards[i]
					rejected := sdk.UnavailableHashboard(board.Position, sdk.HashboardIdentityConflict)
					rejected.Feed, rejected.IdentityClaims = board.Feed, board.IdentityClaims
					// Rejected placements still veto another slot claiming a known alias.
					if board.MACAddress != "" || board.Serial != "" {
						rejected.IdentityClaims = append(rejected.IdentityClaims, sdk.HashboardIdentityClaim{MACAddress: board.MACAddress, Serial: board.Serial})
					}
					hashboards[i] = rejected
				}
				boards = nil
			}
			out.Modules = append(out.Modules, sdk.ContainerModule{Position: m.ID, ReportedID: reportedID, IdentityConfirmed: reportedID != "" && !identityUnavailable, State: state, ObservedAt: moduleObservedAt(boards, state, now), Metrics: moduleMetrics(boards, now), Hashboards: hashboards, Issues: moduleIssuesWithBudget(boards, now, identityConflict, &issuesBudget)})
		}
		sort.Slice(out.Modules, func(i, j int) bool { return out.Modules[i].Position < out.Modules[j].Position })
		result.TopologyComplete = result.TopologyComplete && out.TopologyComplete
		result.Tanks = append(result.Tanks, out)
	}
	sort.Slice(result.Tanks, func(i, j int) bool { return result.Tanks[i].Position < result.Tanks[j].Position })
	return result, nil
}

// Preserve the age of the evidence supporting this classification. A fault
// needs one current witness; hashing/sleeping/isolation require every board.
func moduleObservedAt(boards []Board, state sdk.ModuleState, now time.Time) time.Time {
	if state == sdk.ModuleUnknown {
		return time.Time{}
	}
	oldest := now
	var witness time.Time
	for _, board := range boards {
		stateOfBoard := boardState(board, now)
		observed := now
		if board.MiningState != miningStateError && stateOfBoard != sdk.ModuleOffline {
			if board.Health == nil {
				continue
			}
			observed = board.Health.Timestamp
			if p := board.Health.PSUThermal; p != nil && p.ThermalStateValid && p.ThermalStateAgeMs != nil && *p.ThermalStateAgeMs >= 0 && *p.ThermalStateAgeMs <= float64(healthMaxAge.Milliseconds()) && (p.OverTemperatureFault || p.OverTemperatureWarning) {
				thermalObserved := now.Add(-time.Duration(*p.ThermalStateAgeMs * float64(time.Millisecond)))
				if thermalObserved.Before(observed) {
					observed = thermalObserved
				}
			}
		}
		if observed.After(now) {
			observed = now
		}
		if observed.Before(oldest) {
			oldest = observed
		}
		if stateOfBoard == state && (state == sdk.ModuleAttention || state == sdk.ModuleOffline) && observed.After(witness) {
			witness = observed
		}
	}
	if !witness.IsZero() {
		return witness
	}
	return oldest
}
