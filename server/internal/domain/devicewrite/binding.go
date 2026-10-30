package devicewrite

import (
	"context"
	"fmt"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
)

// WithBindingTransaction shares one non-retrying transaction between placement
// validation and an adapter's binding gate. It is separate from ambient business
// transactions, and never changes the requirement for committed admission.
func (s *Service) WithBindingTransaction(ctx context.Context, call func(context.Context, sqlc.Querier) error) error {
	if binding, ok := ctx.Value(bindingQueriesContextKey{}).(bindingQueries); ok && binding.service.conn == s.conn {
		return call(ctx, binding.queries)
	}
	return db.WithDispatchTransaction(ctx, s.conn, func(q sqlc.Querier) error {
		return call(context.WithValue(ctx, bindingQueriesContextKey{}, bindingQueries{service: s, queries: q}), q)
	})
}

// RecheckIdentityAdmission permits a single-device dispatch to reuse its
// committed identity admission instead of retaining shared identity buckets
// during hardware I/O. Only the exact binding transaction and mutation scope
// qualify. Callers must retain the device's binding and fence locks and recheck
// after acquiring them, since identity or ownership can change while waiting.
// Ordinary reviews, batches, observations and deletion permits do not qualify.
func RecheckIdentityAdmission(ctx context.Context, q sqlc.Querier, organizationID int64, deviceIDs []int64) (bool, error) {
	attempt, ok := ctx.Value(attemptContextKey{}).(*Attempt)
	if !ok || len(deviceIDs) != 1 || attempt.deviceID != deviceIDs[0] || attempt.organizationID != organizationID ||
		attempt.observation || attempt.curtailmentCommand != "" || attempt.deletionBinding != "" {
		return false, nil
	}
	binding, ok := ctx.Value(bindingQueriesContextKey{}).(bindingQueries)
	if !ok || binding.service.conn != attempt.service.conn || binding.queries != q {
		return false, nil
	}
	if err := attempt.Recheck(ctx); err != nil {
		return false, err
	}
	return true, nil
}

// WithBindingDispatch locks every stored component of a physical route until
// the adapter returns. Admission has already committed on a separate connection.
// This transaction must never be retried or exposed as an ambient transaction:
// its callback performs external I/O. Cancellation must not release its locks
// while a context-ignoring adapter is still running.
func (s *Service) WithBindingDispatch(ctx context.Context, deviceID int64, validate func(sqlc.Querier) error, call func(context.Context) error) error {
	return s.withBindingDispatch(ctx, deviceID, true, validate, call)
}

// WithBindingObservation retains the binding without marking a physical mutation.
// Only a durably admitted observation may share the parent lock with other reads;
// a mutation attempt keeps its exclusive lock even during preliminary login.
func (s *Service) WithBindingObservation(ctx context.Context, deviceID int64, validate func(sqlc.Querier) error, call func(context.Context) error) error {
	return s.withBindingDispatch(ctx, deviceID, false, validate, call)
}

func (s *Service) withBindingDispatch(ctx context.Context, deviceID int64, mutation bool, validate func(sqlc.Querier) error, call func(context.Context) error) error {
	invoked := false
	err := s.WithBindingTransaction(ctx, func(ctx context.Context, q sqlc.Querier) error {
		// Choose the lock mode for the entire admitted attempt, never for an
		// individual login/read call: upgrading two read locks for subsequent
		// mutations could deadlock. Unclassified callers remain exclusive.
		attempt, admitted := ctx.Value(attemptContextKey{}).(*Attempt)
		var discoveryID int64
		var err error
		if admitted && attempt.service.conn == s.conn && attempt.deviceID == deviceID && attempt.observation {
			discoveryID, err = q.LockMinerBindingObservationDevice(ctx, deviceID)
		} else {
			discoveryID, err = q.LockMinerBindingDevice(ctx, deviceID)
		}
		if err != nil {
			return lookupError(err)
		}
		if _, err := q.LockMinerBindingDiscovery(ctx, discoveryID); err != nil {
			return lookupError(err)
		}
		if _, err := q.LockMinerBindingPairing(ctx, deviceID); err != nil {
			return err
		}
		if _, err := q.LockMinerBindingCredentials(ctx, deviceID); err != nil {
			return err
		}
		if _, err := q.LockMinerBindingRoutes(ctx, deviceID); err != nil {
			return err
		}
		if err := validate(q); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("dispatch binding canceled: %w", err)
		}
		return dispatch(ctx, func(ctx context.Context) error { invoked = true; return call(ctx) }, mutation)
	})
	if !invoked {
		return NotDispatched(err)
	}
	return err
}

type bindingQueriesContextKey struct{}
type bindingQueries struct {
	service *Service
	queries sqlc.Querier
}

func (s *Service) bindingQueries(ctx context.Context) sqlc.Querier {
	if binding, ok := ctx.Value(bindingQueriesContextKey{}).(bindingQueries); ok && binding.service.conn == s.conn {
		return binding.queries
	}
	return s.queries
}

// ValidateBinding reuses an enclosing dispatch connection, when one exists.
func (s *Service) ValidateBinding(ctx context.Context, validate func(sqlc.Querier) error) error {
	return validate(s.bindingQueries(ctx))
}
