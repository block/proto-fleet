package deployment

import (
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
