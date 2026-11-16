package hostedaccounts

import (
	"context"
	"database/sql"
	"encoding/json"

	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
)

// ReplaceHashboard is the manual fallback for inventory replacement detection.
// Both leaf revisions and all destination policies are checked in one transaction.
func (s *Service) ReplaceHashboard(ctx context.Context, actorID, orgID int64, request *pb.ReplaceAccountHashboardRequest) error {
	if request == nil || request.OldBoard == nil || request.ReplacementBoard == nil ||
		request.OldBoard.Id <= 0 || request.ReplacementBoard.Id <= 0 || request.OldBoard.Id == request.ReplacementBoard.Id ||
		request.OldBoard.ExpectedRevision < 0 || request.ReplacementBoard.ExpectedRevision < 0 {
		return fleeterror.NewInvalidArgumentError("select an allocated board and a different replacement board")
	}
	ctx, cancel := context.WithTimeout(ctx, miningTransactionTimeout)
	defer cancel()
	return db.WithTransactionTimeoutNoResult(ctx, s.conn, miningTransactionTimeout, func(q sqlc.Querier) error {
		e, err := loadMiningEstate(ctx, q, actorID, orgID, request.AccountId, false, miningWrite)
		if err != nil {
			return err
		}
		if e.account.Depth != 0 || e.capabilities&Delegate == 0 {
			return denied()
		}
		encoded, err := json.Marshal([]*pb.MiningAssetReference{
			{Kind: pb.MiningAssetKind_MINING_ASSET_KIND_HASHBOARD, Id: request.OldBoard.Id},
			{Kind: pb.MiningAssetKind_MINING_ASSET_KIND_HASHBOARD, Id: request.ReplacementBoard.Id},
		})
		if err != nil {
			return fleeterror.NewInternalErrorf("encode replacement board selection: %w", err)
		}
		load := func() (sqlc.ListHostedAccountMiningAssetsRow, sqlc.ListHostedAccountMiningAssetsRow, error) {
			rows, err := q.ListHostedAccountMiningAssets(ctx, sqlc.ListHostedAccountMiningAssetsParams{OrganizationID: orgID, AccountInternalID: e.account.ID, IsRoot: true, SelectedAssets: encoded})
			var old, replacement sqlc.ListHostedAccountMiningAssetsRow
			if err != nil {
				return old, replacement, err
			}
			for _, row := range rows {
				if row.ID == request.OldBoard.Id {
					old = row
				}
				if row.ID == request.ReplacementBoard.Id {
					replacement = row
				}
			}
			if old.ID == 0 || replacement.ID == 0 || old.ClientID <= 0 || replacement.ClientID != 0 ||
				old.Revision != request.OldBoard.ExpectedRevision || replacement.Revision != request.ReplacementBoard.ExpectedRevision ||
				!e.physicalRead(old) || !e.physicalRead(replacement) || !replacement.Available {
				return old, replacement, assetConflict()
			}
			return old, replacement, nil
		}
		old, replacement, err := load()
		if err != nil {
			return err
		}
		oldDevice, replacementDevice := old.DeviceID, replacement.DeviceID
		if err := lockMiningDevices(ctx, q, []int64{old.DeviceID, replacement.DeviceID}); err != nil {
			return err
		}
		old, replacement, err = load()
		if err != nil {
			return err
		}
		if old.DeviceID != oldDevice || replacement.DeviceID != replacementDevice {
			return assetConflict()
		}
		if err := requireAllocationMiningGroups(ctx, q, orgID, []int64{replacement.DeviceID}); err != nil {
			return err
		}
		owner := old.OwnerAccountID
		path, policy, err := replacementOwnerPolicy(ctx, q, orgID, owner)
		if err != nil {
			return err
		}
		if !siteAllowed(policy, replacement.SiteID) {
			return fleeterror.NewInvalidArgumentError("replacement board is outside the allocated account's site policy")
		}
		if err := q.BeginHostedAccountAllocationChange(ctx, sqlc.BeginHostedAccountAllocationChangeParams{OrganizationID: orgID, SelectedAssets: encoded}); err != nil {
			return err
		}
		if _, err := q.SetHostedAccountAsset(ctx, sqlc.SetHostedAccountAssetParams{
			OrganizationID: orgID, Kind: int32(pb.MiningAssetKind_MINING_ASSET_KIND_HASHBOARD), AssetID: old.ID, DeviceID: old.DeviceID, HashboardID: sql.NullInt64{Int64: old.ID, Valid: true},
		}); err != nil {
			return err
		}
		if _, err := q.SetHostedAccountAsset(ctx, sqlc.SetHostedAccountAssetParams{
			OrganizationID: orgID, Kind: int32(pb.MiningAssetKind_MINING_ASSET_KIND_HASHBOARD), AssetID: replacement.ID, DeviceID: replacement.DeviceID, HashboardID: sql.NullInt64{Int64: replacement.ID, Valid: true},
			ClientID: sql.NullInt64{Int64: old.ClientID, Valid: true}, SubclientID: sql.NullInt64{Int64: old.SubclientID, Valid: old.SubclientID > 0},
		}); err != nil {
			return err
		}
		actor, err := q.LockActiveHostedAccountUser(ctx, sqlc.LockActiveHostedAccountUserParams{ID: actorID, OrganizationID: orgID})
		if err != nil {
			return err
		}
		return audit(ctx, q, actor, e.account, "account_hashboard_replaced", map[string]any{
			"child_account_id": path[1].AccountID, "allocated_account_id": owner, "asset_count": 1,
			"old_hashboard_id": old.ID, "replacement_hashboard_id": replacement.ID,
		})
	})
}

// Both candidate reads and the final transfer use the deepest owner's full path.
func replacementOwnerPolicy(ctx context.Context, q sqlc.Querier, orgID int64, owner string) ([]Account, Sites, error) {
	path, err := loadPath(ctx, q, orgID, owner)
	if err != nil {
		return nil, Sites{}, err
	}
	for _, account := range path {
		if account.Suspended {
			return nil, Sites{}, denied()
		}
	}
	policy, err := sitePolicy(ctx, q, orgID, path, false)
	return path, policy, err
}

func (e miningEstate) replacementCandidatePolicy(ctx context.Context, q sqlc.Querier, orgID int64, selection *pb.HashboardAllocationSelection) (Sites, error) {
	if e.account.Depth != 0 || e.capabilities&Delegate == 0 {
		return Sites{}, denied()
	}
	encoded, err := json.Marshal([]*pb.MiningAssetReference{{Kind: pb.MiningAssetKind_MINING_ASSET_KIND_HASHBOARD, Id: selection.Id}})
	if err != nil {
		return Sites{}, fleeterror.NewInternalErrorf("encode replacement source: %w", err)
	}
	rows, err := q.ListHostedAccountMiningAssets(ctx, sqlc.ListHostedAccountMiningAssetsParams{
		OrganizationID: orgID, AccountInternalID: e.account.ID, IsRoot: true, SelectedAssets: encoded,
	})
	if err != nil {
		return Sites{}, err
	}
	if len(rows) != 1 {
		return Sites{}, assetConflict()
	}
	board := rows[0]
	if board.ClientID <= 0 || board.Revision != selection.ExpectedRevision || !e.physicalRead(board) {
		return Sites{}, assetConflict()
	}
	_, policy, err := replacementOwnerPolicy(ctx, q, orgID, board.OwnerAccountID)
	return policy, err
}
