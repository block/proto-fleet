package sdk

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestContainerMiningRequestRejectsIgnoredFieldsAndInvalidPower(t *testing.T) {
	for _, r := range []ContainerMiningRequest{
		{Action: "status"}, {Action: "power_target"},
		{Action: "set_mining", Mining: new(false)},
		{Action: "set_power_target", Power: &ContainerPowerTarget{Watts: 100000, PerformanceMode: "PERFORMANCE_MODE_EFFICIENCY", BalanceBays: new(false)}},
	} {
		require.NoError(t, r.Validate())
	}
	for _, r := range []ContainerMiningRequest{
		{}, {Action: "start"}, {Action: "set_mining"},
		{Action: "status", Mining: new(true)},
		{Action: "power_target", Power: &ContainerPowerTarget{Watts: 5}},
		{Action: "set_mining", Mining: new(true), Power: &ContainerPowerTarget{Watts: 5}},
		{Action: "set_power_target", Power: &ContainerPowerTarget{Watts: 0}},
		{Action: "set_power_target", Power: &ContainerPowerTarget{Watts: math.Inf(1)}},
		{Action: "set_power_target", Power: &ContainerPowerTarget{Watts: math.NaN()}},
		{Action: "set_power_target", Power: &ContainerPowerTarget{Watts: 5, PerformanceMode: "unknown"}},
	} {
		require.Error(t, r.Validate(), "%+v", r)
	}
}

func TestContainerMiningResultsRequireExplicitObservation(t *testing.T) {
	for _, tc := range []struct{ action, payload string }{
		{"status", `{}`}, {"status", `{"status":{}}`}, {"status", `{"status":{"mining":null}}`},
		{"power_target", `{"power":{}}`}, {"power_target", `{"power":{"watts":null}}`},
	} {
		_, err := DecodeContainerMiningResult([]byte(tc.payload), ContainerMiningRequest{Action: tc.action})
		require.Error(t, err)
	}
	_, err := DecodeContainerMiningResult([]byte(`{"status":{"mining":false}}`), ContainerMiningRequest{Action: "status"})
	require.NoError(t, err)
	_, err = DecodeContainerMiningResult([]byte(`{"power":{"watts":0}}`), ContainerMiningRequest{Action: "power_target"})
	require.NoError(t, err)
}

func TestContainerMiningWriteResults(t *testing.T) {
	for _, action := range []string{"set_mining", "set_power_target"} {
		t.Run(action, func(t *testing.T) {
			request := ContainerMiningRequest{Action: action}
			result, err := DecodeContainerMiningResult([]byte(`{}`), request)
			require.NoError(t, err)
			require.Equal(t, ContainerMiningResult{}, result)
			for _, payload := range []string{
				`{"status":{"mining":false}}`, `{"power":{"watts":0}}`,
				`{"unknown":true}`, `{`, `{} {}`,
			} {
				_, err := DecodeContainerMiningResult([]byte(payload), request)
				require.Error(t, err, payload)
			}
		})
	}
}
