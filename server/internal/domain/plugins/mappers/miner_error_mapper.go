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
		ComponentType:     models.ComponentTypeUnspecified, // Not defined in SDK yet
		Impact:            sdkError.Impact,
		Summary:           sdkError.Summary,
		VendorCode:        sdkError.VendorAttributes["vendor_code"],
		Firmware:          sdkError.VendorAttributes["firmware"],
	}
}
