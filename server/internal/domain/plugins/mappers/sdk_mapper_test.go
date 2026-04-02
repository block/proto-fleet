package mappers

import (
	"testing"
	"time"

	modelsV2 "github.com/block/proto-fleet/server/internal/domain/telemetry/models/v2"
	"github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSDKDeviceMetricsToV2(t *testing.T) {
	now := time.Now()
	deviceID := "test-device-123"
	healthReason := "All systems nominal"

	sdkMetrics := sdk.DeviceMetrics{
		DeviceID:     deviceID,
		Timestamp:    now,
		Health:       sdk.HealthHealthyActive,
		HealthReason: &healthReason,

		HashrateHS:   &sdk.MetricValue{Value: 100000000000.0, Kind: sdk.MetricKindRate},
		TempC:        &sdk.MetricValue{Value: 65.5, Kind: sdk.MetricKindGauge},
		FanRPM:       &sdk.MetricValue{Value: 4500.0, Kind: sdk.MetricKindGauge},
		PowerW:       &sdk.MetricValue{Value: 3250.0, Kind: sdk.MetricKindGauge},
		EfficiencyJH: &sdk.MetricValue{Value: 32.5, Kind: sdk.MetricKindGauge},

		HashBoards: []sdk.HashBoardMetrics{
			{
				ComponentInfo: sdk.ComponentInfo{
					Index:  0,
					Name:   "HashBoard-0",
					Status: sdk.ComponentStatusHealthy,
				},
				HashRateHS: &sdk.MetricValue{Value: 50000000000.0, Kind: sdk.MetricKindRate},
				TempC:      &sdk.MetricValue{Value: 65.0, Kind: sdk.MetricKindGauge},
			},
		},
	}

	v2Metrics := SDKDeviceMetricsToV2(sdkMetrics)

	assert.Equal(t, deviceID, v2Metrics.DeviceIdentifier)
	assert.Equal(t, now, v2Metrics.Timestamp)
	assert.Equal(t, modelsV2.HealthHealthyActive, v2Metrics.Health)
	assert.Equal(t, &healthReason, v2Metrics.HealthReason)

	require.NotNil(t, v2Metrics.HashrateHS)
	assert.InDelta(t, 100000000000.0, v2Metrics.HashrateHS.Value, 0.0001)
	assert.Equal(t, modelsV2.MetricKindRate, v2Metrics.HashrateHS.Kind)

	require.NotNil(t, v2Metrics.TempC)
	assert.InDelta(t, 65.5, v2Metrics.TempC.Value, 0.0001)

	require.Len(t, v2Metrics.HashBoards, 1)
	assert.Equal(t, 0, v2Metrics.HashBoards[0].Index)
	assert.Equal(t, "HashBoard-0", v2Metrics.HashBoards[0].Name)
	assert.Equal(t, modelsV2.ComponentStatusHealthy, v2Metrics.HashBoards[0].Status)
}

func TestMapMetricValue(t *testing.T) {
	tests := []struct {
		name     string
		input    *sdk.MetricValue
		expected *modelsV2.MetricValue
	}{
		{
			name:     "nil value",
			input:    nil,
			expected: nil,
		},
		{
			name: "simple gauge value",
			input: &sdk.MetricValue{
				Value: 42.5,
				Kind:  sdk.MetricKindGauge,
			},
			expected: &modelsV2.MetricValue{
				Value: 42.5,
				Kind:  modelsV2.MetricKindGauge,
			},
		},
		{
			name: "rate value",
			input: &sdk.MetricValue{
				Value: 1000000.0,
				Kind:  sdk.MetricKindRate,
			},
			expected: &modelsV2.MetricValue{
				Value: 1000000.0,
				Kind:  modelsV2.MetricKindRate,
			},
		},
		{
			name: "counter value",
			input: &sdk.MetricValue{
				Value: 12345.0,
				Kind:  sdk.MetricKindCounter,
			},
			expected: &modelsV2.MetricValue{
				Value: 12345.0,
				Kind:  modelsV2.MetricKindCounter,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mapMetricValue(tt.input)
			if tt.expected == nil {
				assert.Nil(t, result)
			} else {
				require.NotNil(t, result)
				assert.InDelta(t, tt.expected.Value, result.Value, 0.0001)
				assert.Equal(t, tt.expected.Kind, result.Kind)
			}
		})
	}
}

func TestMapMetricValueWithMetadata(t *testing.T) {
	window := 5 * time.Second
	minVal := 40.0
	maxVal := 80.0
	avg := 60.0
	stdDev := 10.0
	ts := time.Now()

	sdkValue := &sdk.MetricValue{
		Value: 65.0,
		Kind:  sdk.MetricKindGauge,
		MetaData: &sdk.MetricValueMetaData{
			Window:    &window,
			Min:       &minVal,
			Max:       &maxVal,
			Avg:       &avg,
			StdDev:    &stdDev,
			Timestamp: &ts,
		},
	}

	result := mapMetricValue(sdkValue)

	require.NotNil(t, result)
	assert.InDelta(t, 65.0, result.Value, 0.0001)
	assert.Equal(t, modelsV2.MetricKindGauge, result.Kind)

	require.NotNil(t, result.MetaData)
	assert.Equal(t, &window, result.MetaData.Window)
	require.NotNil(t, result.MetaData.Min)
	assert.InDelta(t, minVal, *result.MetaData.Min, 0.0001)
	require.NotNil(t, result.MetaData.Max)
	assert.InDelta(t, maxVal, *result.MetaData.Max, 0.0001)
	require.NotNil(t, result.MetaData.Avg)
	assert.InDelta(t, avg, *result.MetaData.Avg, 0.0001)
	assert.Equal(t, &stdDev, result.MetaData.StdDev)
	assert.Equal(t, &ts, result.MetaData.Timestamp)
}

func TestMapMetricKind(t *testing.T) {
	tests := []struct {
		name     string
		input    sdk.MetricKind
		expected modelsV2.MetricKind
	}{
		{"gauge", sdk.MetricKindGauge, modelsV2.MetricKindGauge},
		{"rate", sdk.MetricKindRate, modelsV2.MetricKindRate},
		{"counter", sdk.MetricKindCounter, modelsV2.MetricKindCounter},
		{"unspecified", sdk.MetricKindUnspecified, modelsV2.MetricKindUnknown},
		{"unknown default", sdk.MetricKind(999), modelsV2.MetricKindUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mapMetricKind(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMapHealthStatus(t *testing.T) {
	tests := []struct {
		name     string
		input    sdk.HealthStatus
		expected modelsV2.HealthStatus
	}{
		{"healthy active", sdk.HealthHealthyActive, modelsV2.HealthHealthyActive},
		{"healthy inactive", sdk.HealthHealthyInactive, modelsV2.HealthHealthyInactive},
		{"warning", sdk.HealthWarning, modelsV2.HealthWarning},
		{"critical", sdk.HealthCritical, modelsV2.HealthCritical},
		{"unknown", sdk.HealthUnknown, modelsV2.HealthUnknown},
		{"unspecified", sdk.HealthStatusUnspecified, modelsV2.HealthUnknown},
		{"unknown default", sdk.HealthStatus(999), modelsV2.HealthUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mapHealthStatus(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMapComponentStatus(t *testing.T) {
	tests := []struct {
		name     string
		input    sdk.ComponentStatus
		expected modelsV2.ComponentStatus
	}{
		{"healthy", sdk.ComponentStatusHealthy, modelsV2.ComponentStatusHealthy},
		{"warning", sdk.ComponentStatusWarning, modelsV2.ComponentStatusWarning},
		{"critical", sdk.ComponentStatusCritical, modelsV2.ComponentStatusCritical},
		{"offline", sdk.ComponentStatusOffline, modelsV2.ComponentStatusOffline},
		{"disabled", sdk.ComponentStatusDisabled, modelsV2.ComponentStatusDisabled},
		{"unknown", sdk.ComponentStatusUnknown, modelsV2.ComponentStatusUnknown},
		{"unspecified", sdk.ComponentStatusUnspecified, modelsV2.ComponentStatusUnknown},
		{"unknown default", sdk.ComponentStatus(999), modelsV2.ComponentStatusUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mapComponentStatus(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMapComponentInfo(t *testing.T) {
	statusReason := "Temperature above threshold"
	ts := time.Now()

	sdkInfo := sdk.ComponentInfo{
		Index:        3,
		Name:         "Fan-3",
		Status:       sdk.ComponentStatusWarning,
		StatusReason: &statusReason,
		Timestamp:    &ts,
	}

	result := mapComponentInfo(sdkInfo)

	assert.Equal(t, 3, result.Index)
	assert.Equal(t, "Fan-3", result.Name)
	assert.Equal(t, modelsV2.ComponentStatusWarning, result.Status)
	assert.Equal(t, &statusReason, result.StatusReason)
	assert.Equal(t, &ts, result.Timestamp)
}

func TestMapHashBoards(t *testing.T) {
	t.Run("nil slice", func(t *testing.T) {
		result := mapHashBoards(nil)
		assert.Nil(t, result)
	})

	t.Run("empty slice", func(t *testing.T) {
		result := mapHashBoards([]sdk.HashBoardMetrics{})
		assert.Empty(t, result)
	})

	t.Run("with hashboards", func(t *testing.T) {
		serialNum := "HB-12345"
		chipCount := int32(126)

		sdkHashBoards := []sdk.HashBoardMetrics{
			{
				ComponentInfo: sdk.ComponentInfo{
					Index:  0,
					Name:   "HashBoard-0",
					Status: sdk.ComponentStatusHealthy,
				},
				SerialNumber:     &serialNum,
				HashRateHS:       &sdk.MetricValue{Value: 50000000000.0, Kind: sdk.MetricKindRate},
				TempC:            &sdk.MetricValue{Value: 65.0, Kind: sdk.MetricKindGauge},
				VoltageV:         &sdk.MetricValue{Value: 12.5, Kind: sdk.MetricKindGauge},
				CurrentA:         &sdk.MetricValue{Value: 25.0, Kind: sdk.MetricKindGauge},
				ChipCount:        &chipCount,
				ChipFrequencyMHz: &sdk.MetricValue{Value: 650.0, Kind: sdk.MetricKindGauge},
			},
		}

		result := mapHashBoards(sdkHashBoards)

		require.Len(t, result, 1)
		assert.Equal(t, 0, result[0].Index)
		assert.Equal(t, "HashBoard-0", result[0].Name)
		assert.Equal(t, &serialNum, result[0].SerialNumber)
		require.NotNil(t, result[0].HashRateHS)
		assert.InDelta(t, 50000000000.0, result[0].HashRateHS.Value, 0.0001)
		require.NotNil(t, result[0].ChipCount)
		assert.Equal(t, 126, *result[0].ChipCount)
	})

	t.Run("with ASICs and fans", func(t *testing.T) {
		sdkHashBoards := []sdk.HashBoardMetrics{
			{
				ComponentInfo: sdk.ComponentInfo{
					Index:  0,
					Name:   "HashBoard-0",
					Status: sdk.ComponentStatusHealthy,
				},
				ASICs: []sdk.ASICMetrics{
					{
						ComponentInfo: sdk.ComponentInfo{
							Index:  0,
							Name:   "ASIC-0",
							Status: sdk.ComponentStatusHealthy,
						},
						TempC:        &sdk.MetricValue{Value: 70.0, Kind: sdk.MetricKindGauge},
						FrequencyMHz: &sdk.MetricValue{Value: 650.0, Kind: sdk.MetricKindGauge},
					},
				},
				FanMetrics: []sdk.FanMetrics{
					{
						ComponentInfo: sdk.ComponentInfo{
							Index:  0,
							Name:   "Fan-0",
							Status: sdk.ComponentStatusHealthy,
						},
						RPM: &sdk.MetricValue{Value: 4500.0, Kind: sdk.MetricKindGauge},
					},
				},
			},
		}

		result := mapHashBoards(sdkHashBoards)

		require.Len(t, result, 1)
		require.Len(t, result[0].ASICs, 1)
		assert.Equal(t, 0, result[0].ASICs[0].Index)
		assert.Equal(t, "ASIC-0", result[0].ASICs[0].Name)

		require.Len(t, result[0].FanMetrics, 1)
		assert.Equal(t, 0, result[0].FanMetrics[0].Index)
		assert.Equal(t, "Fan-0", result[0].FanMetrics[0].Name)
	})
}

func TestMapASICMetrics(t *testing.T) {
	t.Run("nil slice", func(t *testing.T) {
		result := mapASICMetrics(nil)
		assert.Nil(t, result)
	})

	t.Run("with ASICs", func(t *testing.T) {
		sdkASICs := []sdk.ASICMetrics{
			{
				ComponentInfo: sdk.ComponentInfo{
					Index:  0,
					Name:   "ASIC-0",
					Status: sdk.ComponentStatusHealthy,
				},
				TempC:        &sdk.MetricValue{Value: 72.5, Kind: sdk.MetricKindGauge},
				FrequencyMHz: &sdk.MetricValue{Value: 650.0, Kind: sdk.MetricKindGauge},
				VoltageV:     &sdk.MetricValue{Value: 0.8, Kind: sdk.MetricKindGauge},
				HashrateHS:   &sdk.MetricValue{Value: 1000000000.0, Kind: sdk.MetricKindRate},
			},
		}

		result := mapASICMetrics(sdkASICs)

		require.Len(t, result, 1)
		assert.Equal(t, 0, result[0].Index)
		assert.Equal(t, "ASIC-0", result[0].Name)
		require.NotNil(t, result[0].TempC)
		assert.InDelta(t, 72.5, result[0].TempC.Value, 0.0001)
	})
}

func TestMapPSUMetrics(t *testing.T) {
	t.Run("nil slice", func(t *testing.T) {
		result := mapPSUMetrics(nil)
		assert.Nil(t, result)
	})

	t.Run("with PSUs", func(t *testing.T) {
		sdkPSUs := []sdk.PSUMetrics{
			{
				ComponentInfo: sdk.ComponentInfo{
					Index:  0,
					Name:   "PSU-0",
					Status: sdk.ComponentStatusHealthy,
				},
				OutputPowerW:      &sdk.MetricValue{Value: 3250.0, Kind: sdk.MetricKindGauge},
				OutputVoltageV:    &sdk.MetricValue{Value: 12.0, Kind: sdk.MetricKindGauge},
				OutputCurrentA:    &sdk.MetricValue{Value: 270.0, Kind: sdk.MetricKindGauge},
				InputPowerW:       &sdk.MetricValue{Value: 3450.0, Kind: sdk.MetricKindGauge},
				InputVoltageV:     &sdk.MetricValue{Value: 220.0, Kind: sdk.MetricKindGauge},
				InputCurrentA:     &sdk.MetricValue{Value: 15.7, Kind: sdk.MetricKindGauge},
				HotSpotTempC:      &sdk.MetricValue{Value: 55.0, Kind: sdk.MetricKindGauge},
				EfficiencyPercent: &sdk.MetricValue{Value: 94.2, Kind: sdk.MetricKindGauge},
				FanMetrics: []sdk.FanMetrics{
					{
						ComponentInfo: sdk.ComponentInfo{
							Index:  0,
							Name:   "PSU-Fan-0",
							Status: sdk.ComponentStatusHealthy,
						},
						RPM: &sdk.MetricValue{Value: 3000.0, Kind: sdk.MetricKindGauge},
					},
				},
			},
		}

		result := mapPSUMetrics(sdkPSUs)

		require.Len(t, result, 1)
		assert.Equal(t, 0, result[0].Index)
		assert.Equal(t, "PSU-0", result[0].Name)
		require.NotNil(t, result[0].OutputPowerW)
		assert.InDelta(t, 3250.0, result[0].OutputPowerW.Value, 0.0001)
		require.NotNil(t, result[0].EfficiencyPercent)
		assert.InDelta(t, 94.2, result[0].EfficiencyPercent.Value, 0.0001)
		require.Len(t, result[0].FanMetrics, 1)
		assert.Equal(t, "PSU-Fan-0", result[0].FanMetrics[0].Name)
	})
}

func TestMapFanMetrics(t *testing.T) {
	t.Run("nil slice", func(t *testing.T) {
		result := mapFanMetrics(nil)
		assert.Nil(t, result)
	})

	t.Run("with fans", func(t *testing.T) {
		sdkFans := []sdk.FanMetrics{
			{
				ComponentInfo: sdk.ComponentInfo{
					Index:  0,
					Name:   "Fan-0",
					Status: sdk.ComponentStatusHealthy,
				},
				RPM:     &sdk.MetricValue{Value: 4500.0, Kind: sdk.MetricKindGauge},
				TempC:   &sdk.MetricValue{Value: 45.0, Kind: sdk.MetricKindGauge},
				Percent: &sdk.MetricValue{Value: 75.0, Kind: sdk.MetricKindGauge},
			},
			{
				ComponentInfo: sdk.ComponentInfo{
					Index:  1,
					Name:   "Fan-1",
					Status: sdk.ComponentStatusWarning,
				},
				RPM:     &sdk.MetricValue{Value: 3000.0, Kind: sdk.MetricKindGauge},
				Percent: &sdk.MetricValue{Value: 50.0, Kind: sdk.MetricKindGauge},
			},
		}

		result := mapFanMetrics(sdkFans)

		require.Len(t, result, 2)
		assert.Equal(t, 0, result[0].Index)
		assert.Equal(t, "Fan-0", result[0].Name)
		require.NotNil(t, result[0].RPM)
		assert.InDelta(t, 4500.0, result[0].RPM.Value, 0.0001)

		assert.Equal(t, 1, result[1].Index)
		assert.Equal(t, modelsV2.ComponentStatusWarning, result[1].Status)
	})
}

func TestMapControlBoardMetrics(t *testing.T) {
	t.Run("nil slice", func(t *testing.T) {
		result := mapControlBoardMetrics(nil)
		assert.Nil(t, result)
	})

	t.Run("with control boards", func(t *testing.T) {
		sdkCBs := []sdk.ControlBoardMetrics{
			{
				ComponentInfo: sdk.ComponentInfo{
					Index:  0,
					Name:   "ControlBoard-0",
					Status: sdk.ComponentStatusHealthy,
				},
			},
		}

		result := mapControlBoardMetrics(sdkCBs)

		require.Len(t, result, 1)
		assert.Equal(t, 0, result[0].Index)
		assert.Equal(t, "ControlBoard-0", result[0].Name)
	})
}

func TestMapSensorMetrics(t *testing.T) {
	t.Run("nil slice", func(t *testing.T) {
		result := mapSensorMetrics(nil)
		assert.Nil(t, result)
	})

	t.Run("with sensors", func(t *testing.T) {
		sdkSensors := []sdk.SensorMetrics{
			{
				ComponentInfo: sdk.ComponentInfo{
					Index:  0,
					Name:   "Humidity-Sensor",
					Status: sdk.ComponentStatusHealthy,
				},
				Type:  "humidity",
				Unit:  "%",
				Value: &sdk.MetricValue{Value: 45.0, Kind: sdk.MetricKindGauge},
			},
			{
				ComponentInfo: sdk.ComponentInfo{
					Index:  1,
					Name:   "Vibration-Sensor",
					Status: sdk.ComponentStatusHealthy,
				},
				Type:  "vibration",
				Unit:  "g",
				Value: &sdk.MetricValue{Value: 0.5, Kind: sdk.MetricKindGauge},
			},
		}

		result := mapSensorMetrics(sdkSensors)

		require.Len(t, result, 2)
		assert.Equal(t, 0, result[0].Index)
		assert.Equal(t, "Humidity-Sensor", result[0].Name)
		assert.Equal(t, "humidity", result[0].Type)
		assert.Equal(t, "%", result[0].Unit)
		require.NotNil(t, result[0].Value)
		assert.InDelta(t, 45.0, result[0].Value.Value, 0.0001)

		assert.Equal(t, "vibration", result[1].Type)
		assert.Equal(t, "g", result[1].Unit)
	})
}

func TestMapInt32ToIntPtr(t *testing.T) {
	t.Run("nil pointer", func(t *testing.T) {
		result := mapInt32ToIntPtr(nil)
		assert.Nil(t, result)
	})

	t.Run("valid value", func(t *testing.T) {
		val := int32(126)
		result := mapInt32ToIntPtr(&val)
		require.NotNil(t, result)
		assert.Equal(t, 126, *result)
	})

	t.Run("zero value", func(t *testing.T) {
		val := int32(0)
		result := mapInt32ToIntPtr(&val)
		require.NotNil(t, result)
		assert.Equal(t, 0, *result)
	})
}

func TestCompleteDeviceMetricsMapping(t *testing.T) {
	// This test verifies a complete, realistic mapping scenario
	now := time.Now()
	deviceID := "proto-miner-abc123"
	healthReason := "All systems operational"
	serialNum := "SN-HB-001"
	chipCount := int32(126)

	sdkMetrics := sdk.DeviceMetrics{
		DeviceID:     deviceID,
		Timestamp:    now,
		Health:       sdk.HealthHealthyActive,
		HealthReason: &healthReason,

		// Device-level metrics
		HashrateHS:   &sdk.MetricValue{Value: 100e12, Kind: sdk.MetricKindRate},
		TempC:        &sdk.MetricValue{Value: 68.5, Kind: sdk.MetricKindGauge},
		FanRPM:       &sdk.MetricValue{Value: 4800.0, Kind: sdk.MetricKindGauge},
		PowerW:       &sdk.MetricValue{Value: 3300.0, Kind: sdk.MetricKindGauge},
		EfficiencyJH: &sdk.MetricValue{Value: 33.0, Kind: sdk.MetricKindGauge},

		// HashBoards with sub-components
		HashBoards: []sdk.HashBoardMetrics{
			{
				ComponentInfo: sdk.ComponentInfo{
					Index:  0,
					Name:   "HashBoard-0",
					Status: sdk.ComponentStatusHealthy,
				},
				SerialNumber:     &serialNum,
				HashRateHS:       &sdk.MetricValue{Value: 50e12, Kind: sdk.MetricKindRate},
				TempC:            &sdk.MetricValue{Value: 68.0, Kind: sdk.MetricKindGauge},
				VoltageV:         &sdk.MetricValue{Value: 12.0, Kind: sdk.MetricKindGauge},
				CurrentA:         &sdk.MetricValue{Value: 137.5, Kind: sdk.MetricKindGauge},
				ChipCount:        &chipCount,
				ChipFrequencyMHz: &sdk.MetricValue{Value: 650.0, Kind: sdk.MetricKindGauge},
				ASICs: []sdk.ASICMetrics{
					{
						ComponentInfo: sdk.ComponentInfo{
							Index:  0,
							Name:   "ASIC-0-0",
							Status: sdk.ComponentStatusHealthy,
						},
						TempC:        &sdk.MetricValue{Value: 70.0, Kind: sdk.MetricKindGauge},
						FrequencyMHz: &sdk.MetricValue{Value: 650.0, Kind: sdk.MetricKindGauge},
					},
				},
			},
		},

		// PSUs
		PSUMetrics: []sdk.PSUMetrics{
			{
				ComponentInfo: sdk.ComponentInfo{
					Index:  0,
					Name:   "PSU-0",
					Status: sdk.ComponentStatusHealthy,
				},
				OutputPowerW:      &sdk.MetricValue{Value: 3300.0, Kind: sdk.MetricKindGauge},
				OutputVoltageV:    &sdk.MetricValue{Value: 12.0, Kind: sdk.MetricKindGauge},
				InputVoltageV:     &sdk.MetricValue{Value: 220.0, Kind: sdk.MetricKindGauge},
				EfficiencyPercent: &sdk.MetricValue{Value: 94.5, Kind: sdk.MetricKindGauge},
			},
		},

		// Device-level fans
		FanMetrics: []sdk.FanMetrics{
			{
				ComponentInfo: sdk.ComponentInfo{
					Index:  0,
					Name:   "Fan-0",
					Status: sdk.ComponentStatusHealthy,
				},
				RPM:     &sdk.MetricValue{Value: 4800.0, Kind: sdk.MetricKindGauge},
				Percent: &sdk.MetricValue{Value: 80.0, Kind: sdk.MetricKindGauge},
			},
		},

		// Sensors
		SensorMetrics: []sdk.SensorMetrics{
			{
				ComponentInfo: sdk.ComponentInfo{
					Index:  0,
					Name:   "Ambient-Temp",
					Status: sdk.ComponentStatusHealthy,
				},
				Type:  "temperature",
				Unit:  "C",
				Value: &sdk.MetricValue{Value: 25.0, Kind: sdk.MetricKindGauge},
			},
		},
	}

	// Perform the mapping
	v2Metrics := SDKDeviceMetricsToV2(sdkMetrics)

	// Verify all top-level fields
	assert.Equal(t, deviceID, v2Metrics.DeviceIdentifier)
	assert.Equal(t, now, v2Metrics.Timestamp)
	assert.Equal(t, modelsV2.HealthHealthyActive, v2Metrics.Health)
	assert.Equal(t, &healthReason, v2Metrics.HealthReason)

	// Verify device-level metrics
	require.NotNil(t, v2Metrics.HashrateHS)
	assert.InDelta(t, 100e12, v2Metrics.HashrateHS.Value, 0.0001)
	assert.Equal(t, modelsV2.MetricKindRate, v2Metrics.HashrateHS.Kind)

	require.NotNil(t, v2Metrics.PowerW)
	assert.InDelta(t, 3300.0, v2Metrics.PowerW.Value, 0.0001)

	// Verify HashBoards
	require.Len(t, v2Metrics.HashBoards, 1)
	hb := v2Metrics.HashBoards[0]
	assert.Equal(t, 0, hb.Index)
	assert.Equal(t, "HashBoard-0", hb.Name)
	assert.Equal(t, modelsV2.ComponentStatusHealthy, hb.Status)
	assert.Equal(t, &serialNum, hb.SerialNumber)
	require.NotNil(t, hb.ChipCount)
	assert.Equal(t, 126, *hb.ChipCount)

	// Verify ASICs
	require.Len(t, hb.ASICs, 1)
	assert.Equal(t, "ASIC-0-0", hb.ASICs[0].Name)

	// Verify PSUs
	require.Len(t, v2Metrics.PSUMetrics, 1)
	psu := v2Metrics.PSUMetrics[0]
	assert.Equal(t, "PSU-0", psu.Name)
	require.NotNil(t, psu.EfficiencyPercent)
	assert.InDelta(t, 94.5, psu.EfficiencyPercent.Value, 0.0001)

	// Verify Fans
	require.Len(t, v2Metrics.FanMetrics, 1)
	fan := v2Metrics.FanMetrics[0]
	assert.Equal(t, "Fan-0", fan.Name)
	require.NotNil(t, fan.RPM)
	assert.InDelta(t, 4800.0, fan.RPM.Value, 0.0001)

	// Verify Sensors
	require.Len(t, v2Metrics.SensorMetrics, 1)
	sensor := v2Metrics.SensorMetrics[0]
	assert.Equal(t, "Ambient-Temp", sensor.Name)
	assert.Equal(t, "temperature", sensor.Type)
	assert.Equal(t, "C", sensor.Unit)
	require.NotNil(t, sensor.Value)
	assert.InDelta(t, 25.0, sensor.Value.Value, 0.0001)
}
