package device

import (
	"testing"
	"time"

	"github.com/btc-mining/proto-fleet/plugin/antminer/pkg/antminer/rpc"
	sdkerrors "github.com/btc-mining/proto-fleet/server/sdk/v1/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testDeviceIDForErrors = "test-device-errors"

// Helper function to create test devs response
func createTestDevsResponse(devs ...rpc.DevInfo) *rpc.DevsResponse {
	return &rpc.DevsResponse{
		Devs: devs,
	}
}

// Helper function to create test summary response
func createTestSummaryResponse(hwErrors int64, hwPercent float64) *rpc.SummaryResponse {
	return &rpc.SummaryResponse{
		Summary: []rpc.SummaryInfo{
			{
				HardwareErrors:        hwErrors,
				DeviceHardwarePercent: hwPercent,
			},
		},
	}
}

// Test: Critical temperature error
func TestDetectTemperatureErrors_Critical(t *testing.T) {
	now := time.Now()
	devs := []rpc.DevInfo{
		{
			ASC:         0,
			Status:      "Alive",
			Enabled:     "Y",
			Temperature: 98.5, // Above critical threshold (95°C)
			MHSAv:       100000000,
		},
	}

	errors := detectTemperatureErrors(devs, testDeviceIDForErrors, now)

	require.Len(t, errors, 1)
	assert.Equal(t, sdkerrors.HashboardOverTemperature, errors[0].MinerError)
	assert.Equal(t, sdkerrors.SeverityCritical, errors[0].Severity)
	assert.Contains(t, errors[0].Summary, "98.5°C")
	assert.Contains(t, errors[0].Summary, "critical")
	assert.Equal(t, sdkerrors.ComponentTypeHashBoard, errors[0].ComponentType)
	require.NotNil(t, errors[0].ComponentID)
	assert.Equal(t, "0", *errors[0].ComponentID)
	assert.Equal(t, "98.5", errors[0].VendorAttributes["temperature_celsius"])
}

// Test: Major temperature warning
func TestDetectTemperatureErrors_Major(t *testing.T) {
	now := time.Now()
	devs := []rpc.DevInfo{
		{
			ASC:         1,
			Status:      "Alive",
			Enabled:     "Y",
			Temperature: 88.0, // Above major threshold (85°C) but below critical
			MHSAv:       100000000,
		},
	}

	errors := detectTemperatureErrors(devs, testDeviceIDForErrors, now)

	require.Len(t, errors, 1)
	assert.Equal(t, sdkerrors.HashboardOverTemperature, errors[0].MinerError)
	assert.Equal(t, sdkerrors.SeverityMajor, errors[0].Severity)
	assert.Contains(t, errors[0].Summary, "88.0°C")
	assert.Equal(t, "1", *errors[0].ComponentID)
}

// Test: Under-temperature warning
func TestDetectTemperatureErrors_Under(t *testing.T) {
	now := time.Now()
	devs := []rpc.DevInfo{
		{
			ASC:         0,
			Status:      "Alive",
			Enabled:     "Y",
			Temperature: -5.0, // Below minimum threshold (0°C)
			MHSAv:       100000000,
		},
	}

	errors := detectTemperatureErrors(devs, testDeviceIDForErrors, now)

	require.Len(t, errors, 1)
	assert.Equal(t, sdkerrors.HashboardASICUnderTemperature, errors[0].MinerError)
	assert.Equal(t, sdkerrors.SeverityMinor, errors[0].Severity)
}

// Test: Temperature detection with Tenperature field (typo in firmware)
func TestDetectTemperatureErrors_WithTypo(t *testing.T) {
	now := time.Now()
	devs := []rpc.DevInfo{
		{
			ASC:         0,
			Status:      "Alive",
			Enabled:     "Y",
			Tenperature: 96.0, // Using the typo field
			MHSAv:       100000000,
		},
	}

	errors := detectTemperatureErrors(devs, testDeviceIDForErrors, now)

	require.Len(t, errors, 1)
	assert.Equal(t, sdkerrors.HashboardOverTemperature, errors[0].MinerError)
	assert.Equal(t, sdkerrors.SeverityCritical, errors[0].Severity)
}

// Test: No errors for normal temperature
func TestDetectTemperatureErrors_NoErrors(t *testing.T) {
	now := time.Now()
	devs := []rpc.DevInfo{
		{
			ASC:         0,
			Status:      "Alive",
			Enabled:     "Y",
			Temperature: 65.0, // Normal temperature
			MHSAv:       100000000,
		},
	}

	errors := detectTemperatureErrors(devs, testDeviceIDForErrors, now)

	assert.Empty(t, errors, "Expected no errors for normal temperature")
}

// Test: Zero temperature is skipped
func TestDetectTemperatureErrors_ZeroTemperature(t *testing.T) {
	now := time.Now()
	devs := []rpc.DevInfo{
		{
			ASC:         0,
			Status:      "Alive",
			Enabled:     "Y",
			Temperature: 0, // Zero temperature should be skipped
			MHSAv:       100000000,
		},
	}

	errors := detectTemperatureErrors(devs, testDeviceIDForErrors, now)

	assert.Empty(t, errors, "Expected no errors for zero temperature (skipped)")
}

// Test: Multiple boards with different temperatures
func TestDetectTemperatureErrors_MultipleBoards(t *testing.T) {
	now := time.Now()
	devs := []rpc.DevInfo{
		{
			ASC:         0,
			Status:      "Alive",
			Enabled:     "Y",
			Temperature: 65.0, // Normal
			MHSAv:       100000000,
		},
		{
			ASC:         1,
			Status:      "Alive",
			Enabled:     "Y",
			Temperature: 90.0, // Major warning
			MHSAv:       100000000,
		},
		{
			ASC:         2,
			Status:      "Alive",
			Enabled:     "Y",
			Temperature: 96.0, // Critical
			MHSAv:       100000000,
		},
	}

	errors := detectTemperatureErrors(devs, testDeviceIDForErrors, now)

	require.Len(t, errors, 2)

	// Find errors by component ID
	var majorErr, criticalErr *sdkerrors.DeviceError
	for i := range errors {
		if *errors[i].ComponentID == "1" {
			majorErr = &errors[i]
		} else if *errors[i].ComponentID == "2" {
			criticalErr = &errors[i]
		}
	}

	require.NotNil(t, majorErr, "Expected major error for board 1")
	assert.Equal(t, sdkerrors.SeverityMajor, majorErr.Severity)

	require.NotNil(t, criticalErr, "Expected critical error for board 2")
	assert.Equal(t, sdkerrors.SeverityCritical, criticalErr.Severity)
}

// Test: Hashboard not alive (communication lost)
func TestDetectHashboardStatusErrors_NotAlive(t *testing.T) {
	now := time.Now()
	devs := []rpc.DevInfo{
		{
			ASC:     0,
			Status:  "Dead",
			Enabled: "Y",
		},
	}

	errors := detectHashboardStatusErrors(devs, testDeviceIDForErrors, now)

	require.Len(t, errors, 1)
	assert.Equal(t, sdkerrors.ASICChainCommunicationLost, errors[0].MinerError)
	assert.Equal(t, sdkerrors.SeverityCritical, errors[0].Severity)
	assert.Contains(t, errors[0].Summary, "Dead")
	assert.Equal(t, "Dead", errors[0].VendorAttributes["status"])
}

// Test: Hashboard disabled
func TestDetectHashboardStatusErrors_Disabled(t *testing.T) {
	now := time.Now()
	devs := []rpc.DevInfo{
		{
			ASC:     0,
			Status:  "Alive",
			Enabled: "N",
		},
	}

	errors := detectHashboardStatusErrors(devs, testDeviceIDForErrors, now)

	require.Len(t, errors, 1)
	assert.Equal(t, sdkerrors.HashboardNotPresent, errors[0].MinerError)
	assert.Equal(t, sdkerrors.SeverityMajor, errors[0].Severity)
}

// Test: Hashboard alive but not hashing
func TestDetectHashboardStatusErrors_NotHashing(t *testing.T) {
	now := time.Now()
	devs := []rpc.DevInfo{
		{
			ASC:     0,
			Status:  "Alive",
			Enabled: "Y",
			MHSAv:   0, // No hashrate
		},
	}

	errors := detectHashboardStatusErrors(devs, testDeviceIDForErrors, now)

	require.Len(t, errors, 1)
	assert.Equal(t, sdkerrors.HashrateBelowTarget, errors[0].MinerError)
	assert.Equal(t, sdkerrors.SeverityMajor, errors[0].Severity)
	assert.Contains(t, errors[0].Summary, "not producing hashrate")
}

// Test: Healthy hashboard produces no errors
func TestDetectHashboardStatusErrors_Healthy(t *testing.T) {
	now := time.Now()
	devs := []rpc.DevInfo{
		{
			ASC:     0,
			Status:  "Alive",
			Enabled: "Y",
			MHSAv:   100000000, // Healthy hashrate
		},
	}

	errors := detectHashboardStatusErrors(devs, testDeviceIDForErrors, now)

	assert.Empty(t, errors, "Expected no errors for healthy hashboard")
}

// Test: Multiple boards with different statuses
func TestDetectHashboardStatusErrors_MultipleBoards(t *testing.T) {
	now := time.Now()
	devs := []rpc.DevInfo{
		{
			ASC:     0,
			Status:  "Alive",
			Enabled: "Y",
			MHSAv:   100000000, // Healthy
		},
		{
			ASC:     1,
			Status:  "Dead", // Communication lost
			Enabled: "Y",
		},
		{
			ASC:     2,
			Status:  "Alive",
			Enabled: "N", // Disabled
		},
	}

	errors := detectHashboardStatusErrors(devs, testDeviceIDForErrors, now)

	require.Len(t, errors, 2)

	// Find errors by component ID
	var deadErr, disabledErr *sdkerrors.DeviceError
	for i := range errors {
		if *errors[i].ComponentID == "1" {
			deadErr = &errors[i]
		} else if *errors[i].ComponentID == "2" {
			disabledErr = &errors[i]
		}
	}

	require.NotNil(t, deadErr, "Expected error for dead board 1")
	assert.Equal(t, sdkerrors.ASICChainCommunicationLost, deadErr.MinerError)

	require.NotNil(t, disabledErr, "Expected error for disabled board 2")
	assert.Equal(t, sdkerrors.HashboardNotPresent, disabledErr.MinerError)
}

// Test: High hardware error percentage (major)
func TestDetectPerBoardHardwareErrors_PercentMajor(t *testing.T) {
	now := time.Now()
	devs := []rpc.DevInfo{
		{
			ASC:                   0,
			Status:                "Alive",
			Enabled:               "Y",
			Temperature:           70.0,
			MHSAv:                 100000000,
			DeviceHardwarePercent: 6.5, // Above 5% threshold
			HardwareErrors:        5000,
		},
	}

	errors := detectPerBoardHardwareErrors(devs, testDeviceIDForErrors, now)

	require.Len(t, errors, 1)
	assert.Equal(t, sdkerrors.HashboardWarnCRCHigh, errors[0].MinerError)
	assert.Equal(t, sdkerrors.SeverityMajor, errors[0].Severity)
	assert.Contains(t, errors[0].Summary, "6.50%")
}

// Test: Elevated hardware error percentage (minor)
func TestDetectPerBoardHardwareErrors_PercentMinor(t *testing.T) {
	now := time.Now()
	devs := []rpc.DevInfo{
		{
			ASC:                   0,
			Status:                "Alive",
			Enabled:               "Y",
			Temperature:           70.0,
			MHSAv:                 100000000,
			DeviceHardwarePercent: 2.5, // Between 1% and 5%
			HardwareErrors:        500,
		},
	}

	errors := detectPerBoardHardwareErrors(devs, testDeviceIDForErrors, now)

	require.Len(t, errors, 1)
	assert.Equal(t, sdkerrors.HashboardWarnCRCHigh, errors[0].MinerError)
	assert.Equal(t, sdkerrors.SeverityMinor, errors[0].Severity)
}

// Test: High hardware error count (minor)
func TestDetectPerBoardHardwareErrors_Count(t *testing.T) {
	now := time.Now()
	devs := []rpc.DevInfo{
		{
			ASC:                   0,
			Status:                "Alive",
			Enabled:               "Y",
			Temperature:           70.0,
			MHSAv:                 100000000,
			DeviceHardwarePercent: 0.5,  // Low percentage
			HardwareErrors:        1500, // But high absolute count (>1000)
		},
	}

	errors := detectPerBoardHardwareErrors(devs, testDeviceIDForErrors, now)

	require.Len(t, errors, 1)
	assert.Equal(t, sdkerrors.HashboardWarnCRCHigh, errors[0].MinerError)
	assert.Equal(t, sdkerrors.SeverityMinor, errors[0].Severity)
	assert.Contains(t, errors[0].Summary, "1500")
}

// Test: No hardware errors for healthy board
func TestDetectPerBoardHardwareErrors_NoErrors(t *testing.T) {
	now := time.Now()
	devs := []rpc.DevInfo{
		{
			ASC:                   0,
			Status:                "Alive",
			Enabled:               "Y",
			Temperature:           70.0,
			MHSAv:                 100000000,
			DeviceHardwarePercent: 0.1,
			HardwareErrors:        10,
		},
	}

	errors := detectPerBoardHardwareErrors(devs, testDeviceIDForErrors, now)

	assert.Empty(t, errors, "Expected no errors for healthy board")
}

// Test: Device-level hardware error from summary (major)
func TestDetectSummaryHardwareErrors_Major(t *testing.T) {
	now := time.Now()
	summary := &rpc.SummaryInfo{
		HardwareErrors:        10000,
		DeviceHardwarePercent: 7.5, // Above 5% threshold
	}

	errors := detectSummaryHardwareErrors(summary, testDeviceIDForErrors, now)

	require.Len(t, errors, 1)
	assert.Equal(t, sdkerrors.HashboardWarnCRCHigh, errors[0].MinerError)
	assert.Equal(t, sdkerrors.SeverityMajor, errors[0].Severity)
	assert.Equal(t, sdkerrors.ComponentTypeControlBoard, errors[0].ComponentType)
}

// Test: Device-level hardware error from summary (minor)
func TestDetectSummaryHardwareErrors_Minor(t *testing.T) {
	now := time.Now()
	summary := &rpc.SummaryInfo{
		HardwareErrors:        1000,
		DeviceHardwarePercent: 2.5, // Between 1% and 5%
	}

	errors := detectSummaryHardwareErrors(summary, testDeviceIDForErrors, now)

	require.Len(t, errors, 1)
	assert.Equal(t, sdkerrors.HashboardWarnCRCHigh, errors[0].MinerError)
	assert.Equal(t, sdkerrors.SeverityMinor, errors[0].Severity)
}

// Test: No summary hardware errors for healthy device
func TestDetectSummaryHardwareErrors_NoErrors(t *testing.T) {
	now := time.Now()
	summary := &rpc.SummaryInfo{
		HardwareErrors:        10,
		DeviceHardwarePercent: 0.1,
	}

	errors := detectSummaryHardwareErrors(summary, testDeviceIDForErrors, now)

	assert.Empty(t, errors, "Expected no errors for healthy device")
}

// Test: High share rejection rate (major)
func TestDetectShareRejectionErrors_Major(t *testing.T) {
	now := time.Now()
	summary := &rpc.SummaryInfo{
		DeviceRejectedPercent: 12.5, // Above 10% threshold
		Rejected:              125,
		Accepted:              875,
	}

	errors := detectShareRejectionErrors(summary, testDeviceIDForErrors, now)

	require.Len(t, errors, 1)
	assert.Equal(t, sdkerrors.HashrateBelowTarget, errors[0].MinerError)
	assert.Equal(t, sdkerrors.SeverityMajor, errors[0].Severity)
	assert.Contains(t, errors[0].Summary, "12.50%")
}

// Test: Elevated share rejection rate (minor)
func TestDetectShareRejectionErrors_Minor(t *testing.T) {
	now := time.Now()
	summary := &rpc.SummaryInfo{
		DeviceRejectedPercent: 7.5, // Between 5% and 10%
		Rejected:              75,
		Accepted:              925,
	}

	errors := detectShareRejectionErrors(summary, testDeviceIDForErrors, now)

	require.Len(t, errors, 1)
	assert.Equal(t, sdkerrors.HashrateBelowTarget, errors[0].MinerError)
	assert.Equal(t, sdkerrors.SeverityMinor, errors[0].Severity)
}

// Test: High stale shares
func TestDetectShareRejectionErrors_Stale(t *testing.T) {
	now := time.Now()
	summary := &rpc.SummaryInfo{
		DeviceRejectedPercent: 0.5, // Low rejection
		Stale:                 150, // Above 100 threshold
	}

	errors := detectShareRejectionErrors(summary, testDeviceIDForErrors, now)

	require.Len(t, errors, 1)
	assert.Equal(t, sdkerrors.HashrateBelowTarget, errors[0].MinerError)
	assert.Equal(t, sdkerrors.SeverityMinor, errors[0].Severity)
	assert.Contains(t, errors[0].Summary, "150 stale")
}

// Test: Both rejection and stale issues
func TestDetectShareRejectionErrors_Multiple(t *testing.T) {
	now := time.Now()
	summary := &rpc.SummaryInfo{
		DeviceRejectedPercent: 12.5, // Major rejection
		Rejected:              125,
		Accepted:              875,
		Stale:                 150, // Also high stale
	}

	errors := detectShareRejectionErrors(summary, testDeviceIDForErrors, now)

	require.Len(t, errors, 2)
}

// Test: No share rejection errors for healthy device
func TestDetectShareRejectionErrors_NoErrors(t *testing.T) {
	now := time.Now()
	summary := &rpc.SummaryInfo{
		DeviceRejectedPercent: 0.5,
		Rejected:              5,
		Accepted:              995,
		Stale:                 10,
	}

	errors := detectShareRejectionErrors(summary, testDeviceIDForErrors, now)

	assert.Empty(t, errors, "Expected no errors for healthy device")
}

// Test: Pool not alive
func TestDetectPoolErrors_NotAlive(t *testing.T) {
	now := time.Now()
	pools := []rpc.PoolInfo{
		{
			Pool:   0,
			URL:    "stratum+tcp://pool.example.com:3333",
			Status: "Dead",
		},
	}

	errors := detectPoolErrors(pools, testDeviceIDForErrors, now)

	require.Len(t, errors, 1)
	assert.Equal(t, sdkerrors.DeviceCommunicationLost, errors[0].MinerError)
	assert.Equal(t, sdkerrors.SeverityMajor, errors[0].Severity)
	assert.Contains(t, errors[0].Summary, "Dead")
	assert.Contains(t, errors[0].Summary, "pool.example.com")
}

// Test: Pool get failures
func TestDetectPoolErrors_GetFailures(t *testing.T) {
	now := time.Now()
	pools := []rpc.PoolInfo{
		{
			Pool:        0,
			URL:         "stratum+tcp://pool.example.com:3333",
			Status:      "Alive",
			GetFailures: 15, // >10 threshold
		},
	}

	errors := detectPoolErrors(pools, testDeviceIDForErrors, now)

	require.Len(t, errors, 1)
	assert.Equal(t, sdkerrors.DeviceCommunicationLost, errors[0].MinerError)
	assert.Contains(t, errors[0].Summary, "15 get failures")
}

// Test: Pool remote failures
func TestDetectPoolErrors_RemoteFailures(t *testing.T) {
	now := time.Now()
	pools := []rpc.PoolInfo{
		{
			Pool:           0,
			URL:            "stratum+tcp://pool.example.com:3333",
			Status:         "Alive",
			RemoteFailures: 20, // >10 threshold
		},
	}

	errors := detectPoolErrors(pools, testDeviceIDForErrors, now)

	require.Len(t, errors, 1)
	assert.Equal(t, sdkerrors.DeviceCommunicationLost, errors[0].MinerError)
	assert.Contains(t, errors[0].Summary, "20 remote failures")
}

// Test: Multiple pools with different issues
func TestDetectPoolErrors_MultiplePools(t *testing.T) {
	now := time.Now()
	pools := []rpc.PoolInfo{
		{
			Pool:   0,
			URL:    "stratum+tcp://pool1.example.com:3333",
			Status: "Alive",
		},
		{
			Pool:   1,
			URL:    "stratum+tcp://pool2.example.com:3333",
			Status: "Dead",
		},
	}

	errors := detectPoolErrors(pools, testDeviceIDForErrors, now)

	require.Len(t, errors, 1)
	assert.Equal(t, "1", *errors[0].ComponentID)
}

// Test: No pool errors for healthy pools
func TestDetectPoolErrors_NoErrors(t *testing.T) {
	now := time.Now()
	pools := []rpc.PoolInfo{
		{
			Pool:           0,
			URL:            "stratum+tcp://pool.example.com:3333",
			Status:         "Alive",
			GetFailures:    5,
			RemoteFailures: 3,
		},
	}

	errors := detectPoolErrors(pools, testDeviceIDForErrors, now)

	assert.Empty(t, errors, "Expected no errors for healthy pool")
}

// ============================================================================
// detectErrors aggregation tests
// ============================================================================

// Helper functions for aggregation tests
func healthyDevInfo(asc int) rpc.DevInfo {
	return rpc.DevInfo{
		ASC:                   asc,
		Status:                "Alive",
		Enabled:               "Y",
		Temperature:           65.0,
		MHSAv:                 100000000, // 100 TH/s
		HardwareErrors:        10,
		DeviceHardwarePercent: 0.1,
	}
}

func healthyPoolInfo(pool int, url string) rpc.PoolInfo {
	return rpc.PoolInfo{
		Pool:           pool,
		URL:            url,
		Status:         "Alive",
		GetFailures:    0,
		RemoteFailures: 0,
	}
}

func createTestPoolsResponse(pools ...rpc.PoolInfo) *rpc.PoolsResponse {
	return &rpc.PoolsResponse{
		Pools: pools,
	}
}

func createFullTestSummaryResponse(hwErrors int64, hwPercent, rejectedPercent float64, stale int64) *rpc.SummaryResponse {
	return &rpc.SummaryResponse{
		Summary: []rpc.SummaryInfo{
			{
				HardwareErrors:        hwErrors,
				DeviceHardwarePercent: hwPercent,
				DeviceRejectedPercent: rejectedPercent,
				Stale:                 stale,
				Accepted:              1000,
				Rejected:              10,
			},
		},
	}
}

// Test: No errors when all metrics are healthy
func TestDetectErrors_NoErrors(t *testing.T) {
	summary := createFullTestSummaryResponse(10, 0.1, 0.5, 5)
	devs := createTestDevsResponse(
		healthyDevInfo(0),
		healthyDevInfo(1),
		healthyDevInfo(2),
	)
	pools := createTestPoolsResponse(
		healthyPoolInfo(0, "stratum+tcp://pool1.example.com:3333"),
	)

	errors := detectErrors(summary, devs, pools, testDeviceIDForErrors)

	assert.Empty(t, errors, "Expected no errors for healthy device")
}

// Test: Multiple concurrent errors
func TestDetectErrors_MultipleConcurrentErrors(t *testing.T) {
	summary := createFullTestSummaryResponse(10, 0.1, 12.5, 5) // High rejection
	devs := createTestDevsResponse(
		rpc.DevInfo{
			ASC:         0,
			Status:      "Alive",
			Enabled:     "Y",
			Temperature: 96.0, // Critical temp
			MHSAv:       100000000,
		},
		rpc.DevInfo{
			ASC:     1,
			Status:  "Dead", // Communication lost
			Enabled: "Y",
		},
	)
	pools := createTestPoolsResponse(
		rpc.PoolInfo{
			Pool:        0,
			URL:         "stratum+tcp://pool.example.com:3333",
			Status:      "Alive",
			GetFailures: 15, // High failures
		},
	)

	errors := detectErrors(summary, devs, pools, testDeviceIDForErrors)

	// Should have: 1 temp error, 1 comm lost, 1 rejection, 1 pool failure
	require.Len(t, errors, 4)

	// Verify we have each type of error
	var hasTemp, hasComm, hasRejection, hasPool bool
	for _, err := range errors {
		switch err.MinerError {
		case sdkerrors.HashboardOverTemperature:
			hasTemp = true
		case sdkerrors.ASICChainCommunicationLost:
			hasComm = true
		case sdkerrors.HashrateBelowTarget:
			hasRejection = true
		case sdkerrors.DeviceCommunicationLost:
			hasPool = true
		}
	}
	assert.True(t, hasTemp, "Expected temperature error")
	assert.True(t, hasComm, "Expected communication error")
	assert.True(t, hasRejection, "Expected rejection error")
	assert.True(t, hasPool, "Expected pool error")
}

// Test: Empty responses
func TestDetectErrors_EmptyResponses(t *testing.T) {
	errors := detectErrors(nil, nil, nil, testDeviceIDForErrors)
	assert.Empty(t, errors)

	errors = detectErrors(&rpc.SummaryResponse{}, &rpc.DevsResponse{}, &rpc.PoolsResponse{}, testDeviceIDForErrors)
	assert.Empty(t, errors)
}

// Test: Timestamps are set correctly
func TestDetectErrors_Timestamps(t *testing.T) {
	beforeTest := time.Now()

	devs := createTestDevsResponse(
		rpc.DevInfo{
			ASC:         0,
			Status:      "Dead",
			Enabled:     "Y",
			Temperature: 70.0,
		},
	)

	errors := detectErrors(nil, devs, nil, testDeviceIDForErrors)

	afterTest := time.Now()

	require.Len(t, errors, 1)
	assert.True(t, errors[0].FirstSeenAt.After(beforeTest) || errors[0].FirstSeenAt.Equal(beforeTest))
	assert.True(t, errors[0].LastSeenAt.Before(afterTest) || errors[0].LastSeenAt.Equal(afterTest))
	assert.Equal(t, errors[0].FirstSeenAt, errors[0].LastSeenAt)
}

// Test: DeviceID is set correctly
func TestDetectErrors_DeviceID(t *testing.T) {
	customDeviceID := "custom-device-123"

	devs := createTestDevsResponse(
		rpc.DevInfo{
			ASC:     0,
			Status:  "Dead",
			Enabled: "Y",
		},
	)

	errors := detectErrors(nil, devs, nil, customDeviceID)

	require.Len(t, errors, 1)
	assert.Equal(t, customDeviceID, errors[0].DeviceID)
}
