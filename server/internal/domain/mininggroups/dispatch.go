package mininggroups

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/poolapplications"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/google/uuid"
)

type stepDispatchError struct {
	message string
	err     error
}

func (e *stepDispatchError) Error() string { return e.err.Error() }
func (e *stepDispatchError) Unwrap() error { return e.err }

func (s *Service) dispatchStep(ctx context.Context, row sqlc.ContainerMiningGroupState, request sdk.MiningGroupRequest) (sdk.MiningGroupResult, error) {
	var response sdk.MiningGroupResult
	var marked, wasUnsent bool
	token := uuid.New()
	err := devicewrite.Dispatch(ctx, func(ctx context.Context) error {
		// Persist outside the binding transaction, after its admission checks.
		// Its rollback must not erase evidence of an attempted controller call.
		var err error
		wasUnsent, err = s.queries.MarkMiningGroupStepDispatch(ctx, sqlc.MarkMiningGroupStepDispatchParams{StepKey: row.StepKey.UUID, AttemptID: row.AttemptID.UUID, DispatchToken: token})
		if err != nil {
			return devicewrite.NotDispatched(&stepDispatchError{message: "Cannot send the request yet. Fleet will retry.", err: fmt.Errorf("mark mining group dispatch: %w", err)})
		}
		marked = true
		response, err = s.call(ctx, row.DeviceID, request)
		return err
	})
	if err == nil || !marked || !wasUnsent || !devicewrite.IsNotDispatched(err) {
		return response, err
	}
	// A SQL connection can disappear while an adapter still runs. The token
	// prevents this invocation's refusal from clearing a newer worker's send.
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	restored, restoreErr := s.queries.RestoreMiningGroupStepUnsent(cleanupCtx, sqlc.RestoreMiningGroupStepUnsentParams{StepKey: row.StepKey.UUID, AttemptID: row.AttemptID.UUID, DispatchToken: token})
	if restoreErr != nil {
		return response, &stepDispatchError{message: "Cannot confirm request delivery. Mining-group changes are paused.", err: errors.Join(err, fmt.Errorf("restore unsent mining group step: %w", restoreErr))}
	}
	if restored != 1 {
		return response, &stepDispatchError{message: "Waiting for the controller to confirm this request.", err: errors.Join(err, fmt.Errorf("mining group dispatch token changed before restore"))}
	}
	return response, err
}

// The outer transaction commits the blocked message before returning this
// diagnostic to the scheduler, which retains normal error backoff and logging.
type recordedWorkflowError struct{ err error }

func (e *recordedWorkflowError) Error() string { return e.err.Error() }
func (e *recordedWorkflowError) Unwrap() error { return e.err }

func (s *Service) blockWithDiagnostic(ctx context.Context, q sqlc.Querier, row sqlc.ContainerMiningGroupState, message string, diagnostic error) error {
	if err := s.block(ctx, q, row, message); err != nil {
		return errors.Join(diagnostic, err)
	}
	return &recordedWorkflowError{err: diagnostic}
}

func (s *Service) handleBoardValidation(ctx context.Context, q sqlc.Querier, row sqlc.ContainerMiningGroupState, err error) error {
	if isWorkflowChanged(err) || poolapplications.IsBindingChanged(err) || fleeterror.IsForbiddenError(err) {
		return s.interrupt(ctx, q, row, err.Error())
	}
	if isInventoryStale(err) || fleeterror.IsFailedPreconditionError(err) {
		return s.block(ctx, q, row, err.Error())
	}
	return err
}
