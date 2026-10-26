package mininggroups

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
)

// The HA runtime owns execution. These fields only schedule retries; they never
// release write admission, replace a saved request, or expire controller work.
func (s *Service) reconcileScheduled(ctx context.Context, row sqlc.ListMiningGroupControllersRow) error {
	callCtx, cancel := context.WithTimeout(ctx, reconciliationTimeout)
	err := s.Reconcile(callCtx, row.OrganizationID, row.DeviceID, row.DeviceIdentifier)
	cancel()
	if ctx.Err() != nil {
		return err
	}
	// Use the active lifetime, not the expired attempt context, so timeouts and
	// transaction rollbacks also get backoff. HA shutdown cancels both contexts.
	recordCtx, stopRecord := context.WithTimeout(ctx, 5*time.Second)
	defer stopRecord()
	if recordErr := s.queries.ScheduleMiningGroupReconciliation(recordCtx, sqlc.ScheduleMiningGroupReconciliationParams{
		DeviceID: row.DeviceID, OrganizationID: row.OrganizationID, AttemptID: row.AttemptID,
		Failed: err != nil, FailureDelayMs: reconciliationFailureDelay(row.RetryCount).Milliseconds(),
	}); recordErr != nil {
		return errors.Join(err, fmt.Errorf("schedule mining group reconciliation: %w", recordErr))
	}
	return err
}

func reconciliationFailureDelay(failures int32) time.Duration {
	// The persisted count is bounded too. Clamp here before shifting so this
	// remains safe even if the schema's bound changes.
	ceiling := min(5*time.Second<<min(max(failures, 0), 7), 5*time.Minute)
	return ceiling*4/5 + time.Duration(rand.Int64N(int64(ceiling/5)+1)) //nolint:gosec // Scheduling jitter is not security-sensitive.
}
