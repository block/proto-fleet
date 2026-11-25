package deployment

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

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

func healthyUpdateEtcd(context.Context, string) (EtcdReport, error) {
	return EtcdReport{Healthy: true}, nil
}

func healthyUpdateCapacity(context.Context, string) error { return nil }

func TestPrepareApplicationUpdateRejectsConnectionCapacityBeforeStaging(t *testing.T) {
	expected := errors.New("database capacity requires maintenance")
	err := prepareApplicationUpdate(t.Context(), "/missing-release", nil, installDependencies{}, nil, nil, healthyUpdateEtcd,
		func(_ context.Context, path string) error {
			require.Equal(t, filepath.Join(configRoot, "node.env"), path)
			return expected
		})
	require.ErrorIs(t, err, expected)
}

func TestPrepareApplicationUpdateRejectsEtcdBeforeStaging(t *testing.T) {
	probeErr := errors.New("observer credentials unavailable")
	for _, test := range []struct {
		name string
		err  error
		want string
	}{
		{name: "unhealthy etcd", want: "below 70% allocated quota"},
		{name: "probe error", err: probeErr, want: "observer credentials unavailable"},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Empty dependencies and a nonexistent release prove refusal happens
			// before staging images, validating Compose, or recording installation.
			err := prepareApplicationUpdate(t.Context(), filepath.Join(t.TempDir(), "missing-release"), nil, installDependencies{}, nil, nil,
				func(_ context.Context, envPath string) (EtcdReport, error) {
					require.Equal(t, filepath.Join(configRoot, "node.env"), envPath)
					return EtcdReport{}, test.err
				}, nil)
			require.ErrorContains(t, err, test.want)
			if test.err != nil {
				require.ErrorIs(t, err, test.err)
			}
		})
	}
}

func TestPrepareApplicationUpdateStopsBeforeImageLoadWhenComposeValidationFails(t *testing.T) {
	// Arrange
	root := testInstallRelease(t)
	composeErr := errors.New("invalid target Compose model")
	var commands []string
	deps := installDependencies{
		readFile: os.ReadFile,
		run: func(_ context.Context, name string, args ...string) ([]byte, error) {
			commands = append(commands, append([]string{name}, args...)...)
			return nil, nil
		},
	}

	// Act
	err := prepareApplicationUpdate(context.Background(), root, fleetApplicationProfile{"ENABLE_BETA_ALERTS": "true"}, deps, func(_ context.Context, _ []string) error {
		return composeErr
	}, nil, healthyUpdateEtcd, healthyUpdateCapacity)

	// Assert
	require.ErrorIs(t, err, composeErr)
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
			return []byte("sha256:api\nsha256:client\n"), nil
		},
	}

	// Act
	err := prepareApplicationUpdate(t.Context(), root, fleetApplicationProfile{"ENABLE_BETA_ALERTS": "true"}, deps, func(context.Context, []string) error { return nil }, testPreparedComposeImages, healthyUpdateEtcd, healthyUpdateCapacity)

	// Assert
	require.NoError(t, err)
	require.Contains(t, strings.Join(commands, "\n"), haActiveInstallMarker)
	proof, err := os.ReadFile(filepath.Join(root, preparedImagesFilename))
	require.NoError(t, err)
	require.JSONEq(t, `{"proto-fleet-api:v1.1.0":"sha256:api","proto-fleet-client:v1.1.0":"sha256:client"}`, string(proof))
	// Maintenance preflight precedes the manual reconciliation.
	require.NotContains(t, strings.Join(commands, "\n"), "fleet-db-transition")
}

func TestUpdateCompatibilityRejectsPreGrafanaProfile(t *testing.T) {
	path := filepath.Join(t.TempDir(), fleetEnvironmentFile)
	require.NoError(t, os.WriteFile(path, []byte(
		"AUTH_CLIENT_SECRET_KEY=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n"+
			"ENCRYPT_SERVICE_MASTER_KEY=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=\n"+
			"DB_DSN=postgresql://fleet:test@db/fleet\n",
	), 0o600))

	_, err := loadUpdateCompatibleProfile(path)

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
		{name: "space pressure during version mismatch", control: &ControlStatus{ControlReady: true, ReasonCodes: []ControlReasonCode{ReasonEtcdSpacePressure, ReasonFleetVersionMismatch}}},
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

func TestActiveUpdateRequiresExternalPublicPath(t *testing.T) {
	for _, test := range []struct {
		name      string
		status    int
		closed    bool
		wantError bool
	}{
		{name: "healthy", status: http.StatusOK},
		{name: "maintenance gate closed", status: http.StatusServiceUnavailable, wantError: true},
		{name: "unreachable", closed: true, wantError: true},
		{name: "redirect", status: http.StatusFound, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api-proxy/health/active" {
					w.WriteHeader(http.StatusNotFound)
					return
				}
				w.Header().Set("Location", "/login")
				w.WriteHeader(test.status)
			}))
			defer server.Close()
			if test.closed {
				server.Close()
			}
			err := requireActivePublicPath(t.Context(), NodeConfig{EndpointMode: "external", PublicURL: server.URL})
			if test.wantError {
				require.ErrorContains(t, err, "requires a healthy public endpoint")
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestActiveUpdatePublicPathLeavesVIPCheckToControlReadiness(t *testing.T) {
	// No additional endpoint probe is needed for VIP mode.
	require.NoError(t, requireActivePublicPath(t.Context(), NodeConfig{EndpointMode: "vip"}))
}

func TestApplicationSchemaCheckRefusesUnreconciledDatabase(t *testing.T) {
	expected := errors.New("database has not been reconciled")
	var got []string
	err := checkApplicationSchema(t.Context(), "/release", fleetApplicationProfile{}, func(_ context.Context, args []string) error {
		got = args
		return expected
	})
	require.ErrorIs(t, err, expected)
	require.Contains(t, strings.Join(got, " "), "run --rm --no-deps --entrypoint /app/fleet-db-transition fleet-api check --state target")
	require.NotContains(t, got, "fleet-client")
}

func TestStopApplicationAllowsBoundedCleanActiveShutdown(t *testing.T) {
	root := t.TempDir()
	environmentPath := filepath.Join(root, fleetEnvironmentFile)
	require.NoError(t, os.WriteFile(environmentPath, []byte("ENABLE_BETA_ALERTS=false\nENABLE_TRACING=false\n"), 0o600))
	for _, role := range []ha.RuntimeRole{ha.RoleActive, ha.RolePassive} {
		t.Run(string(role), func(t *testing.T) {
			// Arrange
			var commands [][]string
			var deadlines []time.Time
			runCompose := func(ctx context.Context, args []string) error {
				commands = append(commands, args)
				deadline, ok := ctx.Deadline()
				if role == ha.RoleActive {
					require.True(t, ok)
					require.InDelta(t, ha.UpdateActiveStopTimeout.Seconds(), time.Until(deadline).Seconds(), 1)
					deadlines = append(deadlines, deadline)
				} else {
					require.False(t, ok, "a passive stop keeps Compose's normal shutdown behavior")
				}
				if role == ha.RoleActive && len(commands) == 1 {
					// Simulate an API drain that used its full grace.
					time.Sleep(50 * time.Millisecond)
				}
				return nil
			}

			// Act
			err := stopApplicationContainers(t.Context(), root, environmentPath, filepath.Join(root, "ownership"), role, runCompose)

			// Assert
			require.NoError(t, err)
			if role == ha.RolePassive {
				require.Len(t, commands, 1)
				require.Equal(t, []string{"down", "--remove-orphans"}, commands[0][slices.Index(commands[0], "down"):])
				return
			}
			require.Len(t, commands, 2)
			require.Equal(t, []string{"stop", "--timeout", "3", "fleet-api"}, commands[0][slices.Index(commands[0], "stop"):],
				"stop the API before Compose down can stop its nginx dependent")
			require.Equal(t, []string{"down", "--timeout", "1", "--remove-orphans"}, commands[1][slices.Index(commands[1], "down"):])
			require.GreaterOrEqual(t, deadlines[1].Sub(deadlines[0]), 50*time.Millisecond, "teardown gets its own bound after the API stop")
		})
	}
}

func TestStopApplicationReportsFailedOrCancelledDrain(t *testing.T) {
	root := t.TempDir()
	environmentPath := filepath.Join(root, fleetEnvironmentFile)
	require.NoError(t, os.WriteFile(environmentPath, []byte("ENABLE_BETA_ALERTS=false\nENABLE_TRACING=false\n"), 0o600))
	stopErr := errors.New("compose stop failed")
	for _, test := range []struct {
		name      string
		failAt    int
		wantCalls int
	}{
		{name: "API stop fails", failAt: 1, wantCalls: 1},
		{name: "teardown fails", failAt: 2, wantCalls: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Arrange
			calls := 0
			runCompose := func(context.Context, []string) error {
				calls++
				if calls == test.failAt {
					return stopErr
				}
				return nil
			}

			// Act
			err := stopApplicationContainers(t.Context(), root, environmentPath, filepath.Join(root, "ownership"), ha.RoleActive, runCompose)

			// Assert
			require.ErrorIs(t, err, stopErr)
			require.Equal(t, test.wantCalls, calls, "failed API stop must not tear down nginx")
		})
	}
	t.Run("caller cancelled", func(t *testing.T) {
		// Arrange
		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		// Act
		err := stopApplicationContainers(ctx, root, environmentPath, filepath.Join(root, "ownership"), ha.RoleActive,
			func(ctx context.Context, _ []string) error { return ctx.Err() })

		// Assert
		require.ErrorIs(t, err, context.Canceled)
	})
}
