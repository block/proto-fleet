package agentenrollment

import "time"

type Status string

const (
	StatusPending              Status = "PENDING"
	StatusAwaitingConfirmation Status = "AWAITING_CONFIRMATION"
	StatusConfirmed            Status = "CONFIRMED"
	StatusExpired              Status = "EXPIRED"
	StatusCancelled            Status = "CANCELLED"
)

type PendingEnrollment struct {
	ID         int64
	CodeHash   string
	OrgID      int64
	CreatedBy  int64
	AgentID    *int64
	Status     Status
	ExpiresAt  time.Time
	ConsumedAt *time.Time
	CreatedAt  time.Time
}

type Agent struct {
	ID                 int64
	OrgID              int64
	Name               string
	IdentityPubkey     []byte
	MinerSigningPubkey []byte
	EnrollmentStatus   string
	LastSeenAt         *time.Time
	CreatedAt          time.Time
	UpdatedAt          time.Time
}
