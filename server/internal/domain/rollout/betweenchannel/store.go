package betweenchannel

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/block/proto-fleet/server/internal/domain/rollout"
)

type LaneStore interface {
	PreviewLane(ctx context.Context, req PreviewLaneRequest) (InitialEnforcementPreview, error)
	CreateLane(ctx context.Context, req CreateLaneRequest) (*Lane, error)
	GetLane(
		ctx context.Context,
		orgID int64,
		laneID uuid.UUID,
		includeFirmwareConvergenceMembers bool,
		firmwareConvergenceMembersUpdatedAfter *time.Time,
	) (*Lane, error)
	ListLanes(ctx context.Context, orgID int64, activeFirmwareConvergenceOnly bool) ([]Lane, error)
	ListMembers(ctx context.Context, req ListMembersRequest) (ListMembersResult, error)
	PreviewMembershipChange(
		ctx context.Context,
		req PreviewMembershipChangeRequest,
	) (MembershipChangePreview, error)
	UpdateMembership(
		ctx context.Context,
		req UpdateMembershipRequest,
	) (UpdateMembershipResult, error)
	DeleteLane(ctx context.Context, req DeleteLaneRequest) error
	StartRollout(ctx context.Context, req StartRolloutRequest) (StartRolloutResult, error)
}

type StrategyStore interface {
	AdmitBatch(ctx context.Context, req rollout.AdmissionRequest) error
	PrepareRevert(ctx context.Context, req rollout.RevertRequest) error
	GetCompletionStatus(
		ctx context.Context,
		orgID int64,
		rolloutID uuid.UUID,
	) (CompletionStatus, error)
	AdvanceLane(
		ctx context.Context,
		orgID int64,
		rolloutID uuid.UUID,
		expectedChannelID int64,
		targetChannelID int64,
	) error
}

type FinalizationStore interface {
	ListFinalizations(ctx context.Context, limit int32) ([]Finalization, error)
	Finalize(ctx context.Context, finalization Finalization) (FinalizationResult, error)
}
