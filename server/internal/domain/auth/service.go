package auth

import (
	"connectrpc.com/connect"
	"context"
	"database/sql"
	"fmt"
	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"
	"time"

	authv1 "github.com/btc-mining/proto-fleet/server/generated/grpc/auth/v1"
	onboardingv1 "github.com/btc-mining/proto-fleet/server/generated/grpc/onboarding/v1"
	"github.com/btc-mining/proto-fleet/server/internal/domain/token"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/db"

	"github.com/btc-mining/proto-fleet/server/generated/sqlc"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
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
			return UserOrgResult{}, newAuthenticationFailedError()
		}
		o, err := q.GetOrganizationsForUser(ctx, u.ID)
		if err != nil {
			return UserOrgResult{}, fleeterror.NewInternalErrorf("error listing user orgs: %v", err)
		}
		if len(o) != 1 {
			return UserOrgResult{}, fleeterror.NewInternalErrorf("user should belong to exactly 1 org: was: %d", len(o))
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
		return nil, newAuthenticationFailedError()
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

func newAuthenticationFailedError() fleeterror.FleetError {
	return fleeterror.NewErrorWithEndpointCode(
		"authentication failed, either the user does not exist, or the password is invalid",
		connect.CodeUnauthenticated,
		int32(authv1.AuthenticateErrorCode_AUTHENTICATE_ERROR_CODE_INVALID_USER_OR_PASSWORD),
	)
}

func (s *Service) CreateAdminUser(ctx context.Context, req *onboardingv1.CreateAdminLoginRequest) (*onboardingv1.CreateAdminLoginResponse, error) {
	if len(req.Username) == 0 {
		return nil, fleeterror.NewInvalidArgumentError("username is required but not provided")
	}

	if len(req.Password) == 0 {
		return nil, fleeterror.NewInvalidArgumentError("password is required but not provided")
	}

	// generate salted password hash
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("error generating password: %v", err)
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
			return fleeterror.NewInternalErrorf("error creating user: %v", err)
		}
		userInternalID, err := result.LastInsertId()
		if err != nil {
			return fleeterror.NewInternalErrorf("error creating user: %v", err)
		}

		// create organization
		orgResult, err := q.CreateOrganization(ctx, sqlc.CreateOrganizationParams{
			Name:  orgName,
			OrgID: externalOrgID,
		})
		if err != nil {
			return fleeterror.NewInternalErrorf("error creating org: %v", err)
		}
		orgID, err := orgResult.LastInsertId()
		if err != nil {
			return fleeterror.NewInternalErrorf("error fetching org id: %v", err)
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
			return fleeterror.NewInternalErrorf("error creating role: %v", err)
		}
		roleID, err := roleResult.LastInsertId()
		if err != nil {
			return fleeterror.NewInternalErrorf("error fetching role id: %v", err)
		}

		// associate user with organization and role
		err = q.CreateUserOrganization(ctx, sqlc.CreateUserOrganizationParams{
			UserID:         userInternalID,
			RoleID:         roleID,
			OrganizationID: orgID,
		})

		if err != nil {
			return fleeterror.NewInternalErrorf("error creating org: %v", err)
		}

		return nil
	})

	if err != nil {
		return nil, fleeterror.NewInternalErrorf("error associating user with org and role: %v", err)
	}
	return &onboardingv1.CreateAdminLoginResponse{
		UserId: externalUserID,
	}, nil
}

func (s *Service) UpdatePassword(ctx context.Context, r *authv1.UpdatePasswordRequest) error {
	claims, err := token.GetJWTClaims(ctx)
	if err != nil {
		return err
	}

	return db.WithTransactionNoResult(ctx, s.conn, func(q *sqlc.Queries) error {
		user, err := q.GetUserById(ctx, claims.UserID)
		if err != nil {
			return fleeterror.NewForbiddenErrorf("error getting user by id, user_id: %d, error: %v", claims.UserID, err)
		}

		if err = bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(r.CurrentPassword)); err != nil {
			return fleeterror.NewErrorWithEndpointCode(
				"old password is not valid",
				connect.CodeInvalidArgument,
				int32(authv1.UpdatePasswordErrorCode_UPDATE_PASSWORD_ERROR_CODE_INVALID_OLD_PASSWORD),
			)
		}

		if r.CurrentPassword == r.NewPassword {
			return fleeterror.NewErrorWithEndpointCode(
				"new password is the same as old password",
				connect.CodeInvalidArgument,
				int32(authv1.UpdatePasswordErrorCode_UPDATE_PASSWORD_ERROR_CODE_NEW_PASSWORD_SAME_AS_OLD_PASSWORD),
			)
		}

		// generate salted password hash
		hashedPassword, err := bcrypt.GenerateFromPassword([]byte(r.NewPassword), bcrypt.DefaultCost)
		if err != nil {
			return fleeterror.NewInternalErrorf("error generating hash of new password for user_id: %d, because: %v", claims.UserID, err)
		}

		if err = q.UpdateUserPassword(ctx, sqlc.UpdateUserPasswordParams{
			ID:           user.ID,
			PasswordHash: string(hashedPassword),
			UpdatedAt:    time.Now(),
		}); err != nil {
			return fleeterror.NewInternalErrorf("error updating password for user_id: %d, because: %v", claims.UserID, err)
		}
		return nil
	})
}

// generateDefaultOrgName returns a default organization name suffixed with the first 8 chars or the orgID
func generateDefaultOrgName(orgID string) string {
	return fmt.Sprintf("Organization %s", orgID[:8])
}
