package sqlstores

import (
	"context"
	"database/sql"
	"errors"
	"slices"

	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/deviceendpoints"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	discovery "github.com/block/proto-fleet/server/internal/domain/minerdiscovery/models"
)

const pairingSnapshotChanged = "device endpoint group, addresses, or owner changed during pairing; discover the device again and retry"

func (s *SQLDiscoveredDeviceStore) LockPairingSnapshot(ctx context.Context, doi discovery.DeviceOrgIdentifier, expected *pairingpb.Device, expectedFleetNodeID *int64) error {
	if expected == nil {
		return fleeterror.NewFailedPreconditionError("pairing discovery snapshot is missing; discover the device again and retry")
	}
	if s.GetTxQueries(ctx) == nil {
		return fleeterror.NewInternalError("pairing snapshot lock requires a transaction")
	}
	q := s.getQueries(ctx)
	existing, err := q.GetDiscoveredDeviceByDeviceIdentifier(ctx, sqlc.GetDiscoveredDeviceByDeviceIdentifierParams{OrgID: doi.OrgID, DeviceIdentifier: doi.DeviceIdentifier})
	if errors.Is(err, sql.ErrNoRows) {
		return fleeterror.NewFailedPreconditionError(pairingSnapshotChanged)
	}
	if err != nil {
		return err
	}
	current, err := lockDiscoveredInventory(ctx, q, existing)
	if errors.Is(err, sql.ErrNoRows) {
		return fleeterror.NewFailedPreconditionError(pairingSnapshotChanged)
	}
	if err != nil {
		return err
	}
	key, err := deviceendpoints.Key(expected.Endpoints)
	if err != nil {
		return fleeterror.NewFailedPreconditionError("pairing discovery snapshot has invalid endpoints; discover the device again and retry")
	}
	if current.DiscoveredByFleetNodeID.Valid != (expectedFleetNodeID != nil) || (expectedFleetNodeID != nil && current.DiscoveredByFleetNodeID.Int64 != *expectedFleetNodeID) {
		return fleeterror.NewFailedPreconditionError(pairingSnapshotChanged)
	}
	if current.DriverName != expected.DriverName || current.DeviceClass != int32(expected.DeviceClass) || current.EndpointKey != key ||
		current.IpAddress != expected.IpAddress || current.Port != expected.Port || current.UrlScheme != expected.UrlScheme ||
		current.RpcPort != expected.RpcPort || current.RpcTls != expected.RpcTls {
		return fleeterror.NewFailedPreconditionError(pairingSnapshotChanged)
	}
	endpoints, err := deviceendpoints.Decode(current.Endpoints)
	if err != nil || len(endpoints) != len(expected.Endpoints) {
		return fleeterror.NewFailedPreconditionError(pairingSnapshotChanged)
	}
	for _, endpoint := range endpoints {
		found := false
		for _, verified := range expected.Endpoints {
			if endpoint.Id != verified.Id {
				continue
			}
			members, verifiedMembers := slices.Clone(endpoint.MemberIds), slices.Clone(verified.MemberIds)
			slices.Sort(members)
			slices.Sort(verifiedMembers)
			if !sameEndpointRoute(endpoint, verified) || endpoint.DeviceKey != verified.DeviceKey || !slices.Equal(members, verifiedMembers) {
				return fleeterror.NewFailedPreconditionError(pairingSnapshotChanged)
			}
			found = true
			break
		}
		if !found {
			return fleeterror.NewFailedPreconditionError(pairingSnapshotChanged)
		}
	}
	return nil
}
