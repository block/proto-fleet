package authz

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/block/proto-fleet/server/generated/sqlc"
)

// PermissionResolver computes the effective permission set for a
// (user, organization) on each authenticated request. It runs one
// query against the user_organization_role × role × role_permission
// join and materializes the result into an EffectivePermissions value
// the middleware can query via Has().
//
// One PermissionResolver is constructed at server boot and reused for
// every request — it holds no per-request state.
type PermissionResolver struct {
	conn *sql.DB
}

// NewPermissionResolver wires the resolver to the application's
// connection pool. The connection is used directly (not via the
// transaction wrapper) because LoadEffective is a read-only single
// query called per request, before any handler transaction begins.
func NewPermissionResolver(conn *sql.DB) *PermissionResolver {
	return &PermissionResolver{conn: conn}
}

// LoadEffective returns the user's full set of (role × scope ×
// permission) grants within the given organization, materialized as
// an EffectivePermissions value. Soft-deleted assignments and roles
// are excluded by the underlying SQL.
//
// Returns an empty (non-nil) EffectivePermissions when the user has
// no live assignments in the org. Has() on that empty value denies
// everything, which is the correct fail-closed default for a
// freshly-deactivated user or a user who was never in this org.
func (r *PermissionResolver) LoadEffective(ctx context.Context, userID, organizationID int64) (*EffectivePermissions, error) {
	return LoadEffectiveTx(ctx, sqlc.New(r.conn), userID, organizationID)
}

// LoadEffectiveTx runs the same query against a caller-supplied
// *sqlc.Queries handle so callers that already hold a transaction can
// re-load the effective set inside their own snapshot. The role-
// management service uses this to re-check the caller's permissions
// inside its mutation transactions, closing the TOCTOU window where a
// concurrent UnassignRole could demote the caller between the
// middleware gate and the role write.
func LoadEffectiveTx(ctx context.Context, q *sqlc.Queries, userID, organizationID int64) (*EffectivePermissions, error) {
	rows, err := q.ListEffectivePermissionsForUser(ctx, sqlc.ListEffectivePermissionsForUserParams{
		UserID:         userID,
		OrganizationID: organizationID,
	})
	if err != nil {
		return nil, fmt.Errorf("authz resolver: list effective permissions: %w", err)
	}
	return assignmentsFromRows(rows), nil
}

// LoadEffectiveForUpdate is the lock-taking variant of LoadEffectiveTx.
// It runs FOR UPDATE on the caller's user_organization_role rows so a
// concurrent UnassignRole or DeactivateUser blocks until this
// transaction commits — the parity check and the role write share a
// consistent snapshot under READ COMMITTED, and a demotion racing the
// mutation either applies before the recheck (and the recheck fails)
// or applies after the commit (and our grant stands against the
// permissions the caller actually held at write time).
//
// Use this from role-management mutations; the request-path resolver
// (LoadEffective) stays lock-free so authenticated traffic does not
// fight for row locks on every RPC.
func LoadEffectiveForUpdate(ctx context.Context, q *sqlc.Queries, userID, organizationID int64) (*EffectivePermissions, error) {
	rows, err := q.ListEffectivePermissionsForUserForUpdate(ctx, sqlc.ListEffectivePermissionsForUserForUpdateParams{
		UserID:         userID,
		OrganizationID: organizationID,
	})
	if err != nil {
		return nil, fmt.Errorf("authz resolver: list effective permissions (for update): %w", err)
	}
	return assignmentsForUpdateFromRows(rows), nil
}

// assignmentsForUpdateFromRows mirrors assignmentsFromRows for the
// locking query's row shape. The two row types are structurally
// identical (sqlc generates a distinct struct per query), so the
// grouping logic is the same — duplicated rather than reflectively
// shared because the sqlc structs aren't interface-typed.
func assignmentsForUpdateFromRows(rows []sqlc.ListEffectivePermissionsForUserForUpdateRow) *EffectivePermissions {
	if len(rows) == 0 {
		return NewEffectivePermissions(nil)
	}
	assignments := []Assignment{newAssignmentForUpdateRow(rows[0])}
	for _, row := range rows {
		current := &assignments[len(assignments)-1]
		if row.AssignmentID != current.AssignmentID {
			assignments = append(assignments, newAssignmentForUpdateRow(row))
			current = &assignments[len(assignments)-1]
		}
		if row.PermissionKey.Valid {
			current.Permissions = append(current.Permissions, row.PermissionKey.String)
		}
	}
	return NewEffectivePermissions(assignments)
}

func newAssignmentForUpdateRow(row sqlc.ListEffectivePermissionsForUserForUpdateRow) Assignment {
	a := Assignment{
		AssignmentID: row.AssignmentID,
		ScopeType:    ScopeType(row.ScopeType),
	}
	if row.ScopeID.Valid {
		site := row.ScopeID.Int64
		a.SiteID = &site
	}
	return a
}

// assignmentsFromRows groups the flat (assignment_id, scope,
// permission_key) rows the SQL returns into one Assignment per
// assignment_id, then materializes the resulting slice into an
// EffectivePermissions. The SQL ORDER BY uor.id makes the grouping
// streaming-friendly without needing a map indirection here.
//
// PermissionKey is nullable because the underlying query LEFT JOINs
// role_permission and permission — a site-scope role with zero
// permissions still produces one row so the resolver can record the
// assignment's existence (and trigger narrowing) even though it
// grants no actions. Rows with a NULL permission key contribute no
// keys to the Assignment's Permissions slice.
func assignmentsFromRows(rows []sqlc.ListEffectivePermissionsForUserRow) *EffectivePermissions {
	if len(rows) == 0 {
		return NewEffectivePermissions(nil)
	}

	assignments := []Assignment{newAssignmentFromRow(rows[0])}
	for _, row := range rows {
		current := &assignments[len(assignments)-1]
		if row.AssignmentID != current.AssignmentID {
			assignments = append(assignments, newAssignmentFromRow(row))
			current = &assignments[len(assignments)-1]
		}
		if row.PermissionKey.Valid {
			current.Permissions = append(current.Permissions, row.PermissionKey.String)
		}
	}

	return NewEffectivePermissions(assignments)
}

// newAssignmentFromRow seeds an Assignment from a row's scope columns
// only. The permission column is appended by the caller inside the
// loop so the same code path handles both first-row and same-assignment
// rows uniformly.
func newAssignmentFromRow(row sqlc.ListEffectivePermissionsForUserRow) Assignment {
	a := Assignment{
		AssignmentID: row.AssignmentID,
		ScopeType:    ScopeType(row.ScopeType),
	}
	if row.ScopeID.Valid {
		site := row.ScopeID.Int64
		a.SiteID = &site
	}
	return a
}
