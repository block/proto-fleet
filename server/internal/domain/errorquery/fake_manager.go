package errorquery

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"sort"
	"sync"
	"time"

	errorsv1 "github.com/btc-mining/proto-fleet/server/generated/grpc/errors/v1"
	"github.com/oklog/ulid/v2"
)

// Configuration constants for random error generation.
const (
	// Probability that a device has any errors (30%).
	errorProbabilityPercent = 30
	probabilityMax          = 100

	// Range of components to affect per device.
	minComponentsAffected = 1
	maxComponentsAffected = 3

	// Range of errors per component.
	minErrorsPerComponent = 1
	maxErrorsPerComponent = 2

	// Time range for first_seen timestamps (up to 7 days ago).
	maxFirstSeenAgeDays = 7

	// Time range for last_seen timestamps (up to 1 hour ago).
	maxLastSeenAgeHours = 1

	// Probability that an error is closed (10%).
	closedErrorProbabilityPercent = 10
)

// Error code ranges by component type.
var componentErrorRanges = map[errorsv1.ComponentType][]errorsv1.MinerError{
	errorsv1.ComponentType_COMPONENT_TYPE_PSU: {
		errorsv1.MinerError_MINER_ERROR_PSU_NOT_PRESENT,
		errorsv1.MinerError_MINER_ERROR_PSU_MODEL_MISMATCH,
		errorsv1.MinerError_MINER_ERROR_PSU_COMMUNICATION_LOST,
		errorsv1.MinerError_MINER_ERROR_PSU_FAULT_GENERIC,
		errorsv1.MinerError_MINER_ERROR_PSU_INPUT_VOLTAGE_LOW,
		errorsv1.MinerError_MINER_ERROR_PSU_INPUT_VOLTAGE_HIGH,
		errorsv1.MinerError_MINER_ERROR_PSU_OUTPUT_VOLTAGE_FAULT,
		errorsv1.MinerError_MINER_ERROR_PSU_OUTPUT_OVERCURRENT,
		errorsv1.MinerError_MINER_ERROR_PSU_FAN_FAILED,
		errorsv1.MinerError_MINER_ERROR_PSU_OVER_TEMPERATURE,
		errorsv1.MinerError_MINER_ERROR_PSU_INPUT_PHASE_IMBALANCE,
		errorsv1.MinerError_MINER_ERROR_PSU_UNDER_TEMPERATURE,
	},
	errorsv1.ComponentType_COMPONENT_TYPE_FAN: {
		errorsv1.MinerError_MINER_ERROR_FAN_FAILED,
		errorsv1.MinerError_MINER_ERROR_FAN_TACH_SIGNAL_LOST,
		errorsv1.MinerError_MINER_ERROR_FAN_SPEED_DEVIATION,
		errorsv1.MinerError_MINER_ERROR_INLET_OVER_TEMPERATURE,
		errorsv1.MinerError_MINER_ERROR_DEVICE_OVER_TEMPERATURE,
		errorsv1.MinerError_MINER_ERROR_DEVICE_UNDER_TEMPERATURE,
		errorsv1.MinerError_MINER_ERROR_THERMAL_MARGIN_LOW,
	},
	errorsv1.ComponentType_COMPONENT_TYPE_HASH_BOARD: {
		errorsv1.MinerError_MINER_ERROR_HASHBOARD_NOT_PRESENT,
		errorsv1.MinerError_MINER_ERROR_HASHBOARD_OVER_TEMPERATURE,
		errorsv1.MinerError_MINER_ERROR_HASHBOARD_MISSING_CHIPS,
		errorsv1.MinerError_MINER_ERROR_ASIC_CHAIN_COMMUNICATION_LOST,
		errorsv1.MinerError_MINER_ERROR_ASIC_CLOCK_PLL_UNLOCKED,
		errorsv1.MinerError_MINER_ERROR_ASIC_CRC_ERROR_EXCESSIVE,
		errorsv1.MinerError_MINER_ERROR_HASHBOARD_ASIC_OVER_TEMPERATURE,
		errorsv1.MinerError_MINER_ERROR_HASHBOARD_ASIC_UNDER_TEMPERATURE,
		errorsv1.MinerError_MINER_ERROR_BOARD_POWER_PGOOD_MISSING,
		errorsv1.MinerError_MINER_ERROR_BOARD_POWER_OVERCURRENT_TRIP,
		errorsv1.MinerError_MINER_ERROR_BOARD_POWER_RAIL_UNDERVOLT,
		errorsv1.MinerError_MINER_ERROR_BOARD_POWER_RAIL_OVERVOLT,
		errorsv1.MinerError_MINER_ERROR_BOARD_POWER_SHORT_DETECTED,
		errorsv1.MinerError_MINER_ERROR_HASHRATE_BELOW_TARGET,
		errorsv1.MinerError_MINER_ERROR_HASHBOARD_WARN_CRC_HIGH,
	},
	errorsv1.ComponentType_COMPONENT_TYPE_CONTROL_BOARD: {
		errorsv1.MinerError_MINER_ERROR_TEMP_SENSOR_OPEN_OR_SHORT,
		errorsv1.MinerError_MINER_ERROR_TEMP_SENSOR_FAULT,
		errorsv1.MinerError_MINER_ERROR_VOLTAGE_SENSOR_FAULT,
		errorsv1.MinerError_MINER_ERROR_CURRENT_SENSOR_FAULT,
		errorsv1.MinerError_MINER_ERROR_FIRMWARE_IMAGE_INVALID,
		errorsv1.MinerError_MINER_ERROR_FIRMWARE_CONFIG_INVALID,
		errorsv1.MinerError_MINER_ERROR_CONTROL_BOARD_COMMUNICATION_LOST,
		errorsv1.MinerError_MINER_ERROR_CONTROL_BOARD_FAILURE,
		errorsv1.MinerError_MINER_ERROR_DEVICE_INTERNAL_BUS_FAULT,
		errorsv1.MinerError_MINER_ERROR_DEVICE_COMMUNICATION_LOST,
	},
	errorsv1.ComponentType_COMPONENT_TYPE_EEPROM: {
		errorsv1.MinerError_MINER_ERROR_EEPROM_CRC_MISMATCH,
		errorsv1.MinerError_MINER_ERROR_EEPROM_READ_FAILURE,
	},
	errorsv1.ComponentType_COMPONENT_TYPE_IO_MODULE: {
		errorsv1.MinerError_MINER_ERROR_IO_MODULE_FAILURE,
	},
}

// Component instance counts per device type.
var componentCounts = map[errorsv1.ComponentType]int{
	errorsv1.ComponentType_COMPONENT_TYPE_PSU:           2,
	errorsv1.ComponentType_COMPONENT_TYPE_FAN:           4,
	errorsv1.ComponentType_COMPONENT_TYPE_HASH_BOARD:    3,
	errorsv1.ComponentType_COMPONENT_TYPE_CONTROL_BOARD: 1,
	errorsv1.ComponentType_COMPONENT_TYPE_EEPROM:        1,
	errorsv1.ComponentType_COMPONENT_TYPE_IO_MODULE:     1,
}

// FakeErrorManager provides in-memory error management with seeded and generated errors.
type FakeErrorManager struct {
	mu              sync.RWMutex
	seededErrors    map[int64][]ErrorRecord // deviceID -> errors
	generatedErrors map[int64][]ErrorRecord // deviceID -> errors
	deviceTypes     map[int64]string        // deviceID -> device type (model)
	metadata        map[errorsv1.MinerError]*MinerErrorMetadata
	errorIndex      map[string]*ErrorRecord // errorID -> error for fast lookup
}

// NewFakeErrorManager creates a new fake error manager with empty state.
func NewFakeErrorManager() *FakeErrorManager {
	return &FakeErrorManager{
		seededErrors:    make(map[int64][]ErrorRecord),
		generatedErrors: make(map[int64][]ErrorRecord),
		deviceTypes:     make(map[int64]string),
		metadata:        BuildMinerErrorMetadata(),
		errorIndex:      make(map[string]*ErrorRecord),
	}
}

// Seed adds controlled test data to the manager.
func (m *FakeErrorManager) Seed(data []SeedData) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, sd := range data {
		errors := make([]ErrorRecord, len(sd.Errors))
		for i, err := range sd.Errors {
			errCopy := err
			if errCopy.ErrorID == "" {
				errCopy.ErrorID = m.generateULID()
			}
			errCopy.DeviceID = sd.DeviceID
			errors[i] = errCopy
			m.errorIndex[errCopy.ErrorID] = &errors[i]
		}
		m.seededErrors[sd.DeviceID] = errors
		if sd.DeviceType != "" {
			m.deviceTypes[sd.DeviceID] = sd.DeviceType
		}
	}
}

// GetErrorsForDevice returns all errors (seeded + generated) for a device.
func (m *FakeErrorManager) GetErrorsForDevice(deviceID int64) []ErrorRecord {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []ErrorRecord
	if seeded, ok := m.seededErrors[deviceID]; ok {
		result = append(result, seeded...)
	}
	if generated, ok := m.generatedErrors[deviceID]; ok {
		result = append(result, generated...)
	}
	return result
}

// GetAllErrors returns all errors across all devices.
func (m *FakeErrorManager) GetAllErrors() []ErrorRecord {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []ErrorRecord
	for _, errors := range m.seededErrors {
		result = append(result, errors...)
	}
	for _, errors := range m.generatedErrors {
		result = append(result, errors...)
	}
	return result
}

// GetErrorByID retrieves a single error by its ID.
func (m *FakeErrorManager) GetErrorByID(errorID string) (*ErrorRecord, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if err, ok := m.errorIndex[errorID]; ok {
		return err, true
	}
	return nil, false
}

// EnsureErrorsExist generates errors for a device if none exist yet (seeded or generated).
func (m *FakeErrorManager) EnsureErrorsExist(deviceID int64, deviceType string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// If device has seeded errors, don't generate more.
	if _, ok := m.seededErrors[deviceID]; ok {
		return
	}

	// If device already has generated errors, don't regenerate.
	if _, ok := m.generatedErrors[deviceID]; ok {
		return
	}

	// Store device type.
	if deviceType != "" {
		m.deviceTypes[deviceID] = deviceType
	}

	// Random probability check.
	if !m.shouldGenerateErrors() {
		m.generatedErrors[deviceID] = []ErrorRecord{}
		return
	}

	// Generate errors.
	errors := m.generateErrorsForDevice(deviceID, deviceType)
	m.generatedErrors[deviceID] = errors

	// Index the errors.
	for i := range errors {
		m.errorIndex[errors[i].ErrorID] = &m.generatedErrors[deviceID][i]
	}
}

// GetDeviceType returns the device type for a device ID.
func (m *FakeErrorManager) GetDeviceType(deviceID int64) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.deviceTypes[deviceID]
}

// SetDeviceType sets the device type for a device ID.
func (m *FakeErrorManager) SetDeviceType(deviceID int64, deviceType string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deviceTypes[deviceID] = deviceType
}

// GetMetadata returns the miner error metadata map.
func (m *FakeErrorManager) GetMetadata() map[errorsv1.MinerError]*MinerErrorMetadata {
	return m.metadata
}

// generateULID generates a new ULID string.
func (m *FakeErrorManager) generateULID() string {
	entropy := ulid.Monotonic(rand.Reader, 0)
	id := ulid.MustNew(ulid.Timestamp(time.Now()), entropy)
	return id.String()
}

// shouldGenerateErrors returns true with errorProbabilityPercent probability.
func (m *FakeErrorManager) shouldGenerateErrors() bool {
	n, _ := rand.Int(rand.Reader, big.NewInt(probabilityMax))
	return n.Int64() < errorProbabilityPercent
}

// generateErrorsForDevice creates random errors for a device.
func (m *FakeErrorManager) generateErrorsForDevice(deviceID int64, _ string) []ErrorRecord {
	var errors []ErrorRecord

	// Select random number of components to affect.
	numComponents := m.randomInt(minComponentsAffected, maxComponentsAffected)
	componentTypes := m.selectRandomComponentTypes(numComponents)

	for _, compType := range componentTypes {
		// Select random instance of this component.
		maxInstances := componentCounts[compType]
		instanceIdx := m.randomInt(0, maxInstances-1)
		componentID := m.formatComponentID(deviceID, compType, instanceIdx)

		// Generate errors for this component.
		numErrors := m.randomInt(minErrorsPerComponent, maxErrorsPerComponent)
		possibleErrors := componentErrorRanges[compType]
		if len(possibleErrors) == 0 {
			continue
		}

		selectedErrors := m.selectRandomErrors(possibleErrors, numErrors)
		for _, errCode := range selectedErrors {
			err := m.createErrorRecord(deviceID, componentID, errCode)
			errors = append(errors, err)
		}
	}

	return errors
}

// selectRandomComponentTypes selects n random component types.
func (m *FakeErrorManager) selectRandomComponentTypes(n int) []errorsv1.ComponentType {
	allTypes := []errorsv1.ComponentType{
		errorsv1.ComponentType_COMPONENT_TYPE_PSU,
		errorsv1.ComponentType_COMPONENT_TYPE_FAN,
		errorsv1.ComponentType_COMPONENT_TYPE_HASH_BOARD,
		errorsv1.ComponentType_COMPONENT_TYPE_CONTROL_BOARD,
		errorsv1.ComponentType_COMPONENT_TYPE_EEPROM,
		errorsv1.ComponentType_COMPONENT_TYPE_IO_MODULE,
	}

	if n > len(allTypes) {
		n = len(allTypes)
	}

	// Shuffle and take first n.
	shuffled := make([]errorsv1.ComponentType, len(allTypes))
	copy(shuffled, allTypes)
	for i := len(shuffled) - 1; i > 0; i-- {
		j := m.randomInt(0, i)
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	}

	return shuffled[:n]
}

// selectRandomErrors selects n random error codes from the list.
func (m *FakeErrorManager) selectRandomErrors(errors []errorsv1.MinerError, n int) []errorsv1.MinerError {
	if n > len(errors) {
		n = len(errors)
	}

	// Shuffle and take first n.
	shuffled := make([]errorsv1.MinerError, len(errors))
	copy(shuffled, errors)
	for i := len(shuffled) - 1; i > 0; i-- {
		j := m.randomInt(0, i)
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	}

	return shuffled[:n]
}

// createErrorRecord creates a new error record with random timestamps.
func (m *FakeErrorManager) createErrorRecord(deviceID int64, componentID string, errCode errorsv1.MinerError) ErrorRecord {
	meta, ok := m.metadata[errCode]
	if !ok {
		meta = &MinerErrorMetadata{
			Code:            errCode,
			Name:            "Unknown Error",
			DefaultSummary:  "Unknown error occurred",
			DefaultSeverity: errorsv1.Severity_SEVERITY_INFO,
			DefaultAction:   "Contact support",
			DefaultImpact:   "Unknown impact",
		}
	}

	now := time.Now()
	firstSeenDaysAgo := m.randomInt(0, maxFirstSeenAgeDays)
	firstSeenHoursAgo := m.randomInt(0, 23)
	firstSeen := now.AddDate(0, 0, -firstSeenDaysAgo).Add(-time.Duration(firstSeenHoursAgo) * time.Hour)

	lastSeenMinutesAgo := m.randomInt(0, maxLastSeenAgeHours*60)
	lastSeen := now.Add(-time.Duration(lastSeenMinutesAgo) * time.Minute)

	// Ensure lastSeen is after firstSeen.
	if lastSeen.Before(firstSeen) {
		lastSeen = firstSeen.Add(time.Duration(m.randomInt(1, 60)) * time.Minute)
	}

	var closedAt *time.Time
	if m.randomInt(0, probabilityMax) < closedErrorProbabilityPercent {
		closed := lastSeen.Add(time.Duration(m.randomInt(1, 30)) * time.Minute)
		closedAt = &closed
	}

	return ErrorRecord{
		ErrorID:           m.generateULID(),
		MinerError:        errCode,
		Summary:           meta.DefaultSummary,
		CauseSummary:      meta.DefaultSummary,
		RecommendedAction: meta.DefaultAction,
		Severity:          meta.DefaultSeverity,
		FirstSeenAt:       firstSeen,
		LastSeenAt:        lastSeen,
		ClosedAt:          closedAt,
		VendorAttributes:  m.generateVendorAttributes(errCode),
		DeviceID:          deviceID,
		ComponentID:       componentID,
		Impact:            meta.DefaultImpact,
	}
}

// formatComponentID creates a component ID in the format "{deviceID}_{type}_{index}".
func (m *FakeErrorManager) formatComponentID(deviceID int64, compType errorsv1.ComponentType, index int) string {
	typeStr := componentTypeToString(compType)
	return fmt.Sprintf("%d_%s_%d", deviceID, typeStr, index)
}

// componentTypeToString returns a short string for the component type.
func componentTypeToString(ct errorsv1.ComponentType) string {
	switch ct {
	case errorsv1.ComponentType_COMPONENT_TYPE_PSU:
		return "psu"
	case errorsv1.ComponentType_COMPONENT_TYPE_FAN:
		return "fan"
	case errorsv1.ComponentType_COMPONENT_TYPE_HASH_BOARD:
		return "hashboard"
	case errorsv1.ComponentType_COMPONENT_TYPE_CONTROL_BOARD:
		return "controlboard"
	case errorsv1.ComponentType_COMPONENT_TYPE_EEPROM:
		return "eeprom"
	case errorsv1.ComponentType_COMPONENT_TYPE_IO_MODULE:
		return "iomodule"
	case errorsv1.ComponentType_COMPONENT_TYPE_UNSPECIFIED:
		return "unknown"
	default:
		return "unknown"
	}
}

// generateVendorAttributes creates vendor-specific attributes for an error.
func (m *FakeErrorManager) generateVendorAttributes(errCode errorsv1.MinerError) map[string]string {
	attrs := make(map[string]string)

	// Add some realistic vendor attributes based on error type.
	switch {
	case errCode >= errorsv1.MinerError_MINER_ERROR_PSU_NOT_PRESENT &&
		errCode <= errorsv1.MinerError_MINER_ERROR_PSU_UNDER_TEMPERATURE:
		attrs["psu_model"] = "APW12"
		if errCode == errorsv1.MinerError_MINER_ERROR_PSU_OVER_TEMPERATURE {
			attrs["psu_temp_c"] = fmt.Sprintf("%d", 85+m.randomInt(0, 10))
		}
	case errCode >= errorsv1.MinerError_MINER_ERROR_FAN_FAILED &&
		errCode <= errorsv1.MinerError_MINER_ERROR_DEVICE_UNDER_TEMPERATURE:
		if errCode == errorsv1.MinerError_MINER_ERROR_FAN_SPEED_DEVIATION {
			attrs["target_rpm"] = fmt.Sprintf("%d", 6000)
			attrs["actual_rpm"] = fmt.Sprintf("%d", 4000+m.randomInt(0, 1000))
		}
	case errCode >= errorsv1.MinerError_MINER_ERROR_HASHBOARD_NOT_PRESENT &&
		errCode <= errorsv1.MinerError_MINER_ERROR_HASHBOARD_ASIC_UNDER_TEMPERATURE:
		attrs["board_serial"] = fmt.Sprintf("HB%08d", m.randomInt(10000000, 99999999))
		if errCode == errorsv1.MinerError_MINER_ERROR_HASHBOARD_MISSING_CHIPS {
			attrs["expected_chips"] = "126"
			attrs["detected_chips"] = fmt.Sprintf("%d", 126-m.randomInt(1, 10))
		}
	}

	return attrs
}

// randomInt returns a random integer in range [minVal, maxVal] inclusive.
func (m *FakeErrorManager) randomInt(minVal, maxVal int) int {
	if maxVal <= minVal {
		return minVal
	}
	n, _ := rand.Int(rand.Reader, big.NewInt(int64(maxVal-minVal+1)))
	return minVal + int(n.Int64())
}

// CalculateStatus computes the aggregate status from a list of errors.
func CalculateStatus(errors []ErrorRecord) errorsv1.Status {
	if len(errors) == 0 {
		return errorsv1.Status_STATUS_OK
	}

	hasCritical := false
	hasMajor := false
	hasMinorOrInfo := false

	for _, err := range errors {
		if err.ClosedAt != nil {
			continue // Skip closed errors for status calculation.
		}
		switch err.Severity {
		case errorsv1.Severity_SEVERITY_CRITICAL:
			return errorsv1.Status_STATUS_ERROR // Short-circuit on critical.
		case errorsv1.Severity_SEVERITY_MAJOR:
			hasMajor = true
		case errorsv1.Severity_SEVERITY_MINOR, errorsv1.Severity_SEVERITY_INFO:
			hasMinorOrInfo = true
		case errorsv1.Severity_SEVERITY_UNSPECIFIED:
			// Ignore unspecified severity.
		}
	}

	if hasCritical {
		return errorsv1.Status_STATUS_ERROR
	}
	if hasMajor {
		return errorsv1.Status_STATUS_WARNING
	}
	if hasMinorOrInfo {
		return errorsv1.Status_STATUS_WARNING
	}
	return errorsv1.Status_STATUS_OK
}

// GenerateSummary creates a summary for a list of errors.
func GenerateSummary(errors []ErrorRecord) *errorsv1.Summary {
	if len(errors) == 0 {
		return &errorsv1.Summary{
			Title:     "No errors",
			Details:   "All systems operating normally.",
			Condensed: "OK",
		}
	}

	// Count by severity (only open errors).
	criticalCount := 0
	majorCount := 0
	minorCount := 0
	infoCount := 0

	for _, err := range errors {
		if err.ClosedAt != nil {
			continue
		}
		switch err.Severity {
		case errorsv1.Severity_SEVERITY_CRITICAL:
			criticalCount++
		case errorsv1.Severity_SEVERITY_MAJOR:
			majorCount++
		case errorsv1.Severity_SEVERITY_MINOR:
			minorCount++
		case errorsv1.Severity_SEVERITY_INFO:
			infoCount++
		case errorsv1.Severity_SEVERITY_UNSPECIFIED:
			// Ignore unspecified severity in counts.
		}
	}

	totalOpen := criticalCount + majorCount + minorCount + infoCount

	// Build title.
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

	// Build details.
	var details string
	if totalOpen > 0 {
		parts := []string{}
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
		details = fmt.Sprintf("%d active errors: %s", totalOpen, joinStrings(parts, ", "))
	} else {
		details = "All errors have been resolved."
	}

	// Build condensed.
	var condensed string
	if criticalCount > 0 {
		condensed = fmt.Sprintf("%d CRIT", criticalCount)
	} else if majorCount > 0 {
		condensed = fmt.Sprintf("%d MAJ", majorCount)
	} else if minorCount > 0 {
		condensed = fmt.Sprintf("%d MIN", minorCount)
	} else if infoCount > 0 {
		condensed = fmt.Sprintf("%d INFO", infoCount)
	} else {
		condensed = "OK"
	}

	return &errorsv1.Summary{
		Title:     title,
		Details:   details,
		Condensed: condensed,
	}
}

// CountsBySeverity returns error counts grouped by severity.
func CountsBySeverity(errors []ErrorRecord) map[string]int32 {
	counts := make(map[string]int32)
	for _, err := range errors {
		key := err.Severity.String()
		counts[key]++
	}
	return counts
}

// SortErrors sorts errors by severity (descending), then last_seen (descending), then error_id.
func SortErrors(errors []ErrorRecord) {
	sort.Slice(errors, func(i, j int) bool {
		// Sort by severity (descending - CRITICAL first).
		if errors[i].Severity != errors[j].Severity {
			return errors[i].Severity < errors[j].Severity // Lower enum value = higher severity.
		}
		// Then by last_seen (descending - most recent first).
		if !errors[i].LastSeenAt.Equal(errors[j].LastSeenAt) {
			return errors[i].LastSeenAt.After(errors[j].LastSeenAt)
		}
		// Finally by error_id (descending).
		return errors[i].ErrorID > errors[j].ErrorID
	})
}

// joinStrings joins strings with a separator.
func joinStrings(parts []string, sep string) string {
	result := ""
	for i, p := range parts {
		if i > 0 {
			result += sep
		}
		result += p
	}
	return result
}

// ParseComponentID extracts device ID, component type, and index from a component ID.
func ParseComponentID(componentID string) (deviceID int64, compType errorsv1.ComponentType, index int, ok bool) {
	var typeStr string
	_, err := fmt.Sscanf(componentID, "%d_%s_%d", &deviceID, &typeStr, &index)
	if err != nil {
		return 0, errorsv1.ComponentType_COMPONENT_TYPE_UNSPECIFIED, 0, false
	}

	compType = stringToComponentType(typeStr)
	return deviceID, compType, index, true
}

// stringToComponentType converts a string back to ComponentType.
func stringToComponentType(s string) errorsv1.ComponentType {
	switch s {
	case "psu":
		return errorsv1.ComponentType_COMPONENT_TYPE_PSU
	case "fan":
		return errorsv1.ComponentType_COMPONENT_TYPE_FAN
	case "hashboard":
		return errorsv1.ComponentType_COMPONENT_TYPE_HASH_BOARD
	case "controlboard":
		return errorsv1.ComponentType_COMPONENT_TYPE_CONTROL_BOARD
	case "eeprom":
		return errorsv1.ComponentType_COMPONENT_TYPE_EEPROM
	case "iomodule":
		return errorsv1.ComponentType_COMPONENT_TYPE_IO_MODULE
	default:
		return errorsv1.ComponentType_COMPONENT_TYPE_UNSPECIFIED
	}
}
