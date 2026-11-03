package poolconfigurations

import (
	"context"

	"github.com/block/proto-fleet/server/generated/sqlc"
)

// Refresh outside the review transaction: controller observations must never be
// repeated by a database transaction retry. The actual review repeats authority
// and binding checks after these observations complete.
func (s *Service) refreshReadiness(ctx context.Context, accountID string, access Access, selection func(sqlc.Querier, Principal) ([]AssignmentTarget, error)) error {
	if s.readiness == nil {
		return nil
	}
	type selected struct {
		principal Principal
		targets   []AssignmentTarget
	}
	resolved, err := withAuthority(ctx, s, accountID, access, func(q sqlc.Querier, p Principal) (selected, error) {
		targets, err := selection(q, p)
		return selected{principal: p, targets: targets}, err
	})
	if err != nil {
		return err
	}
	return s.readiness.RefreshTargets(ctx, resolved.principal.Scope, resolved.principal.ActorID, resolved.targets)
}

func (s *Service) refreshEdit(ctx context.Context, accountID, id string, options RevisionOptions) error {
	return s.refreshReadiness(ctx, accountID, Manage, func(q sqlc.Querier, p Principal) ([]AssignmentTarget, error) {
		options, err := Validate(options)
		if err != nil {
			return nil, err
		}
		config, err := s.repository.Get(ctx, q, p.Scope, id)
		if err != nil {
			return nil, err
		}
		revision, err := q.GetPoolConfigurationRevision(ctx, sqlc.GetPoolConfigurationRevisionParams{OrganizationID: p.Scope.OrganizationID, AccountID: p.Scope.account(), ConfigurationID: id, RevisionID: config.RevisionID})
		if err != nil {
			return nil, storageError(err)
		}
		previous, err := DecodeSlots(revision.Slots)
		if err != nil {
			return nil, err
		}
		encoded, err := EncryptSlots(s.repository.secrets, options.Slots, previous)
		if err != nil {
			return nil, err
		}
		proposed, err := DecodeSlots(encoded)
		if err != nil {
			return nil, err
		}
		equal, err := s.repository.Equivalent(previous, proposed)
		if err != nil || equal {
			return nil, err
		}
		assignments, err := q.ListPoolConfigurationAssignments(ctx, sqlc.ListPoolConfigurationAssignmentsParams{OrganizationID: p.Scope.OrganizationID, AccountID: p.Scope.account(), ConfigurationID: id})
		if err != nil {
			return nil, storageError(err)
		}
		targets := make([]AssignmentTarget, len(assignments))
		for i, a := range assignments {
			targets[i] = AssignmentTarget{Kind: a.TargetKind, ID: a.TargetID, DeviceID: a.DeviceID}
		}
		return targets, nil
	})
}
