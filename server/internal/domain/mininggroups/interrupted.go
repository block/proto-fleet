package mininggroups

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/google/uuid"
	"github.com/sqlc-dev/pqtype"
)

func finishStep(ctx context.Context, q sqlc.Querier, row sqlc.ContainerMiningGroupState) error {
	row.Step = pqtype.NullRawMessage{}
	row.StepKey = uuid.NullUUID{}
	row.ControllerOperationID = ""
	row.Status = statusApplying
	row.Message = "Verifying controller configuration"
	return save(ctx, q, row)
}

func (s *Service) pollTransfer(ctx context.Context, q sqlc.Querier, row sqlc.ContainerMiningGroupState) error {
	request := sdk.MiningGroupRequest{Action: "operation", GroupID: row.ControllerOperationID}
	response, err := s.call(ctx, row.DeviceID, request)
	if err != nil || response.Validate(request) != nil {
		return s.block(ctx, q, row, "Controller has not confirmed the transfer outcome; waiting for its receipt")
	}
	return s.recordTransfer(ctx, q, row, response.Operation)
}

func (s *Service) recordTransfer(ctx context.Context, q sqlc.Querier, row sqlc.ContainerMiningGroupState, operation *sdk.MiningGroupOperation) error {
	if operation == nil || operation.ID == "" {
		return s.block(ctx, q, row, "Waiting for a transfer receipt")
	}
	row.ControllerOperationID = operation.ID
	if operation.Phase == transferPhaseFailed {
		// Only a terminal receipt retires the identity. The next pass observes
		// ownership before retrying or settling an interrupted operation.
		row.TransferFailureCount++
		row.Step = pqtype.NullRawMessage{}
		row.StepKey = uuid.NullUUID{}
		row.ControllerOperationID = ""
		return s.block(ctx, q, row, "Controller transfer failed; refreshing board assignments before recovery")
	}
	if operation.Phase != transferPhaseComplete {
		row.Status = statusApplying
		row.Message = fmt.Sprintf("Waiting for board shutdown confirmation (%d boards blocked)", len(operation.BlockedMACs))
		return save(ctx, q, row)
	}
	return finishStep(ctx, q, row)
}

// interrupt never dispatches a mutation. It can discard a request only when
// durable dispatch tracking proves it never left Fleet. Unknown outcomes retain
// their identity and admission; known transfers settle through receipt polling.
func (s *Service) interrupt(ctx context.Context, q sqlc.Querier, row sqlc.ContainerMiningGroupState, message string) error {
	if row.Step.Valid {
		unsent, err := q.MiningGroupStepIsUnsent(ctx, sqlc.MiningGroupStepIsUnsentParams{StepKey: row.StepKey.UUID, AttemptID: row.AttemptID.UUID})
		if err != nil {
			return err
		}
		if !unsent {
			if row.OperationKind == operationDeactivate {
				return s.block(ctx, q, row, "Operator access changed. Resume disabling mining groups to continue.")
			}
			return s.block(ctx, q, row, "Access or board installation changed; the saved controller request must be resolved before this operation can end")
		}
		row.Step = pqtype.NullRawMessage{}
		row.StepKey = uuid.NullUUID{}
		row.ControllerOperationID = ""
	}
	// The legacy stop predates the saved activation step. A missing stop receipt
	// may mean the call was accepted, so neither revocation nor timeout releases it.
	if row.OperationKind == operationActivate && row.ActivationResumeMining.Valid && !row.ActivationStopRequested {
		return s.block(ctx, q, row, "Operator access changed; the earlier mining stop has not been resolved")
	}
	observed, err := s.call(ctx, row.DeviceID, sdk.MiningGroupRequest{Action: "observe"})
	if err != nil || !observed.Supported || observed.Pending {
		return s.block(ctx, q, row, "Waiting for confirmed controller state before ending the interrupted operation")
	}
	if row.OperationKind == operationActivate && row.ActivationStopRequested && !observed.Enabled {
		status, err := s.call(ctx, row.DeviceID, sdk.MiningGroupRequest{Action: "mining_status"})
		if err != nil || status.MiningStatus == nil || status.MiningStatus.Mining || status.MiningStatus.ActiveBoards > 0 {
			return s.block(ctx, q, row, "Waiting for mining to stop before ending the interrupted activation")
		}
	}
	row.Observed, err = encode(observed)
	if err != nil {
		return err
	}
	row.ObservedAt = sql.NullTime{Time: time.Now().UTC(), Valid: true}
	if row.OperationKind == operationDeactivate && !observed.Enabled {
		if observed.Suspended {
			return s.block(ctx, q, row, "Waiting for the controller to settle before ending deactivation")
		}
		// A settled disabled controller no longer has these group identities,
		// even when authority changed after its deactivation receipt committed.
		row.Bindings = []byte(`{}`)
	}
	if row.OperationKind == operationReturnDefault {
		return s.finishDefaultReturn(ctx, q, row, false, message)
	}
	snapshot, err := s.plannerSnapshot(ctx, q, row, observed)
	if err != nil {
		return s.block(ctx, q, row, "Pool configuration unavailable; waiting to verify applied assignments")
	}
	// Cleanup and admission release commit together. Pruning earlier could erase
	// the evidence of interruption while an observation or transfer is pending.
	if err := pruneDetachedIntents(ctx, q, row.OrganizationID, row.DeviceID); err != nil {
		return err
	}
	cutover, err := q.HasPoolConfigurationCutover(ctx, row.OrganizationID)
	if err != nil {
		return err
	}
	desired := map[string]string{}
	if !cutover {
		intents, err := q.ListHashboardPoolIntents(ctx, sqlc.ListHashboardPoolIntentsParams{OrganizationID: row.OrganizationID, DeviceID: row.DeviceID})
		if err != nil {
			return err
		}
		for _, intent := range intents {
			desired[intent.MacAddress] = intent.PoolRevisionID
		}
	}
	intents, err := q.ListPoolGroupIntents(ctx, sqlc.ListPoolGroupIntentsParams{OrganizationID: row.OrganizationID, DeviceID: row.DeviceID})
	if err != nil {
		return err
	}
	for _, intent := range intents {
		desired[intent.MacAddress] = intent.GroupSnapshotID.String
	}
	return s.failOperation(ctx, q, row, desired, snapshot, message)
}
