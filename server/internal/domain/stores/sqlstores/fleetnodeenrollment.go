package sqlstores

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetnodeenrollment"
)

var _ fleetnodeenrollment.Store = &SQLFleetNodeEnrollmentStore{}

type SQLFleetNodeEnrollmentStore struct {
	SQLConnectionManager
}

func NewSQLFleetNodeEnrollmentStore(conn *sql.DB) *SQLFleetNodeEnrollmentStore {
	return &SQLFleetNodeEnrollmentStore{SQLConnectionManager: NewSQLConnectionManager(conn)}
}

func (s *SQLFleetNodeEnrollmentStore) q(ctx context.Context) *sqlc.Queries {
	return s.GetQueries(ctx)
}

func (s *SQLFleetNodeEnrollmentStore) CreatePendingEnrollment(ctx context.Context, codeHash string, orgID, createdBy int64, expiresAt time.Time) (*fleetnodeenrollment.PendingEnrollment, error) {
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

func (s *SQLFleetNodeEnrollmentStore) GetPendingEnrollmentByCodeHash(ctx context.Context, codeHash string) (*fleetnodeenrollment.PendingEnrollment, error) {
	row, err := s.q(ctx).GetPendingEnrollmentByCodeHash(ctx, codeHash)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fleeterror.NewNotFoundError("pending enrollment not found")
		}
		return nil, err
	}
	return rowToPending(row), nil
}

func (s *SQLFleetNodeEnrollmentStore) GetPendingEnrollmentByAgent(ctx context.Context, agentID, orgID int64) (*fleetnodeenrollment.PendingEnrollment, error) {
	row, err := s.q(ctx).GetPendingEnrollmentByAgent(ctx, sqlc.GetPendingEnrollmentByAgentParams{
		AgentID: sql.NullInt64{Int64: agentID, Valid: true},
		OrgID:   orgID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fleeterror.NewNotFoundError("pending enrollment not found")
		}
		return nil, err
	}
	return rowToPending(row), nil
}

func (s *SQLFleetNodeEnrollmentStore) BindEnrollmentToAgent(ctx context.Context, enrollmentID, agentID int64) (int64, error) {
	return s.q(ctx).BindEnrollmentToAgent(ctx, sqlc.BindEnrollmentToAgentParams{
		AgentID: sql.NullInt64{Int64: agentID, Valid: true},
		ID:      enrollmentID,
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

func (s *SQLFleetNodeEnrollmentStore) CancelEnrollmentForAgent(ctx context.Context, agentID, orgID int64, consumedAt time.Time) (int64, error) {
	return s.q(ctx).CancelEnrollmentForAgent(ctx, sqlc.CancelEnrollmentForAgentParams{
		ConsumedAt: sql.NullTime{Time: consumedAt, Valid: true},
		AgentID:    sql.NullInt64{Int64: agentID, Valid: true},
		OrgID:      orgID,
	})
}

func (s *SQLFleetNodeEnrollmentStore) SweepExpiredEnrollments(ctx context.Context, now time.Time) (int64, error) {
	return s.q(ctx).SweepExpiredEnrollments(ctx, now)
}

func (s *SQLFleetNodeEnrollmentStore) CreateAgent(ctx context.Context, orgID int64, name string, identityPubkey, minerSigningPubkey []byte) (*fleetnodeenrollment.FleetNode, error) {
	row, err := s.q(ctx).CreateAgent(ctx, sqlc.CreateAgentParams{
		OrgID:              orgID,
		Name:               name,
		IdentityPubkey:     identityPubkey,
		MinerSigningPubkey: minerSigningPubkey,
	})
	if err != nil {
		return nil, err
	}
	return rowToAgent(row.ID, row.OrgID, row.Name, row.IdentityPubkey, row.MinerSigningPubkey, row.EnrollmentStatus, row.LastSeenAt, row.CreatedAt, row.UpdatedAt), nil
}

func (s *SQLFleetNodeEnrollmentStore) GetAgentByID(ctx context.Context, agentID, orgID int64) (*fleetnodeenrollment.FleetNode, error) {
	row, err := s.q(ctx).GetAgentByID(ctx, sqlc.GetAgentByIDParams{ID: agentID, OrgID: orgID})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fleeterror.NewNotFoundError("agent not found")
		}
		return nil, err
	}
	return rowToAgent(row.ID, row.OrgID, row.Name, row.IdentityPubkey, row.MinerSigningPubkey, row.EnrollmentStatus, row.LastSeenAt, row.CreatedAt, row.UpdatedAt), nil
}

func (s *SQLFleetNodeEnrollmentStore) LockAgentByID(ctx context.Context, agentID, orgID int64) (*fleetnodeenrollment.FleetNode, error) {
	row, err := s.q(ctx).LockAgentByID(ctx, sqlc.LockAgentByIDParams{ID: agentID, OrgID: orgID})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fleeterror.NewNotFoundError("agent not found")
		}
		return nil, err
	}
	return rowToAgent(row.ID, row.OrgID, row.Name, row.IdentityPubkey, row.MinerSigningPubkey, row.EnrollmentStatus, row.LastSeenAt, row.CreatedAt, row.UpdatedAt), nil
}

func (s *SQLFleetNodeEnrollmentStore) GetAgentByIDUnscoped(ctx context.Context, agentID int64) (*fleetnodeenrollment.FleetNode, error) {
	row, err := s.q(ctx).GetAgentByIDUnscoped(ctx, agentID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fleeterror.NewNotFoundError("agent not found")
		}
		return nil, err
	}
	return rowToAgent(row.ID, row.OrgID, row.Name, row.IdentityPubkey, row.MinerSigningPubkey, row.EnrollmentStatus, row.LastSeenAt, row.CreatedAt, row.UpdatedAt), nil
}

func (s *SQLFleetNodeEnrollmentStore) ListAgentsForOrganization(ctx context.Context, orgID int64) ([]fleetnodeenrollment.FleetNodeListing, error) {
	rows, err := s.q(ctx).ListAgentsForOrganization(ctx, orgID)
	if err != nil {
		return nil, err
	}
	out := make([]fleetnodeenrollment.FleetNodeListing, 0, len(rows))
	for _, r := range rows {
		out = append(out, fleetnodeenrollment.FleetNodeListing{
			FleetNode:               *rowToAgent(r.ID, r.OrgID, r.Name, r.IdentityPubkey, r.MinerSigningPubkey, r.EnrollmentStatus, r.LastSeenAt, r.CreatedAt, r.UpdatedAt),
			PendingEnrollmentStatus: fleetnodeenrollment.Status(r.PendingEnrollmentStatus),
		})
	}
	return out, nil
}

func (s *SQLFleetNodeEnrollmentStore) SetAgentEnrollmentStatus(ctx context.Context, status fleetnodeenrollment.FleetNodeStatus, agentID, orgID int64) (int64, error) {
	return s.q(ctx).SetAgentEnrollmentStatus(ctx, sqlc.SetAgentEnrollmentStatusParams{
		EnrollmentStatus: string(status),
		ID:               agentID,
		OrgID:            orgID,
	})
}

func (s *SQLFleetNodeEnrollmentStore) SoftDeleteAgent(ctx context.Context, agentID, orgID int64, deletedAt time.Time) (int64, error) {
	return s.q(ctx).SoftDeleteAgent(ctx, sqlc.SoftDeleteAgentParams{
		DeletedAt: sql.NullTime{Time: deletedAt, Valid: true},
		ID:        agentID,
		OrgID:     orgID,
	})
}

func (s *SQLFleetNodeEnrollmentStore) SoftDeleteAgentsForExpiredEnrollments(ctx context.Context, now time.Time) (int64, error) {
	return s.q(ctx).SoftDeleteAgentsForExpiredEnrollments(ctx, sql.NullTime{Time: now, Valid: true})
}

func rowToPending(row sqlc.PendingEnrollment) *fleetnodeenrollment.PendingEnrollment {
	return &fleetnodeenrollment.PendingEnrollment{
		ID:          row.ID,
		CodeHash:    row.CodeHash,
		OrgID:       row.OrgID,
		CreatedBy:   row.CreatedBy,
		FleetNodeID: nullInt64ToPtr(row.AgentID),
		Status:      fleetnodeenrollment.Status(row.Status),
		ExpiresAt:   row.ExpiresAt,
		ConsumedAt:  nullTimeToPtr(row.ConsumedAt),
		CreatedAt:   row.CreatedAt,
	}
}

func rowToAgent(id, orgID int64, name string, identityPubkey, minerSigningPubkey []byte, status string, lastSeenAt sql.NullTime, createdAt, updatedAt time.Time) *fleetnodeenrollment.FleetNode {
	return &fleetnodeenrollment.FleetNode{
		ID:                 id,
		OrgID:              orgID,
		Name:               name,
		IdentityPubkey:     identityPubkey,
		MinerSigningPubkey: minerSigningPubkey,
		EnrollmentStatus:   fleetnodeenrollment.FleetNodeStatus(status),
		LastSeenAt:         nullTimeToPtr(lastSeenAt),
		CreatedAt:          createdAt,
		UpdatedAt:          updatedAt,
	}
}
