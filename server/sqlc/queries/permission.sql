-- Permission catalog queries. The catalog is reconciled at startup
-- against domain/authz/catalog.go (see U4 reconciliation).

-- name: ListPermissions :many
SELECT *
FROM permission
ORDER BY key;

-- name: GetPermissionByKey :one
SELECT *
FROM permission
WHERE key = $1;

-- name: GetPermissionsByKeys :many
SELECT *
FROM permission
WHERE key = ANY(sqlc.arg(keys)::text[]);

-- name: UpsertPermission :one
-- Reconciliation entry point. Description is updated on every boot from
-- the in-code catalog so catalog text changes propagate without a new
-- migration.
INSERT INTO permission (key, description)
VALUES ($1, $2)
ON CONFLICT (key) DO UPDATE SET
    description = EXCLUDED.description
RETURNING *;

-- name: ListRolePermissionKeys :many
-- Returns every permission key attached to the given role. Used by the
-- resolver (U6) and by UpdateCustomRole's privilege-parity check (U8).
SELECT p.key
FROM role_permission rp
JOIN permission p ON p.id = rp.permission_id
WHERE rp.role_id = $1
ORDER BY p.key;

-- name: AssignPermissionToRole :exec
-- Idempotent insert used by reconciliation and by UpdateCustomRole.
INSERT INTO role_permission (role_id, permission_id)
VALUES ($1, $2)
ON CONFLICT (role_id, permission_id) DO NOTHING;

-- name: RevokePermissionFromRole :exec
DELETE FROM role_permission
WHERE role_id = $1
  AND permission_id = $2;

-- name: ClearRolePermissions :exec
-- Wholesale removal for the SUPER_ADMIN reconcile path (it is followed
-- by a full re-insert in the same transaction) and for
-- ReplaceRolePermissions in U8.
DELETE FROM role_permission
WHERE role_id = $1;

-- name: PrunePermissionsOutsideKeys :exec
-- Used by SUPER_ADMIN full reconciliation: keep only the permissions
-- whose key is in the supplied set. ADMIN/FIELD_TECH reconciliation
-- never calls this — they are additive-only.
DELETE FROM role_permission
WHERE role_id = $1
  AND permission_id NOT IN (
      SELECT id FROM permission WHERE key = ANY(sqlc.arg(keys)::text[])
  );
