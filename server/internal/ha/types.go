package ha

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Token identifies one Fleet ownership term within a DCS cluster.
type Token struct {
	WriterGeneration int64
	LeaseEpoch       int64
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
		active Ownership,
		duration time.Duration,
	) (Ownership, error)
}
