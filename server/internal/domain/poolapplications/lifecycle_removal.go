package poolapplications

import (
	"context"
	"encoding/json"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

func (c *Checker) lifecycleRemovalBinding(ctx context.Context, q sqlc.Querier, p poolconfigurations.Principal, target poolconfigurations.AssignmentTarget) (Binding, error) {
	var binding Binding
	if _, err := poolconfigurations.AuthorizeLifecycleTransition(ctx, q, p.Scope, target.DeviceID); err != nil {
		return binding, err
	}
	if err := lockPoolBindings(ctx, q, p.Scope, []poolconfigurations.AssignmentTarget{target}, true); err != nil {
		return binding, err
	}
	if err := devicewrite.RequireNoConflictingPoolWritesForRemoval(ctx, q, p.Scope.OrganizationID, target.DeviceID, poolconfigurations.LifecycleTransitionID(ctx)); err != nil {
		return binding, err
	}
	frozen, err := q.PoolConfigurationWritersFrozen(ctx, p.Scope.OrganizationID)
	if err != nil {
		return binding, err
	}
	if frozen {
		return binding, fleeterror.NewFailedPreconditionError("pool configuration maintenance is in progress")
	}
	owner, err := c.reviewer.authority.AuthorizePoolEquipment(ctx, q, p.Scope, 0, target)
	if err != nil {
		return binding, err
	}
	device, err := q.GetPoolConfigurationRuntime(ctx, sqlc.GetPoolConfigurationRuntimeParams{OrgID: p.Scope.OrganizationID, ID: target.DeviceID})
	if err != nil {
		return binding, err
	}
	if device.State != "host" {
		return binding, fleeterror.NewFailedPreconditionError("equipment is reserved for another operation")
	}
	caps := c.reviewer.capabilities.GetRawCapabilitiesForDevice(ctx, device.DriverName, device.Manufacturer, device.Model)
	if target.Kind == poolconfigurations.TargetDevice && !caps[sdk.CapabilityPoolClear] || target.Kind == poolconfigurations.TargetInstallation && !caps[sdk.CapabilityMiningGroupPoolClear] {
		return binding, fleeterror.NewUnimplementedError("device does not support pool clearing")
	}
	binding = Binding{Version: 1, Ownership: owner, Device: device}
	if target.Kind == poolconfigurations.TargetInstallation {
		if commonpb.DeviceClass(device.DeviceClass) != commonpb.DeviceClass_DEVICE_CLASS_CONTAINER {
			return binding, fleeterror.NewFailedPreconditionError("board clearing requires a container")
		}
		boards, err := q.ListPoolSelectionBoards(ctx, sqlc.ListPoolSelectionBoardsParams{OrgID: p.Scope.OrganizationID, InstallationIds: []int64{target.ID}})
		if err != nil {
			return binding, err
		}
		if len(boards) != 1 || !boards[0].Ready || boards[0].DeviceID != target.DeviceID {
			return binding, fleeterror.NewFailedPreconditionError("board installation changed")
		}
		b := boards[0]
		binding.Board = &BoardBinding{InstallationID: b.InstallationID, HashboardID: b.HashboardID, ModuleID: b.ModuleID, Position: b.Position, MAC: b.MacAddress}
	}
	return binding, nil
}

func (c *Checker) validateLifecycleRemoval(ctx context.Context, q sqlc.Querier, p poolconfigurations.Principal, row sqlc.GetPoolRemovalRow) error {
	current, err := poolconfigurations.AuthorizeLifecycleTransition(ctx, q, p.Scope, row.DeviceID)
	if err != nil {
		return err
	}
	if current.TransitionID != row.RemovalID || current.ConfigurationID.Valid || current.TargetKind != row.TargetKind || current.TargetID != row.TargetID || current.ReviewedBinding != row.ReviewedBinding {
		return newBindingChangedError("pool lifecycle intent changed")
	}
	binding, err := c.lifecycleRemovalBinding(ctx, q, p, poolconfigurations.AssignmentTarget{Kind: row.TargetKind, ID: row.TargetID, DeviceID: row.DeviceID})
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(binding)
	if err != nil {
		return fleeterror.NewInternalError("encode lifecycle binding failed")
	}
	if string(encoded) != row.ReviewedBinding {
		return newBindingChangedError("pool lifecycle binding changed")
	}
	return nil
}
