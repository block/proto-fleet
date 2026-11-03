package poolconfigurations

import (
	"context"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

// BoardSelection expands modules once, to their current physical installations.
// Neither future occupants nor a second reference to the same board add targets.
type BoardSelection struct{ ModuleIDs, HashboardIDs, DeviceIDs []int64 }

func (selection BoardSelection) validate() error {
	if len(selection.ModuleIDs)+len(selection.HashboardIDs) == 0 || len(selection.ModuleIDs) > 1000 || len(selection.HashboardIDs) > 3000 || len(selection.DeviceIDs) > 10000 {
		return fleeterror.NewInvalidArgumentError("select modules or hashboards")
	}
	for _, ids := range [][]int64{selection.ModuleIDs, selection.HashboardIDs, selection.DeviceIDs} {
		seen := map[int64]bool{}
		for _, id := range ids {
			if id <= 0 || seen[id] {
				return fleeterror.NewInvalidArgumentError("select unique valid equipment")
			}
			seen[id] = true
		}
	}
	return nil
}

func resolveBoards(ctx context.Context, q sqlc.Querier, p Principal, selection BoardSelection, fresh bool) ([]sqlc.ListPoolSelectionBoardsRow, error) {
	if err := selection.validate(); err != nil {
		return nil, err
	}
	modules, err := q.ListPoolSelectionModules(ctx, sqlc.ListPoolSelectionModulesParams{OrgID: p.Scope.OrganizationID, ModuleIds: selection.ModuleIDs})
	if err != nil {
		return nil, storageError(err)
	}
	if len(modules) != len(selection.ModuleIDs) {
		return nil, fleeterror.NewForbiddenError("selected modules are unavailable")
	}
	for _, module := range modules {
		if fresh && !module.Ready {
			if !module.IdentityConfirmed {
				return nil, fleeterror.NewFailedPreconditionErrorf("module %d has an unconfirmed identity or location; check the controller's module mapping before assigning pools", module.ID)
			}
			return nil, fleeterror.NewFailedPreconditionErrorf("module %d has stale or incomplete inventory; wait for a complete inventory update before assigning pools", module.ID)
		}
	}
	rows, err := q.ListPoolSelectionBoards(ctx, sqlc.ListPoolSelectionBoardsParams{OrgID: p.Scope.OrganizationID, ModuleIds: selection.ModuleIDs, HashboardIds: selection.HashboardIDs})
	if err != nil {
		return nil, storageError(err)
	}
	found := map[int64]bool{}
	for _, row := range rows {
		found[row.HashboardID] = true
		if fresh && !row.Ready {
			return nil, fleeterror.NewFailedPreconditionError("selected boards changed or their inventory is stale")
		}
	}
	for _, id := range selection.HashboardIDs {
		if !found[id] {
			return nil, fleeterror.NewFailedPreconditionError("selected board installation is unavailable")
		}
	}
	if fresh && len(rows) == 0 {
		return nil, fleeterror.NewFailedPreconditionError("selected modules have no installed hashboards")
	}
	return rows, nil
}

func (s *Service) ReviewBoards(ctx context.Context, accountID, id string, version int64, selection BoardSelection) (Review, error) {
	targets, err := withAuthority(ctx, s, accountID, Read, func(q sqlc.Querier, p Principal) ([]AssignmentTarget, error) {
		rows, err := resolveBoards(ctx, q, p, selection, true)
		if err != nil {
			return nil, err
		}
		targets := make([]AssignmentTarget, 0, len(rows)+len(selection.DeviceIDs))
		for _, row := range rows {
			targets = append(targets, AssignmentTarget{Kind: TargetInstallation, ID: row.InstallationID, DeviceID: row.DeviceID})
		}
		for _, id := range selection.DeviceIDs {
			targets = append(targets, AssignmentTarget{Kind: TargetDevice, ID: id, DeviceID: id})
		}
		return targets, nil
	})
	if err != nil {
		return Review{}, err
	}
	return s.ReviewAssignment(ctx, accountID, id, version, targets)
}

type BoardAssignments struct {
	Assignments   []sqlc.ListPoolBoardAssignmentsRow
	SelectedCount int64
}

func (s *Service) GetBoardAssignments(ctx context.Context, accountID string, selection BoardSelection) (BoardAssignments, error) {
	return withAuthority(ctx, s, accountID, Read, func(q sqlc.Querier, p Principal) (BoardAssignments, error) {
		rows, err := resolveBoards(ctx, q, p, selection, false)
		if err != nil {
			return BoardAssignments{}, err
		}
		visible, err := s.authority.PoolEquipmentVisibility(ctx, q, p)
		if err != nil {
			return BoardAssignments{}, err
		}
		ids := make([]int64, 0, len(rows))
		for _, row := range rows {
			if row.AccountID == p.Scope.AccountID && visible(row.SiteID) {
				ids = append(ids, row.InstallationID)
			}
		}
		assignments, err := q.ListPoolBoardAssignments(ctx, sqlc.ListPoolBoardAssignmentsParams{OrganizationID: p.Scope.OrganizationID, AccountID: p.Scope.account(), InstallationIds: ids})
		if err != nil {
			return BoardAssignments{}, storageError(err)
		}
		byID := map[int64]bool{}
		for _, a := range assignments {
			byID[a.InstallationID] = true
		}
		for _, row := range rows {
			if row.AccountID == p.Scope.AccountID && visible(row.SiteID) && !byID[row.InstallationID] {
				assignments = append(assignments, sqlc.ListPoolBoardAssignmentsRow{State: "unavailable", InstallationID: row.InstallationID, HashboardID: row.HashboardID, ModuleID: row.ModuleID, DeviceID: row.DeviceID, DeviceIdentifier: row.DeviceIdentifier, SiteID: row.SiteID})
			}
		}
		return BoardAssignments{Assignments: assignments, SelectedCount: int64(len(ids))}, nil
	})
}
