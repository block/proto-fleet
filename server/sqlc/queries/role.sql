-- name: UpsertCustomRoleForOrg :one
-- Idempotent insert for per-org custom roles. ON CONFLICT targets the
-- partial unique index uq_role_org_custom_name keyed on
-- (organization_id, LOWER(BTRIM(name))) WHERE is_builtin = FALSE AND
-- deleted_at IS NULL — case-insensitive and trim-tolerant. Built-ins
-- go through UpsertBuiltinRoleForOrg below; using this path for
-- SUPER_ADMIN/ADMIN/FIELD_TECH would also be rejected by
-- chk_role_custom_name_not_reserved.
INSERT INTO role (name, description, is_builtin, organization_id)
VALUES ($1, $2, FALSE, $3)
ON CONFLICT (organization_id, (LOWER(BTRIM(name))))
    WHERE is_builtin = FALSE AND deleted_at IS NULL
    DO UPDATE SET
        description = EXCLUDED.description,
        deleted_at = NULL
RETURNING id;

-- name: GetRoleByID :one
SELECT *
FROM role
WHERE id = $1
  AND deleted_at IS NULL;

-- name: ListRoles :many
SELECT *
FROM role
ORDER BY name;

-- name: UpdateRole :exec
UPDATE role
SET name        = $1,
    description = $2
WHERE id = $3;

-- name: SoftDeleteRole :exec
UPDATE role
SET deleted_at = CURRENT_TIMESTAMP
WHERE id = $1;

-- name: UndeleteRole :exec
UPDATE role
SET deleted_at = NULL
WHERE id = $1;

-- name: ListBuiltinRolesForOrg :many
-- Returns the per-org built-in rows for a single organization. Used
-- by the startup reconciler (which iterates orgs) and by the
-- onboarding hook that seeds built-ins for a new org.
SELECT *
FROM role
WHERE is_builtin = TRUE
  AND organization_id = $1
  AND deleted_at IS NULL
ORDER BY builtin_key;

-- name: GetBuiltinRoleForOrg :one
-- The (org, builtin_key) pair is unique among live rows via the
-- partial index uq_role_org_builtin_key.
SELECT *
FROM role
WHERE is_builtin = TRUE
  AND organization_id = $1
  AND builtin_key = $2
  AND deleted_at IS NULL;

-- name: UpsertBuiltinRoleForOrg :one
-- Seed reconciliation entry point. The ON CONFLICT target matches
-- the partial unique index uq_role_org_builtin_key WHERE
-- is_builtin = TRUE AND deleted_at IS NULL.
INSERT INTO role (name, description, is_builtin, builtin_key, organization_id)
VALUES ($1, $2, TRUE, $3, $4)
ON CONFLICT (organization_id, builtin_key)
    WHERE is_builtin = TRUE AND deleted_at IS NULL
    DO UPDATE SET
        name = EXCLUDED.name,
        description = EXCLUDED.description,
        is_builtin = TRUE,
        deleted_at = NULL
RETURNING *;

-- name: ListActiveOrganizationIDs :many
-- The reconciler loops over this list at boot so every org has its
-- per-org built-ins. The onboarding flow also seeds built-ins for
-- new orgs inside its creation transaction.
SELECT id
FROM organization
WHERE deleted_at IS NULL
ORDER BY id;

-- name: ListCustomRolesForOrg :many
-- Per-org custom roles. The role-list handler calls this with the
-- caller's organization_id; the query never returns rows from other
-- orgs, so an admin in org A cannot see or assign org B's custom
-- roles even if they happen to know an internal id.
SELECT *
FROM role
WHERE is_builtin = FALSE
  AND organization_id = $1
  AND deleted_at IS NULL
ORDER BY name;

-- name: CreateCustomRole :one
INSERT INTO role (name, description, is_builtin, organization_id)
VALUES ($1, $2, FALSE, $3)
RETURNING *;

-- name: UpdateCustomRoleName :exec
-- Renames a custom role. Locked to is_builtin = FALSE so no built-in
-- row can be modified through this path; ADMIN and FIELD_TECH have
-- their own per-org editor that goes through a different code path
-- because their seed identity (builtin_key) must be preserved.
UPDATE role
SET name = $1,
    description = $2
WHERE id = $3
  AND deleted_at IS NULL
  AND is_builtin = FALSE;

-- name: SoftDeleteCustomRole :exec
-- Delete is locked for every built-in: the is_builtin = FALSE guard
-- here is the structural backstop, and the domain layer surfaces a
-- BUILTIN_ROLE_IMMUTABLE error so callers see a clear reason rather
-- than a silent no-op.
UPDATE role
SET deleted_at = CURRENT_TIMESTAMP
WHERE id = $1
  AND deleted_at IS NULL
  AND is_builtin = FALSE;
