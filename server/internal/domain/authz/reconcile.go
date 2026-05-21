package authz

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/block/proto-fleet/server/generated/sqlc"
)

// Reconcile converges database state for the permission catalog and the
// built-in roles to match the in-code definition in catalog.go and
// builtin.go. It runs on every server boot from cmd/fleetd/main.go
// after migrations complete and before the HTTP listener starts.
//
// Concurrency: the work runs inside a single transaction that first
// acquires pg_advisory_xact_lock keyed on a stable string. Rolling
// deploys and autoscaler events serialize on the lock; non-winners
// observe the converged state once the winner commits.
//
// Per-role policy:
//
//   - SUPER_ADMIN: full reconcile to AllPermissions(). Catalog growth
//     adds rows; catalog shrinkage prunes them. Operator tampering on
//     SUPER_ADMIN is repaired on every boot.
//   - ADMIN, FIELD_TECH: additive only. Missing seed permissions are
//     inserted; nothing is ever removed. Operator edits via
//     UpdateCustomRole (U8) survive restarts.
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

	// Serialize concurrent reconciliations. Released on commit/rollback.
	if _, err := tx.ExecContext(ctx,
		`SELECT pg_advisory_xact_lock(hashtextextended('authz:builtin_reconcile', 0))`,
	); err != nil {
		return fmt.Errorf("authz reconcile: acquire advisory lock: %w", err)
	}

	q := sqlc.New(tx)

	if err := upsertCatalog(ctx, q); err != nil {
		return fmt.Errorf("authz reconcile: upsert catalog: %w", err)
	}

	for _, spec := range BuiltinRoles() {
		if err := reconcileBuiltinRole(ctx, q, spec); err != nil {
			return fmt.Errorf("authz reconcile: role %s: %w", spec.Key, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("authz reconcile: commit: %w", err)
	}
	return nil
}

// upsertCatalog ensures every catalog entry has a permission row whose
// description matches the in-code text. Keys removed from catalog.go
// are not deleted here — see the Reconcile godoc.
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

func reconcileBuiltinRole(ctx context.Context, q *sqlc.Queries, spec BuiltinRoleSpec) error {
	role, err := q.UpsertBuiltinRole(ctx, sqlc.UpsertBuiltinRoleParams{
		Name:        spec.Name,
		Description: sql.NullString{String: spec.Description, Valid: true},
		BuiltinKey:  sql.NullString{String: string(spec.Key), Valid: true},
	})
	if err != nil {
		return fmt.Errorf("upsert role row: %w", err)
	}

	perms, err := q.GetPermissionsByKeys(ctx, spec.SeedPermissions)
	if err != nil {
		return fmt.Errorf("lookup seed permissions: %w", err)
	}
	if len(perms) != len(spec.SeedPermissions) {
		// upsertCatalog ran in the same transaction, so every seed key
		// should have a permission row. A mismatch means the in-code
		// seed references a key that catalog.go does not declare —
		// fail loud so the discrepancy surfaces on boot, not in
		// production.
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
			RoleID:       role.ID,
			PermissionID: perm.ID,
		}); err != nil {
			return fmt.Errorf("assign permission %q: %w", perm.Key, err)
		}
	}

	if spec.Mode == ReconcileFull {
		if err := q.PrunePermissionsOutsideKeys(ctx, sqlc.PrunePermissionsOutsideKeysParams{
			RoleID: role.ID,
			Keys:   spec.SeedPermissions,
		}); err != nil {
			return fmt.Errorf("prune obsolete permissions: %w", err)
		}
	}

	return nil
}
