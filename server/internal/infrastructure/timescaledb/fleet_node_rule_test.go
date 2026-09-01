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
				UID    string            `yaml:"uid"`
				Title  string            `yaml:"title"`
				For    string            `yaml:"for"`
				Labels map[string]string `yaml:"labels"`
				Data   []struct {
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
			require.NotEmpty(t, rule.Data)
			sql := rule.Data[0].Model.RawSQL
			require.Contains(t, sql, "FROM fleet_node_alert_status")
			require.NotContains(t, sql, "FROM fleet_node\n")
			require.Contains(t, sql, "COALESCE(last_seen_at, updated_at, created_at)")
			require.Contains(t, sql, "INTERVAL '2 minutes'")
			return
		}
	}

	t.Fatal("Fleet Node Unavailable rule not found")
}
