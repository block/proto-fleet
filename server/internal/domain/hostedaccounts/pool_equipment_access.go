package hostedaccounts

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/google/uuid"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	"github.com/block/proto-fleet/server/internal/domain/session"
)

// AuthorizePoolEquipment checks the actor independently of equipment allocation.
// Call inside the review/confirmation transaction and again inside the physical
// dispatch transaction. The caller retains organization, device and inventory
// locks through its work. The returned ownership snapshot detects recall and
// reallocation even when the equipment eventually returns to the same account.
// This does not check firmware capabilities, mining-group readiness or step-up.
func (s *PoolService) AuthorizePoolEquipment(ctx context.Context, q sqlc.Querier, scope poolconfigurations.Scope, actorID int64, target poolconfigurations.AssignmentTarget) (string, error) {
	access, err := s.poolEquipmentAccess(ctx, q, scope, actorID, []poolconfigurations.AssignmentTarget{target})
	if err != nil {
		return "", err
	}
	return access.authorize(scope, target)
}

// AuthorizePoolEquipmentBatch shares authorization reads only within this
// controller and transaction. Missing entries are denied, never inherited from
// another target on the same controller.
func (s *PoolService) AuthorizePoolEquipmentBatch(ctx context.Context, q sqlc.Querier, scope poolconfigurations.Scope, actorID int64, targets []poolconfigurations.AssignmentTarget) (map[int]string, error) {
	access, err := s.poolEquipmentAccess(ctx, q, scope, actorID, targets)
	if err != nil {
		return nil, err
	}
	bindings := make(map[int]string, len(targets))
	for i, target := range targets {
		binding, err := access.authorize(scope, target)
		if err == nil {
			bindings[i] = binding
		}
	}
	return bindings, nil
}

type poolEquipmentAccess struct {
	device        sqlc.GetPoolEquipmentDeviceRow
	host          *authz.EffectivePermissions
	owners        []sqlc.ListPoolEquipmentModuleOwnersRow
	ownersByBoard map[[2]int64][]sqlc.ListPoolEquipmentModuleOwnersRow
	installations map[int64]sqlc.ListPoolEquipmentInstallationsRow
}

func (s *PoolService) poolEquipmentAccess(ctx context.Context, q sqlc.Querier, scope poolconfigurations.Scope, actorID int64, targets []poolconfigurations.AssignmentTarget) (*poolEquipmentAccess, error) {
	var policy Sites
	var host *authz.EffectivePermissions
	var err error
	if len(targets) == 0 {
		return nil, fleeterror.NewInvalidArgumentError("select equipment")
	}
	deviceID := targets[0].DeviceID
	var installationIDs []int64
	for _, target := range targets {
		if target.ID <= 0 || deviceID <= 0 || target.DeviceID != deviceID || (target.Kind != poolconfigurations.TargetDevice && target.Kind != poolconfigurations.TargetInstallation) || (target.Kind == poolconfigurations.TargetDevice && target.ID != deviceID) {
			return nil, fleeterror.NewInvalidArgumentError("invalid pool assignment target")
		}
		if target.Kind == poolconfigurations.TargetInstallation {
			installationIDs = append(installationIDs, target.ID)
		}
	}
	if poolconfigurations.LifecycleTransitionID(ctx) != uuid.Nil {
		if actorID != 0 {
			return nil, denied()
		}
		if _, err := poolconfigurations.AuthorizeLifecycleTransition(ctx, q, scope, deviceID); err != nil {
			return nil, err
		}
	} else {
		info, err := session.GetInfo(ctx)
		if err != nil || info.Actor != "" || info.UserID != actorID || actorID <= 0 || info.OrganizationID != scope.OrganizationID || info.AuthMethod != session.AuthMethodSession || info.AccountKey {
			return nil, denied()
		}
		selected, hasSelection := RequestAccountID(ctx)
		if scope.AccountID < 0 || (scope.AccountID == 0 && hasSelection) || (scope.AccountID != 0 && !hasSelection) {
			return nil, denied()
		}
		if err := lockActorForRead(ctx, q, scope.OrganizationID, actorID); err != nil {
			return nil, err
		}
		if scope.AccountID == 0 {
			host, err = authz.LoadEffectiveForShare(ctx, q, actorID, scope.OrganizationID)
		} else {
			var account Account
			account, err = s.authorizePoolAccess(ctx, q, actorID, scope.OrganizationID, selected, View|ChangePools)
			if err == nil && account.ID != scope.AccountID {
				return nil, denied()
			}
			if err == nil {
				var path []sqlc.HostedAccount
				path, err = loadPath(ctx, q, scope.OrganizationID, selected)
				if err == nil {
					policy, err = sitePolicy(ctx, q, scope.OrganizationID, path, false)
				}
			}
			if err == nil {
				host, err = hostReadAuthority(ctx, q, actorID, scope.OrganizationID, account, miningRead)
			}
		}
		if err != nil {
			return nil, err
		}
	}
	device, err := q.GetPoolEquipmentDevice(ctx, sqlc.GetPoolEquipmentDeviceParams{OrgID: scope.OrganizationID, ID: deviceID})
	if err != nil {
		return nil, accessError(err)
	}
	if !siteAllowed(policy, device.SiteID.Int64) {
		return nil, denied()
	}
	access := &poolEquipmentAccess{device: device, host: host,
		ownersByBoard: make(map[[2]int64][]sqlc.ListPoolEquipmentModuleOwnersRow),
		installations: make(map[int64]sqlc.ListPoolEquipmentInstallationsRow)}
	switch commonpb.DeviceClass(device.DeviceClass) {
	case commonpb.DeviceClass_DEVICE_CLASS_RIG:
		owner, err := q.GetPoolEquipmentMinerOwner(ctx, sqlc.GetPoolEquipmentMinerOwnerParams{OrgID: scope.OrganizationID, ID: deviceID})
		if err != nil {
			return nil, accessError(err)
		}
		access.owners = []sqlc.ListPoolEquipmentModuleOwnersRow{sqlc.ListPoolEquipmentModuleOwnersRow(owner)}
	case commonpb.DeviceClass_DEVICE_CLASS_CONTAINER:
		if err := q.LockHostedAccountAssetInventory(ctx, []int64{deviceID}); err != nil {
			return nil, accessError(err)
		}
		access.owners, err = q.ListPoolEquipmentModuleOwners(ctx, sqlc.ListPoolEquipmentModuleOwnersParams{OrgID: scope.OrganizationID, ID: deviceID})
		if err != nil {
			return nil, accessError(err)
		}
		for _, owner := range access.owners {
			key := [2]int64{owner.AssetID, owner.HashboardID}
			access.ownersByBoard[key] = append(access.ownersByBoard[key], owner)
		}
		if len(installationIDs) > 0 {
			installations, err := q.ListPoolEquipmentInstallations(ctx, sqlc.ListPoolEquipmentInstallationsParams{OrgID: scope.OrganizationID, InstallationIds: installationIDs, DeviceID: deviceID})
			if err != nil {
				return nil, accessError(err)
			}
			for _, installation := range installations {
				access.installations[installation.ID] = installation
			}
		}
	case commonpb.DeviceClass_DEVICE_CLASS_UNSPECIFIED:
		return nil, denied()
	default:
		return nil, denied()
	}
	return access, nil
}

func (a *poolEquipmentAccess) authorize(scope poolconfigurations.Scope, target poolconfigurations.AssignmentTarget) (string, error) {
	if a.host != nil {
		var site *int64
		if a.device.SiteID.Valid {
			site = &a.device.SiteID.Int64
		}
		permissions := []string{authz.PermSiteRead, authz.PermFleetRead, authz.PermMinerRead, authz.PermMinerUpdatePools}
		if target.Kind == poolconfigurations.TargetInstallation {
			permissions = append(permissions, authz.PermMinerStartMining, authz.PermMinerStopMining)
		}
		for _, permission := range permissions {
			if !a.host.Has(permission, authz.ResourceContext{SiteID: site}) {
				return "", denied()
			}
		}
	}
	owners := a.owners
	if target.Kind == poolconfigurations.TargetInstallation {
		if commonpb.DeviceClass(a.device.DeviceClass) != commonpb.DeviceClass_DEVICE_CLASS_CONTAINER {
			return "", denied()
		}
		installation, ok := a.installations[target.ID]
		if !ok {
			return "", accessError(sql.ErrNoRows)
		}
		owners = a.ownersByBoard[[2]int64{installation.ModuleID, installation.HashboardID}]
	}
	if len(owners) == 0 {
		return "", fleeterror.NewFailedPreconditionError("equipment inventory is unavailable; refresh and review again")
	}
	for _, owner := range owners {
		if owner.AccountID != scope.AccountID {
			return "", denied()
		}
	}
	encoded, err := json.Marshal(struct {
		Device sqlc.GetPoolEquipmentDeviceRow
		Owners []sqlc.ListPoolEquipmentModuleOwnersRow
	}{a.device, owners})
	if err != nil {
		return "", fleeterror.NewInternalError("encode equipment ownership failed")
	}
	return string(encoded), nil
}
