package diagnostics

import (
	"fmt"
	"sort"
	"strings"

	"github.com/block/proto-fleet/server/internal/domain/diagnostics/models"
)

const (
	// deviceLevelComponentKey is the suffix used for device-level errors (no component)
	deviceLevelComponentKey = "device"
)

// ============================================================================
// Status Calculation
// ============================================================================

// CalculateStatus computes the aggregate status from a list of errors.
// Uses waterfall logic: Critical → Error, any other severity → Warning, else OK.
// Only considers open errors (ClosedAt == nil).
func CalculateStatus(errors []models.ErrorMessage) models.Status {
	if len(errors) == 0 {
		return models.StatusOK
	}

	hasNonCritical := false

	for _, err := range errors {
		if err.ClosedAt != nil {
			continue
		}
		switch err.Severity {
		case models.SeverityCritical:
			return models.StatusError
		case models.SeverityMajor, models.SeverityMinor, models.SeverityInfo:
			hasNonCritical = true
		}
	}

	if hasNonCritical {
		return models.StatusWarning
	}
	return models.StatusOK
}

// ============================================================================
// Summary Generation
// ============================================================================

// GenerateSummary creates a human-readable summary for a list of errors.
// Only considers open errors (ClosedAt == nil).
func GenerateSummary(errors []models.ErrorMessage) models.Summary {
	if len(errors) == 0 {
		return models.Summary{
			Title:     "No errors",
			Details:   "All systems operating normally.",
			Condensed: "OK",
		}
	}

	counts := CountsBySeverity(errors)
	criticalCount := int(counts[models.SeverityCritical])
	majorCount := int(counts[models.SeverityMajor])
	minorCount := int(counts[models.SeverityMinor])
	infoCount := int(counts[models.SeverityInfo])
	totalOpen := criticalCount + majorCount + minorCount + infoCount

	var title string
	switch {
	case criticalCount > 0:
		title = fmt.Sprintf("%d critical error(s)", criticalCount)
	case majorCount > 0:
		title = fmt.Sprintf("%d major error(s)", majorCount)
	case minorCount > 0:
		title = fmt.Sprintf("%d minor error(s)", minorCount)
	case infoCount > 0:
		title = fmt.Sprintf("%d info message(s)", infoCount)
	default:
		title = "No active errors"
	}

	var details string
	if totalOpen > 0 {
		var parts []string
		if criticalCount > 0 {
			parts = append(parts, fmt.Sprintf("%d critical", criticalCount))
		}
		if majorCount > 0 {
			parts = append(parts, fmt.Sprintf("%d major", majorCount))
		}
		if minorCount > 0 {
			parts = append(parts, fmt.Sprintf("%d minor", minorCount))
		}
		if infoCount > 0 {
			parts = append(parts, fmt.Sprintf("%d info", infoCount))
		}
		details = fmt.Sprintf("%d active errors: %s", totalOpen, strings.Join(parts, ", "))
	} else {
		details = "All errors have been resolved."
	}

	var condensed string
	switch {
	case criticalCount > 0:
		condensed = fmt.Sprintf("%d CRIT", criticalCount)
	case majorCount > 0:
		condensed = fmt.Sprintf("%d MAJ", majorCount)
	case minorCount > 0:
		condensed = fmt.Sprintf("%d MIN", minorCount)
	case infoCount > 0:
		condensed = fmt.Sprintf("%d INFO", infoCount)
	default:
		condensed = "OK"
	}

	return models.Summary{
		Title:     title,
		Details:   details,
		Condensed: condensed,
	}
}

// ============================================================================
// Counts by Severity
// ============================================================================

// CountsBySeverity returns error counts grouped by severity level.
// Only considers open errors (ClosedAt == nil), consistent with CalculateStatus and GenerateSummary.
func CountsBySeverity(errors []models.ErrorMessage) map[models.Severity]int32 {
	counts := make(map[models.Severity]int32)
	for _, err := range errors {
		if err.ClosedAt != nil {
			continue
		}
		counts[err.Severity]++
	}
	return counts
}

// ============================================================================
// Sorting
// ============================================================================

// SortErrors sorts errors by priority (critical first), then by recency (most recent first).
// Lower Severity enum values indicate higher priority (Critical=1, Info=4).
func SortErrors(errors []models.ErrorMessage) {
	sort.Slice(errors, func(i, j int) bool {
		if errors[i].Severity != errors[j].Severity {
			return errors[i].Severity < errors[j].Severity
		}
		// Then by last_seen (descending - most recent first).
		if !errors[i].LastSeenAt.Equal(errors[j].LastSeenAt) {
			return errors[i].LastSeenAt.After(errors[j].LastSeenAt)
		}
		// Finally by error_id (descending).
		return errors[i].ErrorID > errors[j].ErrorID
	})
}

// ============================================================================
// Grouping Functions
// ============================================================================

// GroupByComponent groups errors by their component, returning ComponentErrors slices.
// componentKeyMap maps "deviceIdentifier_componentID" to ComponentKey containing numeric device ID.
// Errors for components not in componentKeyMap are skipped (shouldn't happen in normal flow).
func GroupByComponent(errors []models.ErrorMessage, componentKeyMap map[string]models.ComponentKey) []models.ComponentErrors {
	componentMap := make(map[string][]models.ErrorMessage)

	for _, err := range errors {
		key := buildComponentKeyFromError(err)
		componentMap[key] = append(componentMap[key], err)
	}

	var result []models.ComponentErrors
	for key, compErrors := range componentMap {
		if len(compErrors) == 0 {
			continue
		}

		compKey, exists := componentKeyMap[key]
		if !exists {
			continue
		}

		var componentID string
		if compErrors[0].ComponentID != nil {
			componentID = *compErrors[0].ComponentID
		}

		// Get device type from first error (all errors for same component have same type)
		deviceType := ""
		if len(compErrors) > 0 {
			deviceType = compErrors[0].DeviceType
		}

		result = append(result, models.ComponentErrors{
			ComponentID:      componentID,
			ComponentType:    compErrors[0].ComponentType,
			DeviceID:         compKey.DeviceID,
			DeviceType:       deviceType,
			Status:           CalculateStatus(compErrors),
			Summary:          GenerateSummary(compErrors),
			Errors:           compErrors,
			CountsBySeverity: CountsBySeverity(compErrors),
		})
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Status > result[j].Status
	})

	return result
}

// GroupByDevice groups errors by their device, returning DeviceErrorGroup slices.
// deviceKeyMap maps device_identifier (string) to DeviceKey containing numeric ID and metadata.
// Errors for devices not in deviceKeyMap are skipped (shouldn't happen in normal flow).
func GroupByDevice(errors []models.ErrorMessage, deviceKeyMap map[string]models.DeviceKey) []models.DeviceErrorGroup {
	deviceMap := make(map[string][]models.ErrorMessage)
	for _, err := range errors {
		deviceMap[err.DeviceID] = append(deviceMap[err.DeviceID], err)
	}

	var result []models.DeviceErrorGroup
	for deviceIdentifier, devErrors := range deviceMap {
		deviceKey, exists := deviceKeyMap[deviceIdentifier]
		if !exists {
			continue
		}
		// Get device type from first error (all errors for same device have same type)
		deviceType := ""
		if len(devErrors) > 0 {
			deviceType = devErrors[0].DeviceType
		}
		result = append(result, models.DeviceErrorGroup{
			DeviceID:         deviceKey.DeviceID,
			DeviceType:       deviceType,
			Status:           CalculateStatus(devErrors),
			Summary:          GenerateSummary(devErrors),
			Errors:           devErrors,
			CountsBySeverity: CountsBySeverity(devErrors),
		})
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Status > result[j].Status
	})

	return result
}

// ============================================================================
// Helper Functions
// ============================================================================

// buildComponentKeyFromError creates a unique key for grouping errors by component.
// Returns "{deviceIdentifier}_{componentType}_{componentID}" or "{deviceIdentifier}_{componentType}_device" if no component ID.
// Uses device identifier string (not numeric ID) as the key prefix.
// Includes component_type to distinguish errors on the same component_id but different component_types.
func buildComponentKeyFromError(err models.ErrorMessage) string {
	if err.ComponentID != nil && *err.ComponentID != "" {
		return fmt.Sprintf("%s_%d_%s", err.DeviceID, err.ComponentType, *err.ComponentID)
	}
	return fmt.Sprintf("%s_%d_%s", err.DeviceID, err.ComponentType, deviceLevelComponentKey)
}

// buildComponentKeyFromKey creates a unique key from a ComponentKey for map lookups.
// Returns "{deviceIdentifier}_{componentType}_{componentID}" or "{deviceIdentifier}_{componentType}_device" if no component ID.
// Includes component_type to distinguish components with the same component_id but different component_types.
func buildComponentKeyFromKey(key models.ComponentKey) string {
	if key.ComponentID != nil && *key.ComponentID != "" {
		return fmt.Sprintf("%s_%d_%s", key.DeviceIdentifier, key.ComponentType, *key.ComponentID)
	}
	return fmt.Sprintf("%s_%d_%s", key.DeviceIdentifier, key.ComponentType, deviceLevelComponentKey)
}
