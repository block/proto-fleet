package authz

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
)

// maxRoleNameLength matches the role.name column's VARCHAR(255) schema
// definition. The pre-flight check just gives a clean InvalidArgument
// instead of a Postgres constraint violation; the wire validation
// (buf.validate on CreateCustomRoleRequest.name) is the primary cap
// for callers, and a tighter UI-facing limit belongs in the client.
const maxRoleNameLength = 255

// PostgreSQL SQLSTATE codes used by mapRolePersistError. unique_violation
// is the partial unique index on (org, lower(name)); check_violation is
// the reserved-names CHECK. Matching on the code first, then the
// constraint name, keeps the mapper stable across migrations that
// might restructure the message format.
const (
	pgCheckViolation = "23514" // matches db.PGUniqueViolation ("23505") sibling
)

// Service owns role CRUD for the AuthzService RPC surface. Validation
// (catalog membership, read-pairing, privilege parity) runs inside the
// same transaction as the write so a concurrent UnassignRole or role-
// permission edit can't slip in between the check and the persist.
type Service struct {
	conn *sql.DB
}

// NewService wires a Service to the connection pool. The per-request
// permission resolver lives in the middleware layer; this service
// re-loads effective permissions inside its own transaction via
// LoadEffectiveTx so the parity check is consistent with the write.
func NewService(conn *sql.DB) *Service {
	return &Service{conn: conn}
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
// the full table from a single DB round-trip — the previous
// per-role hydrate-and-count pattern was O(roles * 2 queries) on a
// listing hit by every role-management page open.
func (s *Service) ListRoles(ctx context.Context, orgID int64) ([]RoleView, error) {
	rows, err := sqlc.New(s.conn).ListRolesWithDetailsForOrg(ctx, sql.NullInt64{Int64: orgID, Valid: true})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("authz: list roles: %w", err)
	}
	out := make([]RoleView, 0, len(rows))
	for _, r := range applyBuiltinDisplayOrder(rows) {
		out = append(out, roleDetailRowToView(r))
	}
	return out, nil
}

// applyBuiltinDisplayOrder rebases the SQL ordering so SUPER_ADMIN
// sorts before ADMIN before FIELD_TECH. The query orders built-ins by
// builtin_key (alphabetical: ADMIN, FIELD_TECH, SUPER_ADMIN) so that
// the resolver pins down a deterministic comparison; the UI expects
// the seed-declaration order, which lives in Go.
func applyBuiltinDisplayOrder(rows []sqlc.ListRolesWithDetailsForOrgRow) []sqlc.ListRolesWithDetailsForOrgRow {
	priority := map[string]int{
		string(BuiltinKeySuperAdmin): 0,
		string(BuiltinKeyAdmin):      1,
		string(BuiltinKeyFieldTech):  2,
	}
	out := make([]sqlc.ListRolesWithDetailsForOrgRow, len(rows))
	copy(out, rows)
	sort.SliceStable(out, func(i, j int) bool {
		ai, aIsBuiltin := priority[out[i].BuiltinKey.String], out[i].IsBuiltin
		bj, bIsBuiltin := priority[out[j].BuiltinKey.String], out[j].IsBuiltin
		if aIsBuiltin != bIsBuiltin {
			return aIsBuiltin
		}
		if aIsBuiltin {
			return ai < bj
		}
		return false // preserve SQL ORDER BY name for custom rows
	})
	return out
}

func roleDetailRowToView(r sqlc.ListRolesWithDetailsForOrgRow) RoleView {
	desc := ""
	if r.Description.Valid {
		desc = r.Description.String
	}
	builtinKey := ""
	if r.BuiltinKey.Valid {
		builtinKey = r.BuiltinKey.String
	}
	return RoleView{
		ID:             r.ID,
		Name:           r.Name,
		Description:    desc,
		PermissionKeys: r.PermissionKeys,
		Builtin:        r.IsBuiltin,
		BuiltinKey:     builtinKey,
		MemberCount:    int32(r.MemberCount), //nolint:gosec // G115: assignment count bounded by user_organization_role rowcount per role; far below MaxInt32
		UpdatedAt:      r.UpdatedAt,
	}
}

// CreateCustomRole inserts a custom role with the requested permission
// set in a single transaction. Static validation (name shape, catalog
// membership, read-pairing) runs before the tx; privilege-parity runs
// inside it against a fresh LoadEffectiveTx so a caller demoted between
// the gate and the write cannot persist elevated grants.
func (s *Service) CreateCustomRole(ctx context.Context, callerID, orgID int64, name, description string, permissionKeys []string) (RoleView, error) {
	trimmedName := strings.TrimSpace(name)
	if err := validateRoleName(trimmedName); err != nil {
		return RoleView{}, err
	}
	normalized, err := validateAndNormalizeKeys(permissionKeys)
	if err != nil {
		return RoleView{}, err
	}
	trimmedDescription := strings.TrimSpace(description)
	return db.WithTransaction(ctx, s.conn, func(q *sqlc.Queries) (RoleView, error) {
		if err := authorizeCallerCanGrant(ctx, q, callerID, orgID, normalized); err != nil {
			return RoleView{}, err
		}
		role, err := q.CreateCustomRole(ctx, sqlc.CreateCustomRoleParams{
			Name:           trimmedName,
			Description:    nullStringIfNonEmpty(trimmedDescription),
			OrganizationID: sql.NullInt64{Int64: orgID, Valid: true},
		})
		if err != nil {
			return RoleView{}, mapRolePersistError(err)
		}
		if err := setRolePermissions(ctx, q, role.ID, normalized); err != nil {
			return RoleView{}, err
		}
		return hydrateRole(ctx, q, role)
	})
}

// UpdateCustomRole replaces the name, description, and permission set
// of a custom role in one transaction. Built-in roles are rejected
// with PermissionDenied ("built-in roles cannot be modified through
// this RPC") so callers get a clear reason rather than a silent no-op
// from the is_builtin = FALSE guard on UpdateCustomRoleName.
func (s *Service) UpdateCustomRole(ctx context.Context, callerID, orgID, roleID int64, name, description string, permissionKeys []string) (RoleView, error) {
	trimmedName := strings.TrimSpace(name)
	if err := validateRoleName(trimmedName); err != nil {
		return RoleView{}, err
	}
	normalized, err := validateAndNormalizeKeys(permissionKeys)
	if err != nil {
		return RoleView{}, err
	}
	trimmedDescription := strings.TrimSpace(description)
	return db.WithTransaction(ctx, s.conn, func(q *sqlc.Queries) (RoleView, error) {
		existing, err := getRoleInOrg(ctx, q, orgID, roleID)
		if err != nil {
			return RoleView{}, err
		}
		if existing.IsBuiltin {
			return RoleView{}, fleeterror.NewForbiddenError("built-in roles cannot be modified through this RPC")
		}
		if err := authorizeCallerCanGrant(ctx, q, callerID, orgID, normalized); err != nil {
			return RoleView{}, err
		}
		if err := q.UpdateCustomRoleName(ctx, sqlc.UpdateCustomRoleNameParams{
			Name:        trimmedName,
			Description: nullStringIfNonEmpty(trimmedDescription),
			ID:          roleID,
		}); err != nil {
			return RoleView{}, mapRolePersistError(err)
		}
		if err := q.ClearRolePermissions(ctx, roleID); err != nil {
			return RoleView{}, fleeterror.NewInternalErrorf("authz: clear role permissions: %w", err)
		}
		if err := setRolePermissions(ctx, q, roleID, normalized); err != nil {
			return RoleView{}, err
		}
		updated, err := q.GetRoleByID(ctx, roleID)
		if err != nil {
			// A concurrent DeleteCustomRole between getRoleInOrg above
			// and this reload would soft-delete the row out from under
			// us. Treat that as a clean 4xx (mirrors getRoleInOrg) so
			// the caller sees a benign race as InvalidArgument rather
			// than an Internal.
			if errors.Is(err, sql.ErrNoRows) {
				return RoleView{}, fleeterror.NewInvalidArgumentError("invalid role_id")
			}
			return RoleView{}, fleeterror.NewInternalErrorf("authz: reload role: %w", err)
		}
		return hydrateRole(ctx, q, updated)
	})
}

// DeleteCustomRole soft-deletes a custom role. Refuses on any active
// assignment (live user + live assignment row) so callers see a clear
// blocker (unassign first) instead of the assignment quietly outliving
// its role row. Built-in roles are rejected with PermissionDenied
// ("built-in roles cannot be deleted").
//
// callerID drives the in-transaction parity recheck via
// authorizeCallerCanGrant: the middleware gate verifies role:manage
// at request entry, but a caller demoted between the gate and the
// write would otherwise still be able to commit the delete. The
// recheck takes FOR UPDATE on the caller's assignment rows so a
// concurrent UnassignRole blocks until this tx commits.
func (s *Service) DeleteCustomRole(ctx context.Context, callerID, orgID, roleID int64) error {
	return db.WithTransactionNoResult(ctx, s.conn, func(q *sqlc.Queries) error {
		if err := authorizeCallerCanGrant(ctx, q, callerID, orgID, nil); err != nil {
			return err
		}
		existing, err := getRoleInOrg(ctx, q, orgID, roleID)
		if err != nil {
			return err
		}
		if existing.IsBuiltin {
			return fleeterror.NewForbiddenError("built-in roles cannot be deleted")
		}
		count, err := q.CountActiveAssignmentsForRole(ctx, roleID)
		if err != nil {
			return fleeterror.NewInternalErrorf("authz: count assignments: %w", err)
		}
		if count > 0 {
			return fleeterror.NewFailedPreconditionErrorf("role has %d active assignment(s); unassign before deleting", count)
		}
		if err := q.SoftDeleteCustomRole(ctx, roleID); err != nil {
			return fleeterror.NewInternalErrorf("authz: soft delete role: %w", err)
		}
		return nil
	})
}

// authorizeCallerCanGrant runs privilege-parity inside the active
// transaction with a FOR UPDATE lock on the caller's
// user_organization_role rows. The lock serializes the parity recheck
// against a concurrent UnassignRole / DeactivateUser: those mutations
// block on the same rows until our tx commits, so a demotion either
// applies before the recheck (and we fail closed) or applies after
// our commit (and the role we wrote reflects permissions the caller
// actually held at the moment of write). Without the lock, the same
// query under READ COMMITTED would let a demotion commit between
// SELECT and INSERT/UPDATE.
//
// The check is org-scope-only: site-scope grants do not let an admin
// smuggle wider permissions into a custom role.
//
// Pass an empty normalizedKeys to use this as a bare "caller still
// holds role:manage" recheck — that's how DeleteCustomRole defends
// against the middleware gate's window being too wide.
func authorizeCallerCanGrant(ctx context.Context, q *sqlc.Queries, callerID, orgID int64, normalizedKeys []string) error {
	callerEff, err := LoadEffectiveForUpdate(ctx, q, callerID, orgID)
	if err != nil {
		return fleeterror.NewInternalErrorf("authz: load caller permissions: %w", err)
	}
	if !callerEff.Has(PermRoleManage, ResourceContext{}) {
		return fleeterror.NewForbiddenError("caller does not hold role:manage at org scope")
	}
	for _, k := range normalizedKeys {
		if !callerEff.Has(k, ResourceContext{}) {
			return fleeterror.NewForbiddenError(fmt.Sprintf("cannot grant %s: caller does not hold this permission at org scope", k))
		}
	}
	return nil
}

// validateAndNormalizeKeys checks that every key is in the catalog,
// enforces the read-pairing rule, and returns a dedup'd / sorted slice
// ready for persistence. Pure function — no DB hit and no caller-state
// dependency — so the handler can fail-fast on a malformed request.
func validateAndNormalizeKeys(keys []string) ([]string, error) {
	normalized, err := validateAndDedupKeys(keys)
	if err != nil {
		return nil, err
	}
	if err := validateReadPairing(normalized); err != nil {
		return nil, err
	}
	return normalized, nil
}

// validateRoleName rejects empty/whitespace names and names that would
// blow past the column length. The cap is character-counted (runes)
// to match Postgres VARCHAR(255) and protovalidate's string.max_len,
// both of which count Unicode codepoints — len() would count bytes
// and falsely reject valid multi-byte names (emoji, CJK). Reserved
// names (SUPER_ADMIN / ADMIN / FIELD_TECH) are caught by the DB CHECK
// chk_role_custom_name_not_reserved and surface as an InvalidArgument
// via mapRolePersistError.
func validateRoleName(name string) error {
	if name == "" {
		return fleeterror.NewInvalidArgumentError("name is required")
	}
	if utf8.RuneCountInString(name) > maxRoleNameLength {
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
//
// The miner-only fleet:read floor is inlined rather than table-driven
// — there is exactly one such floor today and the inline branch keeps
// the rule visible next to the rest of the pairing logic. When the
// next floor lands (rack actions joining miner's pattern is the
// likeliest candidate), promote both into a slice on the catalog so
// the rule lives next to the permission declarations themselves.
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
		if entry.Resource == ResourceMiner && !have[PermFleetRead] {
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
		return fleeterror.NewInternalErrorf("authz: lookup permission ids: %w", err)
	}
	if len(perms) != len(keys) {
		return fleeterror.NewInternalErrorf("authz: permission row missing for one of: %v (catalog reconcile may have failed)", keys)
	}
	for _, p := range perms {
		if err := q.AssignPermissionToRole(ctx, sqlc.AssignPermissionToRoleParams{
			RoleID:       roleID,
			PermissionID: p.ID,
		}); err != nil {
			return fleeterror.NewInternalErrorf("authz: attach permission %s: %w", p.Key, err)
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
		return RoleView{}, fleeterror.NewInternalErrorf("authz: list role permissions: %w", err)
	}
	count, err := q.CountActiveAssignmentsForRole(ctx, r.ID)
	if err != nil {
		return RoleView{}, fleeterror.NewInternalErrorf("authz: count role assignments: %w", err)
	}
	desc := ""
	if r.Description.Valid {
		desc = r.Description.String
	}
	builtinKey := ""
	if r.BuiltinKey.Valid {
		builtinKey = r.BuiltinKey.String
	}
	return RoleView{
		ID:             r.ID,
		Name:           r.Name,
		Description:    desc,
		PermissionKeys: keys,
		Builtin:        r.IsBuiltin,
		BuiltinKey:     builtinKey,
		MemberCount:    int32(count), //nolint:gosec // G115: assignment count bounded by user_organization_role rowcount per role; far below MaxInt32
		UpdatedAt:      r.UpdatedAt,
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
		return sqlc.Role{}, fleeterror.NewInternalErrorf("authz: get role: %w", err)
	}
	if !role.OrganizationID.Valid || role.OrganizationID.Int64 != orgID {
		return sqlc.Role{}, fleeterror.NewInvalidArgumentError("invalid role_id")
	}
	return role, nil
}

// nullStringIfNonEmpty wraps a *post-trim* string so a description of
// "   " becomes NULL rather than an empty-but-Valid row. The DB has no
// constraint on this, but mixing "" and NULL in the column makes
// downstream "is this description set" checks misbehave.
func nullStringIfNonEmpty(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}

// mapRolePersistError converts PostgreSQL unique / check violations on
// the role table into user-facing InvalidArgument errors. Matching on
// SQLSTATE codes via *pgconn.PgError (not substring on Message) keeps
// the mapper stable across migrations that might restructure the
// constraint name or message text. Constraint names are still checked
// to disambiguate when the same code can mean different things on the
// same table (a future unique index on a different column, etc).
func mapRolePersistError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case db.PGUniqueViolation:
			if pgErr.ConstraintName == "uq_role_org_custom_name" {
				return fleeterror.NewInvalidArgumentError("a role with this name already exists")
			}
		case pgCheckViolation:
			if pgErr.ConstraintName == "chk_role_custom_name_not_reserved" {
				return fleeterror.NewInvalidArgumentError("name is reserved for a built-in role")
			}
		}
	}
	return fleeterror.NewInternalErrorf("authz: persist role: %w", err)
}
