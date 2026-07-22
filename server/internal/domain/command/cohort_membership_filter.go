package command

import (
	"context"
	"fmt"
	"strings"

	"github.com/block/proto-fleet/server/internal/domain/cohort/models"
	"github.com/block/proto-fleet/server/internal/domain/session"
)

// CohortMembershipFilterName tags Skipped entries for leased cohort devices.
const CohortMembershipFilterName = "cohort_membership"

const cohortMembershipSkipReason = "device is leased by another cohort owner"

// CohortMembershipQuerier is the store surface needed by the lease filter.
type CohortMembershipQuerier interface {
	ListActiveOwnedCohortMemberships(ctx context.Context, orgID int64, deviceIdentifiers []string) ([]models.CohortDeviceOwnership, error)
}

// CohortMembershipFilter blocks external commands against devices leased by
// another user. Cohort enforcement self-traffic bypasses the gate.
type CohortMembershipFilter struct {
	querier CohortMembershipQuerier
}

func NewCohortMembershipFilter(querier CohortMembershipQuerier) *CohortMembershipFilter {
	return &CohortMembershipFilter{querier: querier}
}

func (f *CohortMembershipFilter) Name() string {
	return CohortMembershipFilterName
}

func (f *CohortMembershipFilter) Apply(ctx context.Context, in CommandFilterInput) (CommandFilterOutput, error) {
	if in.Actor == session.ActorCohort || len(in.DeviceIdentifiers) == 0 {
		return CommandFilterOutput{Kept: in.DeviceIdentifiers}, nil
	}

	rows, err := f.querier.ListActiveOwnedCohortMemberships(ctx, in.OrganizationID, in.DeviceIdentifiers)
	if err != nil {
		return CommandFilterOutput{}, fmt.Errorf("failed to list active owned cohort memberships: %w", err)
	}
	if len(rows) == 0 {
		return CommandFilterOutput{Kept: in.DeviceIdentifiers}, nil
	}

	locked := make(map[string]models.CohortDeviceOwnership, len(rows))
	for _, row := range rows {
		locked[row.DeviceIdentifier] = row
	}

	var kept []string
	var skipped []SkippedDevice
	for _, id := range in.DeviceIdentifiers {
		row, ok := locked[id]
		if !ok || row.OwnerUserID == nil || *row.OwnerUserID == in.UserID || commandFilterAdminRole(in.Role) {
			kept = append(kept, id)
			continue
		}
		skipped = append(skipped, SkippedDevice{
			DeviceIdentifier: id,
			FilterName:       f.Name(),
			Reason:           cohortMembershipSkipReason,
		})
	}
	return CommandFilterOutput{Kept: kept, Skipped: skipped}, nil
}

func commandFilterAdminRole(role string) bool {
	switch strings.ToUpper(strings.TrimSpace(role)) {
	case "SUPER_ADMIN", "ADMIN":
		return true
	default:
		return false
	}
}
