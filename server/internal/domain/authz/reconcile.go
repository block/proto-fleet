package authz

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/block/proto-fleet/server/generated/sqlc"
	dbinfra "github.com/block/proto-fleet/server/internal/infrastructure/db"
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
//   - ADMIN and FIELD_TECH are seeded ONCE, when the role row is
//     first created. After that the role belongs to the operator —
//     reconciliation does not touch its role_permission rows. This is
//     intentional: re-asserting the seed set on every boot would
//     silently restore permissions the operator had revoked (e.g.,
//     miner:update_pools or miner:firmware_update). New catalog keys
//     introduced in future releases do NOT auto-propagate to existing
//     orgs' ADMIN/FIELD_TECH; operators add them via the role editor
//     if they want them.
//
// Catalog row policy: permissions are always upserted (description
// text refreshed). A permission key removed from catalog.go is NOT
// dropped from the permission table — that is a deliberate manual
// migration because deleting a catalog row would also drop every
// role_permission referencing it.
//
// Retry semantics: the work runs through db.WithTransactionNoResult,
// which retries on transient Postgres errors (serialization failures,
// connection-reset on commit) using the project's standard backoff.
// The advisory lock is acquired inside the same transaction via the
// AcquireReconcileLock sqlc query, so a retry re-acquires it cleanly.
// Callers must wrap their context with a deadline; without one a
// stuck reconcile would block boot indefinitely.
func Reconcile(ctx context.Context, conn *sql.DB) error {
	return dbinfra.WithTransactionNoResult(ctx, conn, func(q *sqlc.Queries) error {
		if err := q.AcquireReconcileLock(ctx); err != nil {
			return fmt.Errorf("authz reconcile: acquire advisory lock: %w", err)
		}

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
		return nil
	})
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
// migration (000053) has populated the catalog.
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
	orgIDValue := sql.NullInt64{Int64: orgID, Valid: true}

	for _, spec := range BuiltinRoles() {
		existing, err := q.GetBuiltinRoleForOrg(ctx, sqlc.GetBuiltinRoleForOrgParams{
			OrganizationID: orgIDValue,
			BuiltinKey:     sql.NullString{String: string(spec.Key), Valid: true},
		})
		switch {
		case err == nil:
			ids[spec.Key] = existing.ID
			if spec.Mode == ReconcileFull {
				if err := fullyReconcilePermissions(ctx, q, existing.ID, spec); err != nil {
					return nil, fmt.Errorf("reconcile %s permissions: %w", spec.Key, err)
				}
			}
			// Additive-only built-ins: leave role_permission untouched.
			// The operator owns this role once it exists; re-asserting
			// seed permissions would silently restore anything they
			// deliberately revoked. Seed-formula changes that need to
			// reach existing rows ship as explicit one-off migrations
			// (see migrations/000055_backfill_admin_user_manage.up.sql).

		case errors.Is(err, sql.ErrNoRows):
			role, err := q.UpsertBuiltinRoleForOrg(ctx, sqlc.UpsertBuiltinRoleForOrgParams{
				Name:           spec.Name,
				Description:    sql.NullString{String: spec.Description, Valid: true},
				BuiltinKey:     sql.NullString{String: string(spec.Key), Valid: true},
				OrganizationID: orgIDValue,
			})
			if err != nil {
				return nil, fmt.Errorf("create builtin %s: %w", spec.Key, err)
			}
			ids[spec.Key] = role.ID
			if err := assignSeedPermissions(ctx, q, role.ID, spec); err != nil {
				return nil, fmt.Errorf("seed %s permissions: %w", spec.Key, err)
			}

		default:
			return nil, fmt.Errorf("lookup builtin %s: %w", spec.Key, err)
		}
	}
	return ids, nil
}

// assignSeedPermissions inserts role_permission rows for every key in the
// spec's SeedPermissions list. Used on first creation of a per-org built-in
// row — for additive-only built-ins this is the only path that touches
// permissions; subsequent boots leave the row alone.
func assignSeedPermissions(ctx context.Context, q *sqlc.Queries, roleID int64, spec BuiltinRoleSpec) error {
	perms, err := lookupSeedPermissions(ctx, q, spec)
	if err != nil {
		return err
	}
	for _, perm := range perms {
		if err := q.AssignPermissionToRole(ctx, sqlc.AssignPermissionToRoleParams{
			RoleID:       roleID,
			PermissionID: perm.ID,
		}); err != nil {
			return fmt.Errorf("assign permission %q: %w", perm.Key, err)
		}
	}
	return nil
}

// fullyReconcilePermissions converges role_permission to exactly the spec's
// SeedPermissions set: inserts missing keys, removes any extras. Used only
// for SUPER_ADMIN, whose contract is "always everything in the current
// catalog" — operator tampering is repaired and obsolete keys are pruned.
func fullyReconcilePermissions(ctx context.Context, q *sqlc.Queries, roleID int64, spec BuiltinRoleSpec) error {
	if err := assignSeedPermissions(ctx, q, roleID, spec); err != nil {
		return err
	}
	if err := q.PrunePermissionsOutsideKeys(ctx, sqlc.PrunePermissionsOutsideKeysParams{
		RoleID: roleID,
		Keys:   spec.SeedPermissions,
	}); err != nil {
		return fmt.Errorf("prune obsolete permissions: %w", err)
	}
	return nil
}

// lookupSeedPermissions returns the permission rows for every key in the
// spec's SeedPermissions list, failing loudly if any seed key is missing
// from the catalog table (which would indicate the in-code seed formula
// references a permission that catalog.go does not declare).
func lookupSeedPermissions(ctx context.Context, q *sqlc.Queries, spec BuiltinRoleSpec) ([]sqlc.Permission, error) {
	perms, err := q.GetPermissionsByKeys(ctx, spec.SeedPermissions)
	if err != nil {
		return nil, fmt.Errorf("lookup seed permissions: %w", err)
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
		return nil, fmt.Errorf("seed permissions %v not in catalog (likely missing from catalog.go)", missing)
	}
	return perms, nil
}
