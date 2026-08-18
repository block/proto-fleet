package betweenchannel

import (
	"errors"
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
	ErrInitialEnforcementActive               = errors.New("initial firmware enforcement is still active")
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
	Targets         []ReleaseTarget
	Miners          []InitialFirmwareMiner
	MatchingCount   int32
	MismatchedCount int32
	UnknownCount    int32
}

func (p InitialEnforcementPreview) RequiresConfirmation() bool {
	return p.MismatchedCount > 0 || p.UnknownCount > 0
}

type InitialEnforcementStatus struct {
	TotalCount     int32
	PendingCount   int32
	UpdatingCount  int32
	ConfirmedCount int32
	AttentionCount int32
}

type DeviceTransition struct {
	DeviceID              int64
	DeviceIdentifier      string
	Manufacturer          string
	Model                 string
	SourceReleaseTargetID int64
	SourceFirmwareFileID  string
	SourceFirmwareVersion string
	SourceSHA256          string
}

type Lane struct {
	ID                 uuid.UUID
	OrgID              int64
	Label              string
	Description        string
	CurrentChannelID   int64
	Revision           int64
	CreatedByUserID    int64
	CreatedAt          time.Time
	UpdatedAt          time.Time
	Channels           []LaneChannel
	InitialEnforcement InitialEnforcementStatus
}

type LaneChannel struct {
	ChannelID    int64
	ReleaseSetID int64
	Position     int32
	RolloutID    *uuid.UUID
	CreatedAt    time.Time
}

type CreateLaneRequest struct {
	ID                        uuid.UUID
	OrgID                     int64
	Label                     string
	Description               string
	FirmwareFileIDs           []string
	ReleaseTargets            []ReleaseTarget
	DeviceIdentifiers         []string
	IdempotencyKey            string
	RequestFingerprint        string
	ActorUserID               int64
	ConfirmInitialEnforcement bool
}

type PreviewLaneRequest struct {
	OrgID             int64
	FirmwareFileIDs   []string
	ReleaseTargets    []ReleaseTarget
	DeviceIdentifiers []string
}

type StartRolloutRequest struct {
	ID                 uuid.UUID
	OrgID              int64
	LaneID             uuid.UUID
	Name               string
	FirmwareFileIDs    []string
	ReleaseTargets     []ReleaseTarget
	Batches            []rollout.CreateBatch
	IdempotencyKey     string
	RequestFingerprint string
	Reason             string
	ActorUserID        int64
	ActorType          rollout.ActorType
	ActorCredentialID  *string
}

type StartRolloutResult struct {
	Lane    *Lane
	Rollout *rollout.Rollout
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
