package devicewrite

import (
	"context"
	"errors"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

type mutationConflictError string

func (e mutationConflictError) Error() string { return string(e) }

// IsMutationConflict distinguishes unfinished work from invalid ownership or
// permissions. It does not establish that an admitted write is safe to retry.
func IsMutationConflict(err error) bool {
	var conflict mutationConflictError
	return errors.As(err, &conflict)
}

// RequireNoOtherMutations refuses a new configuration while another physical
// write has no terminal outcome or a conflicting command is still queued.
// An executor may retain its own committed admission; observations do not
// authorize mutations and may finish normally.
// The caller holds the device fence lock through its decision or dispatch.
func RequireNoOtherMutations(ctx context.Context, q sqlc.Querier, organizationID, deviceID int64) error {
	attempts, err := q.ListUnresolvedDeviceWriteAttempts(ctx, sqlc.ListUnresolvedDeviceWriteAttemptsParams{OrganizationID: organizationID, DeviceID: deviceID})
	if err != nil {
		return err
	}
	current, _ := ctx.Value(attemptContextKey{}).(*Attempt)
	for _, attempt := range attempts {
		if attempt.OperationKind == operationObservation {
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
	switch kind {
	case commandtype.BlinkLED, commandtype.DownloadLogs, commandtype.SetCoolingMode, commandtype.SetPowerTarget:
		return false
	case commandtype.StartMining, commandtype.StopMining, commandtype.UpdateMiningPools,
		commandtype.Reboot, commandtype.FirmwareUpdate, commandtype.Unpair,
		commandtype.UpdateMinerPassword, commandtype.Curtail, commandtype.Uncurtail,
		commandtype.ApplyCurtailmentConfig, commandtype.SetContainerControl,
		commandtype.SetTankMining, commandtype.SetModuleMining:
		return true
	default:
		return true
	}
}
