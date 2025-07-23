package sqlstores

import (
	"context"
	"database/sql"
	"time"

	"github.com/btc-mining/proto-fleet/server/generated/sqlc"
	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"
	"github.com/btc-mining/proto-fleet/server/internal/domain/stores/interfaces"
)

var _ interfaces.UserStore = &SQLUserStore{}

type SQLUserStore struct {
	SQLConnectionManager
}

func NewSQLUserStore(conn *sql.DB) *SQLUserStore {
	return &SQLUserStore{
		SQLConnectionManager: NewSQLConnectionManager(conn),
	}
}

func (s *SQLUserStore) getQueries(ctx context.Context) *sqlc.Queries {
	return s.GetQueries(ctx)
}

func (s *SQLUserStore) GetUserByUsername(ctx context.Context, username string) (interfaces.User, error) {
	user, err := s.getQueries(ctx).GetUserByUsername(ctx, username)
	if err != nil {
		return interfaces.User{}, err
	}

	return interfaces.User{
		ID:           user.ID,
		UserID:       user.UserID,
		Username:     user.Username,
		PasswordHash: user.PasswordHash,
		CreatedAt:    user.CreatedAt,
		UpdatedAt:    user.UpdatedAt,
	}, nil
}

func (s *SQLUserStore) GetUserByID(ctx context.Context, userID int64) (interfaces.User, error) {
	user, err := s.getQueries(ctx).GetUserById(ctx, userID)
	if err != nil {
		return interfaces.User{}, err
	}

	return interfaces.User{
		ID:           user.ID,
		UserID:       user.UserID,
		Username:     user.Username,
		PasswordHash: user.PasswordHash,
		CreatedAt:    user.CreatedAt,
		UpdatedAt:    user.UpdatedAt,
	}, nil
}

func (s *SQLUserStore) UpdateUserPassword(ctx context.Context, userID int64, passwordHash string) error {
	return s.getQueries(ctx).UpdateUserPassword(ctx, sqlc.UpdateUserPasswordParams{
		ID:           userID,
		PasswordHash: passwordHash,
		UpdatedAt:    time.Now(),
	})
}

func (s *SQLUserStore) GetOrganizationsForUser(ctx context.Context, userID int64) ([]interfaces.Organization, error) {
	orgs, err := s.getQueries(ctx).GetOrganizationsForUser(ctx, userID)
	if err != nil {
		return nil, err
	}

	result := make([]interfaces.Organization, len(orgs))
	for i, org := range orgs {
		result[i] = interfaces.Organization{
			ID:                  org.ID,
			Name:                org.Name,
			OrgID:               org.OrgID,
			MinerAuthPrivateKey: org.MinerAuthPrivateKey,
		}
	}

	return result, nil
}

func (s *SQLUserStore) CreateAdminUserWithOrganization(ctx context.Context, userID string, username string, passwordHash string,
	orgName string, orgID string, minerAuthPrivateKey string, roleName string, roleDescription string) error {

	q := s.getQueries(ctx)

	userResult, err := q.CreateUser(ctx, sqlc.CreateUserParams{
		UserID:       userID,
		Username:     username,
		PasswordHash: passwordHash,
		CreatedAt:    time.Now(),
	})
	if err != nil {
		return fleeterror.NewInternalErrorf("error creating user: %v", err)
	}

	userInternalID, err := userResult.LastInsertId()
	if err != nil {
		return fleeterror.NewInternalErrorf("error creating user: %v", err)
	}

	orgResult, err := q.CreateOrganization(ctx, sqlc.CreateOrganizationParams{
		Name:                orgName,
		OrgID:               orgID,
		MinerAuthPrivateKey: minerAuthPrivateKey,
	})
	if err != nil {
		return fleeterror.NewInternalErrorf("error creating organization: %v", err)
	}

	orgInternalID, err := orgResult.LastInsertId()
	if err != nil {
		return fleeterror.NewInternalErrorf("error getting organization id: %v", err)
	}

	roleResult, err := q.UpsertRole(ctx, sqlc.UpsertRoleParams{
		Name: roleName,
		Description: sql.NullString{
			String: roleDescription,
			Valid:  len(roleDescription) > 0,
		},
	})
	if err != nil {
		return fleeterror.NewInternalErrorf("error creating role: %v", err)
	}

	roleID, err := roleResult.LastInsertId()
	if err != nil {
		return fleeterror.NewInternalErrorf("error getting role id: %v", err)
	}

	return q.CreateUserOrganization(ctx, sqlc.CreateUserOrganizationParams{
		UserID:         userInternalID,
		RoleID:         roleID,
		OrganizationID: orgInternalID,
	})
}

func (s *SQLUserStore) HasUser(ctx context.Context) (bool, error) {
	return s.getQueries(ctx).HasUser(ctx)
}
