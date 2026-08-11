package main

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/ha"
	"github.com/block/proto-fleet/server/internal/ha/deployment"
)

func TestStatusRequiresFailoverReadiness(t *testing.T) {
	// Arrange
	read := func(context.Context, string) (deployment.StatusReport, error) {
		return deployment.StatusReport{
			Runtime: ha.Status{Role: ha.RolePassive, Observation: ha.ObservationCurrent, Endpoint: ha.EndpointNotApplicable},
			Control: &deployment.ControlStatus{ControlReady: true},
		}, nil
	}

	// Act
	err := runStatus(t.Context(), "custom.env", &bytes.Buffer{}, read)

	// Assert
	require.ErrorContains(t, err, "failover readiness")
}

func TestStatusPrintsRedactedContract(t *testing.T) {
	// Arrange
	var output bytes.Buffer
	read := func(_ context.Context, envPath string) (deployment.StatusReport, error) {
		require.Equal(t, "node.env", envPath)
		return deployment.StatusReport{
			Runtime: ha.Status{Version: "v1.2.3", Role: ha.RoleActive, Observation: ha.ObservationCurrent, Endpoint: ha.EndpointHealthy},
			Control: &deployment.ControlStatus{ControlReady: true, FailoverReady: true},
		}, nil
	}

	// Act
	err := runStatus(t.Context(), defaultNodeEnv, &output, read)

	// Assert
	require.NoError(t, err)
	require.Contains(t, output.String(), `"control_ready": true`)
	require.Contains(t, output.String(), `"failover_ready": true`)
	require.NotContains(t, output.String(), "holder")
	require.NotContains(t, output.String(), "token")
}
