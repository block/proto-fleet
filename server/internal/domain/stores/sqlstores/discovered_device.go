package sqlstores

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"

	pb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/minerdiscovery"
	discoverymodels "github.com/block/proto-fleet/server/internal/domain/minerdiscovery/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

var _ interfaces.DiscoveredDeviceStore = &SQLDiscoveredDeviceStore{}

type SQLDiscoveredDeviceStore struct {
	SQLConnectionManager
}

func NewSQLDiscoveredDeviceStore(conn *sql.DB) *SQLDiscoveredDeviceStore {
	return &SQLDiscoveredDeviceStore{
		SQLConnectionManager: NewSQLConnectionManager(conn),
	}
}

func (s *SQLDiscoveredDeviceStore) getQueries(ctx context.Context) *sqlc.Queries {
	return s.GetQueries(ctx)
}

// encodeCursor encodes a device ID to a base64 string
func (s *SQLDiscoveredDeviceStore) encodeCursor(id int64) string {
	if id == 0 {
		return ""
	}
	raw := fmt.Sprintf("%d", id)
	return base64.StdEncoding.EncodeToString([]byte(raw))
}

// decodeCursor decodes a base64 string to a device ID
func (s *SQLDiscoveredDeviceStore) decodeCursor(encoded string) (int64, error) {
	if encoded == "" {
		return 0, nil
	}

	b, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return 0, fleeterror.NewInvalidArgumentErrorf("invalid cursor encoding: %v", err)
	}

	id, err := strconv.ParseInt(string(b), 10, 64)
	if err != nil {
		return 0, fleeterror.NewInvalidArgumentErrorf("invalid cursor value: %v", err)
	}

	return id, nil
}

// Save stores or updates a discovered device and returns the saved device
func (s *SQLDiscoveredDeviceStore) Save(ctx context.Context, doi discoverymodels.DeviceOrgIdentifier, device *discoverymodels.DiscoveredDevice) (*discoverymodels.DiscoveredDevice, error) {
	insertedID, err := s.getQueries(ctx).UpsertDiscoveredDevice(ctx, sqlc.UpsertDiscoveredDeviceParams{
		OrgID:            doi.OrgID,
		DeviceIdentifier: doi.DeviceIdentifier,
		Model:            sql.NullString{String: device.Model, Valid: len(device.Model) > 0},
		Manufacturer:     sql.NullString{String: device.Manufacturer, Valid: len(device.Manufacturer) > 0},
		FirmwareVersion:  sql.NullString{String: device.FirmwareVersion, Valid: len(device.FirmwareVersion) > 0},
		IpAddress:        device.IpAddress,
		Port:             device.Port,
		UrlScheme:        device.UrlScheme,
		IsActive:         device.IsActive,
		DriverName:       device.Device.DriverName,
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to upsert discovered device: %v", err)
	}

	// Fetch the complete record to get timestamps and other fields
	dbDevice, err := s.getQueries(ctx).GetDiscoveredDeviceByID(ctx, sqlc.GetDiscoveredDeviceByIDParams{
		ID:    insertedID,
		OrgID: doi.OrgID,
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to fetch discovered device after upsert: %v", err)
	}

	return toDiscoveredDevice(dbDevice), nil
}

// GetDevice retrieves a discovered device by its organization and device identifier
func (s *SQLDiscoveredDeviceStore) GetDevice(ctx context.Context, doi discoverymodels.DeviceOrgIdentifier) (*discoverymodels.DiscoveredDevice, error) {
	dbDevice, err := s.getQueries(ctx).GetDiscoveredDeviceByDeviceIdentifier(ctx, sqlc.GetDiscoveredDeviceByDeviceIdentifierParams{
		DeviceIdentifier: doi.DeviceIdentifier,
		OrgID:            doi.OrgID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, minerdiscovery.MinerNotFoundFleetError
		}
		return nil, fleeterror.NewInternalErrorf("failed to query discovered device: %v", err)
	}

	return toDiscoveredDevice(dbDevice), nil
}

// GetByIPAndPort retrieves a discovered device by its IP address and port for a given organization
func (s *SQLDiscoveredDeviceStore) GetByIPAndPort(ctx context.Context, orgID int64, ipAddress string, port string) (*discoverymodels.DiscoveredDevice, error) {
	dbDevice, err := s.getQueries(ctx).GetDiscoveredDeviceByIPAndPort(ctx, sqlc.GetDiscoveredDeviceByIPAndPortParams{
		OrgID:     orgID,
		IpAddress: ipAddress,
		Port:      port,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, minerdiscovery.MinerNotFoundFleetError
		}
		return nil, fleeterror.NewInternalErrorf("failed to query discovered device by IP and port: %v", err)
	}

	return toDiscoveredDevice(dbDevice), nil
}

// GetDatabaseID retrieves the database ID (primary key) for a discovered device
func (s *SQLDiscoveredDeviceStore) GetDatabaseID(ctx context.Context, doi discoverymodels.DeviceOrgIdentifier) (int64, error) {
	dbDevice, err := s.getQueries(ctx).GetDiscoveredDeviceByDeviceIdentifier(ctx, sqlc.GetDiscoveredDeviceByDeviceIdentifierParams{
		DeviceIdentifier: doi.DeviceIdentifier,
		OrgID:            doi.OrgID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, minerdiscovery.MinerNotFoundFleetError
		}
		return 0, fleeterror.NewInternalErrorf("failed to query discovered device: %v", err)
	}

	return dbDevice.ID, nil
}

// GetActiveUnpairedDevices retrieves active discovered devices that haven't been paired yet
func (s *SQLDiscoveredDeviceStore) GetActiveUnpairedDevices(ctx context.Context, orgID int64, cursor string, limit int32) ([]*discoverymodels.DiscoveredDevice, string, error) {
	cursorID, err := s.decodeCursor(cursor)
	if err != nil {
		return nil, "", err
	}

	// Query with limit + 1 to check if there are more pages
	dbDevices, err := s.getQueries(ctx).GetActiveUnpairedDiscoveredDevices(ctx, sqlc.GetActiveUnpairedDiscoveredDevicesParams{
		OrgID:    orgID,
		CursorID: sql.NullInt64{Int64: cursorID, Valid: cursorID > 0},
		Limit:    limit + 1, // request one extra to determine if there are more pages
	})
	if err != nil {
		return nil, "", fleeterror.NewInternalErrorf("failed to query active unpaired devices: %v", err)
	}

	// Check if there are more pages
	hasMore := int32(len(dbDevices)) > limit //nolint:gosec
	if hasMore {
		dbDevices = dbDevices[:limit]
	}

	// Convert to domain models
	devices := make([]*discoverymodels.DiscoveredDevice, len(dbDevices))
	for i, dbDevice := range dbDevices {
		devices[i] = toDiscoveredDeviceFromRow(dbDevice)
	}

	// Generate next cursor if there are more pages
	var nextCursor string
	if hasMore && len(dbDevices) > 0 {
		lastDevice := dbDevices[len(dbDevices)-1]
		nextCursor = s.encodeCursor(lastDevice.ID)
	}

	return devices, nextCursor, nil
}

// toDiscoveredDeviceFromRow converts a GetActiveUnpairedDiscoveredDevicesRow to a domain DiscoveredDevice
func toDiscoveredDeviceFromRow(dbDevice sqlc.GetActiveUnpairedDiscoveredDevicesRow) *discoverymodels.DiscoveredDevice {
	return &discoverymodels.DiscoveredDevice{
		Device: pb.Device{
			DeviceIdentifier: dbDevice.DeviceIdentifier,
			Model:            dbDevice.Model.String,
			Manufacturer:     dbDevice.Manufacturer.String,
			FirmwareVersion:  dbDevice.FirmwareVersion.String,
			IpAddress:        dbDevice.IpAddress,
			Port:             dbDevice.Port,
			UrlScheme:        dbDevice.UrlScheme,
			DriverName:       dbDevice.DriverName,
		},
		IsActive:        dbDevice.IsActive,
		OrgID:           dbDevice.OrgID,
		FirstDiscovered: dbDevice.FirstDiscovered.Time,
		LastSeen:        dbDevice.LastSeen.Time,
	}
}

// CountActiveUnpairedDevices returns the total count of active unpaired devices for an organization
func (s *SQLDiscoveredDeviceStore) CountActiveUnpairedDevices(ctx context.Context, orgID int64) (int64, error) {
	count, err := s.getQueries(ctx).CountActiveUnpairedDiscoveredDevices(ctx, orgID)
	if err != nil {
		return 0, fleeterror.NewInternalErrorf("failed to count active unpaired devices: %v", err)
	}

	return count, nil
}

// SoftDelete soft-deletes a discovered device record
func (s *SQLDiscoveredDeviceStore) SoftDelete(ctx context.Context, doi discoverymodels.DeviceOrgIdentifier) error {
	err := s.getQueries(ctx).SoftDeleteDiscoveredDeviceByIdentifier(ctx, sqlc.SoftDeleteDiscoveredDeviceByIdentifierParams{
		DeviceIdentifier: doi.DeviceIdentifier,
		OrgID:            doi.OrgID,
	})
	if err != nil {
		return fleeterror.NewInternalErrorf("failed to soft-delete discovered device %s: %v", doi.DeviceIdentifier, err)
	}
	return nil
}

// toDiscoveredDevice converts a sqlc DiscoveredDevice to a domain DiscoveredDevice
func toDiscoveredDevice(dbDevice sqlc.DiscoveredDevice) *discoverymodels.DiscoveredDevice {
	return &discoverymodels.DiscoveredDevice{
		Device: pb.Device{
			DeviceIdentifier: dbDevice.DeviceIdentifier,
			Model:            dbDevice.Model.String,
			Manufacturer:     dbDevice.Manufacturer.String,
			FirmwareVersion:  dbDevice.FirmwareVersion.String,
			IpAddress:        dbDevice.IpAddress,
			Port:             dbDevice.Port,
			UrlScheme:        dbDevice.UrlScheme,
			DriverName:       dbDevice.DriverName,
		},
		IsActive:        dbDevice.IsActive,
		OrgID:           dbDevice.OrgID,
		FirstDiscovered: dbDevice.FirstDiscovered.Time,
		LastSeen:        dbDevice.LastSeen.Time,
	}
}
