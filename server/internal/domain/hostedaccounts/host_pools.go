package hostedaccounts

import (
	"context"
	"strings"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/google/uuid"
)

// Host profiles use the same encrypted, immutable revision store as customer
// profiles. These explicit operator methods do not grant root account customer
// membership and cannot read another account's configurations.
func (s *PoolService) CreateHost(ctx context.Context, userID, orgID int64, options PoolRevisionOptions) (PoolRevision, error) {
	options, err := validatePoolOptions(options)
	if err != nil {
		return PoolRevision{}, err
	}
	for _, p := range options.Slots {
		if !strings.HasPrefix(p.URL, "stratum+tcp://") || p.WorkerTemplate != "" {
			return PoolRevision{}, fleeterror.NewInvalidArgumentError("mining groups require TCP Stratum V1 and a fixed worker username")
		}
	}
	return db.WithTransaction(ctx, s.accounts.conn, func(q sqlc.Querier) (PoolRevision, error) {
		perms, err := authz.LoadEffectiveForUpdate(ctx, q, userID, orgID)
		if err != nil {
			return PoolRevision{}, err
		}
		if !perms.Has(authz.PermPoolManage, authz.ResourceContext{}) {
			return PoolRevision{}, fleeterror.NewForbiddenError("pool management permission required")
		}
		account, err := q.GetHostPoolAccount(ctx, orgID)
		if err != nil {
			return PoolRevision{}, err
		}
		slots, err := s.encryptSlots(options.Slots, nil)
		if err != nil {
			return PoolRevision{}, err
		}
		pool, err := q.CreateHostedAccountPool(ctx, sqlc.CreateHostedAccountPoolParams{OrganizationID: orgID, AccountID: account.ID, PoolID: uuid.NewString()})
		if err != nil {
			return PoolRevision{}, err
		}
		revision, err := q.CreateHostedAccountPoolRevision(ctx, sqlc.CreateHostedAccountPoolRevisionParams{RevisionID: uuid.NewString(), OrganizationID: orgID, AccountID: account.ID, PoolID: pool.PoolID, Number: 1, Name: options.Name, Slots: slots})
		if err != nil {
			return PoolRevision{}, err
		}
		actor, err := q.GetUserById(ctx, userID)
		if err != nil {
			return PoolRevision{}, err
		}
		if err := audit(ctx, q, actor, account, "create_hosted_account_pool", map[string]any{"pool_id": pool.PoolID, "revision_id": revision.RevisionID, "revision": revision.Number, "slot_count": len(options.Slots)}); err != nil {
			return PoolRevision{}, err
		}
		return publicPoolRevision(revision, false)
	})
}
func (s *PoolService) ListHost(ctx context.Context, userID, orgID int64) ([]PoolRevision, error) {
	perms, err := authz.LoadEffectiveTx(ctx, s.accounts.queries, userID, orgID)
	if err != nil {
		return nil, err
	}
	if !perms.Has(authz.PermPoolRead, authz.ResourceContext{}) {
		return nil, fleeterror.NewForbiddenError("pool read permission required")
	}
	rows, err := s.accounts.queries.ListHostMiningGroupPoolRevisions(ctx, orgID)
	if err != nil {
		return nil, err
	}
	out := make([]PoolRevision, 0, len(rows))
	for _, r := range rows {
		v, err := publicPoolRevision(r, false)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// HostGroupCredentials is private dispatch material, never an API response.
// Callers must authorize the container and profile before calling it.
// Archived revisions remain readable for reconciliation. New assignments must
// first pass the active-only GetHostMiningGroupPoolRevision lookup.
func (s *PoolService) HostGroupCredentials(ctx context.Context, q sqlc.Querier, orgID int64, revisionID string) ([]sdk.MiningGroupPoolInput, error) {
	r, err := q.GetHostMiningGroupPoolRevisionForReconciliation(ctx, sqlc.GetHostMiningGroupPoolRevisionForReconciliationParams{OrganizationID: orgID, RevisionID: revisionID})
	if err != nil {
		return nil, err
	}
	slots, err := decodePoolSlots(r.Slots)
	if err != nil {
		return nil, err
	}
	out := make([]sdk.MiningGroupPoolInput, 0, len(slots))
	for _, slot := range slots {
		if slot.WorkerTemplate != "" {
			return nil, fleeterror.NewFailedPreconditionError("mining groups require fixed worker usernames")
		}
		password, err := s.secrets.Decrypt(slot.PasswordCiphertext)
		if err != nil {
			return nil, err
		}
		out = append(out, sdk.MiningGroupPoolInput{URL: slot.URL, Username: slot.Username, Password: string(password)})
	}
	rqt := sdk.MiningGroupRequest{Action: "replace_pools", GroupID: "validation", Revision: 1, Key: "validation", Pools: out}
	return out, rqt.Validate()
}
