package ct1

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestModuleIssues(t *testing.T) {
	now := time.Now().UTC()
	boards := func() []Board {
		out := []Board{}
		for _, mac := range []string{"left", "middle", "right"} {
			out = append(out, Board{MACAddress: mac, MiningState: "MINING_STATE_MINING", Health: &Health{Timestamp: now, Errors: []Issue{}, Warnings: []Issue{}, PSUThermal: &PSUThermal{ThermalStateValid: true}}})
		}
		return out
	}
	t.Run("board attribution and severity", func(t *testing.T) {
		b := boards()
		b[1].Health.Errors = []Issue{{Cause: "HASHBOARD_ERROR_CAUSE_BOARD_OVER_CURRENT"}}
		b[2].Health.Warnings = []Issue{{Cause: "ASIC_THERMAL_THROTTLING"}}
		got := moduleIssues(b, now, false)
		require.True(t, got.Complete)
		require.Len(t, got.Items, 2)
		require.Equal(t, "middle", got.Items[0].ComponentID)
		require.Equal(t, "fault", got.Items[0].Severity)
		require.Equal(t, "right", got.Items[1].ComponentID)
		require.Equal(t, "warning", got.Items[1].Severity)
		require.Empty(t, moduleIssues(boards(), now, false).Items)
	})
	t.Run("shared PSU deduplicates", func(t *testing.T) {
		b := boards()
		for i := range b {
			b[i].Health.PSUThermal.OverTemperatureFault = true
			b[i].Health.Errors = []Issue{{Cause: "HASHBOARD_ERROR_CAUSE_BOARD_POWER_SUPPLY"}}
		}
		got := moduleIssues(b, now, false)
		require.Len(t, got.Items, 2)
		for _, issue := range got.Items {
			require.Equal(t, "psu", issue.Component)
			require.Equal(t, "psu", issue.ComponentID)
		}
		require.Empty(t, moduleIssues(boards(), now, false).Items)
	})
	t.Run("thermal aliases and warnings do not inflate shared condition", func(t *testing.T) {
		b := boards()
		for i := range b {
			b[i].Health.Errors = []Issue{{Cause: "HASHBOARD_ERROR_CAUSE_BOARD_POWER_SUPPLY_OVER_TEMPERATURE"}}
			b[i].Health.PSUThermal.OverTemperatureFault = true
			b[i].Health.PSUThermal.OverTemperatureWarning = true
		}
		got := moduleIssues(b, now, false)
		require.Len(t, got.Items, 1)
		require.Equal(t, "fault", got.Items[0].Severity)
	})
	t.Run("equal-time PSU replicas retain an explicit fault", func(t *testing.T) {
		b := boards()
		b[2].Health.PSUThermal.OverTemperatureFault = true
		got := moduleIssues(b, now, false)
		require.Len(t, got.Items, 1)
	})
	t.Run("freshest PSU read clears older repeated flags", func(t *testing.T) {
		b := boards()
		b[0].Health.Timestamp = now.Add(-time.Second)
		b[0].Health.PSUThermal.OverTemperatureFault = true
		require.Empty(t, moduleIssues(b, now, false).Items)
	})
	t.Run("mixed states are observations", func(t *testing.T) {
		b := boards()
		b[0].MiningState = miningStateIdle
		got := moduleIssues(b, now, false)
		require.Len(t, got.Items, 1)
		require.Equal(t, "observation", got.Items[0].Severity)
		require.Equal(t, "MIXED_BOARD_STATES", got.Items[0].Cause)
	})
	t.Run("stale missing and conflicting identity", func(t *testing.T) {
		b := boards()
		b[0].Health.Timestamp = now.Add(-time.Hour)
		b[0].Health.Errors = []Issue{{Cause: "fault"}}
		got := moduleIssues(b, now, false)
		require.False(t, got.Complete)
		require.Empty(t, got.Items)
		require.Equal(t, b[0].Health.Timestamp, got.ObservedAt)
		b[0].Health = nil
		require.False(t, moduleIssues(b, now, false).Complete)
		require.Empty(t, moduleIssues(b, now, true).Items)
	})
	t.Run("PSU source age is relative to board sample", func(t *testing.T) {
		b := boards()
		age := 1000.0
		for i := range b {
			b[i].Health.Timestamp = now.Add(-30 * time.Second)
			b[i].Health.PSUThermal.ThermalStateAgeMs = &age
			b[i].Health.PSUThermal.OverTemperatureWarning = true
		}
		got := moduleIssues(b, now, false)
		require.Equal(t, now.Add(-31*time.Second), got.Items[0].ObservedAt)
	})
	t.Run("missing cause arrays cannot confirm healthy", func(t *testing.T) {
		b := boards()
		b[0].Health.Errors = nil
		got := moduleIssues(b, now, false)
		require.False(t, got.Complete)
		require.Empty(t, got.Items)
	})
	t.Run("status alone does not invent a fault", func(t *testing.T) {
		b := boards()
		b[0].MiningState = miningStateError
		for _, issue := range moduleIssues(b, now, false).Items {
			require.Equal(t, "observation", issue.Severity)
		}
	})
}
