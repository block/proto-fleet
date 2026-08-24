package betweenchannel

import (
	"errors"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/block/proto-fleet/server/internal/domain/channel"
	"github.com/block/proto-fleet/server/internal/domain/rollout"
)

const StrategyKey = "between_channel"

var (
	ErrLaneNotFound                           = errors.New("rollout lane not found")
	ErrLaneConflict                           = errors.New("rollout lane changed")
	ErrIdempotencyConflict                    = errors.New("rollout lane idempotency key was reused with different input")
	ErrMembershipConflict                     = errors.New("rollout lane membership changed")
	ErrCompatibility                          = errors.New("rollout lane release is incompatible")
	ErrInitialEnforcementConfirmationRequired = errors.New("initial firmware enforcement confirmation required")
	ErrFirmwareConfirmationRequired           = errors.New("firmware enforcement confirmation required")
	ErrReassignmentConfirmationRequired       = errors.New("rollout lane reassignment confirmation required")
	ErrFirmwareConvergenceActive              = errors.New("firmware convergence is still active")
	ErrLaneWorkActive                         = errors.New("rollout lane still has active work")
	ErrLaneEmpty                              = errors.New("Add miners before starting a rollout.")
	ErrTopologyNotReady                       = errors.New("rollout lane model topology is not ready")
	ErrTopologyAlreadyEnabled                 = errors.New("rollout lane model topology is already enabled")
	ErrTopologyRepairConflict                 = errors.New("rollout lane model binding repair conflicts with current state")
	ErrDeclarationConflict                    = errors.New("rollout lane model declaration changed")
	ErrModelWorkActive                        = errors.New("rollout lane model still has active work")
	ErrScalarProjectionUnavailable            = errors.New("legacy rollout lane scalar projection is unavailable")
)

type ReleaseTarget struct {
	FirmwareFileID  string
	Manufacturer    string
	Model           string
	FirmwareVersion string
	SHA256          string
}

type InitialFirmwareStatus string

const (
	InitialFirmwareMatch    InitialFirmwareStatus = "matching"
	InitialFirmwareMismatch InitialFirmwareStatus = "mismatched"
	InitialFirmwareUnknown  InitialFirmwareStatus = "unknown"
)

type InitialFirmwareMiner struct {
	DeviceID               int64
	DeviceIdentifier       string
	Manufacturer           string
	Model                  string
	CurrentFirmwareVersion string
	TargetFirmwareVersion  string
	TargetFirmwareFileID   string
	Status                 InitialFirmwareStatus
}

type InitialEnforcementPreview struct {
	Targets                       []ReleaseTarget
	Miners                        []InitialFirmwareMiner
	MatchingCount                 int32
	MismatchedCount               int32
	UnknownCount                  int32
	Reassignments                 []MembershipReassignment
	RequiresReassignConfirmation  bool
	ReassignmentConfirmationToken string
}

func (p InitialEnforcementPreview) RequiresConfirmation() bool {
	return p.MismatchedCount > 0 || p.UnknownCount > 0
}

type FirmwareConvergenceStatus struct {
	TotalCount     int32
	PendingCount   int32
	UpdatingCount  int32
	VerifyingCount int32
	ConfirmedCount int32
	AttentionCount int32
	Members        []channel.FirmwareTransitionMiner
}

type DeviceTransition struct {
	DeviceID                int64
	DeviceIdentifier        string
	Manufacturer            string
	Model                   string
	SourceReleaseTargetID   int64
	SourceFirmwareFileID    string
	SourceFirmwareVersion   string
	SourceSHA256            string
	ModelIdentityKey        string
	ModelIdentityObservedAt *time.Time
}

type Lane struct {
	ID                        uuid.UUID
	OrgID                     int64
	Label                     string
	Description               string
	CurrentChannelID          int64
	Revision                  int64
	CreatedByUserID           int64
	CreatedAt                 time.Time
	UpdatedAt                 time.Time
	Channels                  []LaneChannel
	MemberCount               int32
	FirmwareConvergence       FirmwareConvergenceStatus
	Models                    []LaneModel
	ScalarProjectionAvailable bool
	TopologyEnabled           bool
}

type LaneChannel struct {
	ChannelID    int64
	ReleaseSetID int64
	Position     int32
	RolloutID    *uuid.UUID
	CreatedAt    time.Time
}

type LaneModel struct {
	ID                     uuid.UUID
	LaneID                 uuid.UUID
	OrgID                  int64
	ModelIdentityKey       string
	NormalizationVersion   int16
	Manufacturer           string
	Model                  string
	CurrentChannelID       int64
	CurrentReleaseSetID    int64
	CurrentReleaseTargetID int64
	Revision               int64
	CreatedAt              time.Time
	UpdatedAt              time.Time
	CurrentFirmwareTarget  *LaneModelFirmwareTarget
	MemberCount            int32
	Bindings               LaneModelBindingSummary
	FirmwareConvergence    FirmwareConvergenceStatus
	Channels               []LaneModelChannel
	Compatibility          LaneModelCompatibility
}

type LaneModelCompatibility string

const (
	LaneModelCompatible        LaneModelCompatibility = "compatible"
	LaneModelTargetUnavailable LaneModelCompatibility = "target_unavailable"
)

type LaneModelFirmwareTarget struct {
	ReleaseTargetID int64
	ReleaseSetID    int64
	FirmwareFileID  string
	FirmwareVersion string
	SHA256          string
}

type LaneModelChannel struct {
	ChannelID      int64
	Position       int32
	Current        bool
	FirmwareTarget LaneModelFirmwareTarget
	CreatedAt      time.Time
}

type LaneModelBindingSummary struct {
	ActiveCount     int64
	HistoricalCount int64
}

type TopologyAnomalyType string

const (
	TopologyAnomalyNullIdentity           TopologyAnomalyType = "null_identity"
	TopologyAnomalyAmbiguousTargetMatch   TopologyAnomalyType = "ambiguous_target_match"
	TopologyAnomalyNoTargetMatch          TopologyAnomalyType = "no_target_match"
	TopologyAnomalyPhysicalMismatch       TopologyAnomalyType = "physical_mismatch"
	TopologyAnomalyMissingBinding         TopologyAnomalyType = "missing_binding"
	TopologyAnomalyDuplicateActiveBinding TopologyAnomalyType = "duplicate_active_binding"
)

type TopologyRepairAction string

const (
	TopologyRepairConfirmIdentity    TopologyRepairAction = "confirm_identity"
	TopologyRepairSelectDeclaration  TopologyRepairAction = "select_declaration"
	TopologyRepairPhysicalMembership TopologyRepairAction = "repair_physical_membership"
	TopologyRepairEndStaleBinding    TopologyRepairAction = "end_stale_binding"
	TopologyRepairBinding            TopologyRepairAction = "repair_binding"
	TopologyRepairRerunBackfill      TopologyRepairAction = "rerun_backfill"
)

type TopologyAnomaly struct {
	ID                     uuid.UUID
	LaneID                 uuid.UUID
	DeviceID               int64
	DeviceIdentifier       string
	LaneModelID            *uuid.UUID
	LaneModelRevision      *int64
	Type                   TopologyAnomalyType
	SupportedRepairActions []TopologyRepairAction
	Details                map[string]any
}

type TopologyReadiness struct {
	OrgID                    int64
	Enabled                  bool
	Revision                 int64
	AnomalyCount             int64
	ActiveLegacyRolloutCount int64
	Anomalies                []TopologyAnomaly
	UpdatedAt                time.Time
}

type RepairModelBindingRequest struct {
	OperationID        uuid.UUID
	BindingID          uuid.UUID
	OrgID              int64
	LaneID             uuid.UUID
	LaneModelID        uuid.UUID
	DeviceIdentifier   string
	ExpectedRevision   int64
	IdempotencyKey     string
	RequestFingerprint string
	Reason             string
	ActorUserID        int64
	ActorType          rollout.ActorType
	ActorCredentialID  *string
}

type RepairModelBindingResult struct {
	BindingID         uuid.UUID
	ResultingRevision int64
	Replayed          bool
	Readiness         TopologyReadiness
}

type EnableTopologyRequest struct {
	OperationID        uuid.UUID
	OrgID              int64
	ExpectedRevision   int64
	IdempotencyKey     string
	RequestFingerprint string
	Reason             string
	ActorUserID        int64
	ActorType          rollout.ActorType
	ActorCredentialID  *string
}

type EnableTopologyResult struct {
	Readiness TopologyReadiness
	Replayed  bool
}

type CreateLaneRequest struct {
	ChangeID                      uuid.UUID
	ID                            uuid.UUID
	OrgID                         int64
	Label                         string
	Description                   string
	FirmwareFileIDs               []string
	ReleaseTargets                []ReleaseTarget
	DeviceIdentifiers             []string
	IdempotencyKey                string
	RequestFingerprint            string
	ActorUserID                   int64
	ActorType                     rollout.ActorType
	ActorCredentialID             *string
	ConfirmInitialEnforcement     bool
	ConfirmReassignment           bool
	ReassignmentConfirmationToken string
}

type PreviewLaneRequest struct {
	OrgID             int64
	FirmwareFileIDs   []string
	ReleaseTargets    []ReleaseTarget
	DeviceIdentifiers []string
}

type LaneAssignment struct {
	DeviceIdentifier string
	LaneID           uuid.UUID
	LaneLabel        string
}

type DeleteLaneRequest struct {
	OrgID              int64
	LaneID             uuid.UUID
	ExpectedRevision   int64
	IdempotencyKey     string
	RequestFingerprint string
	Reason             string
	ActorUserID        int64
	ActorType          rollout.ActorType
	ActorCredentialID  *string
}

type LaneMember struct {
	DeviceID                int64
	DeviceIdentifier        string
	Manufacturer            string
	Model                   string
	ObservedFirmwareVersion string
	ChannelID               int64
	ChannelPosition         int32
	OnCurrentChannel        bool
	PinnedReleaseVersion    string
	Enforcement             *channel.FirmwareTransitionMiner
}

type ListMembersRequest struct {
	OrgID             int64
	LaneID            uuid.UUID
	LaneModelID       uuid.UUID
	ExpectedRevision  int64
	AfterIdentifier   string
	Limit             int32
	IncludeTotalCount bool
}

type ListMembersResult struct {
	Members        []LaneMember
	NextIdentifier string
	TotalCount     int64
	Revision       int64
}

type PreviewMembershipChangeRequest struct {
	OrgID             int64
	LaneID            uuid.UUID
	AddIdentifiers    []string
	RemoveIdentifiers []string
}

type MembershipReassignment struct {
	DeviceIdentifier      string
	SourceLaneID          uuid.UUID
	SourceLaneLabel       string
	SourceChannelID       int64
	SourceChannelPosition int32
	SourceReleaseVersion  string
	SourceLaneRevision    int64
}

type MembershipChangePreview struct {
	TargetFirmwarePreview        InitialEnforcementPreview
	Reassignments                []MembershipReassignment
	Removals                     []LaneMember
	RequiresFirmwareConfirmation bool
	RequiresReassignConfirmation bool
}

type UpdateMembershipRequest struct {
	ChangeID           uuid.UUID
	OrgID              int64
	LaneID             uuid.UUID
	ExpectedRevision   int64
	AddIdentifiers     []string
	RemoveIdentifiers  []string
	ConfirmFirmware    bool
	ConfirmReassign    bool
	IdempotencyKey     string
	RequestFingerprint string
	Reason             string
	ActorUserID        int64
	ActorType          rollout.ActorType
	ActorCredentialID  *string
}

type UpdateMembershipResult struct {
	Lane              *Lane
	TransitionMembers []LaneMember
}

type ModelDeclarationSelector struct {
	LaneModelID      uuid.UUID
	ModelIdentityKey string
}

func (s ModelDeclarationSelector) IsValid() bool {
	return (s.LaneModelID != uuid.Nil) != (s.ModelIdentityKey != "")
}

type CreateModelDeclarationRequest struct {
	OperationID                   uuid.UUID
	LaneModelID                   uuid.UUID
	OrgID                         int64
	LaneID                        uuid.UUID
	ExpectedRevision              int64
	FirmwareFileIDs               []string
	ReleaseTargets                []ReleaseTarget
	DeviceIdentifiers             []string
	IdempotencyKey                string
	RequestFingerprint            string
	Reason                        string
	ActorUserID                   int64
	ActorType                     rollout.ActorType
	ActorCredentialID             *string
	ConfirmInitialEnforcement     bool
	ConfirmReassignment           bool
	ReassignmentConfirmationToken string
}

type PublishModelTargetRequest struct {
	OperationID        uuid.UUID
	OrgID              int64
	LaneID             uuid.UUID
	LaneModelID        uuid.UUID
	ModelIdentityKey   string
	ExpectedRevision   int64
	FirmwareFileIDs    []string
	ReleaseTargets     []ReleaseTarget
	IdempotencyKey     string
	RequestFingerprint string
	Reason             string
	ActorUserID        int64
	ActorType          rollout.ActorType
	ActorCredentialID  *string
}

func (r PublishModelTargetRequest) Selector() ModelDeclarationSelector {
	return ModelDeclarationSelector{LaneModelID: r.LaneModelID, ModelIdentityKey: r.ModelIdentityKey}
}

type PreviewModelMembershipChangeRequest struct {
	OrgID             int64
	LaneID            uuid.UUID
	LaneModelID       uuid.UUID
	ModelIdentityKey  string
	AddIdentifiers    []string
	RemoveIdentifiers []string
}

func (r PreviewModelMembershipChangeRequest) Selector() ModelDeclarationSelector {
	return ModelDeclarationSelector{LaneModelID: r.LaneModelID, ModelIdentityKey: r.ModelIdentityKey}
}

type UpdateModelMembershipRequest struct {
	OperationID        uuid.UUID
	OrgID              int64
	LaneID             uuid.UUID
	LaneModelID        uuid.UUID
	ModelIdentityKey   string
	ExpectedRevision   int64
	AddIdentifiers     []string
	RemoveIdentifiers  []string
	ConfirmFirmware    bool
	ConfirmReassign    bool
	IdempotencyKey     string
	RequestFingerprint string
	Reason             string
	ActorUserID        int64
	ActorType          rollout.ActorType
	ActorCredentialID  *string
}

func (r UpdateModelMembershipRequest) Selector() ModelDeclarationSelector {
	return ModelDeclarationSelector{LaneModelID: r.LaneModelID, ModelIdentityKey: r.ModelIdentityKey}
}

type StartRolloutRequest struct {
	ParentID           uuid.UUID
	ID                 uuid.UUID
	OrgID              int64
	LaneID             uuid.UUID
	Name               string
	FirmwareFileIDs    []string
	ReleaseTargets     []ReleaseTarget
	Batches            []rollout.CreateBatch
	HashratePolicy     *rollout.HashratePolicy
	IdempotencyKey     string
	RequestFingerprint string
	Reason             string
	ActorUserID        int64
	ActorType          rollout.ActorType
	ActorCredentialID  *string
	ModelPlans         []StartRolloutModelPlan
}

type StartRolloutModelPlan struct {
	LaneModelID           uuid.UUID
	ExpectedModelRevision int64
	FirmwareFileID        string
	ReleaseTarget         ReleaseTarget
	Batches               []rollout.CreateBatch
	HashratePolicy        *rollout.HashratePolicy
	ModelStartKey         string
}

func SortStartRolloutModelPlans(plans []StartRolloutModelPlan) {
	sort.Slice(plans, func(i, j int) bool {
		left := CanonicalModelIdentityKey(
			plans[i].ReleaseTarget.Manufacturer,
			plans[i].ReleaseTarget.Model,
		)
		right := CanonicalModelIdentityKey(
			plans[j].ReleaseTarget.Manufacturer,
			plans[j].ReleaseTarget.Model,
		)
		if left == right {
			return plans[i].LaneModelID.String() < plans[j].LaneModelID.String()
		}
		return left < right
	})
}

type StartedRolloutModel struct {
	Child        *rollout.Rollout
	FirstBatchID int64
}

type StartRolloutResult struct {
	Lane     *Lane
	Rollout  *rollout.Rollout
	Parent   *rollout.Group
	Children []StartedRolloutModel
}

type CompletionStatus struct {
	TotalMembers           int64
	SucceededMembers       int64
	TerminalForwardMembers int64
	RevertMembers          int64
	RevertedMembers        int64
}

type Finalization struct {
	MemberID                 int64
	RolloutID                uuid.UUID
	OrgID                    int64
	BatchID                  int64
	DeviceID                 int64
	DeviceIdentifier         string
	MemberState              rollout.MemberState
	MemberRevision           int64
	EnforcementID            int64
	EnforcementState         channel.EnforcementState
	AuthorityID              uuid.UUID
	LastError                string
	RolloutState             rollout.State
	RolloutRevision          int64
	ForwardAuthorityID       uuid.UUID
	ForwardAuthorityRevision int64
	RevertAuthorityID        *uuid.UUID
	RevertAuthorityRevision  *int64
	CreatedByUserID          int64
	SourceChannelID          int64
	TargetChannelID          int64
	LaneID                   uuid.UUID
	CurrentChannelID         int64
	LaneModelID              *uuid.UUID
	ParentID                 *uuid.UUID
	ModelIdentityKey         string
	Manufacturer             string
	Model                    string
	ModelIdentityValidatedAt *time.Time
	CommandCompletedAt       *time.Time
	ObservedModelIdentityKey string
	ModelIdentityObservedAt  *time.Time
	ModelCurrentChannelID    *int64
}

type FinalizationOutcome string

const (
	FinalizationOutcomeMoved     FinalizationOutcome = "moved"
	FinalizationOutcomeAttention FinalizationOutcome = "attention_required"
	FinalizationOutcomeCancelled FinalizationOutcome = "cancelled"
	FinalizationOutcomeConflict  FinalizationOutcome = "membership_conflict"
)

type FinalizationResult struct {
	Finalization
	Outcome         FinalizationOutcome
	ProjectActivity bool
}
