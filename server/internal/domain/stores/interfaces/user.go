package interfaces

import (
	"context"
	"time"
)

type UserStore interface {
	GetUserByUsername(ctx context.Context, username string) (User, error)
	GetUserByID(ctx context.Context, userID int64) (User, error)
	UpdateUserPassword(ctx context.Context, userID int64, passwordHash string) error
	UpdateUserUsername(ctx context.Context, userID int64, username string) error
	GetOrganizationsForUser(ctx context.Context, userID int64) ([]Organization, error)
	CreateAdminUserWithOrganization(ctx context.Context, userID string, username string, passwordHash string,
		orgName string, orgID string, minerAuthPrivateKey string, roleName string, roleDescription string) error
	HasUser(ctx context.Context) (bool, error)
	PasswordUpdatedAt(ctx context.Context, userID int64) (time.Time, error)
}

type User struct {
	ID                int64
	UserID            string
	Username          string
	PasswordHash      string
	CreatedAt         time.Time
	UpdatedAt         time.Time
	PasswordUpdatedAt time.Time
}

type Organization struct {
	ID                  int64
	Name                string
	OrgID               string
	MinerAuthPrivateKey string
}
