package domain

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/rand"
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

type AuthService struct {
	tokenSvc *TokenService
}

func NewAuthService(tokenSvc *TokenService) *AuthService {
	return &AuthService{
		tokenSvc: tokenSvc,
	}
}

func (s *AuthService) AuthenticateUser(ctx context.Context, q *sqlc.Queries, req *AuthenticateUserRequest) (Token, error) {
	// Fetch user from the database
	user, err := q.GetUserByUsername(ctx, req.Username)
	if err != nil {
		return "", err
	}

	// Compare hashed passwords
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		return "", fmt.Errorf("error validating password: %w", err)
	}

	// Generate and return JWT token
	token, err := s.tokenSvc.GenerateJWT(user.UserID)
	if err != nil {
		return "", err
	}
	return Token(token), err
}

func (s *AuthService) CreateAdminUser(ctx context.Context, q *sqlc.Queries, req *CreateAdminUserRequest) (UserID, error) {
	if len(req.Username) == 0 || len(req.Password) == 0 {
		return "", ErrInvalidInput
	}
	// generate salted password hash
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("error generating password: %w", err)
	}

	// create user
	externalUserID := uuid.New().String()
	result, err := q.CreateUser(ctx, sqlc.CreateUserParams{
		UserID:       externalUserID,
		Username:     req.Username,
		PasswordHash: string(hashedPassword),
		CreatedAt:    time.Now(),
	})
	if err != nil {
		return "", err
	}
	userInternalID, err := result.LastInsertId()
	if err != nil {
		return "", fmt.Errorf("error creating user: %w", err)
	}

	// create organization
	orgName := generateDefaultOrgName()
	externalOrgID := uuid.New().String()
	orgResult, err := q.CreateOrganization(ctx, sqlc.CreateOrganizationParams{
		Name:  orgName,
		OrgID: externalOrgID,
	})
	if err != nil {
		return "", fmt.Errorf("error creating org: %w", err)
	}
	orgID, err := orgResult.LastInsertId()
	if err != nil {
		return "", fmt.Errorf("error fetching org id: %w", err)
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
		return "", fmt.Errorf("error creating role: %w", err)
	}
	roleID, err := roleResult.LastInsertId()
	if err != nil {
		return "", fmt.Errorf("error fetching role id: %w", err)
	}

	// associate user with organization and role
	err = q.CreateUserOrganization(ctx, sqlc.CreateUserOrganizationParams{
		UserID:         userInternalID,
		RoleID:         roleID,
		OrganizationID: orgID,
	})
	if err != nil {
		return "", fmt.Errorf("error associating user with org and role: %w", err)
	}
	return UserID(externalUserID), nil
}

// generateDefaultOrgName returns a default organization name with a random suffix
func generateDefaultOrgName() string {
	seed := time.Now().UnixNano()
	r := rand.New(rand.NewSource(seed))
	suffix := r.Intn(9000) + 1000 // generates a 4-digit number (1000–9999)
	return fmt.Sprintf("Organization %d", suffix)
}
