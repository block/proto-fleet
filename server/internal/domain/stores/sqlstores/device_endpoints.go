package sqlstores

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strconv"

	sdk "github.com/block/proto-fleet/server/sdk/v1"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/deviceendpoints"
	"google.golang.org/protobuf/proto"
)

var errAmbiguousDeviceEndpoints = errors.New("multiple saved devices match this endpoint group; resolve duplicate inventory before pairing")
var errPairedDeviceEndpoints = errors.New("paired device endpoints cannot change during discovery; unpair the device before changing its addresses")
var errDeviceEndpointGroupRequired = errors.New("discovery report is missing the saved device endpoint group; use endpoint-aware discovery")
var errDeviceEndpointGroupChanged = errors.New("device endpoint group changed without a matching saved controller route; rescan the known controllers before pairing")

// A report batch can touch both groups and legacy rows in any order. Serialize
// only its database writes with direct discovery before taking finer locks.
func lockEndpointDiscoveryTransaction(ctx context.Context, q sqlc.Querier, orgID int64) error {
	return q.LockEndpointDiscovery(ctx, fmt.Sprintf("endpoint-discovery:%d", orgID))
}

func lockDiscoveredInventory(ctx context.Context, q sqlc.Querier, existing sqlc.DiscoveredDevice) (sqlc.DiscoveredDevice, error) {
	if _, err := q.LockEndpointDiscoveryDevices(ctx, sqlc.LockEndpointDiscoveryDevicesParams{OrgID: existing.OrgID, DiscoveredDeviceID: existing.ID}); err != nil {
		return existing, err
	}
	return q.LockDiscoveredDeviceForEndpointDiscovery(ctx, sqlc.LockDiscoveredDeviceForEndpointDiscoveryParams{OrgID: existing.OrgID, ID: existing.ID})
}

func lockEndpointDiscoveryInventory(ctx context.Context, q sqlc.Querier, existing sqlc.DiscoveredDevice) (sqlc.DiscoveredDevice, bool, error) {
	current, err := lockDiscoveredInventory(ctx, q, existing)
	if err != nil {
		return existing, false, err
	}
	paired, err := q.EndpointDiscoveryHasPairing(ctx, sqlc.EndpointDiscoveryHasPairingParams{OrgID: existing.OrgID, DiscoveredDeviceID: existing.ID})
	return current, paired, err
}

func sameEndpointRoute(a, b *commonpb.DeviceEndpoint) bool {
	return a.Id == b.Id && a.Host == b.Host && a.Port == b.Port && a.UrlScheme == b.UrlScheme && a.RpcPort == b.RpcPort && a.RpcTls == b.RpcTls
}

func preservePairedEndpointRoutes(existing sqlc.DiscoveredDevice, saved, merged []*commonpb.DeviceEndpoint) error {
	if len(saved) == 0 {
		// A legacy upgrade may name its saved route, but cannot trust a peer's
		// address or an identity learned at another address from an unauthenticated scan.
		if len(merged) != 1 {
			return errPairedDeviceEndpoints
		}
		endpoint := merged[0]
		if endpoint.Host != existing.IpAddress || endpoint.RpcPort != existing.RpcPort || endpoint.RpcTls != existing.RpcTls ||
			(existing.SerialNumber != "" && endpoint.Id != existing.SerialNumber) {
			return errPairedDeviceEndpoints
		}
		return nil
	}
	if len(saved) != len(merged) {
		return errPairedDeviceEndpoints
	}
	for i, endpoint := range saved {
		if !sameEndpointRoute(endpoint, merged[i]) {
			return errPairedDeviceEndpoints
		}
	}
	return nil
}

// Older reports can omit endpoints. Do not let that path clear a paired group
// or relocate a paired container by reusing its inventory identifier.
func guardLegacyEndpointDiscovery(ctx context.Context, q sqlc.Querier, orgID int64, identifier, driver, host, port, scheme string, rpcPort int32, rpcTLS bool, deviceClass commonpb.DeviceClass, fleetNodeID sql.NullInt64) error {
	existing, err := q.GetDiscoveredDeviceByDeviceIdentifier(ctx, sqlc.GetDiscoveredDeviceByDeviceIdentifierParams{OrgID: orgID, DeviceIdentifier: identifier})
	missing := errors.Is(err, sql.ErrNoRows)
	if err != nil && !missing {
		return err
	}
	if deviceClass == commonpb.DeviceClass_DEVICE_CLASS_CONTAINER {
		grouped, err := q.EndpointDiscoveryHasSavedGroupAtSocket(ctx, sqlc.EndpointDiscoveryHasSavedGroupAtSocketParams{OrgID: orgID, DriverName: driver, FleetNodeID: fleetNodeID, Host: host, Port: port, RpcPort: rpcPort})
		if err != nil {
			return err
		}
		if grouped {
			return errDeviceEndpointGroupRequired
		}
	}
	if missing {
		return nil
	}
	if existing.EndpointKey == "" && existing.DeviceClass != int32(commonpb.DeviceClass_DEVICE_CLASS_CONTAINER) {
		return nil
	}
	existing, paired, err := lockEndpointDiscoveryInventory(ctx, q, existing)
	if err != nil {
		return err
	}
	if existing.EndpointKey != "" {
		return errDeviceEndpointGroupRequired
	}
	if !paired {
		return nil
	}
	if deviceClass != commonpb.DeviceClass_DEVICE_CLASS_UNSPECIFIED && int32(deviceClass) != existing.DeviceClass {
		return errPairedDeviceEndpoints
	}
	if existing.IpAddress != host || existing.RpcPort != rpcPort || existing.RpcTls != rpcTLS {
		return errPairedDeviceEndpoints
	}
	if rpcPort == 0 && (existing.Port != port || existing.UrlScheme != scheme) {
		return errPairedDeviceEndpoints
	}
	return nil
}

// Caller must hold a transaction. The lock serializes discovery of different
// addresses for the same plugin-defined device, including first discovery.
func reconcileDeviceEndpoints(ctx context.Context, q sqlc.Querier, criteria sqlc.FindEndpointDiscoveryParams, incoming []*commonpb.DeviceEndpoint) (*sqlc.DiscoveredDevice, []*commonpb.DeviceEndpoint, error) {
	key, err := deviceendpoints.Key(incoming)
	if err != nil {
		return nil, nil, err
	}
	// Replacement nodes share the lock with the revoked owner's discovery group.
	if err := q.LockEndpointDiscovery(ctx, fmt.Sprintf("endpoint:%d:%s:%s", criteria.OrgID, criteria.DriverName, key)); err != nil {
		return nil, nil, err
	}
	ids := make([]string, 0, len(incoming))
	for _, e := range incoming {
		ids = append(ids, e.Id)
		ids = append(ids, e.MemberIds...)
	}
	criteria.GroupKey, criteria.EndpointIds = key, ids
	criteria.ScannedPort, criteria.ScannedRpcPort = incoming[0].Port, incoming[0].RpcPort
	matches, err := q.FindEndpointDiscovery(ctx, criteria)
	if err != nil {
		return nil, nil, err
	}
	// The query puts this network's matches first. A revoked network must not
	// make an existing local group ambiguous.
	if len(matches) == 2 && matches[0].DiscoveredByFleetNodeID == criteria.FleetNodeID && matches[1].DiscoveredByFleetNodeID != criteria.FleetNodeID {
		matches = matches[:1]
	}
	if len(matches) > 1 {
		return nil, nil, errAmbiguousDeviceEndpoints
	}
	if len(matches) == 0 {
		return nil, incoming, nil
	}
	existing, paired, err := lockEndpointDiscoveryInventory(ctx, q, matches[0])
	if err != nil {
		return nil, nil, err
	}
	groupChanged := existing.EndpointKey != "" && existing.EndpointKey != key
	if groupChanged && paired {
		return nil, nil, errPairedDeviceEndpoints
	}
	saved, err := deviceendpoints.Decode(existing.Endpoints)
	if err != nil {
		return nil, nil, err
	}
	byID := map[string]*commonpb.DeviceEndpoint{}
	for _, e := range saved {
		byID[e.Id] = e
	}
	if groupChanged {
		retained := make(map[string]*commonpb.DeviceEndpoint, len(incoming))
		for _, e := range incoming {
			previous := byID[e.Id]
			if previous == nil || previous.Host != e.Host || previous.RpcPort != e.RpcPort || previous.RpcTls != e.RpcTls ||
				(e.RpcPort == 0 && (previous.Port != e.Port || previous.UrlScheme != e.UrlScheme)) {
				return nil, nil, errDeviceEndpointGroupChanged
			}
			retained[e.Id] = previous
		}
		// A replacement group starts with only the routes verified in this scan.
		// Removed peers cannot follow the retained controller into its new group.
		byID = retained
	}
	if paired {
		if len(saved) == 0 {
			if err := preservePairedEndpointRoutes(existing, nil, incoming); err != nil {
				return nil, nil, err
			}
		} else {
			for _, e := range incoming {
				previous := byID[e.Id]
				if previous == nil || previous.Host != e.Host || previous.RpcPort != e.RpcPort || previous.RpcTls != e.RpcTls {
					return nil, nil, errPairedDeviceEndpoints
				}
			}
		}
	}
	// A first scan of the partner must retain the known controller's saved
	// address and custom web settings, even before that controller is scanned.
	if existing.EndpointKey == "" && existing.SerialNumber != "" && slices.Contains(ids, existing.SerialNumber) {
		port, err := sdk.ParsePort(existing.Port)
		if err != nil {
			return nil, nil, err
		}
		byID[existing.SerialNumber] = &commonpb.DeviceEndpoint{
			Id: existing.SerialNumber, DeviceKey: key, MemberIds: slices.Clone(incoming[0].MemberIds),
			Host: existing.IpAddress, Port: port, UrlScheme: existing.UrlScheme, RpcPort: existing.RpcPort, RpcTls: existing.RpcTls,
		}
	}
	for _, e := range incoming {
		candidate := proto.CloneOf(e)
		if previous := byID[e.Id]; previous != nil {
			candidate.Port, candidate.UrlScheme = previous.Port, previous.UrlScheme
			if paired {
				candidate.MemberIds = slices.Clone(previous.MemberIds)
			}
		} else if existing.EndpointKey == "" && existing.IpAddress == e.Host {
			port, err := sdk.ParsePort(existing.Port)
			if err != nil {
				return nil, nil, err
			}
			candidate.Port, candidate.UrlScheme = port, existing.UrlScheme
		}
		// Drop an unverified legacy default at the scanned host instead of guessing
		// which explicit RPC port it means. A separate peer must be scanned again.
		for id, previous := range byID {
			sameAddress := previous.RpcPort == candidate.RpcPort && (candidate.RpcPort != 0 || previous.Port == candidate.Port)
			unknownLegacyAddress := previous.RpcPort == 0 && candidate.RpcPort != 0
			if id != e.Id && previous.Host == candidate.Host && (sameAddress || unknownLegacyAddress) {
				delete(byID, id)
			}
		}
		byID[e.Id] = candidate
	}
	merged := make([]*commonpb.DeviceEndpoint, 0, len(byID))
	for _, e := range byID {
		merged = append(merged, e)
	}
	// Reuse the canonical codec to validate the merged set and sort it by ID.
	encoded, err := deviceendpoints.Encode(merged)
	if err != nil {
		return nil, nil, err
	}
	merged, err = deviceendpoints.Decode(encoded)
	if err == nil && paired {
		err = preservePairedEndpointRoutes(existing, saved, merged)
	}
	return &existing, merged, err
}

// The browser target follows its saved endpoint identity, independently of
// which partner discovery scans or the plugin selects for native RPCs.
func discoveryBrowserEndpoint(existing *sqlc.DiscoveredDevice, endpoints []*commonpb.DeviceEndpoint) (*commonpb.DeviceEndpoint, error) {
	if existing == nil {
		return nil, nil
	}
	saved, err := deviceendpoints.Decode(existing.Endpoints)
	if err != nil {
		return nil, err
	}
	id := existing.SerialNumber
	for _, endpoint := range saved {
		if endpoint.Host == existing.IpAddress && endpoint.RpcPort == existing.RpcPort && strconv.Itoa(int(endpoint.Port)) == existing.Port && endpoint.UrlScheme == existing.UrlScheme {
			id = endpoint.Id
			break
		}
	}
	for _, endpoint := range endpoints {
		if endpoint.Id == id || (id == "" && endpoint.Host == existing.IpAddress) {
			return endpoint, nil
		}
	}
	// Its former address now belongs to a verified peer. Follow a known address
	// rather than retaining the disproven target.
	if len(endpoints) > 0 {
		return endpoints[0], nil
	}
	return nil, fmt.Errorf("saved browser endpoint is missing from device endpoints")
}
