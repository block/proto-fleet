package poolconfigurations

import (
	"context"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

// SetDefault changes future lifecycle policy without modifying equipment.
func (r *Repository) SetDefault(ctx context.Context, q sqlc.Querier, scope Scope, id string, expectedVersion int64) error {
	if err := scope.validate(); err != nil {
		return err
	}
	if _, err := q.LockPoolConfigurationImportOrganization(ctx, scope.OrganizationID); err != nil {
		return storageError(err)
	}
	c, err := q.LockPoolConfiguration(ctx, sqlc.LockPoolConfigurationParams{OrganizationID: scope.OrganizationID, AccountID: scope.account(), ConfigurationID: id})
	if err != nil {
		return storageError(err)
	}
	if c.Archived || c.Version != expectedVersion {
		return fleeterror.NewFailedPreconditionError("configuration changed; refresh and try again")
	}
	if c.IsDefault {
		return nil
	}
	if err := q.ClearPoolConfigurationDefault(ctx, sqlc.ClearPoolConfigurationDefaultParams{OrganizationID: scope.OrganizationID, AccountID: scope.account()}); err != nil {
		return storageError(err)
	}
	return q.SetPoolConfigurationDefault(ctx, sqlc.SetPoolConfigurationDefaultParams{OrganizationID: scope.OrganizationID, AccountID: scope.account(), ConfigurationID: id})
}

func (s *Service) SetDefault(ctx context.Context, accountID, id string, version int64) error {
	_, err := withAuthority(ctx, s, accountID, Manage, func(q sqlc.Querier, p Principal) (struct{}, error) {
		if err := s.repository.SetDefault(ctx, q, p.Scope, id, version); err != nil {
			return struct{}{}, err
		}
		return struct{}{}, configurationAudit(ctx, q, p, "set_default_pool_configuration", id, "")
	})
	return err
}
