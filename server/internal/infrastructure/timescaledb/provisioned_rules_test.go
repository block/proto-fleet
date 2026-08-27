package timescaledb_test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestPackagedDefaultRuleSet(t *testing.T) {
	raw, err := os.ReadFile(ruleFile)
	require.NoError(t, err)

	var doc struct {
		DeleteRules []struct {
			UID string `yaml:"uid"`
		} `yaml:"deleteRules"`
		Groups []struct {
			Name  string `yaml:"name"`
			Rules []struct {
				UID   string `yaml:"uid"`
				Title string `yaml:"title"`
			} `yaml:"rules"`
		} `yaml:"groups"`
	}
	require.NoError(t, yaml.Unmarshal(raw, &doc))

	titlesByGroup := make(map[string][]string, len(doc.Groups))
	provisionedUIDs := make(map[string]struct{})
	for _, group := range doc.Groups {
		for _, rule := range group.Rules {
			titlesByGroup[group.Name] = append(titlesByGroup[group.Name], rule.Title)
			provisionedUIDs[rule.UID] = struct{}{}
		}
	}

	require.ElementsMatch(t, []string{
		"Device Offline",
		"Device Temperature High",
		"Telemetry Poll Failure Rate High",
	}, titlesByGroup["proto-fleet-defaults"])
	require.ElementsMatch(t, []string{
		"Curtailment Active",
		"Curtailment Fan Restore Failed",
		"Curtailment Source Unreachable",
	}, titlesByGroup["proto-fleet-curtailment"])

	deletedUIDs := make(map[string]struct{}, len(doc.DeleteRules))
	for _, rule := range doc.DeleteRules {
		deletedUIDs[rule.UID] = struct{}{}
	}
	const retiredUID = "protofleet-device-hashrate-low"
	require.NotContains(t, provisionedUIDs, retiredUID)
	require.Contains(t, deletedUIDs, retiredUID, "retired defaults need tombstones for existing Grafana installations")
}
