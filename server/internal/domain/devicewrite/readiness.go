package devicewrite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/google/uuid"
)

type poolOperationKey struct{}

// WithPoolOperation identifies the queued operation being admitted by the pool worker.
// It permits only that operation's reservation, never another unresolved write.
func WithPoolOperation(ctx context.Context, id uuid.UUID) context.Context {
	return context.WithValue(ctx, poolOperationKey{}, id)
}

type mutationConflictError string

func (e mutationConflictError) Error() string { return string(e) }

// IsMutationConflict distinguishes unfinished work from invalid ownership or
// permissions. It does not establish that an admitted write is safe to retry.
func IsMutationConflict(err error) bool {
	var conflict mutationConflictError
	return errors.As(err, &conflict)
}

// RequireNoConflictingPoolWrites refuses pool changes while a conflicting
// physical write is unresolved or a conflicting command is still queued.
// An executor may retain its own committed admission; observations do not
// authorize mutations and may finish normally.
// The caller holds the device fence lock through its decision or dispatch.
func RequireNoConflictingPoolWrites(ctx context.Context, q sqlc.Querier, organizationID, deviceID int64) error {
	return RequireNoConflictingPoolWritesForRemoval(ctx, q, organizationID, deviceID, uuid.Nil)
}

// RequireNoConflictingPoolWritesForRemoval permits the caller's exact removal
// journal while still refusing every unresolved physical attempt except its own.
// Recovery uses this only to observe and finish the original removal.
func RequireNoConflictingPoolWritesForRemoval(ctx context.Context, q sqlc.Querier, organizationID, deviceID int64, removalID uuid.UUID) error {
	if err := RequireNoPendingActivationPoolHandoff(ctx, q, organizationID, deviceID); err != nil {
		return err
	}
	return RequireNoUnresolvedPoolWrites(ctx, q, organizationID, deviceID, removalID)
}

// RequireNoUnresolvedPoolWrites checks physical work independently of retained
// activation intent. Only assignment-only recovery may use this without also
// checking the pending activation fence; it must authorize that recovery itself.
func RequireNoUnresolvedPoolWrites(ctx context.Context, q sqlc.Querier, organizationID, deviceID int64, removalID uuid.UUID) error {
	attempts, err := q.ListUnresolvedDeviceWriteAttempts(ctx, sqlc.ListUnresolvedDeviceWriteAttemptsParams{OrganizationID: organizationID, DeviceID: deviceID})
	if err != nil {
		return err
	}
	current, _ := ctx.Value(attemptContextKey{}).(*Attempt)
	removals, err := q.ListActivePoolRemovals(ctx, sqlc.ListActivePoolRemovalsParams{OrganizationID: organizationID, DeviceID: deviceID})
	if err != nil {
		return err
	}
	for _, removal := range removals {
		if removal.RemovalID == removalID || current != nil && removal.PhysicalAttemptID.Valid && removal.PhysicalAttemptID.UUID == current.id {
			continue
		}
		return fleeterror.NewFailedPreconditionErrorf("%w", mutationConflictError("pool removal is unfinished; check its result before another pool change"))
	}
	for _, attempt := range attempts {
		if !MayAffectPools(attempt.OperationKind, attempt.CommandType) {
			continue
		}
		if current != nil && current.id == attempt.AttemptID && current.organizationID == organizationID && current.deviceID == deviceID {
			continue
		}
		return fleeterror.NewFailedPreconditionErrorf("%w", mutationConflictError("device write is fenced by an unresolved configuration operation"))
	}
	// Queue messages have no physical attempt until BeginQueuedHost. Check
	// both pending and claimed messages while retaining the fence that also
	// serializes enqueue and physical admission. Never cancel existing work.
	commands, err := q.ListUnfinishedDeviceWriteCommands(ctx, sqlc.ListUnfinishedDeviceWriteCommandsParams{OrganizationID: organizationID, DeviceID: deviceID})
	if err != nil {
		return err
	}
	for _, command := range commands {
		if MayAffectPools(operationMutation, command) {
			return fleeterror.NewFailedPreconditionErrorf("%w", mutationConflictError("equipment has an unfinished configuration command; wait for it to finish, then review the pool update again"))
		}
	}
	return nil
}

// MayAffectPools treats unclassified work as conflicting. Only explicitly
// independent commands may overlap pool configuration. Curtailment can change
// fallback pools; lifecycle, credential and mining-group changes affect dispatch
// or group reconciliation even when they do not directly edit a pool URL.
func MayAffectPools(operationKind, commandName string) bool {
	if operationKind == operationObservation {
		return false
	}
	kind, err := commandtype.FromString(commandName)
	if err != nil {
		return true
	}
	return kind.MayAffectPools()
}

// requireNoActivePoolRemoval also protects legacy commands and mining-group
// workflows, which do not use the pool configuration review service.
func requireNoActivePoolRemoval(ctx context.Context, q sqlc.Querier, organizationID, deviceID int64) error {
	return requirePoolRemovalAdmission(ctx, q, organizationID, deviceID, commandtype.Type(-1), nil)
}

func requirePoolRemovalAdmission(ctx context.Context, q sqlc.Querier, organizationID, deviceID int64, kind commandtype.Type, payload []byte) error {
	nativeCurtailment := false
	if kind.IsCurtailmentControl() {
		binding, err := q.GetPoolConfigurationRuntime(ctx, sqlc.GetPoolConfigurationRuntimeParams{OrgID: organizationID, ID: deviceID})
		if err != nil {
			return err
		}
		// Proto's native controls do not replace pools. Recheck the current
		// driver at admission and dispatch rather than trusting queue payloads.
		nativeCurtailment = binding.DriverName == "proto"
	}
	if !nativeCurtailment {
		if kind.MayAffectPools() {
			if err := requireActivationPoolCommand(ctx, q, organizationID, deviceID, kind, payload); err != nil {
				return err
			}
		}
		if err := RequireNoMiningGroupDeactivation(ctx, q, organizationID, deviceID); err != nil {
			return err
		}
	}
	rows, err := q.ListActivePoolRemovals(ctx, sqlc.ListActivePoolRemovalsParams{OrganizationID: organizationID, DeviceID: deviceID})
	if err != nil {
		return err
	}
	own, _ := ctx.Value(poolOperationKey{}).(uuid.UUID)
	conflicting := false
	for _, row := range rows {
		if row.RemovalID != own {
			conflicting = true
		}
	}
	if conflicting {
		if nativeCurtailment {
			return nil
		}
		return fleeterror.NewFailedPreconditionError("pool removal is unfinished; check its result before another device change")
	}
	return nil
}

const ActivationPoolPlacementPending = "Mining groups are enabled. The container pool assignment is retained until all hashboard locations are confirmed. Pool changes are blocked. To cancel the transfer, remove the container assignment and keep device settings."

// This fence outlives the activation workflow. It protects retained intent
// while identity is known but assignment placement is still unavailable.
func RequireNoPendingActivationPoolHandoff(ctx context.Context, q sqlc.Querier, organizationID, deviceID int64) error {
	pending, err := q.HasPendingMiningGroupPoolHandoff(ctx, sqlc.HasPendingMiningGroupPoolHandoffParams{OrganizationID: organizationID, DeviceID: deviceID})
	if err != nil {
		return err
	}
	if pending {
		return fleeterror.NewFailedPreconditionError(ActivationPoolPlacementPending)
	}
	return nil
}

// Call under the device fence before enqueueing or admitting a mutation. An
// accepted deactivation reserves mode changes until its exact journal settles.
func RequireNoMiningGroupDeactivation(ctx context.Context, q sqlc.Querier, organizationID, deviceID int64) error {
	row, err := q.GetMiningGroupState(ctx, sqlc.GetMiningGroupStateParams{OrganizationID: organizationID, DeviceID: deviceID})
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if row.OperationKind != "deactivate" || (row.Status != "pending" && row.Status != "applying" && row.Status != "blocked") {
		return nil
	}
	current, _ := ctx.Value(attemptContextKey{}).(*Attempt)
	if current != nil && row.AttemptID.Valid && current.id == row.AttemptID.UUID {
		return nil
	}
	return fleeterror.NewFailedPreconditionError("Mining groups are being disabled. Wait for this change or recovery to finish before submitting device changes.")
}

// Placement alone must not prevent an operator from stopping or rebooting.
// Keep this separate from MayAffectPools: these commands still conflict with
// physical pool writes and must not interrupt an unfinished activation.
func requireActivationPoolCommand(ctx context.Context, q sqlc.Querier, organizationID, deviceID int64, kind commandtype.Type, payload []byte) error {
	pending, err := q.HasPendingMiningGroupPoolHandoff(ctx, sqlc.HasPendingMiningGroupPoolHandoffParams{OrganizationID: organizationID, DeviceID: deviceID})
	if err != nil || !pending {
		return err
	}
	allowed := kind == commandtype.StopMining || kind == commandtype.Reboot
	if kind == commandtype.ContainerMining {
		// Decode the same request envelope used by the queue executor. ContainerMining
		// also carries start and power changes, which do not get this exception.
		var command struct {
			Request sdk.ContainerMiningRequest `json:"request"`
		}
		allowed = json.Unmarshal(payload, &command) == nil && command.Request.Validate() == nil && command.Request.Action == "set_mining" && !*command.Request.Mining
	}
	if !allowed {
		return fleeterror.NewFailedPreconditionError(ActivationPoolPlacementPending)
	}
	state, err := q.GetMiningGroupState(ctx, sqlc.GetMiningGroupStateParams{OrganizationID: organizationID, DeviceID: deviceID})
	if err != nil {
		return err
	}
	if state.OperationKind != "activate" || state.Status != "complete" {
		return fleeterror.NewFailedPreconditionError("Mining group activation is unfinished. Wait for activation or recovery to finish before stopping or rebooting.")
	}
	attempts, err := q.ListUnresolvedDeviceWriteAttempts(ctx, sqlc.ListUnresolvedDeviceWriteAttemptsParams{OrganizationID: organizationID, DeviceID: deviceID})
	if err != nil {
		return err
	}
	for _, attempt := range attempts {
		if MayAffectPools(attempt.OperationKind, attempt.CommandType) {
			return fleeterror.NewFailedPreconditionErrorf("%w", mutationConflictError("device write is fenced by an unresolved configuration operation"))
		}
	}
	return nil
}
