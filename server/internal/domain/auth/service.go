package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/block/proto-fleet/server/internal/infrastructure/encrypt"

	"connectrpc.com/connect"
	"github.com/block/proto-fleet/server/internal/domain/activity"
	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/session"
	id "github.com/block/proto-fleet/server/internal/infrastructure/id"

	"github.com/jackc/pgx/v5/pgconn"

	authv1 "github.com/block/proto-fleet/server/generated/grpc/auth/v1"
	onboardingv1 "github.com/block/proto-fleet/server/generated/grpc/onboarding/v1"
	stores "github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/token"
	"golang.org/x/crypto/bcrypt"
)

const (
	// SuperAdminRoleName is the role name for super admin users who have full system access
	SuperAdminRoleName = "SUPER_ADMIN"
	// AdminRoleName is the role name for admin users with organizational management privileges
	AdminRoleName = "ADMIN"
)

type Service struct {
	userStore           stores.UserStore
	userManagementStore stores.UserManagementStore
	transactor          stores.Transactor
	tokenSvc            *token.Service
	sessionSvc          *session.Service
	encryptSvc          *encrypt.Service
	activitySvc         *activity.Service
}

func NewService(
	userStore stores.UserStore,
	userManagementStore stores.UserManagementStore,
	transactor stores.Transactor,
	tokenSvc *token.Service,
	sessionSvc *session.Service,
	encryptSvc *encrypt.Service,
	activitySvc *activity.Service,
) *Service {
	return &Service{
		userStore:           userStore,
		userManagementStore: userManagementStore,
		transactor:          transactor,
		tokenSvc:            tokenSvc,
		sessionSvc:          sessionSvc,
		encryptSvc:          encryptSvc,
		activitySvc:         activitySvc,
	}
}

func strPtr(s string) *string { return &s }

func (s *Service) logActivity(ctx context.Context, event activitymodels.Event) {
	if s.activitySvc != nil {
		s.activitySvc.Log(ctx, event)
	}
}

// AuthenticateUser validates credentials, creates a session, and returns user info with a session cookie.
func (s *Service) AuthenticateUser(ctx context.Context, req *authv1.AuthenticateRequest, userAgent, ipAddress string) (*authv1.AuthenticateResponse, *http.Cookie, error) {
	if req.Username == "" || utf8.RuneCountInString(req.Username) > 255 {
		return nil, nil, newAuthenticationFailedError()
	}

	user, err := s.userStore.GetUserByUsername(ctx, req.Username)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) && !fleeterror.IsNotFoundError(err) {
			slog.Error("error looking up user", "username", req.Username, "error", err)
			return nil, nil, fleeterror.NewInternalErrorf("authentication service unavailable")
		}
		s.logActivity(ctx, activitymodels.Event{
			Category:     activitymodels.CategoryAuth,
			Type:         "login_failed",
			Description:  "Login failed",
			Result:       activitymodels.ResultFailure,
			ErrorMessage: strPtr("invalid credentials"),
			Username:     &req.Username,
		})
		return nil, nil, newAuthenticationFailedError()
	}

	orgs, err := s.userStore.GetOrganizationsForUser(ctx, user.ID)
	if err != nil {
		return nil, nil, fleeterror.NewInternalErrorf("error listing user orgs: %v", err)
	}

	if len(orgs) != 1 {
		return nil, nil, fleeterror.NewInternalErrorf("user should belong to exactly 1 org: was: %d", len(orgs))
	}

	// Compare hashed passwords
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		// User exists but password wrong — include user_id and org_id
		s.logActivity(ctx, activitymodels.Event{
			Category:       activitymodels.CategoryAuth,
			Type:           "login_failed",
			Description:    "Login failed",
			Result:         activitymodels.ResultFailure,
			ErrorMessage:   strPtr("invalid credentials"),
			UserID:         &user.UserID,
			Username:       &user.Username,
			OrganizationID: &orgs[0].ID,
		})
		return nil, nil, newAuthenticationFailedError()
	}

	// Update last login timestamp (non-critical, don't fail auth if this fails)
	if err := s.userManagementStore.UpdateLastLogin(ctx, user.ID); err != nil {
		slog.Warn("failed to update last login timestamp", "user_id", user.ID, "error", err)
	}

	// Create session
	sess, err := s.sessionSvc.Create(ctx, user.ID, orgs[0].ID, userAgent, ipAddress)
	if err != nil {
		return nil, nil, err
	}

	// Get user's role
	roleName, err := s.userManagementStore.GetUserRoleName(ctx, user.ID, orgs[0].ID)
	if err != nil {
		return nil, nil, fleeterror.NewInternalErrorf("error getting user role: %v", err)
	}

	// Get password updated timestamp
	passwordUpdatedAt, err := s.userStore.PasswordUpdatedAt(ctx, user.ID)
	if err != nil {
		return nil, nil, fleeterror.NewInternalErrorf("error getting password updated timestamp: %v", err)
	}

	cookie := s.sessionSvc.CreateCookie(sess.SessionID)

	s.logActivity(ctx, activitymodels.Event{
		Category:       activitymodels.CategoryAuth,
		Type:           "login",
		Description:    "Login",
		UserID:         &user.UserID,
		Username:       &user.Username,
		OrganizationID: &orgs[0].ID,
	})

	return &authv1.AuthenticateResponse{
		// SessionExpiry is provided for client-side UI purposes (showing remaining time, triggering
		// re-auth prompts). The actual session validation happens server-side via the HTTP-only cookie.
		SessionExpiry: sess.ExpiresAt.Unix(),
		UserInfo: &authv1.UserInfo{
			UserId:                 user.UserID,
			Username:               user.Username,
			PasswordUpdatedAt:      timestamppb.New(passwordUpdatedAt),
			LastLoginAt:            toTimestampProto(user.LastLoginAt),
			Role:                   roleName,
			RequiresPasswordChange: user.RequiresPasswordChange,
		},
	}, cookie, nil
}

// Logout invalidates the current session and returns a cookie to clear the session.
func (s *Service) Logout(ctx context.Context) (*http.Cookie, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}

	if info.AuthMethod == session.AuthMethodAPIKey {
		return nil, fleeterror.NewPlainError("logout is not supported for API key authentication; revoke the key instead", connect.CodeFailedPrecondition)
	}

	if err := s.sessionSvc.Revoke(ctx, info.SessionID); err != nil {
		// Truncate session ID in logs to avoid leaking full identifier
		truncatedID := info.SessionID
		if len(info.SessionID) > 8 {
			truncatedID = info.SessionID[:8] + "..."
		}
		slog.Warn("failed to revoke session", "session_id", truncatedID, "error", err)
		// Return error so user knows logout may not be complete server-side
		return nil, fleeterror.NewInternalErrorf("failed to revoke session: %v", err)
	}

	s.logActivity(ctx, activitymodels.Event{
		Category:       activitymodels.CategoryAuth,
		Type:           "logout",
		Description:    "Logout",
		UserID:         &info.ExternalUserID,
		Username:       &info.Username,
		OrganizationID: &info.OrganizationID,
	})

	return s.sessionSvc.CreateLogoutCookie(), nil
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
			SuperAdminRoleName,
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

	var orgID *int64
	if user, err := s.userStore.GetUserByUsername(ctx, req.Username); err == nil {
		if orgs, err := s.userStore.GetOrganizationsForUser(ctx, user.ID); err == nil && len(orgs) == 1 {
			orgID = &orgs[0].ID
		}
	}

	s.logActivity(ctx, activitymodels.Event{
		Category:       activitymodels.CategoryAuth,
		Type:           "create_admin_user",
		Description:    "Admin account created",
		ActorType:      activitymodels.ActorSystem,
		Username:       &req.Username,
		OrganizationID: orgID,
	})

	return &onboardingv1.CreateAdminLoginResponse{
		UserId: externalUserID,
	}, nil
}

func (s *Service) UpdateUsername(ctx context.Context, username string) error {
	trimmedUsername := strings.TrimSpace(username)
	if trimmedUsername == "" {
		return fleeterror.NewInvalidArgumentError("username cannot be empty")
	}

	info, err := session.GetInfo(ctx)
	if err != nil {
		return err
	}

	oldUsername := info.Username

	if err := s.userStore.UpdateUserUsername(ctx, info.UserID, trimmedUsername); err != nil {
		return err
	}

	s.logActivity(ctx, activitymodels.Event{
		Category:       activitymodels.CategoryAuth,
		Type:           "update_username",
		Description:    "Username updated",
		UserID:         &info.ExternalUserID,
		Username:       &oldUsername,
		OrganizationID: &info.OrganizationID,
		Metadata: map[string]any{
			"old_username": oldUsername,
			"new_username": trimmedUsername,
		},
	})

	return nil
}

// VerifyCredentials verifies username and password without creating a session
// Used for re-authentication in sensitive operations
func (s *Service) VerifyCredentials(ctx context.Context, username, password string) error {
	if username == "" || password == "" {
		return fleeterror.NewInvalidArgumentError("username and password are required")
	}

	user, err := s.userStore.GetUserByUsername(ctx, username)
	if err != nil {
		return fleeterror.NewForbiddenErrorf("invalid credentials")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return fleeterror.NewForbiddenErrorf("invalid credentials")
	}

	return nil
}

func (s *Service) UpdatePassword(ctx context.Context, r *authv1.UpdatePasswordRequest) error {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return err
	}

	if err := s.transactor.RunInTx(ctx, func(ctx context.Context) error {
		user, err := s.userStore.GetUserByID(ctx, info.UserID)
		if err != nil {
			return fleeterror.NewForbiddenErrorf("error getting user by id, user_id: %d, error: %v", info.UserID, err)
		}

		if err = bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(r.CurrentPassword)); err != nil {
			return fleeterror.NewErrorWithEndpointCode(
				"Invalid current password.",
				connect.CodeInvalidArgument,
				int32(authv1.UpdatePasswordErrorCode_UPDATE_PASSWORD_ERROR_CODE_INVALID_OLD_PASSWORD),
			)
		}

		if r.CurrentPassword == r.NewPassword {
			return fleeterror.NewErrorWithEndpointCode(
				"New password cannot be the same as current password.",
				connect.CodeInvalidArgument,
				int32(authv1.UpdatePasswordErrorCode_UPDATE_PASSWORD_ERROR_CODE_NEW_PASSWORD_SAME_AS_OLD_PASSWORD),
			)
		}

		// generate salted password hash
		hashedPassword, err := bcrypt.GenerateFromPassword([]byte(r.NewPassword), bcrypt.DefaultCost)
		if err != nil {
			return fleeterror.NewInternalErrorf("error generating hash of new password for user_id: %d, because: %v", info.UserID, err)
		}

		// Always clear password change requirement when user updates their own password
		if err = s.userManagementStore.UpdateUserPasswordAndClearPasswordChangeFlag(ctx, user.ID, string(hashedPassword)); err != nil {
			return fleeterror.NewInternalErrorf("error updating password for user_id: %d, because: %v", info.UserID, err)
		}

		return nil
	}); err != nil {
		return err
	}

	s.logActivity(ctx, activitymodels.Event{
		Category:       activitymodels.CategoryAuth,
		Type:           "update_password",
		Description:    "Password updated",
		UserID:         &info.ExternalUserID,
		Username:       &info.Username,
		OrganizationID: &info.OrganizationID,
	})

	return nil
}

func (s *Service) GetUserAuditInfo(ctx context.Context) (*authv1.GetUserAuditInfoResponse, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}

	date, err := s.userStore.PasswordUpdatedAt(ctx, info.UserID)
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

// checkCanManageUser checks if the current user can manage (deactivate/reset password) other users
// Only SUPER_ADMIN users can manage other users
func (s *Service) checkCanManageUser(ctx context.Context, organizationID int64) error {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return err
	}

	currentUserRoleName, err := s.userManagementStore.GetUserRoleName(ctx, info.UserID, organizationID)
	if err != nil {
		return fleeterror.NewInternalErrorf("error getting current user role: %v", err)
	}

	// Only SUPER_ADMIN users can manage other users
	if currentUserRoleName != SuperAdminRoleName {
		return fleeterror.NewErrorWithEndpointCode(
			"only super admin users can manage other user accounts",
			connect.CodePermissionDenied,
			int32(authv1.UserManagementErrorCode_USER_MANAGEMENT_ERROR_CODE_UNAUTHORIZED),
		)
	}

	return nil
}

// CreateUser creates a new user with a temporary password (Super Admin only)
func (s *Service) CreateUser(ctx context.Context, req *authv1.CreateUserRequest) (*authv1.CreateUserResponse, error) {
	// Validate username
	trimmedUsername := strings.TrimSpace(req.Username)
	if trimmedUsername == "" {
		return nil, fleeterror.NewInvalidArgumentError("username is required")
	}

	// Get current user's org
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}

	orgs, err := s.userStore.GetOrganizationsForUser(ctx, info.UserID)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("error getting user organizations: %v", err)
	}

	if len(orgs) != 1 {
		return nil, fleeterror.NewInternalErrorf("user should belong to exactly 1 org")
	}

	orgID := orgs[0].ID

	// Generate temporary password
	tempPassword, err := generateTemporaryPassword()
	if err != nil {
		return nil, err
	}

	// Hash password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(tempPassword), bcrypt.DefaultCost)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("error generating password hash: %v", err)
	}

	// Get Admin role
	role, err := s.userManagementStore.GetRoleByName(ctx, AdminRoleName)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("error getting role: %v", err)
	}

	var createdUserID string
	err = s.transactor.RunInTx(ctx, func(ctx context.Context) error {
		// Generate external user ID
		createdUserID = id.GenerateID()

		// Create user
		userID, err := s.userManagementStore.CreateUser(ctx, createdUserID, trimmedUsername, string(hashedPassword), true)
		if err != nil {
			// Check if this is a duplicate key error (PostgreSQL unique_violation code 23505)
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23505" {
				return fleeterror.NewErrorWithEndpointCode(
					"username already exists",
					connect.CodeAlreadyExists,
					int32(authv1.UserManagementErrorCode_USER_MANAGEMENT_ERROR_CODE_USERNAME_EXISTS),
				)
			}
			// For other database errors, return internal error
			return fleeterror.NewInternalErrorf("failed to create user: %v", err)
		}

		// Associate user with organization and role
		if err := s.userManagementStore.CreateUserOrganizationRole(ctx, userID, orgID, role.ID); err != nil {
			return fleeterror.NewInternalErrorf("error creating user organization role: %v", err)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	s.logActivity(ctx, activitymodels.Event{
		Category:       activitymodels.CategoryAuth,
		Type:           "create_user",
		Description:    fmt.Sprintf("User created: %s", trimmedUsername),
		UserID:         &info.ExternalUserID,
		Username:       &info.Username,
		OrganizationID: &orgID,
		Metadata:       map[string]any{"target_user_id": createdUserID, "target_username": trimmedUsername},
	})

	return &authv1.CreateUserResponse{
		UserId:            createdUserID,
		Username:          trimmedUsername,
		TemporaryPassword: tempPassword,
	}, nil
}

// ListUsers returns all users in the current user's organization
func (s *Service) ListUsers(ctx context.Context) (*authv1.ListUsersResponse, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}

	orgs, err := s.userStore.GetOrganizationsForUser(ctx, info.UserID)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("error getting user organizations: %v", err)
	}

	if len(orgs) != 1 {
		return nil, fleeterror.NewInternalErrorf("user should belong to exactly 1 org")
	}

	users, err := s.userManagementStore.ListUsersForOrganization(ctx, orgs[0].ID)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("error listing users: %v", err)
	}

	userInfos := make([]*authv1.UserInfo, len(users))
	for i, user := range users {
		userInfos[i] = &authv1.UserInfo{
			UserId:                 user.UserID,
			Username:               user.Username,
			PasswordUpdatedAt:      timestamppb.New(user.PasswordUpdatedAt),
			LastLoginAt:            toTimestampProto(user.LastLoginAt),
			Role:                   user.RoleName,
			RequiresPasswordChange: user.RequiresPasswordChange,
		}
	}

	return &authv1.ListUsersResponse{
		Users: userInfos,
	}, nil
}

// ResetUserPassword generates a new temporary password for a user (Super Admin only)
func (s *Service) ResetUserPassword(ctx context.Context, req *authv1.ResetUserPasswordRequest) (*authv1.ResetUserPasswordResponse, error) {
	if req.UserId == "" {
		return nil, fleeterror.NewInvalidArgumentError("user_id is required")
	}

	// Get current user's organization
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}

	orgs, err := s.userStore.GetOrganizationsForUser(ctx, info.UserID)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("error getting user organizations: %v", err)
	}

	if len(orgs) != 1 {
		return nil, fleeterror.NewInternalErrorf("user should belong to exactly 1 org")
	}

	orgID := orgs[0].ID

	// Check if current user can manage other users (only SUPER_ADMIN can)
	if err := s.checkCanManageUser(ctx, orgID); err != nil {
		return nil, err
	}

	// Get target user
	user, err := s.userStore.GetUserByExternalID(ctx, req.UserId)
	if err != nil {
		return nil, fleeterror.NewInvalidArgumentError("invalid user_id")
	}

	// Generate new temporary password
	tempPassword, err := generateTemporaryPassword()
	if err != nil {
		return nil, err
	}

	// Hash password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(tempPassword), bcrypt.DefaultCost)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("error generating password hash: %v", err)
	}

	// Update password with temporary flag
	if err := s.userManagementStore.AdminResetUserPassword(ctx, user.ID, string(hashedPassword)); err != nil {
		return nil, fleeterror.NewInternalErrorf("error resetting password: %v", err)
	}

	s.logActivity(ctx, activitymodels.Event{
		Category:       activitymodels.CategoryAuth,
		Type:           "reset_password",
		Description:    fmt.Sprintf("User password reset: %s", user.Username),
		UserID:         &info.ExternalUserID,
		Username:       &info.Username,
		OrganizationID: &orgID,
		Metadata:       map[string]any{"target_user_id": user.UserID, "target_username": user.Username},
	})

	return &authv1.ResetUserPasswordResponse{
		TemporaryPassword: tempPassword,
	}, nil
}

// DeactivateUser soft-deletes a user (Super Admin only)
func (s *Service) DeactivateUser(ctx context.Context, req *authv1.DeactivateUserRequest) (*authv1.DeactivateUserResponse, error) {
	if req.UserId == "" {
		return nil, fleeterror.NewInvalidArgumentError("user_id is required")
	}

	// Get current user
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}

	orgs, err := s.userStore.GetOrganizationsForUser(ctx, info.UserID)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("error getting user organizations: %v", err)
	}

	if len(orgs) != 1 {
		return nil, fleeterror.NewInternalErrorf("user should belong to exactly 1 org")
	}

	orgID := orgs[0].ID

	currentUser, err := s.userStore.GetUserByID(ctx, info.UserID)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("error getting current user: %v", err)
	}

	// Check if current user can manage other users (only SUPER_ADMIN can)
	if err := s.checkCanManageUser(ctx, orgID); err != nil {
		return nil, err
	}

	// Prevent self-deactivation
	if currentUser.UserID == req.UserId {
		return nil, fleeterror.NewErrorWithEndpointCode(
			"cannot deactivate your own account",
			connect.CodeInvalidArgument,
			int32(authv1.UserManagementErrorCode_USER_MANAGEMENT_ERROR_CODE_CANNOT_DEACTIVATE_SELF),
		)
	}

	// Get target user
	user, err := s.userStore.GetUserByExternalID(ctx, req.UserId)
	if err != nil {
		return nil, fleeterror.NewInvalidArgumentError("invalid user_id")
	}

	// Soft delete user
	if err := s.userManagementStore.SoftDeleteUser(ctx, user.ID); err != nil {
		return nil, fleeterror.NewInternalErrorf("error deactivating user: %v", err)
	}

	s.logActivity(ctx, activitymodels.Event{
		Category:       activitymodels.CategoryAuth,
		Type:           "deactivate_user",
		Description:    fmt.Sprintf("User deactivated: %s", user.Username),
		UserID:         &info.ExternalUserID,
		Username:       &info.Username,
		OrganizationID: &orgID,
		Metadata:       map[string]any{"target_user_id": user.UserID, "target_username": user.Username},
	})

	return &authv1.DeactivateUserResponse{}, nil
}

// toTimestampProto converts time.Time to *timestamppb.Timestamp
// Returns nil for zero time values (representing NULL in the database)
func toTimestampProto(t time.Time) *timestamppb.Timestamp {
	if t.IsZero() {
		return nil
	}
	return timestamppb.New(t)
}
