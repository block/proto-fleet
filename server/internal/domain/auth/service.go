package auth

import (
	"context"
	"fmt"
	"strings"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/encrypt"

	"connectrpc.com/connect"
	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"
	id "github.com/btc-mining/proto-fleet/server/internal/infrastructure/id"

	authv1 "github.com/btc-mining/proto-fleet/server/generated/grpc/auth/v1"
	onboardingv1 "github.com/btc-mining/proto-fleet/server/generated/grpc/onboarding/v1"
	stores "github.com/btc-mining/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/btc-mining/proto-fleet/server/internal/domain/token"
	"golang.org/x/crypto/bcrypt"
)

const AdminRoleName = "SUPER_ADMIN"

type Service struct {
	userStore  stores.UserStore
	transactor stores.Transactor
	tokenSvc   *token.Service
	encryptSvc *encrypt.Service
}

func NewService(
	userStore stores.UserStore,
	transactor stores.Transactor,
	tokenSvc *token.Service,
	encryptSvc *encrypt.Service,
) *Service {
	return &Service{
		userStore:  userStore,
		transactor: transactor,
		tokenSvc:   tokenSvc,
		encryptSvc: encryptSvc,
	}
}

func (s *Service) AuthenticateUser(ctx context.Context, req *authv1.AuthenticateRequest) (*authv1.AuthenticateResponse, error) {
	user, err := s.userStore.GetUserByUsername(ctx, req.Username)
	if err != nil {
		return nil, newAuthenticationFailedError()
	}

	orgs, err := s.userStore.GetOrganizationsForUser(ctx, user.ID)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("error listing user orgs: %v", err)
	}

	if len(orgs) != 1 {
		return nil, fleeterror.NewInternalErrorf("user should belong to exactly 1 org: was: %d", len(orgs))
	}

	// Compare hashed passwords
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		return nil, newAuthenticationFailedError()
	}

	// Generate and return JWT authToken
	authToken, exp, err := s.tokenSvc.GenerateClientAuthJWT(user.ID, orgs[0].ID)
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

	externalUserID := id.GenerateID()
	externalOrgID := id.GenerateID()
	orgName := generateDefaultOrgName(externalOrgID)

	minerAuthPrivateKey, err := s.tokenSvc.CreateMinerAuthPrivateKeyForOrganization()
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("error creating miner auth private key: %v", err)
	}

	encryptedMinerAuthPrivateKey, err := s.encryptSvc.Encrypt(minerAuthPrivateKey)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("error encrypting miner auth private key: %v", err)
	}

	created, err := s.transactor.RunInTxWithResult(ctx, func(ctx context.Context) (any, error) {
		hasUser, err := s.userStore.HasUser(ctx)
		if err != nil {
			return false, err
		}

		if hasUser {
			return false, nil
		}

		err = s.userStore.CreateAdminUserWithOrganization(
			ctx,
			externalUserID,
			req.Username,
			string(hashedPassword),
			orgName,
			externalOrgID,
			encryptedMinerAuthPrivateKey,
			AdminRoleName,
			"Super admin role",
		)
		userCreated := err == nil
		return userCreated, err
	})

	if err != nil {
		return nil, err
	}

	createdBool, ok := created.(bool)
	if !ok {
		return nil, fleeterror.NewInternalErrorf("unexpected result type: %T", created)
	}

	if !createdBool {
		return nil, fleeterror.NewPlainError("fleet already onboarded", connect.CodeAlreadyExists)
	}

	return &onboardingv1.CreateAdminLoginResponse{
		UserId: externalUserID,
	}, nil
}

func (s *Service) UpdateUsername(ctx context.Context, username string) error {
	trimmedUsername := strings.TrimSpace(username)
	if trimmedUsername == "" {
		return fleeterror.NewInvalidArgumentError("username cannot be empty")
	}

	claims, err := token.GetClientAuthJWTClaims(ctx)
	if err != nil {
		return err
	}

	return s.userStore.UpdateUserUsername(ctx, claims.UserID, trimmedUsername)
}

func (s *Service) UpdatePassword(ctx context.Context, r *authv1.UpdatePasswordRequest) error {
	claims, err := token.GetClientAuthJWTClaims(ctx)
	if err != nil {
		return err
	}

	return s.transactor.RunInTx(ctx, func(ctx context.Context) error {
		user, err := s.userStore.GetUserByID(ctx, claims.UserID)
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

		if err = s.userStore.UpdateUserPassword(ctx, user.ID, string(hashedPassword)); err != nil {
			return fleeterror.NewInternalErrorf("error updating password for user_id: %d, because: %v", claims.UserID, err)
		}

		return nil
	})
}

func (s *Service) GetUserAuditInfo(ctx context.Context) (*authv1.GetUserAuditInfoResponse, error) {
	claims, err := token.GetClientAuthJWTClaims(ctx)
	if err != nil {
		return nil, err
	}

	date, err := s.userStore.PasswordUpdatedAt(ctx, claims.UserID)
	if err != nil {
		return nil, err
	}

	protoTimestamp := timestamppb.New(date)

	return &authv1.GetUserAuditInfoResponse{Info: &authv1.UserAuditInfo{PasswordUpdatedAt: protoTimestamp}}, nil
}

// generateDefaultOrgName returns a default organization name suffixed with the first 8 chars or the orgID
func generateDefaultOrgName(orgID string) string {
	return fmt.Sprintf("Organization %s", orgID[:8])
}
