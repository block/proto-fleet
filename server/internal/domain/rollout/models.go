package rollout

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	ErrNotFound              = errors.New("rollout not found")
	ErrRevisionConflict      = errors.New("rollout revision changed")
	ErrIdempotencyConflict   = errors.New("rollout idempotency key was reused with different input")
	ErrInvalidTransition     = errors.New("invalid rollout state transition")
	ErrOwnershipConflict     = errors.New("miner is already owned by a nonterminal rollout")
	ErrStrategyUnavailable   = errors.New("rollout admission strategy is not registered")
	ErrParentNotControllable = errors.New("aggregate rollout parents do not accept child controls")
)

type State string

const (
	StateCreated               State = "created"
	StateRunning               State = "running"
	StatePaused                State = "paused"
	StateReview                State = "review"
	StateAborted               State = "aborted"
	StateCompleted             State = "completed"
	StateCompletedWithFailures State = "completed_with_failures"
	StateReverting             State = "reverting"
	StateReverted              State = "reverted"
)

func (s State) IsTerminal() bool {
	switch s {
	case StateAborted, StateCompleted, StateCompletedWithFailures, StateReverted:
		return true
	case StateCreated, StateRunning, StatePaused, StateReview, StateReverting:
		return false
	default:
		return false
	}
}

type BatchState string

const (
	BatchStatePending   BatchState = "pending"
	BatchStateAdmitted  BatchState = "admitted"
	BatchStateCompleted BatchState = "completed"
	BatchStateCancelled BatchState = "cancelled"
)

type MemberState string

const (
	MemberStatePending           MemberState = "pending"
	MemberStateAdmitted          MemberState = "admitted"
	MemberStateSucceeded         MemberState = "succeeded"
	MemberStateFailed            MemberState = "failed"
	MemberStateAttentionRequired MemberState = "attention_required"
	MemberStateCancelled         MemberState = "cancelled"
	MemberStateReverting         MemberState = "reverting"
	MemberStateReverted          MemberState = "reverted"
)

type ControlOperation string

const (
	ControlOperationCreate   ControlOperation = "create"
	ControlOperationAdmit    ControlOperation = "admit"
	ControlOperationContinue ControlOperation = "continue"
	ControlOperationPause    ControlOperation = "pause"
	ControlOperationResume   ControlOperation = "resume"
	ControlOperationAbort    ControlOperation = "abort"
	ControlOperationRevert   ControlOperation = "revert"
	ControlOperationComplete ControlOperation = "complete"
)

type ControlStatus string

const (
	ControlStatusStarted   ControlStatus = "started"
	ControlStatusSucceeded ControlStatus = "succeeded"
	ControlStatusFailed    ControlStatus = "failed"
)

type ActorType string

const (
	ActorTypeUser   ActorType = "user"
	ActorTypeAPIKey ActorType = "api_key"
	ActorTypeSystem ActorType = "system"
)

func (a ActorType) Valid() bool {
	switch a {
	case "", ActorTypeUser, ActorTypeAPIKey, ActorTypeSystem:
		return true
	default:
		return false
	}
}

type EvidencePhase string

const (
	EvidencePhaseBaseline EvidencePhase = "baseline"
	EvidencePhasePost     EvidencePhase = "post"
)

type EvidenceStatus string

const (
	EvidenceStatusPending         EvidenceStatus = "pending"
	EvidenceStatusCollecting      EvidenceStatus = "collecting"
	EvidenceStatusUnavailable     EvidenceStatus = "unavailable"
	EvidenceStatusObserving       EvidenceStatus = "observing"
	EvidenceStatusHealthy         EvidenceStatus = "healthy"
	EvidenceStatusHeld            EvidenceStatus = "held"
	EvidenceStatusStale           EvidenceStatus = "stale"
	EvidenceStatusAutomationError EvidenceStatus = "automation_error"
	EvidenceStatusFinalized       EvidenceStatus = "finalized"
	EvidenceStatusCancelled       EvidenceStatus = "cancelled"
)

type HashratePolicy struct {
	MaxDropBasisPoints     int32
	HealthyDurationSeconds int32
}

type Rollout struct {
	ID                       uuid.UUID
	OrgID                    int64
	Name                     string
	StrategyKey              string
	State                    State
	ResumeState              *State
	Revision                 int64
	ForwardAuthorityID       uuid.UUID
	ForwardAuthorityRevision int64
	RevertAuthorityID        *uuid.UUID
	RevertAuthorityRevision  *int64
	SourceChannelID          *int64
	TargetChannelID          *int64
	SourceReleaseSetID       *int64
	TargetReleaseSetID       *int64
	SourceSnapshot           map[string]any
	TargetSnapshot           map[string]any
	RevertSnapshot           map[string]any
	HashratePolicy           *HashratePolicy
	Reason                   string
	CreatedByUserID          int64
	StartedAt                *time.Time
	PausedAt                 *time.Time
	AbortedAt                *time.Time
	CompletedAt              *time.Time
	RevertingAt              *time.Time
	RevertedAt               *time.Time
	CreatedAt                time.Time
	UpdatedAt                time.Time
	Batches                  []Batch
	Members                  []Member
	Causes                   []Cause
	GroupID                  *uuid.UUID
	LaneID                   *uuid.UUID
	LaneModelID              *uuid.UUID
	ModelIdentityKey         string
	ModelIdentityValidatedAt *time.Time
	Manufacturer             string
	Model                    string
	SourceReleaseTargetID    *int64
	TargetReleaseTargetID    *int64
	FailedAdmission          bool
}

// Group is the durable aggregate for independently controlled model rollouts.
// Its lifecycle is derived from children; ResultRevision is dismissal metadata,
// not a control revision.
type Group struct {
	ID                uuid.UUID
	LaneID            uuid.UUID
	OrgID             int64
	Name              string
	ResultRevision    int64
	TerminalOutcome   GroupTerminalOutcome
	ResultReady       bool
	Lifecycle         GroupLifecycle
	Activity          GroupActivity
	NeedsAction       bool
	EvidenceReadiness GroupEvidenceReadiness
	Reason            string
	CreatedByUserID   int64
	ActorType         ActorType
	ActorCredentialID *string
	CreatedAt         time.Time
	UpdatedAt         time.Time
	ModelSnapshots    []GroupModelSnapshot
	Children          []Rollout
}

type GroupLifecycle string

const (
	GroupLifecycleActive   GroupLifecycle = "active"
	GroupLifecycleTerminal GroupLifecycle = "terminal"
)

type GroupActivity string

const (
	GroupActivityFailedAdmission   GroupActivity = "failed_admission"
	GroupActivityAttentionRequired GroupActivity = "attention_required"
	GroupActivityReview            GroupActivity = "review"
	GroupActivityPaused            GroupActivity = "paused"
	GroupActivityReverting         GroupActivity = "reverting"
	GroupActivityFinalizing        GroupActivity = "finalizing"
	GroupActivityRunning           GroupActivity = "running"
	GroupActivityCreated           GroupActivity = "created"
	GroupActivitySettled           GroupActivity = "settled"
)

type GroupTerminalOutcome string

const (
	GroupTerminalOutcomePending               GroupTerminalOutcome = "pending"
	GroupTerminalOutcomeSuccessful            GroupTerminalOutcome = "successful"
	GroupTerminalOutcomeReverted              GroupTerminalOutcome = "reverted"
	GroupTerminalOutcomeAborted               GroupTerminalOutcome = "aborted"
	GroupTerminalOutcomeCompletedWithFailures GroupTerminalOutcome = "completed_with_failures"
	GroupTerminalOutcomeMixed                 GroupTerminalOutcome = "mixed"
)

type GroupEvidenceReadiness string

const (
	GroupEvidencePending GroupEvidenceReadiness = "pending"
	GroupEvidenceReady   GroupEvidenceReadiness = "ready"
)

type GroupProjection struct {
	Lifecycle         GroupLifecycle
	Activity          GroupActivity
	NeedsAction       bool
	TerminalOutcome   GroupTerminalOutcome
	EvidenceReadiness GroupEvidenceReadiness
	ResultReady       bool
}

type GroupModelSnapshot struct {
	GroupID               uuid.UUID
	LaneID                uuid.UUID
	LaneModelID           uuid.UUID
	OrgID                 int64
	ModelIdentityKey      string
	SourceChannelID       int64
	SourceReleaseSetID    int64
	SourceReleaseTargetID int64
	TargetChannelID       int64
	TargetReleaseSetID    int64
	TargetReleaseTargetID int64
	ChildRolloutID        *uuid.UUID
	Snapshot              map[string]any
	CreatedAt             time.Time
}

type Batch struct {
	ID                                 int64
	RolloutID                          uuid.UUID
	OrgID                              int64
	Position                           int32
	Label                              string
	State                              BatchState
	Revision                           int64
	CompletedAt                        *time.Time
	EvidenceStatus                     EvidenceStatus
	EvidenceTotalCount                 int64
	EvidencePairedCount                int64
	CumulativeBaselineHashrateHS       *float64
	CumulativeCurrentHashrateHS        *float64
	CumulativeDeltaBasisPoints         *int32
	LatestPolicyBucketHashrateHS       *float64
	LatestPolicyBucketDeltaBasisPoints *int32
	HealthySince                       *time.Time
	LastPolicyBucketBoundary           *time.Time
	EvaluatedAt                        *time.Time
	EvidenceErrorMessage               *string
	PostWindowFinalized                bool
	PostWindowFinalizedAt              *time.Time
	EvidenceCancellationReason         *string
	EvidenceCancelledAt                *time.Time
	CreatedAt                          time.Time
	UpdatedAt                          time.Time
	Members                            []Member
	AdmissionAttempt                   int32
}

type Member struct {
	ID                       int64
	RolloutID                uuid.UUID
	BatchID                  int64
	OrgID                    int64
	DeviceID                 int64
	DeviceIdentifier         string
	Position                 int32
	State                    MemberState
	Revision                 int64
	SourceSnapshot           map[string]any
	TargetSnapshot           map[string]any
	RevertSnapshot           map[string]any
	EnforcementID            *int64
	CommandBatchUUID         *string
	LastError                *string
	AdmittedAt               *time.Time
	SettledAt                *time.Time
	OwnerReleasedAt          *time.Time
	CreatedAt                time.Time
	UpdatedAt                time.Time
	Evidence                 []Evidence
	ModelIdentityKey         string
	ModelIdentityValidatedAt *time.Time
}

type Evidence struct {
	ID              int64
	RolloutID       uuid.UUID
	MemberID        int64
	OrgID           int64
	Phase           EvidencePhase
	WindowStart     time.Time
	WindowEnd       time.Time
	ObservedAt      *time.Time
	AvgHashrateHS   *float64
	AvgPowerW       *float64
	AvgTemperatureC *float64
	ErrorCount      *int64
	SampleCount     *int64
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type Cause struct {
	ID                int64
	RolloutID         uuid.UUID
	MemberID          *int64
	ControlID         *uuid.UUID
	OrgID             int64
	Operation         ControlOperation
	Reason            string
	ActorUserID       int64
	ActorType         ActorType
	ActorCredentialID *string
	FromState         *State
	ToState           State
	RolloutRevision   int64
	CreatedAt         time.Time
}

type Control struct {
	ID                 uuid.UUID
	RolloutID          uuid.UUID
	OrgID              int64
	BatchID            *int64
	Operation          ControlOperation
	IdempotencyKey     string
	RequestFingerprint string
	ExpectedRevision   int64
	ResultingRevision  int64
	Status             ControlStatus
	ErrorMessage       *string
	CreatedByUserID    int64
	ActorType          ActorType
	ActorCredentialID  *string
	CreatedAt          time.Time
	UpdatedAt          time.Time
	AdmissionAttempt   *int32
}

type CreateMember struct {
	DeviceIdentifier         string
	ModelIdentityKey         string
	ModelIdentityValidatedAt *time.Time
	SourceSnapshot           map[string]any
	TargetSnapshot           map[string]any
	RevertSnapshot           map[string]any
}

type CreateBatch struct {
	Label   string
	Members []CreateMember
}

type CreateRequest struct {
	ID                       uuid.UUID
	OrgID                    int64
	Name                     string
	StrategyKey              string
	SourceChannelID          *int64
	TargetChannelID          *int64
	SourceReleaseSetID       *int64
	TargetReleaseSetID       *int64
	SourceSnapshot           map[string]any
	TargetSnapshot           map[string]any
	RevertSnapshot           map[string]any
	HashratePolicy           *HashratePolicy
	Batches                  []CreateBatch
	IdempotencyKey           string
	RequestFingerprint       string
	Reason                   string
	ActorUserID              int64
	ActorType                ActorType
	ActorCredentialID        *string
	GroupID                  *uuid.UUID
	LaneID                   *uuid.UUID
	LaneModelID              *uuid.UUID
	ModelIdentityKey         string
	ModelIdentityValidatedAt *time.Time
	SourceReleaseTargetID    *int64
	TargetReleaseTargetID    *int64
}

type CreateResult struct {
	Rollout  *Rollout
	Replayed bool
}

type ControlRequest struct {
	OrgID              int64
	RolloutID          uuid.UUID
	BatchID            int64
	ExpectedRevision   int64
	Operation          ControlOperation
	IdempotencyKey     string
	RequestFingerprint string
	Reason             string
	ActorUserID        int64
	ActorType          ActorType
	ActorCredentialID  *string
	WithFailures       bool
}

type ControlResult struct {
	Rollout  *Rollout
	Batch    *Batch
	Control  Control
	Replayed bool
}

type AdmissionOutcome string

const (
	AdmissionOutcomeCommitted              AdmissionOutcome = "committed"
	AdmissionOutcomeDefinitivelyRolledBack AdmissionOutcome = "definitively_rolled_back"
	AdmissionOutcomeUnknown                AdmissionOutcome = "transaction_outcome_unknown"
)

type AdmissionResult struct {
	Outcome AdmissionOutcome
	Err     error
}

type FinishControlRequest struct {
	OrgID        int64
	RolloutID    uuid.UUID
	ControlID    uuid.UUID
	Success      bool
	ErrorMessage string
}

type MemberUpdateRequest struct {
	OrgID            int64
	RolloutID        uuid.UUID
	MemberID         int64
	ExpectedRevision int64
	State            MemberState
	EnforcementID    *int64
	CommandBatchUUID *string
	LastError        *string
}

type EvidenceRequest struct {
	OrgID       int64
	RolloutID   uuid.UUID
	Phase       EvidencePhase
	WindowStart time.Time
	WindowEnd   time.Time
	FreshAfter  time.Time
}
