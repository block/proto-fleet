package auth

import (
	"context"
	"crypto/subtle"
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
	"github.com/block/proto-fleet/server/internal/domain/authz"
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

// errBadPassword is returned by AuthenticateUser's transaction callback
// when a concurrent password change is detected via the version check.
var errBadPassword = errors.New("bad password")

type Service struct {
	userStore           stores.UserStore
	userManagementStore stores.UserManagementStore
	transactor          stores.Transactor
	tokenSvc            *token.Service
	sessionSvc          *session.Service
	encryptSvc          *encrypt.Service
	activitySvc         *activity.Service
	permResolver        *authz.PermissionResolver
}

func NewService(
	userStore stores.UserStore,
	userManagementStore stores.UserManagementStore,
	transactor stores.Transactor,
	tokenSvc *token.Service,
	sessionSvc *session.Service,
	encryptSvc *encrypt.Service,
	activitySvc *activity.Service,
	permResolver *authz.PermissionResolver,
) *Service {
	return &Service{
		userStore:           userStore,
		userManagementStore: userManagementStore,
		transactor:          transactor,
		tokenSvc:            tokenSvc,
		sessionSvc:          sessionSvc,
		encryptSvc:          encryptSvc,
		activitySvc:         activitySvc,
		permResolver:        permResolver,
	}
}

func strPtr(s string) *string { return &s }

func (s *Service) logActivity(ctx context.Context, event activitymodels.Event) {
	if s.activitySvc != nil {
		s.activitySvc.Log(ctx, event)
	}
}

func (s *Service) logLoginFailed(ctx context.Context, username string, userID *string, organizationID *int64) {
	s.logActivity(ctx, activitymodels.Event{
		Category:       activitymodels.CategoryAuth,
		Type:           "login_failed",
		Description:    "Login failed",
		Result:         activitymodels.ResultFailure,
		ErrorMessage:   strPtr("invalid credentials"),
		UserID:         userID,
		Username:       &username,
		OrganizationID: organizationID,
	})
}

// AuthenticateUser validates credentials, creates a session, and returns user info with a session cookie.
//
// To avoid holding a row lock during expensive bcrypt comparison, the flow is:
//  1. Optimistic read — fetch the user row and password hash without locking.
//  2. bcrypt comparison — CPU-intensive, runs outside any transaction.
//  3. Short locked transaction — FOR UPDATE the user row, verify password_updated_at
//     hasn't changed (i.e. no concurrent password rotation), then create the session.
//
// This ensures wrong-password attempts never acquire a lock, and correct-password
// attempts only hold the lock for the brief session INSERT.
func (s *Service) AuthenticateUser(ctx context.Context, req *authv1.AuthenticateRequest, userAgent, ipAddress string) (*authv1.AuthenticateResponse, *http.Cookie, error) {
	if req.Username == "" || utf8.RuneCountInString(req.Username) > 255 {
		return nil, nil, newAuthenticationFailedError()
	}

	// --- Step 1: Optimistic read (no lock) ---

	user, err := s.userStore.GetUserByUsername(ctx, req.Username)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) && !fleeterror.IsNotFoundError(err) {
			slog.Error("error looking up user", "username", req.Username, "error", err)
			return nil, nil, fleeterror.NewInternalErrorf("authentication service unavailable")
		}
		s.logLoginFailed(ctx, req.Username, nil, nil)
		return nil, nil, newAuthenticationFailedError()
	}

	orgs, err := s.userStore.GetOrganizationsForUser(ctx, user.ID)
	if err != nil {
		return nil, nil, fleeterror.NewInternalErrorf("error listing user orgs: %v", err)
	}
	if len(orgs) != 1 {
		return nil, nil, fleeterror.NewInternalErrorf("user should belong to exactly 1 org: was: %d", len(orgs))
	}

	// --- Step 2: bcrypt comparison (no lock held) ---

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		s.logLoginFailed(ctx, user.Username, &user.UserID, &orgs[0].ID)
		return nil, nil, newAuthenticationFailedError()
	}

	// Snapshot the password version so we can detect concurrent rotation.
	snapshotPasswordUpdatedAt := user.PasswordUpdatedAt

	// --- Step 3: Short locked transaction — verify version, create session ---

	var (
		sess              *session.Session
		loginTime         time.Time
		passwordUpdatedAt time.Time
	)

	txErr := s.transactor.RunInTx(ctx, func(ctx context.Context) error {
		// Re-read with lock to detect a concurrent password change.
		freshUser, err := s.userStore.GetUserByIDForUpdate(ctx, user.ID)
		if err != nil {
			return err
		}

		// If the password was rotated between the optimistic read and now,
		// the hash we validated is stale — reject the login.
		if freshUser.PasswordUpdatedAt != snapshotPasswordUpdatedAt {
			return errBadPassword
		}

		passwordUpdatedAt = freshUser.PasswordUpdatedAt

		// Update last login timestamp (non-critical, don't fail auth if this fails).
		loginTime = user.LastLoginAt
		if err := s.userManagementStore.UpdateLastLogin(ctx, user.ID); err != nil {
			slog.Warn("failed to update last login timestamp", "user_id", user.ID, "error", err)
		} else {
			loginTime = time.Now()
		}

		sess, err = s.sessionSvc.Create(ctx, user.ID, orgs[0].ID, userAgent, ipAddress)
		return err
	})

	if txErr != nil {
		if errors.Is(txErr, errBadPassword) {
			s.logLoginFailed(ctx, user.Username, &user.UserID, &orgs[0].ID)
			return nil, nil, newAuthenticationFailedError()
		}
		return nil, nil, txErr
	}

	roleName, err := s.userManagementStore.GetUserRoleName(ctx, user.ID, orgs[0].ID)
	if err != nil {
		return nil, nil, fleeterror.NewInternalErrorf("error getting user role: %v", err)
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
			PasswordUpdatedAt:      toTimestampProto(passwordUpdatedAt),
			LastLoginAt:            toTimestampProto(loginTime),
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

func newInvalidCurrentPasswordError() fleeterror.FleetError {
	return fleeterror.NewErrorWithEndpointCode(
		"Invalid current password.",
		connect.CodeInvalidArgument,
		int32(authv1.UpdatePasswordErrorCode_UPDATE_PASSWORD_ERROR_CODE_INVALID_OLD_PASSWORD),
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

// VerifySessionCredentials verifies that the provided username and password match
// the currently authenticated session user. Both must match — this prevents
// cross-user credential usage in step-up authentication flows.
func (s *Service) VerifySessionCredentials(ctx context.Context, username, password string) error {
	if username == "" || password == "" {
		return fleeterror.NewInvalidArgumentError("username and password are required")
	}

	info, err := session.GetInfo(ctx)
	if err != nil {
		return fleeterror.NewInternalErrorf("error getting session info: %v", err)
	}

	user, err := s.userStore.GetUserByID(ctx, info.UserID)
	if err != nil {
		return fleeterror.NewInternalErrorf("error looking up session user: %v", err)
	}

	usernameMatch := subtle.ConstantTimeCompare([]byte(user.Username), []byte(username)) == 1
	passwordErr := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password))

	if !usernameMatch || passwordErr != nil {
		s.logActivity(ctx, activitymodels.Event{
			Category:       activitymodels.CategoryAuth,
			Type:           "step_up_auth_failed",
			Description:    "Step-up authentication failed",
			Result:         activitymodels.ResultFailure,
			ErrorMessage:   strPtr("invalid credentials"),
			UserID:         &info.ExternalUserID,
			Username:       &info.Username,
			OrganizationID: &info.OrganizationID,
		})
		return fleeterror.NewForbiddenErrorf("invalid credentials")
	}

	return nil
}

// UpdatePassword changes the user's password, revokes all existing sessions,
// and creates a replacement session — all in one transaction. Returns a fresh
// session cookie so the caller stays logged in while every other session is
// invalidated.
func (s *Service) UpdatePassword(ctx context.Context, r *authv1.UpdatePasswordRequest, userAgent, ipAddress string) (*http.Cookie, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}

	if r.CurrentPassword == r.NewPassword {
		return nil, fleeterror.NewErrorWithEndpointCode(
			"New password cannot be the same as current password.",
			connect.CodeInvalidArgument,
			int32(authv1.UpdatePasswordErrorCode_UPDATE_PASSWORD_ERROR_CODE_NEW_PASSWORD_SAME_AS_OLD_PASSWORD),
		)
	}

	user, err := s.userStore.GetUserByID(ctx, info.UserID)
	if err != nil {
		return nil, fleeterror.NewForbiddenErrorf("error getting user by id, user_id: %d, error: %v", info.UserID, err)
	}

	if err = bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(r.CurrentPassword)); err != nil {
		return nil, newInvalidCurrentPasswordError()
	}

	// Snapshot the password version before starting the transaction so we can
	// detect a concurrent password rotation after the optimistic check.
	snapshotPasswordUpdatedAt := user.PasswordUpdatedAt

	// Hash the new password before we take the row lock, so the transaction only
	// holds the lock for the update/revoke/create sequence.
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(r.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("error generating hash of new password for user_id: %d, because: %v", info.UserID, err)
	}

	var sess *session.Session

	if err := s.transactor.RunInTx(ctx, func(ctx context.Context) error {
		freshUser, err := s.userStore.GetUserByIDForUpdate(ctx, info.UserID)
		if err != nil {
			return fleeterror.NewForbiddenErrorf("error getting user by id, user_id: %d, error: %v", info.UserID, err)
		}

		if freshUser.PasswordUpdatedAt != snapshotPasswordUpdatedAt {
			return newInvalidCurrentPasswordError()
		}

		if err = s.userManagementStore.UpdateUserPasswordAndClearPasswordChangeFlag(ctx, freshUser.ID, string(hashedPassword)); err != nil {
			return fleeterror.NewInternalErrorf("error updating password for user_id: %d, because: %v", info.UserID, err)
		}

		if err := s.sessionSvc.RevokeAllSessions(ctx, info.UserID); err != nil {
			return fleeterror.NewInternalErrorf("failed to revoke sessions: %v", err)
		}

		// Mint replacement session inside the same transaction so the
		// entire operation is atomic — no partial-commit failure mode.
		sess, err = s.sessionSvc.Create(ctx, info.UserID, info.OrganizationID, userAgent, ipAddress)
		if err != nil {
			return fleeterror.NewInternalErrorf("failed to create replacement session: %v", err)
		}

		return nil
	}); err != nil {
		return nil, err
	}

	cookie := s.sessionSvc.CreateCookie(sess.SessionID)

	s.logActivity(ctx, activitymodels.Event{
		Category:       activitymodels.CategoryAuth,
		Type:           "update_password",
		Description:    "Password updated",
		UserID:         &info.ExternalUserID,
		Username:       &info.Username,
		OrganizationID: &info.OrganizationID,
	})

	return cookie, nil
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

	return &authv1.GetUserAuditInfoResponse{Info: &authv1.UserAuditInfo{PasswordUpdatedAt: toTimestampProto(date)}}, nil
}

// generateDefaultOrgName returns a default organization name suffixed with the first 8 chars or the orgID
func generateDefaultOrgName(orgID string) string {
	return fmt.Sprintf("Organization %s", orgID[:8])
}

// authorizeCallerForUser is the shared lookup + parity check for
// ResetUserPassword and DeactivateUser. It resolves the target by
// external ID, masks cross-tenant lookups as InvalidArgument so
// existence does not leak across orgs, and runs the privilege-parity
// gate. Returns the resolved target so callers can use its IDs / name
// downstream.
func (s *Service) authorizeCallerForUser(ctx context.Context, callerUserID, orgID int64, targetExternalID string) (stores.User, error) {
	target, err := s.userStore.GetUserByExternalID(ctx, targetExternalID)
	if err != nil {
		return stores.User{}, fleeterror.NewInvalidArgumentError("invalid user_id")
	}

	// Cross-org guard: GetUserByExternalID is a global lookup. ErrNoRows
	// here means the target isn't in the caller's org; mask as
	// InvalidArgument to avoid leaking existence across tenants. Other
	// errors are transient and must propagate as Internal.
	if _, err := s.userManagementStore.GetUserRoleName(ctx, target.ID, orgID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return stores.User{}, fleeterror.NewInvalidArgumentError("invalid user_id")
		}
		return stores.User{}, fleeterror.NewInternalErrorf("error getting target user role: %v", err)
	}

	targetEff, err := s.permResolver.LoadEffective(ctx, target.ID, orgID)
	if err != nil {
		return stores.User{}, fleeterror.NewInternalErrorf("error loading target permissions: %v", err)
	}
	callerEff, err := s.permResolver.LoadEffective(ctx, callerUserID, orgID)
	if err != nil {
		return stores.User{}, fleeterror.NewInternalErrorf("error loading caller permissions: %v", err)
	}
	if err := requireCallerCanManageTarget(callerEff, targetEff); err != nil {
		return stores.User{}, err
	}
	return target, nil
}

// authorizeCallerForNewUserWithRole is the CreateUser counterpart of
// authorizeCallerForUser: the target does not yet exist, so the parity
// check uses a synthetic effective-permissions snapshot built from the
// role being assigned. Returns the resolved role so the caller can
// bind the new user to it.
func (s *Service) authorizeCallerForNewUserWithRole(ctx context.Context, callerUserID, orgID int64, roleName string) (stores.Role, error) {
	role, err := s.userManagementStore.GetBuiltinRoleForOrg(ctx, orgID, roleName)
	if err != nil {
		return stores.Role{}, fleeterror.NewInternalErrorf("error getting %s role for org: %v", roleName, err)
	}

	targetKeys, err := s.userManagementStore.ListPermissionKeysByRoleID(ctx, role.ID)
	if err != nil {
		return stores.Role{}, fleeterror.NewInternalErrorf("error listing target role permissions: %v", err)
	}
	targetEff := authz.NewEffectivePermissions([]authz.Assignment{{
		ScopeType:   authz.ScopeOrg,
		Permissions: targetKeys,
	}})
	callerEff, err := s.permResolver.LoadEffective(ctx, callerUserID, orgID)
	if err != nil {
		return stores.Role{}, fleeterror.NewInternalErrorf("error loading caller permissions: %v", err)
	}
	if err := requireCallerCanManageTarget(callerEff, targetEff); err != nil {
		return stores.Role{}, err
	}
	return role, nil
}

// requireCallerCanManageTarget enforces the user-management hierarchy:
// callers with org-scope role:manage need only subsume the target;
// everyone else must strictly dominate it. Without the strict-dominate
// requirement, ADMIN (equal perms to a fresh ADMIN account) could mint
// new ADMINs via CreateUser and walk off with the temp password.
func requireCallerCanManageTarget(callerEff, targetEff *authz.EffectivePermissions) error {
	if callerEff.Has(authz.PermRoleManage, authz.ResourceContext{}) {
		if !targetEff.IsSubsumedBy(callerEff) {
			return fleeterror.NewForbiddenError("insufficient permissions to manage this user")
		}
		return nil
	}
	if !callerEff.StrictlyDominates(targetEff) {
		return fleeterror.NewForbiddenError("insufficient permissions to manage this user")
	}
	return nil
}

// CreateUser creates a new user with a temporary password. Authorization is
// enforced by the Connect handler via RequirePermission(PermUserManage);
// callers outside the handler layer must add their own permission gate.
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

	// Look up the ADMIN role for this org and gate the caller against
	// the permission set the new user will inherit. Org-scoped row
	// lookup avoids binding to a different tenant's ADMIN.
	role, err := s.authorizeCallerForNewUserWithRole(ctx, info.UserID, orgID, AdminRoleName)
	if err != nil {
		return nil, err
	}

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
			PasswordUpdatedAt:      toTimestampProto(user.PasswordUpdatedAt),
			LastLoginAt:            toTimestampProto(user.LastLoginAt),
			Role:                   user.RoleName,
			RequiresPasswordChange: user.RequiresPasswordChange,
		}
	}

	return &authv1.ListUsersResponse{
		Users: userInfos,
	}, nil
}

// ResetUserPassword generates a new temporary password for a user.
// Authorization is enforced by the Connect handler via RequirePermission
// (PermUserManage); callers outside the handler layer must add their own
// permission gate.
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

	user, err := s.authorizeCallerForUser(ctx, info.UserID, orgID, req.UserId)
	if err != nil {
		return nil, err
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

	// Update password and revoke all sessions atomically.
	if err := s.transactor.RunInTx(ctx, func(ctx context.Context) error {
		if err := s.userManagementStore.AdminResetUserPassword(ctx, user.ID, string(hashedPassword)); err != nil {
			return fleeterror.NewInternalErrorf("error resetting password: %v", err)
		}
		if err := s.sessionSvc.RevokeAllSessions(ctx, user.ID); err != nil {
			return fleeterror.NewInternalErrorf("failed to revoke sessions: %v", err)
		}
		return nil
	}); err != nil {
		return nil, err
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

// DeactivateUser soft-deletes a user. Authorization is enforced by the
// Connect handler via RequirePermission(PermUserManage); callers outside
// the handler layer must add their own permission gate.
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

	// Prevent self-deactivation
	if currentUser.UserID == req.UserId {
		return nil, fleeterror.NewErrorWithEndpointCode(
			"cannot deactivate your own account",
			connect.CodeInvalidArgument,
			int32(authv1.UserManagementErrorCode_USER_MANAGEMENT_ERROR_CODE_CANNOT_DEACTIVATE_SELF),
		)
	}

	user, err := s.authorizeCallerForUser(ctx, info.UserID, orgID, req.UserId)
	if err != nil {
		return nil, err
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
