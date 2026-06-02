package authz

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
)

// maxRoleNameLength bounds the role.name column. The DB enforces a
// length CHECK; this constant gives us a cheap pre-flight rejection
// so we surface InvalidArgument instead of an Internal error from a
// constraint violation.
const maxRoleNameLength = 64

// fleetReadFloorResources is the set of resources whose action keys
// require fleet:read in addition to their own :read partner. Miner
// actions need both because the fleet dashboard is the entry surface
// for any miner interaction — a role with miner:reboot but no
// fleet:read can act on a miner it cannot navigate to.
var fleetReadFloorResources = map[string]bool{
	ResourceMiner: true,
}

// Service owns role CRUD for the AuthzService RPC surface. It runs
// validation (catalog membership, read-pairing rule, privilege parity)
// and persists changes inside a single transaction so a half-applied
// role never appears to callers.
type Service struct {
	conn         *sql.DB
	permResolver *PermissionResolver
}

// NewService wires a Service to the connection pool and the per-request
// permission resolver. The resolver is reused (not constructed here) so
// the service shares the same EffectivePermissions caching surface the
// middleware uses.
func NewService(conn *sql.DB, resolver *PermissionResolver) *Service {
	return &Service{conn: conn, permResolver: resolver}
}

// RoleView is the domain-layer projection of a role row plus its
// permission set and live assignment count. The Connect handler maps
// this to the wire Role message.
type RoleView struct {
	ID             int64
	Name           string
	Description    string
	PermissionKeys []string
	Builtin        bool
	BuiltinKey     string
	MemberCount    int32
	UpdatedAt      time.Time
}

// ListRoles returns built-in and custom roles in the caller's org in a
// stable display order: built-ins first (SUPER_ADMIN, ADMIN, FIELD_TECH
// per the seed), then custom roles by name. Each entry carries its
// permission keys and live assignment count so the admin UI can render
// the full table from a single response.
func (s *Service) ListRoles(ctx context.Context, orgID int64) ([]RoleView, error) {
	return db.WithTransaction(ctx, s.conn, func(q *sqlc.Queries) ([]RoleView, error) {
		builtins, err := q.ListBuiltinRolesForOrg(ctx, sql.NullInt64{Int64: orgID, Valid: true})
		if err != nil {
			return nil, fleeterror.NewInternalErrorf("authz: list builtin roles: %v", err)
		}
		custom, err := q.ListCustomRolesForOrg(ctx, sql.NullInt64{Int64: orgID, Valid: true})
		if err != nil {
			return nil, fleeterror.NewInternalErrorf("authz: list custom roles: %v", err)
		}
		out := make([]RoleView, 0, len(builtins)+len(custom))
		for _, r := range orderBuiltins(builtins) {
			view, err := hydrateRole(ctx, q, r)
			if err != nil {
				return nil, err
			}
			out = append(out, view)
		}
		for _, r := range custom {
			view, err := hydrateRole(ctx, q, r)
			if err != nil {
				return nil, err
			}
			out = append(out, view)
		}
		return out, nil
	})
}

// CreateCustomRole inserts a custom role with the requested permission
// set in a single transaction. Validates name shape, catalog membership,
// read-pairing, and privilege parity before any write.
func (s *Service) CreateCustomRole(ctx context.Context, callerID, orgID int64, name, description string, permissionKeys []string) (RoleView, error) {
	trimmedName := strings.TrimSpace(name)
	if err := validateRoleName(trimmedName); err != nil {
		return RoleView{}, err
	}
	normalized, err := s.normalizeAndAuthorizeKeys(ctx, callerID, orgID, permissionKeys)
	if err != nil {
		return RoleView{}, err
	}
	return db.WithTransaction(ctx, s.conn, func(q *sqlc.Queries) (RoleView, error) {
		role, err := q.CreateCustomRole(ctx, sqlc.CreateCustomRoleParams{
			Name:           trimmedName,
			Description:    sql.NullString{String: strings.TrimSpace(description), Valid: description != ""},
			OrganizationID: sql.NullInt64{Int64: orgID, Valid: true},
		})
		if err != nil {
			return RoleView{}, mapRoleInsertError(err)
		}
		if err := setRolePermissions(ctx, q, role.ID, normalized); err != nil {
			return RoleView{}, err
		}
		return hydrateRole(ctx, q, role)
	})
}

// UpdateCustomRole replaces the name, description, and permission set
// of a custom role in one transaction. Built-in roles are rejected with
// BUILTIN_ROLE_IMMUTABLE so callers get a clear reason rather than a
// silent no-op from the is_builtin = FALSE guard on UpdateCustomRoleName.
func (s *Service) UpdateCustomRole(ctx context.Context, callerID, orgID, roleID int64, name, description string, permissionKeys []string) (RoleView, error) {
	trimmedName := strings.TrimSpace(name)
	if err := validateRoleName(trimmedName); err != nil {
		return RoleView{}, err
	}
	normalized, err := s.normalizeAndAuthorizeKeys(ctx, callerID, orgID, permissionKeys)
	if err != nil {
		return RoleView{}, err
	}
	return db.WithTransaction(ctx, s.conn, func(q *sqlc.Queries) (RoleView, error) {
		existing, err := getRoleInOrg(ctx, q, orgID, roleID)
		if err != nil {
			return RoleView{}, err
		}
		if existing.IsBuiltin {
			return RoleView{}, fleeterror.NewForbiddenError("built-in roles cannot be modified through this RPC")
		}
		if err := q.UpdateCustomRoleName(ctx, sqlc.UpdateCustomRoleNameParams{
			Name:        trimmedName,
			Description: sql.NullString{String: strings.TrimSpace(description), Valid: description != ""},
			ID:          roleID,
		}); err != nil {
			return RoleView{}, mapRoleInsertError(err)
		}
		if err := q.ClearRolePermissions(ctx, roleID); err != nil {
			return RoleView{}, fleeterror.NewInternalErrorf("authz: clear role permissions: %v", err)
		}
		if err := setRolePermissions(ctx, q, roleID, normalized); err != nil {
			return RoleView{}, err
		}
		updated, err := q.GetRoleByID(ctx, roleID)
		if err != nil {
			return RoleView{}, fleeterror.NewInternalErrorf("authz: reload role: %v", err)
		}
		return hydrateRole(ctx, q, updated)
	})
}

// DeleteCustomRole soft-deletes a custom role. Refuses on any active
// assignment so callers see a clear blocker (unassign first) instead of
// the assignment quietly outliving its role row. Built-in roles are
// rejected with BUILTIN_ROLE_IMMUTABLE.
func (s *Service) DeleteCustomRole(ctx context.Context, orgID, roleID int64) error {
	return db.WithTransactionNoResult(ctx, s.conn, func(q *sqlc.Queries) error {
		existing, err := getRoleInOrg(ctx, q, orgID, roleID)
		if err != nil {
			return err
		}
		if existing.IsBuiltin {
			return fleeterror.NewForbiddenError("built-in roles cannot be deleted")
		}
		count, err := q.CountActiveAssignmentsForRole(ctx, roleID)
		if err != nil {
			return fleeterror.NewInternalErrorf("authz: count assignments: %v", err)
		}
		if count > 0 {
			return fleeterror.NewFailedPreconditionErrorf("role has %d active assignment(s); unassign before deleting", count)
		}
		if err := q.SoftDeleteCustomRole(ctx, roleID); err != nil {
			return fleeterror.NewInternalErrorf("authz: soft delete role: %v", err)
		}
		return nil
	})
}

// normalizeAndAuthorizeKeys runs catalog membership, read-pairing, and
// privilege-parity checks. Returns a dedup'd, lexicographically sorted
// slice ready for persistence. Caller-side privilege parity uses the
// caller's *org-scope* effective set: site-scope grants do not let an
// admin smuggle wider permissions into a custom role.
func (s *Service) normalizeAndAuthorizeKeys(ctx context.Context, callerID, orgID int64, keys []string) ([]string, error) {
	normalized, err := validateAndDedupKeys(keys)
	if err != nil {
		return nil, err
	}
	if err := validateReadPairing(normalized); err != nil {
		return nil, err
	}
	callerEff, err := s.permResolver.LoadEffective(ctx, callerID, orgID)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("authz: load caller permissions: %v", err)
	}
	for _, k := range normalized {
		if !callerEff.Has(k, ResourceContext{}) {
			return nil, fleeterror.NewForbiddenError(fmt.Sprintf("cannot grant %s: caller does not hold this permission at org scope", k))
		}
	}
	return normalized, nil
}

// validateRoleName rejects empty/whitespace names and names that would
// blow past the column length. Reserved names (SUPER_ADMIN / ADMIN /
// FIELD_TECH) are caught by the DB CHECK chk_role_custom_name_not_reserved
// and surface as an InvalidArgument via mapRoleInsertError.
func validateRoleName(name string) error {
	if name == "" {
		return fleeterror.NewInvalidArgumentError("name is required")
	}
	if len(name) > maxRoleNameLength {
		return fleeterror.NewInvalidArgumentErrorf("name must be at most %d characters", maxRoleNameLength)
	}
	return nil
}

// validateAndDedupKeys returns the input keys deduplicated and sorted.
// Rejects unknown keys with InvalidArgument; the catalog is the source
// of truth and a typo here would silently persist a no-op permission.
func validateAndDedupKeys(keys []string) ([]string, error) {
	seen := make(map[string]bool, len(keys))
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		if _, ok := Lookup(k); !ok {
			return nil, fleeterror.NewInvalidArgumentErrorf("unknown permission key: %s", k)
		}
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, k)
	}
	sort.Strings(out)
	return out, nil
}

// validateReadPairing enforces "every action requires its same-resource
// :read partner, and miner actions also require fleet:read". The
// catalog comment on PermFleetRead and PermMinerRead documents this
// rule; this is the runtime enforcement.
func validateReadPairing(keys []string) error {
	have := make(map[string]bool, len(keys))
	for _, k := range keys {
		have[k] = true
	}
	for _, k := range keys {
		entry, _ := Lookup(k)
		readKey := entry.Resource + ":read"
		if k == readKey {
			continue
		}
		// Some resources (role, apikey) are manage-only — the catalog
		// has no :read partner because their surfaces live under
		// route-guarded Settings, not a list view a viewer-only role
		// would navigate to. Skip the pair check when the partner does
		// not exist in the catalog at all; pair-when-exists is the
		// invariant, not pair-everything.
		if _, ok := Lookup(readKey); ok && !have[readKey] {
			return fleeterror.NewInvalidArgumentErrorf("%s requires %s in the same role", k, readKey)
		}
		if fleetReadFloorResources[entry.Resource] && !have[PermFleetRead] {
			return fleeterror.NewInvalidArgumentErrorf("%s requires %s in the same role", k, PermFleetRead)
		}
	}
	return nil
}

// setRolePermissions persists the (role, permission_keys) mapping by
// looking up permission ids via key. Caller is responsible for clearing
// any prior rows first (UpdateCustomRole does this with
// ClearRolePermissions). The permission table is reconciled at boot, so
// every catalog key has a row.
func setRolePermissions(ctx context.Context, q *sqlc.Queries, roleID int64, keys []string) error {
	if len(keys) == 0 {
		return nil
	}
	perms, err := q.GetPermissionsByKeys(ctx, keys)
	if err != nil {
		return fleeterror.NewInternalErrorf("authz: lookup permission ids: %v", err)
	}
	if len(perms) != len(keys) {
		return fleeterror.NewInternalErrorf("authz: permission row missing for one of: %v (catalog reconcile may have failed)", keys)
	}
	for _, p := range perms {
		if err := q.AssignPermissionToRole(ctx, sqlc.AssignPermissionToRoleParams{
			RoleID:       roleID,
			PermissionID: p.ID,
		}); err != nil {
			return fleeterror.NewInternalErrorf("authz: attach permission %s: %v", p.Key, err)
		}
	}
	return nil
}

// hydrateRole loads the permission keys and live assignment count for a
// role row and assembles a RoleView. Called per row inside ListRoles
// and per-row after mutations.
func hydrateRole(ctx context.Context, q *sqlc.Queries, r sqlc.Role) (RoleView, error) {
	keys, err := q.ListRolePermissionKeys(ctx, r.ID)
	if err != nil {
		return RoleView{}, fleeterror.NewInternalErrorf("authz: list role permissions: %v", err)
	}
	count, err := q.CountActiveAssignmentsForRole(ctx, r.ID)
	if err != nil {
		return RoleView{}, fleeterror.NewInternalErrorf("authz: count role assignments: %v", err)
	}
	desc := ""
	if r.Description.Valid {
		desc = r.Description.String
	}
	builtinKey := ""
	if r.BuiltinKey.Valid {
		builtinKey = r.BuiltinKey.String
	}
	updated := r.UpdatedAt
	return RoleView{
		ID:             r.ID,
		Name:           r.Name,
		Description:    desc,
		PermissionKeys: keys,
		Builtin:        r.IsBuiltin,
		BuiltinKey:     builtinKey,
		MemberCount:    int32(count), //nolint:gosec // G115: assignment count bounded by user_organization_role rowcount per role; far below MaxInt32
		UpdatedAt:      updated,
	}, nil
}

// getRoleInOrg fetches a role and rejects cross-org access with
// InvalidArgument so an admin in org A cannot probe role ids belonging
// to org B. NotFound is also masked as InvalidArgument for the same
// existence-leak reason.
func getRoleInOrg(ctx context.Context, q *sqlc.Queries, orgID, roleID int64) (sqlc.Role, error) {
	role, err := q.GetRoleByID(ctx, roleID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return sqlc.Role{}, fleeterror.NewInvalidArgumentError("invalid role_id")
		}
		return sqlc.Role{}, fleeterror.NewInternalErrorf("authz: get role: %v", err)
	}
	if !role.OrganizationID.Valid || role.OrganizationID.Int64 != orgID {
		return sqlc.Role{}, fleeterror.NewInvalidArgumentError("invalid role_id")
	}
	return role, nil
}

// orderBuiltins sorts built-in roles by their fixed display order
// (SUPER_ADMIN, ADMIN, FIELD_TECH). The DB returns them ordered by
// builtin_key, which is alphabetical (ADMIN, FIELD_TECH, SUPER_ADMIN) —
// the admin UI expects SUPER_ADMIN at the top.
func orderBuiltins(rows []sqlc.Role) []sqlc.Role {
	priority := map[string]int{
		string(BuiltinKeySuperAdmin): 0,
		string(BuiltinKeyAdmin):      1,
		string(BuiltinKeyFieldTech):  2,
	}
	out := make([]sqlc.Role, len(rows))
	copy(out, rows)
	sort.SliceStable(out, func(i, j int) bool {
		return priority[out[i].BuiltinKey.String] < priority[out[j].BuiltinKey.String]
	})
	return out
}

// mapRoleInsertError converts pq unique/check violations on the role
// table into user-facing InvalidArgument errors. The DB enforces
// case-insensitive name uniqueness per org (uq_role_org_custom_name)
// and rejects reserved names via chk_role_custom_name_not_reserved.
func mapRoleInsertError(err error) error {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "uq_role_org_custom_name"):
		return fleeterror.NewInvalidArgumentError("a role with this name already exists")
	case strings.Contains(msg, "chk_role_custom_name_not_reserved"):
		return fleeterror.NewInvalidArgumentError("name is reserved for a built-in role")
	default:
		return fleeterror.NewInternalErrorf("authz: persist role: %v", err)
	}
}
