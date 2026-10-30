package hostedaccounts

import (
	"context"
	"database/sql"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	"github.com/block/proto-fleet/server/internal/domain/session"
)

// PoolEquipmentVisibility applies the same site and physical-read authority as
// the equipment views. Allocation grants no pool-change permission.
func (s *PoolService) PoolEquipmentVisibility(ctx context.Context, q sqlc.Querier, p poolconfigurations.Principal) (func(sql.NullInt64) bool, error) {
	var estate miningEstate
	var err error
	if p.Scope.AccountID == 0 {
		estate.hostPermissions, err = authz.LoadEffectiveForShare(ctx, q, p.ActorID, p.Scope.OrganizationID)
	} else {
		estate, err = loadMiningEstate(ctx, q, p.ActorID, p.Scope.OrganizationID, p.AccountID, false, miningRead)
	}
	if err != nil {
		return nil, err
	}
	return func(site sql.NullInt64) bool {
		return siteAllowed(estate.policy, site.Int64) && estate.physicalRead(sqlc.ListHostedAccountMiningAssetsRow{SiteID: site.Int64})
	}, nil
}

// AuthorizePoolConfiguration keeps private account resolution out of the shared
// store. Selected-account requests can never fall back to the operator library.
func (s *PoolService) AuthorizePoolConfiguration(ctx context.Context, q sqlc.Querier, accountID string, access poolconfigurations.Access) (poolconfigurations.Principal, error) {
	info, err := session.GetInfo(ctx)
	if err != nil || info.Actor != "" || info.UserID <= 0 || info.OrganizationID <= 0 {
		return poolconfigurations.Principal{}, denied()
	}
	if access != poolconfigurations.Read && access != poolconfigurations.Manage {
		return poolconfigurations.Principal{}, denied()
	}
	selected, hasSelection := RequestAccountID(ctx)
	if (accountID == "" && (hasSelection || info.AccountKey)) || (accountID != "" && (!hasSelection || selected != accountID)) {
		return poolconfigurations.Principal{}, denied()
	}
	if accountID != "" && info.AuthMethod == session.AuthMethodAPIKey && !info.AccountKey {
		return poolconfigurations.Principal{}, denied()
	}
	var actor sqlc.User
	if access == poolconfigurations.Read {
		// Match equipment dispatch's shared admission so status reads can
		// proceed during I/O, while administration still excludes readers.
		if _, err = q.LockHostedAccountOrganizationForShare(ctx, info.OrganizationID); err == nil {
			actor, err = q.LockActiveHostedAccountUserForShare(ctx, sqlc.LockActiveHostedAccountUserForShareParams{ID: info.UserID, OrganizationID: info.OrganizationID})
		}
		if err != nil {
			return poolconfigurations.Principal{}, accessError(err)
		}
	} else {
		actor, err = lockActor(ctx, q, info.OrganizationID, info.UserID)
	}
	if err != nil {
		return poolconfigurations.Principal{}, err
	}
	principal := poolconfigurations.Principal{Scope: poolconfigurations.Scope{OrganizationID: info.OrganizationID}, ActorID: actor.ID, UserID: actor.UserID, Username: actor.Username}
	if accountID == "" {
		permissions, err := authz.LoadEffectiveForShare(ctx, q, info.UserID, info.OrganizationID)
		if err != nil {
			return poolconfigurations.Principal{}, err
		}
		permission := authz.PermPoolRead
		if access == poolconfigurations.Manage {
			permission = authz.PermPoolManage
		}
		if !permissions.Has(permission, authz.ResourceContext{}) {
			return poolconfigurations.Principal{}, denied()
		}
		return principal, nil
	}
	required := View
	if access == poolconfigurations.Manage {
		required = ManagePools
	}
	account, err := s.authorizePoolAccess(ctx, q, info.UserID, info.OrganizationID, accountID, required)
	if err != nil {
		return poolconfigurations.Principal{}, err
	}
	principal.Scope.AccountID = account.ID
	principal.AccountID, principal.AccountName = account.AccountID, account.Name
	return principal, nil
}
