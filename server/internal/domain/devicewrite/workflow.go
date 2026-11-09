package devicewrite

import (
	"context"
	"database/sql"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/block/proto-fleet/server/internal/runtimepolicy"
	"github.com/google/uuid"
)

// BeginWorkflow commits a durable physical admission together with the caller's
// journal. Unlike a single RPC, this admission stays running across restarts
// until the journal verifies the controller's terminal outcome. The record
// callback is database-only: device I/O must wait until admission commits.
func (s *Service) BeginWorkflow(ctx context.Context, orgID, deviceID int64, record func(context.Context, sqlc.Querier, uuid.UUID, int64) error) error {
	return db.WithTransactionTimeoutNoResult(ctx, s.conn, runtimepolicy.CommandTransactionBound, func(q sqlc.Querier) error {
		keys, err := lockIdentity(ctx, q, deviceID)
		if err != nil {
			return err
		}
		fence, err := lockFence(ctx, q, orgID, deviceID)
		if err != nil {
			return err
		}
		if fence.State != "host" {
			return blocked()
		}
		if err := requireNoActivePoolRemoval(ctx, q, orgID, deviceID); err != nil {
			return err
		}
		id := uuid.New()
		if err := q.CreateDeviceWriteAttempt(ctx, sqlc.CreateDeviceWriteAttemptParams{AttemptID: id, DeviceID: deviceID, OrganizationID: orgID, Epoch: fence.Epoch, IdentityKeys: keys, OwnerInstanceID: processInstanceID, OperationKind: operationMutation, CommandType: "MiningGroups"}); err != nil {
			return err
		}
		admission := &Attempt{service: s, id: id, deviceID: deviceID, organizationID: orgID, epoch: fence.Epoch}
		return record(admission.Context(ctx), q, id, fence.Epoch)
	})
}

// ResumeWorkflow only restores the admission linked to an active mining-group
// journal. A recovered or fenced attempt can never resume dispatch.
func (s *Service) ResumeWorkflow(ctx context.Context, orgID, deviceID int64, id uuid.UUID) (*Attempt, error) {
	row, err := s.queries.GetMiningGroupWorkflowAttempt(ctx, sqlc.GetMiningGroupWorkflowAttemptParams{AttemptID: id, DeviceID: deviceID, OrganizationID: orgID})
	if err != nil {
		return nil, lookupError(err)
	}
	a := &Attempt{service: s, id: id, deviceID: deviceID, organizationID: orgID, epoch: row.Epoch}
	if err := a.Recheck(ctx); err != nil {
		return nil, err
	}
	return a, nil
}

// CompleteWorkflow is called within the journal's transaction, only after an
// authoritative observation confirms all mutations are settled, including async
// transfers. A terminal failed workflow can release admission without claiming
// its requested configuration succeeded; the journal retains that outcome.
func CompleteWorkflow(ctx context.Context, q sqlc.Querier, id uuid.UUID) error {
	n, err := q.FinishDeviceWriteAttempt(ctx, sqlc.FinishDeviceWriteAttemptParams{AttemptID: id, Status: statusComplete, WriteOutcome: []byte("{}")})
	if err != nil {
		return err
	}
	if n != 1 {
		return sql.ErrNoRows
	}
	return nil
}
