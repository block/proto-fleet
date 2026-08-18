package rollout

import (
	"context"

	"github.com/google/uuid"
)

type Store interface {
	Create(ctx context.Context, req CreateRequest) (CreateResult, error)
	Get(ctx context.Context, orgID int64, rolloutID uuid.UUID) (*Rollout, error)
	List(ctx context.Context, orgID int64, states []State) ([]Rollout, error)
	ApplyControl(ctx context.Context, req ControlRequest) (ControlResult, error)
	FinishControl(ctx context.Context, req FinishControlRequest) (*Rollout, error)
	UpdateMember(ctx context.Context, req MemberUpdateRequest) (Member, error)
	CaptureEvidence(ctx context.Context, req EvidenceRequest) ([]Evidence, error)
}

type AdmissionStrategy interface {
	Key() string
	Admit(ctx context.Context, req AdmissionRequest) error
	Revert(ctx context.Context, req RevertRequest) error
}

type AdmissionRequest struct {
	Rollout        Rollout
	Batch          Batch
	ControlID      uuid.UUID
	IdempotencyKey string
}

type RevertRequest struct {
	Rollout        Rollout
	ControlID      uuid.UUID
	IdempotencyKey string
}
