package migrations_test

import (
	"testing"

	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/block/proto-fleet/server/migrations"
	"github.com/stretchr/testify/require"
)

func TestDiscoveryIdentityUpgradePreservesRowsWithoutInventingEvidence(t *testing.T) {
	cfg, err := testutil.GetTestConfig()
	require.NoError(t, err)
	database := testutil.NewDatabaseService(t, cfg)
	user := database.CreateSuperAdminUser()
	device := database.CreateDevice(user.OrganizationID, "proto")
	_, err = database.DB.ExecContext(t.Context(), "UPDATE device SET serial_number='paired-serial',mac_address='00:11:22:33:44:55' WHERE id=$1", device.DatabaseID)
	require.NoError(t, err)
	var discoveredID int64
	require.NoError(t, database.DB.QueryRowContext(t.Context(), "SELECT discovered_device_id FROM device WHERE id=$1", device.DatabaseID).Scan(&discoveredID))
	down, err := migrations.Current.ReadFile("current/001014_internal_discovery_identity.down.sql")
	require.NoError(t, err)
	_, err = database.DB.ExecContext(t.Context(), string(down))
	require.NoError(t, err)
	up, err := migrations.Current.ReadFile("current/001014_internal_discovery_identity.up.sql")
	require.NoError(t, err)
	_, err = database.DB.ExecContext(t.Context(), string(up))
	require.NoError(t, err)
	var serial, mac string
	require.NoError(t, database.DB.QueryRowContext(t.Context(), "SELECT serial_number,mac_address FROM discovered_device WHERE id=$1", discoveredID).Scan(&serial, &mac))
	require.Empty(t, serial, "stored device identity does not prove a fresh discovery response")
	require.Empty(t, mac)
	var linked int64
	require.NoError(t, database.DB.QueryRowContext(t.Context(), "SELECT discovered_device_id FROM device WHERE id=$1", device.DatabaseID).Scan(&linked))
	require.Equal(t, discoveredID, linked)
}
