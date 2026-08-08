package main

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/ha"
	"github.com/block/proto-fleet/server/internal/ha/deployment"
	"github.com/block/proto-fleet/server/internal/updaterapi"
)

type fakeUpdaterClient struct {
	triggered  bool
	triggerErr error
	operation  updaterapi.Operation
}

func (f *fakeUpdaterClient) Status(context.Context) (updaterapi.StatusResponse, error) {
	return updaterapi.StatusResponse{}, nil
}

func (f *fakeUpdaterClient) Trigger(_ context.Context, operationID, targetVersion string) (updaterapi.Operation, error) {
	f.triggered = true
	if f.triggerErr != nil {
		return updaterapi.Operation{}, f.triggerErr
	}
	if f.operation.Phase != "" {
		f.operation.ID = operationID
		f.operation.TargetVersion = targetVersion
		return f.operation, nil
	}
	return updaterapi.Operation{ID: operationID, TargetVersion: targetVersion, Phase: updaterapi.PhaseSucceeded}, nil
}

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

func TestUpdateRequiresPassiveBeforeTriggering(t *testing.T) {
	// Arrange
	client := &fakeUpdaterClient{}

	// Act
	err := runUpdate(t.Context(), "v1.2.3", &bytes.Buffer{}, func(context.Context, string, string) error {
		return errors.New("local Fleet is active")
	}, client)

	// Assert
	require.ErrorContains(t, err, "active")
	require.False(t, client.triggered)
}

func TestUpdateReportsTerminalSuccess(t *testing.T) {
	// Arrange
	client := &fakeUpdaterClient{operation: updaterapi.Operation{
		Phase:   updaterapi.PhaseSucceeded,
		Message: "Fleet v1.2.3 is running; host updater refresh needs attention",
		LogPath: "/var/log/proto-fleet-updater/update.log",
	}}
	var output bytes.Buffer

	// Act
	err := runUpdate(t.Context(), "v1.2.3", &output, func(context.Context, string, string) error { return nil }, client)

	// Assert
	require.NoError(t, err)
	require.True(t, client.triggered)
	require.Contains(t, output.String(), "v1.2.3: succeeded")
	require.Contains(t, output.String(), "host updater refresh needs attention")
	require.Contains(t, output.String(), "Log: /var/log/proto-fleet-updater/update.log")
}

func TestPassiveUpdateReportsDegradedFailoverReadiness(t *testing.T) {
	// Arrange
	client := &fakeUpdaterClient{}
	var output bytes.Buffer
	read := func(context.Context, string) (deployment.StatusReport, error) {
		return deployment.StatusReport{
			Runtime: ha.Status{Role: ha.RolePassive},
			Control: &deployment.ControlStatus{},
		}, nil
	}

	// Act
	err := runPassiveUpdate(t.Context(), "v1.2.3", &output, func(context.Context, string, string) error { return nil }, client, read)

	// Assert
	require.NoError(t, err)
	require.Contains(t, output.String(), "failover redundancy is degraded")
}

func TestUpdateReturnsWhenUpdaterIsUnavailable(t *testing.T) {
	// Arrange
	client := &fakeUpdaterClient{triggerErr: updaterapi.ErrUnavailable}

	// Act
	err := runUpdate(t.Context(), "v1.2.3", &bytes.Buffer{}, func(context.Context, string, string) error { return nil }, client)

	// Assert
	require.ErrorIs(t, err, updaterapi.ErrUnavailable)
}

func TestUpdateFailureIncludesRecoveryDetails(t *testing.T) {
	// Arrange
	client := &fakeUpdaterClient{operation: updaterapi.Operation{
		Phase: updaterapi.PhaseFailed, Error: "startup failed",
		RecoveryCommand: "fleet-ha app-start v1.2.3", LogPath: "/var/log/proto-fleet-updater/update.log",
	}}
	var output bytes.Buffer

	// Act
	err := runUpdate(t.Context(), "v1.2.3", &output, func(context.Context, string, string) error { return nil }, client)

	// Assert
	require.ErrorContains(t, err, "Recovery: fleet-ha app-start v1.2.3")
	require.ErrorContains(t, err, "Log: /var/log/proto-fleet-updater/update.log")
	require.Contains(t, output.String(), "Update operation")
}
