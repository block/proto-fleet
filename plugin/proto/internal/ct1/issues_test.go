package ct1

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
	pb "github.com/block/proto-fleet/server/sdk/v1/pb/generated"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
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
	t.Run("oversized causes are omitted without confirming a clear reading", func(t *testing.T) {
		b := boards()
		b[0].Health.Errors = []Issue{{Cause: strings.Repeat("x", 4<<20)}, {Cause: "KNOWN_FAULT"}}
		got := moduleIssues(b, now, false)
		require.False(t, got.Complete)
		require.Empty(t, got.SourceTimes)
		require.Len(t, got.Items, 1)
		require.Equal(t, "KNOWN_FAULT", got.Items[0].Cause)
		b[0].Health.Errors = []Issue{{Cause: strings.Repeat("x", maxIssueCauseBytes)}}
		got = moduleIssues(b, now, false)
		require.True(t, got.Complete, "the exact cause byte limit is retained")
		require.Len(t, got.SourceTimes, boardsPerModule+1)
		require.Len(t, got.Items, 1)
	})
	t.Run("count limit retains unique causes and permits duplicate severity upgrades", func(t *testing.T) {
		b := boards()
		for i := range maxModuleIssues {
			b[0].Health.Warnings = append(b[0].Health.Warnings, Issue{Cause: fmt.Sprintf("PSU_CAUSE_%d", i)})
		}
		got := moduleIssues(b, now, false)
		require.True(t, got.Complete)
		require.Len(t, got.Items, maxModuleIssues)
		b[1].Health.Errors = []Issue{{Cause: "PSU_CAUSE_0"}, {Cause: "PSU_OMITTED"}}
		got = moduleIssues(b, now, false)
		require.False(t, got.Complete)
		require.Empty(t, got.SourceTimes)
		require.Len(t, got.Items, maxModuleIssues)
		require.Equal(t, "PSU_CAUSE_0", got.Items[0].Cause)
		require.Equal(t, "fault", got.Items[0].Severity)
	})
	t.Run("component IDs and clear reading keys are bounded", func(t *testing.T) {
		b := boards()
		b[0].MACAddress = strings.Repeat("x", maxIssueComponentIDBytes+1)
		b[0].Health.Errors = []Issue{{Cause: "BOARD_FAULT"}, {Cause: "PSU_FAULT"}}
		got := moduleIssues(b, now, false)
		require.False(t, got.Complete)
		require.Empty(t, got.SourceTimes)
		require.Len(t, got.Items, 1)
		require.Equal(t, "PSU_FAULT", got.Items[0].Cause)
	})
	t.Run("shared budget includes clear source keys", func(t *testing.T) {
		budget := issueBudget{remaining: 64 + len("hashboard:left") + 64}
		got := moduleIssuesWithBudget(boards(), now, false, &budget)
		require.False(t, got.Complete)
		require.Empty(t, got.SourceTimes)
		require.Empty(t, got.Items)
		require.Zero(t, budget.remaining)
		require.Nil(t, moduleIssuesWithBudget(boards(), now, false, &budget), "exhausted evidence is absent")
	})
}

func TestSnapshotSharesIssueBudgetAcrossModules(t *testing.T) {
	now := time.Now().UTC()
	wire := MapResponse{ContainerMap: &ContainerMap{Tanks: []Tank{{ID: 1}}}}
	for moduleIndex := range 200 {
		module := Module{ID: int32(moduleIndex + 1)} //nolint:gosec // Test index is bounded above.
		for boardIndex := range boardsPerModule {
			board := &Board{MACAddress: fmt.Sprintf("board-%d-%d", moduleIndex, boardIndex), MiningState: miningStateMining, Health: &Health{Timestamp: now, Errors: []Issue{}, Warnings: []Issue{}, PSUThermal: &PSUThermal{ThermalStateValid: true}}}
			if boardIndex == 0 {
				for causeIndex := range maxModuleIssues {
					board.Health.Errors = append(board.Health.Errors, Issue{Cause: fmt.Sprintf("%03d", causeIndex) + strings.Repeat("x", maxIssueCauseBytes-3)})
				}
			}
			module.Slots = append(module.Slots, Slot{Occupied: true, Hashboard: board})
		}
		wire.ContainerMap.Tanks[0].Modules = append(wire.ContainerMap.Tanks[0].Modules, module)
	}
	got, err := snapshot(wire, now)
	require.NoError(t, err)
	require.Len(t, got.Tanks[0].Modules, 200)
	require.True(t, got.Tanks[0].Modules[0].Issues.Complete)
	require.Nil(t, got.Tanks[0].Modules[199].Issues)
	for _, module := range got.Tanks[0].Modules {
		if module.Issues != nil && !module.Issues.Complete {
			require.Empty(t, module.Issues.SourceTimes)
		}
		require.Equal(t, sdk.ModuleAttention, module.State)
	}
	// Measure the module issue budget independently of raw hashboard payloads.
	// SDK hashboard JSON embeds objects; protobuf carries those objects as bytes,
	// so the SDK snapshot itself is no longer interchangeable with protobuf JSON.
	issueSnapshot := *got
	issueSnapshot.Tanks = append([]sdk.ContainerTank(nil), got.Tanks...)
	for i := range issueSnapshot.Tanks {
		issueSnapshot.Tanks[i].Modules = append([]sdk.ContainerModule(nil), got.Tanks[i].Modules...)
		for j := range issueSnapshot.Tanks[i].Modules {
			issueSnapshot.Tanks[i].Modules[j].Hashboards = nil
		}
	}
	encoded, err := json.Marshal(&issueSnapshot)
	require.NoError(t, err)
	var container pb.ContainerSnapshot
	require.NoError(t, protojson.Unmarshal(encoded, &container))
	require.Less(t, proto.Size(&pb.DeviceMetrics{Container: &container}), 2<<20, "issue evidence leaves room below the plugin RPC receive limit")
}
