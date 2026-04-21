package interfaces

//go:generate go run go.uber.org/mock/mockgen -source=user.go -destination=mocks/mock_user_store.go -package=mocks UserStore UserManagementStore

import (
	"context"
	"time"
)

type UserStore interface { //nolint:interfacebloat // GetUserByIDForUpdate is a locking counterpart of GetUserByID
	GetUserByUsername(ctx context.Context, username string) (User, error)
	GetUserByID(ctx context.Context, userID int64) (User, error)
	GetUserByIDForUpdate(ctx context.Context, userID int64) (User, error)
	GetUserByExternalID(ctx context.Context, userID string) (User, error)
	UpdateUserPassword(ctx context.Context, userID int64, passwordHash string) error
	UpdateUserUsername(ctx context.Context, userID int64, username string) error
	GetOrganizationsForUser(ctx context.Context, userID int64) ([]Organization, error)
	CreateAdminUserWithOrganization(ctx context.Context, userID string, username string, passwordHash string,
		orgName string, orgID string, minerAuthPrivateKey string, roleName string, roleDescription string) error
	HasUser(ctx context.Context) (bool, error)
	PasswordUpdatedAt(ctx context.Context, userID int64) (time.Time, error)
	GetOrganizationPrivateKey(ctx context.Context, orgID int64) (string, error)
}

// UserManagementStore provides multi-user account management operations
type UserManagementStore interface {
	CreateUser(ctx context.Context, externalUserID string, username string, passwordHash string, requiresPasswordChange bool) (int64, error)
	CreateUserOrganizationRole(ctx context.Context, userID int64, organizationID int64, roleID int64) error
	GetRoleByName(ctx context.Context, roleName string) (Role, error)
	UpdateUserPasswordAndClearPasswordChangeFlag(ctx context.Context, userID int64, passwordHash string) error
	AdminResetUserPassword(ctx context.Context, userID int64, passwordHash string) error
	SoftDeleteUser(ctx context.Context, userID int64) error
	UpdateLastLogin(ctx context.Context, userID int64) error
	ListUsersForOrganization(ctx context.Context, organizationID int64) ([]User, error)
	GetUserRoleName(ctx context.Context, userID int64, organizationID int64) (string, error)
}

type User struct {
	ID                     int64
	UserID                 string
	Username               string
	PasswordHash           string
	CreatedAt              time.Time
	UpdatedAt              time.Time
	PasswordUpdatedAt      time.Time
	LastLoginAt            time.Time
	RequiresPasswordChange bool
	RoleName               string // Only populated by ListUsersForOrganization
}

type Organization struct {
	ID                  int64
	Name                string
	OrgID               string
	MinerAuthPrivateKey string
}

type Role struct {
	ID          int64
	Name        string
	Description string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
