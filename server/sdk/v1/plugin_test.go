package sdk

import (
	"context"
	"errors"
	"testing"
	"time"

	pb "github.com/block/proto-fleet/server/sdk/v1/pb/generated"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Test constants for deterministic testing
var (
	testTime = time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC)
)

// ============================================================================
// Test Helpers
// ============================================================================

// createFullDeviceMetrics creates a DeviceMetrics with all fields populated
func createFullDeviceMetrics() DeviceMetrics {
	healthReason := "All systems operational"
	serialNumber := "HB-12345"
	chipCount := int32(120)

	window := 5 * time.Second
	tempTimestamp := testTime.Add(1 * time.Second)

	return DeviceMetrics{
		DeviceID:     "test-device-123",
		Timestamp:    testTime,
		Health:       HealthHealthyActive,
		HealthReason: &healthReason,

		// Device-level metrics
		HashrateHS:   &MetricValue{Value: 100e12, Kind: MetricKindRate},
		TempC:        &MetricValue{Value: 65.5, Kind: MetricKindGauge},
		FanRPM:       &MetricValue{Value: 3000, Kind: MetricKindGauge},
		PowerW:       &MetricValue{Value: 3200.5, Kind: MetricKindGauge},
		EfficiencyJH: &MetricValue{Value: 32.0, Kind: MetricKindGauge},

		// Hash boards
		HashBoards: []HashBoardMetrics{
			{
				ComponentInfo: ComponentInfo{
					Index:  0,
					Name:   "HashBoard 0",
					Status: ComponentStatusHealthy,
				},
				SerialNumber: &serialNumber,
				HashRateHS:   &MetricValue{Value: 50e12, Kind: MetricKindRate},
				TempC: &MetricValue{Value: 65.5, Kind: MetricKindGauge, MetaData: &MetricValueMetaData{
					Window:    &window,
					Min:       ptr(62.0),
					Max:       ptr(68.0),
					Avg:       ptr(65.0),
					StdDev:    ptr(1.5),
					Timestamp: &tempTimestamp,
				}},
				VoltageV:         &MetricValue{Value: 12.0, Kind: MetricKindGauge},
				CurrentA:         &MetricValue{Value: 10.5, Kind: MetricKindGauge},
				InletTempC:       &MetricValue{Value: 60.0, Kind: MetricKindGauge},
				OutletTempC:      &MetricValue{Value: 70.0, Kind: MetricKindGauge},
				AmbientTempC:     &MetricValue{Value: 25.0, Kind: MetricKindGauge},
				ChipCount:        &chipCount,
				ChipFrequencyMHz: &MetricValue{Value: 650.0, Kind: MetricKindGauge},
				ASICs: []ASICMetrics{
					{
						ComponentInfo: ComponentInfo{
							Index:  0,
							Name:   "ASIC 0",
							Status: ComponentStatusHealthy,
						},
						TempC:        &MetricValue{Value: 64.0, Kind: MetricKindGauge},
						FrequencyMHz: &MetricValue{Value: 650.0, Kind: MetricKindGauge},
						VoltageV:     &MetricValue{Value: 0.85, Kind: MetricKindGauge},
						HashrateHS:   &MetricValue{Value: 1e12, Kind: MetricKindRate},
					},
				},
				FanMetrics: []FanMetrics{
					{
						ComponentInfo: ComponentInfo{
							Index:  0,
							Name:   "Fan 0",
							Status: ComponentStatusHealthy,
						},
						RPM:     &MetricValue{Value: 3000, Kind: MetricKindGauge},
						TempC:   &MetricValue{Value: 45.0, Kind: MetricKindGauge},
						Percent: &MetricValue{Value: 75.0, Kind: MetricKindGauge},
					},
				},
			},
		},

		// PSUs
		PSUMetrics: []PSUMetrics{
			{
				ComponentInfo: ComponentInfo{
					Index:  0,
					Name:   "PSU 0",
					Status: ComponentStatusHealthy,
				},
				OutputPowerW:      &MetricValue{Value: 3200.0, Kind: MetricKindGauge},
				OutputVoltageV:    &MetricValue{Value: 12.0, Kind: MetricKindGauge},
				OutputCurrentA:    &MetricValue{Value: 266.0, Kind: MetricKindGauge},
				InputPowerW:       &MetricValue{Value: 3400.0, Kind: MetricKindGauge},
				InputVoltageV:     &MetricValue{Value: 240.0, Kind: MetricKindGauge},
				InputCurrentA:     &MetricValue{Value: 14.2, Kind: MetricKindGauge},
				HotSpotTempC:      &MetricValue{Value: 80.0, Kind: MetricKindGauge},
				EfficiencyPercent: &MetricValue{Value: 94.1, Kind: MetricKindGauge},
				FanMetrics: []FanMetrics{
					{
						ComponentInfo: ComponentInfo{
							Index:  0,
							Name:   "PSU Fan 0",
							Status: ComponentStatusHealthy,
						},
						RPM:   &MetricValue{Value: 2500, Kind: MetricKindGauge},
						TempC: &MetricValue{Value: 55.0, Kind: MetricKindGauge},
					},
				},
			},
		},

		// Control boards
		ControlBoardMetrics: []ControlBoardMetrics{
			{
				ComponentInfo: ComponentInfo{
					Index:  0,
					Name:   "ControlBoard 0",
					Status: ComponentStatusHealthy,
				},
			},
		},

		// Device-level fans
		FanMetrics: []FanMetrics{
			{
				ComponentInfo: ComponentInfo{
					Index:  0,
					Name:   "Chassis Fan 0",
					Status: ComponentStatusHealthy,
				},
				RPM:     &MetricValue{Value: 3500, Kind: MetricKindGauge},
				Percent: &MetricValue{Value: 80.0, Kind: MetricKindGauge},
			},
		},

		// Sensors
		SensorMetrics: []SensorMetrics{
			{
				ComponentInfo: ComponentInfo{
					Index:  0,
					Name:   "Humidity Sensor",
					Status: ComponentStatusHealthy,
				},
				Type:  "humidity",
				Unit:  "%",
				Value: &MetricValue{Value: 45.0, Kind: MetricKindGauge},
			},
		},
	}
}

// createMinimalDeviceMetrics creates a DeviceMetrics with only required fields
func createMinimalDeviceMetrics() DeviceMetrics {
	return DeviceMetrics{
		DeviceID:  "minimal-device",
		Timestamp: testTime,
		Health:    HealthUnknown,
	}
}

// ptr is a helper to create pointers to literals
func ptr[T any](v T) *T {
	return &v
}

type fakeDriver struct {
	newDeviceFunc func(ctx context.Context, deviceID string, deviceInfo DeviceInfo, secret SecretBundle) (NewDeviceResult, error)
}

func (f fakeDriver) Handshake(ctx context.Context) (DriverIdentifier, error) {
	return DriverIdentifier{}, nil
}

func (f fakeDriver) DescribeDriver(ctx context.Context) (DriverIdentifier, Capabilities, error) {
	return DriverIdentifier{}, Capabilities{}, nil
}

func (f fakeDriver) DiscoverDevice(ctx context.Context, ipAddress, port string) (DeviceInfo, error) {
	return DeviceInfo{}, nil
}

func (f fakeDriver) PairDevice(ctx context.Context, device DeviceInfo, access SecretBundle) (DeviceInfo, error) {
	return DeviceInfo{}, nil
}

func (f fakeDriver) NewDevice(ctx context.Context, deviceID string, deviceInfo DeviceInfo, secret SecretBundle) (NewDeviceResult, error) {
	return f.newDeviceFunc(ctx, deviceID, deviceInfo, secret)
}

type fakeDevice struct {
	describeDeviceFunc      func(ctx context.Context) (DeviceInfo, Capabilities, error)
	statusFunc              func(ctx context.Context) (DeviceMetrics, error)
	startMiningFunc         func(ctx context.Context) error
	setCoolingModeFunc      func(ctx context.Context, mode CoolingMode) error
	updateMinerPasswordFunc func(ctx context.Context, currentPassword, newPassword string) error
}

func (f fakeDevice) ID() string { return "device-123" }

func (f fakeDevice) DescribeDevice(ctx context.Context) (DeviceInfo, Capabilities, error) {
	return f.describeDeviceFunc(ctx)
}

func (f fakeDevice) Status(ctx context.Context) (DeviceMetrics, error) {
	return f.statusFunc(ctx)
}

func (f fakeDevice) Close(ctx context.Context) error { return nil }
func (f fakeDevice) StartMining(ctx context.Context) error {
	if f.startMiningFunc != nil {
		return f.startMiningFunc(ctx)
	}
	return nil
}
func (f fakeDevice) StopMining(ctx context.Context) error { return nil }
func (f fakeDevice) BlinkLED(ctx context.Context) error   { return nil }
func (f fakeDevice) Reboot(ctx context.Context) error     { return nil }

type fakeCurtailingDevice struct {
	fakeDevice
	curtailFunc   func(ctx context.Context, req CurtailRequest) error
	uncurtailFunc func(ctx context.Context, req UncurtailRequest) error
}

func (f fakeCurtailingDevice) Curtail(ctx context.Context, req CurtailRequest) error {
	if f.curtailFunc != nil {
		return f.curtailFunc(ctx, req)
	}
	return nil
}

func (f fakeCurtailingDevice) Uncurtail(ctx context.Context, req UncurtailRequest) error {
	if f.uncurtailFunc != nil {
		return f.uncurtailFunc(ctx, req)
	}
	return nil
}

func (f fakeDevice) SetCoolingMode(ctx context.Context, mode CoolingMode) error {
	if f.setCoolingModeFunc != nil {
		return f.setCoolingModeFunc(ctx, mode)
	}
	return nil
}
func (f fakeDevice) GetCoolingMode(ctx context.Context) (CoolingMode, error) {
	return CoolingModeUnspecified, nil
}
func (f fakeDevice) SetPowerTarget(ctx context.Context, performanceMode PerformanceMode) error {
	return nil
}
func (f fakeDevice) UpdateMiningPools(ctx context.Context, pools []MiningPoolConfig) error {
	return nil
}
func (f fakeDevice) UpdateMinerPassword(ctx context.Context, currentPassword string, newPassword string) error {
	if f.updateMinerPasswordFunc != nil {
		return f.updateMinerPasswordFunc(ctx, currentPassword, newPassword)
	}
	return nil
}
func (f fakeDevice) GetMiningPools(ctx context.Context) ([]ConfiguredPool, error) { return nil, nil }
func (f fakeDevice) DownloadLogs(ctx context.Context, since *time.Time, batchLogUUID string) (string, bool, error) {
	return "", false, nil
}
func (f fakeDevice) FirmwareUpdate(ctx context.Context, firmware FirmwareFile) error { return nil }
func (f fakeDevice) Unpair(ctx context.Context) error                                { return nil }
func (f fakeDevice) GetErrors(ctx context.Context) (DeviceErrors, error)             { return DeviceErrors{}, nil }
func (f fakeDevice) TryBatchStatus(ctx context.Context, ids []string) (map[string]DeviceMetrics, bool, error) {
	return nil, false, nil
}
func (f fakeDevice) TrySubscribe(ctx context.Context, ids []string) (<-chan DeviceMetrics, bool, error) {
	return nil, false, nil
}
func (f fakeDevice) TryGetWebViewURL(ctx context.Context) (string, bool, error) {
	return "", false, nil
}
func (f fakeDevice) TryGetTimeSeriesData(
	ctx context.Context,
	metricNames []string,
	startTime, endTime time.Time,
	granularity *time.Duration,
	maxPoints int32,
	pageToken string,
) ([]DeviceMetrics, string, bool, error) {
	return nil, "", false, nil
}

// ============================================================================
// DeviceMetrics Round-Trip Conversion Tests
// ============================================================================

func TestDeviceMetricsRoundTrip_Full(t *testing.T) {
	original := createFullDeviceMetrics()

	// Convert to protobuf
	pbMetrics := deviceMetricsToProto(original)
	require.NotNil(t, pbMetrics)

	// Convert back to SDK
	converted := deviceMetricsFromProto(pbMetrics)

	// Verify basic fields
	assert.Equal(t, original.DeviceID, converted.DeviceID)
	assert.Equal(t, original.Timestamp.Unix(), converted.Timestamp.Unix())
	assert.Equal(t, original.Health, converted.Health)
	assert.Equal(t, *original.HealthReason, *converted.HealthReason)

	// Verify device-level metrics
	assertMetricValueEqual(t, original.HashrateHS, converted.HashrateHS)
	assertMetricValueEqual(t, original.TempC, converted.TempC)
	assertMetricValueEqual(t, original.FanRPM, converted.FanRPM)
	assertMetricValueEqual(t, original.PowerW, converted.PowerW)
	assertMetricValueEqual(t, original.EfficiencyJH, converted.EfficiencyJH)

	// Verify component counts
	assert.Len(t, converted.HashBoards, len(original.HashBoards))
	assert.Len(t, converted.PSUMetrics, len(original.PSUMetrics))
	assert.Len(t, converted.ControlBoardMetrics, len(original.ControlBoardMetrics))
	assert.Len(t, converted.FanMetrics, len(original.FanMetrics))
	assert.Len(t, converted.SensorMetrics, len(original.SensorMetrics))

	// Verify hash board details
	if len(converted.HashBoards) > 0 {
		origHB := original.HashBoards[0]
		convHB := converted.HashBoards[0]

		assert.Equal(t, origHB.Index, convHB.Index)
		assert.Equal(t, origHB.Name, convHB.Name)
		assert.Equal(t, origHB.Status, convHB.Status)
		assert.Equal(t, *origHB.SerialNumber, *convHB.SerialNumber)
		assertMetricValueEqual(t, origHB.HashRateHS, convHB.HashRateHS)
		assertMetricValueEqual(t, origHB.TempC, convHB.TempC)

		// Verify metadata
		assert.NotNil(t, convHB.TempC.MetaData)
		assert.Equal(t, *origHB.TempC.MetaData.Window, *convHB.TempC.MetaData.Window)
		assert.InDelta(t, *origHB.TempC.MetaData.Min, *convHB.TempC.MetaData.Min, 1e-10)
		assert.InDelta(t, *origHB.TempC.MetaData.Max, *convHB.TempC.MetaData.Max, 1e-10)

		// Verify nested ASICs
		assert.Len(t, convHB.ASICs, len(origHB.ASICs))
		if len(convHB.ASICs) > 0 {
			assert.Equal(t, origHB.ASICs[0].Index, convHB.ASICs[0].Index)
			assertMetricValueEqual(t, origHB.ASICs[0].TempC, convHB.ASICs[0].TempC)
		}

		// Verify nested fans
		assert.Len(t, convHB.FanMetrics, len(origHB.FanMetrics))
	}

	// Verify PSU details
	if len(converted.PSUMetrics) > 0 {
		origPSU := original.PSUMetrics[0]
		convPSU := converted.PSUMetrics[0]

		assert.Equal(t, origPSU.Index, convPSU.Index)
		assertMetricValueEqual(t, origPSU.OutputPowerW, convPSU.OutputPowerW)
		assertMetricValueEqual(t, origPSU.InputPowerW, convPSU.InputPowerW)
		assertMetricValueEqual(t, origPSU.EfficiencyPercent, convPSU.EfficiencyPercent)

		// Verify PSU nested fans
		assert.Len(t, convPSU.FanMetrics, len(origPSU.FanMetrics))
	}

	// Verify sensors
	if len(converted.SensorMetrics) > 0 {
		origSensor := original.SensorMetrics[0]
		convSensor := converted.SensorMetrics[0]

		assert.Equal(t, origSensor.Type, convSensor.Type)
		assert.Equal(t, origSensor.Unit, convSensor.Unit)
		assertMetricValueEqual(t, origSensor.Value, convSensor.Value)
	}
}

func TestDeviceMetricsRoundTrip_Minimal(t *testing.T) {
	original := createMinimalDeviceMetrics()

	pbMetrics := deviceMetricsToProto(original)
	converted := deviceMetricsFromProto(pbMetrics)

	assert.Equal(t, original.DeviceID, converted.DeviceID)
	assert.Equal(t, original.Timestamp.Unix(), converted.Timestamp.Unix())
	assert.Equal(t, original.Health, converted.Health)
	assert.Nil(t, converted.HealthReason)
	assert.Nil(t, converted.HashrateHS)
	assert.Empty(t, converted.HashBoards)
	assert.Empty(t, converted.PSUMetrics)
}

func TestDeviceMetricsRoundTrip_EmptyArrays(t *testing.T) {
	original := DeviceMetrics{
		DeviceID:            "test",
		Timestamp:           testTime,
		Health:              HealthHealthyInactive,
		HashBoards:          []HashBoardMetrics{},
		PSUMetrics:          []PSUMetrics{},
		ControlBoardMetrics: []ControlBoardMetrics{},
		FanMetrics:          []FanMetrics{},
		SensorMetrics:       []SensorMetrics{},
	}

	pbMetrics := deviceMetricsToProto(original)
	converted := deviceMetricsFromProto(pbMetrics)

	assert.Empty(t, converted.HashBoards)
	assert.Empty(t, converted.PSUMetrics)
}

// ============================================================================
// Component Conversion Tests
// ============================================================================

func TestHashBoardMetrics_AllFields(t *testing.T) {
	window := 10 * time.Second
	serialNumber := "HB-TEST-001"
	chipCount := int32(100)

	original := HashBoardMetrics{
		ComponentInfo: ComponentInfo{
			Index:  1,
			Name:   "Test HashBoard",
			Status: ComponentStatusWarning,
		},
		SerialNumber:     &serialNumber,
		HashRateHS:       &MetricValue{Value: 75e12, Kind: MetricKindRate},
		TempC:            &MetricValue{Value: 72.5, Kind: MetricKindGauge},
		VoltageV:         &MetricValue{Value: 12.5, Kind: MetricKindGauge},
		CurrentA:         &MetricValue{Value: 15.3, Kind: MetricKindGauge},
		InletTempC:       &MetricValue{Value: 65.0, Kind: MetricKindGauge},
		OutletTempC:      &MetricValue{Value: 80.0, Kind: MetricKindGauge},
		AmbientTempC:     &MetricValue{Value: 28.0, Kind: MetricKindGauge},
		ChipCount:        &chipCount,
		ChipFrequencyMHz: &MetricValue{Value: 700.0, Kind: MetricKindGauge, MetaData: &MetricValueMetaData{Window: &window}},
		ASICs:            []ASICMetrics{},
		FanMetrics:       []FanMetrics{},
	}

	pb := hashBoardMetricsToProto(original)
	converted := hashBoardMetricsFromProto(pb)

	assert.Equal(t, original.Index, converted.Index)
	assert.Equal(t, original.Name, converted.Name)
	assert.Equal(t, original.Status, converted.Status)
	assert.Equal(t, *original.SerialNumber, *converted.SerialNumber)
	assert.Equal(t, *original.ChipCount, *converted.ChipCount)
	assertMetricValueEqual(t, original.HashRateHS, converted.HashRateHS)
	assertMetricValueEqual(t, original.TempC, converted.TempC)
	assertMetricValueEqual(t, original.ChipFrequencyMHz, converted.ChipFrequencyMHz)

	// Verify metadata was preserved
	require.NotNil(t, converted.ChipFrequencyMHz.MetaData)
	assert.Equal(t, window, *converted.ChipFrequencyMHz.MetaData.Window)
}

func TestASICMetrics_AllFields(t *testing.T) {
	original := ASICMetrics{
		ComponentInfo: ComponentInfo{
			Index:  5,
			Name:   "ASIC Chip 5",
			Status: ComponentStatusHealthy,
		},
		TempC:        &MetricValue{Value: 68.0, Kind: MetricKindGauge},
		FrequencyMHz: &MetricValue{Value: 680.0, Kind: MetricKindGauge},
		VoltageV:     &MetricValue{Value: 0.88, Kind: MetricKindGauge},
		HashrateHS:   &MetricValue{Value: 2e12, Kind: MetricKindRate},
	}

	pb := asicMetricsToProto(original)
	converted := asicMetricsFromProto(pb)

	assert.Equal(t, original.Index, converted.Index)
	assert.Equal(t, original.Name, converted.Name)
	assert.Equal(t, original.Status, converted.Status)
	assertMetricValueEqual(t, original.TempC, converted.TempC)
	assertMetricValueEqual(t, original.FrequencyMHz, converted.FrequencyMHz)
	assertMetricValueEqual(t, original.VoltageV, converted.VoltageV)
	assertMetricValueEqual(t, original.HashrateHS, converted.HashrateHS)
}

func TestPSUMetrics_WithNestedFans(t *testing.T) {
	original := PSUMetrics{
		ComponentInfo: ComponentInfo{
			Index:  0,
			Name:   "PSU Main",
			Status: ComponentStatusHealthy,
		},
		OutputPowerW:      &MetricValue{Value: 3500.0, Kind: MetricKindGauge},
		OutputVoltageV:    &MetricValue{Value: 12.1, Kind: MetricKindGauge},
		OutputCurrentA:    &MetricValue{Value: 289.0, Kind: MetricKindGauge},
		InputPowerW:       &MetricValue{Value: 3700.0, Kind: MetricKindGauge},
		InputVoltageV:     &MetricValue{Value: 240.0, Kind: MetricKindGauge},
		InputCurrentA:     &MetricValue{Value: 15.4, Kind: MetricKindGauge},
		HotSpotTempC:      &MetricValue{Value: 85.0, Kind: MetricKindGauge},
		EfficiencyPercent: &MetricValue{Value: 94.6, Kind: MetricKindGauge},
		FanMetrics: []FanMetrics{
			{
				ComponentInfo: ComponentInfo{Index: 0, Name: "PSU Fan", Status: ComponentStatusHealthy},
				RPM:           &MetricValue{Value: 2800, Kind: MetricKindGauge},
			},
		},
	}

	pb := psuMetricsToProto(original)
	converted := psuMetricsFromProto(pb)

	assert.Equal(t, original.Index, converted.Index)
	assertMetricValueEqual(t, original.OutputPowerW, converted.OutputPowerW)
	assertMetricValueEqual(t, original.InputPowerW, converted.InputPowerW)
	assertMetricValueEqual(t, original.EfficiencyPercent, converted.EfficiencyPercent)

	require.Len(t, converted.FanMetrics, 1)
	assert.Equal(t, original.FanMetrics[0].Index, converted.FanMetrics[0].Index)
	assertMetricValueEqual(t, original.FanMetrics[0].RPM, converted.FanMetrics[0].RPM)
}

func TestFanMetrics_AllFields(t *testing.T) {
	original := FanMetrics{
		ComponentInfo: ComponentInfo{
			Index:  2,
			Name:   "Exhaust Fan 2",
			Status: ComponentStatusWarning,
		},
		RPM:     &MetricValue{Value: 4500, Kind: MetricKindGauge},
		TempC:   &MetricValue{Value: 50.0, Kind: MetricKindGauge},
		Percent: &MetricValue{Value: 90.0, Kind: MetricKindGauge},
	}

	pb := fanMetricsToProto(original)
	converted := fanMetricsFromProto(pb)

	assert.Equal(t, original.Index, converted.Index)
	assert.Equal(t, original.Name, converted.Name)
	assert.Equal(t, original.Status, converted.Status)
	assertMetricValueEqual(t, original.RPM, converted.RPM)
	assertMetricValueEqual(t, original.TempC, converted.TempC)
	assertMetricValueEqual(t, original.Percent, converted.Percent)
}

func TestSensorMetrics_AllFields(t *testing.T) {
	original := SensorMetrics{
		ComponentInfo: ComponentInfo{
			Index:  0,
			Name:   "Vibration Sensor",
			Status: ComponentStatusHealthy,
		},
		Type:  "vibration",
		Unit:  "g",
		Value: &MetricValue{Value: 0.05, Kind: MetricKindGauge},
	}

	pb := sensorMetricsToProto(original)
	converted := sensorMetricsFromProto(pb)

	assert.Equal(t, original.Index, converted.Index)
	assert.Equal(t, original.Type, converted.Type)
	assert.Equal(t, original.Unit, converted.Unit)
	assertMetricValueEqual(t, original.Value, converted.Value)
}

// ============================================================================
// MetricValue Conversion Tests
// ============================================================================

func TestMetricValue_SimpleValue(t *testing.T) {
	original := MetricValue{
		Value: 42.5,
		Kind:  MetricKindGauge,
	}

	pb := metricValueToProto(original)
	converted := metricValueFromProto(pb)

	assert.InDelta(t, original.Value, converted.Value, 1e-10)
	assert.Equal(t, original.Kind, converted.Kind)
	assert.Nil(t, converted.MetaData)
}

func TestMetricValue_WithFullMetadata(t *testing.T) {
	window := 30 * time.Second
	mdTimestamp := testTime.Add(5 * time.Second)

	original := MetricValue{
		Value: 100.0,
		Kind:  MetricKindRate,
		MetaData: &MetricValueMetaData{
			Window:    &window,
			Min:       ptr(95.0),
			Max:       ptr(105.0),
			Avg:       ptr(100.0),
			StdDev:    ptr(2.5),
			Timestamp: &mdTimestamp,
		},
	}

	pb := metricValueToProto(original)
	converted := metricValueFromProto(pb)

	assert.InDelta(t, original.Value, converted.Value, 1e-10)
	assert.Equal(t, original.Kind, converted.Kind)

	require.NotNil(t, converted.MetaData)
	assert.Equal(t, *original.MetaData.Window, *converted.MetaData.Window)
	assert.InDelta(t, *original.MetaData.Min, *converted.MetaData.Min, 1e-10)
	assert.InDelta(t, *original.MetaData.Max, *converted.MetaData.Max, 1e-10)
	assert.InDelta(t, *original.MetaData.Avg, *converted.MetaData.Avg, 1e-10)
	assert.InDelta(t, *original.MetaData.StdDev, *converted.MetaData.StdDev, 1e-10)
	assert.Equal(t, original.MetaData.Timestamp.Unix(), converted.MetaData.Timestamp.Unix())
}

func TestMetricValue_WithPartialMetadata(t *testing.T) {
	window := 15 * time.Second

	original := MetricValue{
		Value: 75.0,
		Kind:  MetricKindCounter,
		MetaData: &MetricValueMetaData{
			Window: &window,
			Min:    ptr(70.0),
			Max:    ptr(80.0),
			// No Avg, StdDev, or Timestamp
		},
	}

	pb := metricValueToProto(original)
	converted := metricValueFromProto(pb)

	require.NotNil(t, converted.MetaData)
	assert.Equal(t, *original.MetaData.Window, *converted.MetaData.Window)
	assert.InDelta(t, *original.MetaData.Min, *converted.MetaData.Min, 1e-10)
	assert.InDelta(t, *original.MetaData.Max, *converted.MetaData.Max, 1e-10)
	assert.Nil(t, converted.MetaData.Avg)
	assert.Nil(t, converted.MetaData.StdDev)
	assert.Nil(t, converted.MetaData.Timestamp)
}

func TestMetricValue_DifferentKinds(t *testing.T) {
	tests := []struct {
		name string
		kind MetricKind
	}{
		{"gauge", MetricKindGauge},
		{"rate", MetricKindRate},
		{"counter", MetricKindCounter},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			original := MetricValue{Value: 123.45, Kind: tt.kind}
			pb := metricValueToProto(original)
			converted := metricValueFromProto(pb)

			assert.Equal(t, original.Kind, converted.Kind)
		})
	}
}

// ============================================================================
// Enum Conversion Tests
// ============================================================================

func TestHealthStatus_AllValues(t *testing.T) {
	tests := []struct {
		name   string
		status HealthStatus
	}{
		{"unknown", HealthUnknown},
		{"healthy_active", HealthHealthyActive},
		{"healthy_inactive", HealthHealthyInactive},
		{"warning", HealthWarning},
		{"critical", HealthCritical},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dm := DeviceMetrics{
				DeviceID:  "test",
				Timestamp: testTime,
				Health:    tt.status,
			}

			pb := deviceMetricsToProto(dm)
			converted := deviceMetricsFromProto(pb)

			assert.Equal(t, tt.status, converted.Health)
		})
	}
}

func TestComponentStatus_AllValues(t *testing.T) {
	tests := []struct {
		name   string
		status ComponentStatus
	}{
		{"unknown", ComponentStatusUnknown},
		{"healthy", ComponentStatusHealthy},
		{"warning", ComponentStatusWarning},
		{"critical", ComponentStatusCritical},
		{"offline", ComponentStatusOffline},
		{"disabled", ComponentStatusDisabled},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fan := FanMetrics{
				ComponentInfo: ComponentInfo{
					Index:  0,
					Name:   "Test",
					Status: tt.status,
				},
			}

			pb := fanMetricsToProto(fan)
			converted := fanMetricsFromProto(pb)

			assert.Equal(t, tt.status, converted.Status)
		})
	}
}

// ============================================================================
// Edge Cases & Error Handling Tests
// ============================================================================

func TestDeviceMetrics_NilMetricValues(t *testing.T) {
	dm := DeviceMetrics{
		DeviceID:  "test",
		Timestamp: testTime,
		Health:    HealthHealthyActive,
		// All metric values are nil
		HashrateHS:   nil,
		TempC:        nil,
		FanRPM:       nil,
		PowerW:       nil,
		EfficiencyJH: nil,
	}

	pb := deviceMetricsToProto(dm)
	converted := deviceMetricsFromProto(pb)

	assert.Nil(t, converted.HashrateHS)
	assert.Nil(t, converted.TempC)
	assert.Nil(t, converted.FanRPM)
	assert.Nil(t, converted.PowerW)
	assert.Nil(t, converted.EfficiencyJH)
}

func TestMetricValue_ExtremeValues(t *testing.T) {
	tests := []struct {
		name  string
		value float64
	}{
		{"zero", 0.0},
		{"negative", -100.5},
		{"very_large", 1.7976931348623157e+308},
		{"very_small", 2.2250738585072014e-308},
		{"negative_large", -1.7976931348623157e+308},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			original := MetricValue{Value: tt.value, Kind: MetricKindGauge}
			pb := metricValueToProto(original)
			converted := metricValueFromProto(pb)

			assert.InDelta(t, original.Value, converted.Value, 1e-10)
		})
	}
}

func TestComponentInfo_EmptyOptionalFields(t *testing.T) {
	original := ComponentInfo{
		Index:  0,
		Name:   "Test",
		Status: ComponentStatusHealthy,
		// StatusReason and Timestamp are nil
	}

	pb := componentInfoToProto(original)
	converted := componentInfoFromProto(pb)

	assert.Equal(t, original.Index, converted.Index)
	assert.Equal(t, original.Name, converted.Name)
	assert.Equal(t, original.Status, converted.Status)
	assert.Nil(t, converted.StatusReason)
	assert.Nil(t, converted.Timestamp)
}

func TestDeviceMetrics_DeeplyNestedComponents(t *testing.T) {
	// Create a hash board with nested ASICs and fans
	dm := DeviceMetrics{
		DeviceID:  "test",
		Timestamp: testTime,
		Health:    HealthHealthyActive,
		HashBoards: []HashBoardMetrics{
			{
				ComponentInfo: ComponentInfo{Index: 0, Name: "HB0", Status: ComponentStatusHealthy},
				ASICs: []ASICMetrics{
					{ComponentInfo: ComponentInfo{Index: 0, Name: "ASIC0", Status: ComponentStatusHealthy}},
					{ComponentInfo: ComponentInfo{Index: 1, Name: "ASIC1", Status: ComponentStatusHealthy}},
				},
				FanMetrics: []FanMetrics{
					{ComponentInfo: ComponentInfo{Index: 0, Name: "Fan0", Status: ComponentStatusHealthy}},
				},
			},
		},
		PSUMetrics: []PSUMetrics{
			{
				ComponentInfo: ComponentInfo{Index: 0, Name: "PSU0", Status: ComponentStatusHealthy},
				FanMetrics: []FanMetrics{
					{ComponentInfo: ComponentInfo{Index: 0, Name: "PSUFan0", Status: ComponentStatusHealthy}},
					{ComponentInfo: ComponentInfo{Index: 1, Name: "PSUFan1", Status: ComponentStatusHealthy}},
				},
			},
		},
	}

	pb := deviceMetricsToProto(dm)
	converted := deviceMetricsFromProto(pb)

	require.Len(t, converted.HashBoards, 1)
	assert.Len(t, converted.HashBoards[0].ASICs, 2)
	assert.Len(t, converted.HashBoards[0].FanMetrics, 1)

	require.Len(t, converted.PSUMetrics, 1)
	assert.Len(t, converted.PSUMetrics[0].FanMetrics, 2)
}

// ============================================================================
// Benchmark Tests
// ============================================================================

func BenchmarkDeviceMetricsConversion(b *testing.B) {
	dm := createFullDeviceMetrics()

	b.Run("to_proto", func(b *testing.B) {
		for range b.N {
			_ = deviceMetricsToProto(dm)
		}
	})

	pb := deviceMetricsToProto(dm)
	b.Run("from_proto", func(b *testing.B) {
		for range b.N {
			_ = deviceMetricsFromProto(pb)
		}
	})

	b.Run("round_trip", func(b *testing.B) {
		for range b.N {
			pb := deviceMetricsToProto(dm)
			_ = deviceMetricsFromProto(pb)
		}
	})
}

func BenchmarkMetricValueConversion(b *testing.B) {
	window := 5 * time.Second
	mv := MetricValue{
		Value: 100.0,
		Kind:  MetricKindRate,
		MetaData: &MetricValueMetaData{
			Window: &window,
			Min:    ptr(95.0),
			Max:    ptr(105.0),
			Avg:    ptr(100.0),
			StdDev: ptr(2.5),
		},
	}

	b.Run("to_proto", func(b *testing.B) {
		for range b.N {
			_ = metricValueToProto(mv)
		}
	})

	pb := metricValueToProto(mv)
	b.Run("from_proto", func(b *testing.B) {
		for range b.N {
			_ = metricValueFromProto(pb)
		}
	})
}

// ============================================================================
// Helper Assertion Functions
// ============================================================================

// assertMetricValueEqual compares two MetricValue pointers
func assertMetricValueEqual(t *testing.T, expected, actual *MetricValue) {
	t.Helper()

	if expected == nil {
		assert.Nil(t, actual)
		return
	}

	require.NotNil(t, actual)
	assert.InDelta(t, expected.Value, actual.Value, 1e-10)
	assert.Equal(t, expected.Kind, actual.Kind)

	if expected.MetaData == nil {
		assert.Nil(t, actual.MetaData)
		return
	}

	require.NotNil(t, actual.MetaData)

	if expected.MetaData.Window != nil {
		require.NotNil(t, actual.MetaData.Window)
		assert.Equal(t, *expected.MetaData.Window, *actual.MetaData.Window)
	}

	if expected.MetaData.Min != nil {
		require.NotNil(t, actual.MetaData.Min)
		assert.InDelta(t, *expected.MetaData.Min, *actual.MetaData.Min, 1e-10)
	}

	if expected.MetaData.Max != nil {
		require.NotNil(t, actual.MetaData.Max)
		assert.InDelta(t, *expected.MetaData.Max, *actual.MetaData.Max, 1e-10)
	}

	if expected.MetaData.Avg != nil {
		require.NotNil(t, actual.MetaData.Avg)
		assert.InDelta(t, *expected.MetaData.Avg, *actual.MetaData.Avg, 1e-10)
	}

	if expected.MetaData.StdDev != nil {
		require.NotNil(t, actual.MetaData.StdDev)
		assert.InDelta(t, *expected.MetaData.StdDev, *actual.MetaData.StdDev, 1e-10)
	}

	if expected.MetaData.Timestamp != nil {
		require.NotNil(t, actual.MetaData.Timestamp)
		assert.Equal(t, expected.MetaData.Timestamp.Unix(), actual.MetaData.Timestamp.Unix())
	}
}

// ============================================================================
// SDK Error Tests
// ============================================================================

func TestNewErrorAuthenticationFailed(t *testing.T) {
	tests := []struct {
		name             string
		deviceID         string
		underlyingErr    error
		expectedCode     ErrorCode
		expectedContains string
	}{
		{
			name:             "basic_auth_error",
			deviceID:         "device-123",
			underlyingErr:    nil,
			expectedCode:     ErrCodeAuthenticationFailed,
			expectedContains: "device-123",
		},
		{
			name:             "auth_error_with_underlying",
			deviceID:         "proto-miner-456",
			underlyingErr:    errors.New("invalid JWT token"),
			expectedCode:     ErrCodeAuthenticationFailed,
			expectedContains: "proto-miner-456",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var sdkErr SDKError
			if tt.underlyingErr != nil {
				sdkErr = NewErrorAuthenticationFailed(tt.deviceID, tt.underlyingErr)
			} else {
				sdkErr = NewErrorAuthenticationFailed(tt.deviceID)
			}

			assert.Equal(t, tt.expectedCode, sdkErr.Code)
			assert.Contains(t, sdkErr.Message, tt.expectedContains)
			assert.Contains(t, sdkErr.Error(), tt.expectedContains)

			if tt.underlyingErr != nil {
				assert.Equal(t, tt.underlyingErr, sdkErr.Unwrap())
			} else {
				assert.Nil(t, sdkErr.Unwrap())
			}
		})
	}
}

func TestSDKErrorToGRPCStatus_AuthenticationFailed(t *testing.T) {
	sdkErr := NewErrorAuthenticationFailed("device-123", errors.New("missing api key"))

	grpcErr := sdkErrorToGRPCStatus(sdkErr)

	require.Error(t, grpcErr)
	assert.Contains(t, grpcErr.Error(), "authentication failed")

	// Extract the gRPC status from the wrapped error
	st, ok := status.FromError(errors.Unwrap(grpcErr))
	require.True(t, ok, "should be able to extract gRPC status")
	assert.Equal(t, codes.Unauthenticated, st.Code())
}

func TestSDKErrorToGRPCStatus_AllErrorCodes(t *testing.T) {
	tests := []struct {
		name         string
		sdkErr       SDKError
		expectedCode codes.Code
	}{
		{
			name:         "device_not_found",
			sdkErr:       NewErrorDeviceNotFound("device-123"),
			expectedCode: codes.NotFound,
		},
		{
			name:         "unsupported_capability",
			sdkErr:       NewErrUnsupportedCapability("streaming"),
			expectedCode: codes.Unimplemented,
		},
		{
			name:         "invalid_config",
			sdkErr:       NewErrorInvalidConfig("device-123"),
			expectedCode: codes.InvalidArgument,
		},
		{
			name:         "device_unavailable",
			sdkErr:       NewErrorDeviceUnavailable("device-123"),
			expectedCode: codes.Unavailable,
		},
		{
			name:         "driver_shutdown",
			sdkErr:       NewErrorDriverShutdown(),
			expectedCode: codes.Aborted,
		},
		{
			name:         "authentication_failed",
			sdkErr:       NewErrorAuthenticationFailed("device-123"),
			expectedCode: codes.Unauthenticated,
		},
		{
			name:         "curtail_capability_not_supported",
			sdkErr:       NewErrCurtailCapabilityNotSupported("device-123", int32(CurtailLevelEfficiency)),
			expectedCode: codes.Unimplemented,
		},
		{
			name:         "curtail_transient",
			sdkErr:       NewErrCurtailTransient("device-123", errors.New("timeout")),
			expectedCode: codes.Unavailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			grpcErr := sdkErrorToGRPCStatus(tt.sdkErr)

			require.Error(t, grpcErr)

			st, ok := status.FromError(errors.Unwrap(grpcErr))
			require.True(t, ok, "should be able to extract gRPC status from %v", grpcErr)
			assert.Equal(t, tt.expectedCode, st.Code())
		})
	}
}

func TestDriverGRPCServer_CurtailMapsSDKErrorStatus(t *testing.T) {
	device := fakeCurtailingDevice{
		curtailFunc: func(_ context.Context, req CurtailRequest) error {
			assert.Equal(t, CurtailLevelFull, req.Level)
			return NewErrCurtailCapabilityNotSupported("device-123", int32(req.Level))
		},
	}
	server := &DriverGRPCServer{
		devices: map[string]Device{"device-123": device},
	}

	_, err := server.Curtail(context.Background(), &pb.CurtailRequest{
		Ref:   &pb.DeviceRef{DeviceId: "device-123"},
		Level: pb.CurtailLevel_CURTAIL_LEVEL_FULL,
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok, "should be able to extract gRPC status from %v", err)
	assert.Equal(t, codes.Unimplemented, st.Code())
}

func TestDriverGRPCServer_CurtailRequiresDeviceRef(t *testing.T) {
	server := &DriverGRPCServer{
		devices: map[string]Device{"device-123": fakeCurtailingDevice{}},
	}

	_, err := server.Curtail(context.Background(), &pb.CurtailRequest{
		Level: pb.CurtailLevel_CURTAIL_LEVEL_FULL,
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok, "should be able to extract gRPC status from %v", err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestDriverGRPCServer_UncurtailMapsSDKErrorStatus(t *testing.T) {
	device := fakeCurtailingDevice{
		uncurtailFunc: func(_ context.Context, _ UncurtailRequest) error {
			return NewErrCurtailTransient("device-123", errors.New("temporary network failure"))
		},
	}
	server := &DriverGRPCServer{
		devices: map[string]Device{"device-123": device},
	}

	_, err := server.Uncurtail(context.Background(), &pb.UncurtailRequest{
		Ref: &pb.DeviceRef{DeviceId: "device-123"},
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok, "should be able to extract gRPC status from %v", err)
	assert.Equal(t, codes.Unavailable, st.Code())
}

func TestDriverGRPCServer_UncurtailRequiresDeviceRef(t *testing.T) {
	server := &DriverGRPCServer{
		devices: map[string]Device{"device-123": fakeCurtailingDevice{}},
	}

	_, err := server.Uncurtail(context.Background(), &pb.UncurtailRequest{})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok, "should be able to extract gRPC status from %v", err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestDriverGRPCServer_CurtailReturnsUnimplementedWhenDeviceLacksCurtailment(t *testing.T) {
	server := &DriverGRPCServer{
		devices: map[string]Device{"device-123": fakeDevice{}},
	}

	_, err := server.Curtail(context.Background(), &pb.CurtailRequest{
		Ref:   &pb.DeviceRef{DeviceId: "device-123"},
		Level: pb.CurtailLevel_CURTAIL_LEVEL_FULL,
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok, "should be able to extract gRPC status from %v", err)
	assert.Equal(t, codes.Unimplemented, st.Code())
	assert.Contains(t, st.Message(), "device does not support curtailment")
}

func TestDriverGRPCServer_UncurtailReturnsUnimplementedWhenDeviceLacksCurtailment(t *testing.T) {
	server := &DriverGRPCServer{
		devices: map[string]Device{"device-123": fakeDevice{}},
	}

	_, err := server.Uncurtail(context.Background(), &pb.UncurtailRequest{
		Ref: &pb.DeviceRef{DeviceId: "device-123"},
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok, "should be able to extract gRPC status from %v", err)
	assert.Equal(t, codes.Unimplemented, st.Code())
	assert.Contains(t, st.Message(), "device does not support curtailment")
}

// Control RPCs should preserve SDKError status codes across gRPC.
func TestDriverGRPCServer_ControlRPCsMapSDKErrorStatus(t *testing.T) {
	tests := []struct {
		name         string
		device       fakeDevice
		invoke       func(server *DriverGRPCServer) error
		expectedCode codes.Code
	}{
		{
			name: "StartMining maps Unauthenticated",
			device: fakeDevice{
				startMiningFunc: func(_ context.Context) error {
					return NewErrorAuthenticationFailed("device-123")
				},
			},
			invoke: func(server *DriverGRPCServer) error {
				_, err := server.StartMining(context.Background(), &pb.DeviceRef{DeviceId: "device-123"})
				return err
			},
			expectedCode: codes.Unauthenticated,
		},
		{
			name: "SetCoolingMode maps DeviceUnavailable",
			device: fakeDevice{
				setCoolingModeFunc: func(_ context.Context, _ CoolingMode) error {
					return NewErrorDeviceUnavailable("device-123")
				},
			},
			invoke: func(server *DriverGRPCServer) error {
				_, err := server.SetCoolingMode(context.Background(), &pb.SetCoolingModeRequest{
					Ref: &pb.DeviceRef{DeviceId: "device-123"},
				})
				return err
			},
			expectedCode: codes.Unavailable,
		},
		{
			name: "UpdateMinerPassword maps UnsupportedCapability",
			device: fakeDevice{
				updateMinerPasswordFunc: func(_ context.Context, _, _ string) error {
					return NewErrUnsupportedCapability("update_miner_password")
				},
			},
			invoke: func(server *DriverGRPCServer) error {
				_, err := server.UpdateMinerPassword(context.Background(), &pb.UpdateMinerPasswordRequest{
					Ref: &pb.DeviceRef{DeviceId: "device-123"},
				})
				return err
			},
			expectedCode: codes.Unimplemented,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := &DriverGRPCServer{
				devices: map[string]Device{"device-123": tt.device},
			}
			err := tt.invoke(server)
			require.Error(t, err)
			st, ok := status.FromError(err)
			require.True(t, ok, "should be able to extract gRPC status from %v", err)
			assert.Equal(t, tt.expectedCode, st.Code())
		})
	}
}

func TestSDKError_ErrorsAs(t *testing.T) {
	originalErr := errors.New("original cause")
	sdkErr := NewErrorAuthenticationFailed("device-123", originalErr)

	wrappedErr := errors.New("wrapped: " + sdkErr.Error())

	var extractedErr SDKError
	assert.True(t, errors.As(sdkErr, &extractedErr))
	assert.Equal(t, ErrCodeAuthenticationFailed, extractedErr.Code)

	assert.False(t, errors.As(wrappedErr, &extractedErr))
}
