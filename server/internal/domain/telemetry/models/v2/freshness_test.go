package models

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestFreshnessObservationRequiresMeasurementsAndPreservesTheirAge(t *testing.T) {
	now := time.Now()
	old := now.Add(-time.Hour)
	future := now.Add(time.Hour)
	for _, test := range []struct {
		name    string
		metrics DeviceMetrics
		want    time.Time
	}{
		{"status only", DeviceMetrics{Timestamp: now, Health: HealthHealthyActive}, time.Time{}},
		{"missing time", DeviceMetrics{HashrateHS: &MetricValue{Value: 1}}, time.Time{}},
		{"zero is valid", DeviceMetrics{Timestamp: now, HashrateHS: &MetricValue{Value: 0}}, now},
		{"future snapshot", DeviceMetrics{Timestamp: future, HashrateHS: &MetricValue{Value: 1}}, time.Time{}},
		{"future measurement", DeviceMetrics{Timestamp: now, HashrateHS: &MetricValue{Value: 1, MetaData: &MetricValueMetaData{Timestamp: &future}}}, time.Time{}},
		{"nonfinite", DeviceMetrics{Timestamp: now, HashrateHS: &MetricValue{Value: math.NaN()}}, time.Time{}},
		{"cached measurement", DeviceMetrics{Timestamp: now, HashrateHS: &MetricValue{Value: 1, MetaData: &MetricValueMetaData{Timestamp: &old}}}, old},
	} {
		t.Run(test.name, func(t *testing.T) { require.Equal(t, test.want, test.metrics.FreshnessObservationTime()) })
	}
}

func TestHasUsableMeasurementUsesDeviceEvidence(t *testing.T) {
	for _, value := range []struct {
		name  string
		value *MetricValue
		want  bool
	}{
		{"absent", nil, false}, {"zero", &MetricValue{Value: 0}, true},
		{"finite", &MetricValue{Value: 1}, true}, {"nan", &MetricValue{Value: math.NaN()}, false},
		{"infinite", &MetricValue{Value: math.Inf(1)}, false},
	} {
		for _, field := range []string{"hashrate", "temperature", "power", "fan", "efficiency"} {
			t.Run(field+"/"+value.name, func(t *testing.T) {
				m := DeviceMetrics{}
				switch field {
				case "hashrate":
					m.HashrateHS = value.value
				case "temperature":
					m.TempC = value.value
				case "power":
					m.PowerW = value.value
				case "fan":
					m.FanRPM = value.value
				case "efficiency":
					m.EfficiencyJH = value.value
				}
				require.Equal(t, value.want, m.HasUsableMeasurement())
			})
		}
	}
}
