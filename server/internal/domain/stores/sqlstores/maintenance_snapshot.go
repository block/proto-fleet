package sqlstores

import (
	"context"
	"database/sql"
	"errors"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/maintenance/models"
)

// snapshotRepairTicketOpening runs with the ticket insert's transaction. Physical
// placement locks are held by the domain; allocation locks below retain the
// original owner and subowner until their target and audience rows commit.
func (s *SQLMaintenanceStore) snapshotRepairTicketOpening(ctx context.Context, params models.CreateParams, ticketID int64) error {
	q := s.GetQueries(ctx)
	audience := make(map[int64]struct{})
	if params.CreatedByAccountID != nil {
		audience[*params.CreatedByAccountID] = struct{}{}
	}
	targets := make([]sqlc.RepairTicketAssetOption, 0, len(params.Targets)+1)
	if params.MinerIdentifier != nil {
		target, err := q.ResolveRepairTicketMinerTarget(ctx, sqlc.ResolveRepairTicketMinerTargetParams{OrgID: params.OrgID, MinerIdentifier: *params.MinerIdentifier})
		if err != nil {
			return maintenanceWriteError("resolve opening miner", err)
		}
		targets = append(targets, target)
	}
	for _, selection := range params.Targets {
		target, err := q.ResolveRepairTicketAsset(ctx, sqlc.ResolveRepairTicketAssetParams{OrgID: params.OrgID, Kind: int16(selection.Kind), ID: selection.ID})
		if err != nil {
			return maintenanceWriteError("resolve opening target", err)
		}
		if selection.ParentModuleID != nil && (!target.ModuleID.Valid || target.ModuleID.Int64 != *selection.ParentModuleID) {
			return fleeterror.NewInvalidArgumentError("hashboard parent is not its current installation")
		}
		targets = append(targets, target)
	}
	for _, target := range targets {
		allocation, err := q.LockRepairTicketTargetAllocation(ctx, sqlc.LockRepairTicketTargetAllocationParams{OrgID: params.OrgID, AllocationKind: target.AllocationKind, AssetID: target.ParentAssetID})
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return maintenanceWriteError("lock opening target ownership", err)
		}
		for _, account := range []sql.NullInt64{allocation.ClientID, allocation.SubclientID} {
			if account.Valid {
				audience[account.Int64] = struct{}{}
			}
		}
		row := sqlc.InsertRepairTicketOpeningTargetParams{
			OrgID: params.OrgID, TicketID: ticketID, Kind: target.Kind, AssetID: target.ID,
			MinerIdentifier: ptrToNullString(nonEmptyStringPtr(target.MinerIdentifier)), BuildingID: target.BuildingID, RackID: target.RackID,
			ModuleID: target.ModuleID, InstallationID: target.InstallationID, ContainerID: target.ContainerID,
			SiteID: target.SiteID, Label: target.Label, Model: target.Model, Position: target.Position,
			ClientID: allocation.ClientID, SubclientID: allocation.SubclientID,
		}
		if models.TicketCategory(target.Kind) == models.TicketCategoryMiner {
			row.MinerID = sql.NullInt64{Int64: target.ID, Valid: true}
		} else if models.TicketCategory(target.Kind) == models.TicketCategoryHashboard {
			row.HashboardID = sql.NullInt64{Int64: target.ID, Valid: true}
		}
		if err := q.InsertRepairTicketOpeningTarget(ctx, row); err != nil {
			return maintenanceWriteError("insert opening target", err)
		}
	}
	accountIDs := make([]int64, 0, len(audience))
	for accountID := range audience {
		accountIDs = append(accountIDs, accountID)
	}
	if err := q.InsertRepairTicketOpeningAudience(ctx, sqlc.InsertRepairTicketOpeningAudienceParams{OrgID: params.OrgID, TicketID: ticketID, AccountIds: accountIDs}); err != nil {
		return maintenanceWriteError("insert opening audience", err)
	}
	return nil
}
