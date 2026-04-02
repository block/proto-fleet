package models

import (
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/telemetry/models"
	"github.com/stretchr/testify/assert"
)

func TestDeviceMetrics_ExtractRawMeasurement(t *testing.T) {
	timestamp := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)

	tests := []struct {
		name            string
		metrics         *DeviceMetrics
		measurementType models.MeasurementType
		expectedValue   float64
		expectedTime    time.Time
		expectedOK      bool
	}{
		{
			name: "extracts hashrate in raw H/s",
			metrics: &DeviceMetrics{
				Timestamp:  timestamp,
				HashrateHS: &MetricValue{Value: 100e12}, // 100 TH/s in H/s
			},
			measurementType: models.MeasurementTypeHashrate,
			expectedValue:   100e12, // Raw H/s value
			expectedTime:    timestamp,
			expectedOK:      true,
		},
		{
			name: "extracts temperature unchanged",
			metrics: &DeviceMetrics{
				Timestamp: timestamp,
				TempC:     &MetricValue{Value: 75.5},
			},
			measurementType: models.MeasurementTypeTemperature,
			expectedValue:   75.5,
			expectedTime:    timestamp,
			expectedOK:      true,
		},
		{
			name: "extracts power in raw W",
			metrics: &DeviceMetrics{
				Timestamp: timestamp,
				PowerW:    &MetricValue{Value: 3200}, // 3.2 kW in W
			},
			measurementType: models.MeasurementTypePower,
			expectedValue:   3200, // Raw W value
			expectedTime:    timestamp,
			expectedOK:      true,
		},
		{
			name: "extracts efficiency in raw J/H",
			metrics: &DeviceMetrics{
				Timestamp:    timestamp,
				EfficiencyJH: &MetricValue{Value: 30e-12}, // 30 J/TH in J/H
			},
			measurementType: models.MeasurementTypeEfficiency,
			expectedValue:   30e-12, // Raw J/H value
			expectedTime:    timestamp,
			expectedOK:      true,
		},
		{
			name: "extracts fan speed unchanged",
			metrics: &DeviceMetrics{
				Timestamp: timestamp,
				FanRPM:    &MetricValue{Value: 6000},
			},
			measurementType: models.MeasurementTypeFanSpeed,
			expectedValue:   6000,
			expectedTime:    timestamp,
			expectedOK:      true,
		},
		{
			name: "returns false for nil hashrate",
			metrics: &DeviceMetrics{
				Timestamp:  timestamp,
				HashrateHS: nil,
			},
			measurementType: models.MeasurementTypeHashrate,
			expectedOK:      false,
		},
		{
			name: "returns false for nil temperature",
			metrics: &DeviceMetrics{
				Timestamp: timestamp,
				TempC:     nil,
			},
			measurementType: models.MeasurementTypeTemperature,
			expectedOK:      false,
		},
		{
			name: "returns false for nil power",
			metrics: &DeviceMetrics{
				Timestamp: timestamp,
				PowerW:    nil,
			},
			measurementType: models.MeasurementTypePower,
			expectedOK:      false,
		},
		{
			name: "returns false for nil efficiency",
			metrics: &DeviceMetrics{
				Timestamp:    timestamp,
				EfficiencyJH: nil,
			},
			measurementType: models.MeasurementTypeEfficiency,
			expectedOK:      false,
		},
		{
			name: "returns false for nil fan speed",
			metrics: &DeviceMetrics{
				Timestamp: timestamp,
				FanRPM:    nil,
			},
			measurementType: models.MeasurementTypeFanSpeed,
			expectedOK:      false,
		},
		{
			name: "returns false for unsupported type voltage",
			metrics: &DeviceMetrics{
				Timestamp: timestamp,
			},
			measurementType: models.MeasurementTypeVoltage,
			expectedOK:      false,
		},
		{
			name: "returns false for unsupported type current",
			metrics: &DeviceMetrics{
				Timestamp: timestamp,
			},
			measurementType: models.MeasurementTypeCurrent,
			expectedOK:      false,
		},
		{
			name: "returns false for unknown measurement type",
			metrics: &DeviceMetrics{
				Timestamp: timestamp,
			},
			measurementType: models.MeasurementTypeUnknown,
			expectedOK:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange - values set in test case

			// Act
			value, ts, ok := tt.metrics.ExtractRawMeasurement(tt.measurementType)

			// Assert
			assert.Equal(t, tt.expectedOK, ok)

			if tt.expectedOK {
				assert.InDelta(t, tt.expectedValue, value, 1e-9)
				assert.Equal(t, tt.expectedTime, ts)
			}
		})
	}
}
