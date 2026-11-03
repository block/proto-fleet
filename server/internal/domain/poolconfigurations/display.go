package poolconfigurations

import (
	"context"

	fleetpb "github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

type EquipmentAssignments struct {
	Assignments   []sqlc.ListPoolEquipmentAssignmentsRow
	SelectedCount int64
}

// GetAssignments reads current intent without requiring write permission or
// contacting hardware. Missing/unreadable assignments do not expose another
// owner's configuration, even to a host operator viewing allocated equipment.
func (s *Service) GetAssignments(ctx context.Context, accountID string, identifiers []string, deviceIDs []int64, filter *fleetpb.MinerListFilter) (EquipmentAssignments, error) {
	if err := validateEquipmentSelection(identifiers, deviceIDs, filter); err != nil {
		return EquipmentAssignments{}, err
	}
	if filter != nil {
		var err error
		identifiers, err = s.resolvePoolFilter(ctx, accountID, filter)
		if err != nil {
			return EquipmentAssignments{}, err
		}
	}
	return withAuthority(ctx, s, accountID, Read, func(q sqlc.Querier, p Principal) (EquipmentAssignments, error) {
		visible, err := s.authority.PoolEquipmentVisibility(ctx, q, p)
		if err != nil {
			return EquipmentAssignments{}, err
		}
		rows, err := q.ListPoolEquipmentAssignments(ctx, sqlc.ListPoolEquipmentAssignmentsParams{
			OrganizationID: p.Scope.OrganizationID, AccountID: p.Scope.account(), DeviceIds: deviceIDs, DeviceIdentifiers: identifiers,
		})
		if err != nil {
			return EquipmentAssignments{}, storageError(err)
		}
		result := EquipmentAssignments{SelectedCount: int64(len(identifiers) + len(deviceIDs))}
		for _, row := range rows {
			if visible(row.SiteID) {
				result.Assignments = append(result.Assignments, row)
			}
		}
		return result, nil
	})
}

func validateEquipmentSelection(identifiers []string, ids []int64, filter *fleetpb.MinerListFilter) error {
	selections := 0
	for _, present := range []bool{len(identifiers) > 0, len(ids) > 0, filter != nil} {
		if present {
			selections++
		}
	}
	if selections != 1 {
		return fleeterror.NewInvalidArgumentError("provide exactly one equipment selection")
	}
	if len(identifiers) > 10000 || len(ids) > 10000 {
		return fleeterror.NewInvalidArgumentError("select at most 10000 devices")
	}
	seenIDs := map[int64]bool{}
	for _, id := range ids {
		if id <= 0 || seenIDs[id] {
			return fleeterror.NewInvalidArgumentError("select unique valid devices")
		}
		seenIDs[id] = true
	}
	seen := map[string]bool{}
	for _, id := range identifiers {
		if id == "" || len(id) > 255 || seen[id] {
			return fleeterror.NewInvalidArgumentError("select unique valid devices")
		}
		seen[id] = true
	}
	return nil
}
