package hostedaccounts

import (
	"context"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/infrastructure/encrypt"
)

type CredentialsVerifier interface {
	VerifySessionCredentials(ctx context.Context, username, password string) error
}

type PoolService struct {
	accounts *Service
	secrets  *encrypt.Service
	keys     *KeyService
	verifier CredentialsVerifier
}

func NewPoolService(accounts *Service, secrets *encrypt.Service, keys *KeyService, verifier CredentialsVerifier) *PoolService {
	return &PoolService{accounts: accounts, secrets: secrets, keys: keys, verifier: verifier}
}

// authorizePoolAccess is the same live authority check for cheap admission and
// locked mutation. It performs no actor/organization locking on its own.
func (s *PoolService) authorizePoolAccess(ctx context.Context, q sqlc.Querier, actorID, orgID int64, accountID string, required Capabilities) (Account, error) {
	path, err := loadPath(ctx, q, orgID, accountID)
	if err != nil {
		return Account{}, err
	}
	caps, err := authorize(ctx, q, actorID, orgID, path, required)
	if err != nil {
		return Account{}, err
	}
	account := path[len(path)-1]
	info, infoErr := session.GetInfo(ctx)
	scope, scoped := ctx.Value(machineScopeKey{}).(machineScope)
	if infoErr == nil && (info.UserID != actorID || info.OrganizationID != orgID) {
		return Account{}, denied()
	}
	if scoped || (infoErr == nil && info.AccountKey) {
		if infoErr != nil || !info.AccountKey || info.AuthMethod != session.AuthMethodAPIKey || info.APIKeyDatabaseID <= 0 || s.keys == nil || !scoped || scope.accountID != account.AccountID {
			return Account{}, denied()
		}
		fresh, err := s.keys.authorize(ctx, q, info.APIKeyDatabaseID, actorID, orgID, account.AccountID)
		if err != nil {
			return Account{}, err
		}
		if fresh&caps&scope.capabilities&required != required {
			return Account{}, denied()
		}
	}
	return account, nil
}
