package sqlstores

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"strconv"

	"github.com/block/proto-fleet/server/internal/domain/deviceendpoints"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
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

func (s *SQLFleetNodePairingStore) q(ctx context.Context) sqlc.Querier {
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

func (s *SQLFleetNodePairingStore) DeviceHasRevokedFleetNodeStatus(ctx context.Context, deviceID, orgID int64) (bool, error) {
	return s.q(ctx).DeviceHasRevokedFleetNodeStatus(ctx, sqlc.DeviceHasRevokedFleetNodeStatusParams{
		DeviceID: deviceID,
		OrgID:    orgID,
	})
}

func (s *SQLFleetNodePairingStore) ClearFleetNodeRevokedStatus(ctx context.Context, fleetNodeID, deviceID, orgID int64) (int64, error) {
	return s.q(ctx).ClearFleetNodeRevokedStatus(ctx, sqlc.ClearFleetNodeRevokedStatusParams{
		FleetNodeID: fleetNodeID,
		DeviceID:    deviceID,
		OrgID:       orgID,
	})
}

func (s *SQLFleetNodePairingStore) LockDeviceForFleetNodePairing(ctx context.Context, deviceID, orgID int64) (bool, error) {
	rows, err := s.q(ctx).LockFleetNodePairingDevice(ctx, sqlc.LockFleetNodePairingDeviceParams{
		DeviceID: deviceID,
		OrgID:    orgID,
	})
	if err != nil {
		return false, err
	}
	return len(rows) != 0, nil
}

func (s *SQLFleetNodePairingStore) TransferDiscoveredDeviceAttribution(ctx context.Context, fleetNodeID, deviceID, orgID int64) (int64, error) {
	return s.q(ctx).TransferDiscoveredDeviceAttribution(ctx, sqlc.TransferDiscoveredDeviceAttributionParams{
		FleetNodeID: fleetNodeID,
		DeviceID:    deviceID,
		OrgID:       orgID,
	})
}

func (s *SQLFleetNodePairingStore) DeleteMinerCredentialsByDeviceIDAndOrgID(ctx context.Context, deviceID, orgID int64) (int64, error) {
	return s.q(ctx).DeleteMinerCredentialsByDeviceIDAndOrgID(ctx, sqlc.DeleteMinerCredentialsByDeviceIDAndOrgIDParams{
		ID:    deviceID,
		OrgID: orgID,
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
			Manufacturer:     r.Manufacturer,
			Model:            r.Model,
			AssignedAt:       r.AssignedAt,
			AssignedBy:       nullInt64ToPtr(r.AssignedBy),
		})
	}
	return out, nil
}

func (s *SQLFleetNodePairingStore) CountFleetNodeDevicesByNode(ctx context.Context, orgID int64) (map[int64]int64, error) {
	rows, err := s.q(ctx).CountFleetNodeDevicesByNode(ctx, orgID)
	if err != nil {
		return nil, err
	}
	counts := make(map[int64]int64, len(rows))
	for _, row := range rows {
		counts[row.FleetNodeID] = row.DeviceCount
	}
	return counts, nil
}

func (s *SQLFleetNodePairingStore) GetFleetNodePairedDeviceIdentifier(ctx context.Context, deviceID, orgID int64) (string, error) {
	return s.q(ctx).GetFleetNodePairedDeviceIdentifier(ctx, sqlc.GetFleetNodePairedDeviceIdentifierParams{
		DeviceID: deviceID,
		OrgID:    orgID,
	})
}

func (s *SQLFleetNodePairingStore) ListFleetNodeDiscoveredDevices(ctx context.Context, orgID int64, fleetNodeID *int64, filter pairing.FleetNodeDiscoveredDeviceFilter) ([]pairing.FleetNodeDiscoveredDevice, error) {
	params := sqlc.ListFleetNodeDiscoveredDevicesParams{
		OrgID:             orgID,
		FleetNodeID:       ptrToNullInt64(fleetNodeID),
		Identifiers:       filter.Identifiers,
		CursorID:          ptrToNullInt64(filter.CursorID),
		Limit:             ptrToNullInt64(filter.Limit),
		ExcludeAuthNeeded: sql.NullBool{Bool: filter.ExcludeAuthNeeded, Valid: filter.ExcludeAuthNeeded},
		PairingStatuses:   filter.PairingStatuses,
		Models:            filter.Models,
		Manufacturers:     filter.Manufacturers,
	}
	out := make([]pairing.FleetNodeDiscoveredDevice, 0)
	for {
		rows, err := s.q(ctx).ListFleetNodeDiscoveredDevices(ctx, params)
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			controllers, err := deviceendpoints.Decode(r.Endpoints)
			if err != nil {
				slog.WarnContext(ctx, "skipping Fleet Node discovered device with invalid endpoints", "org_id", orgID, "discovered_device_id", r.ID, "error", err)
				continue
			}
			out = append(out, pairing.FleetNodeDiscoveredDevice{
				Endpoints:        controllers,
				ID:               r.ID,
				FleetNodeID:      r.DiscoveredByFleetNodeID.Int64,
				DeviceIdentifier: r.DeviceIdentifier,
				IPAddress:        r.IpAddress,
				Port:             r.Port,
				URLScheme:        r.UrlScheme,
				RPCPort:          r.RpcPort,
				RPCTLS:           r.RpcTls,
				DriverName:       r.DriverName,
				Model:            r.Model.String,
				DeviceClass:      commonpb.DeviceClass(r.DeviceClass),
				Manufacturer:     r.Manufacturer.String,
				FirmwareVersion:  r.FirmwareVersion.String,
				LastSeen:         r.LastSeen.Time,
				PairingStatus:    r.PairingStatus,
			})
		}
		if !params.Limit.Valid || len(rows) == 0 || int64(len(rows)) < params.Limit.Int64 || int64(len(out)) == *filter.Limit {
			return out, nil
		}
		// Refill past invalid rows so a short page does not hide later devices.
		params.CursorID = sql.NullInt64{Int64: rows[len(rows)-1].ID, Valid: true}
		params.Limit.Int64 = *filter.Limit - int64(len(out))
	}
}

func (s *SQLFleetNodePairingStore) UpsertDiscoveredDeviceFromFleetNode(ctx context.Context, orgID, fleetNodeID int64, report pairing.DiscoveredDeviceReport) (pairing.DiscoveredDeviceReport, bool, error) {
	var saved pairing.DiscoveredDeviceReport
	var stored bool
	err := NewSQLTransactor(s.conn.DB).RunInTx(ctx, func(ctx context.Context) error {
		stored = false
		if err := lockEndpointDiscoveryTransaction(ctx, s.q(ctx), orgID); err != nil {
			return err
		}
		candidate := report
		allowGroupChange := false
		if len(report.Endpoints) > 0 {
			existing, endpoints, err := reconcileDeviceEndpoints(ctx, s.q(ctx), sqlc.FindEndpointDiscoveryParams{
				OrgID: orgID, FleetNodeID: sql.NullInt64{Int64: fleetNodeID, Valid: true}, DriverName: report.DriverName, ScannedHost: report.IPAddress,
				DeviceClass: int32(report.DeviceClass), Model: report.Model, Manufacturer: report.Manufacturer,
			}, report.Endpoints)
			if errors.Is(err, errAmbiguousDeviceEndpoints) || errors.Is(err, errPairedDeviceEndpoints) || errors.Is(err, errDeviceEndpointGroupChanged) {
				logEndpointDiscoveryRejection(ctx, orgID, fleetNodeID, report.DeviceIdentifier, err)
				return nil
			}
			if err != nil {
				return err
			}
			candidate.Endpoints = endpoints
			if existing != nil {
				candidate.DeviceIdentifier = existing.DeviceIdentifier
				allowGroupChange = endpointIdentityReplaced(existing, endpoints)
			} else {
				candidate.DeviceIdentifier, err = deviceendpoints.NodeIdentifier(fleetNodeID, report.DriverName, endpoints)
				if err != nil {
					return err
				}
			}
			browser, err := discoveryBrowserEndpoint(existing, endpoints)
			if err != nil {
				return err
			}
			if browser != nil {
				candidate.IPAddress, candidate.Port, candidate.URLScheme = browser.Host, strconv.Itoa(int(browser.Port)), browser.UrlScheme
				candidate.RPCPort, candidate.RPCTLS = browser.RpcPort, browser.RpcTls
			}
		} else if err := guardLegacyEndpointDiscovery(ctx, s.q(ctx), orgID, report.DeviceIdentifier, report.DriverName, report.IPAddress, report.Port, report.URLScheme, report.RPCPort, report.RPCTLS, report.DeviceClass, sql.NullInt64{Int64: fleetNodeID, Valid: true}); err != nil {
			if errors.Is(err, errPairedDeviceEndpoints) || errors.Is(err, errDeviceEndpointGroupRequired) {
				logEndpointDiscoveryRejection(ctx, orgID, fleetNodeID, report.DeviceIdentifier, err)
				return nil
			}
			return err
		}
		var err error
		saved, stored, err = s.upsertDiscoveredDeviceFromFleetNode(ctx, orgID, fleetNodeID, candidate, allowGroupChange)
		return err
	})
	return saved, stored, err
}

func logEndpointDiscoveryRejection(ctx context.Context, orgID, fleetNodeID int64, identifier string, reason error) {
	slog.WarnContext(ctx, "rejected Fleet Node device discovery report", "org_id", orgID, "fleet_node_id", fleetNodeID, "device_identifier", identifier, "reason", reason)
}

func (s *SQLFleetNodePairingStore) upsertDiscoveredDeviceFromFleetNode(ctx context.Context, orgID, fleetNodeID int64, report pairing.DiscoveredDeviceReport, allowGroupChange bool) (pairing.DiscoveredDeviceReport, bool, error) {
	endpoints, err := deviceendpoints.Encode(report.Endpoints)
	if err != nil {
		return report, false, err
	}
	key, err := deviceendpoints.Key(report.Endpoints)
	if err != nil {
		return report, false, err
	}
	row, err := s.q(ctx).UpsertDiscoveredDeviceFromFleetNode(ctx, sqlc.UpsertDiscoveredDeviceFromFleetNodeParams{
		AllowEndpointGroupChange: allowGroupChange,
		EndpointKey:              key, Endpoints: endpoints, OrgID: orgID, DeviceIdentifier: report.DeviceIdentifier,
		IpAddress: report.IPAddress, Port: report.Port, UrlScheme: report.URLScheme, RpcPort: report.RPCPort, RpcTls: report.RPCTLS,
		DriverName: report.DriverName, Model: emptyToNullString(report.Model), DeviceClass: int32(report.DeviceClass),
		Manufacturer: emptyToNullString(report.Manufacturer), FirmwareVersion: emptyToNullString(report.FirmwareVersion),
		DiscoveredByFleetNodeID: sql.NullInt64{Int64: fleetNodeID, Valid: true},
	})
	if errors.Is(err, sql.ErrNoRows) {
		return report, false, nil
	}
	if err != nil {
		return report, false, err
	}
	report.Endpoints, err = deviceendpoints.Decode(row.Endpoints)
	report.DeviceIdentifier, report.DeviceClass = row.DeviceIdentifier, commonpb.DeviceClass(row.DeviceClass)
	return report, err == nil, err
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
