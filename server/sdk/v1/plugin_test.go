package sdk

import (
	"testing"
	"time"

	pb "github.com/btc-mining/proto-fleet/server/sdk/v1/pb/generated"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test constants for deterministic testing
var (
	testTime = time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC)
)

// Helper functions for creating test data
func createTestMetric(name string, value MetricValue) Metric {
	return Metric{
		Name:       name,
		Value:      value,
		Unit:       UnitWatt,
		Kind:       MetricKindGauge,
		ObservedAt: testTime,
		Window:     time.Second * 5,
		Labels:     map[string]string{"test": "label"},
	}
}

func createTestDeviceStatusResponse(metrics []Metric) DeviceStatusResponse {
	return DeviceStatusResponse{
		DeviceID:     "test-device-123",
		Timestamp:    testTime,
		Summary:      "Test device status",
		Health:       HealthyActive,
		ExtraMetrics: metrics,
	}
}

// TestNewMetricValue tests the factory function for creating MetricValue instances
func TestNewMetricValue(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		wantType ValueType
		wantVal  any
	}{
		{"float64", 42.5, ValueTypeFloat64, 42.5},
		{"int", 100, ValueTypeInt, 100},
		{"int64", int64(200), ValueTypeInt, 200},
		{"bool_true", true, ValueTypeBool, true},
		{"bool_false", false, ValueTypeBool, false},
		{"string", "test", ValueTypeString, "test"},
		{"empty_string", "", ValueTypeString, ""},
		{"zero_float", 0.0, ValueTypeFloat64, 0.0},
		{"zero_int", 0, ValueTypeInt, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var mv MetricValue

			// Use type assertion to call the appropriate NewMetricValue
			switch v := tt.input.(type) {
			case float64:
				mv = NewMetricValue(v)
			case int:
				mv = NewMetricValue(v)
			case int64:
				mv = NewMetricValue(v)
			case bool:
				mv = NewMetricValue(v)
			case string:
				mv = NewMetricValue(v)
			default:
				t.Fatalf("unsupported input type: %T", tt.input)
			}

			// Verify type
			assert.Equal(t, tt.wantType, mv.Type())

			// Verify value using appropriate getter
			switch tt.wantType {
			case ValueTypeFloat64:
				val, ok := mv.AsFloat64()
				require.True(t, ok)
				assert.InDelta(t, tt.wantVal, val, 1e-15)
			case ValueTypeInt:
				val, ok := mv.AsInt()
				require.True(t, ok)
				assert.Equal(t, tt.wantVal, val)
			case ValueTypeBool:
				val, ok := mv.AsBool()
				require.True(t, ok)
				assert.Equal(t, tt.wantVal, val)
			case ValueTypeString:
				val, ok := mv.AsString()
				require.True(t, ok)
				assert.Equal(t, tt.wantVal, val)
			}
		})
	}
}

// TestMetricValueToProto tests conversion from MetricValue to protobuf
func TestMetricValueToProto(t *testing.T) {
	tests := []struct {
		name      string
		value     MetricValue
		wantProto any
	}{
		{
			name:      "float64",
			value:     NewMetricValue(42.5),
			wantProto: &pb.Metric_DoubleValue{DoubleValue: 42.5},
		},
		{
			name:      "int",
			value:     NewMetricValue(100),
			wantProto: &pb.Metric_IntValue{IntValue: 100},
		},
		{
			name:      "bool",
			value:     NewMetricValue(true),
			wantProto: &pb.Metric_BoolValue{BoolValue: true},
		},
		{
			name:      "string",
			value:     NewMetricValue("test"),
			wantProto: &pb.Metric_StringValue{StringValue: "test"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			metric := createTestMetric(tt.name, tt.value)
			response := createTestDeviceStatusResponse([]Metric{metric})

			pbResp := statusResponseToProto(response)

			require.Len(t, pbResp.ExtraMetrics, 1)
			assert.Equal(t, tt.wantProto, pbResp.ExtraMetrics[0].Value)
		})
	}
}

// TestMetricValueFromProto tests conversion from protobuf to MetricValue
func TestMetricValueFromProto(t *testing.T) {
	tests := []struct {
		name     string
		pbValue  func() *pb.Metric
		wantType ValueType
		wantVal  any
	}{
		{
			name: "double_value",
			pbValue: func() *pb.Metric {
				return &pb.Metric{
					Name:  "double_test",
					Value: &pb.Metric_DoubleValue{DoubleValue: 42.5},
				}
			},
			wantType: ValueTypeFloat64,
			wantVal:  42.5,
		},
		{
			name: "int_value",
			pbValue: func() *pb.Metric {
				return &pb.Metric{
					Name:  "int_test",
					Value: &pb.Metric_IntValue{IntValue: 100},
				}
			},
			wantType: ValueTypeInt,
			wantVal:  100,
		},
		{
			name: "bool_value",
			pbValue: func() *pb.Metric {
				return &pb.Metric{
					Name:  "bool_test",
					Value: &pb.Metric_BoolValue{BoolValue: true},
				}
			},
			wantType: ValueTypeBool,
			wantVal:  true,
		},
		{
			name: "string_value",
			pbValue: func() *pb.Metric {
				return &pb.Metric{
					Name:  "string_test",
					Value: &pb.Metric_StringValue{StringValue: "test"},
				}
			},
			wantType: ValueTypeString,
			wantVal:  "test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pbMetric := tt.pbValue()

			pbResp := &pb.DeviceStatusResponse{
				DeviceId:     "test-device",
				ExtraMetrics: []*pb.Metric{pbMetric},
			}

			converted := statusResponseFromProto(pbResp)

			require.Len(t, converted.ExtraMetrics, 1)
			metric := converted.ExtraMetrics[0]

			assert.Equal(t, tt.wantType, metric.Value.Type())

			// Verify value using appropriate getter
			switch tt.wantType {
			case ValueTypeFloat64:
				val, ok := metric.Value.AsFloat64()
				require.True(t, ok)
				assert.InDelta(t, tt.wantVal, val, 1e-15)
			case ValueTypeInt:
				val, ok := metric.Value.AsInt()
				require.True(t, ok)
				assert.Equal(t, tt.wantVal, val)
			case ValueTypeBool:
				val, ok := metric.Value.AsBool()
				require.True(t, ok)
				assert.Equal(t, tt.wantVal, val)
			case ValueTypeString:
				val, ok := metric.Value.AsString()
				require.True(t, ok)
				assert.Equal(t, tt.wantVal, val)
			}
		})
	}
}

// TestMetricValueRoundTrip tests full round-trip conversion
func TestMetricValueRoundTrip(t *testing.T) {
	tests := []struct {
		name  string
		value MetricValue
	}{
		{"float64", NewMetricValue(42.5)},
		{"int", NewMetricValue(100)},
		{"bool_true", NewMetricValue(true)},
		{"bool_false", NewMetricValue(false)},
		{"string", NewMetricValue("test")},
		{"zero_float", NewMetricValue(0.0)},
		{"zero_int", NewMetricValue(0)},
		{"empty_string", NewMetricValue("")},
		{"large_int", NewMetricValue(2147483647)},
		{"negative_int", NewMetricValue(-2147483648)},
		{"large_float", NewMetricValue(1.7976931348623157e+308)},
		{"negative_float", NewMetricValue(-1.7976931348623157e+308)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			original := createTestMetric(tt.name, tt.value)
			response := createTestDeviceStatusResponse([]Metric{original})

			// Convert to protobuf and back
			pbResp := statusResponseToProto(response)
			converted := statusResponseFromProto(pbResp)

			require.Len(t, converted.ExtraMetrics, 1)
			convertedMetric := converted.ExtraMetrics[0]

			// Verify type matches
			assert.Equal(t, original.Value.Type(), convertedMetric.Value.Type())

			// Verify values match using type-specific comparison
			assertMetricValuesEqual(t, original.Value, convertedMetric.Value)
		})
	}
}

// TestMetricValueNilHandling tests handling of nil values
func TestMetricValueNilHandling(t *testing.T) {
	original := Metric{
		Name:  "nil_test",
		Value: nil,
		Unit:  UnitUnspecified,
		Kind:  MetricKindGauge,
	}

	response := createTestDeviceStatusResponse([]Metric{original})

	// Convert to protobuf and back
	pbResp := statusResponseToProto(response)
	converted := statusResponseFromProto(pbResp)

	require.Len(t, converted.ExtraMetrics, 1)
	assert.Nil(t, converted.ExtraMetrics[0].Value)
}

// TestDeviceStatusResponseConversion tests full DeviceStatusResponse conversion
func TestDeviceStatusResponseConversion(t *testing.T) {
	metrics := []Metric{
		createTestMetric("temp", NewMetricValue(65.5)),
		createTestMetric("fan", NewMetricValue(3000)),
		createTestMetric("active", NewMetricValue(true)),
		createTestMetric("status", NewMetricValue("healthy")),
	}

	original := createTestDeviceStatusResponse(metrics)

	// Convert to protobuf and back
	pbResp := statusResponseToProto(original)
	converted := statusResponseFromProto(pbResp)

	// Verify basic fields
	assert.Equal(t, original.DeviceID, converted.DeviceID)
	assert.Equal(t, original.Summary, converted.Summary)
	assert.Equal(t, original.Health, converted.Health)

	// Verify metrics
	require.Len(t, converted.ExtraMetrics, len(original.ExtraMetrics))

	for i, originalMetric := range original.ExtraMetrics {
		convertedMetric := converted.ExtraMetrics[i]
		assert.Equal(t, originalMetric.Name, convertedMetric.Name)
		assert.Equal(t, originalMetric.Unit, convertedMetric.Unit)
		assert.Equal(t, originalMetric.Kind, convertedMetric.Kind)

		if originalMetric.Value != nil {
			require.NotNil(t, convertedMetric.Value)
			assertMetricValuesEqual(t, originalMetric.Value, convertedMetric.Value)
		} else {
			assert.Nil(t, convertedMetric.Value)
		}
	}
}

// Benchmark tests for performance
func BenchmarkNewMetricValue(b *testing.B) {
	benchmarks := []struct {
		name  string
		value any
	}{
		{"float64", 42.5},
		{"int", 100},
		{"bool", true},
		{"string", "test"},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			for range b.N {
				switch v := bm.value.(type) {
				case float64:
					_ = NewMetricValue(v)
				case int:
					_ = NewMetricValue(v)
				case bool:
					_ = NewMetricValue(v)
				case string:
					_ = NewMetricValue(v)
				}
			}
		})
	}
}

func BenchmarkMetricValueConversion(b *testing.B) {
	metric := createTestMetric("bench", NewMetricValue(42.5))
	response := createTestDeviceStatusResponse([]Metric{metric})

	b.Run("to_proto", func(b *testing.B) {
		for range b.N {
			_ = statusResponseToProto(response)
		}
	})

	pbResp := statusResponseToProto(response)
	b.Run("from_proto", func(b *testing.B) {
		for range b.N {
			_ = statusResponseFromProto(pbResp)
		}
	})

	b.Run("round_trip", func(b *testing.B) {
		for range b.N {
			pb := statusResponseToProto(response)
			_ = statusResponseFromProto(pb)
		}
	})
}

// Helper function to assert MetricValue equality
func assertMetricValuesEqual(t *testing.T, expected, actual MetricValue) {
	t.Helper()

	require.Equal(t, expected.Type(), actual.Type(), "MetricValue types should match")

	switch expected.Type() {
	case ValueTypeFloat64:
		expectedVal, _ := expected.AsFloat64()
		actualVal, ok := actual.AsFloat64()
		require.True(t, ok, "should be able to retrieve as float64")
		assert.InDelta(t, expectedVal, actualVal, 1e-15)

	case ValueTypeInt:
		expectedVal, _ := expected.AsInt()
		actualVal, ok := actual.AsInt()
		require.True(t, ok, "should be able to retrieve as int")
		assert.Equal(t, expectedVal, actualVal)

	case ValueTypeBool:
		expectedVal, _ := expected.AsBool()
		actualVal, ok := actual.AsBool()
		require.True(t, ok, "should be able to retrieve as bool")
		assert.Equal(t, expectedVal, actualVal)

	case ValueTypeString:
		expectedVal, _ := expected.AsString()
		actualVal, ok := actual.AsString()
		require.True(t, ok, "should be able to retrieve as string")
		assert.Equal(t, expectedVal, actualVal)

	default:
		t.Fatalf("unsupported MetricValue type: %v", expected.Type())
	}
}
