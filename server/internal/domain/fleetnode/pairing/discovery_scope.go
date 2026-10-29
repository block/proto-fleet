package pairing

import (
	"context"

	"google.golang.org/protobuf/proto"

	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/internal/domain/discoveryranges"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

func targetInDiscoveryScope(scope discoveryranges.Scope, target *pairingpb.FleetNodePairTarget) bool {
	if target == nil || !scope.ContainsString(target.GetIpAddress()) {
		return false
	}
	for _, endpoint := range target.GetEndpoints() {
		if !scope.ContainsString(endpoint.GetHost()) {
			return false
		}
	}
	return true
}

func (s *Service) validatePairTargetsDiscoveryScope(ctx context.Context, nodeID, orgID int64, targets []*pairingpb.FleetNodePairTarget) error {
	if s.discoveryConfig == nil {
		return nil
	}
	scope, parseErr := discoveryranges.Parse(s.discoveryConfig(nodeID).GetRanges())
	var repairs map[string]*pairingpb.FleetNodePairTarget
	for _, target := range targets {
		if parseErr == nil && targetInDiscoveryScope(scope, target) {
			continue
		}
		// Authentication repair may retain an existing assignment's saved route
		// after a range change. An identifier alone cannot authorize a new route.
		if repairs == nil {
			repairs = map[string]*pairingpb.FleetNodePairTarget{}
			if s.store != nil {
				pairs, err := s.store.ListFleetNodeDevices(ctx, orgID, &nodeID)
				if err != nil {
					return logInternal("lookup existing fleet node bindings", clientErrPair, err)
				}
				ids := make([]string, 0, len(pairs))
				for _, pair := range pairs {
					ids = append(ids, pair.DeviceIdentifier)
				}
				if len(ids) > 0 {
					candidates, err := s.store.ListFleetNodeDiscoveredDevices(ctx, orgID, &nodeID, FleetNodeDiscoveredDeviceFilter{Identifiers: ids})
					if err != nil {
						return logInternal("lookup authentication repair routes", clientErrPair, err)
					}
					for _, candidate := range candidates {
						if candidate.PairingStatus == StatusAuthenticationNeeded {
							repairs[candidate.DeviceIdentifier] = pairTargetsFromDiscoveredDevices([]FleetNodeDiscoveredDevice{candidate})[0]
						}
					}
				}
			}
		}
		if !samePairTargetRoute(repairs[target.GetDeviceIdentifier()], target) {
			return fleeterror.NewFailedPreconditionError("new device pairing requires all device addresses within this fleet node's live discovery ranges")
		}
	}
	return nil
}

func samePairTargetRoute(saved, target *pairingpb.FleetNodePairTarget) bool {
	if saved == nil || target == nil || saved.IpAddress != target.IpAddress || saved.Port != target.Port ||
		saved.UrlScheme != target.UrlScheme || saved.RpcPort != target.RpcPort || saved.RpcTls != target.RpcTls ||
		saved.DriverName != target.DriverName || saved.DeviceClass != target.DeviceClass || len(saved.Endpoints) != len(target.Endpoints) {
		return false
	}
	for i, endpoint := range saved.Endpoints {
		if !proto.Equal(endpoint, target.Endpoints[i]) {
			return false
		}
	}
	return true
}

func (s *Service) validateDeviceDiscoveryScope(ctx context.Context, nodeID, deviceID, orgID int64) error {
	if s.discoveryConfig == nil {
		return nil
	}
	sameNode, err := s.deviceBoundToFleetNode(ctx, nodeID, deviceID, orgID)
	if err != nil {
		return logInternal("lookup existing fleet node binding", clientErrPair, err)
	}
	if sameNode {
		return nil
	}
	store, ok := s.store.(interface {
		GetFleetNodePairingTarget(ctx context.Context, deviceID, orgID int64) (*pairingpb.FleetNodePairTarget, error)
	})
	if !ok {
		return fleeterror.NewInternalError("device discovery scope lookup is not configured")
	}
	target, err := store.GetFleetNodePairingTarget(ctx, deviceID, orgID)
	if err != nil {
		return logInternal("lookup device discovery scope", clientErrPair, err)
	}
	scope, err := discoveryranges.Parse(s.discoveryConfig(nodeID).GetRanges())
	if err != nil || !targetInDiscoveryScope(scope, target) {
		return fleeterror.NewFailedPreconditionError("new device binding requires all device addresses within this fleet node's live discovery ranges")
	}
	return nil
}
