package models

import (
	"testing"
	"time"

	"github.com/btc-mining/proto-fleet/server/internal/domain/telemetry/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestDeviceMetrics_ToRawTelemetry(t *testing.T) {
	timestamp := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)

	t.Run("converts all default measurement types with raw values", func(t *testing.T) {
		// Arrange
		metrics := &DeviceMetrics{
			DeviceID:     "device-123",
			Timestamp:    timestamp,
			HashrateHS:   &MetricValue{Value: 100e12},
			TempC:        &MetricValue{Value: 75.5},
			PowerW:       &MetricValue{Value: 3200},
			EfficiencyJH: &MetricValue{Value: 30e-12},
			FanRPM:       &MetricValue{Value: 6000},
		}

		// Act
		result := metrics.ToRawTelemetry(nil)

		// Assert
		require.Len(t, result, 5)

		// Verify each telemetry record - values should be RAW (not converted)
		telemetryMap := make(map[string]models.Telemetry)
		for _, r := range result {
			telemetryMap[r.Measurement] = r
		}

		// Hashrate - raw H/s value (not TH/s)
		hashrate, ok := telemetryMap["hashrate_mhs"]
		require.True(t, ok)
		assert.Equal(t, "device-123", hashrate.Tags["device_id"])
		assert.InDelta(t, 100e12, hashrate.Fields["value"], 1e3) // Raw H/s
		assert.Equal(t, timestamp, hashrate.Timestamp)

		// Temperature - unchanged
		temp, ok := telemetryMap["temperature_c"]
		require.True(t, ok)
		assert.InDelta(t, 75.5, temp.Fields["value"], 1e-9)

		// Power - raw W value (not kW)
		power, ok := telemetryMap["power_w"]
		require.True(t, ok)
		assert.InDelta(t, 3200, power.Fields["value"], 1e-9) // Raw W

		// Efficiency - raw J/H value (not J/TH)
		efficiency, ok := telemetryMap["efficiency_jh"]
		require.True(t, ok)
		assert.InDelta(t, 30e-12, efficiency.Fields["value"], 1e-21) // Raw J/H

		// Fan speed - unchanged
		fan, ok := telemetryMap["fan_rpm"]
		require.True(t, ok)
		assert.InDelta(t, 6000.0, fan.Fields["value"], 1e-9)
	})

	t.Run("converts only requested measurement types", func(t *testing.T) {
		// Arrange
		metrics := &DeviceMetrics{
			DeviceID:     "device-456",
			Timestamp:    timestamp,
			HashrateHS:   &MetricValue{Value: 50e12},
			TempC:        &MetricValue{Value: 65.0},
			PowerW:       &MetricValue{Value: 2500},
			EfficiencyJH: &MetricValue{Value: 25e-12},
			FanRPM:       &MetricValue{Value: 5500},
		}
		requestedTypes := []models.MeasurementType{
			models.MeasurementTypeHashrate,
			models.MeasurementTypePower,
		}

		// Act
		result := metrics.ToRawTelemetry(requestedTypes)

		// Assert
		require.Len(t, result, 2)

		measurements := make(map[string]bool)
		for _, r := range result {
			measurements[r.Measurement] = true
		}
		assert.True(t, measurements["hashrate_mhs"])
		assert.True(t, measurements["power_w"])
		assert.False(t, measurements["temperature_c"])
	})

	t.Run("skips unavailable measurements", func(t *testing.T) {
		// Arrange
		metrics := &DeviceMetrics{
			DeviceID:   "device-789",
			Timestamp:  timestamp,
			HashrateHS: &MetricValue{Value: 75e12},
			// Other fields are nil
		}

		// Act
		result := metrics.ToRawTelemetry(nil)

		// Assert
		require.Len(t, result, 1)
		assert.Equal(t, "hashrate_mhs", result[0].Measurement)
	})

	t.Run("returns empty slice when no measurements available", func(t *testing.T) {
		// Arrange
		metrics := &DeviceMetrics{
			DeviceID:  "device-empty",
			Timestamp: timestamp,
			// All metric fields are nil
		}

		// Act
		result := metrics.ToRawTelemetry(nil)

		// Assert
		assert.Empty(t, result)
	})

	t.Run("empty requested types uses defaults", func(t *testing.T) {
		// Arrange
		metrics := &DeviceMetrics{
			DeviceID:   "device-default",
			Timestamp:  timestamp,
			HashrateHS: &MetricValue{Value: 100e12},
			TempC:      &MetricValue{Value: 70.0},
		}

		// Act
		result := metrics.ToRawTelemetry([]models.MeasurementType{})

		// Assert - should use DefaultMeasurementTypes
		require.Len(t, result, 2) // Only hashrate and temp are available
	})
}
