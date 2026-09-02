package timescaledb_test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestFleetNodeUnavailableRuleUsesHeartbeatStaleness(t *testing.T) {
	raw, err := os.ReadFile(ruleFile)
	require.NoError(t, err)

	var doc struct {
		Groups []struct {
			Rules []struct {
				UID         string            `yaml:"uid"`
				Title       string            `yaml:"title"`
				For         string            `yaml:"for"`
				Labels      map[string]string `yaml:"labels"`
				Annotations map[string]string `yaml:"annotations"`
				Data        []struct {
					Model struct {
						RawSQL string `yaml:"rawSql"`
					} `yaml:"model"`
				} `yaml:"data"`
			} `yaml:"rules"`
		} `yaml:"groups"`
	}
	require.NoError(t, yaml.Unmarshal(raw, &doc))

	for _, group := range doc.Groups {
		for _, rule := range group.Rules {
			if rule.Title != "Fleet Node Unavailable" {
				continue
			}
			require.LessOrEqual(t, len(rule.UID), 40)
			require.Equal(t, "3m", rule.For)
			require.Equal(t, rule.UID, rule.Labels["proto_fleet_rule_uid"])
			require.Equal(t, "fleet-node-unavailable", rule.Labels["template"])
			require.Equal(t, "A Fleet Node is unavailable.", rule.Annotations["summary"])
			require.NotContains(t, rule.Annotations["summary"], "fleet_node_name")
			require.NotEmpty(t, rule.Data)
			sql := rule.Data[0].Model.RawSQL
			require.Contains(t, sql, "FROM fleet_node")
			require.Contains(t, sql, "org_id::text AS organization_id")
			require.Contains(t, sql, "id::text AS fleet_node_id")
			require.Contains(t, sql, "enrollment_status = 'CONFIRMED'")
			require.Contains(t, sql, "deleted_at IS NULL")
			require.Contains(t, sql, "INTERVAL '2 minutes'")
			return
		}
	}

	t.Fatal("Fleet Node Unavailable rule not found")
}

func TestFleetNodeUnavailableUsesColumnLevelGrafanaGrants(t *testing.T) {
	upSQL, err := os.ReadFile("../../../migrations/000145_grant_fleet_node_alert_access.up.sql")
	require.NoError(t, err)
	downSQL, err := os.ReadFile("../../../migrations/000145_grant_fleet_node_alert_access.down.sql")
	require.NoError(t, err)
	runFleet, err := os.ReadFile("../../../../deployment-files/run-fleet.sh")
	require.NoError(t, err)

	for _, sql := range []string{string(upSQL), string(runFleet)} {
		require.Contains(t, sql, "GRANT SELECT (")
		for _, column := range []string{
			"org_id", "id", "last_seen_at", "updated_at", "created_at", "enrollment_status", "deleted_at",
		} {
			require.Contains(t, sql, column)
		}
		require.NotContains(t, sql, "identity_pubkey")
		require.NotContains(t, sql, "encryption_pubkey")
	}

	require.Contains(t, string(upSQL), "ON fleet_node TO grafana_ha_ro")
	require.Contains(t, string(downSQL), "REVOKE SELECT (")
	require.Contains(t, string(downSQL), "ON fleet_node FROM grafana_ha_ro")
	require.NotContains(t, string(runFleet), "GRANT SELECT ON fleet_node")
}
