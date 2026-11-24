package deployment

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRuntimeMetricsProfile(t *testing.T) {
	for _, value := range []string{"true", "false"} {
		profile, err := fleetApplicationProfileFromValues(map[string]string{"FLEET_RUNTIME_METRICS_ENABLED": value}, false)
		require.NoError(t, err)
		require.Equal(t, value, profile["FLEET_RUNTIME_METRICS_ENABLED"])
	}
	_, err := fleetApplicationProfileFromValues(map[string]string{"FLEET_RUNTIME_METRICS_ENABLED": "not-a-bool"}, false)
	require.Error(t, err)
}
