package devicerollup

import (
	"math"
	"testing"

	minerModels "github.com/block/proto-fleet/server/internal/domain/miner/models"
	modelsV2 "github.com/block/proto-fleet/server/internal/domain/telemetry/models/v2"
)

func metricValue(v float64) *modelsV2.MetricValue {
	return &modelsV2.MetricValue{Value: v}
}

func TestAggregateLatestMetricsAllowsNegativeTemperature(t *testing.T) {
	rollup := AggregateLatestMetrics(
		map[minerModels.DeviceIdentifier]modelsV2.DeviceMetrics{
			"cold": {TempC: metricValue(-3.5)},
			"warm": {TempC: metricValue(19.25)},
			"bad":  {TempC: metricValue(math.Inf(1))},
		},
		[]minerModels.DeviceIdentifier{"cold", "warm", "bad"},
	)

	if rollup.ReportingCount != 3 {
		t.Fatalf("ReportingCount: got %d want 3", rollup.ReportingCount)
	}
	if rollup.TemperatureReportingCount != 2 {
		t.Fatalf("TemperatureReportingCount: got %d want 2", rollup.TemperatureReportingCount)
	}
	if rollup.MinTemperatureC != -3.5 {
		t.Errorf("MinTemperatureC: got %g want -3.5", rollup.MinTemperatureC)
	}
	if rollup.MaxTemperatureC != 19.25 {
		t.Errorf("MaxTemperatureC: got %g want 19.25", rollup.MaxTemperatureC)
	}
}
