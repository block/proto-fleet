package hostedaccounts

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
)

const (
	invitationAttemptLimit  = 1000
	invitationExposureLimit = 100000
)

func lockInvitationRateLimit(ctx context.Context, q sqlc.Querier) error {
	_, err := q.LockHostedInvitationRateLimit(ctx)
	return err
}

// lookupInvitation commits the charge before reporting a missing digest. Failed
// attempts, rejected redemptions and later transaction rollbacks never refund it.
// Lookup occurs under the same lock as issuance: a newly issued code cannot be
// guessed using a charge based on the earlier, smaller active population.
func (s *InvitationService) lookupInvitation(ctx context.Context, hash string) (sqlc.GetHostedAccountInvitationByHashRow, error) {
	initial, err := db.WithTransactionTimeout(ctx, s.accounts.conn, 2*time.Second, func(q sqlc.Querier) (sqlc.GetHostedAccountInvitationByHashRow, error) {
		var empty sqlc.GetHostedAccountInvitationByHashRow
		if err := lockInvitationRateLimit(ctx, q); err != nil {
			return empty, err
		}
		charged, err := q.ConsumeHostedInvitationRateLimit(ctx, sqlc.ConsumeHostedInvitationRateLimitParams{
			AttemptLimit: invitationAttemptLimit, ExposureLimit: invitationExposureLimit,
		})
		if err != nil {
			return empty, err
		}
		if charged != 1 {
			return empty, fleeterror.NewResourceExhaustedErrorf("invitation checks temporarily limited; try again later")
		}
		initial, err := q.GetHostedAccountInvitationByHash(ctx, hash)
		if errors.Is(err, sql.ErrNoRows) {
			return empty, nil
		}
		return initial, err
	})
	if err != nil {
		return initial, err
	}
	if initial.InvitationID == "" {
		return initial, unavailableInvitation()
	}
	return initial, nil
}
