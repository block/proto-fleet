package mininggroups

import (
	"context"
	"database/sql"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

// Known idle containers need observations for new allocations as well as board
// replacement. This read never creates a workflow or changes controller mode.
func (s *Service) refreshAllocationReadiness(ctx context.Context, orgID, deviceID int64, identifier string) error {
	if err := s.queries.EnsureDeviceWriteFences(ctx, []int64{deviceID}); err != nil {
		return err
	}
	// Keep the retry schedule even if the first observation transaction rolls back.
	if err := s.queries.EnsureMiningGroupState(ctx, sqlc.EnsureMiningGroupStateParams{OrganizationID: orgID, DeviceID: deviceID}); err != nil {
		return err
	}
	target, err := s.queries.GetMiningGroupTarget(ctx, sqlc.GetMiningGroupTargetParams{OrgID: orgID, DeviceIdentifier: identifier})
	if err != nil {
		return err
	}
	return s.writes.WithHostObservation(ctx, orgID, deviceID, target.Epoch, func(ctx context.Context) error {
		return s.writes.WithExclusiveBindingObservation(ctx, deviceID, func(sqlc.Querier) error {
			return s.queries.MarkMiningGroupObservationPending(ctx, sqlc.MarkMiningGroupObservationPendingParams{OrganizationID: orgID, DeviceID: deviceID})
		}, func(ctx context.Context) error {
			return s.writes.WithBindingTransaction(ctx, func(ctx context.Context, q sqlc.Querier) error {
				row, err := q.LockMiningGroupState(ctx, sqlc.LockMiningGroupStateParams{OrganizationID: orgID, DeviceID: deviceID})
				if err != nil {
					return err
				}
				// Admission may have changed while this task waited for binding locks.
				if active(row.Status) {
					return nil
				}
				observed, err := s.call(ctx, deviceID, sdk.MiningGroupRequest{Action: "observe"})
				if err != nil {
					return err
				}
				row.Observed, err = encode(observed)
				if err != nil {
					return err
				}
				row.ObservedAt = sql.NullTime{Time: time.Now().UTC(), Valid: true}
				return save(ctx, q, row)
			})
		})
	})
}
