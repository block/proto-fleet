package diagnostics

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/btc-mining/proto-fleet/server/internal/domain/diagnostics/models"
)

// ============================================================================
// CalculateStatus Tests
// ============================================================================

func TestCalculateStatus_WithNoErrors_ShouldReturnOK(t *testing.T) {
	// Act
	status := CalculateStatus([]models.ErrorMessage{})

	// Assert
	assert.Equal(t, models.StatusOK, status)
}

func TestCalculateStatus_WithCriticalError_ShouldReturnError(t *testing.T) {
	// Arrange
	errors := []models.ErrorMessage{
		{Severity: models.SeverityCritical},
	}

	// Act
	status := CalculateStatus(errors)

	// Assert
	assert.Equal(t, models.StatusError, status)
}

func TestCalculateStatus_WithMajorError_ShouldReturnWarning(t *testing.T) {
	// Arrange
	errors := []models.ErrorMessage{
		{Severity: models.SeverityMajor},
	}

	// Act
	status := CalculateStatus(errors)

	// Assert
	assert.Equal(t, models.StatusWarning, status)
}

func TestCalculateStatus_WithMinorError_ShouldReturnWarning(t *testing.T) {
	// Arrange
	errors := []models.ErrorMessage{
		{Severity: models.SeverityMinor},
	}

	// Act
	status := CalculateStatus(errors)

	// Assert
	assert.Equal(t, models.StatusWarning, status)
}

func TestCalculateStatus_WithInfoError_ShouldReturnWarning(t *testing.T) {
	// Arrange
	errors := []models.ErrorMessage{
		{Severity: models.SeverityInfo},
	}

	// Act
	status := CalculateStatus(errors)

	// Assert
	assert.Equal(t, models.StatusWarning, status)
}

func TestCalculateStatus_WithMixedSeverities_ShouldReturnHighestStatus(t *testing.T) {
	// Arrange - critical should take precedence
	errors := []models.ErrorMessage{
		{Severity: models.SeverityInfo},
		{Severity: models.SeverityMinor},
		{Severity: models.SeverityCritical},
		{Severity: models.SeverityMajor},
	}

	// Act
	status := CalculateStatus(errors)

	// Assert
	assert.Equal(t, models.StatusError, status)
}

func TestCalculateStatus_WithClosedErrors_ShouldIgnoreThem(t *testing.T) {
	// Arrange
	closedAt := time.Now()
	errors := []models.ErrorMessage{
		{Severity: models.SeverityCritical, ClosedAt: &closedAt}, // Closed critical
		{Severity: models.SeverityMinor},                         // Open minor
	}

	// Act
	status := CalculateStatus(errors)

	// Assert
	assert.Equal(t, models.StatusWarning, status) // Critical is closed, so only minor counts
}

func TestCalculateStatus_WithAllClosedErrors_ShouldReturnOK(t *testing.T) {
	// Arrange
	closedAt := time.Now()
	errors := []models.ErrorMessage{
		{Severity: models.SeverityCritical, ClosedAt: &closedAt},
		{Severity: models.SeverityMajor, ClosedAt: &closedAt},
	}

	// Act
	status := CalculateStatus(errors)

	// Assert
	assert.Equal(t, models.StatusOK, status)
}

// ============================================================================
// GenerateSummary Tests
// ============================================================================

func TestGenerateSummary_WithNoErrors_ShouldReturnNoErrorsSummary(t *testing.T) {
	// Act
	summary := GenerateSummary([]models.ErrorMessage{})

	// Assert
	assert.Equal(t, "No errors", summary.Title)
	assert.Equal(t, "All systems operating normally.", summary.Details)
	assert.Equal(t, "OK", summary.Condensed)
}

func TestGenerateSummary_WithCriticalErrors_ShouldShowCriticalCount(t *testing.T) {
	// Arrange
	errors := []models.ErrorMessage{
		{Severity: models.SeverityCritical},
		{Severity: models.SeverityCritical},
	}

	// Act
	summary := GenerateSummary(errors)

	// Assert
	assert.Equal(t, "2 critical error(s)", summary.Title)
	assert.Contains(t, summary.Details, "2 critical")
	assert.Equal(t, "2 CRIT", summary.Condensed)
}

func TestGenerateSummary_WithMixedSeverities_ShouldShowAll(t *testing.T) {
	// Arrange
	errors := []models.ErrorMessage{
		{Severity: models.SeverityCritical},
		{Severity: models.SeverityMajor},
		{Severity: models.SeverityMajor},
		{Severity: models.SeverityMinor},
		{Severity: models.SeverityInfo},
	}

	// Act
	summary := GenerateSummary(errors)

	// Assert
	assert.Equal(t, "1 critical error(s)", summary.Title)
	assert.Contains(t, summary.Details, "5 active errors")
	assert.Contains(t, summary.Details, "1 critical")
	assert.Contains(t, summary.Details, "2 major")
	assert.Contains(t, summary.Details, "1 minor")
	assert.Contains(t, summary.Details, "1 info")
	assert.Equal(t, "1 CRIT", summary.Condensed)
}

func TestGenerateSummary_WithOnlyMajorErrors_ShouldShowMajor(t *testing.T) {
	// Arrange
	errors := []models.ErrorMessage{
		{Severity: models.SeverityMajor},
		{Severity: models.SeverityMajor},
	}

	// Act
	summary := GenerateSummary(errors)

	// Assert
	assert.Equal(t, "2 major error(s)", summary.Title)
	assert.Equal(t, "2 MAJ", summary.Condensed)
}

func TestGenerateSummary_WithClosedErrors_ShouldIgnoreThem(t *testing.T) {
	// Arrange
	closedAt := time.Now()
	errors := []models.ErrorMessage{
		{Severity: models.SeverityCritical, ClosedAt: &closedAt}, // Closed
		{Severity: models.SeverityMinor},                         // Open
	}

	// Act
	summary := GenerateSummary(errors)

	// Assert
	assert.Equal(t, "1 minor error(s)", summary.Title)
	assert.Equal(t, "1 MIN", summary.Condensed)
}

func TestGenerateSummary_WithOnlyInfoErrors_ShouldShowInfoCount(t *testing.T) {
	// Arrange
	errors := []models.ErrorMessage{
		{Severity: models.SeverityInfo},
		{Severity: models.SeverityInfo},
		{Severity: models.SeverityInfo},
	}

	// Act
	summary := GenerateSummary(errors)

	// Assert
	assert.Equal(t, "3 info message(s)", summary.Title)
	assert.Contains(t, summary.Details, "3 info")
	assert.Equal(t, "3 INFO", summary.Condensed)
}

// ============================================================================
// CountsBySeverity Tests
// ============================================================================

func TestCountsBySeverity_WithEmptySlice_ShouldReturnEmptyMap(t *testing.T) {
	// Act
	counts := CountsBySeverity([]models.ErrorMessage{})

	// Assert
	assert.Empty(t, counts)
}

func TestCountsBySeverity_WithMixedSeverities_ShouldCountCorrectly(t *testing.T) {
	// Arrange
	errors := []models.ErrorMessage{
		{Severity: models.SeverityCritical},
		{Severity: models.SeverityCritical},
		{Severity: models.SeverityMajor},
		{Severity: models.SeverityMinor},
		{Severity: models.SeverityMinor},
		{Severity: models.SeverityMinor},
		{Severity: models.SeverityInfo},
	}

	// Act
	counts := CountsBySeverity(errors)

	// Assert
	assert.Equal(t, int32(2), counts[models.SeverityCritical])
	assert.Equal(t, int32(1), counts[models.SeverityMajor])
	assert.Equal(t, int32(3), counts[models.SeverityMinor])
	assert.Equal(t, int32(1), counts[models.SeverityInfo])
}

func TestCountsBySeverity_ExcludesClosedErrors(t *testing.T) {
	// Arrange - CountsBySeverity should filter closed errors (consistent with CalculateStatus/GenerateSummary)
	closedAt := time.Now()
	errors := []models.ErrorMessage{
		{Severity: models.SeverityCritical, ClosedAt: &closedAt},
		{Severity: models.SeverityCritical},
	}

	// Act
	counts := CountsBySeverity(errors)

	// Assert - only the open error should be counted
	assert.Equal(t, int32(1), counts[models.SeverityCritical])
}

// ============================================================================
// SortErrors Tests
// ============================================================================

func TestSortErrors_ShouldSortBySeverityFirst(t *testing.T) {
	// Arrange
	now := time.Now()
	errors := []models.ErrorMessage{
		{ErrorID: "3", Severity: models.SeverityInfo, LastSeenAt: now},
		{ErrorID: "1", Severity: models.SeverityCritical, LastSeenAt: now},
		{ErrorID: "2", Severity: models.SeverityMajor, LastSeenAt: now},
	}

	// Act
	SortErrors(errors)

	// Assert - Critical (1) < Major (2) < Info (4)
	assert.Equal(t, models.SeverityCritical, errors[0].Severity)
	assert.Equal(t, models.SeverityMajor, errors[1].Severity)
	assert.Equal(t, models.SeverityInfo, errors[2].Severity)
}

func TestSortErrors_WithSameSeverity_ShouldSortByLastSeenDescending(t *testing.T) {
	// Arrange
	now := time.Now()
	errors := []models.ErrorMessage{
		{ErrorID: "OLD", Severity: models.SeverityMajor, LastSeenAt: now.Add(-time.Hour)},
		{ErrorID: "NEW", Severity: models.SeverityMajor, LastSeenAt: now},
		{ErrorID: "MID", Severity: models.SeverityMajor, LastSeenAt: now.Add(-30 * time.Minute)},
	}

	// Act
	SortErrors(errors)

	// Assert - most recent first
	assert.Equal(t, "NEW", errors[0].ErrorID)
	assert.Equal(t, "MID", errors[1].ErrorID)
	assert.Equal(t, "OLD", errors[2].ErrorID)
}

func TestSortErrors_WithSameSeverityAndTime_ShouldSortByErrorIDDescending(t *testing.T) {
	// Arrange
	now := time.Now()
	errors := []models.ErrorMessage{
		{ErrorID: "AAA", Severity: models.SeverityMajor, LastSeenAt: now},
		{ErrorID: "ZZZ", Severity: models.SeverityMajor, LastSeenAt: now},
		{ErrorID: "MMM", Severity: models.SeverityMajor, LastSeenAt: now},
	}

	// Act
	SortErrors(errors)

	// Assert - descending by error ID
	assert.Equal(t, "ZZZ", errors[0].ErrorID)
	assert.Equal(t, "MMM", errors[1].ErrorID)
	assert.Equal(t, "AAA", errors[2].ErrorID)
}

// ============================================================================
// GroupByDevice Tests
// ============================================================================

func TestGroupByDevice_WithEmptySlice_ShouldReturnEmptySlice(t *testing.T) {
	// Act
	result := GroupByDevice([]models.ErrorMessage{}, map[int64]string{})

	// Assert
	assert.Empty(t, result)
}

func TestGroupByDevice_WithSingleDevice_ShouldGroupCorrectly(t *testing.T) {
	// Arrange
	now := time.Now()
	errors := []models.ErrorMessage{
		{DeviceID: "123", Severity: models.SeverityCritical, LastSeenAt: now},
		{DeviceID: "123", Severity: models.SeverityMajor, LastSeenAt: now},
	}
	deviceTypeMap := map[int64]string{123: "S19"}

	// Act
	result := GroupByDevice(errors, deviceTypeMap)

	// Assert
	assert.Len(t, result, 1)
	assert.Equal(t, int64(123), result[0].DeviceID)
	assert.Equal(t, "S19", result[0].DeviceType)
	assert.Len(t, result[0].Errors, 2)
	assert.Equal(t, models.StatusError, result[0].Status)
}

func TestGroupByDevice_WithMultipleDevices_ShouldGroupAndSort(t *testing.T) {
	// Arrange
	now := time.Now()
	errors := []models.ErrorMessage{
		{DeviceID: "100", Severity: models.SeverityMinor, LastSeenAt: now},
		{DeviceID: "200", Severity: models.SeverityCritical, LastSeenAt: now},
		{DeviceID: "100", Severity: models.SeverityMinor, LastSeenAt: now},
	}
	deviceTypeMap := map[int64]string{100: "R2", 200: "S19"}

	// Act
	result := GroupByDevice(errors, deviceTypeMap)

	// Assert
	assert.Len(t, result, 2)
	// Device 200 should be first (has ERROR status due to critical)
	assert.Equal(t, int64(200), result[0].DeviceID)
	assert.Equal(t, models.StatusError, result[0].Status)
	// Device 100 should be second (has WARNING status due to minor)
	assert.Equal(t, int64(100), result[1].DeviceID)
	assert.Equal(t, models.StatusWarning, result[1].Status)
}

// ============================================================================
// GroupByComponent Tests
// ============================================================================

func TestGroupByComponent_WithEmptySlice_ShouldReturnEmptySlice(t *testing.T) {
	// Act
	result := GroupByComponent([]models.ErrorMessage{}, map[int64]string{})

	// Assert
	assert.Empty(t, result)
}

func TestGroupByComponent_WithComponentID_ShouldGroupByComponent(t *testing.T) {
	// Arrange
	now := time.Now()
	hashboard0 := "HB0"
	hashboard1 := "HB1"
	errors := []models.ErrorMessage{
		{DeviceID: "123", ComponentID: &hashboard0, ComponentType: models.ComponentTypeHashBoards, Severity: models.SeverityCritical, LastSeenAt: now},
		{DeviceID: "123", ComponentID: &hashboard0, ComponentType: models.ComponentTypeHashBoards, Severity: models.SeverityMajor, LastSeenAt: now},
		{DeviceID: "123", ComponentID: &hashboard1, ComponentType: models.ComponentTypeHashBoards, Severity: models.SeverityMinor, LastSeenAt: now},
	}
	deviceTypeMap := map[int64]string{123: "S19"}

	// Act
	result := GroupByComponent(errors, deviceTypeMap)

	// Assert
	assert.Len(t, result, 2)
}

func TestGroupByComponent_WithoutComponentID_ShouldGroupByDevice(t *testing.T) {
	// Arrange
	now := time.Now()
	errors := []models.ErrorMessage{
		{DeviceID: "123", ComponentType: models.ComponentTypeUnspecified, Severity: models.SeverityMajor, LastSeenAt: now},
		{DeviceID: "123", ComponentType: models.ComponentTypeUnspecified, Severity: models.SeverityMinor, LastSeenAt: now},
	}
	deviceTypeMap := map[int64]string{123: "S19"}

	// Act
	result := GroupByComponent(errors, deviceTypeMap)

	// Assert
	assert.Len(t, result, 1)
	assert.Equal(t, "", result[0].ComponentID) // No component ID
}

func TestGroupByComponent_SortsByStatus(t *testing.T) {
	// Arrange
	now := time.Now()
	psu := "PSU0"
	fan := "FAN0"
	errors := []models.ErrorMessage{
		{DeviceID: "123", ComponentID: &fan, ComponentType: models.ComponentTypeFans, Severity: models.SeverityMinor, LastSeenAt: now},
		{DeviceID: "123", ComponentID: &psu, ComponentType: models.ComponentTypePSU, Severity: models.SeverityCritical, LastSeenAt: now},
	}
	deviceTypeMap := map[int64]string{123: "S19"}

	// Act
	result := GroupByComponent(errors, deviceTypeMap)

	// Assert
	assert.Len(t, result, 2)
	// PSU should be first (ERROR status)
	assert.Equal(t, models.StatusError, result[0].Status)
	// Fan should be second (WARNING status)
	assert.Equal(t, models.StatusWarning, result[1].Status)
}

func TestGroupByComponent_WithUnparseableDeviceID_ShouldSkipErrors(t *testing.T) {
	// Arrange - errors with non-numeric DeviceIDs should be skipped to prevent cross-contamination
	now := time.Now()
	hb0 := "HB0"
	errors := []models.ErrorMessage{
		{DeviceID: "not-a-number", ComponentID: &hb0, ComponentType: models.ComponentTypeHashBoards, Severity: models.SeverityCritical, LastSeenAt: now},
		{DeviceID: "also-invalid", ComponentID: &hb0, ComponentType: models.ComponentTypeHashBoards, Severity: models.SeverityMajor, LastSeenAt: now},
	}

	// Act
	result := GroupByComponent(errors, map[int64]string{})

	// Assert - all errors should be skipped since deviceID parses to 0
	assert.Empty(t, result)
}

func TestGroupByComponent_WithMixedValidAndInvalidDeviceIDs_ShouldOnlyIncludeValid(t *testing.T) {
	// Arrange
	now := time.Now()
	hb0 := "HB0"
	errors := []models.ErrorMessage{
		{DeviceID: "123", ComponentID: &hb0, ComponentType: models.ComponentTypeHashBoards, Severity: models.SeverityCritical, LastSeenAt: now},
		{DeviceID: "invalid", ComponentID: &hb0, ComponentType: models.ComponentTypeHashBoards, Severity: models.SeverityMajor, LastSeenAt: now},
	}
	deviceTypeMap := map[int64]string{123: "S19"}

	// Act
	result := GroupByComponent(errors, deviceTypeMap)

	// Assert - only the valid deviceID error should be included
	assert.Len(t, result, 1)
	assert.Equal(t, int64(123), result[0].DeviceID)
	assert.Len(t, result[0].Errors, 1)
}

func TestGroupByDevice_WithUnparseableDeviceID_ShouldSkipErrors(t *testing.T) {
	// Arrange - errors with non-numeric DeviceIDs should be skipped
	now := time.Now()
	errors := []models.ErrorMessage{
		{DeviceID: "not-a-number", Severity: models.SeverityCritical, LastSeenAt: now},
		{DeviceID: "also-invalid", Severity: models.SeverityMajor, LastSeenAt: now},
	}

	// Act
	result := GroupByDevice(errors, map[int64]string{})

	// Assert - all errors should be skipped
	assert.Empty(t, result)
}

func TestGroupByDevice_WithMixedValidAndInvalidDeviceIDs_ShouldOnlyIncludeValid(t *testing.T) {
	// Arrange
	now := time.Now()
	errors := []models.ErrorMessage{
		{DeviceID: "456", Severity: models.SeverityCritical, LastSeenAt: now},
		{DeviceID: "invalid-id", Severity: models.SeverityMajor, LastSeenAt: now},
		{DeviceID: "456", Severity: models.SeverityMinor, LastSeenAt: now},
	}
	deviceTypeMap := map[int64]string{456: "R2"}

	// Act
	result := GroupByDevice(errors, deviceTypeMap)

	// Assert - only valid deviceID errors should be grouped
	assert.Len(t, result, 1)
	assert.Equal(t, int64(456), result[0].DeviceID)
	assert.Len(t, result[0].Errors, 2)
}
