package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/btc-mining/miner-firmware/fleet/internal/domain/token"
	"github.com/btc-mining/miner-firmware/fleet/internal/infrastructure/db"
	"time"

	"github.com/btc-mining/miner-firmware/fleet/generated/sqlc"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

type AuthenticateUserRequest struct {
	Username string
	Password string
}

type CreateAdminUserRequest struct {
	Username string
	Password string
}

type Token string

type UserID string

var (
	ErrInvalidInput = errors.New("invalid input")
)

const adminRoleName = "SUPER_ADMIN"

type Service struct {
	conn     *sql.DB
	tokenSvc *token.Service
}

func NewService(conn *sql.DB, tokenSvc *token.Service) *Service {
	return &Service{
		tokenSvc: tokenSvc,
		conn:     conn,
	}
}

func (s *Service) AuthenticateUser(ctx context.Context, req *AuthenticateUserRequest) (Token, error) {
	user, err := db.WithTransaction(ctx, s.conn, func(q *sqlc.Queries) (sqlc.GetUserByUsernameRow, error) {
		return q.GetUserByUsername(ctx, req.Username)
	})

	if err != nil {
		return "", err
	}

	// Compare hashed passwords
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		return "", fmt.Errorf("error validating password: %w", err)
	}

	// Generate and return JWT authToken
	authToken, err := s.tokenSvc.GenerateJWT(user.UserID)
	if err != nil {
		return "", err
	}
	return Token(authToken), err
}

func (s *Service) CreateAdminUser(ctx context.Context, req *CreateAdminUserRequest) (UserID, error) {
	if len(req.Username) == 0 || len(req.Password) == 0 {
		return "", ErrInvalidInput
	}
	// generate salted password hash
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("error generating password: %w", err)
	}

	externalUserID := uuid.New().String()
	externalOrgID := uuid.New().String()
	orgName := generateDefaultOrgName(externalOrgID)

	err = db.WithTransactionNoResult(ctx, s.conn, func(q *sqlc.Queries) error {
		// create user
		result, err := q.CreateUser(ctx, sqlc.CreateUserParams{
			UserID:       externalUserID,
			Username:     req.Username,
			PasswordHash: string(hashedPassword),
			CreatedAt:    time.Now(),
		})
		if err != nil {
			return err
		}
		userInternalID, err := result.LastInsertId()
		if err != nil {
			return fmt.Errorf("error creating user: %w", err)
		}

		// create organization
		orgResult, err := q.CreateOrganization(ctx, sqlc.CreateOrganizationParams{
			Name:  orgName,
			OrgID: externalOrgID,
		})
		if err != nil {
			return fmt.Errorf("error creating org: %w", err)
		}
		orgID, err := orgResult.LastInsertId()
		if err != nil {
			return fmt.Errorf("error fetching org id: %w", err)
		}

		// create role
		roleResult, err := q.UpsertRole(ctx, sqlc.UpsertRoleParams{
			Name: adminRoleName,
			Description: sql.NullString{
				String: "Super admin role",
				Valid:  true,
			},
		})
		if err != nil {
			return fmt.Errorf("error creating role: %w", err)
		}
		roleID, err := roleResult.LastInsertId()
		if err != nil {
			return fmt.Errorf("error fetching role id: %w", err)
		}

		// associate user with organization and role
		err = q.CreateUserOrganization(ctx, sqlc.CreateUserOrganizationParams{
			UserID:         userInternalID,
			RoleID:         roleID,
			OrganizationID: orgID,
		})
		return err
	})

	if err != nil {
		return "", fmt.Errorf("error associating user with org and role: %w", err)
	}
	return UserID(externalUserID), nil
}

// generateDefaultOrgName returns a default organization name suffixed with the first 8 chars or the orgID
func generateDefaultOrgName(orgID string) string {
	return fmt.Sprintf("Organization %s", orgID[:8])
}
