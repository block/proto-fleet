package timescaledb

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	modelsV2 "github.com/block/proto-fleet/server/internal/domain/telemetry/models/v2"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

// Release channels verify a container's PSU firmware from this stored
// observation, kept beside the hashboard firmware observation.
func TestContainerInventoryKeepsThePSUFirmwareObservation(t *testing.T) {
	r := newHashboardReviewRig(t)
	observed := func() (version string, mixed bool, hashboards string) {
		t.Helper()
		require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT i.psu_firmware_version, i.psu_firmware_mixed, i.hashboard_firmware_version
			FROM container_inventory i JOIN device d ON d.id = i.device_id WHERE d.device_identifier = $1`, r.deviceID).Scan(&version, &mixed, &hashboards))
		return version, mixed, hashboards
	}
	store := func(offset time.Duration, version string, mixed bool) {
		t.Helper()
		at := r.base.Add(offset)
		board := reviewBoard("aa:bb:cc:dd:ee:01", "board-a")
		board.ObservedAt, board.StateObservedAt = at, at
		snapshot := &sdk.ContainerSnapshot{
			ObservedAt: at, TopologyComplete: true, HashboardFirmwareVersion: "2.0.0", PSUFirmwareVersion: version, PSUFirmwareMixed: mixed,
			Tanks: []sdk.ContainerTank{{Position: 1, Modules: []sdk.ContainerModule{{
				Position: 1, ReportedID: "psu-0", State: sdk.MinerStatusActive, ObservedAt: at, Hashboards: []sdk.ContainerHashboard{board},
			}}}},
		}
		require.NoError(t, r.store.StoreDeviceMetrics(t.Context(), modelsV2.DeviceMetrics{
			DeviceIdentifier: r.deviceID, Timestamp: at, Health: modelsV2.HealthHealthyActive, Container: snapshot,
		}))
	}

	store(time.Second, "app=1310;bl=1110", false)
	version, mixed, hashboards := observed()
	require.Equal(t, "app=1310;bl=1110", version)
	require.False(t, mixed)
	require.Equal(t, "2.0.0", hashboards)

	store(2*time.Second, "", true)
	version, mixed, _ = observed()
	require.Empty(t, version)
	require.True(t, mixed, "a later inventory replaces the observation")

	store(3*time.Second, "", false)
	version, mixed, hashboards = observed()
	require.Empty(t, version, "an inventory that cannot name a version clears the earlier one")
	require.False(t, mixed)
	require.Equal(t, "2.0.0", hashboards, "the hashboard observation is independent")

	store(time.Second, "app=1410;bl=1110", false)
	version, _, _ = observed()
	require.Empty(t, version, "an older inventory does not replace a newer observation")
}
