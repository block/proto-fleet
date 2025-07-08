package sqlstores

import (
	"context"
	"database/sql"

	fm "github.com/btc-mining/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/btc-mining/proto-fleet/server/generated/sqlc"
	"github.com/btc-mining/proto-fleet/server/internal/domain/minerdiscovery"
	stores "github.com/btc-mining/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/secrets"
)

var _ stores.DeviceStore = &SQLDeviceStore{}

type SQLDeviceStore struct {
	SQLConnectionManager
}

func NewSQLDeviceStore(conn *sql.DB) *SQLDeviceStore {
	return &SQLDeviceStore{
		SQLConnectionManager: NewSQLConnectionManager(conn),
	}
}

func (s *SQLDeviceStore) getQueries(ctx context.Context) *sqlc.Queries {
	return s.GetQueries(ctx)
}

func (s *SQLDeviceStore) GetDeviceByDeviceIdentifier(ctx context.Context, identifier string, orgID int64) (*pb.Device, error) {
	device, err := s.getQueries(ctx).GetDeviceByDeviceIdentifier(ctx, sqlc.GetDeviceByDeviceIdentifierParams{
		DeviceIdentifier: identifier,
		OrgID:            orgID,
	})
	if err != nil {
		return nil, err
	}
	return &pb.Device{
		DeviceIdentifier: device.DeviceIdentifier,
		MacAddress:       device.MacAddress,
		SerialNumber:     device.SerialNumber.String,
		Model:            device.Model.String,
		Manufacturer:     device.Manufacturer.String,
	}, nil
}

func (s *SQLDeviceStore) UpsertDevice(ctx context.Context, device *pb.Device, orgID int64, deviceType string) error {
	_, err := s.getQueries(ctx).UpsertDevice(ctx, sqlc.UpsertDeviceParams{
		OrgID:            orgID,
		DeviceIdentifier: device.DeviceIdentifier,
		MacAddress:       device.MacAddress,
		SerialNumber:     sql.NullString{String: device.SerialNumber, Valid: len(device.SerialNumber) > 0},
		Model:            sql.NullString{String: device.Model, Valid: len(device.Model) > 0},
		Manufacturer:     sql.NullString{String: device.Manufacturer, Valid: len(device.Manufacturer) > 0},
		Type:             deviceType,
		IsActive:         sql.NullBool{Bool: true, Valid: true},
	})

	return err
}

func (s *SQLDeviceStore) UpsertDeviceIPAssignment(ctx context.Context, device *pb.Device, orgID int64, ipAddress string, port string) error {
	dbDevice, err := s.getQueries(ctx).GetDeviceByDeviceIdentifier(ctx, sqlc.GetDeviceByDeviceIdentifierParams{
		DeviceIdentifier: device.DeviceIdentifier,
		OrgID:            orgID,
	})
	if err != nil {
		return err
	}

	// Handle IP assignment
	currentIPAssignment, err := s.getQueries(ctx).GetActiveDeviceIPAssignmentByDeviceID(ctx, dbDevice.ID)
	if err != nil && err != sql.ErrNoRows {
		return err
	} else if err != sql.ErrNoRows && currentIPAssignment.IpAddress == device.IpAddress && currentIPAssignment.Port == device.Port {
		// Device IP assignment already exists
		return nil
	}

	// Create and activate new IP assignment
	err = s.getQueries(ctx).CreateInactiveDeviceIPAssignment(ctx, sqlc.CreateInactiveDeviceIPAssignmentParams{
		DeviceID:  dbDevice.ID,
		IpAddress: ipAddress,
		Port:      port,
	})
	if err != nil {
		return err
	}

	return s.getQueries(ctx).ActivateNewIPAssignment(ctx, sqlc.ActivateNewIPAssignmentParams{
		DeviceID:  dbDevice.ID,
		IpAddress: ipAddress,
		Port:      port,
	})
}

func (s *SQLDeviceStore) UpsertMinerCredentials(ctx context.Context, device *pb.Device, orgID int64, usernameEnc string, passwordEnc *secrets.Text) error {
	dbDevice, err := s.getQueries(ctx).GetDeviceByDeviceIdentifier(ctx, sqlc.GetDeviceByDeviceIdentifierParams{
		DeviceIdentifier: device.DeviceIdentifier,
		OrgID:            orgID,
	})
	if err != nil {
		return err
	}
	err = s.getQueries(ctx).UpsertMinerCredentials(ctx, sqlc.UpsertMinerCredentialsParams{
		DeviceID:    dbDevice.ID,
		UsernameEnc: usernameEnc,
		PasswordEnc: passwordEnc.String(),
	})

	return err
}

func (s *SQLDeviceStore) UpsertDevicePairing(ctx context.Context, device *pb.Device, orgID int64, pairingToken string, pairingStatus string) error {
	dbDevice, err := s.getQueries(ctx).GetDeviceByDeviceIdentifier(ctx, sqlc.GetDeviceByDeviceIdentifierParams{
		DeviceIdentifier: device.DeviceIdentifier,
		OrgID:            orgID,
	})
	if err != nil {
		return err
	}
	_, err = s.getQueries(ctx).UpsertDevicePairing(ctx, sqlc.UpsertDevicePairingParams{
		DeviceID:      dbDevice.ID,
		PairingToken:  sql.NullString{String: pairingToken, Valid: len(pairingToken) > 0},
		PairingStatus: sqlc.DevicePairingPairingStatus(pairingStatus),
	})

	return err
}

func (s *SQLDeviceStore) GetMinerCredentials(ctx context.Context, device *pb.Device, orgID int64) (*pb.Credentials, error) {
	dbDevice, err := s.GetQueries(ctx).GetDeviceByDeviceIdentifier(ctx, sqlc.GetDeviceByDeviceIdentifierParams{
		DeviceIdentifier: device.DeviceIdentifier,
		OrgID:            orgID,
	})
	if err != nil {
		return nil, err
	}
	credentials, err := s.GetQueries(ctx).GetMinerCredentialsByDeviceID(ctx, dbDevice.ID)
	if err != nil {
		return nil, err
	}
	return &pb.Credentials{
		Username: credentials.UsernameEnc,
		Password: &credentials.PasswordEnc,
	}, nil
}

func (s *SQLDeviceStore) GetDeviceWithIPAssignment(ctx context.Context, deviceIdentifier string, orgID int64) (*minerdiscovery.DiscoveredDevice, error) {
	q := s.GetQueries(ctx)

	device, err := q.GetDeviceByDeviceIdentifier(ctx, sqlc.GetDeviceByDeviceIdentifierParams{
		DeviceIdentifier: deviceIdentifier,
		OrgID:            orgID,
	})
	if err != nil {
		return nil, err
	}

	// Get the IP assignment for this device
	ipAssignment, err := q.GetActiveDeviceIPAssignmentByDeviceID(ctx, device.ID)
	if err != nil {
		return nil, err
	}

	return &minerdiscovery.DiscoveredDevice{
		Device: pb.Device{
			DeviceIdentifier: device.DeviceIdentifier,
			MacAddress:       device.MacAddress,
			SerialNumber:     device.SerialNumber.String,
			Model:            device.Model.String,
			Manufacturer:     device.Manufacturer.String,
			IpAddress:        ipAssignment.IpAddress,
			Port:             ipAssignment.Port,
		},
		OrgID: orgID,
		Type:  device.Type,
	}, nil
}

func (s *SQLDeviceStore) GetTotalPairedDevices(ctx context.Context) (int64, error) {
	return s.GetQueries(ctx).GetTotalPairedDevices(ctx)
}

func (s *SQLDeviceStore) ListPairedDevices(ctx context.Context, cursor stores.Cursor, pageSize int32) ([]*fm.PairedDevice, stores.Cursor, error) {
	result, err := s.GetQueries(ctx).ListPairedDevices(ctx, sqlc.ListPairedDevicesParams{
		CursorID:       sql.NullInt64{Int64: cursor.ID, Valid: cursor.ID > 0},
		DeviceCursorID: sql.NullInt64{Int64: cursor.DeviceID, Valid: cursor.DeviceID > 0},
		Limit:          pageSize + 1, // request one extra to determine if there are more pages
	})
	if err != nil {
		return nil, stores.Cursor{}, err
	}

	devices := make([]*fm.PairedDevice, len(result))
	for i, row := range result {
		devices[i] = &fm.PairedDevice{
			DeviceIdentifier: row.DeviceIdentifier,
			MacAddress:       row.MacAddress,
			SerialNumber:     row.SerialNumber.String,
		}
	}

	// Handle pagination
	if len(devices) > int(pageSize) {
		// We got an extra record, so there are more pages
		devices = devices[:pageSize]

		// Create next page token from last visible item
		lastDevice := result[pageSize-1]
		cursor = stores.Cursor{
			ID:       lastDevice.CursorID,
			DeviceID: lastDevice.DeviceID,
		}
	} else {
		// This is the last page
		cursor = stores.Cursor{}
	}

	return devices, cursor, nil
}

func (s *SQLDeviceStore) ListPairedMinersWithStatus(ctx context.Context, orgID int64, pageSize int32) ([]*pb.Device, error) {
	result, err := s.GetQueries(ctx).ListPairedMinersWithStatus(ctx, sqlc.ListPairedMinersWithStatusParams{
		OrgID: orgID,
		Limit: pageSize,
	})
	if err != nil {
		return nil, err
	}

	devices := make([]*pb.Device, len(result))
	for i, row := range result {
		devices[i] = &pb.Device{
			DeviceIdentifier: row.DeviceIdentifier,
			MacAddress:       row.MacAddress,
			SerialNumber:     row.SerialNumber.String,
			Model:            row.Model.String,
			Manufacturer:     row.Manufacturer.String,
			IpAddress:        row.IpAddress.String,
			Port:             row.Port.String,
		}
	}

	return devices, nil
}
