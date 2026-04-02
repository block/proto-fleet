package command

import (
	"context"
	"database/sql"
	"math"

	capabilitiespb "github.com/block/proto-fleet/server/generated/grpc/capabilities/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

// CapabilitiesProvider retrieves miner capabilities for devices.
// This interface is implemented by the plugins.Service.
type CapabilitiesProvider interface {
	GetMinerCapabilitiesForDevice(ctx context.Context, device *pairingpb.Device) *capabilitiespb.MinerCapabilities
}

// CapabilityChecker validates command support for devices.
type CapabilityChecker struct {
	conn                 *sql.DB
	capabilitiesProvider CapabilitiesProvider
}

// NewCapabilityChecker creates a new capability checker.
func NewCapabilityChecker(conn *sql.DB, provider CapabilitiesProvider) *CapabilityChecker {
	return &CapabilityChecker{
		conn:                 conn,
		capabilitiesProvider: provider,
	}
}

// deviceInfo holds device information needed for capability checking.
type deviceInfo struct {
	DeviceIdentifier string
	Manufacturer     string
	Model            string
	FirmwareVersion  string
	DriverName       string
}

// newDeviceInfo creates a deviceInfo from raw database fields.
func newDeviceInfo(deviceIdentifier string, manufacturer, model, firmwareVersion sql.NullString, driverName string) deviceInfo {
	return deviceInfo{
		DeviceIdentifier: deviceIdentifier,
		Manufacturer:     stringOrEmpty(manufacturer),
		Model:            stringOrEmpty(model),
		FirmwareVersion:  stringOrEmpty(firmwareVersion),
		DriverName:       driverName,
	}
}

// CheckCapabilities validates command support for all devices in the selector.
// Returns a response with supported/unsupported counts, groups, and identifiers.
func (c *CapabilityChecker) CheckCapabilities(
	ctx context.Context,
	selector *pb.DeviceSelector,
	cmdType pb.CommandType,
	orgID int64,
) (*pb.CheckCommandCapabilitiesResponse, error) {
	if selector == nil {
		return nil, fleeterror.NewInvalidArgumentError("device selector is required")
	}

	requiredCaps := GetRequiredCapabilities(cmdType)
	if len(requiredCaps) == 0 {
		return nil, fleeterror.NewInvalidArgumentErrorf("unknown or unsupported command type: %v", cmdType)
	}

	devices, err := c.getDeviceInfo(ctx, selector, orgID)
	if err != nil {
		return nil, err
	}

	return c.checkDeviceCapabilities(ctx, devices, requiredCaps), nil
}

// getDeviceInfo retrieves device information based on the selector.
func (c *CapabilityChecker) getDeviceInfo(
	ctx context.Context,
	selector *pb.DeviceSelector,
	orgID int64,
) ([]deviceInfo, error) {
	switch x := selector.SelectionType.(type) {
	case *pb.DeviceSelector_AllDevices:
		return c.getAllDeviceInfo(ctx, orgID)
	case *pb.DeviceSelector_IncludeDevices:
		return c.getDeviceInfoByDeviceIdentifiers(ctx, x.IncludeDevices.DeviceIdentifiers, orgID)
	default:
		return nil, fleeterror.NewInternalErrorf("unknown device selector type: %T", x)
	}
}

// getAllDeviceInfo retrieves info for all paired devices in the organization.
func (c *CapabilityChecker) getAllDeviceInfo(ctx context.Context, orgID int64) ([]deviceInfo, error) {
	return db.WithTransaction(ctx, c.conn, func(q *sqlc.Queries) ([]deviceInfo, error) {
		rows, err := q.GetAllDeviceInfoForCapabilityCheck(ctx, orgID)
		if err != nil {
			return nil, fleeterror.NewInternalErrorf("error getting device info: %v", err)
		}

		devices := make([]deviceInfo, len(rows))
		for i, row := range rows {
			devices[i] = newDeviceInfo(row.DeviceIdentifier, row.Manufacturer, row.Model, row.FirmwareVersion, row.DriverName)
		}
		return devices, nil
	})
}

// getDeviceInfoByDeviceIdentifiers retrieves info for specific device identifiers.
func (c *CapabilityChecker) getDeviceInfoByDeviceIdentifiers(
	ctx context.Context,
	deviceIdentifiers []string,
	orgID int64,
) ([]deviceInfo, error) {
	if len(deviceIdentifiers) == 0 {
		return []deviceInfo{}, nil
	}

	return db.WithTransaction(ctx, c.conn, func(q *sqlc.Queries) ([]deviceInfo, error) {
		rows, err := q.GetDeviceInfoForCapabilityCheck(ctx, sqlc.GetDeviceInfoForCapabilityCheckParams{
			DeviceIdentifiers: deviceIdentifiers,
			OrgID:             orgID,
		})
		if err != nil {
			return nil, fleeterror.NewInternalErrorf("error getting device info: %v", err)
		}

		devices := make([]deviceInfo, len(rows))
		for i, row := range rows {
			devices[i] = newDeviceInfo(row.DeviceIdentifier, row.Manufacturer, row.Model, row.FirmwareVersion, row.DriverName)
		}
		return devices, nil
	})
}

// checkDeviceCapabilities checks capabilities for each device and groups results.
func (c *CapabilityChecker) checkDeviceCapabilities(
	ctx context.Context,
	devices []deviceInfo,
	requiredCaps []string,
) *pb.CheckCommandCapabilitiesResponse {
	var supportedIdentifiers []string
	unsupportedByGroup := make(map[string]*pb.UnsupportedMinerGroup)

	for _, device := range devices {
		if c.deviceSupportsCommand(ctx, device, requiredCaps) {
			supportedIdentifiers = append(supportedIdentifiers, device.DeviceIdentifier)
		} else {
			groupKey := device.Model + "|" + device.FirmwareVersion
			if group, exists := unsupportedByGroup[groupKey]; exists {
				group.Count++
			} else {
				model := device.Model
				if model == "" {
					model = "Unknown"
				}
				firmware := device.FirmwareVersion
				if firmware == "" {
					firmware = "Unknown"
				}
				unsupportedByGroup[groupKey] = &pb.UnsupportedMinerGroup{
					FirmwareVersion: firmware,
					Model:           model,
					Count:           1,
				}
			}
		}
	}

	unsupportedGroups := make([]*pb.UnsupportedMinerGroup, 0, len(unsupportedByGroup))
	for _, group := range unsupportedByGroup {
		unsupportedGroups = append(unsupportedGroups, group)
	}

	supportedCount := safeIntToInt32(len(supportedIdentifiers))
	unsupportedCount := safeIntToInt32(len(devices)) - supportedCount
	totalCount := safeIntToInt32(len(devices))

	return &pb.CheckCommandCapabilitiesResponse{
		SupportedCount:             supportedCount,
		UnsupportedCount:           unsupportedCount,
		TotalCount:                 totalCount,
		AllSupported:               unsupportedCount == 0,
		NoneSupported:              supportedCount == 0 && totalCount > 0,
		UnsupportedGroups:          unsupportedGroups,
		SupportedDeviceIdentifiers: supportedIdentifiers,
	}
}

// deviceSupportsCommand checks if a device supports the command.
// Returns true if ANY of the required capabilities is supported.
func (c *CapabilityChecker) deviceSupportsCommand(
	ctx context.Context,
	device deviceInfo,
	requiredCaps []string,
) bool {
	capabilities := c.capabilitiesProvider.GetMinerCapabilitiesForDevice(ctx, &pairingpb.Device{
		Manufacturer: device.Manufacturer,
		Model:        device.Model,
		DriverName:   device.DriverName,
	})

	if capabilities == nil || capabilities.Commands == nil {
		return false
	}

	return hasAnyCapability(capabilities.Commands, capabilities.Firmware, requiredCaps)
}

// hasAnyCapability checks if the device capabilities include any of the required capabilities.
func hasAnyCapability(commands *capabilitiespb.CommandCapabilities, firmware *capabilitiespb.FirmwareCapabilities, requiredCaps []string) bool {
	for _, cap := range requiredCaps {
		switch cap {
		case sdk.CapabilityReboot:
			if commands.RebootSupported {
				return true
			}
		case sdk.CapabilityMiningStart:
			if commands.MiningStartSupported {
				return true
			}
		case sdk.CapabilityMiningStop:
			if commands.MiningStopSupported {
				return true
			}
		case sdk.CapabilityLEDBlink:
			if commands.LedBlinkSupported {
				return true
			}
		case sdk.CapabilityCoolingModeAir:
			if commands.AirCoolingSupported {
				return true
			}
		case sdk.CapabilityCoolingModeImmerse:
			if commands.ImmersionCoolingSupported {
				return true
			}
		case sdk.CapabilityPoolConfig:
			if commands.PoolSwitchingSupported {
				return true
			}
		case sdk.CapabilityLogsDownload:
			if commands.LogsDownloadSupported {
				return true
			}
		case sdk.CapabilityManualUpload:
			if firmware != nil && firmware.ManualUploadSupported {
				return true
			}
		case sdk.CapabilityFactoryReset:
			if commands.FactoryResetSupported {
				return true
			}
		case sdk.CapabilityPowerModeEfficiency:
			if commands.PowerModeEfficiencySupported {
				return true
			}
		case sdk.CapabilityUpdateMinerPassword:
			if commands.UpdateMinerPasswordSupported {
				return true
			}
		}
	}
	return false
}

// stringOrEmpty returns the string value or empty string if the sql.NullString is not valid.
func stringOrEmpty(ns sql.NullString) string {
	if ns.Valid {
		return ns.String
	}
	return ""
}

// safeIntToInt32 safely converts int to int32.
func safeIntToInt32(i int) int32 {
	if i > math.MaxInt32 || i < math.MinInt32 {
		return 0
	}
	return int32(i)
}
