package ct1

import (
	"testing"
	"time"

	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	sdkerrors "github.com/block/proto-fleet/server/sdk/v1/errors"
	"github.com/stretchr/testify/require"
)

func TestCanonicalCT1CauseCoverage(t *testing.T) {
	for number, name := range mdk.HashboardErrorCause_name {
		if number == 0 || number == 99 {
			continue
		}
		t.Run(name, func(t *testing.T) {
			code, severity := canonicalIssue(name, false)
			require.NotEqual(t, sdkerrors.VendorErrorUnmapped, code)
			require.NotEqual(t, sdkerrors.SeverityUnspecified, severity)
		})
	}
	code, _ := canonicalIssue("future_cause", false)
	require.Equal(t, sdkerrors.VendorErrorUnmapped, code)
	for _, cause := range []string{"ASIC_ENUMERATION", "ASIC_ECC", "ASIC_HOTSPOT_TEMPERATURE_DELTA"} {
		code, _ := canonicalIssue(cause, false)
		require.NotContains(t, []sdkerrors.MinerError{sdkerrors.HashboardMissingChips, sdkerrors.ASICCRCErrorExcessive, sdkerrors.HashboardASICOverTemperature}, code)
	}
}

func TestCanonicalStatusKeepsWarningsIsolationAndOperationDistinct(t *testing.T) {
	now := time.Now().UTC()
	board := Board{MiningState: miningStateMining, Isolated: true, Health: &Health{Timestamp: now, Errors: []Issue{}, Warnings: []Issue{{Cause: "HASHBOARD_ERROR_CAUSE_BOARD_OVER_TEMPERATURE"}}}}
	require.Equal(t, sdk.MinerStatusActive, boardState(board, now))
	code, severity := canonicalIssue(board.Health.Warnings[0].Cause, true)
	require.Equal(t, sdkerrors.HashboardOverTemperature, code)
	require.Equal(t, sdkerrors.SeverityMajor, severity)
	board.Health.Errors = board.Health.Warnings
	require.Equal(t, sdk.MinerStatusError, boardState(board, now))
	board.Health.Timestamp = now.Add(-time.Hour)
	require.Equal(t, sdk.MinerStatusActive, boardState(board, now), "stale diagnostics cannot override a current operating observation")
	board.MiningState = miningStateError
	require.Equal(t, sdk.MinerStatusError, boardState(board, now), "explicit failure does not require a health sample")
	board.MiningState = "MINING_STATE_FLASHING"
	require.Equal(t, sdk.MinerStatusUpdating, boardState(board, now))
}

func TestCanonicalModuleStatusRetainsPartialProduction(t *testing.T) {
	now := time.Now()
	boards := []Board{{MiningState: miningStateMining}, {MiningState: miningStateIdle}, {MiningState: "MINING_STATE_DISCONNECTED"}}
	require.Equal(t, sdk.MinerStatusActive, moduleState(boards, now))
	boards[1].MiningState = miningStateError
	require.Equal(t, sdk.MinerStatusError, moduleState(boards, now))
	require.Equal(t, sdk.MinerStatusUnknown, moduleState(boards[:1], now))
}

func TestCanonicalPSUFaultRetainsSourceTimestamp(t *testing.T) {
	now := time.Now().UTC()
	health := now.Add(-10 * time.Second)
	board := Board{MiningState: miningStateMining, Health: &Health{Timestamp: health, PSUThermal: &PSUThermal{ThermalStateValid: true, ThermalStateAgeMs: new(15000.0), OverTemperatureFault: true}}}
	require.Equal(t, sdk.MinerStatusError, boardState(board, now))
	require.Equal(t, now.Add(-25*time.Second), moduleObservedAt([]Board{board}, sdk.MinerStatusError, now))
}
