package sqlstores

import (
	"context"
	"fmt"
	"slices"
	"strconv"

	sdk "github.com/block/proto-fleet/server/sdk/v1"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/deviceendpoints"
	"google.golang.org/protobuf/proto"
)

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
		return nil, nil, fmt.Errorf("multiple saved devices match this endpoint group; resolve duplicate inventory before pairing")
	}
	if len(matches) == 0 {
		return nil, incoming, nil
	}
	existing := matches[0]
	saved, err := deviceendpoints.Decode(existing.Endpoints)
	if err != nil {
		return nil, nil, err
	}
	byID := map[string]*commonpb.DeviceEndpoint{}
	for _, e := range saved {
		byID[e.Id] = e
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
		} else if existing.EndpointKey == "" && existing.IpAddress == e.Host {
			port, err := sdk.ParsePort(existing.Port)
			if err != nil {
				return nil, nil, err
			}
			candidate.Port, candidate.UrlScheme = port, existing.UrlScheme
		}
		// A verified identity at this address disproves the retained peer address.
		for id, previous := range byID {
			if id != e.Id && previous.Host == candidate.Host && previous.RpcPort == candidate.RpcPort &&
				(candidate.RpcPort != 0 || previous.Port == candidate.Port) {
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
