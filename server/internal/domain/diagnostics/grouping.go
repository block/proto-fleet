package diagnostics

import (
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"

	"github.com/btc-mining/proto-fleet/server/internal/domain/diagnostics/models"
)

const (
	// invalidDeviceID indicates a device ID that failed to parse as int64
	invalidDeviceID = 0
	// decimalBase is the numeric base for parsing device IDs
	decimalBase = 10
	// int64Bits is the bit size for parsing device IDs
	int64Bits = 64
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
// deviceTypeMap maps device_id to device type (model name).
// Errors with unparseable device IDs are skipped to prevent cross-contamination.
func GroupByComponent(errors []models.ErrorMessage, deviceTypeMap map[int64]string) []models.ComponentErrors {
	componentMap := make(map[string][]models.ErrorMessage)
	componentDeviceMap := make(map[string]int64)
	componentTypeMap := make(map[string]models.ComponentType)

	for _, err := range errors {
		deviceID := parseDeviceID(err.DeviceID)
		if deviceID == invalidDeviceID {
			continue
		}
		key := buildComponentKey(err)
		componentMap[key] = append(componentMap[key], err)

		if _, exists := componentDeviceMap[key]; !exists {
			componentDeviceMap[key] = deviceID
			componentTypeMap[key] = err.ComponentType
		}
	}

	var result []models.ComponentErrors
	for key, compErrors := range componentMap {
		if len(compErrors) == 0 {
			continue
		}

		deviceID := componentDeviceMap[key]
		componentType := componentTypeMap[key]

		var componentID string
		if compErrors[0].ComponentID != nil {
			componentID = *compErrors[0].ComponentID
		}

		result = append(result, models.ComponentErrors{
			ComponentID:      componentID,
			ComponentType:    componentType,
			DeviceID:         deviceID,
			DeviceType:       deviceTypeMap[deviceID],
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
// deviceTypeMap maps device_id to device type (model name).
// Errors with unparseable device IDs are skipped to prevent cross-contamination.
func GroupByDevice(errors []models.ErrorMessage, deviceTypeMap map[int64]string) []models.DeviceErrorGroup {
	deviceMap := make(map[int64][]models.ErrorMessage)
	for _, err := range errors {
		deviceID := parseDeviceID(err.DeviceID)
		if deviceID == invalidDeviceID {
			continue
		}
		deviceMap[deviceID] = append(deviceMap[deviceID], err)
	}

	var result []models.DeviceErrorGroup
	for deviceID, devErrors := range deviceMap {
		result = append(result, models.DeviceErrorGroup{
			DeviceID:         deviceID,
			DeviceType:       deviceTypeMap[deviceID],
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

// buildComponentKey creates a unique key for grouping errors by component.
// Returns "{deviceID}_{componentID}" or "{deviceID}_device" if no component ID.
func buildComponentKey(err models.ErrorMessage) string {
	if err.ComponentID != nil && *err.ComponentID != "" {
		return fmt.Sprintf("%s_%s", err.DeviceID, *err.ComponentID)
	}
	return fmt.Sprintf("%s_%s", err.DeviceID, deviceLevelComponentKey)
}

// parseDeviceID converts a device identifier string to int64.
// Returns invalidDeviceID (0) if parsing fails. Empty strings return 0 silently,
// while non-empty invalid strings are logged at debug level.
func parseDeviceID(deviceID string) int64 {
	if deviceID == "" {
		return invalidDeviceID
	}
	id, err := strconv.ParseInt(deviceID, decimalBase, int64Bits)
	if err != nil {
		slog.Debug("failed to parse device ID as int64", "deviceID", deviceID, "error", err)
		return invalidDeviceID
	}
	return id
}
