package hostedaccounts

import (
	"context"
	"encoding/json"

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
	info, err := session.GetInfo(ctx)
	if err != nil || info.Actor != "" || info.UserID != actorID || actorID <= 0 || info.OrganizationID != scope.OrganizationID || info.AuthMethod != session.AuthMethodSession || info.AccountKey {
		return "", denied()
	}
	selected, hasSelection := RequestAccountID(ctx)
	if scope.AccountID < 0 || (scope.AccountID == 0 && hasSelection) || (scope.AccountID != 0 && !hasSelection) {
		return "", denied()
	}
	if target.ID <= 0 || target.DeviceID <= 0 || (target.Kind != poolconfigurations.TargetDevice && target.Kind != poolconfigurations.TargetInstallation) || (target.Kind == poolconfigurations.TargetDevice && target.ID != target.DeviceID) {
		return "", fleeterror.NewInvalidArgumentError("invalid pool assignment target")
	}
	if err := lockActorForRead(ctx, q, scope.OrganizationID, actorID); err != nil {
		return "", err
	}
	var policy Sites
	var host *authz.EffectivePermissions
	if scope.AccountID == 0 {
		host, err = authz.LoadEffectiveForShare(ctx, q, actorID, scope.OrganizationID)
	} else {
		var account Account
		account, err = s.authorizePoolAccess(ctx, q, actorID, scope.OrganizationID, selected, View|ChangePools)
		if err == nil && account.ID != scope.AccountID {
			return "", denied()
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
		return "", err
	}
	device, err := q.GetPoolEquipmentDevice(ctx, sqlc.GetPoolEquipmentDeviceParams{OrgID: scope.OrganizationID, ID: target.DeviceID})
	if err != nil {
		return "", accessError(err)
	}
	if !siteAllowed(policy, device.SiteID.Int64) {
		return "", denied()
	}
	if host != nil {
		var site *int64
		if device.SiteID.Valid {
			site = &device.SiteID.Int64
		}
		permissions := []string{authz.PermSiteRead, authz.PermFleetRead, authz.PermMinerRead, authz.PermMinerUpdatePools}
		if target.Kind == poolconfigurations.TargetInstallation {
			permissions = append(permissions, authz.PermMinerStartMining, authz.PermMinerStopMining)
		}
		for _, permission := range permissions {
			if !host.Has(permission, authz.ResourceContext{SiteID: site}) {
				return "", denied()
			}
		}
	}
	owners := []sqlc.ListPoolEquipmentModuleOwnersRow{}
	switch commonpb.DeviceClass(device.DeviceClass) {
	case commonpb.DeviceClass_DEVICE_CLASS_RIG:
		if target.Kind != poolconfigurations.TargetDevice {
			return "", denied()
		}
		owner, err := q.GetPoolEquipmentMinerOwner(ctx, sqlc.GetPoolEquipmentMinerOwnerParams{OrgID: scope.OrganizationID, ID: target.DeviceID})
		if err != nil {
			return "", accessError(err)
		}
		owners = append(owners, sqlc.ListPoolEquipmentModuleOwnersRow(owner))
	case commonpb.DeviceClass_DEVICE_CLASS_CONTAINER:
		if err := q.LockHostedAccountAssetInventory(ctx, []int64{target.DeviceID}); err != nil {
			return "", accessError(err)
		}
		owners, err = q.ListPoolEquipmentModuleOwners(ctx, sqlc.ListPoolEquipmentModuleOwnersParams{OrgID: scope.OrganizationID, ID: target.DeviceID})
		if err != nil {
			return "", accessError(err)
		}
		if target.Kind == poolconfigurations.TargetInstallation {
			installation, err := q.GetPoolEquipmentInstallation(ctx, sqlc.GetPoolEquipmentInstallationParams{OrgID: scope.OrganizationID, ID: target.ID, ID_2: target.DeviceID})
			if err != nil {
				return "", accessError(err)
			}
			selectedOwners := owners[:0]
			for _, owner := range owners {
				if owner.AssetID == installation.ModuleID && owner.HashboardID == installation.HashboardID {
					selectedOwners = append(selectedOwners, owner)
				}
			}
			owners = selectedOwners
		}
	case commonpb.DeviceClass_DEVICE_CLASS_UNSPECIFIED:
		return "", denied()
	default:
		return "", denied()
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
	}{device, owners})
	if err != nil {
		return "", fleeterror.NewInternalError("encode equipment ownership failed")
	}
	return string(encoded), nil
}
