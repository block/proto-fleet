package betweenchannel

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/block/proto-fleet/server/internal/domain/rollout"
)

//nolint:interfacebloat // Lane lifecycle operations share one transactional store implementation.
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
	GetLaneForRollout(ctx context.Context, orgID int64, rolloutID uuid.UUID) (*Lane, error)
	ListLanes(ctx context.Context, orgID int64, activeFirmwareConvergenceOnly bool) ([]Lane, error)
	ListMembers(ctx context.Context, req ListMembersRequest) (ListMembersResult, error)
	GetAssignments(ctx context.Context, orgID int64, deviceIdentifiers []string) ([]LaneAssignment, error)
	PreviewMembershipChange(
		ctx context.Context,
		req PreviewMembershipChangeRequest,
	) (MembershipChangePreview, error)
	UpdateMembership(
		ctx context.Context,
		req UpdateMembershipRequest,
	) (UpdateMembershipResult, error)
	CreateModelDeclaration(ctx context.Context, req CreateModelDeclarationRequest) (*Lane, error)
	PublishModelTarget(ctx context.Context, req PublishModelTargetRequest) (*Lane, error)
	PreviewModelMembershipChange(
		ctx context.Context,
		req PreviewModelMembershipChangeRequest,
	) (MembershipChangePreview, error)
	UpdateModelMembership(
		ctx context.Context,
		req UpdateModelMembershipRequest,
	) (UpdateMembershipResult, error)
	GetTopologyReadiness(ctx context.Context, orgID int64) (TopologyReadiness, error)
	GetTopologyReadinessPage(
		ctx context.Context,
		orgID int64,
		req TopologyReadinessRequest,
	) (TopologyReadiness, error)
	RepairModelBinding(
		ctx context.Context,
		req RepairModelBindingRequest,
	) (RepairModelBindingResult, error)
	EnableTopology(
		ctx context.Context,
		req EnableTopologyRequest,
	) (EnableTopologyResult, error)
	DeleteLane(ctx context.Context, req DeleteLaneRequest) error
	StartRollout(ctx context.Context, req StartRolloutRequest) (StartRolloutResult, error)
}

type StrategyStore interface {
	AdmitBatch(ctx context.Context, req rollout.AdmissionRequest) rollout.AdmissionResult
	PrepareRevert(ctx context.Context, req rollout.RevertRequest) rollout.RevertResult
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
