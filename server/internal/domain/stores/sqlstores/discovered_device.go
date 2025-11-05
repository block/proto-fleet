package sqlstores

import (
	"context"
	"database/sql"

	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/btc-mining/proto-fleet/server/generated/sqlc"
	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"
	"github.com/btc-mining/proto-fleet/server/internal/domain/minerdiscovery"
	discoverymodels "github.com/btc-mining/proto-fleet/server/internal/domain/minerdiscovery/models"
	"github.com/btc-mining/proto-fleet/server/internal/domain/stores/interfaces"
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

// Save stores or updates a discovered device and returns the saved device
func (s *SQLDiscoveredDeviceStore) Save(ctx context.Context, doi discoverymodels.DeviceOrgIdentifier, device *discoverymodels.DiscoveredDevice) (*discoverymodels.DiscoveredDevice, error) {
	result, err := s.getQueries(ctx).UpsertDiscoveredDevice(ctx, sqlc.UpsertDiscoveredDeviceParams{
		OrgID:            doi.OrgID,
		DeviceIdentifier: doi.DeviceIdentifier,
		Model:            sql.NullString{String: device.Model, Valid: len(device.Model) > 0},
		Manufacturer:     sql.NullString{String: device.Manufacturer, Valid: len(device.Manufacturer) > 0},
		Type:             device.Type,
		IpAddress:        device.IpAddress,
		Port:             device.Port,
		UrlScheme:        device.UrlScheme,
		IsActive:         device.IsActive,
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to upsert discovered device: %v", err)
	}

	// Get the ID of the inserted/updated row
	id, err := result.LastInsertId()
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to get last insert ID: %v", err)
	}

	// Fetch the complete record to get timestamps and other fields
	dbDevice, err := s.getQueries(ctx).GetDiscoveredDeviceByID(ctx, sqlc.GetDiscoveredDeviceByIDParams{
		ID:    id,
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
		if err == sql.ErrNoRows {
			return nil, minerdiscovery.MinerNotFoundFleetError
		}
		return nil, fleeterror.NewInternalErrorf("failed to query discovered device: %v", err)
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
		if err == sql.ErrNoRows {
			return 0, minerdiscovery.MinerNotFoundFleetError
		}
		return 0, fleeterror.NewInternalErrorf("failed to query discovered device: %v", err)
	}

	return dbDevice.ID, nil
}

// toDiscoveredDevice converts a sqlc DiscoveredDevice to a domain DiscoveredDevice
func toDiscoveredDevice(dbDevice sqlc.DiscoveredDevice) *discoverymodels.DiscoveredDevice {
	return &discoverymodels.DiscoveredDevice{
		Device: pb.Device{
			DeviceIdentifier: dbDevice.DeviceIdentifier,
			Model:            dbDevice.Model.String,
			Manufacturer:     dbDevice.Manufacturer.String,
			Type:             dbDevice.Type,
			IpAddress:        dbDevice.IpAddress,
			Port:             dbDevice.Port,
			UrlScheme:        dbDevice.UrlScheme,
		},
		IsActive:        dbDevice.IsActive,
		OrgID:           dbDevice.OrgID,
		FirstDiscovered: dbDevice.FirstDiscovered.Time,
		LastSeen:        dbDevice.LastSeen.Time,
	}
}
