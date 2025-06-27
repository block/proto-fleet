package pairing

import (
	"context"
	"database/sql"

	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/models"
	"github.com/btc-mining/proto-fleet/server/internal/domain/minerdiscovery"

	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/btc-mining/proto-fleet/server/generated/sqlc"
)

// pairing statuses
const (
	StatusPaired   = "PAIRED"
	StatusUnpaired = "UNPAIRED"
)

type Config struct {
	SecretKey string `help:"Secret key for signing the pairing tokens" env:"SECRET_KEY" required:""`
}

type Pairer interface {
	// PairDevice handles the entire pairing process including saving the device to the database
	PairDevice(ctx context.Context, device *minerdiscovery.DiscoveredDevice, credentials *pb.Credentials) error
	GetMinerType() models.Type
}

// SaveDiscoveredDevice saves a discovered device to the database and handles IP assignments
func SaveDiscoveredDevice(ctx context.Context, q *sqlc.Queries, device *minerdiscovery.DiscoveredDevice) (int64, error) {
	result, err := q.UpsertDevice(ctx, sqlc.UpsertDeviceParams{
		OrgID:            device.OrgID,
		DeviceIdentifier: device.DeviceIdentifier,
		MacAddress:       device.MacAddress,
		SerialNumber:     sql.NullString{String: device.SerialNumber, Valid: len(device.SerialNumber) > 0},
		Model:            sql.NullString{String: device.Model, Valid: len(device.Model) > 0},
		Manufacturer:     sql.NullString{String: device.Manufacturer, Valid: len(device.Manufacturer) > 0},
		Type:             device.Type,
		IsActive:         sql.NullBool{Bool: true, Valid: true},
	})
	if err != nil {
		return 0, fleeterror.NewInternalErrorf("failed to upsert device: %v", err)
	}

	deviceID, err := result.LastInsertId()
	if err != nil {
		return 0, fleeterror.NewInternalErrorf("failed to get device ID: %v", err)
	}

	// Handle IP assignment
	currentIPAssignment, err := q.GetActiveDeviceIPAssignmentByDeviceID(ctx, deviceID)
	if err != nil && err != sql.ErrNoRows {
		return 0, fleeterror.NewInternalErrorf("failed to query active device IP assignment: %v", err)
	} else if err != sql.ErrNoRows && currentIPAssignment.IpAddress == device.IpAddress && currentIPAssignment.Port == device.Port {
		// Device IP assignment already exists, continue with pairing
		return deviceID, nil
	}

	// Create and activate new IP assignment
	err = q.CreateInactiveDeviceIPAssignment(ctx, sqlc.CreateInactiveDeviceIPAssignmentParams{
		DeviceID:  deviceID,
		IpAddress: device.IpAddress,
		Port:      device.Port,
	})
	if err != nil {
		return 0, fleeterror.NewInternalErrorf("failed to create IP assignment: %v", err)
	}

	err = q.ActivateNewIPAssignment(ctx, sqlc.ActivateNewIPAssignmentParams{
		DeviceID:  deviceID,
		IpAddress: device.IpAddress,
		Port:      device.Port,
	})
	if err != nil {
		return 0, fleeterror.NewInternalErrorf("failed to activate new IP assignment: %v", err)
	}

	return deviceID, nil
}
