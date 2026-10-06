package timescaledb_test

import (
	"os"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

const haRuleFile = "../../../monitoring/grafana/ha/proto-fleet-ha-rules.yaml"

func TestHAReadinessRuleRequiresMinuteOfDegradation(t *testing.T) {
	raw, err := os.ReadFile(haRuleFile)
	require.NoError(t, err)

	var doc struct {
		Groups []struct {
			Rules []struct {
				Title string `yaml:"title"`
				For   string `yaml:"for"`
			} `yaml:"rules"`
		} `yaml:"groups"`
	}
	require.NoError(t, yaml.Unmarshal(raw, &doc))

	for _, group := range doc.Groups {
		for _, rule := range group.Rules {
			if rule.Title == "HA Failover Readiness Degraded" {
				require.Equal(t, "1m", rule.For)
				return
			}
		}
	}
	t.Fatal("HA readiness rule not found")
}

func TestHAReadinessRuleUsesLatestValueAndFreshness(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	db := testutil.GetTestDB(t)
	rawSQL := loadRuleSQLFrom(t, haRuleFile, "HA Failover Readiness Degraded", "fleet_ha_failover_ready")
	orgID := seedActiveOrg(t, db, 0)

	for _, test := range []struct {
		name  string
		value *float64
		age   time.Duration
		alert bool
	}{
		{name: "no sample", alert: true},
		{name: "fresh ready", value: float64Ptr(1)},
		{name: "fresh degraded", value: float64Ptr(0), alert: true},
		{name: "stale ready", value: float64Ptr(1), age: 3 * time.Minute, alert: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			clearSystemSamples(t, db)
			if test.value != nil {
				writeSystemSample(t, db, "fleet_ha_failover_ready", *test.value, test.age)
			}

			got := runRule(t, db, rawSQL)

			if test.alert {
				require.Equal(t, map[string]float64{orgID: 1}, got)
			} else {
				require.Empty(t, got)
			}
		})
	}
}

func float64Ptr(value float64) *float64 {
	return &value
}
