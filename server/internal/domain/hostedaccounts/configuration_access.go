package hostedaccounts

import (
	"context"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	"github.com/block/proto-fleet/server/internal/domain/session"
)

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
	actor, err := lockActor(ctx, q, info.OrganizationID, info.UserID)
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
