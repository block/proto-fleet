package sqlstores

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/enrollment"
)

var _ enrollment.Store = &SQLFleetNodeEnrollmentStore{}

type SQLFleetNodeEnrollmentStore struct {
	SQLConnectionManager
}

func NewSQLFleetNodeEnrollmentStore(conn *sql.DB) *SQLFleetNodeEnrollmentStore {
	return &SQLFleetNodeEnrollmentStore{SQLConnectionManager: NewSQLConnectionManager(conn)}
}

func (s *SQLFleetNodeEnrollmentStore) q(ctx context.Context) *sqlc.Queries {
	return s.GetQueries(ctx)
}

func (s *SQLFleetNodeEnrollmentStore) CreatePendingEnrollment(ctx context.Context, codeHash string, orgID, createdBy int64, expiresAt time.Time) (*enrollment.PendingEnrollment, error) {
	row, err := s.q(ctx).CreatePendingEnrollment(ctx, sqlc.CreatePendingEnrollmentParams{
		CodeHash:  codeHash,
		OrgID:     orgID,
		CreatedBy: createdBy,
		ExpiresAt: expiresAt,
	})
	if err != nil {
		return nil, err
	}
	return rowToPending(row), nil
}

func (s *SQLFleetNodeEnrollmentStore) GetPendingEnrollmentByCodeHash(ctx context.Context, codeHash string) (*enrollment.PendingEnrollment, error) {
	row, err := s.q(ctx).GetPendingEnrollmentByCodeHash(ctx, codeHash)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fleeterror.NewNotFoundError("pending enrollment not found")
		}
		return nil, err
	}
	return rowToPending(row), nil
}

func (s *SQLFleetNodeEnrollmentStore) GetPendingEnrollmentByFleetNode(ctx context.Context, fleetNodeID, orgID int64) (*enrollment.PendingEnrollment, error) {
	row, err := s.q(ctx).GetPendingEnrollmentByFleetNode(ctx, sqlc.GetPendingEnrollmentByFleetNodeParams{
		FleetNodeID: sql.NullInt64{Int64: fleetNodeID, Valid: true},
		OrgID:       orgID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fleeterror.NewNotFoundError("pending enrollment not found")
		}
		return nil, err
	}
	return rowToPending(row), nil
}

func (s *SQLFleetNodeEnrollmentStore) BindEnrollmentToFleetNode(ctx context.Context, enrollmentID, fleetNodeID int64) (int64, error) {
	return s.q(ctx).BindEnrollmentToFleetNode(ctx, sqlc.BindEnrollmentToFleetNodeParams{
		FleetNodeID: sql.NullInt64{Int64: fleetNodeID, Valid: true},
		ID:          enrollmentID,
	})
}

func (s *SQLFleetNodeEnrollmentStore) ConfirmEnrollment(ctx context.Context, enrollmentID int64, consumedAt time.Time) (int64, error) {
	return s.q(ctx).ConfirmEnrollment(ctx, sqlc.ConfirmEnrollmentParams{
		ConsumedAt: sql.NullTime{Time: consumedAt, Valid: true},
		ID:         enrollmentID,
	})
}

func (s *SQLFleetNodeEnrollmentStore) CancelPendingEnrollment(ctx context.Context, enrollmentID, orgID int64, consumedAt time.Time) (int64, error) {
	return s.q(ctx).CancelPendingEnrollment(ctx, sqlc.CancelPendingEnrollmentParams{
		ConsumedAt: sql.NullTime{Time: consumedAt, Valid: true},
		ID:         enrollmentID,
		OrgID:      orgID,
	})
}

func (s *SQLFleetNodeEnrollmentStore) CancelEnrollmentForFleetNode(ctx context.Context, fleetNodeID, orgID int64, consumedAt time.Time) (int64, error) {
	return s.q(ctx).CancelEnrollmentForFleetNode(ctx, sqlc.CancelEnrollmentForFleetNodeParams{
		ConsumedAt:  sql.NullTime{Time: consumedAt, Valid: true},
		FleetNodeID: sql.NullInt64{Int64: fleetNodeID, Valid: true},
		OrgID:       orgID,
	})
}

func (s *SQLFleetNodeEnrollmentStore) SweepExpiredEnrollments(ctx context.Context, now time.Time) (int64, error) {
	return s.q(ctx).SweepExpiredEnrollments(ctx, now)
}

func (s *SQLFleetNodeEnrollmentStore) CreateFleetNode(ctx context.Context, orgID int64, name string, identityPubkey []byte) (*enrollment.FleetNode, error) {
	row, err := s.q(ctx).CreateFleetNode(ctx, sqlc.CreateFleetNodeParams{
		OrgID:          orgID,
		Name:           name,
		IdentityPubkey: identityPubkey,
	})
	if err != nil {
		return nil, err
	}
	return rowToFleetNode(row.ID, row.OrgID, row.Name, row.IdentityPubkey, row.EnrollmentStatus, row.LastSeenAt, row.CreatedAt, row.UpdatedAt), nil
}

func (s *SQLFleetNodeEnrollmentStore) GetFleetNodeByID(ctx context.Context, fleetNodeID, orgID int64) (*enrollment.FleetNode, error) {
	row, err := s.q(ctx).GetFleetNodeByID(ctx, sqlc.GetFleetNodeByIDParams{ID: fleetNodeID, OrgID: orgID})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fleeterror.NewNotFoundError("fleet node not found")
		}
		return nil, err
	}
	return rowToFleetNode(row.ID, row.OrgID, row.Name, row.IdentityPubkey, row.EnrollmentStatus, row.LastSeenAt, row.CreatedAt, row.UpdatedAt), nil
}

func (s *SQLFleetNodeEnrollmentStore) LockFleetNodeByID(ctx context.Context, fleetNodeID, orgID int64) (*enrollment.FleetNode, error) {
	row, err := s.q(ctx).LockFleetNodeByID(ctx, sqlc.LockFleetNodeByIDParams{ID: fleetNodeID, OrgID: orgID})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fleeterror.NewNotFoundError("fleet node not found")
		}
		return nil, err
	}
	return rowToFleetNode(row.ID, row.OrgID, row.Name, row.IdentityPubkey, row.EnrollmentStatus, row.LastSeenAt, row.CreatedAt, row.UpdatedAt), nil
}

func (s *SQLFleetNodeEnrollmentStore) GetFleetNodeByIDUnscoped(ctx context.Context, fleetNodeID int64) (*enrollment.FleetNode, error) {
	row, err := s.q(ctx).GetFleetNodeByIDUnscoped(ctx, fleetNodeID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fleeterror.NewNotFoundError("fleet node not found")
		}
		return nil, err
	}
	return rowToFleetNode(row.ID, row.OrgID, row.Name, row.IdentityPubkey, row.EnrollmentStatus, row.LastSeenAt, row.CreatedAt, row.UpdatedAt), nil
}

func (s *SQLFleetNodeEnrollmentStore) ListFleetNodesForOrganization(ctx context.Context, orgID int64) ([]enrollment.FleetNodeListing, error) {
	rows, err := s.q(ctx).ListFleetNodesForOrganization(ctx, orgID)
	if err != nil {
		return nil, err
	}
	out := make([]enrollment.FleetNodeListing, 0, len(rows))
	for _, r := range rows {
		out = append(out, enrollment.FleetNodeListing{
			FleetNode:               *rowToFleetNode(r.ID, r.OrgID, r.Name, r.IdentityPubkey, r.EnrollmentStatus, r.LastSeenAt, r.CreatedAt, r.UpdatedAt),
			PendingEnrollmentStatus: enrollment.Status(r.PendingEnrollmentStatus),
		})
	}
	return out, nil
}

func (s *SQLFleetNodeEnrollmentStore) SetFleetNodeEnrollmentStatus(ctx context.Context, status enrollment.FleetNodeStatus, fleetNodeID, orgID int64) (int64, error) {
	return s.q(ctx).SetFleetNodeEnrollmentStatus(ctx, sqlc.SetFleetNodeEnrollmentStatusParams{
		EnrollmentStatus: string(status),
		ID:               fleetNodeID,
		OrgID:            orgID,
	})
}

func (s *SQLFleetNodeEnrollmentStore) SoftDeleteFleetNode(ctx context.Context, fleetNodeID, orgID int64, deletedAt time.Time) (int64, error) {
	return s.q(ctx).SoftDeleteFleetNode(ctx, sqlc.SoftDeleteFleetNodeParams{
		DeletedAt: sql.NullTime{Time: deletedAt, Valid: true},
		ID:        fleetNodeID,
		OrgID:     orgID,
	})
}

func (s *SQLFleetNodeEnrollmentStore) SoftDeleteFleetNodesForExpiredEnrollments(ctx context.Context, now time.Time) (int64, error) {
	return s.q(ctx).SoftDeleteFleetNodesForExpiredEnrollments(ctx, sql.NullTime{Time: now, Valid: true})
}

func (s *SQLFleetNodeEnrollmentStore) UpdateLastSeen(ctx context.Context, fleetNodeID, orgID int64, now time.Time) (int64, error) {
	return s.q(ctx).UpdateFleetNodeLastSeenAt(ctx, sqlc.UpdateFleetNodeLastSeenAtParams{
		LastSeenAt: sql.NullTime{Time: now, Valid: true},
		ID:         fleetNodeID,
		OrgID:      orgID,
	})
}

func (s *SQLFleetNodeEnrollmentStore) DeletePairingsForFleetNode(ctx context.Context, fleetNodeID, orgID int64) (int64, error) {
	return s.q(ctx).DeletePairingsForFleetNode(ctx, sqlc.DeletePairingsForFleetNodeParams{
		FleetNodeID: fleetNodeID,
		OrgID:       orgID,
	})
}

func (s *SQLFleetNodeEnrollmentStore) ListDeviceIDsForFleetNode(ctx context.Context, fleetNodeID, orgID int64) ([]int64, error) {
	return s.q(ctx).ListFleetNodeDeviceIDsForRevocation(ctx, sqlc.ListFleetNodeDeviceIDsForRevocationParams{
		FleetNodeID: fleetNodeID,
		OrgID:       orgID,
	})
}

func (s *SQLFleetNodeEnrollmentStore) DeleteMinerCredentialsForFleetNode(ctx context.Context, fleetNodeID, orgID int64) (int64, error) {
	return s.q(ctx).DeleteMinerCredentialsForFleetNode(ctx, sqlc.DeleteMinerCredentialsForFleetNodeParams{
		FleetNodeID: fleetNodeID,
		OrgID:       orgID,
	})
}

func rowToPending(row sqlc.PendingEnrollment) *enrollment.PendingEnrollment {
	return &enrollment.PendingEnrollment{
		ID:          row.ID,
		CodeHash:    row.CodeHash,
		OrgID:       row.OrgID,
		CreatedBy:   row.CreatedBy,
		FleetNodeID: nullInt64ToPtr(row.FleetNodeID),
		Status:      enrollment.Status(row.Status),
		ExpiresAt:   row.ExpiresAt,
		ConsumedAt:  nullTimeToPtr(row.ConsumedAt),
		CreatedAt:   row.CreatedAt,
	}
}

func rowToFleetNode(id, orgID int64, name string, identityPubkey []byte, status string, lastSeenAt sql.NullTime, createdAt, updatedAt time.Time) *enrollment.FleetNode {
	return &enrollment.FleetNode{
		ID:               id,
		OrgID:            orgID,
		Name:             name,
		IdentityPubkey:   identityPubkey,
		EnrollmentStatus: enrollment.FleetNodeStatus(status),
		LastSeenAt:       nullTimeToPtr(lastSeenAt),
		CreatedAt:        createdAt,
		UpdatedAt:        updatedAt,
	}
}
