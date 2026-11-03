package poolconfigurations

import (
	"context"
	"encoding/json"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

func (s *Service) ConfirmReview(ctx context.Context, accountID, reviewID, password string) (ReviewedSave, error) {
	admission, err := s.admitConfirmation(ctx, accountID, reviewID)
	if err != nil {
		return ReviewedSave{}, err
	}
	if password != "" && !admission.changesEquipment && !admission.equipmentReplay {
		return ReviewedSave{}, fleeterror.NewInvalidArgumentError("omit the confirmation password when saving settings without equipment updates")
	}
	var proof CredentialProof
	if admission.changesEquipment {
		if s.equipment == nil {
			return ReviewedSave{}, fleeterror.NewInternalError("equipment confirmation is unavailable")
		}
		proof, err = s.equipment.VerifyPoolEquipmentCredentials(ctx, accountID, password)
		if err != nil {
			return ReviewedSave{}, err
		}
		if proof == nil {
			return ReviewedSave{}, fleeterror.NewInternalError("equipment credential proof is unavailable")
		}
	}
	return withAuthority(ctx, s, accountID, admission.access, func(q sqlc.Querier, p Principal) (ReviewedSave, error) {
		if proof != nil {
			if err := proof(ctx, q); err != nil {
				return ReviewedSave{}, err
			}
		}
		result, err := s.repository.ConfirmReview(ctx, q, p.Scope, p.ActorID, reviewID, s.equipment)
		if err != nil {
			return ReviewedSave{}, err
		}
		summary, err := s.repository.Get(ctx, q, p.Scope, result.Configuration.ID)
		if err != nil {
			return ReviewedSave{}, err
		}
		result.AssignedCount = summary.AssignedCount
		if !result.Replay {
			event := "update_pool_configuration"
			if admission.access == Read {
				event = "apply_pool_configuration"
			}
			if err := configurationAudit(ctx, q, p, event, result.Configuration.ID, result.Configuration.RevisionID); err != nil {
				return ReviewedSave{}, err
			}
		}
		return result, nil
	})
}

type confirmationAdmission struct {
	access           Access
	changesEquipment bool
	equipmentReplay  bool
}

// Reject unauthorized or stale changes before spending the actor's password
// verification budget. Confirmation repeats all checks after verification.
func (s *Service) admitConfirmation(ctx context.Context, accountID, reviewID string) (confirmationAdmission, error) {
	return withAuthority(ctx, s, accountID, Read, func(q sqlc.Querier, p Principal) (confirmationAdmission, error) {
		review, err := q.LockPoolConfigurationReview(ctx, sqlc.LockPoolConfigurationReviewParams{OrganizationID: p.Scope.OrganizationID, AccountID: p.Scope.account(), ActorID: p.ActorID, ReviewID: reviewID})
		if err != nil {
			return confirmationAdmission{}, storageError(err)
		}
		admission := confirmationAdmission{access: Read}
		if review.Kind == "edit" {
			admission.access = Manage
			if _, err := s.authority.AuthorizePoolConfiguration(ctx, q, accountID, Manage); err != nil {
				return confirmationAdmission{}, err
			}
		}
		if review.SavedVersion.Valid {
			// A retry of an accepted equipment confirmation may repeat its
			// password, but only returns the original result without verifying again.
			admission.equipmentReplay = review.OperationID.Valid
			return admission, nil
		}
		if !review.Valid {
			return confirmationAdmission{}, staleReview()
		}
		var reviewed []reviewedTarget
		if json.Unmarshal(review.Targets, &reviewed) != nil {
			return confirmationAdmission{}, fleeterror.NewInternalError("decode pool review failed")
		}
		if len(reviewed) == 0 {
			return admission, nil
		}
		public, err := publicConfiguration(sqlc.PoolConfiguration{}, review.Slots)
		if err != nil {
			return confirmationAdmission{}, err
		}
		targets := make([]AssignmentTarget, len(reviewed))
		for i, item := range reviewed {
			targets[i] = item.Change.Target
		}
		bindings, err := reviewTargets(ctx, q, p.Scope, p.ActorID, targets, public.Slots, s.equipment)
		if err != nil {
			return confirmationAdmission{}, err
		}
		for i, item := range reviewed {
			if item.Binding != bindings[i] {
				return confirmationAdmission{}, staleReview()
			}
		}
		admission.changesEquipment = true
		return admission, nil
	})
}

func (s *Service) ReviewAssignment(ctx context.Context, accountID, id string, version int64, targets []AssignmentTarget) (Review, error) {
	if err := s.refreshReadiness(ctx, accountID, Read, func(sqlc.Querier, Principal) ([]AssignmentTarget, error) { return targets, nil }); err != nil {
		return Review{}, err
	}
	return withAuthority(ctx, s, accountID, Read, func(q sqlc.Querier, p Principal) (Review, error) {
		return s.repository.ReviewAssignment(ctx, q, p.Scope, p.ActorID, id, version, targets, s.equipment)
	})
}

func (s *Service) ReviewDevices(ctx context.Context, accountID, id string, version int64, identifiers []string) (Review, error) {
	targets, err := withAuthority(ctx, s, accountID, Read, func(q sqlc.Querier, p Principal) ([]AssignmentTarget, error) {
		if len(identifiers) < 1 || len(identifiers) > 10000 {
			return nil, fleeterror.NewInvalidArgumentError("select between 1 and 10000 devices")
		}
		devices, err := q.GetOrgDeviceIDsWithIdentifiers(ctx, sqlc.GetOrgDeviceIDsWithIdentifiersParams{OrgID: p.Scope.OrganizationID, DeviceIdentifiers: identifiers})
		if err != nil {
			return nil, storageError(err)
		}
		if len(devices) != len(identifiers) {
			return nil, fleeterror.NewForbiddenError("selected equipment is unavailable")
		}
		targets := make([]AssignmentTarget, len(devices))
		for i, device := range devices {
			targets[i] = AssignmentTarget{Kind: TargetDevice, ID: device.ID, DeviceID: device.ID}
		}
		return targets, nil
	})
	if err != nil {
		return Review{}, err
	}
	return s.ReviewAssignment(ctx, accountID, id, version, targets)
}

func (s *Service) GetOperation(ctx context.Context, accountID, operationID string) (Operation, error) {
	return withAuthority(ctx, s, accountID, Read, func(q sqlc.Querier, p Principal) (Operation, error) {
		operation, err := s.repository.GetOperation(ctx, q, p.Scope, operationID)
		if err != nil {
			return Operation{}, err
		}
		visible, err := s.authority.PoolEquipmentVisibility(ctx, q, p)
		if err != nil {
			return Operation{}, err
		}
		sites, err := q.ListPoolConfigurationOperationSites(ctx, sqlc.ListPoolConfigurationOperationSitesParams{OrganizationID: p.Scope.OrganizationID, OperationID: operationID})
		if err != nil {
			return Operation{}, storageError(err)
		}
		for _, site := range sites {
			if !visible(site) {
				return Operation{}, fleeterror.NewForbiddenError("equipment access denied")
			}
		}
		return operation, nil
	})
}
