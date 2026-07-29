package ha

import (
	"cmp"
	"context"
	"time"

	"github.com/google/uuid"
)

// Token totally orders Fleet ownership within one PostgreSQL cluster.
type Token struct {
	WriterGeneration int64
	LeaseEpoch       int64
}

// Compare orders ownership tokens lexicographically. A writer promotion always
// outranks every lease epoch from an older writer generation.
func (t Token) Compare(other Token) int {
	return cmp.Or(
		cmp.Compare(t.WriterGeneration, other.WriterGeneration),
		cmp.Compare(t.LeaseEpoch, other.LeaseEpoch),
	)
}

// WriterObservation identifies the writable PostgreSQL server reached through
// Fleet's multi-host DSN. The timeline is the writer generation.
type WriterObservation struct {
	PostgresSystemIdentifier string
	WriterGeneration         int64
	ServerAddress            string
	ServerPort               int32
}

// Ownership is the exact database lease identity held by one Fleet process.
type Ownership struct {
	PostgresSystemIdentifier string
	Token                    Token
	HolderID                 uuid.UUID
	DatabaseTime             time.Time
	ExpiresAt                time.Time
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
