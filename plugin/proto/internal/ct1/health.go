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
	sdkerrors "github.com/block/proto-fleet/server/sdk/v1/errors"
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

// Consolidate observations into the same status enum used by ordinary miners.
// Warnings remain errors with severity; they do not stop an active board.
func boardState(b Board, now time.Time) sdk.MinerStatus {
	state, _ := boardStateObservation(b, now)
	return state
}

// Keep classification and the evidence that establishes it together. A board
// fault in the health sample is newer than a thermal fault aged within it.
func boardStateObservation(b Board, now time.Time) (sdk.MinerStatus, time.Time) {
	switch b.MiningState {
	case miningStateError:
		return sdk.MinerStatusError, now
	case "MINING_STATE_DISCONNECTED":
		return sdk.MinerStatusOffline, now
	case "MINING_STATE_FLASHING":
		return sdk.MinerStatusUpdating, now
	}
	state := miningStatus(b.MiningState)
	observed := now
	if state == sdk.MinerStatusUnknown {
		observed = time.Time{}
	}
	fresh := b.Health != nil && !b.Health.Timestamp.IsZero() && now.Sub(b.Health.Timestamp) < healthMaxAge && b.Health.Timestamp.Sub(now) <= clockSkewAllowance
	if fresh {
		healthAt := b.Health.Timestamp
		if healthAt.After(now) {
			healthAt = now
		}
		for _, issue := range b.Health.Errors {
			if _, severity := canonicalIssue(issue.Cause, false); severity == sdkerrors.SeverityCritical {
				return sdk.MinerStatusError, healthAt
			}
		}
		if b.MiningState == miningStateMining && ((b.Health.HashrailOK != nil && !*b.Health.HashrailOK) || (b.Health.RefclksOK != nil && !*b.Health.RefclksOK)) {
			return sdk.MinerStatusError, healthAt
		}
		if p := b.Health.PSUThermal; p != nil && p.ThermalStateValid && p.OverTemperatureFault {
			at := b.Health.Timestamp
			if p.ThermalStateAgeMs != nil {
				if !nonnegative(p.ThermalStateAgeMs) || *p.ThermalStateAgeMs >= float64(healthMaxAge.Milliseconds()) {
					return state, observed
				}
				at = at.Add(-time.Duration(*p.ThermalStateAgeMs * float64(time.Millisecond)))
			}
			if now.Sub(at) < healthMaxAge {
				if at.After(now) {
					at = now
				}
				return sdk.MinerStatusError, at
			}
		}
	}
	return state, observed
}

func miningStatus(state string) sdk.MinerStatus {
	switch state {
	case miningStateMining:
		return sdk.MinerStatusActive
	case miningStateIdle, "MINING_STATE_READY", "MINING_STATE_IDENTIFYING", "MINING_STATE_CONFIGURING", "MINING_STATE_INIT", "MINING_STATE_STARTUP":
		return sdk.MinerStatusInactive
	default:
		return sdk.MinerStatusUnknown
	}
}

func moduleState(boards []Board, now time.Time) sdk.MinerStatus {
	counts := map[sdk.MinerStatus]int{}
	for _, board := range boards {
		counts[boardState(board, now)]++
	}
	if counts[sdk.MinerStatusError] > 0 {
		return sdk.MinerStatusError
	}
	if counts[sdk.MinerStatusUpdating] > 0 {
		return sdk.MinerStatusUpdating
	}
	if len(boards) != boardsPerModule || counts[sdk.MinerStatusUnknown] > 0 {
		return sdk.MinerStatusUnknown
	}
	// Partial production remains active, with board observations explaining it.
	if counts[sdk.MinerStatusActive] > 0 {
		return sdk.MinerStatusActive
	}
	if counts[sdk.MinerStatusInactive] > 0 {
		return sdk.MinerStatusInactive
	}
	return sdk.MinerStatusOffline
}

// snapshot rejects ambiguous topology rather than changing the card's denominator
// through duplicate modules or boards. Empty mapped slots are not live readings.
func snapshot(wire MapResponse, now time.Time) (*sdk.ContainerSnapshot, error) {
	return snapshotEvidence(wire, now, nil)
}

// snapshotEvidence also hands each accepted module's issue evidence to error
// polling, so both views classify the same validated boards. A nil collector
// records nothing.
func snapshotEvidence(wire MapResponse, now time.Time, collect *errorCollector) (*sdk.ContainerSnapshot, error) {
	if wire.ContainerMap == nil {
		return nil, fmt.Errorf("container map missing")
	}
	result := &sdk.ContainerSnapshot{ObservedAt: now, TopologyComplete: len(wire.ContainerMap.Tanks) == sdk.CT1TankCount, Tanks: []sdk.ContainerTank{}}
	tankIDs := map[int32]bool{}
	identities := collectTopologyIdentities(wire)
	issuesBudget := issueBudget{remaining: maxContainerIssueBytes}
	for _, tank := range wire.ContainerMap.Tanks {
		if tank.ID < 0 || tankIDs[tank.ID] {
			return nil, fmt.Errorf("invalid or duplicate tank %d", tank.ID)
		}
		tankIDs[tank.ID] = true
		if tank.ID == 0 {
			// In switch_ports mode Frigate groups unlocated inventory under
			// tank=0/module=0. This is not a physical three-slot module and may
			// contain arbitrarily many boards. Keep its identity claims, but
			// never manufacture placements or claim complete inventory coverage.
			var unlocatedBoards uint32
			moduleSeen := false
			for _, m := range tank.Modules {
				if m.ID != 0 || moduleSeen {
					return nil, fmt.Errorf("invalid or duplicate module %d in tank %d", m.ID, tank.ID)
				}
				moduleSeen = true
				for _, slot := range m.Slots {
					if slot.Hashboard != nil || slot.Occupied {
						unlocatedBoards++
						observation := hashboardObservation(slot, 0, now)
						for _, key := range boardIdentityKeys(observation.MACAddress, observation.Serial) {
							if identities.boards[key] > 1 {
								observation = unavailableBoard(0, sdk.HashboardIdentityConflict, slot.Hashboard)
								break
							}
						}
						collect.unlocated(slot.Hashboard, observation, now)
						result.UnlocatedHashboards = append(result.UnlocatedHashboards, observation)
					}
				}
			}
			result.TopologyComplete = false
			slog.Warn("CT1 inventory contains unlocated hashboards", "hashboards", unlocatedBoards)
			continue
		}
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
				state = sdk.MinerStatusUnknown
				reportedID = ""
				out.TopologyComplete = false
				// PSU identity does not determine board identity. Keep independently
				// validated board readings, but withhold this module's shared metrics.
				boards = nil
			}
			issues, sources := moduleIssueEvidence(boards, now, identityConflict, &issuesBudget)
			collect.module(tank.ID, m.ID, hashboards, issues, sources)
			out.Modules = append(out.Modules, sdk.ContainerModule{Position: m.ID, ReportedID: reportedID, IdentityConfirmed: reportedID != "" && !identityUnavailable, State: state, ObservedAt: moduleObservedAt(boards, state, now), Metrics: moduleMetrics(boards, now), Hashboards: hashboards, Issues: issues})
		}
		sort.Slice(out.Modules, func(i, j int) bool { return out.Modules[i].Position < out.Modules[j].Position })
		result.TopologyComplete = result.TopologyComplete && out.TopologyComplete
		result.Tanks = append(result.Tanks, out)
	}
	sort.Slice(result.Tanks, func(i, j int) bool { return result.Tanks[i].Position < result.Tanks[j].Position })
	return result, nil
}

// Any current error witness establishes the module error. Keep the newest
// sufficient evidence so an older independent fault cannot expire it early.
func moduleObservedAt(boards []Board, state sdk.MinerStatus, now time.Time) time.Time {
	if state == sdk.MinerStatusUnknown {
		return time.Time{}
	}
	if state != sdk.MinerStatusError {
		return now
	}
	var observed time.Time
	for _, b := range boards {
		boardState, at := boardStateObservation(b, now)
		if boardState == sdk.MinerStatusError && at.After(observed) {
			observed = at
		}
	}
	return observed
}
