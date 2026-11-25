package poolconfigurations

import (
	"context"

	fleetpb "github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"google.golang.org/protobuf/proto"
)

func (s *Service) ReviewSelection(ctx context.Context, accountID, id string, version int64, identifiers []string, deviceIDs []int64, filter *fleetpb.MinerListFilter) (Review, error) {
	if err := validateEquipmentSelection(identifiers, deviceIDs, filter); err != nil {
		return Review{}, err
	}
	if len(deviceIDs) > 0 {
		targets := make([]AssignmentTarget, len(deviceIDs))
		for i, deviceID := range deviceIDs {
			targets[i] = AssignmentTarget{Kind: TargetDevice, ID: deviceID, DeviceID: deviceID}
		}
		return s.ReviewAssignment(ctx, accountID, id, version, targets)
	}
	if filter == nil {
		return s.ReviewDevices(ctx, accountID, id, version, identifiers)
	}
	identifiers, err := s.resolvePoolFilter(ctx, accountID, filter)
	if err != nil {
		return Review{}, err
	}
	// Resolve only at review. Confirmation uses the frozen concrete target set.
	return s.ReviewDevices(ctx, accountID, id, version, identifiers)
}

func (s *Service) resolvePoolFilter(ctx context.Context, accountID string, filter *fleetpb.MinerListFilter) ([]string, error) {
	if accountID != "" {
		return nil, fleeterror.NewInvalidArgumentError("account assignments require explicit equipment")
	}
	p, err := withAuthority(ctx, s, accountID, Read, func(_ sqlc.Querier, p Principal) (Principal, error) { return p, nil })
	if err != nil {
		return nil, err
	}
	if s.selector == nil {
		return nil, fleeterror.NewInternalError("equipment selection is unavailable")
	}
	resolved, ok := proto.Clone(filter).(*fleetpb.MinerListFilter)
	if !ok {
		return nil, fleeterror.NewInternalError("invalid equipment filter")
	}
	if len(resolved.PairingStatuses) == 0 {
		resolved.PairingStatuses = []fleetpb.PairingStatus{fleetpb.PairingStatus_PAIRING_STATUS_PAIRED, fleetpb.PairingStatus_PAIRING_STATUS_DEFAULT_PASSWORD}
	}
	identifiers, err := s.selector.ResolveDeviceIdentifiers(ctx, &fleetpb.DeviceSelector{SelectionType: &fleetpb.DeviceSelector_AllDevices{AllDevices: resolved}}, p.Scope.OrganizationID)
	if err != nil {
		return nil, err
	}
	return identifiers, nil
}
