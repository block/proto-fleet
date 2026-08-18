package channel

import (
	"time"

	"github.com/google/uuid"
)

type EnforcementState string

const (
	EnforcementStatePending           EnforcementState = "pending"
	EnforcementStateHeld              EnforcementState = "held"
	EnforcementStateDispatching       EnforcementState = "dispatching"
	EnforcementStateDispatched        EnforcementState = "dispatched"
	EnforcementStateVerifying         EnforcementState = "verifying"
	EnforcementStateConfirmed         EnforcementState = "confirmed"
	EnforcementStateAttentionRequired EnforcementState = "attention_required"
	EnforcementStateCancelled         EnforcementState = "cancelled"
)

func (s EnforcementState) IsTerminal() bool {
	switch s {
	case EnforcementStateConfirmed,
		EnforcementStateAttentionRequired,
		EnforcementStateCancelled:
		return true
	case EnforcementStatePending,
		EnforcementStateHeld,
		EnforcementStateDispatching,
		EnforcementStateDispatched,
		EnforcementStateVerifying:
		return false
	default:
		return false
	}
}

type FirmwareTransitionState string

const (
	FirmwareTransitionPending        FirmwareTransitionState = "pending"
	FirmwareTransitionUpdating       FirmwareTransitionState = "updating"
	FirmwareTransitionVerifying      FirmwareTransitionState = "verifying"
	FirmwareTransitionConfirmed      FirmwareTransitionState = "confirmed"
	FirmwareTransitionNeedsAttention FirmwareTransitionState = "needs_attention"
)

type FirmwareTransitionMiner struct {
	DeviceIdentifier              string
	Manufacturer                  string
	Model                         string
	LatestObservedFirmwareVersion string
	TargetFirmwareVersion         string
	State                         FirmwareTransitionState
	LastError                     string
	UpdatedAt                     time.Time
}

type Authority struct {
	ID              uuid.UUID
	OrgID           int64
	Type            string
	Reference       string
	Revision        int64
	HaltedAt        *time.Time
	CreatedByUserID int64
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type Enforcement struct {
	ID                          int64
	OrgID                       int64
	DeviceID                    int64
	DeviceIdentifier            string
	DesiredReleaseSetID         int64
	DesiredReleaseTargetID      int64
	DesiredFirmwareFileID       string
	DesiredFirmwareVersion      string
	CauseType                   string
	CauseReference              *string
	AuthorityID                 uuid.UUID
	AuthorityRevision           int64
	State                       EnforcementState
	AttemptCount                int32
	CommandBatchUUID            string
	Revision                    int64
	DesiredAt                   time.Time
	HeldAt                      *time.Time
	ClaimedAt                   *time.Time
	EnqueuedAt                  *time.Time
	CommandCompletedAt          *time.Time
	NextReconcileAt             time.Time
	LastObservedFirmwareVersion *string
	FirmwareObservedAt          *time.Time
	LastObservedHashrateHS      *float64
	HashingObservedAt           *time.Time
	ConfirmedAt                 *time.Time
	AttentionRequiredAt         *time.Time
	LastError                   *string
	CreatedAt                   time.Time
	UpdatedAt                   time.Time
	CreatedByUserID             int64
}

type CommandOutcomeStatus string

const (
	CommandOutcomeMissing    CommandOutcomeStatus = "missing"
	CommandOutcomePending    CommandOutcomeStatus = "pending"
	CommandOutcomeProcessing CommandOutcomeStatus = "processing"
	CommandOutcomeSuccess    CommandOutcomeStatus = "success"
	CommandOutcomeFailed     CommandOutcomeStatus = "failed"
)

type CommandOutcome struct {
	Status      CommandOutcomeStatus
	Error       string
	CompletedAt time.Time
}

type Observation struct {
	FirmwareVersion string
	ObservedAt      time.Time
	HashrateHS      *float64
	Error           string
}

type CreateAuthorityParams struct {
	ID              uuid.UUID
	OrgID           int64
	Type            string
	Reference       string
	CreatedByUserID int64
}

type CreateEnforcementParams struct {
	OrgID             int64
	DeviceID          int64
	ReleaseTargetID   int64
	CauseType         string
	CauseReference    *string
	AuthorityID       uuid.UUID
	AuthorityRevision int64
}
