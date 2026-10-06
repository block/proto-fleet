package enrollment

import "time"

type Status string

const (
	StatusPending              Status = "PENDING"
	StatusAwaitingConfirmation Status = "AWAITING_CONFIRMATION"
	StatusConfirmed            Status = "CONFIRMED"
	StatusExpired              Status = "EXPIRED"
	StatusCancelled            Status = "CANCELLED"
)

type FleetNodeStatus string

const (
	FleetNodeStatusPending   FleetNodeStatus = "PENDING"
	FleetNodeStatusConfirmed FleetNodeStatus = "CONFIRMED"
	FleetNodeStatusRevoked   FleetNodeStatus = "REVOKED"
)

type PendingEnrollment struct {
	ID          int64
	CodeHash    string
	OrgID       int64
	CreatedBy   int64
	FleetNodeID *int64
	Status      Status
	ExpiresAt   time.Time
	ConsumedAt  *time.Time
	CreatedAt   time.Time
}

type FleetNode struct {
	ID               int64
	OrgID            int64
	Name             string
	IdentityPubkey   []byte
	EncryptionPubkey []byte
	EnrollmentStatus FleetNodeStatus
	LastSeenAt       *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// AWAITING_CONFIRMATION lives on pending_enrollment.status, not on
// agent.enrollment_status, so operator listings need both fields.
type FleetNodeListing struct {
	FleetNode
	PendingEnrollmentID     *int64
	PendingEnrollmentStatus Status
}
