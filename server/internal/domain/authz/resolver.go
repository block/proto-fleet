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
// query — the request middleware calls it before any handler work,
// so there's no surrounding transaction to participate in.
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
	rows, err := sqlc.New(r.conn).ListEffectivePermissionsForUser(ctx, sqlc.ListEffectivePermissionsForUserParams{
		UserID:         userID,
		OrganizationID: organizationID,
	})
	if err != nil {
		return nil, fmt.Errorf("authz resolver: list effective permissions: %w", err)
	}
	return assignmentsFromRows(rows), nil
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

	var (
		assignments []Assignment
		current     Assignment
		started     bool
	)
	flush := func() {
		if started {
			assignments = append(assignments, current)
		}
	}
	for _, row := range rows {
		if !started || row.AssignmentID != current.AssignmentID {
			flush()
			current = Assignment{
				AssignmentID: row.AssignmentID,
				ScopeType:    ScopeType(row.ScopeType),
			}
			if row.ScopeID.Valid {
				site := row.ScopeID.Int64
				current.SiteID = &site
			}
			started = true
		}
		if row.PermissionKey.Valid {
			current.Permissions = append(current.Permissions, row.PermissionKey.String)
		}
	}
	flush()

	return NewEffectivePermissions(assignments)
}
