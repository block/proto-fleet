package sqlstores

import (
	"context"
	"database/sql"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/authz"
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

func (s *SQLUserStore) getQueries(ctx context.Context) sqlc.Querier {
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
			ID:    org.ID,
			Name:  org.Name,
			OrgID: org.OrgID,
		}
	}

	return result, nil
}

func (s *SQLUserStore) CreateAdminUserWithOrganization(ctx context.Context, userID string, username string, passwordHash string,
	orgName string, orgID string, roleName string, roleDescription string) error {

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
		Name:  orgName,
		OrgID: orgID,
	})
	if err != nil {
		return fleeterror.NewInternalErrorf("error creating organization: %v", err)
	}

	// Seed per-org SUPER_ADMIN / ADMIN / FIELD_TECH built-in role rows
	// in the same transaction as org creation so the founding user can
	// be assigned a real per-org SUPER_ADMIN immediately, without
	// waiting for the next boot's startup reconciliation. The
	// roleName/roleDescription params are kept on the interface for
	// backward compatibility but are no longer used — built-in
	// definitions come from server/internal/domain/authz/builtin.go.
	_ = roleName
	_ = roleDescription
	builtinIDs, err := authz.SeedOrgBuiltins(ctx, q, orgInternalID)
	if err != nil {
		return fleeterror.NewInternalErrorf("error seeding per-org built-in roles: %v", err)
	}
	superAdminRoleID, ok := builtinIDs[authz.BuiltinKeySuperAdmin]
	if !ok {
		return fleeterror.NewInternalErrorf("seeding did not return SUPER_ADMIN role id")
	}

	if err := q.CreateUserOrganization(ctx, sqlc.CreateUserOrganizationParams{
		UserID:         userInternalID,
		RoleID:         superAdminRoleID,
		OrganizationID: orgInternalID,
	}); err != nil {
		return fleeterror.NewInternalErrorf("error creating user_organization row: %v", err)
	}

	// Dual-write to user_organization_role so the per-request permission
	// resolver sees this assignment without waiting for the next
	// migration pass. The legacy user_organization row above stays for
	// the soak window; both will be retired in a later migration.
	if _, err := q.AssignRole(ctx, sqlc.AssignRoleParams{
		UserID:         userInternalID,
		OrganizationID: orgInternalID,
		RoleID:         superAdminRoleID,
		ScopeType:      "org",
		ScopeID:        sql.NullInt64{},
	}); err != nil {
		return fleeterror.NewInternalErrorf("error creating user_organization_role row: %v", err)
	}
	return nil
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
	q := s.getQueries(ctx)
	if err := q.CreateUserOrganization(ctx, sqlc.CreateUserOrganizationParams{
		UserID:         userID,
		OrganizationID: organizationID,
		RoleID:         roleID,
	}); err != nil {
		return err
	}
	// Dual-write so the per-request permission resolver picks up the
	// assignment immediately. Without this, users created after PR 1
	// deploys but before the resolver swap would have no
	// user_organization_role row and the new gate would deny everything.
	if _, err := q.AssignRole(ctx, sqlc.AssignRoleParams{
		UserID:         userID,
		OrganizationID: organizationID,
		RoleID:         roleID,
		ScopeType:      "org",
		ScopeID:        sql.NullInt64{},
	}); err != nil {
		return fleeterror.NewInternalErrorf("error creating user_organization_role row: %v", err)
	}
	return nil
}

func (s *SQLUserStore) GetBuiltinRoleForOrg(ctx context.Context, organizationID int64, builtinKey string) (interfaces.Role, error) {
	role, err := s.getQueries(ctx).GetBuiltinRoleForOrg(ctx, sqlc.GetBuiltinRoleForOrgParams{
		OrganizationID: sql.NullInt64{Int64: organizationID, Valid: true},
		BuiltinKey:     sql.NullString{String: builtinKey, Valid: true},
	})
	if err != nil {
		return interfaces.Role{}, err
	}
	return toRole(role), nil
}

// GetRoleByID fetches a role row by primary key. Returns sql.ErrNoRows if
// the role is missing (or soft-deleted); CreateUser resolves that to an
// InvalidArgument so callers cannot probe role-id existence.
func (s *SQLUserStore) GetRoleByID(ctx context.Context, roleID int64) (interfaces.Role, error) {
	role, err := s.getQueries(ctx).GetRoleByID(ctx, roleID)
	if err != nil {
		return interfaces.Role{}, err
	}
	return toRole(role), nil
}

// GetRoleByIDForUpdate is the locking counterpart of GetRoleByID. Callers
// must be inside a transaction; the FOR UPDATE row lock serializes against
// SoftDeleteCustomRole so a freshly-resolved role cannot be soft-deleted
// out from under an assignment write.
func (s *SQLUserStore) GetRoleByIDForUpdate(ctx context.Context, roleID int64) (interfaces.Role, error) {
	role, err := s.getQueries(ctx).GetRoleByIDForUpdate(ctx, roleID)
	if err != nil {
		return interfaces.Role{}, err
	}
	return toRole(role), nil
}

// GetOrgScopeAssignmentForUser returns the user's live org-scope assignment.
// Takes FOR UPDATE on the assignment row (see the SQL comment) — callers
// must be inside a transaction. Returns sql.ErrNoRows when the (user, org)
// pair has none.
func (s *SQLUserStore) GetOrgScopeAssignmentForUser(ctx context.Context, userID int64, organizationID int64) (interfaces.OrgScopeAssignment, error) {
	row, err := s.getQueries(ctx).GetOrgScopeAssignmentForUser(ctx, sqlc.GetOrgScopeAssignmentForUserParams{
		UserID:         userID,
		OrganizationID: organizationID,
	})
	if err != nil {
		return interfaces.OrgScopeAssignment{}, err
	}
	return interfaces.OrgScopeAssignment{
		AssignmentID: row.AssignmentID,
		RoleID:       row.RoleID,
		BuiltinKey:   row.RoleBuiltinKey.String,
	}, nil
}

// LockAndCountOrgScopeSuperAdmins locks every live org-scope SUPER_ADMIN
// assignment in the org and returns the count. Callers about to demote a
// SUPER_ADMIN compare against 1 (count <= 1 means the assignment they're
// dropping is the last seat). Must be called inside a transaction so the
// row locks survive past the count return.
func (s *SQLUserStore) LockAndCountOrgScopeSuperAdmins(ctx context.Context, organizationID int64) (int64, error) {
	return s.getQueries(ctx).LockAndCountOrgScopeSuperAdmins(ctx, organizationID)
}

// UpdateUserOrganizationRole atomically swaps a user's org-scope role inside
// the surrounding tx. Dual-writes user_organization (legacy single-role row
// still consulted by other read paths during the soak window) and
// user_organization_role. Callers must already hold a FOR UPDATE lock on the
// new role row (via GetRoleByIDForUpdate) and have run the last-SUPER_ADMIN
// guard when the old assignment is a SUPER_ADMIN seat.
func (s *SQLUserStore) UpdateUserOrganizationRole(ctx context.Context, userID int64, organizationID int64, oldAssignmentID int64, newRoleID int64) error {
	q := s.getQueries(ctx)
	if err := q.UpdateUserRole(ctx, sqlc.UpdateUserRoleParams{
		RoleID:         newRoleID,
		UserID:         userID,
		OrganizationID: organizationID,
	}); err != nil {
		return fleeterror.NewInternalErrorf("error updating user_organization row: %v", err)
	}
	if err := q.UnassignRole(ctx, oldAssignmentID); err != nil {
		return fleeterror.NewInternalErrorf("error soft-deleting old user_organization_role row: %v", err)
	}
	if _, err := q.AssignRole(ctx, sqlc.AssignRoleParams{
		UserID:         userID,
		OrganizationID: organizationID,
		RoleID:         newRoleID,
		ScopeType:      "org",
		ScopeID:        sql.NullInt64{},
	}); err != nil {
		return fleeterror.NewInternalErrorf("error inserting new user_organization_role row: %v", err)
	}
	return nil
}

func toRole(role sqlc.Role) interfaces.Role {
	var orgID *int64
	if role.OrganizationID.Valid {
		v := role.OrganizationID.Int64
		orgID = &v
	}
	return interfaces.Role{
		ID:             role.ID,
		Name:           role.Name,
		Description:    role.Description.String,
		CreatedAt:      role.CreatedAt,
		UpdatedAt:      role.UpdatedAt,
		OrganizationID: orgID,
		BuiltinKey:     role.BuiltinKey.String,
	}
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

func (s *SQLUserStore) ListPermissionKeysByRoleID(ctx context.Context, roleID int64) ([]string, error) {
	return s.getQueries(ctx).ListRolePermissionKeys(ctx, roleID)
}
