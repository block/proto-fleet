package rollout

import (
	"database/sql"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/infrastructure/files"
	"github.com/stretchr/testify/require"
)

func TestCT1RolloutConvergesOnHashboardsNotController(t *testing.T) {
	f := newFixture(t, 0)
	id := f.addMiner(t, "ct1", "CT1")
	_, err := f.conn.ExecContext(t.Context(), `UPDATE discovered_device SET device_class=2,firmware_version='2.0.0' WHERE device_identifier='ct1'`)
	require.NoError(t, err)
	f.files.metadata = map[string]files.FirmwareMetadata{}
	f.files.metadata["fw-2"] = files.FirmwareMetadata{TargetManufacturer: "Proto", TargetModel: "CT1", FirmwareVersion: "2.0.0", HashboardBuild: "dev"}
	f.channel(t, pilotOf1, "ct1")
	started, err := f.svc.ApplyFirmware(t.Context(), f.orgID, testActor, f.channelID, []Assignment{{Manufacturer: "Proto", Model: "CT1", FirmwareFileID: "fw-2"}}, nil)
	require.NoError(t, err)
	require.Len(t, started, 1)
	f.svc.EnforceTick(t.Context())
	require.Equal(t, []string{"ct1"}, f.dispatcher.sentIdentifiers())
	require.Equal(t, PhaseInProgress, phaseOf(f.rollout(t, started[0].ID), "ct1"), "controller version cannot prove board convergence")
	q := sqlc.New(f.conn)
	_, err = q.AdvanceContainerInventory(t.Context(), sqlc.AdvanceContainerInventoryParams{DeviceIdentifier: "ct1", ObservedAt: time.Now(), HashboardFirmwareVersion: "2.0.0"})
	require.NoError(t, err)
	f.svc.EnforceTick(t.Context())
	require.Equal(t, PhaseDone, phaseOf(f.rollout(t, started[0].ID), "ct1"))

	observe := func(t *testing.T, version string, mixed bool, age string) sql.NullString {
		t.Helper()
		_, err := f.conn.ExecContext(t.Context(), `UPDATE container_inventory SET hashboard_firmware_version=$2, hashboard_firmware_mixed=$3, observed_at=now()-$4::interval WHERE device_id=$1`, id, version, mixed, age)
		require.NoError(t, err)
		var reported sql.NullString
		require.NoError(t, f.conn.QueryRowContext(t.Context(), `SELECT release_channel_firmware_version($1,2,'2.0.0')`, id).Scan(&reported))
		return reported
	}
	// Unknown evidence neither proves convergence nor shows drift, so a
	// converged container is not reflashed while its boards are unobserved.
	for _, tc := range []struct {
		name, version, age string
	}{
		{"no uniform version", "", "0 seconds"},
		{"stale", "2.0.0", "3 minutes"},
		{"future", "2.0.0", "-10 seconds"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.False(t, observe(t, tc.version, false, tc.age).Valid)
			f.advanceClock(time.Hour)
			f.svc.EnforceTick(t.Context())
			f.svc.EnforceTick(t.Context())
			require.Equal(t, []string{"ct1"}, f.dispatcher.sentIdentifiers())
			require.Equal(t, PhaseDone, phaseOf(f.rollout(t, started[0].ID), "ct1"))
			needing, err := q.ListReleaseChannelFirmwareNeedingRollout(t.Context())
			require.NoError(t, err)
			require.Empty(t, needing)
		})
	}
	// Fresh evidence of a board on another version is drift that enforcement
	// corrects, e.g. a newly adopted board.
	require.Equal(t, sql.NullString{String: "", Valid: true}, observe(t, "", true, "0 seconds"))
	f.svc.EnforceTick(t.Context())
	require.Equal(t, []string{"ct1", "ct1"}, f.dispatcher.sentIdentifiers())
	require.NotEqual(t, PhaseDone, phaseOf(f.rollout(t, started[0].ID), "ct1"))
}
