package sqlstores

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strconv"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/deviceendpoints"
	"google.golang.org/protobuf/proto"
)

var errAmbiguousDeviceEndpoints = errors.New("multiple saved devices match this endpoint group; resolve duplicate inventory before pairing")
var errPairedDeviceEndpoints = errors.New("paired device endpoints cannot change during discovery; unpair the device before changing its addresses")
var errDeviceEndpointGroupRequired = errors.New("discovery report is missing the saved device endpoint group; use endpoint-aware discovery")
var errDeviceEndpointGroupChanged = errors.New("another cluster owns this saved controller address; unpair and remove its saved discovery before scanning again")

// Serialize report batches with direct discovery before taking inventory locks.
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

// A report without a group must not overwrite a saved endpoint group.
func guardUngroupedEndpointDiscovery(ctx context.Context, q sqlc.Querier, orgID int64, identifier, driver, host, port string, rpcPort int32, fleetNodeID sql.NullInt64) error {
	grouped, err := q.EndpointDiscoveryHasSavedGroupAtSocket(ctx, sqlc.EndpointDiscoveryHasSavedGroupAtSocketParams{OrgID: orgID, DriverName: driver, FleetNodeID: fleetNodeID, Host: host, Port: port, RpcPort: rpcPort})
	if err != nil {
		return err
	}
	if grouped {
		return errDeviceEndpointGroupRequired
	}
	existing, err := q.GetDiscoveredDeviceByDeviceIdentifier(ctx, sqlc.GetDiscoveredDeviceByDeviceIdentifierParams{OrgID: orgID, DeviceIdentifier: identifier})
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if existing.EndpointKey != "" {
		return errDeviceEndpointGroupRequired
	}
	return nil
}

// Caller must hold the organization's endpoint discovery transaction lock.
func reconcileDeviceEndpoints(ctx context.Context, q sqlc.Querier, criteria sqlc.FindEndpointDiscoveryParams, incoming []*commonpb.DeviceEndpoint) (*sqlc.DiscoveredDevice, []*commonpb.DeviceEndpoint, error) {
	key, err := deviceendpoints.Key(incoming)
	if err != nil {
		return nil, nil, err
	}
	criteria.GroupKey = key
	criteria.ScannedPort, criteria.ScannedRpcPort = incoming[0].Port, incoming[0].RpcPort
	matches, err := q.FindEndpointDiscovery(ctx, criteria)
	if err != nil {
		return nil, nil, err
	}
	// Prefer this network's inventory over inventory from a revoked node.
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
	if existing.EndpointKey != "" && existing.EndpointKey != key {
		return nil, nil, errDeviceEndpointGroupChanged
	}
	saved, err := deviceendpoints.Decode(existing.Endpoints)
	if err != nil {
		return nil, nil, err
	}
	byID := make(map[string]*commonpb.DeviceEndpoint, len(saved)+len(incoming))
	for _, endpoint := range saved {
		if !paired && len(incoming[0].MemberIds) > 0 {
			if !slices.Contains(incoming[0].MemberIds, endpoint.Id) {
				continue
			}
			endpoint.MemberIds = slices.Clone(incoming[0].MemberIds)
		}
		byID[endpoint.Id] = endpoint
	}
	for _, endpoint := range incoming {
		previous := byID[endpoint.Id]
		if paired && (previous == nil || previous.Host != endpoint.Host || previous.RpcPort != endpoint.RpcPort || previous.RpcTls != endpoint.RpcTls) {
			return nil, nil, errPairedDeviceEndpoints
		}
		candidate := proto.CloneOf(endpoint)
		if previous != nil {
			candidate.Port, candidate.UrlScheme = previous.Port, previous.UrlScheme
		}
		// Only the scanned controller can own its address. Do not retain a
		// different identity at that socket after an unpaired address change.
		for id, previous := range byID {
			if id != endpoint.Id && previous.Host == candidate.Host && previous.RpcPort == candidate.RpcPort && (candidate.RpcPort != 0 || previous.Port == candidate.Port) {
				delete(byID, id)
			}
		}
		byID[endpoint.Id] = candidate
	}
	merged := make([]*commonpb.DeviceEndpoint, 0, len(byID))
	for _, endpoint := range byID {
		merged = append(merged, endpoint)
	}
	encoded, err := deviceendpoints.Encode(merged)
	if err != nil {
		return nil, nil, err
	}
	merged, err = deviceendpoints.Decode(encoded)
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
