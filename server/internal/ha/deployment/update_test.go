package deployment

import (
	"net/http"
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

func TestRecoveryStartDoesNotRecreateRunningApplication(t *testing.T) {
	for _, test := range []struct {
		name       string
		running    int
		wantOption string
		wantNoop   bool
	}{
		{name: "both services running", running: 2, wantNoop: true},
		{name: "one service running", running: 1, wantOption: "--no-recreate"},
		{name: "services stopped", wantOption: "--force-recreate"},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Act
			args := applicationStartArgs("/release", true, test.running)

			// Assert
			if test.wantNoop {
				require.Nil(t, args)
				return
			}
			require.Contains(t, args, test.wantOption)
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
	ready, err := rollingUpdateApplicationReady(report, public, "v1.1.0")

	// Assert
	require.False(t, ready)
	require.ErrorContains(t, err, "became active")
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

func TestUpdatedPassivePeerReady(t *testing.T) {
	// Act and assert
	require.True(t, updatedPassivePeerReady(fleetHostStatus{reachable: true, passive: true, version: "v1.1.0"}, "v1.1.0"))
	require.False(t, updatedPassivePeerReady(fleetHostStatus{reachable: true, version: "v1.1.0"}, "v1.1.0"))
	require.False(t, updatedPassivePeerReady(fleetHostStatus{reachable: true, active: true, version: "v1.1.0"}, "v1.1.0"))
	require.False(t, updatedPassivePeerReady(fleetHostStatus{reachable: true, passive: true, version: "v1.0.0"}, "v1.1.0"))
}
