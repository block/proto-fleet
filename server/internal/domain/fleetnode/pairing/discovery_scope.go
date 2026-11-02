package pairing

import (
	"context"

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

// splitPairTargetsByDiscoveryScope separates targets the node may pair now from
// those with an address outside its live ranges, so one stale device cannot
// block the rest of a batch.
func (s *Service) splitPairTargetsByDiscoveryScope(nodeID int64, targets []*pairingpb.FleetNodePairTarget) (allowed, rejected []*pairingpb.FleetNodePairTarget) {
	if s.discoveryConfig == nil {
		return targets, nil
	}
	scope, err := discoveryranges.Parse(s.discoveryConfig(nodeID).GetRanges())
	for _, target := range targets {
		if err == nil && targetInDiscoveryScope(scope, target) {
			allowed = append(allowed, target)
		} else {
			rejected = append(rejected, target)
		}
	}
	return allowed, rejected
}

func (s *Service) validateDeviceDiscoveryScope(ctx context.Context, nodeID, deviceID, orgID int64) error {
	if s.discoveryConfig == nil {
		return nil
	}
	target, err := s.store.GetFleetNodePairingTarget(ctx, deviceID, orgID)
	if err != nil {
		return logInternal("lookup device discovery scope", clientErrPair, err)
	}
	scope, err := discoveryranges.Parse(s.discoveryConfig(nodeID).GetRanges())
	if err != nil || !targetInDiscoveryScope(scope, target) {
		return fleeterror.NewFailedPreconditionError("device binding requires all device addresses within this fleet node's live discovery ranges")
	}
	return nil
}
