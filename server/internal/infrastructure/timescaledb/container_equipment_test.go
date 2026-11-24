package timescaledb

import (
	"math"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/testutil"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

func TestContainerControlsOnlyReplaceChangedChildren(t *testing.T) {
	database := testutil.NewDatabaseService(t, nil)
	user := database.CreateSuperAdminUser()
	device := database.CreateDevice(user.OrganizationID, "proto")
	_, err := database.DB.ExecContext(t.Context(), `
		UPDATE discovered_device SET device_class=2
		WHERE id=(SELECT discovered_device_id FROM device WHERE id=$1)`, device.DatabaseID)
	require.NoError(t, err)

	store := func(controls *sdk.ContainerControls) {
		t.Helper()
		tx, err := database.DB.BeginTx(t.Context(), nil)
		require.NoError(t, err)
		defer func() { _ = tx.Rollback() }()
		require.NoError(t, storeContainerEquipment(t.Context(), sqlc.New(tx), device.ID, &sdk.ContainerSnapshot{Controls: controls}))
		require.NoError(t, tx.Commit())
	}
	childVersions := func() map[string]string {
		t.Helper()
		rows, err := database.DB.QueryContext(t.Context(), `
			SELECT 'control/' || name, xmin::text FROM container_control_observation WHERE device_id=$1
			UNION ALL
			SELECT 'setpoint/' || name, xmin::text FROM container_setpoint_observation WHERE device_id=$1
			ORDER BY 1`, device.DatabaseID)
		require.NoError(t, err)
		defer rows.Close()
		versions := map[string]string{}
		for rows.Next() {
			var name, version string
			require.NoError(t, rows.Scan(&name, &version))
			versions[name] = version
		}
		require.NoError(t, rows.Err())
		return versions
	}
	observedAt := func() time.Time {
		t.Helper()
		var observed time.Time
		require.NoError(t, database.DB.QueryRowContext(t.Context(), `
			SELECT observed_at FROM container_controls_observation WHERE device_id=$1`, device.DatabaseID).Scan(&observed))
		return observed
	}

	base := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)
	disabled := false
	negativeZero := math.Copysign(0, -1)
	store(&sdk.ContainerControls{
		ObservedAt: base,
		Controls: []sdk.ContainerControl{
			{Name: "coolant_pump"},
			{Name: "dry_cooler", Enabled: &disabled},
		},
		Setpoints: []sdk.ContainerSetpoint{
			{Name: "coolant_temperature", Unit: "C"},
			{Name: "pump_speed", Value: &negativeZero, Unit: ""},
		},
	})
	initialVersions := childVersions()
	require.Len(t, initialVersions, 4)

	zero := 0.0
	store(&sdk.ContainerControls{
		ObservedAt: base.Add(time.Second),
		Controls: []sdk.ContainerControl{
			{Name: "dry_cooler", Enabled: &disabled},
			{Name: "coolant_pump"},
		},
		Setpoints: []sdk.ContainerSetpoint{
			{Name: "pump_speed", Value: &zero, Unit: ""},
			{Name: "coolant_temperature", Unit: "C"},
		},
	})
	require.True(t, base.Add(time.Second).Equal(observedAt()))
	require.Equal(t, initialVersions, childVersions(), "equivalent child content must not be rewritten")

	changed := 42.0
	store(&sdk.ContainerControls{
		ObservedAt: base.Add(2 * time.Second),
		Controls: []sdk.ContainerControl{
			{Name: "coolant_pump"},
			{Name: "dry_cooler", Enabled: &disabled},
		},
		Setpoints: []sdk.ContainerSetpoint{
			{Name: "coolant_temperature", Unit: "C"},
			{Name: "pump_speed", Value: &changed, Unit: ""},
		},
	})
	require.True(t, base.Add(2*time.Second).Equal(observedAt()))
	require.NotEqual(t, initialVersions, childVersions(), "changed child content must be replaced")
	var stored float64
	require.NoError(t, database.DB.QueryRowContext(t.Context(), `
		SELECT value FROM container_setpoint_observation WHERE device_id=$1 AND name='pump_speed'`, device.DatabaseID).Scan(&stored))
	require.Equal(t, changed, stored)
}
