package mappers

import (
	"github.com/btc-mining/proto-fleet/server/internal/domain/diagnostics/models"
	sdkv1 "github.com/btc-mining/proto-fleet/server/sdk/v1"
	sdkv1models "github.com/btc-mining/proto-fleet/server/sdk/v1/errors"
)

const (
	// minValidEnumValue represents the minimum valid value for enum types
	minValidEnumValue = 0
)

func SDKMinerErrorToFleetMinerError(errCode sdkv1models.MinerError) models.MinerError {
	if errCode < minValidEnumValue {
		return models.MinerErrorUnspecified
	}
	// #nosec G115 -- Validated non-negative above; protobuf enums are never negative in practice
	return models.MinerError(errCode)
}

func SDKSeverityToFleetSeverity(sev sdkv1models.Severity) models.Severity {
	if sev < minValidEnumValue {
		return models.SeverityUnspecified
	}
	// #nosec G115 -- Validated non-negative above; protobuf enums are never negative in practice
	return models.Severity(sev)
}

// SDKComponentTypeToFleetComponentType converts SDK ComponentType to fleet domain ComponentType.
// SDK and Fleet have different enum value assignments, so explicit mapping is required.
func SDKComponentTypeToFleetComponentType(sdkType sdkv1models.ComponentType) models.ComponentType {
	if sdkType < minValidEnumValue {
		return models.ComponentTypeUnspecified
	}

	switch sdkType {
	case sdkv1models.ComponentTypeUnspecified:
		return models.ComponentTypeUnspecified
	case sdkv1models.ComponentTypePSU:
		return models.ComponentTypePSU
	case sdkv1models.ComponentTypeHashBoard:
		return models.ComponentTypeHashBoards
	case sdkv1models.ComponentTypeFan:
		return models.ComponentTypeFans
	case sdkv1models.ComponentTypeControlBoard:
		return models.ComponentTypeControlBoard
	case sdkv1models.ComponentTypeEEPROM:
		// No Fleet equivalent - map to Unspecified
		return models.ComponentTypeUnspecified
	case sdkv1models.ComponentTypeIOModule:
		// No Fleet equivalent - map to Unspecified
		return models.ComponentTypeUnspecified
	default:
		// Unknown component type - map to Unspecified
		return models.ComponentTypeUnspecified
	}
}

// SDKDeviceErrorsToFleetDeviceErrors converts SDK DeviceErrors (plural) to fleet domain DeviceErrors.
func SDKDeviceErrorsToFleetDeviceErrors(sdkErrors sdkv1.DeviceErrors) models.DeviceErrors {
	errors := make([]models.ErrorMessage, len(sdkErrors.Errors))
	for i, sdkErr := range sdkErrors.Errors {
		errors[i] = SDKDeviceErrorToFleetErrorMessage(sdkErr)
	}
	return models.DeviceErrors{
		DeviceID: sdkErrors.DeviceID,
		Errors:   errors,
	}
}

// SDKDeviceErrorToFleetErrorMessage converts a single SDK DeviceError to fleet domain ErrorMessage.
func SDKDeviceErrorToFleetErrorMessage(sdkError sdkv1.DeviceError) models.ErrorMessage {
	return models.ErrorMessage{
		ErrorID:           "", // Assigned by Store on insert (ULID)
		MinerError:        SDKMinerErrorToFleetMinerError(sdkError.MinerError),
		CauseSummary:      sdkError.CauseSummary,
		RecommendedAction: sdkError.RecommendedAction,
		Severity:          SDKSeverityToFleetSeverity(sdkError.Severity),
		FirstSeenAt:       sdkError.FirstSeenAt,
		LastSeenAt:        sdkError.LastSeenAt,
		ClosedAt:          sdkError.ClosedAt,
		VendorAttributes:  sdkError.VendorAttributes,
		DeviceID:          sdkError.DeviceID,
		ComponentID:       sdkError.ComponentID,
		ComponentType:     SDKComponentTypeToFleetComponentType(sdkError.ComponentType),
		Impact:            sdkError.Impact,
		Summary:           sdkError.Summary,
		VendorCode:        sdkError.VendorAttributes["vendor_code"],
		Firmware:          sdkError.VendorAttributes["firmware"],
	}
}
