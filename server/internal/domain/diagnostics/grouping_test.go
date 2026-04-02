package diagnostics

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/block/proto-fleet/server/internal/domain/diagnostics/models"
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
	result := GroupByDevice([]models.ErrorMessage{}, map[string]models.DeviceKey{})

	// Assert
	assert.Empty(t, result)
}

func TestGroupByDevice_WithSingleDevice_ShouldGroupCorrectly(t *testing.T) {
	// Arrange - use realistic device identifier (not a numeric string)
	now := time.Now()
	errors := []models.ErrorMessage{
		{DeviceID: "proto-123", DeviceType: "S19", Severity: models.SeverityCritical, LastSeenAt: now},
		{DeviceID: "proto-123", DeviceType: "S19", Severity: models.SeverityMajor, LastSeenAt: now},
	}
	deviceKeyMap := map[string]models.DeviceKey{
		"proto-123": {DeviceID: 123, DeviceIdentifier: "proto-123"},
	}

	// Act
	result := GroupByDevice(errors, deviceKeyMap)

	// Assert
	assert.Len(t, result, 1)
	assert.Equal(t, int64(123), result[0].DeviceID)
	assert.Equal(t, "S19", result[0].DeviceType)
	assert.Len(t, result[0].Errors, 2)
	assert.Equal(t, models.StatusError, result[0].Status)
}

func TestGroupByDevice_WithMultipleDevices_ShouldGroupAndSort(t *testing.T) {
	// Arrange - use realistic device identifiers
	now := time.Now()
	errors := []models.ErrorMessage{
		{DeviceID: "antminer-100", DeviceType: "R2", Severity: models.SeverityMinor, LastSeenAt: now},
		{DeviceID: "proto-200", DeviceType: "S19", Severity: models.SeverityCritical, LastSeenAt: now},
		{DeviceID: "antminer-100", DeviceType: "R2", Severity: models.SeverityMinor, LastSeenAt: now},
	}
	deviceKeyMap := map[string]models.DeviceKey{
		"antminer-100": {DeviceID: 100, DeviceIdentifier: "antminer-100"},
		"proto-200":    {DeviceID: 200, DeviceIdentifier: "proto-200"},
	}

	// Act
	result := GroupByDevice(errors, deviceKeyMap)

	// Assert
	assert.Len(t, result, 2)
	// Device proto-200 should be first (has ERROR status due to critical)
	assert.Equal(t, int64(200), result[0].DeviceID)
	assert.Equal(t, models.StatusError, result[0].Status)
	// Device antminer-100 should be second (has WARNING status due to minor)
	assert.Equal(t, int64(100), result[1].DeviceID)
	assert.Equal(t, models.StatusWarning, result[1].Status)
}

func TestGroupByDevice_WithDeviceNotInKeyMap_ShouldSkipErrors(t *testing.T) {
	// Arrange - errors for devices not in the key map should be skipped
	now := time.Now()
	errors := []models.ErrorMessage{
		{DeviceID: "proto-123", DeviceType: "S19", Severity: models.SeverityCritical, LastSeenAt: now},
		{DeviceID: "unknown-device", DeviceType: "X1", Severity: models.SeverityMajor, LastSeenAt: now},
	}
	deviceKeyMap := map[string]models.DeviceKey{
		"proto-123": {DeviceID: 123, DeviceIdentifier: "proto-123"},
	}

	// Act
	result := GroupByDevice(errors, deviceKeyMap)

	// Assert - only device in key map should be included
	assert.Len(t, result, 1)
	assert.Equal(t, int64(123), result[0].DeviceID)
	assert.Len(t, result[0].Errors, 1)
}

// ============================================================================
// GroupByComponent Tests
// ============================================================================

func TestGroupByComponent_WithEmptySlice_ShouldReturnEmptySlice(t *testing.T) {
	// Act
	result := GroupByComponent([]models.ErrorMessage{}, map[string]models.ComponentKey{})

	// Assert
	assert.Empty(t, result)
}

func TestGroupByComponent_WithComponentID_ShouldGroupByComponent(t *testing.T) {
	// Arrange - use realistic device identifier
	now := time.Now()
	hashboard0 := "HB0"
	hashboard1 := "HB1"
	errors := []models.ErrorMessage{
		{DeviceID: "proto-123", ComponentID: &hashboard0, ComponentType: models.ComponentTypeHashBoards, DeviceType: "S19", Severity: models.SeverityCritical, LastSeenAt: now},
		{DeviceID: "proto-123", ComponentID: &hashboard0, ComponentType: models.ComponentTypeHashBoards, DeviceType: "S19", Severity: models.SeverityMajor, LastSeenAt: now},
		{DeviceID: "proto-123", ComponentID: &hashboard1, ComponentType: models.ComponentTypeHashBoards, DeviceType: "S19", Severity: models.SeverityMinor, LastSeenAt: now},
	}
	componentKeyMap := map[string]models.ComponentKey{
		"proto-123_2_HB0": {DeviceID: 123, DeviceIdentifier: "proto-123", ComponentType: models.ComponentTypeHashBoards, ComponentID: &hashboard0},
		"proto-123_2_HB1": {DeviceID: 123, DeviceIdentifier: "proto-123", ComponentType: models.ComponentTypeHashBoards, ComponentID: &hashboard1},
	}

	// Act
	result := GroupByComponent(errors, componentKeyMap)

	// Assert
	assert.Len(t, result, 2)
}

func TestGroupByComponent_WithoutComponentID_ShouldGroupByDevice(t *testing.T) {
	// Arrange - use realistic device identifier
	now := time.Now()
	errors := []models.ErrorMessage{
		{DeviceID: "proto-123", ComponentType: models.ComponentTypeUnspecified, DeviceType: "S19", Severity: models.SeverityMajor, LastSeenAt: now},
		{DeviceID: "proto-123", ComponentType: models.ComponentTypeUnspecified, DeviceType: "S19", Severity: models.SeverityMinor, LastSeenAt: now},
	}
	componentKeyMap := map[string]models.ComponentKey{
		"proto-123_0_device": {DeviceID: 123, DeviceIdentifier: "proto-123", ComponentType: models.ComponentTypeUnspecified, ComponentID: nil},
	}

	// Act
	result := GroupByComponent(errors, componentKeyMap)

	// Assert
	assert.Len(t, result, 1)
	assert.Equal(t, "", result[0].ComponentID) // No component ID
}

func TestGroupByComponent_SortsByStatus(t *testing.T) {
	// Arrange - use realistic device identifier
	now := time.Now()
	psu := "PSU0"
	fan := "FAN0"
	errors := []models.ErrorMessage{
		{DeviceID: "proto-123", ComponentID: &fan, ComponentType: models.ComponentTypeFans, DeviceType: "S19", Severity: models.SeverityMinor, LastSeenAt: now},
		{DeviceID: "proto-123", ComponentID: &psu, ComponentType: models.ComponentTypePSU, DeviceType: "S19", Severity: models.SeverityCritical, LastSeenAt: now},
	}
	componentKeyMap := map[string]models.ComponentKey{
		"proto-123_1_PSU0": {DeviceID: 123, DeviceIdentifier: "proto-123", ComponentType: models.ComponentTypePSU, ComponentID: &psu},
		"proto-123_3_FAN0": {DeviceID: 123, DeviceIdentifier: "proto-123", ComponentType: models.ComponentTypeFans, ComponentID: &fan},
	}

	// Act
	result := GroupByComponent(errors, componentKeyMap)

	// Assert
	assert.Len(t, result, 2)
	// PSU should be first (ERROR status)
	assert.Equal(t, models.StatusError, result[0].Status)
	// Fan should be second (WARNING status)
	assert.Equal(t, models.StatusWarning, result[1].Status)
}

func TestGroupByComponent_WithComponentNotInKeyMap_ShouldSkipErrors(t *testing.T) {
	// Arrange - errors for components not in the key map should be skipped
	now := time.Now()
	hb0 := "HB0"
	errors := []models.ErrorMessage{
		{DeviceID: "proto-123", ComponentID: &hb0, ComponentType: models.ComponentTypeHashBoards, DeviceType: "S19", Severity: models.SeverityCritical, LastSeenAt: now},
	}
	componentKeyMap := map[string]models.ComponentKey{} // Empty map

	// Act
	result := GroupByComponent(errors, componentKeyMap)

	// Assert - no components should be included
	assert.Empty(t, result)
}

func TestGroupByComponent_WithMixedKeysInMap_ShouldOnlyIncludeMatched(t *testing.T) {
	// Arrange - only errors for components in the key map should be included
	now := time.Now()
	hb0 := "HB0"
	hb1 := "HB1"
	errors := []models.ErrorMessage{
		{DeviceID: "proto-123", ComponentID: &hb0, ComponentType: models.ComponentTypeHashBoards, DeviceType: "S19", Severity: models.SeverityCritical, LastSeenAt: now},
		{DeviceID: "proto-123", ComponentID: &hb1, ComponentType: models.ComponentTypeHashBoards, DeviceType: "S19", Severity: models.SeverityMajor, LastSeenAt: now},
	}
	componentKeyMap := map[string]models.ComponentKey{
		"proto-123_2_HB0": {DeviceID: 123, DeviceIdentifier: "proto-123", ComponentType: models.ComponentTypeHashBoards, ComponentID: &hb0},
	}

	// Act
	result := GroupByComponent(errors, componentKeyMap)

	// Assert - only HB0 component should be included
	assert.Len(t, result, 1)
	assert.Equal(t, int64(123), result[0].DeviceID)
	assert.Len(t, result[0].Errors, 1)
	assert.Equal(t, "HB0", result[0].ComponentID)
}

// TestGroupByComponent_WithSameComponentIDButDifferentTypes_ShouldKeepSeparate verifies that components with the same ID but different types are kept separate.
// This test ensures that errors with the same component_id but different component_types (e.g., pool errors
// vs hashboard errors both with component_id='0') are kept separate and not incorrectly grouped together.
func TestGroupByComponent_WithSameComponentIDButDifferentTypes_ShouldKeepSeparate(t *testing.T) {
	// Arrange - device has errors with same component_id but different component_types
	now := time.Now()
	componentID0 := "0"

	errors := []models.ErrorMessage{
		// Pool error with component_type=Unspecified and component_id='0'
		{DeviceID: "proto-123", ComponentID: &componentID0, ComponentType: models.ComponentTypeUnspecified, DeviceType: "S19", Severity: models.SeverityMajor, LastSeenAt: now},
		// Hashboard error with component_type=HashBoards and component_id='0'
		{DeviceID: "proto-123", ComponentID: &componentID0, ComponentType: models.ComponentTypeHashBoards, DeviceType: "S19", Severity: models.SeverityCritical, LastSeenAt: now},
	}

	// Key map includes component_type to distinguish the two components
	componentKeyMap := map[string]models.ComponentKey{
		"proto-123_0_0": {DeviceID: 123, DeviceIdentifier: "proto-123", ComponentType: models.ComponentTypeUnspecified, ComponentID: &componentID0},
		"proto-123_2_0": {DeviceID: 123, DeviceIdentifier: "proto-123", ComponentType: models.ComponentTypeHashBoards, ComponentID: &componentID0},
	}

	// Act
	result := GroupByComponent(errors, componentKeyMap)

	// Assert - should have 2 separate component groups, not 1
	assert.Len(t, result, 2, "Errors with same component_id but different component_types should be kept separate")

	// Verify each component group has the correct type and errors
	var poolComponent, hashboardComponent *models.ComponentErrors
	for i := range result {
		if result[i].ComponentType == models.ComponentTypeUnspecified {
			poolComponent = &result[i]
		} else if result[i].ComponentType == models.ComponentTypeHashBoards {
			hashboardComponent = &result[i]
		}
	}

	assert.NotNil(t, poolComponent, "Pool component should be present")
	assert.NotNil(t, hashboardComponent, "Hashboard component should be present")

	assert.Equal(t, models.ComponentTypeUnspecified, poolComponent.ComponentType)
	assert.Len(t, poolComponent.Errors, 1, "Pool component should have 1 error")
	assert.Equal(t, models.SeverityMajor, poolComponent.Errors[0].Severity)

	assert.Equal(t, models.ComponentTypeHashBoards, hashboardComponent.ComponentType)
	assert.Len(t, hashboardComponent.Errors, 1, "Hashboard component should have 1 error")
	assert.Equal(t, models.SeverityCritical, hashboardComponent.Errors[0].Severity)
}
