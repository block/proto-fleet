package ct1

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	sdkerrors "github.com/block/proto-fleet/server/sdk/v1/errors"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const errorsDevice = "ct1-errors"

func errorsMAC(position uint32) string { return fmt.Sprintf("aa:bb:cc:dd:ee:%02x", position) }

// errorsBoard is a healthy mining board in tank 1, module 1.
func errorsBoard(position uint32, at time.Time, edits ...func(*mdk.Hashboard)) *mdk.Slot {
	board := &mdk.Hashboard{
		MacAddress: errorsMAC(position), ModuleId: "module-1", MiningState: mdk.MiningState_MINING_STATE_MINING,
		Location: &mdk.HashboardLocation{Tank: 1, Module: 1, HashboardPosition: new(position)},
		Health:   &mdk.HashboardHealth{Timestamp: timestamppb.New(at), PsuThermal: &mdk.PsuThermalHealth{ThermalStateValid: true}},
	}
	for _, edit := range edits {
		edit(board)
	}
	return &mdk.Slot{Occupied: true, Hashboard: board}
}

func errorsMap(slots ...*mdk.Slot) *mdk.GetContainerMapResponse {
	return &mdk.GetContainerMapResponse{ContainerMap: &mdk.ContainerMap{Tanks: []*mdk.Tank{{Id: 1, Modules: []*mdk.Module{{Id: 1, Slots: slots}}}}}}
}

func healthErrors(causes ...mdk.HashboardErrorCause) []*mdk.HashboardHealthError {
	out := make([]*mdk.HashboardHealthError, 0, len(causes))
	for _, cause := range causes {
		out = append(out, &mdk.HashboardHealthError{Cause: cause})
	}
	return out
}

func withErrors(causes ...mdk.HashboardErrorCause) func(*mdk.Hashboard) {
	return func(b *mdk.Hashboard) { b.Health.Errors = healthErrors(causes...) }
}

func withWarnings(causes ...mdk.HashboardErrorCause) func(*mdk.Hashboard) {
	return func(b *mdk.Hashboard) { b.Health.Warnings = healthErrors(causes...) }
}

func withPSUFault(b *mdk.Hashboard) { b.Health.PsuThermal.OverTemperatureFault = true }

// withoutHealth is how Frigate reports a board with no sample in two minutes.
func withoutHealth(b *mdk.Hashboard) { b.Health = nil }

func plantHealth(at time.Time, alarms ...*mdk.PlantAlarm) *mdk.GetContainerHealthResponse {
	return &mdk.GetContainerHealthResponse{Timestamp: timestamppb.New(at), Plant: &mdk.PlantHealth{AlarmActive: new(len(alarms) > 0)}, Alarms: alarms}
}

func pollErrors(t *testing.T, state *errorState, at time.Time, wire *mdk.GetContainerMapResponse, plant *mdk.GetContainerHealthResponse, plantErr error) sdk.DeviceErrors {
	t.Helper()
	collector := newErrorCollector(at)
	_, err := snapshotEvidence(mapResponse(wire), at, collector)
	require.NoError(t, err)
	collector.plant(plant, plantErr, at)
	state.observe(collector.result())
	return state.report(errorsDevice)
}

func requireError(t *testing.T, report sdk.DeviceErrors, kind sdkerrors.ComponentType, id string, code sdkerrors.MinerError) sdk.DeviceError {
	t.Helper()
	var found []sdk.DeviceError
	for _, reported := range report.Errors {
		if reported.ComponentType == kind && *reported.ComponentID == id && reported.MinerError == code {
			found = append(found, reported)
		}
	}
	require.Len(t, found, 1, "errors: %+v", report.Errors)
	return found[0]
}

func requireEvidence(t *testing.T, reported sdk.DeviceError, want string) {
	t.Helper()
	require.Equal(t, want, reported.VendorAttributes["ct1_evidence"], reported.Summary)
}

const (
	kindBoard = sdkerrors.ComponentTypeHashBoard
	kindPSU   = sdkerrors.ComponentTypePSU
	kindPlant = sdkerrors.ComponentTypeUnspecified
	modulePSU = "tank:1/module:1"
)

func TestErrorReportBoardPSUAndPlantFaults(t *testing.T) {
	at := time.Now().UTC()
	sampled := at.Add(-5 * time.Second)
	wire := errorsMap(
		errorsBoard(1, sampled, withPSUFault, withErrors(mdk.HashboardErrorCause_HASHBOARD_ERROR_CAUSE_BOARD_OVER_CURRENT)),
		errorsBoard(2, sampled, withPSUFault, withWarnings(mdk.HashboardErrorCause_HASHBOARD_ERROR_CAUSE_ASIC_THERMAL_THROTTLING), func(b *mdk.Hashboard) {
			b.Health.LastShutdownError = &mdk.HashboardHealthError{Cause: mdk.HashboardErrorCause_HASHBOARD_ERROR_CAUSE_BOARD_OVER_VOLTAGE}
		}),
		errorsBoard(3, sampled, withPSUFault, withErrors(mdk.HashboardErrorCause_HASHBOARD_ERROR_CAUSE_BOARD_POWER_SUPPLY)),
	)
	alarms := []*mdk.PlantAlarm{
		{Id: "register-1-bit-2", Name: "Coolant level low", Scope: "plant", Severity: mdk.Severity_SEVERITY_CRITICAL, ComponentId: "tank-a"},
		{Id: "ac-disabled", Name: "Air conditioners disabled", Severity: mdk.Severity_SEVERITY_ERROR},
		{Id: "ambient-high", Name: "Inside ambient temperature high", Severity: mdk.Severity_SEVERITY_WARNING},
		{Id: "plant-change", Name: "Setpoint changed", Severity: mdk.Severity_SEVERITY_INFO},
		{Id: "unrated", Severity: mdk.Severity_SEVERITY_UNSPECIFIED},
	}
	report := pollErrors(t, &errorState{}, at, wire, plantHealth(sampled, alarms...), nil)
	require.Equal(t, errorsDevice, report.DeviceID)
	require.Len(t, report.Errors, 9)

	board := requireError(t, report, kindBoard, "mac:"+errorsMAC(1), sdkerrors.BoardPowerOvercurrent)
	require.Equal(t, sdkerrors.SeverityCritical, board.Severity)
	require.Equal(t, "Tank 1, module 1, hashboard 1 (aa:bb:cc:dd:ee:01): Board over current", board.Summary)
	require.Equal(t, "Board over current", board.CauseSummary)
	require.Equal(t, errorsDevice, board.DeviceID)
	require.True(t, board.FirstSeenAt.Equal(sampled), "first seen is the controller's sample time")
	require.True(t, board.LastSeenAt.Equal(at), "last seen is this collection")
	require.Equal(t, map[string]string{
		"tank": "1", "module": "1", "hashboard_position": "1", "mac_address": errorsMAC(1),
		"vendor_code": "HASHBOARD_ERROR_CAUSE_BOARD_OVER_CURRENT", "ct1_evidence": errorEvidenceCurrent,
	}, board.VendorAttributes)

	throttled := requireError(t, report, kindBoard, "mac:"+errorsMAC(2), sdkerrors.ASICThermalThrottling)
	require.Equal(t, sdkerrors.SeverityMajor, throttled.Severity)

	thermal := requireError(t, report, kindPSU, modulePSU, sdkerrors.PSUOverTemperature)
	require.Equal(t, sdkerrors.SeverityCritical, thermal.Severity)
	require.Equal(t, "Tank 1, module 1 PSU: PSU over temperature", thermal.Summary)
	require.Equal(t, "PSU_OVER_TEMPERATURE", thermal.VendorAttributes["vendor_code"])
	supply := requireError(t, report, kindPSU, modulePSU, sdkerrors.PSUFaultGeneric)
	require.Equal(t, sdkerrors.SeverityMajor, supply.Severity)
	require.Equal(t, "Tank 1, module 1 PSU: Board power supply", supply.Summary)

	critical := requireError(t, report, kindPlant, "plant-alarm:register-1-bit-2", sdkerrors.VendorErrorUnmapped)
	require.Equal(t, sdkerrors.SeverityCritical, critical.Severity)
	require.Equal(t, "Plant alarm: Coolant level low (component tank-a)", critical.Summary)
	require.True(t, critical.FirstSeenAt.Equal(sampled))
	require.Equal(t, map[string]string{
		"vendor_code": "register-1-bit-2", "plant_alarm_scope": "plant", "plant_component_id": "tank-a",
		"plant_alarm_severity": "SEVERITY_CRITICAL", "ct1_evidence": errorEvidenceCurrent,
	}, critical.VendorAttributes)
	for id, severity := range map[string]sdkerrors.Severity{
		"ac-disabled": sdkerrors.SeverityMajor, "ambient-high": sdkerrors.SeverityMinor,
		"plant-change": sdkerrors.SeverityInfo, "unrated": sdkerrors.SeverityMinor,
	} {
		require.Equal(t, severity, requireError(t, report, kindPlant, "plant-alarm:"+id, sdkerrors.VendorErrorUnmapped).Severity, id)
	}
	require.Equal(t, "Plant alarm: unrated", requireError(t, report, kindPlant, "plant-alarm:unrated", sdkerrors.VendorErrorUnmapped).Summary)

	require.True(t, slices.IsSortedFunc(report.Errors, func(a, b sdk.DeviceError) int { return int(a.Severity) - int(b.Severity) }), "most severe first")
	for _, reported := range report.Errors {
		require.NotContains(t, reported.VendorAttributes["vendor_code"], "BOARD_OVER_VOLTAGE", "the last shutdown error is history")
	}
}

func TestErrorReportMergesCausesSharingAFleetCode(t *testing.T) {
	at := time.Now().UTC()
	wire := errorsMap(
		errorsBoard(1, at,
			withErrors(mdk.HashboardErrorCause_HASHBOARD_ERROR_CAUSE_ASIC_HOTSPOT_TEMPERATURE_DELTA, mdk.HashboardErrorCause_HASHBOARD_ERROR_CAUSE_ASIC_CORNER_TEMPERATURE_DELTA),
			withWarnings(mdk.HashboardErrorCause_HASHBOARD_ERROR_CAUSE_ASIC_LEADING_EDGE_TEMPERATURE_DELTA)),
		errorsBoard(2, at), errorsBoard(3, at),
	)
	report := pollErrors(t, &errorState{}, at, wire, plantHealth(at), nil)
	require.Len(t, report.Errors, 1, "Fleet keys an error by code and component")
	merged := requireError(t, report, kindBoard, "mac:"+errorsMAC(1), sdkerrors.ASICTemperatureImbalance)
	require.Equal(t, sdkerrors.SeverityMajor, merged.Severity, "the most severe cause decides")
	require.Equal(t, "Tank 1, module 1, hashboard 1 (aa:bb:cc:dd:ee:01): ASIC corner temperature delta; ASIC hotspot temperature delta; ASIC leading edge temperature delta", merged.Summary)
	require.Equal(t, "HASHBOARD_ERROR_CAUSE_ASIC_CORNER_TEMPERATURE_DELTA,HASHBOARD_ERROR_CAUSE_ASIC_HOTSPOT_TEMPERATURE_DELTA,HASHBOARD_ERROR_CAUSE_ASIC_LEADING_EDGE_TEMPERATURE_DELTA", merged.VendorAttributes["vendor_code"])
}

func TestErrorReportBoundsMergedText(t *testing.T) {
	at := time.Now().UTC()
	// Causes newer than the pinned contract arrive as bare numbers.
	var causes []mdk.HashboardErrorCause
	for i := range maxModuleIssues {
		causes = append(causes, mdk.HashboardErrorCause(5000+i)) //nolint:gosec // bounded test index
	}
	report := pollErrors(t, &errorState{}, at, errorsMap(errorsBoard(1, at, withErrors(causes...)), errorsBoard(2, at), errorsBoard(3, at)), plantHealth(at), nil)
	require.Len(t, report.Errors, 1)
	unmapped := requireError(t, report, kindBoard, "mac:"+errorsMAC(1), sdkerrors.VendorErrorUnmapped)
	code := unmapped.VendorAttributes["vendor_code"]
	require.LessOrEqual(t, len(code), maxVendorCodeBytes)
	require.True(t, strings.HasPrefix(code, "5000,5001,"), code)
	require.Contains(t, code, " more)", "omitted causes are counted, not dropped silently")
	require.LessOrEqual(t, len(unmapped.Summary), 4096)
}

func TestErrorReportHealthyObservationClearsFaults(t *testing.T) {
	at := time.Now().UTC()
	state := &errorState{}
	faulted := errorsMap(errorsBoard(1, at, withPSUFault, withErrors(mdk.HashboardErrorCause_HASHBOARD_ERROR_CAUSE_BOARD_OVER_CURRENT)), errorsBoard(2, at, withPSUFault), errorsBoard(3, at, withPSUFault))
	alarm := &mdk.PlantAlarm{Id: "register-1-bit-2", Severity: mdk.Severity_SEVERITY_CRITICAL}
	require.Len(t, pollErrors(t, state, at, faulted, plantHealth(at, alarm), nil).Errors, 3)

	at = at.Add(15 * time.Second)
	report := pollErrors(t, state, at, errorsMap(errorsBoard(1, at), errorsBoard(2, at), errorsBoard(3, at)), plantHealth(at), nil)
	require.Equal(t, errorsDevice, report.DeviceID)
	require.NotNil(t, report.Errors)
	require.Empty(t, report.Errors, "fresh readings without the faults withdraw them")
}

func TestErrorReportColdStartInventsNothing(t *testing.T) {
	at := time.Now().UTC()
	wire := errorsMap(errorsBoard(1, at, withoutHealth), errorsBoard(2, at.Add(-time.Hour), withErrors(mdk.HashboardErrorCause_HASHBOARD_ERROR_CAUSE_BOARD_OVER_CURRENT)), errorsBoard(3, at))
	report := pollErrors(t, &errorState{}, at, wire, nil, status.Error(codes.Unavailable, "plant unavailable"))
	require.Empty(t, report.Errors, "silence and stale samples are not current faults")
}

func TestErrorReportRetainsFaultsOfSilentSources(t *testing.T) {
	first := time.Now().UTC().Truncate(time.Second)
	state := &errorState{}
	overCurrent, overTemperature := mdk.HashboardErrorCause_HASHBOARD_ERROR_CAUSE_BOARD_OVER_CURRENT, mdk.HashboardErrorCause_HASHBOARD_ERROR_CAUSE_BOARD_OVER_TEMPERATURE
	report := pollErrors(t, state, first, errorsMap(errorsBoard(1, first, withErrors(overCurrent)), errorsBoard(2, first, withErrors(overTemperature)), errorsBoard(3, first)), plantHealth(first), nil)
	require.Len(t, report.Errors, 2)

	// Board 1 stops reporting; board 2 reports that its fault cleared.
	at := first.Add(30 * time.Second)
	report = pollErrors(t, state, at, errorsMap(errorsBoard(1, at, withoutHealth), errorsBoard(2, at), errorsBoard(3, at)), plantHealth(at), nil)
	require.Len(t, report.Errors, 1, "only the silent board keeps its fault")
	retained := requireError(t, report, kindBoard, "mac:"+errorsMAC(1), sdkerrors.BoardPowerOvercurrent)
	requireEvidence(t, retained, errorEvidenceRetained)
	confirmed := first.Format(time.RFC3339)
	require.Equal(t, confirmed, retained.VendorAttributes["ct1_last_confirmed_at"])
	require.Equal(t, "Tank 1, module 1, hashboard 1 (aa:bb:cc:dd:ee:01): Board over current (not confirmed or cleared since "+confirmed+")", retained.Summary)
	require.Equal(t, sdkerrors.SeverityCritical, retained.Severity)
	require.True(t, retained.FirstSeenAt.Equal(first))
	require.True(t, retained.LastSeenAt.IsZero(), "a carried fault claims no new observation")
	crossed := sdkerrors.DeviceErrorFromProto(retained.ToProto())
	require.True(t, crossed.LastSeenAt.IsZero(), "it stays unset across the plugin RPC, so Fleet stamps its own poll time")
	require.Equal(t, retained.VendorAttributes, crossed.VendorAttributes)

	// A sample older than two minutes neither clears the fault nor adds its own.
	at = at.Add(30 * time.Second)
	stale := errorsBoard(1, at.Add(-10*time.Minute), withErrors(mdk.HashboardErrorCause_HASHBOARD_ERROR_CAUSE_ASIC_OVER_TEMPERATURE))
	unchanged := pollErrors(t, state, at, errorsMap(stale, errorsBoard(2, at), errorsBoard(3, at)), plantHealth(at), nil)
	require.Equal(t, report.Errors, unchanged.Errors, "a retained error is stable across polls")

	// The board reports again with the fault still active.
	at = at.Add(30 * time.Second)
	report = pollErrors(t, state, at, errorsMap(errorsBoard(1, at, withErrors(overCurrent)), errorsBoard(2, at), errorsBoard(3, at)), plantHealth(at), nil)
	current := requireError(t, report, kindBoard, "mac:"+errorsMAC(1), sdkerrors.BoardPowerOvercurrent)
	requireEvidence(t, current, errorEvidenceCurrent)
	require.NotContains(t, current.VendorAttributes, "ct1_last_confirmed_at")
	require.True(t, current.FirstSeenAt.Equal(first), "the same fault keeps its first observation")
	require.True(t, current.LastSeenAt.Equal(at))

	at = at.Add(30 * time.Second)
	report = pollErrors(t, state, at, errorsMap(errorsBoard(1, at), errorsBoard(2, at), errorsBoard(3, at)), plantHealth(at), nil)
	require.Empty(t, report.Errors, "its fresh clear reading withdraws the fault")
}

func TestErrorReportDropsFaultsOnlyForBoardsThatLeft(t *testing.T) {
	overCurrent := withErrors(mdk.HashboardErrorCause_HASHBOARD_ERROR_CAUSE_BOARD_OVER_CURRENT)
	for _, tc := range []struct {
		name     string
		slot     func(at time.Time) *mdk.Slot
		empty    bool
		retained bool
	}{
		// Outside switch_ports mode Frigate simply stops listing the board.
		{name: "no longer listed"},
		{name: "no board listed at all", empty: true, retained: true},
		{name: "listed without health", retained: true, slot: func(at time.Time) *mdk.Slot { return errorsBoard(1, at, withoutHealth) }},
		{name: "rejected with its identity", retained: true, slot: func(at time.Time) *mdk.Slot {
			return errorsBoard(1, at, func(b *mdk.Hashboard) { b.Location.HashboardPosition = new(uint32(9)) })
		}},
		{name: "another board rejected without identity", retained: true, slot: func(at time.Time) *mdk.Slot {
			return errorsBoard(1, at, func(b *mdk.Hashboard) { b.MacAddress = "" })
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			at := time.Now().UTC()
			state := &errorState{}
			require.Len(t, pollErrors(t, state, at, errorsMap(errorsBoard(1, at, overCurrent), errorsBoard(2, at), errorsBoard(3, at)), plantHealth(at), nil).Errors, 1)
			at = at.Add(15 * time.Second)
			slots := []*mdk.Slot{errorsBoard(2, at), errorsBoard(3, at)}
			if tc.slot != nil {
				slots = append(slots, tc.slot(at))
			}
			wire := errorsMap(slots...)
			if tc.empty {
				wire = &mdk.GetContainerMapResponse{ContainerMap: &mdk.ContainerMap{}}
			}
			report := pollErrors(t, state, at, wire, plantHealth(at), nil)
			if !tc.retained {
				require.Empty(t, report.Errors, "a removed board cannot hold an error open")
				return
			}
			require.Len(t, report.Errors, 1)
			requireEvidence(t, requireError(t, report, kindBoard, "mac:"+errorsMAC(1), sdkerrors.BoardPowerOvercurrent), errorEvidenceRetained)
		})
	}
}

func TestErrorReportPSUEvidenceFollowsItsSource(t *testing.T) {
	healthy := func(at time.Time) *mdk.GetContainerMapResponse {
		return errorsMap(errorsBoard(1, at), errorsBoard(2, at), errorsBoard(3, at))
	}
	t.Run("thermal state clears with a silent sibling", func(t *testing.T) {
		at := time.Now().UTC()
		state := &errorState{}
		report := pollErrors(t, state, at, errorsMap(errorsBoard(1, at, withPSUFault), errorsBoard(2, at, withPSUFault), errorsBoard(3, at, withPSUFault)), plantHealth(at), nil)
		requireEvidence(t, requireError(t, report, kindPSU, modulePSU, sdkerrors.PSUOverTemperature), errorEvidenceCurrent)
		at = at.Add(15 * time.Second)
		report = pollErrors(t, state, at, errorsMap(errorsBoard(1, at, withoutHealth), errorsBoard(2, at), errorsBoard(3, at)), plantHealth(at), nil)
		require.Empty(t, report.Errors, "the live boards share the PSU's thermal state")
	})
	t.Run("thermal state is retained when no board reports", func(t *testing.T) {
		at := time.Now().UTC()
		state := &errorState{}
		pollErrors(t, state, at, errorsMap(errorsBoard(1, at, withPSUFault), errorsBoard(2, at, withPSUFault), errorsBoard(3, at, withPSUFault)), plantHealth(at), nil)
		at = at.Add(15 * time.Second)
		report := pollErrors(t, state, at, errorsMap(errorsBoard(1, at, withoutHealth), errorsBoard(2, at, withoutHealth), errorsBoard(3, at, withoutHealth)), plantHealth(at), nil)
		requireEvidence(t, requireError(t, report, kindPSU, modulePSU, sdkerrors.PSUOverTemperature), errorEvidenceRetained)
	})
	t.Run("a board's power supply cause stays with that board", func(t *testing.T) {
		at := time.Now().UTC()
		state := &errorState{}
		pollErrors(t, state, at, errorsMap(errorsBoard(1, at, withErrors(mdk.HashboardErrorCause_HASHBOARD_ERROR_CAUSE_BOARD_POWER_SUPPLY)), errorsBoard(2, at), errorsBoard(3, at)), plantHealth(at), nil)
		at = at.Add(15 * time.Second)
		report := pollErrors(t, state, at, errorsMap(errorsBoard(1, at, withoutHealth), errorsBoard(2, at), errorsBoard(3, at)), plantHealth(at), nil)
		requireEvidence(t, requireError(t, report, kindPSU, modulePSU, sdkerrors.PSUFaultGeneric), errorEvidenceRetained)
		at = at.Add(15 * time.Second)
		require.Empty(t, pollErrors(t, state, at, healthy(at), plantHealth(at), nil).Errors)
	})
}

func TestErrorReportPlantAlarms(t *testing.T) {
	listed := &mdk.PlantAlarm{Id: "register-1-bit-2", Name: "Coolant level low", Severity: mdk.Severity_SEVERITY_CRITICAL}
	other := &mdk.PlantAlarm{Id: "register-2-bit-0", Name: "Pump fault", Severity: mdk.Severity_SEVERITY_ERROR}
	const listedID, otherID = "plant-alarm:register-1-bit-2", "plant-alarm:register-2-bit-0"
	for _, tc := range []struct {
		name     string
		plant    func(at time.Time) *mdk.GetContainerHealthResponse
		err      error
		evidence string // of the listed alarm; empty means cleared
		extra    string // another expected plant component ID
	}{
		{name: "read failed", err: status.Error(codes.Unavailable, "container monitor is not configured"), evidence: errorEvidenceRetained},
		{name: "controller without plant health", err: status.Error(codes.Unimplemented, "unknown method"), evidence: errorEvidenceRetained},
		{name: "stale snapshot", evidence: errorEvidenceRetained, plant: func(at time.Time) *mdk.GetContainerHealthResponse { return plantHealth(at.Add(-5 * time.Minute)) }},
		{name: "future snapshot", evidence: errorEvidenceRetained, plant: func(at time.Time) *mdk.GetContainerHealthResponse { return plantHealth(at.Add(time.Minute)) }},
		{name: "no plant telemetry", evidence: errorEvidenceRetained, plant: func(at time.Time) *mdk.GetContainerHealthResponse {
			return &mdk.GetContainerHealthResponse{Timestamp: timestamppb.New(at)}
		}},
		{name: "flags without an alarm list", evidence: errorEvidenceRetained, extra: "plant", plant: func(at time.Time) *mdk.GetContainerHealthResponse {
			return &mdk.GetContainerHealthResponse{Timestamp: timestamppb.New(at), Plant: &mdk.PlantHealth{AlarmActive: new(true), CriticalAlarmActive: new(true)}}
		}},
		{name: "malformed alarm list", evidence: errorEvidenceRetained, plant: func(at time.Time) *mdk.GetContainerHealthResponse {
			return plantHealth(at, &mdk.PlantAlarm{Name: "alarm without an ID"})
		}},
		{name: "still listed", evidence: errorEvidenceCurrent, plant: func(at time.Time) *mdk.GetContainerHealthResponse { return plantHealth(at, listed) }},
		{name: "another alarm listed", extra: otherID, plant: func(at time.Time) *mdk.GetContainerHealthResponse { return plantHealth(at, other) }},
		{name: "fresh empty list", plant: func(at time.Time) *mdk.GetContainerHealthResponse { return plantHealth(at) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			at := time.Now().UTC()
			state := &errorState{}
			board := func(at time.Time) *mdk.GetContainerMapResponse {
				return errorsMap(errorsBoard(1, at, withErrors(mdk.HashboardErrorCause_HASHBOARD_ERROR_CAUSE_BOARD_OVER_CURRENT)), errorsBoard(2, at), errorsBoard(3, at))
			}
			require.Len(t, pollErrors(t, state, at, board(at), plantHealth(at, listed), nil).Errors, 2)

			at = at.Add(15 * time.Second)
			var wire *mdk.GetContainerHealthResponse
			if tc.plant != nil {
				wire = tc.plant(at)
			}
			report := pollErrors(t, state, at, board(at), wire, tc.err)
			want := 1
			requireEvidence(t, requireError(t, report, kindBoard, "mac:"+errorsMAC(1), sdkerrors.BoardPowerOvercurrent), errorEvidenceCurrent)
			if tc.evidence != "" {
				want++
				requireEvidence(t, requireError(t, report, kindPlant, listedID, sdkerrors.VendorErrorUnmapped), tc.evidence)
			}
			if tc.extra != "" {
				want++
				requireEvidence(t, requireError(t, report, kindPlant, tc.extra, sdkerrors.VendorErrorUnmapped), errorEvidenceCurrent)
			}
			require.Len(t, report.Errors, want)
		})
	}
}

func TestErrorReportPlantFlagsWithoutAlarmList(t *testing.T) {
	at := time.Now().UTC()
	state := &errorState{}
	flags := func(at time.Time, critical bool) *mdk.GetContainerHealthResponse {
		return &mdk.GetContainerHealthResponse{Timestamp: timestamppb.New(at), Plant: &mdk.PlantHealth{AlarmActive: new(true), CriticalAlarmActive: new(critical)}}
	}
	report := pollErrors(t, state, at, errorsMap(), flags(at, false), nil)
	require.Len(t, report.Errors, 1)
	aggregate := requireError(t, report, kindPlant, "plant", sdkerrors.VendorErrorUnmapped)
	require.Equal(t, sdkerrors.SeverityMajor, aggregate.Severity)
	require.Equal(t, "Plant: Alarm active; the controller did not list its alarms", aggregate.Summary)
	require.Equal(t, "PLANT_ALARM_ACTIVE", aggregate.VendorAttributes["vendor_code"])

	at = at.Add(15 * time.Second)
	report = pollErrors(t, state, at, errorsMap(), flags(at, true), nil)
	require.Len(t, report.Errors, 1)
	require.Equal(t, sdkerrors.SeverityCritical, requireError(t, report, kindPlant, "plant", sdkerrors.VendorErrorUnmapped).Severity)

	// Once Frigate lists alarms again, the list replaces the flag.
	at = at.Add(15 * time.Second)
	report = pollErrors(t, state, at, errorsMap(), plantHealth(at, &mdk.PlantAlarm{Id: "a", Severity: mdk.Severity_SEVERITY_WARNING}), nil)
	require.Len(t, report.Errors, 1)
	requireError(t, report, kindPlant, "plant-alarm:a", sdkerrors.VendorErrorUnmapped)
}

func TestErrorReportUnlocatedBoards(t *testing.T) {
	at := time.Now().UTC()
	unlocated := func(edits ...func(*mdk.Hashboard)) *mdk.GetContainerMapResponse {
		slot := errorsBoard(1, at, edits...)
		slot.Hashboard.Location = nil
		return &mdk.GetContainerMapResponse{ContainerMap: &mdk.ContainerMap{Tanks: []*mdk.Tank{{Id: 0, Modules: []*mdk.Module{{Id: 0, Slots: []*mdk.Slot{slot}}}}}}}
	}
	state := &errorState{}
	id := "mac:" + errorsMAC(1)
	report := pollErrors(t, state, at, unlocated(withPSUFault, withErrors(mdk.HashboardErrorCause_HASHBOARD_ERROR_CAUSE_BOARD_OVER_CURRENT)), plantHealth(at), nil)
	require.Len(t, report.Errors, 2)
	board := requireError(t, report, kindBoard, id, sdkerrors.BoardPowerOvercurrent)
	require.Equal(t, "Unlocated hashboard aa:bb:cc:dd:ee:01: Board over current", board.Summary)
	require.Equal(t, errorsMAC(1), board.VendorAttributes["mac_address"])
	require.NotContains(t, board.VendorAttributes, "tank")
	thermal := requireError(t, report, kindPSU, id, sdkerrors.PSUOverTemperature)
	require.Equal(t, "PSU of unlocated hashboard aa:bb:cc:dd:ee:01: PSU over temperature", thermal.Summary)

	at = at.Add(15 * time.Second)
	report = pollErrors(t, state, at, unlocated(withoutHealth), plantHealth(at), nil)
	require.Len(t, report.Errors, 2)
	for _, reported := range report.Errors {
		requireEvidence(t, reported, errorEvidenceRetained)
	}
}

func TestErrorStateIgnoresOlderEvidence(t *testing.T) {
	at := time.Now().UTC()
	state := &errorState{}
	healthy := errorsMap(errorsBoard(1, at), errorsBoard(2, at), errorsBoard(3, at))
	require.Empty(t, pollErrors(t, state, at, healthy, plantHealth(at), nil).Errors)
	earlier := at.Add(-5 * time.Second)
	faulted := errorsMap(errorsBoard(1, earlier, withErrors(mdk.HashboardErrorCause_HASHBOARD_ERROR_CAUSE_BOARD_OVER_CURRENT)), errorsBoard(2, earlier), errorsBoard(3, earlier))
	require.Empty(t, pollErrors(t, state, earlier, faulted, plantHealth(earlier), nil).Errors, "a delayed read cannot restore what a newer one cleared")
}

// A later read can return an earlier sample of a source, for example from a
// standby that lags the primary. The sample is still under two minutes old.
func TestErrorReportIgnoresSamplesOlderThanOnesAlreadyApplied(t *testing.T) {
	overCurrent := withErrors(mdk.HashboardErrorCause_HASHBOARD_ERROR_CAUSE_BOARD_OVER_CURRENT)
	boardID := "mac:" + errorsMAC(1)
	listed := &mdk.PlantAlarm{Id: "register-1-bit-2", Name: "Coolant level low", Severity: mdk.Severity_SEVERITY_CRITICAL}
	const listedID = "plant-alarm:register-1-bit-2"

	t.Run("board sample cannot clear a newer error", func(t *testing.T) {
		first := time.Now().UTC().Truncate(time.Second)
		state := &errorState{}
		require.Len(t, pollErrors(t, state, first, errorsMap(errorsBoard(1, first, overCurrent), errorsBoard(2, first), errorsBoard(3, first)), plantHealth(first), nil).Errors, 1)

		at := first.Add(15 * time.Second)
		report := pollErrors(t, state, at, errorsMap(errorsBoard(1, first.Add(-10*time.Second)), errorsBoard(2, at), errorsBoard(3, at)), plantHealth(at), nil)
		require.Len(t, report.Errors, 1, "the earlier healthy sample says nothing about the newer error")
		retained := requireError(t, report, kindBoard, boardID, sdkerrors.BoardPowerOvercurrent)
		requireEvidence(t, retained, errorEvidenceRetained)
		require.Equal(t, first.Format(time.RFC3339), retained.VendorAttributes["ct1_last_confirmed_at"])

		// The error was not lost: the board going silent still keeps it.
		at = at.Add(15 * time.Second)
		report = pollErrors(t, state, at, errorsMap(errorsBoard(1, at, withoutHealth), errorsBoard(2, at), errorsBoard(3, at)), plantHealth(at), nil)
		requireEvidence(t, requireError(t, report, kindBoard, boardID, sdkerrors.BoardPowerOvercurrent), errorEvidenceRetained)

		at = at.Add(15 * time.Second)
		require.Empty(t, pollErrors(t, state, at, errorsMap(errorsBoard(1, at), errorsBoard(2, at), errorsBoard(3, at)), plantHealth(at), nil).Errors, "a newer healthy sample clears it")
	})
	t.Run("board sample cannot restore a cleared error", func(t *testing.T) {
		first := time.Now().UTC().Truncate(time.Second)
		state := &errorState{}
		require.Empty(t, pollErrors(t, state, first, errorsMap(errorsBoard(1, first), errorsBoard(2, first), errorsBoard(3, first)), plantHealth(first), nil).Errors)

		// The ordering is remembered although the source holds no error.
		at := first.Add(15 * time.Second)
		report := pollErrors(t, state, at, errorsMap(errorsBoard(1, first.Add(-10*time.Second), overCurrent), errorsBoard(2, at), errorsBoard(3, at)), plantHealth(at), nil)
		require.Empty(t, report.Errors, "the newer sample already reported the board without this error")

		at = at.Add(15 * time.Second)
		report = pollErrors(t, state, at, errorsMap(errorsBoard(1, at, overCurrent), errorsBoard(2, at), errorsBoard(3, at)), plantHealth(at), nil)
		requireEvidence(t, requireError(t, report, kindBoard, boardID, sdkerrors.BoardPowerOvercurrent), errorEvidenceCurrent)
	})
	t.Run("samples are ordered by their own time when the controller clock is ahead", func(t *testing.T) {
		first := time.Now().UTC().Truncate(time.Second)
		state := &errorState{}
		ahead := first.Add(4 * time.Second)
		require.Len(t, pollErrors(t, state, first, errorsMap(errorsBoard(1, ahead, overCurrent), errorsBoard(2, first), errorsBoard(3, first)), plantHealth(first), nil).Errors, 1)

		// This sample is later than the first collection but earlier than the
		// sample that collection applied.
		at := first.Add(15 * time.Second)
		report := pollErrors(t, state, at, errorsMap(errorsBoard(1, ahead.Add(-2*time.Second)), errorsBoard(2, at), errorsBoard(3, at)), plantHealth(at), nil)
		requireEvidence(t, requireError(t, report, kindBoard, boardID, sdkerrors.BoardPowerOvercurrent), errorEvidenceRetained)
	})
	t.Run("the same sample read again still applies", func(t *testing.T) {
		first := time.Now().UTC().Truncate(time.Second)
		state := &errorState{}
		wire := func() *mdk.GetContainerMapResponse {
			return errorsMap(errorsBoard(1, first, overCurrent), errorsBoard(2, first), errorsBoard(3, first))
		}
		pollErrors(t, state, first, wire(), plantHealth(first), nil)
		report := pollErrors(t, state, first.Add(15*time.Second), wire(), plantHealth(first), nil)
		requireEvidence(t, requireError(t, report, kindBoard, boardID, sdkerrors.BoardPowerOvercurrent), errorEvidenceCurrent)
	})
	t.Run("PSU thermal state", func(t *testing.T) {
		first := time.Now().UTC().Truncate(time.Second)
		state := &errorState{}
		report := pollErrors(t, state, first, errorsMap(errorsBoard(1, first, withPSUFault), errorsBoard(2, first, withPSUFault), errorsBoard(3, first, withPSUFault)), plantHealth(first), nil)
		requireEvidence(t, requireError(t, report, kindPSU, modulePSU, sdkerrors.PSUOverTemperature), errorEvidenceCurrent)

		// The boards' own samples are new, but their copy of the shared PSU
		// state was taken before the one already applied.
		at := first.Add(15 * time.Second)
		aged := func(b *mdk.Hashboard) { b.Health.PsuThermal.ThermalStateAgeMs = new(uint32(30_000)) }
		report = pollErrors(t, state, at, errorsMap(errorsBoard(1, at, aged, overCurrent), errorsBoard(2, at, aged), errorsBoard(3, at, aged)), plantHealth(at), nil)
		require.Len(t, report.Errors, 2)
		requireEvidence(t, requireError(t, report, kindPSU, modulePSU, sdkerrors.PSUOverTemperature), errorEvidenceRetained)
		requireEvidence(t, requireError(t, report, kindBoard, boardID, sdkerrors.BoardPowerOvercurrent), errorEvidenceCurrent)

		at = at.Add(15 * time.Second)
		require.Empty(t, pollErrors(t, state, at, errorsMap(errorsBoard(1, at), errorsBoard(2, at), errorsBoard(3, at)), plantHealth(at), nil).Errors)
	})
	t.Run("plant alarms", func(t *testing.T) {
		first := time.Now().UTC().Truncate(time.Second)
		state := &errorState{}
		require.Len(t, pollErrors(t, state, first, errorsMap(), plantHealth(first, listed), nil).Errors, 1)

		at := first.Add(15 * time.Second)
		older := first.Add(-10 * time.Second)
		report := pollErrors(t, state, at, errorsMap(), plantHealth(older), nil)
		require.Len(t, report.Errors, 1, "an earlier empty alarm list cannot clear the alarm")
		requireEvidence(t, requireError(t, report, kindPlant, listedID, sdkerrors.VendorErrorUnmapped), errorEvidenceRetained)

		at = at.Add(15 * time.Second)
		report = pollErrors(t, state, at, errorsMap(), plantHealth(older, &mdk.PlantAlarm{Id: "register-2-bit-0", Severity: mdk.Severity_SEVERITY_ERROR}), nil)
		require.Len(t, report.Errors, 1, "nor can an earlier list raise another alarm")
		requireEvidence(t, requireError(t, report, kindPlant, listedID, sdkerrors.VendorErrorUnmapped), errorEvidenceRetained)

		at = at.Add(15 * time.Second)
		require.Empty(t, pollErrors(t, state, at, errorsMap(), plantHealth(at), nil).Errors)
	})
}

func TestErrorStateOrderingMarksExpire(t *testing.T) {
	at := time.Now().UTC()
	state := &errorState{}
	pollErrors(t, state, at, errorsMap(errorsBoard(1, at), errorsBoard(2, at), errorsBoard(3, at)), plantHealth(at), nil)
	require.Len(t, state.sampledAt, 6, "three boards, their PSU, and the two plant readings, although none holds an error")

	// A sample is only accepted while under two minutes old, so a mark past
	// that age can reject nothing and is dropped.
	at = at.Add(healthMaxAge + time.Second)
	pollErrors(t, state, at, errorsMap(errorsBoard(1, at, withoutHealth), errorsBoard(2, at, withoutHealth), errorsBoard(3, at, withoutHealth)), nil, status.Error(codes.Unavailable, "plant unavailable"))
	require.Empty(t, state.sampledAt)
}

// Frigate health can carry more causes than a report retains. The omitted
// causes are unknown, so the report is not a complete reading of its boards.
func TestErrorReportTruncatedEvidenceClearsNothing(t *testing.T) {
	at := time.Now().UTC()
	state := &errorState{}
	boardID := "mac:" + errorsMAC(1)
	require.Len(t, pollErrors(t, state, at, errorsMap(errorsBoard(1, at, withErrors(mdk.HashboardErrorCause_HASHBOARD_ERROR_CAUSE_BOARD_OVER_CURRENT)), errorsBoard(2, at), errorsBoard(3, at)), plantHealth(at), nil).Errors, 1)

	var causes []mdk.HashboardErrorCause
	for i := range maxModuleIssues + 1 {
		causes = append(causes, mdk.HashboardErrorCause(5000+i)) //nolint:gosec // bounded test index
	}
	at = at.Add(15 * time.Second)
	report := pollErrors(t, state, at, errorsMap(errorsBoard(1, at, withErrors(causes...)), errorsBoard(2, at), errorsBoard(3, at)), plantHealth(at), nil)
	require.Len(t, report.Errors, 2)
	requireEvidence(t, requireError(t, report, kindBoard, boardID, sdkerrors.BoardPowerOvercurrent), errorEvidenceRetained)
	requireEvidence(t, requireError(t, report, kindBoard, boardID, sdkerrors.VendorErrorUnmapped), errorEvidenceCurrent)

	at = at.Add(15 * time.Second)
	require.Empty(t, pollErrors(t, state, at, errorsMap(errorsBoard(1, at), errorsBoard(2, at), errorsBoard(3, at)), plantHealth(at), nil).Errors)
}

// A power-supply cause is asserted by the board that reported it and by the
// module's shared thermal state, possibly at different severities.
func TestErrorReportKeepsTheMostSevereUnresolvedClaim(t *testing.T) {
	first := time.Now().UTC().Truncate(time.Second)
	state := &errorState{}
	boardFault := withErrors(mdk.HashboardErrorCause_HASHBOARD_ERROR_CAUSE_BOARD_POWER_SUPPLY_OVER_TEMPERATURE)
	thermalWarning := func(b *mdk.Hashboard) { b.Health.PsuThermal.OverTemperatureWarning = true }

	report := pollErrors(t, state, first, errorsMap(errorsBoard(1, first, boardFault), errorsBoard(2, first), errorsBoard(3, first)), plantHealth(first), nil)
	require.Len(t, report.Errors, 1)
	require.Equal(t, sdkerrors.SeverityCritical, requireError(t, report, kindPSU, modulePSU, sdkerrors.PSUOverTemperature).Severity)

	// Board 1 stops reporting while the shared thermal state warns.
	at := first.Add(15 * time.Second)
	report = pollErrors(t, state, at, errorsMap(errorsBoard(1, at, withoutHealth), errorsBoard(2, at, thermalWarning), errorsBoard(3, at, thermalWarning)), plantHealth(at), nil)
	require.Len(t, report.Errors, 1)
	mixed := requireError(t, report, kindPSU, modulePSU, sdkerrors.PSUOverTemperature)
	require.Equal(t, sdkerrors.SeverityCritical, mixed.Severity, "the warning does not withdraw the silent board's fault")
	requireEvidence(t, mixed, errorEvidenceCurrent)
	require.True(t, mixed.LastSeenAt.Equal(at))
	require.True(t, mixed.FirstSeenAt.Equal(first))

	// The warning clears. Board 1 has still not withdrawn its fault.
	at = at.Add(15 * time.Second)
	report = pollErrors(t, state, at, errorsMap(errorsBoard(1, at, withoutHealth), errorsBoard(2, at), errorsBoard(3, at)), plantHealth(at), nil)
	require.Len(t, report.Errors, 1)
	retained := requireError(t, report, kindPSU, modulePSU, sdkerrors.PSUOverTemperature)
	require.Equal(t, sdkerrors.SeverityCritical, retained.Severity)
	requireEvidence(t, retained, errorEvidenceRetained)
	require.Equal(t, first.Format(time.RFC3339), retained.VendorAttributes["ct1_last_confirmed_at"], "the board's fault was last confirmed by the board")

	// A warning alone is reported at its own severity once the fault is withdrawn.
	at = at.Add(15 * time.Second)
	report = pollErrors(t, state, at, errorsMap(errorsBoard(1, at, thermalWarning), errorsBoard(2, at, thermalWarning), errorsBoard(3, at, thermalWarning)), plantHealth(at), nil)
	require.Equal(t, sdkerrors.SeverityMajor, requireError(t, report, kindPSU, modulePSU, sdkerrors.PSUOverTemperature).Severity)

	at = at.Add(15 * time.Second)
	require.Empty(t, pollErrors(t, state, at, errorsMap(errorsBoard(1, at), errorsBoard(2, at), errorsBoard(3, at)), plantHealth(at), nil).Errors)
}

func TestErrorPollingCapabilityIsAdvertised(t *testing.T) {
	require.True(t, Capabilities()[sdk.CapabilityGetErrors])
}

// isolateErrorStates keeps carried faults from leaking between tests that
// address the same simulated container.
func isolateErrorStates(t *testing.T) {
	t.Helper()
	errorStates.Lock()
	saved := errorStates.byContainer
	errorStates.byContainer = map[string]*errorState{}
	errorStates.Unlock()
	t.Cleanup(func() {
		errorStates.Lock()
		errorStates.byContainer = saved
		errorStates.Unlock()
	})
}

// expireErrorEvidence makes the next GetErrors read the controller again.
func expireErrorEvidence(d *Device) {
	state := d.errorsShared()
	state.mu.Lock()
	state.observedAt = state.observedAt.Add(-errorEvidenceMaxAge)
	state.mu.Unlock()
}

// errorsController serves the controller reads over real gRPC.
type errorsController struct {
	mu          sync.Mutex
	wire        func(at time.Time) *mdk.GetContainerMapResponse
	mapStatus   int
	plant       func(at time.Time) *mdk.GetContainerHealthResponse
	plantStatus int
	calls       map[string]int
}

func (c *errorsController) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.calls == nil {
		c.calls = map[string]int{}
	}
	c.calls[r.URL.Path]++
	at := time.Now().UTC()
	switch r.URL.Path {
	case "/api/v2/container/map":
		if c.mapStatus != 0 {
			w.WriteHeader(c.mapStatus)
			return
		}
		data, err := protojson.Marshal(c.wire(at))
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write(data)
	case "/api/v2/telemetry/container":
		if c.plantStatus != 0 {
			w.WriteHeader(c.plantStatus)
			return
		}
		wire := plantHealth(at)
		if c.plant != nil {
			wire = c.plant(at)
		}
		data, err := protojson.Marshal(wire)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write(data)
	default:
		_, _ = w.Write([]byte(`{}`))
	}
}

func (c *errorsController) set(edit func(*errorsController)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	edit(c)
}

func (c *errorsController) count(path string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls[path]
}

func (c *errorsController) device(t *testing.T) *Device {
	t.Helper()
	server := newGRPCFixture(t, c)
	u, err := url.Parse(server.URL)
	require.NoError(t, err)
	port, err := strconv.ParseInt(u.Port(), 10, 32)
	require.NoError(t, err)
	d, err := NewDevice(t.Context(), errorsDevice, sdk.DeviceInfo{Host: u.Hostname(), Port: 8080, URLScheme: "http", RPCPort: int32(port)}, sdk.UsernamePassword{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close(context.Background()) })
	return d
}

func faultedBoards(at time.Time) *mdk.GetContainerMapResponse {
	return errorsMap(errorsBoard(1, at, withErrors(mdk.HashboardErrorCause_HASHBOARD_ERROR_CAUSE_BOARD_OVER_CURRENT)), errorsBoard(2, at), errorsBoard(3, at))
}

func TestGetErrorsReadsControllerFaults(t *testing.T) {
	isolateErrorStates(t)
	controller := &errorsController{wire: faultedBoards, plant: func(at time.Time) *mdk.GetContainerHealthResponse {
		return plantHealth(at, &mdk.PlantAlarm{Id: "register-1-bit-2", Name: "Coolant level low", Severity: mdk.Severity_SEVERITY_CRITICAL})
	}}
	d := controller.device(t)
	report, err := d.GetErrors(t.Context())
	require.NoError(t, err)
	require.Equal(t, errorsDevice, report.DeviceID)
	require.Len(t, report.Errors, 2)
	requireError(t, report, kindBoard, "mac:"+errorsMAC(1), sdkerrors.BoardPowerOvercurrent)
	requireError(t, report, kindPlant, "plant-alarm:register-1-bit-2", sdkerrors.VendorErrorUnmapped)
	require.Equal(t, 1, controller.count("/api/v2/container/map"))
	require.Equal(t, 1, controller.count("/api/v2/telemetry/container"))
	require.Zero(t, controller.count("/api/v2/mining/status"), "error polling reads only its own sources")

	controller.set(func(c *errorsController) {
		c.wire = func(at time.Time) *mdk.GetContainerMapResponse {
			return errorsMap(errorsBoard(1, at), errorsBoard(2, at), errorsBoard(3, at))
		}
		c.plant = nil
	})
	expireErrorEvidence(d)
	report, err = d.GetErrors(t.Context())
	require.NoError(t, err)
	require.Empty(t, report.Errors)
}

func TestGetErrorsReusesTheTelemetryPoll(t *testing.T) {
	isolateErrorStates(t)
	controller := &errorsController{wire: faultedBoards}
	d := controller.device(t)
	_, err := d.Status(t.Context())
	require.NoError(t, err)
	report, err := d.GetErrors(t.Context())
	require.NoError(t, err)
	requireError(t, report, kindBoard, "mac:"+errorsMAC(1), sdkerrors.BoardPowerOvercurrent)
	require.Equal(t, 1, controller.count("/api/v2/container/map"), "the telemetry poll's map read serves error polling")
	require.Equal(t, 1, controller.count("/api/v2/telemetry/container"))

	expireErrorEvidence(d)
	_, err = d.GetErrors(t.Context())
	require.NoError(t, err)
	require.Equal(t, 2, controller.count("/api/v2/container/map"), "older evidence is not reused")
}

func TestGetErrorsFailedMapReadIsNotAnEmptyReport(t *testing.T) {
	isolateErrorStates(t)
	controller := &errorsController{wire: faultedBoards}
	d := controller.device(t)
	report, err := d.GetErrors(t.Context())
	require.NoError(t, err)
	require.Len(t, report.Errors, 1)

	controller.set(func(c *errorsController) { c.mapStatus = http.StatusServiceUnavailable })
	expireErrorEvidence(d)
	report, err = d.GetErrors(t.Context())
	require.Equal(t, codes.Unavailable, status.Code(err))
	require.Empty(t, report.Errors)

	// The failed read changed nothing: the next good read still sees the fault.
	controller.set(func(c *errorsController) { c.mapStatus = 0 })
	report, err = d.GetErrors(t.Context())
	require.NoError(t, err)
	requireEvidence(t, requireError(t, report, kindBoard, "mac:"+errorsMAC(1), sdkerrors.BoardPowerOvercurrent), errorEvidenceCurrent)
}

func TestGetErrorsKeepsBoardFaultsWithoutPlantHealth(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
	}{
		{"controller without plant health", http.StatusNotFound},
		{"plant read failing", http.StatusServiceUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolateErrorStates(t)
			controller := &errorsController{wire: faultedBoards, plant: func(at time.Time) *mdk.GetContainerHealthResponse {
				return plantHealth(at, &mdk.PlantAlarm{Id: "register-1-bit-2", Severity: mdk.Severity_SEVERITY_CRITICAL})
			}}
			d := controller.device(t)
			report, err := d.GetErrors(t.Context())
			require.NoError(t, err)
			require.Len(t, report.Errors, 2)

			controller.set(func(c *errorsController) { c.plantStatus = tc.status })
			expireErrorEvidence(d)
			report, err = d.GetErrors(t.Context())
			require.NoError(t, err, "plant health is a supplement")
			require.Len(t, report.Errors, 2)
			requireEvidence(t, requireError(t, report, kindBoard, "mac:"+errorsMAC(1), sdkerrors.BoardPowerOvercurrent), errorEvidenceCurrent)
			requireEvidence(t, requireError(t, report, kindPlant, "plant-alarm:register-1-bit-2", sdkerrors.VendorErrorUnmapped), errorEvidenceRetained)
		})
	}
}

// Fleet Node opens a new handle for every command.
func TestGetErrorsCarriesFaultsAcrossHandles(t *testing.T) {
	isolateErrorStates(t)
	controller := &errorsController{wire: faultedBoards}
	first := controller.device(t)
	report, err := first.GetErrors(t.Context())
	require.NoError(t, err)
	require.Len(t, report.Errors, 1)

	controller.set(func(c *errorsController) {
		c.wire = func(at time.Time) *mdk.GetContainerMapResponse {
			return errorsMap(errorsBoard(1, at, withoutHealth), errorsBoard(2, at), errorsBoard(3, at))
		}
	})
	expireErrorEvidence(first)
	second := controller.device(t)
	report, err = second.GetErrors(t.Context())
	require.NoError(t, err)
	require.Len(t, report.Errors, 1)
	requireEvidence(t, requireError(t, report, kindBoard, "mac:"+errorsMAC(1), sdkerrors.BoardPowerOvercurrent), errorEvidenceRetained)
}

func TestGetErrorsFallsBackToTheVerifiedStandby(t *testing.T) {
	isolateErrorStates(t)
	a, b := startHAServer(t, "a", "b"), startHAServer(t, "b", "a")
	a.primary.Store(true)
	a.mapError = status.Error(codes.Unavailable, "primary cannot read hashboards")
	b.containerMap = faultedBoards(time.Now().UTC())
	d, err := NewDevice(t.Context(), errorsDevice, haInfo(a, b), sdk.UsernamePassword{})
	require.NoError(t, err)
	defer d.Close(t.Context())
	report, err := d.GetErrors(t.Context())
	require.NoError(t, err)
	// These controllers serve no plant health, like older Frigate releases.
	require.Len(t, report.Errors, 1)
	requireError(t, report, kindBoard, "mac:"+errorsMAC(1), sdkerrors.BoardPowerOvercurrent)
	require.EqualValues(t, 1, a.mapReads.Load())
	require.EqualValues(t, 1, b.mapReads.Load())

	b.mapError = status.Error(codes.Unavailable, "standby cannot read hashboards")
	expireErrorEvidence(d)
	_, err = d.GetErrors(t.Context())
	require.Equal(t, codes.Unavailable, status.Code(err), "both controllers failing is a failed poll")
}

func TestGetErrorsStandbySampleCannotClearAPrimaryError(t *testing.T) {
	isolateErrorStates(t)
	a, b := startHAServer(t, "a", "b"), startHAServer(t, "b", "a")
	a.primary.Store(true)
	sampled := time.Now().UTC()
	a.containerMap = faultedBoards(sampled)
	// The standby still serves each board's earlier, healthy sample.
	earlier := sampled.Add(-10 * time.Second)
	b.containerMap = errorsMap(errorsBoard(1, earlier), errorsBoard(2, earlier), errorsBoard(3, earlier))
	d, err := NewDevice(t.Context(), errorsDevice, haInfo(a, b), sdk.UsernamePassword{})
	require.NoError(t, err)
	defer d.Close(t.Context())
	report, err := d.GetErrors(t.Context())
	require.NoError(t, err)
	requireEvidence(t, requireError(t, report, kindBoard, "mac:"+errorsMAC(1), sdkerrors.BoardPowerOvercurrent), errorEvidenceCurrent)

	a.mapError = status.Error(codes.Unavailable, "primary cannot read hashboards")
	expireErrorEvidence(d)
	report, err = d.GetErrors(t.Context())
	require.NoError(t, err)
	require.EqualValues(t, 1, b.mapReads.Load(), "this report came from the standby")
	require.Len(t, report.Errors, 1)
	requireEvidence(t, requireError(t, report, kindBoard, "mac:"+errorsMAC(1), sdkerrors.BoardPowerOvercurrent), errorEvidenceRetained)
}
