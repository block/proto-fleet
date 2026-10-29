package hostedaccounts

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/google/uuid"
)

// ResolveHostFleetPools snapshots saved Fleet pools for board assignments. Pool
// IDs and their update versions define the identity, so separate saved pools
// never merge merely because their settings happen to match.
func (s *PoolService) ResolveHostFleetPools(ctx context.Context, userID, orgID int64, ids []int64) (PoolRevision, error) {
	if len(ids) == 0 || len(ids) > 3 {
		return PoolRevision{}, fleeterror.NewInvalidArgumentError("select one to three pools")
	}
	return db.WithTransaction(ctx, s.accounts.conn, func(q sqlc.Querier) (PoolRevision, error) {
		// Serialize snapshot creation across operators using the existing account lock.
		if _, err := q.LockHostedAccountOrganization(ctx, orgID); err != nil {
			return PoolRevision{}, err
		}
		perms, err := authz.LoadEffectiveForShare(ctx, q, userID, orgID)
		if err != nil {
			return PoolRevision{}, err
		}
		if !perms.Has(authz.PermPoolRead, authz.ResourceContext{}) {
			return PoolRevision{}, fleeterror.NewForbiddenError("pool read permission required")
		}
		account, err := q.GetHostPoolAccount(ctx, orgID)
		if err != nil {
			return PoolRevision{}, err
		}
		type source struct {
			ID        int64
			UpdatedAt time.Time
		}
		sources := []source{}
		options := poolconfigurations.RevisionOptions{Name: "Fleet pool assignment"}
		inputs := []sdk.MiningGroupPoolInput{}
		seen := map[int64]bool{}
		for _, id := range ids {
			if id <= 0 || seen[id] {
				return PoolRevision{}, fleeterror.NewInvalidArgumentError("select unique saved pools")
			}
			seen[id] = true
			pool, err := q.GetPool(ctx, sqlc.GetPoolParams{OrgID: orgID, ID: id})
			if errors.Is(err, sql.ErrNoRows) {
				return PoolRevision{}, fleeterror.NewNotFoundError("pool not found")
			}
			if err != nil {
				return PoolRevision{}, err
			}
			password := ""
			if pool.PasswordEnc != "" {
				plain, err := s.secrets.Decrypt(pool.PasswordEnc)
				if err != nil {
					return PoolRevision{}, err
				}
				password = string(plain)
			}
			sources = append(sources, source{ID: pool.ID, UpdatedAt: pool.UpdatedAt})
			options.Slots = append(options.Slots, poolconfigurations.SlotOptions{URL: pool.Url, Username: pool.Username, Password: &password})
			inputs = append(inputs, sdk.MiningGroupPoolInput{URL: pool.Url, Username: pool.Username, Password: password})
		}
		if err := (sdk.MiningGroupRequest{Action: "replace_pools", GroupID: "validation", Revision: 1, Key: "validation", Pools: inputs}).Validate(); err != nil {
			return PoolRevision{}, fleeterror.NewInvalidArgumentError("mining groups require TCP Stratum V1 pools with fixed worker usernames")
		}
		identity, err := json.Marshal(struct {
			Organization int64
			Sources      []source
		}{orgID, sources})
		if err != nil {
			return PoolRevision{}, fmt.Errorf("encode saved pool snapshot identity: %w", err)
		}
		id := uuid.NewSHA1(uuid.NameSpaceURL, append([]byte("fleet-mining-group-pools:"), identity...)).String()
		existing, err := q.GetHostMiningGroupPoolRevision(ctx, sqlc.GetHostMiningGroupPoolRevisionParams{OrganizationID: orgID, RevisionID: id})
		if err == nil {
			return publicPoolRevision(existing, false)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return PoolRevision{}, err
		}
		slots, err := poolconfigurations.EncryptSlots(s.secrets, options.Slots, nil)
		if err != nil {
			return PoolRevision{}, err
		}
		pool, err := q.CreateHostedAccountPool(ctx, sqlc.CreateHostedAccountPoolParams{OrganizationID: orgID, AccountID: account.ID, PoolID: id})
		if err != nil {
			return PoolRevision{}, err
		}
		revision, err := q.CreateHostedAccountPoolRevision(ctx, sqlc.CreateHostedAccountPoolRevisionParams{RevisionID: id, OrganizationID: orgID, AccountID: account.ID, PoolID: pool.PoolID, Number: 1, Name: options.Name, Slots: slots})
		if err != nil {
			return PoolRevision{}, err
		}
		return publicPoolRevision(revision, false)
	})
}
