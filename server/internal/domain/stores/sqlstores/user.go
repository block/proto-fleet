package sqlstores

import (
	"context"
	"database/sql"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

var _ interfaces.UserStore = &SQLUserStore{}
var _ interfaces.UserManagementStore = &SQLUserStore{}

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

// sqlTimeToTime converts sql.NullTime to time.Time using zero time for NULL values
func sqlTimeToTime(nt sql.NullTime) time.Time {
	if !nt.Valid {
		return time.Time{}
	}
	return nt.Time
}

func (s *SQLUserStore) GetUserByUsername(ctx context.Context, username string) (interfaces.User, error) {
	user, err := s.getQueries(ctx).GetUserByUsername(ctx, username)
	if err != nil {
		return interfaces.User{}, err
	}
	return toUser(user), nil
}

func (s *SQLUserStore) GetUserByID(ctx context.Context, userID int64) (interfaces.User, error) {
	user, err := s.getQueries(ctx).GetUserById(ctx, userID)
	if err != nil {
		return interfaces.User{}, err
	}
	return toUser(user), nil
}

func (s *SQLUserStore) GetUserByIDForUpdate(ctx context.Context, userID int64) (interfaces.User, error) {
	user, err := s.getQueries(ctx).GetUserByIdForUpdate(ctx, userID)
	if err != nil {
		return interfaces.User{}, err
	}
	return toUser(user), nil
}

func toUser(user sqlc.User) interfaces.User {
	return interfaces.User{
		ID:                     user.ID,
		UserID:                 user.UserID,
		Username:               user.Username,
		PasswordHash:           user.PasswordHash,
		CreatedAt:              user.CreatedAt,
		UpdatedAt:              user.UpdatedAt,
		PasswordUpdatedAt:      sqlTimeToTime(user.PasswordUpdatedAt),
		LastLoginAt:            sqlTimeToTime(user.LastLoginAt),
		RequiresPasswordChange: user.RequiresPasswordChange,
	}
}

func (s *SQLUserStore) UpdateUserPassword(ctx context.Context, userID int64, passwordHash string) error {
	return s.getQueries(ctx).UpdateUserPassword(ctx, sqlc.UpdateUserPasswordParams{
		ID:           userID,
		PasswordHash: passwordHash,
	})
}

func (s *SQLUserStore) UpdateUserUsername(ctx context.Context, userID int64, username string) error {
	return s.getQueries(ctx).UpdateUserUsername(ctx, sqlc.UpdateUserUsernameParams{Username: username, ID: userID})
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

	userInternalID, err := q.CreateUser(ctx, sqlc.CreateUserParams{
		UserID:       userID,
		Username:     username,
		PasswordHash: passwordHash,
		CreatedAt:    time.Now(),
	})
	if err != nil {
		return fleeterror.NewInternalErrorf("error creating user: %v", err)
	}

	orgInternalID, err := q.CreateOrganization(ctx, sqlc.CreateOrganizationParams{
		Name:                orgName,
		OrgID:               orgID,
		MinerAuthPrivateKey: minerAuthPrivateKey,
	})
	if err != nil {
		return fleeterror.NewInternalErrorf("error creating organization: %v", err)
	}

	roleID, err := q.UpsertRole(ctx, sqlc.UpsertRoleParams{
		Name: roleName,
		Description: sql.NullString{
			String: roleDescription,
			Valid:  len(roleDescription) > 0,
		},
	})
	if err != nil {
		return fleeterror.NewInternalErrorf("error creating role: %v", err)
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

func (s *SQLUserStore) PasswordUpdatedAt(ctx context.Context, userID int64) (time.Time, error) {
	result, err := s.getQueries(ctx).PasswordUpdatedAt(ctx, userID)
	if err != nil {
		return time.Time{}, err
	}
	return sqlTimeToTime(result), nil
}

func (s *SQLUserStore) GetOrganizationPrivateKey(ctx context.Context, orgID int64) (string, error) {
	return s.getQueries(ctx).GetOrganizationPrivateKey(ctx, orgID)
}

func (s *SQLUserStore) GetUserByExternalID(ctx context.Context, userID string) (interfaces.User, error) {
	user, err := s.getQueries(ctx).GetUserByExternalId(ctx, userID)
	if err != nil {
		return interfaces.User{}, err
	}
	return toUser(user), nil
}

func (s *SQLUserStore) CreateUser(ctx context.Context, externalUserID string, username string, passwordHash string, requiresPasswordChange bool) (int64, error) {
	return s.getQueries(ctx).CreateUser(ctx, sqlc.CreateUserParams{
		UserID:                 externalUserID,
		Username:               username,
		PasswordHash:           passwordHash,
		RequiresPasswordChange: requiresPasswordChange,
		CreatedAt:              time.Now(),
	})
}

func (s *SQLUserStore) CreateUserOrganizationRole(ctx context.Context, userID int64, organizationID int64, roleID int64) error {
	return s.getQueries(ctx).CreateUserOrganization(ctx, sqlc.CreateUserOrganizationParams{
		UserID:         userID,
		OrganizationID: organizationID,
		RoleID:         roleID,
	})
}

func (s *SQLUserStore) GetRoleByName(ctx context.Context, roleName string) (interfaces.Role, error) {
	role, err := s.getQueries(ctx).GetRoleByName(ctx, roleName)
	if err != nil {
		return interfaces.Role{}, err
	}

	return interfaces.Role{
		ID:          role.ID,
		Name:        role.Name,
		Description: role.Description.String,
		CreatedAt:   role.CreatedAt,
		UpdatedAt:   role.UpdatedAt,
	}, nil
}

func (s *SQLUserStore) UpdateUserPasswordAndClearPasswordChangeFlag(ctx context.Context, userID int64, passwordHash string) error {
	return s.getQueries(ctx).UpdateUserPasswordAndFlag(ctx, sqlc.UpdateUserPasswordAndFlagParams{
		PasswordHash: passwordHash,
		ID:           userID,
	})
}

func (s *SQLUserStore) AdminResetUserPassword(ctx context.Context, userID int64, passwordHash string) error {
	return s.getQueries(ctx).AdminResetUserPassword(ctx, sqlc.AdminResetUserPasswordParams{
		PasswordHash: passwordHash,
		ID:           userID,
	})
}

func (s *SQLUserStore) SoftDeleteUser(ctx context.Context, userID int64) error {
	return s.getQueries(ctx).SoftDeleteUser(ctx, userID)
}

func (s *SQLUserStore) UpdateLastLogin(ctx context.Context, userID int64) error {
	return s.getQueries(ctx).UpdateLastLogin(ctx, userID)
}

func (s *SQLUserStore) ListUsersForOrganization(ctx context.Context, organizationID int64) ([]interfaces.User, error) {
	users, err := s.getQueries(ctx).ListUsersForOrganization(ctx, organizationID)
	if err != nil {
		return nil, err
	}

	result := make([]interfaces.User, len(users))
	for i, user := range users {
		result[i] = interfaces.User{
			ID:                     user.ID,
			UserID:                 user.UserID,
			Username:               user.Username,
			PasswordUpdatedAt:      sqlTimeToTime(user.PasswordUpdatedAt),
			LastLoginAt:            sqlTimeToTime(user.LastLoginAt),
			RoleName:               user.RoleName,
			RequiresPasswordChange: user.RequiresPasswordChange,
			CreatedAt:              user.CreatedAt,
		}
	}

	return result, nil
}

func (s *SQLUserStore) GetUserRoleName(ctx context.Context, userID int64, organizationID int64) (string, error) {
	return s.getQueries(ctx).GetUserRoleName(ctx, sqlc.GetUserRoleNameParams{
		UserID:         userID,
		OrganizationID: organizationID,
	})
}
