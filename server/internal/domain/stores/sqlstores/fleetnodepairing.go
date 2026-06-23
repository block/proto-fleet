package sqlstores

import (
	"context"
	"database/sql"
	"errors"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/pairing"
)

var _ pairing.Store = &SQLFleetNodePairingStore{}

type SQLFleetNodePairingStore struct {
	SQLConnectionManager
}

func NewSQLFleetNodePairingStore(conn *sql.DB) *SQLFleetNodePairingStore {
	return &SQLFleetNodePairingStore{SQLConnectionManager: NewSQLConnectionManager(conn)}
}

func (s *SQLFleetNodePairingStore) q(ctx context.Context) *sqlc.Queries {
	return s.GetQueries(ctx)
}

func (s *SQLFleetNodePairingStore) PairDeviceToFleetNode(ctx context.Context, fleetNodeID, deviceID, orgID int64, assignedBy *int64) (int64, error) {
	return s.q(ctx).PairDeviceToFleetNode(ctx, sqlc.PairDeviceToFleetNodeParams{
		FleetNodeID: fleetNodeID,
		DeviceID:    deviceID,
		OrgID:       orgID,
		AssignedBy:  ptrToNullInt64(assignedBy),
	})
}

func (s *SQLFleetNodePairingStore) TransferDiscoveredDeviceAttribution(ctx context.Context, fleetNodeID, deviceID, orgID int64) (int64, error) {
	return s.q(ctx).TransferDiscoveredDeviceAttribution(ctx, sqlc.TransferDiscoveredDeviceAttributionParams{
		FleetNodeID: fleetNodeID,
		DeviceID:    deviceID,
		OrgID:       orgID,
	})
}

func (s *SQLFleetNodePairingStore) DeviceHasActiveCloudPairing(ctx context.Context, deviceID, orgID int64) (bool, error) {
	return s.q(ctx).DeviceHasActiveCloudPairing(ctx, sqlc.DeviceHasActiveCloudPairingParams{
		DeviceID: deviceID,
		OrgID:    orgID,
	})
}

func (s *SQLFleetNodePairingStore) DeviceHasActivePairing(ctx context.Context, deviceID, orgID int64) (bool, error) {
	return s.q(ctx).DeviceHasActivePairing(ctx, sqlc.DeviceHasActivePairingParams{
		DeviceID: deviceID,
		OrgID:    orgID,
	})
}

func (s *SQLFleetNodePairingStore) UnpairDevice(ctx context.Context, deviceID, orgID int64) (int64, error) {
	return s.q(ctx).UnpairDevice(ctx, sqlc.UnpairDeviceParams{
		DeviceID: deviceID,
		OrgID:    orgID,
	})
}

func (s *SQLFleetNodePairingStore) ListFleetNodeDevices(ctx context.Context, orgID int64, fleetNodeID *int64) ([]pairing.FleetNodeDevice, error) {
	rows, err := s.q(ctx).ListFleetNodeDevices(ctx, sqlc.ListFleetNodeDevicesParams{
		OrgID:       orgID,
		FleetNodeID: ptrToNullInt64(fleetNodeID),
	})
	if err != nil {
		return nil, err
	}
	out := make([]pairing.FleetNodeDevice, 0, len(rows))
	for _, r := range rows {
		out = append(out, pairing.FleetNodeDevice{
			FleetNodeID:      r.FleetNodeID,
			DeviceID:         r.DeviceID,
			DeviceIdentifier: r.DeviceIdentifier,
			DeviceType:       r.DeviceType,
			AssignedAt:       r.AssignedAt,
			AssignedBy:       nullInt64ToPtr(r.AssignedBy),
		})
	}
	return out, nil
}

func (s *SQLFleetNodePairingStore) ListFleetNodeDiscoveredDevices(ctx context.Context, orgID int64, fleetNodeID *int64, filter pairing.FleetNodeDiscoveredDeviceFilter) ([]pairing.FleetNodeDiscoveredDevice, error) {
	rows, err := s.q(ctx).ListFleetNodeDiscoveredDevices(ctx, sqlc.ListFleetNodeDiscoveredDevicesParams{
		OrgID:             orgID,
		FleetNodeID:       ptrToNullInt64(fleetNodeID),
		Identifiers:       filter.Identifiers,
		CursorID:          ptrToNullInt64(filter.CursorID),
		Limit:             ptrToNullInt64(filter.Limit),
		ExcludeAuthNeeded: sql.NullBool{Bool: filter.ExcludeAuthNeeded, Valid: filter.ExcludeAuthNeeded},
		PairingStatuses:   filter.PairingStatuses,
		Models:            filter.Models,
		Manufacturers:     filter.Manufacturers,
	})
	if err != nil {
		return nil, err
	}
	out := make([]pairing.FleetNodeDiscoveredDevice, 0, len(rows))
	for _, r := range rows {
		out = append(out, pairing.FleetNodeDiscoveredDevice{
			ID:               r.ID,
			FleetNodeID:      r.DiscoveredByFleetNodeID.Int64,
			DeviceIdentifier: r.DeviceIdentifier,
			IPAddress:        r.IpAddress,
			Port:             r.Port,
			URLScheme:        r.UrlScheme,
			DriverName:       r.DriverName,
			Model:            r.Model.String,
			Manufacturer:     r.Manufacturer.String,
			FirmwareVersion:  r.FirmwareVersion.String,
			LastSeen:         r.LastSeen.Time,
			PairingStatus:    r.PairingStatus,
		})
	}
	return out, nil
}

func (s *SQLFleetNodePairingStore) UpsertDiscoveredDeviceFromFleetNode(ctx context.Context, orgID, fleetNodeID int64, report pairing.DiscoveredDeviceReport) (int64, error) {
	return s.q(ctx).UpsertDiscoveredDeviceFromFleetNode(ctx, sqlc.UpsertDiscoveredDeviceFromFleetNodeParams{
		OrgID:                   orgID,
		DeviceIdentifier:        report.DeviceIdentifier,
		IpAddress:               report.IPAddress,
		Port:                    report.Port,
		UrlScheme:               report.URLScheme,
		DriverName:              report.DriverName,
		Model:                   emptyToNullString(report.Model),
		Manufacturer:            emptyToNullString(report.Manufacturer),
		FirmwareVersion:         emptyToNullString(report.FirmwareVersion),
		DiscoveredByFleetNodeID: sql.NullInt64{Int64: fleetNodeID, Valid: true},
	})
}

func (s *SQLFleetNodePairingStore) GetDeviceIDByDeviceIdentifier(ctx context.Context, identifier string) (int64, error) {
	return s.q(ctx).GetDeviceIDByDeviceIdentifier(ctx, identifier)
}

func (s *SQLFleetNodePairingStore) DeviceExistsInOrg(ctx context.Context, deviceID, orgID int64) (bool, error) {
	_, err := s.q(ctx).GetDeviceByID(ctx, sqlc.GetDeviceByIDParams{ID: deviceID, OrgID: orgID})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
