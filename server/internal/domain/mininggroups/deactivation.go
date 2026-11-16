package mininggroups

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

type deactivationReadiness struct {
	Reason             string
	CanReturnToDefault bool
}

// Informational reads use the same rules as admission. Mutation callers retain
// organization, binding and fence locks until their decision has committed.
func deactivationBlocker(ctx context.Context, q sqlc.Querier, row sqlc.ContainerMiningGroupState, observed sdk.MiningGroupResult, ownWorkflow bool) (deactivationReadiness, error) {
	reason, err := q.GetMiningGroupDeactivationBlocker(ctx, sqlc.GetMiningGroupDeactivationBlockerParams{OrganizationID: row.OrganizationID, DeviceID: row.DeviceID, IgnoreDeferred: ownWorkflow})
	if err != nil || reason != "" {
		return deactivationReadiness{Reason: reason}, err
	}
	if active(row.Status) && !ownWorkflow {
		return deactivationReadiness{Reason: "Wait for the mining-group change or recovery to finish before disabling mining groups."}, nil
	}
	if _, err := q.GetBlockingFrigateUpdateJobForDevice(ctx, row.DeviceID); err == nil {
		return deactivationReadiness{Reason: "Wait for the Frigate software update to finish or cancel it before disabling mining groups."}, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return deactivationReadiness{}, err
	}
	if err := devicewrite.RequireNoConflictingPoolWrites(ctx, q, row.OrganizationID, row.DeviceID); err != nil {
		if fleeterror.IsFailedPreconditionError(err) {
			return deactivationReadiness{Reason: "Wait for pending device changes or resolve uncertain writes before disabling mining groups."}, nil
		}
		return deactivationReadiness{}, err
	}
	if !row.ObservedAt.Valid || time.Since(row.ObservedAt.Time) > 2*time.Minute || time.Until(row.ObservedAt.Time) > 5*time.Second || !observed.Supported {
		return deactivationReadiness{Reason: "Reconnect the controller and refresh its mining-group state before disabling mining groups."}, nil
	}
	if observed.Pending || observed.Suspended {
		return deactivationReadiness{Reason: "Wait for controller mining-group work and recovery to finish before disabling mining groups."}, nil
	}
	for _, group := range observed.Groups {
		if group.ID != "default" && len(group.MACs) > 0 {
			return deactivationReadiness{Reason: "Move hashboards to the default group before disabling mining groups.", CanReturnToDefault: true}, nil
		}
	}
	return deactivationReadiness{}, nil
}

func (s *Service) reconcileDeactivation(ctx context.Context, q sqlc.Querier, row sqlc.ContainerMiningGroupState) error {
	// A saved request may already have reached the controller. Replay its exact
	// identity rather than treating a later observation as a mutation receipt.
	observed, err := s.call(ctx, row.DeviceID, sdk.MiningGroupRequest{Action: "observe"})
	if err != nil {
		return s.block(ctx, q, row, "Controller unavailable; waiting to confirm mining-group deactivation")
	}
	row.Observed, err = encode(observed)
	if err != nil {
		return err
	}
	row.ObservedAt = sql.NullTime{Time: time.Now().UTC(), Valid: true}
	reason, err := deactivationBlocker(ctx, q, row, observed, true)
	if err != nil {
		return err
	}
	if reason.Reason != "" {
		return s.block(ctx, q, row, reason.Reason)
	}
	if row.Step.Valid {
		return s.apply(ctx, q, row, nil)
	}
	if !observed.Enabled {
		// Disabled mode ends these controller group identities. Keep the saved
		// request until its receipt settles, then release their live bindings.
		row.Bindings = []byte(`{}`)
		return s.complete(ctx, q, row)
	}
	return s.prepareStep(ctx, q, row, sdk.MiningGroupRequest{Action: operationDeactivate, Revision: observed.Revision}, "", "Disabling mining groups")
}
