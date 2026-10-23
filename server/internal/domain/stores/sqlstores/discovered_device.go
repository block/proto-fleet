package sqlstores

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"strconv"

	"github.com/block/proto-fleet/server/internal/domain/deviceendpoints"
	"google.golang.org/protobuf/proto"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
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

func (s *SQLDiscoveredDeviceStore) getQueries(ctx context.Context) sqlc.Querier {
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
	return s.save(ctx, doi, device, false)
}

func (s *SQLDiscoveredDeviceStore) ActivateDirect(ctx context.Context, doi discoverymodels.DeviceOrgIdentifier) error {
	count, err := s.getQueries(ctx).ActivateDirectDiscoveredDevice(ctx, sqlc.ActivateDirectDiscoveredDeviceParams{
		OrgID: doi.OrgID, DeviceIdentifier: doi.DeviceIdentifier,
	})
	if err != nil {
		return err
	}
	if count == 0 {
		return fleeterror.NewFailedPreconditionError("discovered device is missing or managed by a Fleet Node")
	}
	return nil
}

func (s *SQLDiscoveredDeviceStore) SaveDirect(ctx context.Context, doi discoverymodels.DeviceOrgIdentifier, device *discoverymodels.DiscoveredDevice) (*discoverymodels.DiscoveredDevice, error) {
	var saved *discoverymodels.DiscoveredDevice
	err := NewSQLTransactor(s.conn.DB).RunInTx(ctx, func(ctx context.Context) error {
		if err := lockEndpointDiscoveryTransaction(ctx, s.getQueries(ctx), doi.OrgID); err != nil {
			return err
		}
		if len(device.Endpoints) == 0 {
			if err := guardLegacyEndpointDiscovery(ctx, s.getQueries(ctx), doi.OrgID, doi.DeviceIdentifier, device.IpAddress, device.Port, device.UrlScheme, device.RpcPort, device.RpcTls); err != nil {
				return err
			}
			var err error
			saved, err = s.save(ctx, doi, device, true)
			return err
		}
		existing, endpoints, err := reconcileDeviceEndpoints(ctx, s.getQueries(ctx), sqlc.FindEndpointDiscoveryParams{
			OrgID: doi.OrgID, DriverName: device.DriverName, ScannedHost: device.IpAddress,
			DeviceClass: int32(device.DeviceClass), Model: device.Model, Manufacturer: device.Manufacturer,
		}, device.Endpoints)
		if err != nil {
			return err
		}
		candidate := &discoverymodels.DiscoveredDevice{
			IsActive: device.IsActive, OrgID: device.OrgID,
			FirstDiscovered: device.FirstDiscovered, LastSeen: device.LastSeen,
			DiscoveredByFleetNodeID: device.DiscoveredByFleetNodeID,
			DefaultPasswordActive:   device.DefaultPasswordActive,
		}
		proto.Merge(&candidate.Device, &device.Device)
		candidate.Endpoints = endpoints
		if existing != nil {
			doi.DeviceIdentifier = existing.DeviceIdentifier
			if existing.SerialNumber != "" {
				candidate.SerialNumber = existing.SerialNumber
			}
		}
		browser, err := discoveryBrowserEndpoint(existing, endpoints)
		if err != nil {
			return err
		}
		if browser != nil {
			candidate.IpAddress, candidate.Port, candidate.UrlScheme = browser.Host, strconv.Itoa(int(browser.Port)), browser.UrlScheme
			candidate.RpcPort, candidate.RpcTls = browser.RpcPort, browser.RpcTls
		}
		saved, err = s.save(ctx, doi, candidate, true)
		return err
	})
	if errors.Is(err, errPairedDeviceEndpoints) {
		return nil, fleeterror.NewFailedPreconditionError(errPairedDeviceEndpoints.Error())
	}
	if errors.Is(err, errDeviceEndpointGroupRequired) {
		return nil, fleeterror.NewFailedPreconditionError(errDeviceEndpointGroupRequired.Error())
	}
	return saved, err
}

func (s *SQLDiscoveredDeviceStore) save(ctx context.Context, doi discoverymodels.DeviceOrgIdentifier, device *discoverymodels.DiscoveredDevice, direct bool) (*discoverymodels.DiscoveredDevice, error) {
	controllers, err := deviceendpoints.Encode(device.Endpoints)
	if err != nil {
		return nil, err
	}
	key, err := deviceendpoints.Key(device.Endpoints)
	if err != nil {
		return nil, err
	}
	insertedID, err := s.getQueries(ctx).UpsertDiscoveredDevice(ctx, sqlc.UpsertDiscoveredDeviceParams{
		DirectDiscovery: direct,
		EndpointKey:     key, Endpoints: controllers,
		OrgID:            doi.OrgID,
		DeviceIdentifier: doi.DeviceIdentifier,
		Model:            sql.NullString{String: device.Model, Valid: len(device.Model) > 0},
		Manufacturer:     sql.NullString{String: device.Manufacturer, Valid: len(device.Manufacturer) > 0},
		FirmwareVersion:  sql.NullString{String: device.FirmwareVersion, Valid: len(device.FirmwareVersion) > 0},
		IpAddress:        device.IpAddress,
		Port:             device.Port,
		UrlScheme:        device.UrlScheme,
		RpcPort:          device.RpcPort,
		RpcTls:           device.RpcTls,
		IsActive:         device.IsActive,
		DriverName:       device.Device.DriverName,
		DeviceClass:      int32(device.DeviceClass),
		SerialNumber:     device.SerialNumber,
		MacAddress:       device.MacAddress,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fleeterror.NewFailedPreconditionError("discovered device ownership or endpoint group changed; rescan before retrying")
		}
		return nil, fleeterror.NewInternalErrorf("failed to upsert discovered device: %w", err)
	}

	// Fetch the complete record to get timestamps and other fields
	dbDevice, err := s.getQueries(ctx).GetDiscoveredDeviceByID(ctx, sqlc.GetDiscoveredDeviceByIDParams{
		ID:    insertedID,
		OrgID: doi.OrgID,
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to fetch discovered device after upsert: %w", err)
	}

	return toDiscoveredDevice(dbDevice)
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

	return toDiscoveredDevice(dbDevice)
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

	return toDiscoveredDevice(dbDevice)
}

func (s *SQLDiscoveredDeviceStore) GetDirectContainerByIP(ctx context.Context, orgID int64, ipAddress, driverName, model, manufacturer string) (*discoverymodels.DiscoveredDevice, error) {
	rows, err := s.getQueries(ctx).FindDirectContainerByIP(ctx, sqlc.FindDirectContainerByIPParams{
		OrgID: orgID, IpAddress: ipAddress, DriverName: driverName, Model: model, Manufacturer: manufacturer,
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to find container at shared IP: %v", err)
	}
	switch len(rows) {
	case 0:
		return nil, minerdiscovery.MinerNotFoundFleetError
	case 1:
		return toDiscoveredDevice(rows[0])
	default:
		return nil, fleeterror.NewFailedPreconditionError("multiple containers match this IP; resolve duplicate inventory before rediscovery")
	}
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

	devices := make([]*discoverymodels.DiscoveredDevice, 0)
	var lastID int64
	for {
		// Read one extra valid device to determine whether another page exists.
		rows, err := s.getQueries(ctx).GetActiveUnpairedDiscoveredDevices(ctx, sqlc.GetActiveUnpairedDiscoveredDevicesParams{
			OrgID: orgID, CursorID: sql.NullInt64{Int64: cursorID, Valid: cursorID > 0}, Limit: limit + 1,
		})
		if err != nil {
			return nil, "", fleeterror.NewInternalErrorf("failed to query active unpaired devices: %v", err)
		}
		for _, row := range rows {
			device, err := toDiscoveredDeviceFromRow(row)
			if err != nil {
				slog.WarnContext(ctx, "skipping discovered device with invalid endpoints", "org_id", orgID, "discovered_device_id", row.ID, "error", err)
				continue
			}
			if len(devices) == int(limit) {
				return devices, s.encodeCursor(lastID), nil
			}
			devices = append(devices, device)
			lastID = row.ID
		}
		if len(rows) < int(limit)+1 {
			return devices, "", nil
		}
		cursorID = rows[len(rows)-1].ID
	}
}

// toDiscoveredDeviceFromRow converts a GetActiveUnpairedDiscoveredDevicesRow to a domain DiscoveredDevice
func toDiscoveredDeviceFromRow(dbDevice sqlc.GetActiveUnpairedDiscoveredDevicesRow) (*discoverymodels.DiscoveredDevice, error) {
	controllers, err := deviceendpoints.Decode(dbDevice.Endpoints)
	if err != nil {
		return nil, err
	}
	return &discoverymodels.DiscoveredDevice{
		Device: pb.Device{
			DeviceIdentifier: dbDevice.DeviceIdentifier,
			Endpoints:        controllers,
			SerialNumber:     dbDevice.SerialNumber,
			MacAddress:       dbDevice.MacAddress,
			Model:            dbDevice.Model.String,
			DeviceClass:      commonpb.DeviceClass(dbDevice.DeviceClass),
			Manufacturer:     dbDevice.Manufacturer.String,
			FirmwareVersion:  dbDevice.FirmwareVersion.String,
			IpAddress:        dbDevice.IpAddress,
			Port:             dbDevice.Port,
			UrlScheme:        dbDevice.UrlScheme,
			RpcPort:          dbDevice.RpcPort,
			RpcTls:           dbDevice.RpcTls,
			DriverName:       dbDevice.DriverName,
		},
		IsActive:        dbDevice.IsActive,
		OrgID:           dbDevice.OrgID,
		FirstDiscovered: dbDevice.FirstDiscovered.Time,
		LastSeen:        dbDevice.LastSeen.Time,
	}, nil
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
func toDiscoveredDevice(dbDevice sqlc.DiscoveredDevice) (*discoverymodels.DiscoveredDevice, error) {
	controllers, err := deviceendpoints.Decode(dbDevice.Endpoints)
	if err != nil {
		return nil, fleeterror.NewFailedPreconditionErrorf("invalid stored endpoints for discovered device %q; repair its inventory before use: %v", dbDevice.DeviceIdentifier, err)
	}
	var attribution *int64
	if dbDevice.DiscoveredByFleetNodeID.Valid {
		v := dbDevice.DiscoveredByFleetNodeID.Int64
		attribution = &v
	}
	return &discoverymodels.DiscoveredDevice{
		Device: pb.Device{
			DeviceIdentifier: dbDevice.DeviceIdentifier,
			Endpoints:        controllers,
			SerialNumber:     dbDevice.SerialNumber,
			MacAddress:       dbDevice.MacAddress,
			Model:            dbDevice.Model.String,
			DeviceClass:      commonpb.DeviceClass(dbDevice.DeviceClass),
			Manufacturer:     dbDevice.Manufacturer.String,
			FirmwareVersion:  dbDevice.FirmwareVersion.String,
			IpAddress:        dbDevice.IpAddress,
			Port:             dbDevice.Port,
			UrlScheme:        dbDevice.UrlScheme,
			RpcPort:          dbDevice.RpcPort,
			RpcTls:           dbDevice.RpcTls,
			DriverName:       dbDevice.DriverName,
		},
		IsActive:                dbDevice.IsActive,
		OrgID:                   dbDevice.OrgID,
		FirstDiscovered:         dbDevice.FirstDiscovered.Time,
		LastSeen:                dbDevice.LastSeen.Time,
		DiscoveredByFleetNodeID: attribution,
	}, nil
}
