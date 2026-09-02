package migrations_test

import (
	"os"
	"testing"

	"github.com/block/proto-fleet/server/migrations"
	"github.com/stretchr/testify/require"
)

func TestFleetNodeAlertStatusViewUsesNarrowGrafanaGrants(t *testing.T) {
	upBytes, err := migrations.Migrations.ReadFile("000145_fleet_node_alert_status.up.sql")
	require.NoError(t, err)
	upSQL := string(upBytes)
	require.Contains(t, upSQL, "CREATE VIEW fleet_node_alert_status AS")
	require.Contains(t, upSQL, "org_id::text AS organization_id")
	require.Contains(t, upSQL, "id::text AS fleet_node_id")
	require.NotContains(t, upSQL, "name AS fleet_node_name")
	require.Contains(t, upSQL, "GRANT SELECT ON fleet_node_alert_status TO grafana_ha_ro")

	downBytes, err := migrations.Migrations.ReadFile("000145_fleet_node_alert_status.down.sql")
	require.NoError(t, err)
	require.Contains(t, string(downBytes), "DROP VIEW IF EXISTS fleet_node_alert_status")

	runFleetBytes, err := os.ReadFile("../../deployment-files/run-fleet.sh")
	require.NoError(t, err)
	runFleet := string(runFleetBytes)
	require.Contains(t, runFleet, "to_regclass('public.fleet_node_alert_status') IS NOT NULL")
	require.Contains(t, runFleet, "GRANT SELECT ON fleet_node_alert_status TO \"${grafana_user}\"")
	require.Contains(t, runFleet, "SELECT 1 FROM fleet_node_alert_status LIMIT 0")
}
