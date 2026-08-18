package betweenchannel

import (
	"context"
	"fmt"

	"github.com/block/proto-fleet/server/internal/domain/rollout"
)

type Strategy struct {
	store StrategyStore
}

var (
	_ rollout.AdmissionStrategy  = (*Strategy)(nil)
	_ rollout.CreationStrategy   = (*Strategy)(nil)
	_ rollout.CompletionStrategy = (*Strategy)(nil)
	_ rollout.RevertStrategy     = (*Strategy)(nil)
)

func NewStrategy(store StrategyStore) *Strategy {
	return &Strategy{store: store}
}

func (s *Strategy) Key() string {
	return StrategyKey
}

func (s *Strategy) ValidateCreate(
	context.Context,
	rollout.CreateRequest,
) error {
	return fmt.Errorf(
		"%w: between-channel rollouts must be started through a rollout lane",
		ErrLaneConflict,
	)
}

func (s *Strategy) Admit(ctx context.Context, req rollout.AdmissionRequest) error {
	if req.Rollout.SourceChannelID == nil ||
		req.Rollout.TargetChannelID == nil ||
		req.Rollout.TargetReleaseSetID == nil {
		return fmt.Errorf("%w: rollout channel and release targets are required", ErrCompatibility)
	}
	return s.store.AdmitBatch(ctx, req)
}

func (s *Strategy) Revert(ctx context.Context, req rollout.RevertRequest) error {
	if req.Rollout.SourceChannelID == nil ||
		req.Rollout.TargetChannelID == nil ||
		req.Rollout.SourceReleaseSetID == nil ||
		req.Rollout.RevertAuthorityID == nil ||
		req.Rollout.RevertAuthorityRevision == nil {
		return fmt.Errorf("%w: rollout source and revert authority are required", ErrCompatibility)
	}
	return s.store.PrepareRevert(ctx, req)
}

func (s *Strategy) ValidateRevert(
	_ context.Context,
	req rollout.RevertValidationRequest,
) error {
	for _, member := range req.Rollout.Members {
		if member.State == rollout.MemberStateAdmitted {
			return fmt.Errorf(
				"%w: admitted members must settle before revert starts",
				ErrMembershipConflict,
			)
		}
	}
	return nil
}

func (s *Strategy) ValidateComplete(
	ctx context.Context,
	req rollout.CompletionRequest,
) error {
	if req.Rollout.State == rollout.StateCompleted ||
		req.Rollout.State == rollout.StateCompletedWithFailures ||
		req.Rollout.State == rollout.StateReverted {
		return nil
	}
	status, err := s.store.GetCompletionStatus(
		ctx,
		req.Rollout.OrgID,
		req.Rollout.ID,
	)
	if err != nil {
		return err
	}
	if status.TotalMembers == 0 {
		return fmt.Errorf("%w: rollout has no frozen members", ErrCompatibility)
	}
	if req.Rollout.State == rollout.StateReverting {
		if status.RevertMembers == 0 ||
			status.RevertedMembers != status.RevertMembers {
			return fmt.Errorf(
				"%w: %d of %d members have confirmed source membership",
				ErrMembershipConflict,
				status.RevertedMembers,
				status.RevertMembers,
			)
		}
		return nil
	}
	if status.TerminalForwardMembers != status.TotalMembers {
		return fmt.Errorf(
			"%w: %d of %d members have terminal forward outcomes",
			ErrMembershipConflict,
			status.TerminalForwardMembers,
			status.TotalMembers,
		)
	}
	if !req.WithFailures && status.SucceededMembers != status.TotalMembers {
		return fmt.Errorf(
			"%w: completion with failures requires explicit approval",
			ErrMembershipConflict,
		)
	}
	return nil
}

func (s *Strategy) Complete(
	ctx context.Context,
	req rollout.CompletionRequest,
) error {
	if req.Rollout.SourceChannelID == nil || req.Rollout.TargetChannelID == nil {
		return fmt.Errorf("%w: rollout source and target channels are required", ErrCompatibility)
	}
	switch req.Rollout.State {
	case rollout.StateCompleted, rollout.StateCompletedWithFailures:
		return s.store.AdvanceLane(
			ctx,
			req.Rollout.OrgID,
			req.Rollout.ID,
			*req.Rollout.SourceChannelID,
			*req.Rollout.TargetChannelID,
		)
	case rollout.StateReverted:
		return s.store.AdvanceLane(
			ctx,
			req.Rollout.OrgID,
			req.Rollout.ID,
			*req.Rollout.TargetChannelID,
			*req.Rollout.SourceChannelID,
		)
	case rollout.StateCreated,
		rollout.StateRunning,
		rollout.StatePaused,
		rollout.StateReview,
		rollout.StateAborted,
		rollout.StateReverting:
		return fmt.Errorf(
			"%w: rollout state %s cannot finalize its lane",
			ErrLaneConflict,
			req.Rollout.State,
		)
	}
	return fmt.Errorf("%w: unknown rollout state %s", ErrLaneConflict, req.Rollout.State)
}
