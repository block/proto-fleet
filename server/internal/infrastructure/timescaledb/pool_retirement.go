package timescaledb

import (
	"context"

	"github.com/block/proto-fleet/server/generated/sqlc"
)

// The inventory transaction owns the organization lock throughout both reads
// and retirement. A module leaving and later returning cannot revive intent:
// the first accepted placement change permanently retires that generation.
func retireChangedPoolAssignments(ctx context.Context, q *sqlc.Queries, selection sqlc.ListPoolAssignmentPhysicalBindingsParams, before []sqlc.ListPoolAssignmentPhysicalBindingsRow) error {
	if len(before) == 0 {
		return nil
	}
	after, err := q.ListPoolAssignmentPhysicalBindings(ctx, selection)
	if err != nil {
		return err
	}
	current := make(map[int64]sqlc.ListPoolAssignmentPhysicalBindingsRow, len(after))
	for _, binding := range after {
		current[binding.InstallationID] = binding
	}
	retired := make([]int64, 0)
	for _, previous := range before {
		next, found := current[previous.InstallationID]
		if !found || !previous.Valid || !next.Valid || previous.PhysicalBinding != next.PhysicalBinding {
			retired = append(retired, previous.InstallationID)
		}
	}
	if len(retired) == 0 {
		return nil
	}
	return q.RetirePoolConfigurationEquipmentAssignments(ctx, sqlc.RetirePoolConfigurationEquipmentAssignmentsParams{OrganizationID: selection.OrganizationID, InstallationIds: retired})
}
