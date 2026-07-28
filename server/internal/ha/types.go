package ha

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Token totally orders Fleet ownership within one DCS cluster identity.
type Token struct {
	WriterGeneration int64
	LeaseEpoch       int64
}

// Compare orders ownership tokens lexicographically. A writer promotion always
// outranks every lease epoch from an older writer generation.
func (t Token) Compare(other Token) int {
	switch {
	case t.WriterGeneration < other.WriterGeneration:
		return -1
	case t.WriterGeneration > other.WriterGeneration:
		return 1
	case t.LeaseEpoch < other.LeaseEpoch:
		return -1
	case t.LeaseEpoch > other.LeaseEpoch:
		return 1
	default:
		return 0
	}
}

// WriterObservation is a fail-closed binding between one DCS leader term and
// the writable PostgreSQL server reached through Fleet's multi-host DSN.
type WriterObservation struct {
	DCSClusterID     string
	WriterGeneration int64
	LeaderName       string
	ServerAddress    string
	ServerPort       int32
	Timeline         int64
	DCSProofDeadline time.Time
}

// Ownership is the exact database lease identity held by one Fleet process.
type Ownership struct {
	DCSClusterID string
	Token        Token
	HolderID     uuid.UUID
	DatabaseTime time.Time
	ExpiresAt    time.Time
}

type writerObserver interface {
	ObserveAndRun(
		ctx context.Context,
		action func(context.Context, WriterObservation) error,
	) (WriterObservation, error)
}

type ownershipStore interface {
	Acquire(
		ctx context.Context,
		observed WriterObservation,
		holderID uuid.UUID,
		duration time.Duration,
	) (Ownership, error)
	Renew(
		ctx context.Context,
		observed WriterObservation,
		ownership Ownership,
		duration time.Duration,
	) (Ownership, error)
}
