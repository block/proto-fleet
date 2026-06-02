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
