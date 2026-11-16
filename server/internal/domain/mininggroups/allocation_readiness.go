package mininggroups

import (
	"context"
	"database/sql"
	"errors"
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
	var previousObservation *sqlc.ContainerMiningGroupState
	err = s.writes.WithHostObservation(ctx, orgID, deviceID, target.Epoch, func(ctx context.Context) error {
		return s.writes.WithBindingObservation(ctx, deviceID, func(sqlc.Querier) error { return nil }, func(ctx context.Context) error {
			return s.writes.WithBindingTransaction(ctx, func(ctx context.Context, q sqlc.Querier) error {
				row, err := q.LockMiningGroupState(ctx, sqlc.LockMiningGroupStateParams{OrganizationID: orgID, DeviceID: deviceID})
				if err != nil {
					return err
				}
				// Admission may have changed while this task waited for binding locks.
				if active(row.Status) {
					return nil
				}
				before := row
				previousObservation = &before
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
	if previousObservation != nil {
		err = errors.Join(err, s.invalidateFailedObservation(ctx, *previousObservation))
	}
	return err
}

// The read may have exhausted its context or aborted its binding transaction.
// Finalize on a fresh connection after locks unwind, with a bounded lifetime.
// Matching the captured timestamp protects a later successful observation.
// Run after every attempted read: a committed refresh advances the timestamp,
// making this a no-op. A failed save or commit still invalidates the old cache.
func (s *Service) invalidateFailedObservation(ctx context.Context, row sqlc.ContainerMiningGroupState) error {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return s.queries.InvalidateMiningGroupObservation(cleanupCtx, sqlc.InvalidateMiningGroupObservationParams{
		OrganizationID: row.OrganizationID, DeviceID: row.DeviceID, ObservedAt: row.ObservedAt,
	})
}
