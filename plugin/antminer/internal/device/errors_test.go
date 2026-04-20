package device

import (
	"fmt"
	"testing"
	"time"

	"github.com/block/proto-fleet/plugin/antminer/pkg/antminer/rpc"
	"github.com/block/proto-fleet/plugin/antminer/pkg/antminer/web"
	sdkerrors "github.com/block/proto-fleet/server/sdk/v1/errors"
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
func createTestSummaryResponse() *rpc.SummaryResponse {
	return &rpc.SummaryResponse{
		Summary: []rpc.SummaryInfo{
			{
				HardwareErrors:        10,
				DeviceHardwarePercent: 0.1,
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

	errors := detectHashboardStatusErrors(devs, testDeviceIDForErrors, now, false)

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

	errors := detectHashboardStatusErrors(devs, testDeviceIDForErrors, now, false)

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

	errors := detectHashboardStatusErrors(devs, testDeviceIDForErrors, now, false)

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

	errors := detectHashboardStatusErrors(devs, testDeviceIDForErrors, now, false)

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

	errors := detectHashboardStatusErrors(devs, testDeviceIDForErrors, now, false)

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
	assert.Equal(t, sdkerrors.VendorErrorUnmapped, errors[0].MinerError)
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
	assert.Equal(t, sdkerrors.VendorErrorUnmapped, errors[0].MinerError)
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
	assert.Equal(t, sdkerrors.VendorErrorUnmapped, errors[0].MinerError)
	assert.Contains(t, errors[0].Summary, "20 remote failures")
}

// Test: Multiple pools with different issues - should not error if one pool is working
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

	// Should not report errors because pool 0 is working (failover is functioning)
	assert.Empty(t, errors, "Expected no errors when at least one pool is working")
}

// Test: All pools down - should report errors for all
func TestDetectPoolErrors_AllPoolsDown(t *testing.T) {
	now := time.Now()
	pools := []rpc.PoolInfo{
		{
			Pool:   0,
			URL:    "stratum+tcp://pool1.example.com:3333",
			Status: "Dead",
		},
		{
			Pool:   1,
			URL:    "stratum+tcp://pool2.example.com:3333",
			Status: "Dead",
		},
		{
			Pool:   2,
			URL:    "stratum+tcp://pool3.example.com:3333",
			Status: "Dead",
		},
	}

	errors := detectPoolErrors(pools, testDeviceIDForErrors, now)

	// Should report errors for all pools since none are working
	require.Len(t, errors, 3, "Expected errors for all pools when all are down")
	assert.Equal(t, "0", *errors[0].ComponentID)
	assert.Equal(t, "1", *errors[1].ComponentID)
	assert.Equal(t, "2", *errors[2].ComponentID)
}

// Test: All pools have high failures - should report errors
func TestDetectPoolErrors_AllPoolsHighFailures(t *testing.T) {
	now := time.Now()
	pools := []rpc.PoolInfo{
		{
			Pool:        0,
			URL:         "stratum+tcp://pool1.example.com:3333",
			Status:      "Alive",
			GetFailures: 15, // >10 threshold
		},
		{
			Pool:           1,
			URL:            "stratum+tcp://pool2.example.com:3333",
			Status:         "Alive",
			RemoteFailures: 20, // >10 threshold
		},
	}

	errors := detectPoolErrors(pools, testDeviceIDForErrors, now)

	// Should report errors for all pools since all have high failures
	require.Len(t, errors, 2, "Expected errors for all pools when all have failures")
	assert.Contains(t, errors[0].Summary, "get failures")
	assert.Contains(t, errors[1].Summary, "remote failures")
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

	errors := detectErrors(summary, devs, pools, nil, testDeviceIDForErrors, false)

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

	errors := detectErrors(summary, devs, pools, nil, testDeviceIDForErrors, false)

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
		case sdkerrors.VendorErrorUnmapped:
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
	errors := detectErrors(nil, nil, nil, nil, testDeviceIDForErrors, false)
	assert.Empty(t, errors)

	errors = detectErrors(&rpc.SummaryResponse{}, &rpc.DevsResponse{}, &rpc.PoolsResponse{}, nil, testDeviceIDForErrors, false)
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

	errors := detectErrors(nil, devs, nil, nil, testDeviceIDForErrors, false)

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

	errors := detectErrors(nil, devs, nil, nil, customDeviceID, false)

	require.Len(t, errors, 1)
	assert.Equal(t, customDeviceID, errors[0].DeviceID)
}

// ============================================================================
// Stats API-based Error Detection Tests (with fallback to RPC)
// ============================================================================

// Helper function to create test ChainStats for stats API tests
func createTestChainStats(index int, tempChip []float64, rateReal float64, hwp float64, hw int) web.ChainStats {
	return web.ChainStats{
		Index:     index,
		TempChip:  tempChip,
		RateReal:  rateReal,
		RateIdeal: rateReal * 1.1, // Ideal is typically slightly higher
		HWP:       hwp,
		HW:        hw,
		SN:        fmt.Sprintf("SN-CHAIN-%d", index),
	}
}

func createTestPSUStats(index int, status string) *web.PSUStats {
	return &web.PSUStats{
		Index:  index,
		Status: status,
	}
}

// Test: Stats API temperature detection - Critical
func TestDetectTemperatureErrorsFromStats_Critical(t *testing.T) {
	now := time.Now()
	chains := []web.ChainStats{
		createTestChainStats(0, []float64{96.0, 97.0, 95.5, 96.5}, 14000.0, 0.1, 10),
	}

	errors := detectTemperatureErrorsFromStats(chains, testDeviceIDForErrors, now)

	require.Len(t, errors, 1)
	assert.Equal(t, sdkerrors.HashboardOverTemperature, errors[0].MinerError)
	assert.Equal(t, sdkerrors.SeverityCritical, errors[0].Severity)
	assert.Contains(t, errors[0].Summary, "97.0°C") // Max temp from array
	assert.Contains(t, errors[0].VendorAttributes, "chain_index")
	assert.Contains(t, errors[0].VendorAttributes, "serial_number")
	assert.Equal(t, "SN-CHAIN-0", errors[0].VendorAttributes["serial_number"])
}

// Test: Stats API temperature detection - Major
func TestDetectTemperatureErrorsFromStats_Major(t *testing.T) {
	now := time.Now()
	chains := []web.ChainStats{
		createTestChainStats(1, []float64{86.0, 87.0, 85.5, 86.5}, 14000.0, 0.1, 10),
	}

	errors := detectTemperatureErrorsFromStats(chains, testDeviceIDForErrors, now)

	require.Len(t, errors, 1)
	assert.Equal(t, sdkerrors.HashboardOverTemperature, errors[0].MinerError)
	assert.Equal(t, sdkerrors.SeverityMajor, errors[0].Severity)
	assert.Contains(t, errors[0].Summary, "87.0°C")
}

// Test: Stats API temperature detection - Multiple chains
func TestDetectTemperatureErrorsFromStats_MultipleChains(t *testing.T) {
	now := time.Now()
	chains := []web.ChainStats{
		createTestChainStats(0, []float64{96.0, 97.0}, 14000.0, 0.1, 10), // Critical
		createTestChainStats(1, []float64{70.0, 72.0}, 14000.0, 0.1, 10), // OK
		createTestChainStats(2, []float64{86.0, 87.0}, 14000.0, 0.1, 10), // Major
	}

	errors := detectTemperatureErrorsFromStats(chains, testDeviceIDForErrors, now)

	require.Len(t, errors, 2)
	// Should have one critical and one major
	severities := []sdkerrors.Severity{errors[0].Severity, errors[1].Severity}
	assert.Contains(t, severities, sdkerrors.SeverityCritical)
	assert.Contains(t, severities, sdkerrors.SeverityMajor)
}

// Test: Stats API temperature detection - Empty temp array
func TestDetectTemperatureErrorsFromStats_EmptyTempArray(t *testing.T) {
	now := time.Now()
	chains := []web.ChainStats{
		createTestChainStats(0, []float64{}, 14000.0, 0.1, 10),
	}

	errors := detectTemperatureErrorsFromStats(chains, testDeviceIDForErrors, now)

	assert.Empty(t, errors, "Should not detect errors when temp array is empty")
}

// Test: Stats API temperature detection - All invalid temperatures
func TestDetectTemperatureErrorsFromStats_InvalidTemps(t *testing.T) {
	now := time.Now()
	chains := []web.ChainStats{
		createTestChainStats(0, []float64{-300.0, -400.0}, 14000.0, 0.1, 10),
	}

	errors := detectTemperatureErrorsFromStats(chains, testDeviceIDForErrors, now)

	assert.Empty(t, errors, "Should not detect errors when all temps are invalid")
}

// Test: Stats API hashboard status - Not hashing
func TestDetectHashboardStatusErrorsFromStats_NotHashing(t *testing.T) {
	now := time.Now()
	chains := []web.ChainStats{
		createTestChainStats(0, []float64{70.0, 72.0}, 0.0, 0.0, 0), // RateReal = 0
	}

	errors := detectHashboardStatusErrorsFromStats(chains, testDeviceIDForErrors, now, false)

	require.Len(t, errors, 1)
	assert.Equal(t, sdkerrors.HashrateBelowTarget, errors[0].MinerError)
	assert.Equal(t, sdkerrors.SeverityMajor, errors[0].Severity)
	assert.Contains(t, errors[0].Summary, "not producing hashrate")
	assert.Contains(t, errors[0].VendorAttributes, "rate_real_ghs")
	assert.Contains(t, errors[0].VendorAttributes, "serial_number")
	// rate_ideal_ghs is not included when RateReal is 0 (validation skips invalid values)
	assert.NotContains(t, errors[0].VendorAttributes, "rate_ideal_ghs")
}

// Test: Stats API hashboard status - Multiple chains with issues
func TestDetectHashboardStatusErrorsFromStats_MultipleChains(t *testing.T) {
	now := time.Now()
	chains := []web.ChainStats{
		createTestChainStats(0, []float64{70.0}, 0.0, 0.0, 0),      // Not hashing
		createTestChainStats(1, []float64{72.0}, 14000.0, 0.1, 10), // OK
		createTestChainStats(2, []float64{71.0}, -1.0, 0.0, 0),     // Negative rate
	}

	errors := detectHashboardStatusErrorsFromStats(chains, testDeviceIDForErrors, now, false)

	require.Len(t, errors, 2, "Should detect 2 non-hashing chains")
}

// Test: Stats API hashboard status - All healthy
func TestDetectHashboardStatusErrorsFromStats_Healthy(t *testing.T) {
	now := time.Now()
	chains := []web.ChainStats{
		createTestChainStats(0, []float64{70.0}, 14000.0, 0.1, 10),
		createTestChainStats(1, []float64{72.0}, 13500.0, 0.1, 10),
	}

	errors := detectHashboardStatusErrorsFromStats(chains, testDeviceIDForErrors, now, false)

	assert.Empty(t, errors, "Should not detect errors for healthy chains")
}

func TestDetectFanErrorsFromStats_Failed(t *testing.T) {
	now := time.Now()

	errors := detectFanErrorsFromStats([]int{0, 7050, 6980, 7020}, 4, testDeviceIDForErrors, now)

	require.Len(t, errors, 1)
	assert.Equal(t, sdkerrors.FanFailed, errors[0].MinerError)
	assert.Equal(t, sdkerrors.SeverityCritical, errors[0].Severity)
	assert.Equal(t, sdkerrors.ComponentTypeFan, errors[0].ComponentType)
	require.NotNil(t, errors[0].ComponentID)
	assert.Equal(t, "1", *errors[0].ComponentID)
	assert.Contains(t, errors[0].Summary, "Fan 1")
	assert.Equal(t, "1", errors[0].VendorAttributes["fan_index"])
	assert.Equal(t, "0", errors[0].VendorAttributes["fan_rpm"])
}

func TestDetectFanErrorsFromStats_Healthy(t *testing.T) {
	now := time.Now()

	errors := detectFanErrorsFromStats([]int{7000, 7050, 6980, 7020}, 4, testDeviceIDForErrors, now)

	assert.Empty(t, errors)
}

func TestDetectFanErrorsFromStats_IgnoresInactivePlaceholderSlots(t *testing.T) {
	now := time.Now()

	errors := detectFanErrorsFromStats([]int{7000, 7050, 0, 0}, 2, testDeviceIDForErrors, now)

	assert.Empty(t, errors)
}

func TestDetectFanErrorsFromStats_SkipsChecksWhenNoActiveFansReported(t *testing.T) {
	now := time.Now()

	errors := detectFanErrorsFromStats([]int{0, 0, 0, 0}, 0, testDeviceIDForErrors, now)

	assert.Empty(t, errors)
}

func TestDetectPSUErrorsFromStats_Fault(t *testing.T) {
	now := time.Now()

	errors := detectPSUErrorsFromStats(createTestPSUStats(0, "fault"), testDeviceIDForErrors, now)

	require.Len(t, errors, 1)
	assert.Equal(t, sdkerrors.PSUFaultGeneric, errors[0].MinerError)
	assert.Equal(t, sdkerrors.SeverityMajor, errors[0].Severity)
	assert.Equal(t, sdkerrors.ComponentTypePSU, errors[0].ComponentType)
	require.NotNil(t, errors[0].ComponentID)
	assert.Equal(t, "1", *errors[0].ComponentID)
	assert.Contains(t, errors[0].Summary, "PSU 1")
	assert.Equal(t, "1", errors[0].VendorAttributes["psu_index"])
	assert.Equal(t, "fault", errors[0].VendorAttributes["psu_status"])
}

func TestDetectPSUErrorsFromStats_Healthy(t *testing.T) {
	now := time.Now()

	errors := detectPSUErrorsFromStats(createTestPSUStats(0, "ok"), testDeviceIDForErrors, now)

	assert.Empty(t, errors)
}

// Test: Stats API hardware errors - Major percentage
func TestDetectPerBoardHardwareErrorsFromStats_PercentMajor(t *testing.T) {
	now := time.Now()
	chains := []web.ChainStats{
		createTestChainStats(0, []float64{70.0}, 14000.0, 6.0, 5000), // HWP = 6% > 5% threshold
	}

	errors := detectPerBoardHardwareErrorsFromStats(chains, testDeviceIDForErrors, now)

	require.Len(t, errors, 1)
	assert.Equal(t, sdkerrors.HashboardWarnCRCHigh, errors[0].MinerError)
	assert.Equal(t, sdkerrors.SeverityMajor, errors[0].Severity)
	assert.Contains(t, errors[0].Summary, "6.00%")
	assert.Contains(t, errors[0].VendorAttributes, "hw_error_percent")
	assert.Contains(t, errors[0].VendorAttributes, "hw_error_count")
	assert.Equal(t, "5000", errors[0].VendorAttributes["hw_error_count"])
}

// Test: Stats API hardware errors - Minor percentage
func TestDetectPerBoardHardwareErrorsFromStats_PercentMinor(t *testing.T) {
	now := time.Now()
	chains := []web.ChainStats{
		createTestChainStats(0, []float64{70.0}, 14000.0, 2.0, 1500), // HWP = 2% between 1-5%
	}

	errors := detectPerBoardHardwareErrorsFromStats(chains, testDeviceIDForErrors, now)

	require.Len(t, errors, 1)
	assert.Equal(t, sdkerrors.HashboardWarnCRCHigh, errors[0].MinerError)
	assert.Equal(t, sdkerrors.SeverityMinor, errors[0].Severity)
	assert.Contains(t, errors[0].Summary, "2.00%")
}

// Test: Stats API hardware errors - High count threshold
func TestDetectPerBoardHardwareErrorsFromStats_Count(t *testing.T) {
	now := time.Now()
	chains := []web.ChainStats{
		createTestChainStats(0, []float64{70.0}, 14000.0, 0.5, 1200), // Low % but high count
	}

	errors := detectPerBoardHardwareErrorsFromStats(chains, testDeviceIDForErrors, now)

	require.Len(t, errors, 1)
	assert.Equal(t, sdkerrors.SeverityMinor, errors[0].Severity)
	assert.Contains(t, errors[0].Summary, "1200 hardware errors")
}

// Test: Stats API hardware errors - No errors
func TestDetectPerBoardHardwareErrorsFromStats_NoErrors(t *testing.T) {
	now := time.Now()
	chains := []web.ChainStats{
		createTestChainStats(0, []float64{70.0}, 14000.0, 0.1, 10),
		createTestChainStats(1, []float64{72.0}, 13500.0, 0.2, 20),
	}

	errors := detectPerBoardHardwareErrorsFromStats(chains, testDeviceIDForErrors, now)

	assert.Empty(t, errors, "Should not detect errors with low HW error rates")
}

// ============================================================================
// Fallback Behavior Tests
// ============================================================================

// Test: Fallback from stats to RPC when stats is nil
func TestDetectErrors_FallbackToRPC_StatsNil(t *testing.T) {
	summary := createTestSummaryResponse()
	devs := createTestDevsResponse(
		rpc.DevInfo{
			ASC:         0,
			Status:      "Alive",
			Enabled:     "Y",
			Temperature: 96.0, // Critical temp
			MHSAv:       100000000,
		},
	)

	// Pass nil for stats - should use RPC devs data
	errors := detectErrors(summary, devs, nil, nil, testDeviceIDForErrors, false)

	require.Len(t, errors, 1, "Should detect temperature error from RPC devs")
	assert.Equal(t, sdkerrors.HashboardOverTemperature, errors[0].MinerError)
	// Check that vendor attributes use RPC field name (asc_index not chain_index)
	assert.Contains(t, errors[0].VendorAttributes, "asc_index")
	assert.NotContains(t, errors[0].VendorAttributes, "chain_index")
}

// Test: Fallback from stats to RPC when stats has no chains
func TestDetectErrors_FallbackToRPC_EmptyChains(t *testing.T) {
	summary := createTestSummaryResponse()
	devs := createTestDevsResponse(
		rpc.DevInfo{
			ASC:                   0,
			Status:                "Alive",
			Enabled:               "Y",
			Temperature:           96.0,
			MHSAv:                 100000000,
			DeviceHardwarePercent: 6.0,
			HardwareErrors:        5000,
		},
	)
	stats := &web.StatsInfo{
		STATS: []web.StatsData{
			{Chain: []web.ChainStats{}}, // Empty chains
		},
	}

	errors := detectErrors(summary, devs, nil, stats, testDeviceIDForErrors, false)

	require.Len(t, errors, 2, "Should detect temp + HW errors from RPC devs")
	assert.Contains(t, errors[0].VendorAttributes, "asc_index")
}

// Test: Prefer stats over RPC when stats is available
func TestDetectErrors_PreferStats_WhenAvailable(t *testing.T) {
	summary := createTestSummaryResponse()
	// RPC devs has one error
	devs := createTestDevsResponse(
		rpc.DevInfo{
			ASC:         0,
			Status:      "Alive",
			Enabled:     "Y",
			Temperature: 96.0, // Would trigger error
			MHSAv:       100000000,
		},
	)
	// Stats has different (healthier) data
	stats := &web.StatsInfo{
		STATS: []web.StatsData{
			{
				Chain: []web.ChainStats{
					createTestChainStats(0, []float64{70.0, 72.0}, 14000.0, 0.1, 10), // Healthy
				},
			},
		},
	}

	errors := detectErrors(summary, devs, nil, stats, testDeviceIDForErrors, false)

	// Should use stats data (healthy) not RPC devs data (overheating)
	assert.Empty(t, errors, "Should use healthy stats data, not RPC data")
}

// Test: Stats data takes precedence and uses chain_index
func TestDetectErrors_StatsDataUsesChainIndex(t *testing.T) {
	summary := createTestSummaryResponse()
	stats := &web.StatsInfo{
		STATS: []web.StatsData{
			{
				Chain: []web.ChainStats{
					createTestChainStats(0, []float64{96.0, 97.0}, 14000.0, 0.1, 10), // Critical
				},
			},
		},
	}

	errors := detectErrors(summary, nil, nil, stats, testDeviceIDForErrors, false)

	require.Len(t, errors, 1)
	// Verify it uses stats-specific field name
	assert.Contains(t, errors[0].VendorAttributes, "chain_index")
	assert.Contains(t, errors[0].VendorAttributes, "serial_number")
	assert.NotContains(t, errors[0].VendorAttributes, "asc_index")
}
