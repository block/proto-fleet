package hostedaccounts

import (
	"context"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
)

func (s *PoolService) VerifyPoolEquipmentCredentials(ctx context.Context, accountID, password string) (poolconfigurations.CredentialProof, error) {
	info, err := session.GetInfo(ctx)
	if err != nil || info.AuthMethod != session.AuthMethodSession || info.AccountKey || info.Actor != "" {
		return nil, denied()
	}
	credential, err := db.WithTransaction(ctx, s.accounts.conn, func(q sqlc.Querier) (sqlc.User, error) {
		p, err := s.AuthorizePoolConfiguration(ctx, q, accountID, poolconfigurations.Read)
		if err != nil {
			return sqlc.User{}, err
		}
		if accountID != "" {
			if _, err := s.authorizePoolAccess(ctx, q, p.ActorID, p.Scope.OrganizationID, accountID, View|ChangePools); err != nil {
				return sqlc.User{}, err
			}
		}
		return q.GetUserById(ctx, p.ActorID)
	})
	if err != nil {
		return nil, err
	}
	if s.verifier == nil {
		return nil, fleeterror.NewInternalError("credential verification is unavailable")
	}
	if err := s.verifier.VerifySessionCredentials(ctx, credential.Username, password); err != nil {
		return nil, err
	}
	return func(ctx context.Context, q sqlc.Querier) error {
		current, err := lockActor(ctx, q, info.OrganizationID, info.UserID)
		if err != nil {
			return err
		}
		if current.PasswordHash != credential.PasswordHash || current.Username != credential.Username || current.PasswordUpdatedAt != credential.PasswordUpdatedAt {
			return fleeterror.NewForbiddenError("credentials changed; verify again")
		}
		return nil
	}, nil
}
