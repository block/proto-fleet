package deployment

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/ha"
)

func TestApplicationReadyRequiresAPIAndClientOnTargetRelease(t *testing.T) {
	for _, test := range []struct {
		name    string
		runtime ha.Status
		public  fleetHostStatus
		want    bool
	}{
		{name: "passive ready", runtime: ha.Status{Version: "v1.1.0", Role: ha.RolePassive, Observation: ha.ObservationCurrent}, public: fleetHostStatus{reachable: true, passive: true, version: "v1.1.0"}, want: true},
		{name: "active takeover ready", runtime: ha.Status{Version: "v1.1.0", Role: ha.RoleActive, Observation: ha.ObservationCurrent}, public: fleetHostStatus{reachable: true, active: true, version: "v1.1.0"}, want: true},
		{name: "client unavailable"},
		{name: "client on old release", runtime: ha.Status{Version: "v1.1.0", Role: ha.RolePassive, Observation: ha.ObservationCurrent}, public: fleetHostStatus{reachable: true, passive: true, version: "v1.0.0"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Act
			ready := applicationReady(test.runtime, test.public, "v1.1.0")

			// Assert
			require.Equal(t, test.want, ready)
		})
	}
}

func TestRollingUpdateApplicationRejectsActiveTakeover(t *testing.T) {
	// Arrange
	report := StatusReport{
		Runtime: ha.Status{Version: "v1.1.0", Role: ha.RoleActive, Observation: ha.ObservationCurrent},
		Control: &ControlStatus{ControlReady: true, FailoverReady: true},
	}
	public := fleetHostStatus{reachable: true, active: true, version: "v1.1.0"}

	// Act
	ready, err := rollingUpdateApplicationReady(report, public, "v1.1.0", false)

	// Assert
	require.False(t, ready)
	require.ErrorContains(t, err, "became active")
}

func TestApplicationConvergenceRequiresExpectedRuntimeRole(t *testing.T) {
	for _, test := range []struct {
		name           string
		runtime        ha.Status
		requirePassive bool
		want           bool
	}{
		{name: "expected passive", runtime: ha.Status{Version: "v1.1.0", Role: ha.RolePassive, Observation: ha.ObservationCurrent}, requirePassive: true, want: true},
		{name: "active passive update", runtime: ha.Status{Version: "v1.1.0", Role: ha.RoleActive, Observation: ha.ObservationCurrent}, requirePassive: true},
		{name: "active recovery", runtime: ha.Status{Version: "v1.1.0", Role: ha.RoleActive, Observation: ha.ObservationCurrent}, want: true},
		{name: "target version role is initializing", runtime: ha.Status{Version: "v1.1.0", Role: ha.RoleInitializing, Observation: ha.ObservationUnavailable}, requirePassive: true, want: true},
		{name: "wrong version", runtime: ha.Status{Version: "v1.0.0", Role: ha.RolePassive, Observation: ha.ObservationCurrent}},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Act
			got := applicationMayConverge(test.runtime, "v1.1.0", test.requirePassive)

			// Assert
			require.Equal(t, test.want, got)
		})
	}
}

func TestPrepareApplicationUpdateStopsBeforeImageLoadWhenComposeValidationFails(t *testing.T) {
	// Arrange
	root := testInstallRelease(t)
	composeErr := errors.New("invalid target Compose model")
	var composeArgs []string
	var commands []string
	deps := installDependencies{
		readFile: os.ReadFile,
		run: func(_ context.Context, name string, args ...string) ([]byte, error) {
			commands = append(commands, append([]string{name}, args...)...)
			return nil, nil
		},
	}

	// Act
	err := prepareApplicationUpdate(context.Background(), root, deps, func(_ context.Context, args []string) error {
		composeArgs = append([]string(nil), args...)
		return composeErr
	})

	// Assert
	require.ErrorIs(t, err, composeErr)
	require.Equal(t, []string{
		"--project-name", "deployment",
		"--env-file", filepath.Join(configRoot, "base.env"),
		"--env-file", filepath.Join(configRoot, fleetEnvironmentFile),
		"--env-file", filepath.Join(configRoot, "node.env"),
		"--file", filepath.Join(root, "docker-compose.yaml"),
		"--file", filepath.Join(root, "docker-compose.alerts.yaml"),
		"--file", filepath.Join(root, "ha", "fleet-compose.yaml"),
		"config", "--quiet", "fleet-api", "fleet-client", "grafana",
	}, composeArgs)
	require.Empty(t, commands)
}

func TestPrepareApplicationUpdateRecordsActiveInstallForExistingHADeployment(t *testing.T) {
	// Arrange
	root := testInstallRelease(t)
	var commands []string
	deps := installDependencies{
		readFile: os.ReadFile,
		run: func(_ context.Context, name string, args ...string) ([]byte, error) {
			commands = append(commands, strings.Join(append([]string{name}, args...), " "))
			return nil, nil
		},
	}

	// Act
	err := prepareApplicationUpdate(t.Context(), root, deps, func(context.Context, []string) error { return nil })

	// Assert
	require.NoError(t, err)
	require.Contains(t, strings.Join(commands, "\n"), haActiveInstallMarker)
}

func TestUpdateCompatibilityRejectsPreGrafanaProfile(t *testing.T) {
	path := filepath.Join(t.TempDir(), fleetEnvironmentFile)
	require.NoError(t, os.WriteFile(path, []byte(
		"AUTH_CLIENT_SECRET_KEY=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n"+
			"ENCRYPT_SERVICE_MASTER_KEY=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=\n"+
			"DB_DSN=postgresql://fleet:test@db/fleet\n",
	), 0o600))

	err := requireUpdateCompatibleProfile(path)

	require.ErrorContains(t, err, "reinstall both database hosts")
}

func TestRecoveryAcceptsHealthyActiveApplication(t *testing.T) {
	// Arrange
	report := StatusReport{
		Runtime: ha.Status{Version: "v1.1.0", Role: ha.RoleActive, Observation: ha.ObservationCurrent},
		Control: &ControlStatus{ControlReady: true},
	}
	public := fleetHostStatus{reachable: true, active: true, version: "v1.1.0"}

	// Act
	ready, err := updatedApplicationReady(report, public, "v1.1.0", false, false)

	// Assert
	require.NoError(t, err)
	require.True(t, ready)
}

func TestApplicationConvergenceRequiresExpectedVersionAndRole(t *testing.T) {
	for _, test := range []struct {
		name           string
		runtime        ha.Status
		requirePassive bool
		want           bool
	}{
		{name: "active recovery", runtime: ha.Status{Version: "v1.1.0", Role: ha.RoleActive, Observation: ha.ObservationCurrent}, want: true},
		{name: "active passive-update", runtime: ha.Status{Version: "v1.1.0", Role: ha.RoleActive, Observation: ha.ObservationCurrent}, requirePassive: true},
		{name: "wrong version", runtime: ha.Status{Version: "v1.0.0", Role: ha.RolePassive, Observation: ha.ObservationCurrent}},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Act
			got := applicationMayConverge(test.runtime, "v1.1.0", test.requirePassive)

			// Assert
			require.Equal(t, test.want, got)
		})
	}
}

func TestCompletedUpdateRequiresFullFailoverReadiness(t *testing.T) {
	// Arrange
	report := StatusReport{
		Runtime: ha.Status{Version: "v1.1.0", Role: ha.RolePassive, Observation: ha.ObservationCurrent},
		Control: &ControlStatus{ControlReady: true, ReasonCodes: []ControlReasonCode{ReasonFleetVersionMismatch}},
	}
	public := fleetHostStatus{reachable: true, passive: true, version: "v1.1.0"}

	// Act
	ready, err := rollingUpdateApplicationReady(report, public, "v1.1.0", true)

	// Assert
	require.NoError(t, err)
	require.False(t, ready)
}

func TestRollingUpdateControlAllowsOnlyExpectedVersionMismatch(t *testing.T) {
	for _, test := range []struct {
		name    string
		control *ControlStatus
		want    bool
	}{
		{name: "fully ready", control: &ControlStatus{ControlReady: true, FailoverReady: true}, want: true},
		{name: "version mismatch", control: &ControlStatus{ControlReady: true, ReasonCodes: []ControlReasonCode{ReasonFleetVersionMismatch}}, want: true},
		{name: "database redundancy degraded", control: &ControlStatus{ControlReady: true, ReasonCodes: []ControlReasonCode{ReasonFleetVersionMismatch, ReasonDatabaseRedundancyDegraded}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Act
			ready := rollingUpdateControlReady(test.control)

			// Assert
			require.Equal(t, test.want, ready)
		})
	}
}

func TestAcceptVIPVersionRejectsWrongPeerRelease(t *testing.T) {
	// Act
	ready, err := acceptVIPVersion(http.StatusOK, "v1.0.0", "v1.1.0")

	// Assert
	require.False(t, ready)
	require.ErrorContains(t, err, "v1.0.0")
}

func TestAcceptVIPVersionWaitsForActivePeer(t *testing.T) {
	// Act
	ready, err := acceptVIPVersion(http.StatusServiceUnavailable, "v1.1.0", "v1.1.0")

	// Assert
	require.NoError(t, err)
	require.False(t, ready)
}

func TestUpdatedPassivePeerReady(t *testing.T) {
	// Act and assert
	require.True(t, updatedPassivePeerReady(fleetHostStatus{reachable: true, passive: true, version: "v1.1.0"}, "v1.1.0"))
	require.False(t, updatedPassivePeerReady(fleetHostStatus{reachable: true, version: "v1.1.0"}, "v1.1.0"))
	require.False(t, updatedPassivePeerReady(fleetHostStatus{reachable: true, active: true, version: "v1.1.0"}, "v1.1.0"))
	require.False(t, updatedPassivePeerReady(fleetHostStatus{reachable: true, passive: true, version: "v1.0.0"}, "v1.1.0"))
}
