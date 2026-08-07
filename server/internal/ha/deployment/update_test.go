package deployment

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/ha"
)

func TestApplicationReadyRequiresAPIAndClientOnTargetRelease(t *testing.T) {
	for _, test := range []struct {
		name   string
		public fleetHostStatus
		want   bool
	}{
		{name: "ready", public: fleetHostStatus{reachable: true, version: "v1.1.0"}, want: true},
		{name: "client unavailable"},
		{name: "client on old release", public: fleetHostStatus{reachable: true, version: "v1.0.0"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Arrange
			runtime := ha.Status{Version: "v1.1.0", Role: ha.RolePassive, Observation: ha.ObservationCurrent}

			// Act
			ready := applicationReady(runtime, test.public, "v1.1.0", true)

			// Assert
			require.Equal(t, test.want, ready)
		})
	}
}
