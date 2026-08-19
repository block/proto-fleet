package timescaledb_test

import (
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/require"
)

const haRuleFile = "../../../monitoring/grafana/ha/proto-fleet-ha-rules.yaml"

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
