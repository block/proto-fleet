package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	authv1 "github.com/btc-mining/proto-fleet/server/generated/grpc/auth/v1"
	onboardingv1 "github.com/btc-mining/proto-fleet/server/generated/grpc/onboarding/v1"
	"github.com/btc-mining/proto-fleet/server/internal/domain/token"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/db"

	"github.com/btc-mining/proto-fleet/server/generated/sqlc"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidInput            = errors.New("invalid input")
	ErrUnsupportedUserMultiOrg = errors.New("unsupported user multi org")
)

const AdminRoleName = "SUPER_ADMIN"

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

func (s *Service) AuthenticateUser(ctx context.Context, req *authv1.AuthenticateRequest) (*authv1.AuthenticateResponse, error) {
	type UserOrgResult struct {
		User sqlc.User
		Org  sqlc.Organization
	}
	result, err := db.WithTransaction(ctx, s.conn, func(q *sqlc.Queries) (UserOrgResult, error) {
		u, err := q.GetUserByUsername(ctx, req.Username)
		if err != nil {
			return UserOrgResult{}, err
		}
		o, err := q.GetOrganizationsForUser(ctx, u.ID)
		if err != nil {
			return UserOrgResult{}, err
		}
		if len(o) != 1 {
			return UserOrgResult{}, ErrUnsupportedUserMultiOrg
		}
		return UserOrgResult{
			User: u,
			Org:  o[0],
		}, nil
	})
	if err != nil {
		return nil, err
	}

	// Compare hashed passwords
	if err := bcrypt.CompareHashAndPassword([]byte(result.User.PasswordHash), []byte(req.Password)); err != nil {
		return nil, fmt.Errorf("error validating password: %w", err)
	}
	// Generate and return JWT authToken
	authToken, exp, err := s.tokenSvc.GenerateJWT(result.User.ID, result.Org.ID)
	if err != nil {
		return nil, err
	}
	return &authv1.AuthenticateResponse{
		Token:       authToken,
		TokenExpiry: exp,
	}, err
}

func (s *Service) CreateAdminUser(ctx context.Context, req *onboardingv1.CreateAdminLoginRequest) (*onboardingv1.CreateAdminLoginResponse, error) {
	if len(req.Username) == 0 || len(req.Password) == 0 {
		return nil, ErrInvalidInput
	}
	// generate salted password hash
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("error generating password: %w", err)
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
			Name: AdminRoleName,
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
		return nil, fmt.Errorf("error associating user with org and role: %w", err)
	}
	return &onboardingv1.CreateAdminLoginResponse{
		UserId: externalUserID,
	}, nil
}

// generateDefaultOrgName returns a default organization name suffixed with the first 8 chars or the orgID
func generateDefaultOrgName(orgID string) string {
	return fmt.Sprintf("Organization %s", orgID[:8])
}
