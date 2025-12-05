package mappers

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/btc-mining/proto-fleet/server/internal/domain/diagnostics/models"
	sdkv1 "github.com/btc-mining/proto-fleet/server/sdk/v1"
	sdkv1errors "github.com/btc-mining/proto-fleet/server/sdk/v1/errors"
)

func TestSDKDeviceErrorsToFleetDeviceErrors(t *testing.T) {
	now := time.Now()
	componentID := "hashboard-0"

	sdkErrors := sdkv1.DeviceErrors{
		DeviceID: "device-123",
		Errors: []sdkv1.DeviceError{
			{
				MinerError:        3001, // HashboardOverTemperature
				CauseSummary:      "Hashboard temperature exceeded threshold",
				RecommendedAction: "Check cooling system",
				Severity:          2, // Major
				FirstSeenAt:       now.Add(-time.Hour),
				LastSeenAt:        now,
				VendorAttributes: map[string]string{
					"vendor_code": "E001",
					"firmware":    "v1.2.3",
				},
				DeviceID:    "device-123",
				ComponentID: &componentID,
				Impact:      "Reduced hashrate",
				Summary:     "Temperature warning on hashboard 0",
			},
			{
				MinerError:        2000, // FanFailed
				CauseSummary:      "Fan stopped spinning",
				RecommendedAction: "Replace fan",
				Severity:          1, // Critical
				FirstSeenAt:       now.Add(-2 * time.Hour),
				LastSeenAt:        now,
				VendorAttributes:  map[string]string{},
				DeviceID:          "device-123",
				ComponentID:       nil,
				Impact:            "Device may overheat",
				Summary:           "Fan failure detected",
			},
		},
	}

	result := SDKDeviceErrorsToFleetDeviceErrors(sdkErrors)

	assert.Equal(t, "device-123", result.DeviceID)
	require.Len(t, result.Errors, 2)

	// First error - hashboard over temp
	err0 := result.Errors[0]
	assert.Equal(t, models.MinerError(3001), err0.MinerError)
	assert.Equal(t, models.Severity(2), err0.Severity)
	assert.Equal(t, "E001", err0.VendorCode)
	assert.Equal(t, "v1.2.3", err0.Firmware)
	assert.Empty(t, err0.ErrorID)

	// Second error - fan failed
	err1 := result.Errors[1]
	assert.Equal(t, models.MinerError(2000), err1.MinerError)
	assert.Equal(t, models.Severity(1), err1.Severity)
	assert.Empty(t, err1.VendorCode)
	assert.Empty(t, err1.Firmware)
	assert.Nil(t, err1.ComponentID)
}

func TestSDKMinerErrorToFleetMinerError(t *testing.T) {
	tests := []struct {
		name     string
		input    sdkv1errors.MinerError
		expected models.MinerError
	}{
		{"unspecified", 0, models.MinerErrorUnspecified},
		{"psu_not_present", 1000, models.PSUNotPresent},
		{"hashboard_over_temp", 3001, models.HashboardOverTemperature},
		{"negative_returns_unspecified", -1, models.MinerErrorUnspecified},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SDKMinerErrorToFleetMinerError(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSDKSeverityToFleetSeverity(t *testing.T) {
	tests := []struct {
		name     string
		input    sdkv1errors.Severity
		expected models.Severity
	}{
		{"unspecified", 0, models.SeverityUnspecified},
		{"critical", 1, models.SeverityCritical},
		{"major", 2, models.SeverityMajor},
		{"negative_returns_unspecified", -1, models.SeverityUnspecified},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SDKSeverityToFleetSeverity(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
