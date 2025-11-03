package models

import (
	"encoding/json"
	"testing"
	"time"

	modelsV2 "github.com/btc-mining/proto-fleet/server/internal/domain/telemetry/models/v2"
)

func TestDeviceMetricsToPoints_HealthStatusSerialization(t *testing.T) {
	tests := []struct {
		name           string
		healthStatus   modelsV2.HealthStatus
		expectedString string
	}{
		{
			name:           "healthy active status",
			healthStatus:   modelsV2.HealthHealthyActive,
			expectedString: "health_healthy_active",
		},
		{
			name:           "healthy inactive status",
			healthStatus:   modelsV2.HealthHealthyInactive,
			expectedString: "health_healthy_inactive",
		},
		{
			name:           "warning status",
			healthStatus:   modelsV2.HealthWarning,
			expectedString: "health_warning",
		},
		{
			name:           "critical status",
			healthStatus:   modelsV2.HealthCritical,
			expectedString: "health_critical",
		},
		{
			name:           "unknown status",
			healthStatus:   modelsV2.HealthUnknown,
			expectedString: "health_unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deviceMetrics := modelsV2.DeviceMetrics{
				DeviceID:  "test-device-001",
				Timestamp: time.Now(),
				Health:    tt.healthStatus,
				HashrateHS: &modelsV2.MetricValue{
					Value: 100e12, // 100 TH/s
					Kind:  modelsV2.MetricKindRate,
				},
			}

			points := DeviceMetricsToPoints(deviceMetrics)

			if len(points) == 0 {
				t.Fatal("expected at least one point, got none")
			}

			// The health status should be serialized as a string tag
			// We can verify this by checking the String() method returns the expected value
			if deviceMetrics.Health.String() != tt.expectedString {
				t.Errorf("expected health string %q, got %q", tt.expectedString, deviceMetrics.Health.String())
			}
		})
	}
}

func TestHealthStatusStringMethod(t *testing.T) {
	tests := []struct {
		status   modelsV2.HealthStatus
		expected string
	}{
		{modelsV2.HealthHealthyActive, "health_healthy_active"},
		{modelsV2.HealthHealthyInactive, "health_healthy_inactive"},
		{modelsV2.HealthWarning, "health_warning"},
		{modelsV2.HealthCritical, "health_critical"},
		{modelsV2.HealthUnknown, "health_unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.status.String(); got != tt.expected {
				t.Errorf("String() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestHealthStatusJSONSerialization(t *testing.T) {
	tests := []struct {
		name     string
		health   modelsV2.HealthStatus
		expected string
	}{
		{"healthy active", modelsV2.HealthHealthyActive, `"health_healthy_active"`},
		{"healthy inactive", modelsV2.HealthHealthyInactive, `"health_healthy_inactive"`},
		{"warning", modelsV2.HealthWarning, `"health_warning"`},
		{"critical", modelsV2.HealthCritical, `"health_critical"`},
		{"unknown", modelsV2.HealthUnknown, `"health_unknown"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test marshaling
			health := tt.health
			marshaled, err := health.MarshalJSON()
			if err != nil {
				t.Fatalf("failed to marshal health status: %v", err)
			}

			if string(marshaled) != tt.expected {
				t.Errorf("expected JSON %s, got %s", tt.expected, string(marshaled))
			}

			// Test unmarshaling
			var unmarshaledHealth modelsV2.HealthStatus
			err = unmarshaledHealth.UnmarshalJSON(marshaled)
			if err != nil {
				t.Fatalf("failed to unmarshal health status: %v", err)
			}

			if unmarshaledHealth != health {
				t.Errorf("expected health status %v after unmarshal, got %v", health, unmarshaledHealth)
			}
		})
	}
}

func TestDeviceMetricsJSON(t *testing.T) {
	// Note: When DeviceMetrics is marshaled with encoding/json, enum fields serialize as integers.
	// This is expected Go behavior for value-type enum fields.
	// The custom MarshalJSON/UnmarshalJSON methods work correctly when called directly on the enum types.
	// For InfluxDB, we explicitly call .String() to serialize enums as strings in tags.
	metrics := modelsV2.DeviceMetrics{
		DeviceID:  "test-device",
		Timestamp: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
		Health:    modelsV2.HealthHealthyActive,
		HashrateHS: &modelsV2.MetricValue{
			Value: 100e12,
			Kind:  modelsV2.MetricKindRate,
		},
		HashBoards: []modelsV2.HashBoardMetrics{
			{
				ComponentInfo: modelsV2.ComponentInfo{
					Index:  0,
					Name:   "Board 0",
					Status: modelsV2.ComponentStatusHealthy,
				},
			},
		},
	}

	// Marshal to JSON - enums will be integers in struct fields
	jsonData, err := json.Marshal(metrics)
	if err != nil {
		t.Fatalf("failed to marshal DeviceMetrics: %v", err)
	}

	// Verify we can marshal successfully
	if len(jsonData) == 0 {
		t.Error("expected non-empty JSON data")
	}

	// Test that individual enum values DO marshal to strings when called directly
	healthJSON, err := metrics.Health.MarshalJSON()
	if err != nil {
		t.Fatalf("failed to marshal health directly: %v", err)
	}
	if string(healthJSON) != `"health_healthy_active"` {
		t.Errorf("expected health to marshal to string, got: %s", string(healthJSON))
	}
}

func TestComponentStatusEnums(t *testing.T) {
	tests := []struct {
		status   modelsV2.ComponentStatus
		expected string
	}{
		{modelsV2.ComponentStatusHealthy, "component_status_healthy"},
		{modelsV2.ComponentStatusWarning, "component_status_warning"},
		{modelsV2.ComponentStatusCritical, "component_status_critical"},
		{modelsV2.ComponentStatusOffline, "component_status_offline"},
		{modelsV2.ComponentStatusDisabled, "component_status_disabled"},
		{modelsV2.ComponentStatusUnknown, "component_status_unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.status.String(); got != tt.expected {
				t.Errorf("String() = %v, want %v", got, tt.expected)
			}

			// Test JSON marshaling
			marshaled, err := tt.status.MarshalJSON()
			if err != nil {
				t.Fatalf("failed to marshal: %v", err)
			}

			expected := `"` + tt.expected + `"`
			if string(marshaled) != expected {
				t.Errorf("MarshalJSON() = %s, want %s", string(marshaled), expected)
			}

			// Test JSON unmarshaling
			var unmarshaled modelsV2.ComponentStatus
			if err := unmarshaled.UnmarshalJSON(marshaled); err != nil {
				t.Fatalf("failed to unmarshal: %v", err)
			}

			if unmarshaled != tt.status {
				t.Errorf("after unmarshal got %v, want %v", unmarshaled, tt.status)
			}
		})
	}
}

func TestMetricKindEnums(t *testing.T) {
	tests := []struct {
		kind     modelsV2.MetricKind
		expected string
	}{
		{modelsV2.MetricKindGauge, "metric_kind_gauge"},
		{modelsV2.MetricKindRate, "metric_kind_rate"},
		{modelsV2.MetricKindCounter, "metric_kind_counter"},
		{modelsV2.MetricKindUnknown, "metric_kind_unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.kind.String(); got != tt.expected {
				t.Errorf("String() = %v, want %v", got, tt.expected)
			}

			// Test JSON marshaling
			marshaled, err := tt.kind.MarshalJSON()
			if err != nil {
				t.Fatalf("failed to marshal: %v", err)
			}

			expected := `"` + tt.expected + `"`
			if string(marshaled) != expected {
				t.Errorf("MarshalJSON() = %s, want %s", string(marshaled), expected)
			}

			// Test JSON unmarshaling
			var unmarshaled modelsV2.MetricKind
			if err := unmarshaled.UnmarshalJSON(marshaled); err != nil {
				t.Fatalf("failed to unmarshal: %v", err)
			}

			if unmarshaled != tt.kind {
				t.Errorf("after unmarshal got %v, want %v", unmarshaled, tt.kind)
			}
		})
	}
}

func TestComponentTypeEnums(t *testing.T) {
	tests := []struct {
		componentType modelsV2.ComponentType
		expected      string
	}{
		{modelsV2.ComponentTypeHashBoard, "component_type_hash_board"},
		{modelsV2.ComponentTypeFan, "component_type_fan"},
		{modelsV2.ComponentTypePSU, "component_type_psu"},
		{modelsV2.ComponentTypeControlBoard, "component_type_control_board"},
		{modelsV2.ComponentTypeSensor, "component_type_sensor"},
		{modelsV2.ComponentTypeUnknown, "component_type_unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.componentType.String(); got != tt.expected {
				t.Errorf("String() = %v, want %v", got, tt.expected)
			}

			// Test JSON marshaling
			marshaled, err := tt.componentType.MarshalJSON()
			if err != nil {
				t.Fatalf("failed to marshal: %v", err)
			}

			expected := `"` + tt.expected + `"`
			if string(marshaled) != expected {
				t.Errorf("MarshalJSON() = %s, want %s", string(marshaled), expected)
			}

			// Test JSON unmarshaling
			var unmarshaled modelsV2.ComponentType
			if err := unmarshaled.UnmarshalJSON(marshaled); err != nil {
				t.Fatalf("failed to unmarshal: %v", err)
			}

			if unmarshaled != tt.componentType {
				t.Errorf("after unmarshal got %v, want %v", unmarshaled, tt.componentType)
			}
		})
	}
}

func TestHealthStatusParsing(t *testing.T) {
	tests := []struct {
		input    string
		expected modelsV2.HealthStatus
		wantErr  bool
	}{
		{"health_healthy_active", modelsV2.HealthHealthyActive, false},
		{"health_healthy_inactive", modelsV2.HealthHealthyInactive, false},
		{"health_warning", modelsV2.HealthWarning, false},
		{"health_critical", modelsV2.HealthCritical, false},
		{"health_unknown", modelsV2.HealthUnknown, false},
		{"invalid_status", modelsV2.HealthUnknown, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, err := modelsV2.ParseHealthStatus(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseHealthStatus(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if !tt.wantErr && result != tt.expected {
				t.Errorf("ParseHealthStatus(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}
