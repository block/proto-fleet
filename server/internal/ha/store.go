package ha

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/block/proto-fleet/server/generated/sqlc"
)

var (
	ErrLeaseUnavailable = errors.New("Fleet active lease is unavailable")
	ErrOwnershipLost    = errors.New("Fleet active lease ownership was lost")
	ErrOwnershipExpired = errors.New("Fleet active lease expired")
)

type leaseQuerier interface {
	AcquireFleetRuntimeLease(
		ctx context.Context,
		arg sqlc.AcquireFleetRuntimeLeaseParams,
	) (sqlc.AcquireFleetRuntimeLeaseRow, error)
	RenewFleetRuntimeLease(
		ctx context.Context,
		arg sqlc.RenewFleetRuntimeLeaseParams,
	) (sqlc.RenewFleetRuntimeLeaseRow, error)
}

// LeaseStore persists the single fleet-active owner through generated sqlc
// queries. Production callers should pass the repo's prepared/retrying querier.
type LeaseStore struct {
	queries leaseQuerier
}

func NewLeaseStore(queries leaseQuerier) *LeaseStore {
	return &LeaseStore{queries: queries}
}

// Acquire atomically creates or advances the fleet-active lease. A strictly
// newer writer generation supersedes stale state even before its old expiry;
// the same generation must wait for database-time expiry.
func (s *LeaseStore) Acquire(
	ctx context.Context,
	observed WriterObservation,
	holderID uuid.UUID,
	duration time.Duration,
) (Ownership, error) {
	if err := validateLeaseInput(observed, holderID, duration); err != nil {
		return Ownership{}, err
	}
	row, err := s.queries.AcquireFleetRuntimeLease(
		ctx,
		sqlc.AcquireFleetRuntimeLeaseParams{
			ServerAddress:             observed.ServerAddress,
			ServerPort:                observed.ServerPort,
			Timeline:                  observed.Timeline,
			DcsClusterID:              observed.DCSClusterID,
			WriterGeneration:          observed.WriterGeneration,
			HolderID:                  holderID,
			LeaseDurationMilliseconds: duration.Milliseconds(),
		},
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Ownership{}, ErrLeaseUnavailable
	}
	if err != nil {
		return Ownership{}, fmt.Errorf("acquire Fleet active lease: %w", err)
	}
	return ownershipFromAcquire(row), nil
}

// Renew extends only the exact unexpired holder/token using database time.
func (s *LeaseStore) Renew(
	ctx context.Context,
	observed WriterObservation,
	active Ownership,
	duration time.Duration,
) (Ownership, error) {
	if err := validateWriterObservation(observed); err != nil {
		return Ownership{}, err
	}
	if observed.DCSClusterID != active.DCSClusterID ||
		observed.WriterGeneration != active.Token.WriterGeneration ||
		active.DCSClusterID == "" ||
		active.Token.WriterGeneration <= 0 ||
		active.Token.LeaseEpoch <= 0 ||
		active.HolderID == uuid.Nil ||
		duration.Milliseconds() <= 0 {
		return Ownership{}, errors.New("invalid Fleet active lease renewal")
	}
	row, err := s.queries.RenewFleetRuntimeLease(
		ctx,
		sqlc.RenewFleetRuntimeLeaseParams{
			ServerAddress:             observed.ServerAddress,
			ServerPort:                observed.ServerPort,
			Timeline:                  observed.Timeline,
			LeaseDurationMilliseconds: duration.Milliseconds(),
			DcsClusterID:              active.DCSClusterID,
			WriterGeneration:          active.Token.WriterGeneration,
			LeaseEpoch:                active.Token.LeaseEpoch,
			HolderID:                  active.HolderID,
		},
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Ownership{}, ErrOwnershipLost
	}
	if err != nil {
		return Ownership{}, fmt.Errorf("renew Fleet active lease: %w", err)
	}
	return Ownership{
		DCSClusterID: row.DcsClusterID,
		Token: Token{
			WriterGeneration: row.HighestWriterGeneration,
			LeaseEpoch:       row.LeaseEpoch,
		},
		HolderID:     row.HolderID,
		ExpiresAt:    row.ExpiresAt,
		DatabaseTime: row.DatabaseTime,
	}, nil
}

func validateLeaseInput(
	observed WriterObservation,
	holderID uuid.UUID,
	duration time.Duration,
) error {
	if err := validateWriterObservation(observed); err != nil {
		return err
	}
	if holderID == uuid.Nil {
		return errors.New("Fleet active lease holder ID is required")
	}
	if duration.Milliseconds() <= 0 {
		return errors.New("Fleet active lease duration must be at least one millisecond")
	}
	return nil
}

func validateWriterObservation(observed WriterObservation) error {
	if observed.DCSClusterID == "" ||
		observed.WriterGeneration <= 0 ||
		observed.ServerAddress == "" ||
		observed.ServerPort <= 0 ||
		observed.Timeline <= 0 {
		return errors.New("invalid writer observation for Fleet active lease")
	}
	return nil
}

func ownershipFromAcquire(row sqlc.AcquireFleetRuntimeLeaseRow) Ownership {
	return Ownership{
		DCSClusterID: row.DcsClusterID,
		Token: Token{
			WriterGeneration: row.HighestWriterGeneration,
			LeaseEpoch:       row.LeaseEpoch,
		},
		HolderID:     row.HolderID,
		ExpiresAt:    row.ExpiresAt,
		DatabaseTime: row.DatabaseTime,
	}
}
