package rollout

import (
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
	// Mixed and stale observations withdraw the effective version without
	// changing the controller's version or deployed artifact provenance.
	_, err = q.AdvanceContainerInventory(t.Context(), sqlc.AdvanceContainerInventoryParams{DeviceIdentifier: "ct1", ObservedAt: time.Now(), HashboardFirmwareVersion: ""})
	require.NoError(t, err)
	var version string
	require.NoError(t, f.conn.QueryRowContext(t.Context(), `SELECT release_channel_firmware_version($1,2,'2.0.0')`, id).Scan(&version))
	require.Empty(t, version)
	_, err = f.conn.ExecContext(t.Context(), `UPDATE container_inventory SET hashboard_firmware_version='2.0.0',observed_at=now()-INTERVAL '3 minutes' WHERE device_id=$1`, id)
	require.NoError(t, err)
	require.NoError(t, f.conn.QueryRowContext(t.Context(), `SELECT release_channel_firmware_version($1,2,'2.0.0')`, id).Scan(&version))
	require.Empty(t, version)
}
