package main

import (
	"net/http"
	"os"
	"time"

	"google.golang.org/grpc/codes"
)

// Module voltage models the controller's durable per-module setting. A fixed
// target is validated against every member board's firmware limits, saved, and
// then sent to each board. A board that does not take it is reported as not
// applied and retried later; the saved setting stays. Clearing the target
// restores automatic voltage.
//
// Firmware limits are known only for boards that have reported since start, so
// a module with a disconnected board has no supported range and refuses a fixed
// target. A board in an error state does not acknowledge the command, which
// gives the demo topology modules that apply on some boards only.

const (
	boardMinimumMillivolts = 26000
	boardMaximumMillivolts = 30000
	defaultVoltageActor    = "fake-ct1"

	readinessReady             = "MODULE_VOLTAGE_READINESS_READY"
	readinessLimitsUnavailable = "MODULE_VOLTAGE_READINESS_LIMITS_UNAVAILABLE"
	readinessNoMembers         = "MODULE_VOLTAGE_READINESS_NO_MEMBERS"

	limitsUnavailableMessage = "firmware voltage limits are unavailable for one or more hashboards"
	notPrimaryMessage        = "this node is not the container control primary"
)

// moduleVoltageScenario selects a failure for module voltage writes:
// "write-refused" refuses every write before saving it, and "write-unknown"
// saves the write and then fails the reply, so only a re-read shows the result.
func moduleVoltageScenario() string { return os.Getenv("CT1_MODULE_VOLTAGE_SCENARIO") }

type setModuleVoltageRequest struct {
	Millivolts int64str `json:"millivolts"`
}

type moduleVoltageResponse struct {
	Setting       moduleVoltageSetting `json:"setting"`
	BoardsUpdated int                  `json:"boardsUpdated"`
	Results       []boardResult        `json:"results"`
}

type listModuleVoltageSettingsResponse struct {
	Settings []moduleVoltageSetting `json:"settings"`
}

// moduleVoltageSetting mirrors mdk.v2.ModuleVoltageSetting. An absent
// fixedMillivolts means automatic voltage, and the supported range is present
// only when every member board reported limits.
type moduleVoltageSetting struct {
	ModuleID                   string     `json:"moduleId"`
	FixedMillivolts            *int64str  `json:"fixedMillivolts,omitempty"`
	SupportedMinimumMillivolts int64str   `json:"supportedMinimumMillivolts,omitempty"`
	SupportedMaximumMillivolts int64str   `json:"supportedMaximumMillivolts,omitempty"`
	BoardCount                 int        `json:"boardCount"`
	Readiness                  string     `json:"readiness"`
	ReadinessMessage           string     `json:"readinessMessage,omitempty"`
	UpdatedBy                  string     `json:"updatedBy,omitempty"`
	UpdatedAt                  *time.Time `json:"updatedAt,omitempty"`
}

func voltageActor(header string) string {
	if header != "" {
		return header
	}
	return defaultVoltageActor
}

func moduleBoards(m *Module) []*Hashboard {
	boards := make([]*Hashboard, 0, len(m.Slots))
	for _, slot := range m.Slots {
		if slot.Board != nil {
			boards = append(boards, slot.Board)
		}
	}
	return boards
}

// moduleVoltageLimitsKnown reports whether every member board's firmware has
// reported its voltage limits.
func moduleVoltageLimitsKnown(boards []*Hashboard) bool {
	for _, board := range boards {
		if board.State == miningStateDisconnected {
			return false
		}
	}
	return true
}

func (s *ContainerState) moduleVoltageSetting(m *Module) moduleVoltageSetting {
	boards := moduleBoards(m)
	setting := moduleVoltageSetting{ModuleID: m.ModuleID, BoardCount: len(boards), Readiness: readinessReady}
	switch {
	case len(boards) == 0:
		setting.Readiness = readinessNoMembers
		setting.ReadinessMessage = "module has no hashboards"
	case !moduleVoltageLimitsKnown(boards):
		setting.Readiness = readinessLimitsUnavailable
		setting.ReadinessMessage = limitsUnavailableMessage
	default:
		setting.SupportedMinimumMillivolts = boardMinimumMillivolts
		setting.SupportedMaximumMillivolts = boardMaximumMillivolts
	}
	if m.FixedVoltageSet {
		fixed := int64str(m.FixedMillivolts)
		setting.FixedMillivolts = &fixed
	}
	if !m.VoltageUpdatedAt.IsZero() {
		updatedAt := m.VoltageUpdatedAt
		setting.UpdatedBy = m.VoltageUpdatedBy
		setting.UpdatedAt = &updatedAt
	}
	return setting
}

func (s *ContainerState) listModuleVoltageSettings() listModuleVoltageSettingsResponse {
	settings := make([]moduleVoltageSetting, 0, len(s.Modules))
	for _, m := range s.Modules {
		settings = append(settings, s.moduleVoltageSetting(m))
	}
	return listModuleVoltageSettingsResponse{Settings: settings}
}

// setModuleFixedVoltage saves a module's fixed target, or restores automatic
// voltage when pin is false, then sends it to each member board.
func (s *ContainerState) setModuleFixedVoltage(moduleID string, millivolts int64, pin bool, actor string) (moduleVoltageResponse, error) {
	if moduleVoltageScenario() == "write-refused" {
		return moduleVoltageResponse{}, &apiError{http.StatusBadRequest, codes.FailedPrecondition, notPrimaryMessage}
	}
	module := s.modulesByID[moduleID]
	if module == nil {
		return moduleVoltageResponse{}, notFoundf("unknown module %q", moduleID)
	}
	boards := moduleBoards(module)
	if pin {
		if millivolts <= 0 {
			return moduleVoltageResponse{}, invalidArgument("millivolts must be greater than zero")
		}
		if len(boards) == 0 {
			return moduleVoltageResponse{}, &apiError{http.StatusBadRequest, codes.FailedPrecondition, "module has no hashboards"}
		}
		if !moduleVoltageLimitsKnown(boards) {
			return moduleVoltageResponse{}, &apiError{http.StatusBadRequest, codes.FailedPrecondition, limitsUnavailableMessage}
		}
		if millivolts < boardMinimumMillivolts || millivolts > boardMaximumMillivolts {
			return moduleVoltageResponse{}, invalidArgument("fixed voltage is outside the hashboards' supported range of 26000-30000 mV")
		}
	}
	if !pin {
		millivolts = 0
	}
	module.FixedMillivolts, module.FixedVoltageSet = millivolts, pin
	module.VoltageUpdatedBy = voltageActor(actor)
	module.VoltageUpdatedAt = s.now().UTC().Truncate(time.Second)

	if moduleVoltageScenario() == "write-unknown" {
		return moduleVoltageResponse{}, &apiError{http.StatusInternalServerError, codes.Internal, "lost the reply after saving the module voltage"}
	}

	results := make([]boardResult, 0, len(boards))
	updated := 0
	for _, board := range boards {
		switch board.State {
		case miningStateDisconnected:
			results = append(results, boardResult{MACAddress: board.MAC, Message: "hashboard is not connected; the controller will retry"})
		case miningStateError:
			results = append(results, boardResult{MACAddress: board.MAC, Message: "hashboard did not acknowledge the voltage command; the controller will retry"})
		default:
			results = append(results, boardResult{MACAddress: board.MAC, Applied: true})
			updated++
		}
	}
	return moduleVoltageResponse{Setting: s.moduleVoltageSetting(module), BoardsUpdated: updated, Results: results}, nil
}
