package mappers

import (
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/models"
	sdk "github.com/btc-mining/proto-fleet/server/sdk/v1"
)

// FleetTypeToSDKType converts Fleet's miner type to SDK DeviceType.
// This mapping ensures correct enum alignment between Fleet and SDK types.
func FleetTypeToSDKType(t models.Type) sdk.DeviceType {
	switch t {
	case models.TypeAntminer:
		return sdk.DeviceTypeASIC
	case models.TypeProto:
		return sdk.DeviceTypeASIC
	case models.TypeWhatsminer:
		return sdk.DeviceTypeASIC
	case models.TypeAvalon:
		return sdk.DeviceTypeASIC
	case models.TypeVirtual:
		return sdk.DeviceTypeASIC
	case models.TypeUnknown:
		return sdk.DeviceTypeUnspecified
	default:
		return sdk.DeviceTypeUnspecified
	}
}
