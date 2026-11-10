package poolapplications

import (
	"testing"

	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

func TestCheckBoardPoolsRequiresUnambiguousSettledMatchingSettings(t *testing.T) {
	expected := []poolconfigurations.PoolReading{{Priority: 0, URL: "stratum+tcp://pool.example", Username: "worker"}}
	for _, tc := range []struct {
		name, state string
		modify      func(*sdk.MiningGroupResult)
	}{
		{"unsupported", "applied_unverified", func(r *sdk.MiningGroupResult) { r.Supported = false }},
		{"custom policy", "different", func(r *sdk.MiningGroupResult) { r.Groups[0].CustomPolicy = true }},
		{"matching", "verified", func(*sdk.MiningGroupResult) {}},
		{"manual edit", "different", func(r *sdk.MiningGroupResult) { r.Groups[0].Pools[0].Username = "changed" }},
		{"pending", "applied_unverified", func(r *sdk.MiningGroupResult) { r.Pending = true }},
		{"disabled", "applied_unverified", func(r *sdk.MiningGroupResult) { r.Enabled = false }},
		{"suspended", "applied_unverified", func(r *sdk.MiningGroupResult) { r.Suspended = true }},
		{"missing board", "applied_unverified", func(r *sdk.MiningGroupResult) { r.Groups[0].MACs = nil }},
		{"duplicate membership", "applied_unverified", func(r *sdk.MiningGroupResult) { r.Groups = append(r.Groups, r.Groups[0]) }},
		{"disabled pool", "different", func(r *sdk.MiningGroupResult) { r.Groups[0].Pools[0].Enabled = false }},
		{"weighted pool", "different", func(r *sdk.MiningGroupResult) { r.Groups[0].Pools[0].ShareWeight = new(float64) }},
		{"priority differs", "different", func(r *sdk.MiningGroupResult) { r.Groups[0].Pools[0].Priority = 1 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := sdk.MiningGroupResult{Supported: true, Enabled: true, Groups: []sdk.MiningGroup{{MACs: []string{"mac"}, Pools: []sdk.MiningGroupPool{{URL: expected[0].URL, Username: expected[0].Username, Enabled: true}}}}}
			tc.modify(&r)
			state, _, _ := CheckBoardPools(r, "mac", expected)
			require.Equal(t, tc.state, state)
		})
	}
}

func TestReportedBoardPoolsPreservesSettingsForDisplay(t *testing.T) {
	observed := sdk.MiningGroupResult{Supported: true, Enabled: true, Groups: []sdk.MiningGroup{{MACs: []string{"board"}, CustomPolicy: true, Pools: []sdk.MiningGroupPool{
		{Priority: 1, URL: "backup", Username: "worker", Enabled: false, ShareWeight: new(float64)},
		{Priority: 0, URL: "primary", Username: "worker", Enabled: true},
	}}}}
	readback := reportedBoardPools(observed, "board")
	require.True(t, readback.Available, "custom and disabled settings remain visible even though they cannot verify a configuration")
	require.Equal(t, []poolconfigurations.PoolReading{{Priority: 0, URL: "primary", Username: "worker"}, {Priority: 1, URL: "backup", Username: "worker"}}, readback.Pools)
	observed.Groups = append(observed.Groups, observed.Groups[0])
	readback = reportedBoardPools(observed, "board")
	require.False(t, readback.Available, "ambiguous membership is not a reliable readback")
	state, _, _ := CheckBoardPools(observed, "board", readback.Pools)
	require.Equal(t, resultUnverified, state, "ambiguous membership cannot establish a reliable policy comparison")
}

func TestCheckBoardPoolsPreservesReadablePolicyMismatches(t *testing.T) {
	for _, policy := range []string{"custom", "weighted", "disabled"} {
		t.Run(policy, func(t *testing.T) {
			expected := []poolconfigurations.PoolReading{{Priority: 0, URL: "stratum+tcp://pool.example", Username: "worker"}}
			observed := sdk.MiningGroupResult{Supported: true, Enabled: true, Groups: []sdk.MiningGroup{{MACs: []string{"mac"}, Pools: []sdk.MiningGroupPool{{Priority: 0, URL: expected[0].URL, Username: expected[0].Username, Enabled: true}}}}}
			switch policy {
			case "custom":
				observed.Groups[0].CustomPolicy = true
			case "weighted":
				observed.Groups[0].Pools[0].ShareWeight = new(float64)
			case "disabled":
				observed.Groups[0].Pools[0].Enabled = false
			}
			state, code, reported := CheckBoardPools(observed, "mac", expected)
			require.Equal(t, resultDifferent, state)
			require.Equal(t, codeSettingsDiffer, code)
			require.Equal(t, expected, reported, "a policy mismatch must retain readable URLs and usernames")

			observed.Groups = append(observed.Groups, observed.Groups[0])
			state, code, reported = CheckBoardPools(observed, "mac", expected)
			require.Equal(t, resultUnverified, state, "ambiguous membership cannot supply reliable readback")
			require.Equal(t, codeReadbackUnavailable, code)
			require.Empty(t, reported)
		})
	}
}
