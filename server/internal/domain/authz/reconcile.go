package authz

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/block/proto-fleet/server/generated/sqlc"
)

// Reconcile converges database state for the permission catalog and
// per-org built-in roles to match the in-code definition in catalog.go
// and builtin.go. It runs once at server boot from cmd/fleetd/main.go
// after migrations complete and before the HTTP listener starts.
//
// Concurrency: the work runs inside a single transaction that first
// acquires pg_advisory_xact_lock keyed on a stable string. Rolling
// deploys and autoscaler events serialize on the lock; non-winners
// observe the converged state once the winner commits.
//
// Per-org policy:
//
//   - Every active organization gets its own SUPER_ADMIN, ADMIN, and
//     FIELD_TECH role row. Editing one org's ADMIN cannot leak into
//     another org's ADMIN.
//   - SUPER_ADMIN is fully reconciled to AllPermissions() per org.
//     Tampering on the org's SUPER_ADMIN row is repaired on every
//     boot.
//   - ADMIN and FIELD_TECH are reconciled additive-only per org.
//     Missing seed permissions get inserted; nothing is ever removed.
//     Operator edits to those roles survive restarts.
//
// Catalog row policy: permissions are always upserted (description
// text refreshed). A permission key removed from catalog.go is NOT
// dropped from the permission table — that is a deliberate manual
// migration because deleting a catalog row would also drop every
// role_permission referencing it.
func Reconcile(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("authz reconcile: begin tx: %w", err)
	}
	//goland:noinspection GoUnhandledErrorResult
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx,
		`SELECT pg_advisory_xact_lock(hashtextextended('authz:builtin_reconcile', 0))`,
	); err != nil {
		return fmt.Errorf("authz reconcile: acquire advisory lock: %w", err)
	}

	q := sqlc.New(tx)

	if err := upsertCatalog(ctx, q); err != nil {
		return fmt.Errorf("authz reconcile: upsert catalog: %w", err)
	}

	orgIDs, err := q.ListActiveOrganizationIDs(ctx)
	if err != nil {
		return fmt.Errorf("authz reconcile: list orgs: %w", err)
	}
	for _, orgID := range orgIDs {
		if _, err := seedOrgBuiltins(ctx, q, orgID); err != nil {
			return fmt.Errorf("authz reconcile: org %d: %w", orgID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("authz reconcile: commit: %w", err)
	}
	return nil
}

// SeedOrgBuiltins ensures the three built-in role rows exist for an
// organization with their seed permission sets reconciled per
// builtin.go policy. Callers must hold a sqlc.Queries bound to a live
// transaction so seeding participates in the surrounding work
// atomically.
//
// Returns a map of BuiltinKey → role id so callers (e.g. the
// onboarding flow that needs the new org's SUPER_ADMIN role id to
// create the founding user's assignment) can wire up dependent
// writes in the same transaction.
//
// SeedOrgBuiltins does NOT upsert catalog permission rows; the boot
// reconciler handles that once per process via upsertCatalog. Callers
// outside the boot reconciler are expected to run after the seed
// migration (000052) has populated the catalog.
func SeedOrgBuiltins(ctx context.Context, q *sqlc.Queries, orgID int64) (map[BuiltinKey]int64, error) {
	return seedOrgBuiltins(ctx, q, orgID)
}

func upsertCatalog(ctx context.Context, q *sqlc.Queries) error {
	for _, entry := range Catalog() {
		if _, err := q.UpsertPermission(ctx, sqlc.UpsertPermissionParams{
			Key:         entry.Key,
			Description: entry.Description,
		}); err != nil {
			return fmt.Errorf("upsert permission %q: %w", entry.Key, err)
		}
	}
	return nil
}

func seedOrgBuiltins(ctx context.Context, q *sqlc.Queries, orgID int64) (map[BuiltinKey]int64, error) {
	ids := make(map[BuiltinKey]int64, 3)
	for _, spec := range BuiltinRoles() {
		role, err := q.UpsertBuiltinRoleForOrg(ctx, sqlc.UpsertBuiltinRoleForOrgParams{
			Name:           spec.Name,
			Description:    sql.NullString{String: spec.Description, Valid: true},
			BuiltinKey:     sql.NullString{String: string(spec.Key), Valid: true},
			OrganizationID: sql.NullInt64{Int64: orgID, Valid: true},
		})
		if err != nil {
			return nil, fmt.Errorf("upsert builtin %s: %w", spec.Key, err)
		}
		ids[spec.Key] = role.ID

		if err := reconcileBuiltinPermissions(ctx, q, role.ID, spec); err != nil {
			return nil, fmt.Errorf("reconcile permissions for %s: %w", spec.Key, err)
		}
	}
	return ids, nil
}

func reconcileBuiltinPermissions(ctx context.Context, q *sqlc.Queries, roleID int64, spec BuiltinRoleSpec) error {
	perms, err := q.GetPermissionsByKeys(ctx, spec.SeedPermissions)
	if err != nil {
		return fmt.Errorf("lookup seed permissions: %w", err)
	}
	if len(perms) != len(spec.SeedPermissions) {
		got := make(map[string]bool, len(perms))
		for _, p := range perms {
			got[p.Key] = true
		}
		var missing []string
		for _, key := range spec.SeedPermissions {
			if !got[key] {
				missing = append(missing, key)
			}
		}
		return fmt.Errorf("seed permissions %v not in catalog (likely missing from catalog.go)", missing)
	}

	for _, perm := range perms {
		if err := q.AssignPermissionToRole(ctx, sqlc.AssignPermissionToRoleParams{
			RoleID:       roleID,
			PermissionID: perm.ID,
		}); err != nil {
			return fmt.Errorf("assign permission %q: %w", perm.Key, err)
		}
	}

	if spec.Mode == ReconcileFull {
		if err := q.PrunePermissionsOutsideKeys(ctx, sqlc.PrunePermissionsOutsideKeysParams{
			RoleID: roleID,
			Keys:   spec.SeedPermissions,
		}); err != nil {
			return fmt.Errorf("prune obsolete permissions: %w", err)
		}
	}

	return nil
}
