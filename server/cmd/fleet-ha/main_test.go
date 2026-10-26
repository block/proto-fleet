package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/alecthomas/kong"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/ha"
	"github.com/block/proto-fleet/server/internal/ha/deployment"
	"github.com/block/proto-fleet/server/internal/updaterapi"
)

type fakeUpdaterClient struct {
	triggered   bool
	complete    bool
	prepareOnly bool
	prepared    bool
	triggerErr  error
	operation   updaterapi.Operation
}

func TestExternalCommandsParseWithoutInteractiveInput(t *testing.T) {
	var parsed cli
	parser, err := kong.New(&parsed, kong.Vars{"default_node_env": defaultNodeEnv, "default_firewall_template": defaultFirewallTemplate})
	require.NoError(t, err)
	_, err = parser.Parse([]string{"prepare-external", "/secure/cluster", "--database-a", "10.0.1.10", "--database-b", "10.0.2.10", "--witness", "10.0.3.10", "--public-url", "https://fleet.example.com"})
	require.NoError(t, err)
	require.Equal(t, "https://fleet.example.com", parsed.PrepareExternal.PublicURL)
	_, err = parser.Parse([]string{"install-prepared", "/secure/ha-a/node.env", "--etcd-root-password-file", "/secure/root-password"})
	require.NoError(t, err)
	require.Equal(t, "/secure/ha-a/node.env", parsed.InstallPrepared.NodeEnv)
	require.Equal(t, "/secure/root-password", parsed.InstallPrepared.EtcdRootPasswordFile)
}

func TestResetPasswordCommandParsesStdinFlag(t *testing.T) {
	var parsed cli
	parser, err := kong.New(&parsed, kong.Vars{
		"default_node_env":          defaultNodeEnv,
		"default_firewall_template": defaultFirewallTemplate,
	})
	require.NoError(t, err)

	ctx, err := parser.Parse([]string{"reset-password", "--password-stdin"})

	require.NoError(t, err)
	require.Equal(t, "reset-password", ctx.Command())
	require.True(t, parsed.ResetPassword.PasswordStdin)
}

func TestRunResetPasswordGeneratesOnHostAndUsesContainerStdin(t *testing.T) {
	var output bytes.Buffer
	var containerInput string

	err := runResetPassword(
		t.Context(),
		false,
		strings.NewReader("ignored"),
		&output,
		func() (string, error) { return "generated-secret", nil },
		func(_ context.Context, input io.Reader) error {
			contents, err := io.ReadAll(input)
			require.NoError(t, err)
			containerInput = string(contents)
			return nil
		},
	)

	require.NoError(t, err)
	require.Equal(t, "generated-secret\n", containerInput)
	require.Equal(t, "Temporary password: generated-secret\n", output.String())
}

func TestRunResetPasswordDoesNotPrintGeneratedPasswordWhenResetFails(t *testing.T) {
	var output bytes.Buffer

	err := runResetPassword(
		t.Context(),
		false,
		strings.NewReader(""),
		&output,
		func() (string, error) { return "generated-secret", nil },
		func(context.Context, io.Reader) error { return errors.New("reset failed") },
	)

	require.ErrorContains(t, err, "reset failed")
	require.Empty(t, output.String())
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("ssh session closed") }

func TestRunResetPasswordReportsCommittedResetWhenDeliveryFails(t *testing.T) {
	err := runResetPassword(
		t.Context(),
		false,
		strings.NewReader("ignored"),
		failingWriter{},
		func() (string, error) { return "generated-secret", nil },
		func(context.Context, io.Reader) error { return nil },
	)

	require.ErrorContains(t, err, "already committed")
	require.ErrorContains(t, err, "rerun recovery")
	require.ErrorContains(t, err, "ssh session closed")
}

func TestRunResetPasswordForwardsSuppliedStdinWithoutHostOutput(t *testing.T) {
	var output bytes.Buffer
	supplied := strings.NewReader("supplied-secret\n")
	generated := false

	err := runResetPassword(
		t.Context(),
		true,
		supplied,
		&output,
		func() (string, error) {
			generated = true
			return "", nil
		},
		func(_ context.Context, input io.Reader) error {
			require.Same(t, supplied, input)
			return nil
		},
	)

	require.NoError(t, err)
	require.False(t, generated)
	require.Empty(t, output.String())
}

func (f *fakeUpdaterClient) TriggerComplete(_ context.Context, operationID, targetVersion string) (updaterapi.Operation, error) {
	f.triggered = true
	f.complete = true
	if f.triggerErr != nil {
		return updaterapi.Operation{}, f.triggerErr
	}
	return updaterapi.Operation{ID: operationID, TargetVersion: targetVersion, Phase: updaterapi.PhaseSucceeded}, nil
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
	err := runUpdate(t.Context(), "v1.2.3", false, &bytes.Buffer{}, func(context.Context, string, string, bool) error {
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
	err := runUpdate(t.Context(), "v1.2.3", false, &output, func(context.Context, string, string, bool) error { return nil }, client)

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
	err := runPassiveUpdate(t.Context(), "v1.2.3", false, &output, func(context.Context, string, string, bool) error { return nil }, client, read)

	// Assert
	require.ErrorContains(t, err, "failover readiness is degraded")
	require.Contains(t, output.String(), "failover redundancy is degraded")
}

func TestPassiveUpdateAllowsExpectedVersionMismatch(t *testing.T) {
	// Arrange
	client := &fakeUpdaterClient{}
	read := func(context.Context, string) (deployment.StatusReport, error) {
		return deployment.StatusReport{Control: &deployment.ControlStatus{
			ControlReady: true,
			ReasonCodes:  []deployment.ControlReasonCode{deployment.ReasonFleetVersionMismatch},
		}}, nil
	}

	// Act
	err := runPassiveUpdate(t.Context(), "v1.2.3", false, &bytes.Buffer{}, func(context.Context, string, string, bool) error { return nil }, client, read)

	// Assert
	require.NoError(t, err)
}

func TestCompleteUpdateRejectsExpectedVersionMismatch(t *testing.T) {
	// Arrange
	client := &fakeUpdaterClient{}
	read := func(context.Context, string) (deployment.StatusReport, error) {
		return deployment.StatusReport{Control: &deployment.ControlStatus{
			ControlReady: true,
			ReasonCodes:  []deployment.ControlReasonCode{deployment.ReasonFleetVersionMismatch},
		}}, nil
	}

	// Act
	err := runPassiveUpdate(t.Context(), "v1.2.3", true, &bytes.Buffer{}, func(context.Context, string, string, bool) error { return nil }, client, read)

	// Assert
	require.ErrorContains(t, err, "failover readiness is degraded")
}

func TestUpdateReturnsWhenUpdaterIsUnavailable(t *testing.T) {
	// Arrange
	client := &fakeUpdaterClient{triggerErr: updaterapi.ErrUnavailable}

	// Act
	err := runUpdate(t.Context(), "v1.2.3", false, &bytes.Buffer{}, func(context.Context, string, string, bool) error { return nil }, client)

	// Assert
	require.ErrorIs(t, err, updaterapi.ErrUnavailable)
}

func TestTriggerUpdateReconcilesAfterAmbiguousSubmission(t *testing.T) {
	// Arrange
	ctx, cancel := context.WithCancel(t.Context())
	calls := 0
	trigger := func(context.Context, string, string) (updaterapi.Operation, error) {
		calls++
		if calls == 1 {
			return updaterapi.Operation{}, &updaterapi.TransportError{Cause: errors.New("response lost")}
		}
		cancel()
		return updaterapi.Operation{}, updaterapi.ErrUnavailable
	}

	// Act
	_, err := triggerUpdate(ctx, "operation-123", "v1.2.3", trigger)

	// Assert
	require.Equal(t, 2, calls)
	require.ErrorContains(t, err, "operation-123")
	require.ErrorContains(t, err, "may have been accepted and may continue")
}

func TestUpdateFailureIncludesRecoveryDetails(t *testing.T) {
	// Arrange
	client := &fakeUpdaterClient{operation: updaterapi.Operation{
		Phase: updaterapi.PhaseFailed, Error: "startup failed",
		RecoveryCommand: "fleet-ha app-start v1.2.3", LogPath: "/var/log/proto-fleet-updater/update.log",
	}}
	var output bytes.Buffer

	// Act
	err := runUpdate(t.Context(), "v1.2.3", false, &output, func(context.Context, string, string, bool) error { return nil }, client)

	// Assert
	require.ErrorContains(t, err, "Recovery: fleet-ha app-start v1.2.3")
	require.ErrorContains(t, err, "Log: /var/log/proto-fleet-updater/update.log")
	require.Contains(t, output.String(), "Update operation")
}

func TestCompleteUpdateRequiresActiveAndUsesCompletionRequest(t *testing.T) {
	// Arrange
	client := &fakeUpdaterClient{}
	activeChecked := false

	// Act
	err := runUpdate(
		t.Context(), "v1.2.3", true, &bytes.Buffer{},
		func(_ context.Context, _, _ string, complete bool) error {
			activeChecked = complete
			return nil
		},
		client,
	)

	// Assert
	require.NoError(t, err)
	require.True(t, activeChecked)
	require.True(t, client.complete)
}

func (f *fakeUpdaterClient) Prepare(ctx context.Context, id, version string) (updaterapi.Operation, error) {
	f.prepareOnly = true
	operation, err := f.Trigger(ctx, id, version)
	operation.PrepareOnly = true
	return operation, err
}
func (f *fakeUpdaterClient) TriggerPrepared(ctx context.Context, id, version string, complete bool) (updaterapi.Operation, error) {
	f.prepared = true
	f.complete = complete
	operation, err := f.Trigger(ctx, id, version)
	operation.Prepared = true
	operation.Complete = complete
	return operation, err
}

func TestPreparedUpdateFlagsParse(t *testing.T) {
	for _, args := range [][]string{
		{"update", "--capabilities"},
		{"update", "v1.2.3", "--prepare-only"},
		{"update", "v1.2.3", "--prepared"},
		{"update", "v1.2.3", "--prepared", "--complete"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			// Arrange
			var parsed cli
			parser, err := kong.New(&parsed, kong.Vars{"default_node_env": defaultNodeEnv, "default_firewall_template": defaultFirewallTemplate})
			require.NoError(t, err)

			// Act
			_, err = parser.Parse(args)

			// Assert
			require.NoError(t, err)
			require.Equal(t, args[1] == "--capabilities", parsed.Update.Capabilities)
			if len(args) > 2 {
				require.Equal(t, "v1.2.3", parsed.Update.Version)
			}
			require.Equal(t, slices.Contains(args, "--prepare-only"), parsed.Update.PrepareOnly)
			require.Equal(t, slices.Contains(args, "--prepared"), parsed.Update.Prepared)
			require.Equal(t, slices.Contains(args, "--complete"), parsed.Update.Complete)
		})
	}
}

func TestUpdateCapabilitiesOutput(t *testing.T) {
	// Arrange
	var output bytes.Buffer

	// Act
	err := writeUpdateCapabilities(&output)

	// Assert
	require.NoError(t, err)
	require.JSONEq(t, `{"capabilities":["prepared-update-v1"]}`, output.String())
	require.True(t, strings.HasSuffix(output.String(), "\n"))
}

func TestUpdateCommandRejectsInvalidFlagCombinations(t *testing.T) {
	for _, tc := range []struct {
		name    string
		command updateCmd
		wantErr string
	}{
		{name: "capabilities with version", command: updateCmd{Capabilities: true, Version: "v1.2.3"}, wantErr: "--capabilities cannot be combined"},
		{name: "capabilities with complete", command: updateCmd{Capabilities: true, Complete: true}, wantErr: "--capabilities cannot be combined"},
		{name: "capabilities with prepare-only", command: updateCmd{Capabilities: true, PrepareOnly: true}, wantErr: "--capabilities cannot be combined"},
		{name: "capabilities with prepared", command: updateCmd{Capabilities: true, Prepared: true}, wantErr: "--capabilities cannot be combined"},
		{name: "missing version", command: updateCmd{}, wantErr: "target application version is required"},
		{name: "prepare-only with prepared", command: updateCmd{Version: "v1.2.3", PrepareOnly: true, Prepared: true}, wantErr: "--prepare-only cannot be combined"},
		{name: "prepare-only with complete", command: updateCmd{Version: "v1.2.3", PrepareOnly: true, Complete: true}, wantErr: "--prepare-only cannot be combined"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			command := tc.command

			// Act
			err := command.Run(t.Context())

			// Assert
			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}

func TestRunUpdateModeDispatchesToRequestedUpdaterOperation(t *testing.T) {
	for _, tc := range []struct {
		name                            string
		complete, prepareOnly, prepared bool
		wantComplete                    bool
	}{
		{name: "prepare only", prepareOnly: true},
		{name: "prepared", prepared: true},
		{name: "prepared complete", prepared: true, complete: true, wantComplete: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			client := &fakeUpdaterClient{}
			var preflightComplete bool

			// Act
			err := runUpdateMode(t.Context(), "v1.2.3", tc.complete, tc.prepareOnly, tc.prepared, &bytes.Buffer{},
				func(_ context.Context, _, _ string, complete bool) error {
					preflightComplete = complete
					return nil
				}, client)

			// Assert
			require.NoError(t, err)
			require.Equal(t, tc.prepareOnly, client.prepareOnly)
			require.Equal(t, tc.prepared, client.prepared)
			require.Equal(t, tc.wantComplete, client.complete)
			require.Equal(t, tc.complete, preflightComplete)
		})
	}
}
