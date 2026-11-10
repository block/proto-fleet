package mininggroups

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

// MDK currently supplies activation prerequisites as text, including through
// Fleet Node's wrapped errors. Recognize its known reasons, but never publish
// arbitrary controller error text (which may contain private data).
func activationWaitingMessage(err error) string {
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "stop self mining before activating groups"),
		strings.Contains(message, "active boards to stop before activating groups"):
		return "Waiting for mining to stop. Activation will retry automatically."
	case strings.Contains(message, "factory run-in reservations to be released before activating groups"):
		return "Finish or cancel factory tests and release their board reservations. Activation will retry automatically."
	case strings.Contains(message, "has no fresh hosted-capable session"):
		return "A controller is offline or does not support mining groups. Reconnect it or update its firmware; activation will retry automatically."
	case strings.Contains(message, "ha owner ids are unavailable"),
		strings.Contains(message, "include every eligible owner"):
		return "Check which controllers are configured to manage this container. Activation will retry automatically."
	case strings.Contains(message, "the container power controller must be enabled before activating mining groups"):
		return "Enable the container's power controller. Activation will retry automatically."
	case strings.Contains(message, "release isolated boards before activation"):
		return "End isolated board maintenance on this container. Activation will retry automatically."
	default:
		return "The controller has not confirmed activation. Fleet will retry automatically."
	}
}

// The original running state is committed before any physical write. The stop
// is an idempotent desired-state command; retries never recapture its outcome
// as the original state. Activation and resume use the usual durable journal.
func (s *Service) reconcileActivation(ctx context.Context, q sqlc.Querier, row sqlc.ContainerMiningGroupState) error {
	beforeObservation := row
	capture, done, err := s.activationPoolCapture(ctx, q, row)
	if err == nil && capture.SnapshotID != "" {
		err = s.validateActivationPools(ctx, q, row, capture, done)
	}
	if err != nil {
		if fleeterror.IsFailedPreconditionError(err) || fleeterror.IsForbiddenError(err) {
			return s.interrupt(ctx, q, row, activationPoolRecovery)
		}
		return err
	}
	observed, err := s.call(ctx, row.DeviceID, sdk.MiningGroupRequest{Action: "observe"})
	if done && capture.SnapshotID != "" {
		readback := observed
		if err != nil {
			readback = sdk.MiningGroupResult{}
		}
		if saveErr := s.observeActivationPools(ctx, q, row, readback, capture); saveErr != nil {
			return saveErr
		}
	}
	if err != nil {
		return s.block(ctx, q, row, "Controller unavailable; waiting to refresh mining groups")
	}
	row.Observed, err = encode(observed)
	if err != nil {
		return err
	}
	row.ObservedAt = sql.NullTime{Time: time.Now().UTC(), Valid: true}
	if !observed.Supported {
		return s.block(ctx, q, row, "Controller firmware does not support mining groups")
	}
	if observed.Enabled {
		// A cached disabled observation at admission is not proof these boards
		// were present before an external activation. The original mining state
		// is captured only after this workflow freshly observes disabled mode.
		if !done && !row.ActivationResumeMining.Valid {
			return s.interrupt(ctx, q, row, activationPoolRecovery)
		}
		if row.Step.Valid {
			return s.apply(ctx, q, row, nil)
		}
		if observed.Pending || observed.Suspended {
			return s.block(ctx, q, row, "Waiting for mining-group activation to settle")
		}
		if !done {
			return &activationPoolHandoffReady{row: beforeObservation, observed: observed, observedAt: row.ObservedAt.Time}
		}
		if !row.ActivationResumeMining.Valid || !row.ActivationResumeMining.Bool {
			return s.complete(ctx, q, row)
		}
		for _, group := range observed.Groups {
			if group.ID != "default" {
				continue
			}
			if group.Mining {
				return s.complete(ctx, q, row)
			}
			return s.prepareStep(ctx, q, row, sdk.MiningGroupRequest{Action: "start", GroupID: "default", Revision: group.Revision}, "", "Resuming mining with the existing pools")
		}
		return s.block(ctx, q, row, "Waiting for the default mining group before resuming mining")
	}
	if !row.ActivationResumeMining.Valid {
		result, err := s.call(ctx, row.DeviceID, sdk.MiningGroupRequest{Action: "mining_status"})
		if err != nil || result.MiningStatus == nil {
			return s.block(ctx, q, row, "Waiting to confirm the current mining state before activation")
		}
		row.ActivationResumeMining = sql.NullBool{Bool: result.MiningStatus.Mining || result.MiningStatus.ActiveBoards > 0, Valid: true}
		row.Status = statusApplying
		row.Message = "Preparing to pause mining"
		return save(ctx, q, row)
	}
	if !row.ActivationStopRequested {
		var stopped sdk.MiningGroupResult
		err := devicewrite.Dispatch(ctx, func(ctx context.Context) error {
			var callErr error
			stopped, callErr = s.call(ctx, row.DeviceID, sdk.MiningGroupRequest{Action: "stop_legacy", Key: row.OperationID.UUID.String() + ":stop"})
			return callErr
		})
		if err != nil || !stopped.StopRequested {
			return s.block(ctx, q, row, "Waiting for the controller to accept the mining stop; retrying automatically")
		}
		row.ActivationStopRequested = true
		row.Status = statusApplying
		row.Message = "Waiting for mining to stop"
		return save(ctx, q, row)
	}
	result, err := s.call(ctx, row.DeviceID, sdk.MiningGroupRequest{Action: "mining_status"})
	if err != nil || result.MiningStatus == nil {
		return s.block(ctx, q, row, "Waiting for the controller to confirm mining has stopped")
	}
	if result.MiningStatus.Mining || result.MiningStatus.ActiveBoards > 0 {
		row.Status = statusApplying
		row.Message = "Waiting for mining to stop"
		return save(ctx, q, row)
	}
	if row.Step.Valid {
		return s.apply(ctx, q, row, nil)
	}
	return s.prepareStep(ctx, q, row, sdk.MiningGroupRequest{Action: "activate", Revision: observed.Revision}, "", "Enabling mining groups")
}
