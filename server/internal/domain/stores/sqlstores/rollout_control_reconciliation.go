package sqlstores

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/rollout"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
)

const controlReconciliationRollbackMessage = "strategy transaction definitively rolled back before durable work was created"

func (s *SQLRolloutStore) ListStartedControlCandidates(
	ctx context.Context,
	staleBefore time.Time,
	limit int32,
) ([]rollout.StartedControlCandidate, error) {
	rows, err := sqlc.New(s.conn).ListFirmwareRolloutStartedControlCandidates(
		ctx,
		sqlc.ListFirmwareRolloutStartedControlCandidatesParams{
			StaleBefore:    staleBefore,
			CandidateLimit: limit,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("list started firmware rollout controls: %w", err)
	}
	candidates := make([]rollout.StartedControlCandidate, 0, len(rows))
	for _, row := range rows {
		candidates = append(candidates, rollout.StartedControlCandidate{
			ID:        row.ID,
			RolloutID: row.RolloutID,
			OrgID:     row.OrgID,
			Operation: rollout.ControlOperation(row.Operation),
			UpdatedAt: row.UpdatedAt,
		})
	}
	return candidates, nil
}

func (s *SQLRolloutStore) ReconcileStartedControl(
	ctx context.Context,
	candidate rollout.StartedControlCandidate,
) (rollout.ControlReconciliationOutcome, error) {
	outcome, err := db.WithTransaction(
		ctx,
		s.conn,
		func(q sqlc.Querier) (rollout.ControlReconciliationOutcome, error) {
			if _, lockErr := q.LockFirmwareRollout(
				ctx,
				sqlc.LockFirmwareRolloutParams{
					RolloutID: candidate.RolloutID,
					OrgID:     candidate.OrgID,
				},
			); errors.Is(lockErr, sql.ErrNoRows) {
				return rollout.ControlReconciliationSettled, nil
			} else if lockErr != nil {
				return "", lockErr
			}
			control, getErr := q.GetFirmwareRolloutControl(
				ctx,
				sqlc.GetFirmwareRolloutControlParams{
					ControlID: candidate.ID,
					RolloutID: candidate.RolloutID,
					OrgID:     candidate.OrgID,
				},
			)
			if errors.Is(getErr, sql.ErrNoRows) {
				return rollout.ControlReconciliationSettled, nil
			}
			if getErr != nil {
				return "", getErr
			}
			if control.Status != string(rollout.ControlStatusStarted) {
				return rollout.ControlReconciliationSettled, nil
			}
			if control.Operation != string(candidate.Operation) {
				return "", rollout.ErrRevisionConflict
			}

			selected, durable, stateErr := controlReconciliationState(ctx, q, control)
			if stateErr != nil {
				return "", stateErr
			}
			if durable > 0 && durable < selected {
				return rollout.ControlReconciliationDeferred, nil
			}

			success := selected > 0 && durable == selected
			finishRequest := rollout.FinishControlRequest{
				OrgID:        candidate.OrgID,
				RolloutID:    candidate.RolloutID,
				ControlID:    candidate.ID,
				Success:      success,
				ErrorMessage: controlReconciliationRollbackMessage,
			}
			reconciled, finishErr := finishControl(ctx, q, finishRequest)
			if finishErr != nil {
				return "", finishErr
			}
			if reconciled.LaneID != nil {
				if _, releaseErr := releaseRolloutLaneActiveParentIfSettled(
					ctx,
					q,
					*reconciled.LaneID,
					candidate.OrgID,
				); releaseErr != nil {
					return "", releaseErr
				}
			}
			if success {
				return rollout.ControlReconciliationCommitted, nil
			}
			return rollout.ControlReconciliationRolledBack, nil
		},
	)
	if err != nil {
		return "", fmt.Errorf("reconcile started firmware rollout control: %w", err)
	}
	return outcome, nil
}

func controlReconciliationState(
	ctx context.Context,
	q sqlc.Querier,
	control sqlc.FirmwareRolloutControl,
) (int64, int64, error) {
	switch rollout.ControlOperation(control.Operation) {
	case rollout.ControlOperationAdmit, rollout.ControlOperationContinue:
		state, err := q.GetFirmwareRolloutAdmissionReconciliationState(
			ctx,
			sqlc.GetFirmwareRolloutAdmissionReconciliationStateParams{
				ControlID: control.ID,
				RolloutID: control.RolloutID,
				OrgID:     control.OrgID,
			},
		)
		return state.SelectedMembers, state.DurableMembers, err
	case rollout.ControlOperationRevert:
		state, err := q.GetFirmwareRolloutRevertReconciliationState(
			ctx,
			sqlc.GetFirmwareRolloutRevertReconciliationStateParams{
				ControlID: control.ID,
				RolloutID: control.RolloutID,
				OrgID:     control.OrgID,
			},
		)
		return state.SelectedMembers, state.DurableMembers, err
	case rollout.ControlOperationCreate,
		rollout.ControlOperationPause,
		rollout.ControlOperationResume,
		rollout.ControlOperationAbort,
		rollout.ControlOperationComplete:
		return 0, 0, rollout.ErrInvalidTransition
	}
	return 0, 0, rollout.ErrInvalidTransition
}
