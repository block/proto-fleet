package rollout

import (
	"context"
	"log/slog"

	"github.com/block/proto-fleet/server/generated/sqlc"
)

// reconcileCompletedDispatches observes work that finished after its rollout
// stopped. It writes deployment evidence only: canceled or failed target phases,
// suppression and terminal lifecycle timestamps remain historical. The normal
// deployment trigger advances the revision when live evidence changes.
func (s *Service) reconcileCompletedDispatches(ctx context.Context) {
	rollouts, err := s.store.GetQueries(ctx).ListTerminalFirmwareRolloutsNeedingProvenance(ctx)
	if err != nil {
		slog.Error("rollout enforcement: list terminal deployments", "error", err)
		return
	}
	for _, r := range rollouts {
		if err := s.tx.RunInTx(ctx, func(ctx context.Context) error {
			if _, err := s.store.GetQueries(ctx).GetReleaseChannelForUpdate(ctx, sqlc.GetReleaseChannelForUpdateParams{ChannelID: r.ChannelID, OrgID: r.OrgID}); err != nil {
				return channelLookupError(r.ChannelID, err)
			}
			current, _, err := s.lockRollout(ctx, r.OrgID, r.ID, 0)
			if err != nil {
				return err
			}
			if current.Status == StatusActive {
				return nil
			}
			return s.reconcileTerminalProvenance(ctx, current)
		}); err != nil {
			slog.Error("rollout enforcement: reconcile terminal deployment", "rollout_id", r.ID, "error", err)
		}
	}
}

// reconcileTerminalProvenance requires the caller's channel and rollout locks.
// Retry also calls this before creating fresh work so an already successful
// canceled dispatch is not lost when the operator retries before the next tick.
func (s *Service) reconcileTerminalProvenance(ctx context.Context, r sqlc.FirmwareRollout) error {
	targets, err := s.listTargets(ctx, r)
	if err != nil {
		return err
	}
	_, err = s.recordTerminalProvenance(ctx, r, targets)
	return err
}
