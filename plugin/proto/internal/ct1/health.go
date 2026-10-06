// Package ct1 collects Frigate MDK v2 container observations. It never creates
// Fleet miners for the modules behind a controller.
package ct1

import (
	"fmt"
	"sort"
	"time"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

// Frigate health is sampled independently of HTTP responses. A successful poll
// does not make an old board sample fresh. Allow brief replication/poll delays.
const healthMaxAge = 2 * time.Minute
const clockSkewAllowance = 5 * time.Second
const boardsPerModule = 3

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
	MACAddress  string  `json:"macAddress"`
	ModuleID    string  `json:"moduleId"`
	MiningState string  `json:"miningState"`
	Isolated    bool    `json:"isolated"`
	Health      *Health `json:"health"`
}
type Slot struct {
	Occupied  bool   `json:"occupied"`
	Hashboard *Board `json:"hashboard"`
}
type Module struct {
	ID    int32  `json:"id"`
	Slots []Slot `json:"slots"`
}
type Tank struct {
	ID      int32    `json:"id"`
	Modules []Module `json:"modules"`
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
	if b.Isolated {
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
	result := &sdk.ContainerSnapshot{ObservedAt: now, TopologyComplete: true, Tanks: []sdk.ContainerTank{}}
	tankIDs := map[int32]bool{}
	boardIDs := map[string]bool{}
	issuesBudget := issueBudget{remaining: maxContainerIssueBytes}
	for _, tank := range wire.ContainerMap.Tanks {
		if tank.ID <= 0 || tankIDs[tank.ID] {
			return nil, fmt.Errorf("invalid or duplicate tank %d", tank.ID)
		}
		tankIDs[tank.ID] = true
		out := sdk.ContainerTank{Position: tank.ID, Modules: []sdk.ContainerModule{}}
		moduleIDs := map[int32]bool{}
		for _, m := range tank.Modules {
			if m.ID <= 0 || moduleIDs[m.ID] {
				return nil, fmt.Errorf("invalid or duplicate module %d in tank %d", m.ID, tank.ID)
			}
			moduleIDs[m.ID] = true
			boards := []Board{}
			reportedID := ""
			identityConflict := false
			for _, slot := range m.Slots {
				if !slot.Occupied || slot.Hashboard == nil {
					continue
				}
				b := *slot.Hashboard
				if b.MACAddress == "" || boardIDs[b.MACAddress] {
					return nil, fmt.Errorf("missing or duplicate hashboard identity")
				}
				boardIDs[b.MACAddress] = true
				if b.ModuleID != "" {
					if reportedID != "" && reportedID != b.ModuleID {
						identityConflict = true
					}
					reportedID = b.ModuleID
				}
				boards = append(boards, b)
			}
			state := moduleState(boards, now)
			if identityConflict {
				reportedID = ""
				state = sdk.ModuleUnknown
			}
			out.Modules = append(out.Modules, sdk.ContainerModule{Position: m.ID, ReportedID: reportedID, State: state, ObservedAt: moduleObservedAt(boards, state, now), Metrics: moduleMetrics(boards, now), Issues: moduleIssuesWithBudget(boards, now, identityConflict, &issuesBudget)})
		}
		sort.Slice(out.Modules, func(i, j int) bool { return out.Modules[i].Position < out.Modules[j].Position })
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
