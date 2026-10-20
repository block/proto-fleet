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

// Shared across preview, acceptance and registration in every process. Each
// admitted check costs max(1, active invitations) units of exposure.
const (
	invitationAttemptLimit  = 1000
	invitationExposureLimit = 100000
)

func invitationGuessLimited() error {
	return fleeterror.NewResourceExhaustedErrorf("invitation checks temporarily limited; try again later")
}

func lockInvitationGuessBudget(ctx context.Context, q sqlc.Querier) error {
	_, err := q.LockHostedInvitationGuessBudget(ctx)
	return err
}

// admitInvitation commits the charge before reporting a missing digest. Failed
// attempts, rejected redemptions and later transaction rollbacks never refund it.
// Lookup occurs under the same lock as issuance: a newly issued code cannot be
// guessed using a charge based on the earlier, smaller active population.
func (s *InvitationService) admitInvitation(ctx context.Context, hash string) (sqlc.GetHostedAccountInvitationByHashRow, error) {
	initial, err := db.WithTransactionTimeout(ctx, s.accounts.conn, 2*time.Second, func(q sqlc.Querier) (sqlc.GetHostedAccountInvitationByHashRow, error) {
		var empty sqlc.GetHostedAccountInvitationByHashRow
		if err := lockInvitationGuessBudget(ctx, q); err != nil {
			return empty, err
		}
		if err := q.PruneHostedInvitationGuesses(ctx); err != nil {
			return empty, err
		}
		charged, err := q.ChargeHostedInvitationGuess(ctx, sqlc.ChargeHostedInvitationGuessParams{
			AttemptLimit: invitationAttemptLimit, ExposureLimit: invitationExposureLimit,
		})
		if err != nil {
			return empty, err
		}
		if charged != 1 {
			return empty, invitationGuessLimited()
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
