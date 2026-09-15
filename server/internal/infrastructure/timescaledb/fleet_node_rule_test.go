package timescaledb_test

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestFleetNodeUnavailableRuleUsesHeartbeatStaleness(t *testing.T) {
	for _, path := range []string{ruleFile, haRuleFile} {
		t.Run(path, func(t *testing.T) {
			raw, err := os.ReadFile(path)
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
					require.Contains(t, sql, "last_seen_at IS NOT NULL")
					require.Contains(t, sql, "last_seen_at < NOW() - INTERVAL '2 minutes'")
					require.NotContains(t, sql, "COALESCE(last_seen_at")
					require.NotContains(t, sql, "updated_at")
					require.NotContains(t, sql, "created_at")
					return
				}
			}

			t.Fatal("Fleet Node Unavailable rule not found")
		})
	}
}

func TestFleetNodeUnavailableUsesColumnLevelGrafanaGrants(t *testing.T) {
	upSQL, err := os.ReadFile("../../../migrations/000145_grant_fleet_node_alert_access.up.sql")
	require.NoError(t, err)
	downSQL, err := os.ReadFile("../../../migrations/000145_grant_fleet_node_alert_access.down.sql")
	require.NoError(t, err)
	runFleet, err := os.ReadFile("../../../../deployment-files/run-fleet.sh")
	require.NoError(t, err)

	requiredColumns := []string{"org_id", "id", "last_seen_at", "enrollment_status", "deleted_at"}
	for _, sql := range []string{string(upSQL), string(runFleet)} {
		require.ElementsMatch(t, requiredColumns, fleetNodeSelectColumns(t, sql, "GRANT"))
	}
	require.ElementsMatch(t, requiredColumns, fleetNodeSelectColumns(t, string(downSQL), "REVOKE"))

	require.Contains(t, string(upSQL), "ON fleet_node TO grafana_ha_ro")
	require.Contains(t, string(downSQL), "ON fleet_node FROM grafana_ha_ro")
	require.NotContains(t, string(runFleet), "GRANT SELECT ON fleet_node")
}

func fleetNodeSelectColumns(t *testing.T, sql, operation string) []string {
	t.Helper()
	pattern := regexp.MustCompile(`(?is)\b` + operation + `\s+SELECT\s*\(([^)]*)\)\s*ON\s+fleet_node\b`)
	matches := pattern.FindAllStringSubmatch(sql, -1)
	require.Len(t, matches, 1)

	columns := strings.Split(matches[0][1], ",")
	for i := range columns {
		columns[i] = strings.TrimSpace(columns[i])
	}
	return columns
}
