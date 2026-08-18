package channel

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

var ErrCASConflict = errors.New("channel firmware enforcement state changed")

type AuthorityStore interface {
	CreateAuthority(ctx context.Context, params CreateAuthorityParams) (Authority, error)
	AdvanceAuthorityRevision(
		ctx context.Context,
		authorityID uuid.UUID,
		orgID int64,
		expectedRevision int64,
	) (Authority, error)
	HaltAuthority(
		ctx context.Context,
		authorityID uuid.UUID,
		orgID int64,
		expectedRevision int64,
	) (Authority, error)
}

type DesiredStateStore interface {
	CreateEnforcement(ctx context.Context, params CreateEnforcementParams) (Enforcement, error)
	GetEnforcement(ctx context.Context, enforcementID int64) (Enforcement, error)
}

type EnforcementStore interface {
	ListForReconcile(ctx context.Context, limit int32) ([]Enforcement, error)
	Claim(
		ctx context.Context,
		enforcement Enforcement,
		commandBatchUUID string,
		claimedAt time.Time,
	) (Enforcement, error)
	Hold(ctx context.Context, enforcement Enforcement, reason string, heldAt time.Time) error
	ReturnPending(ctx context.Context, enforcement Enforcement, reason string) error
	MarkDispatched(ctx context.Context, enforcement Enforcement, enqueuedAt time.Time) error
	CommandOutcome(ctx context.Context, enforcement Enforcement) (CommandOutcome, error)
	MarkVerifying(
		ctx context.Context,
		enforcement Enforcement,
		commandCompletedAt time.Time,
	) error
	RecordObservation(
		ctx context.Context,
		enforcement Enforcement,
		observation Observation,
	) error
	Confirm(ctx context.Context, enforcement Enforcement, observation Observation) error
	MarkAttentionRequired(
		ctx context.Context,
		enforcement Enforcement,
		reason string,
		at time.Time,
	) error
}
