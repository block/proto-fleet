-- name: UpsertRole :one
-- PostgreSQL version returns the id using RETURNING
INSERT INTO role (name, description)
VALUES ($1, $2)
ON CONFLICT (name) DO UPDATE SET
    description = EXCLUDED.description,
    deleted_at = NULL
RETURNING id;

-- name: GetRoleByID :one
SELECT *
FROM role
WHERE id = $1
  AND deleted_at IS NULL;

-- name: GetRoleByName :one
SELECT * FROM role
WHERE name = $1;

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
-- by U4 startup reconciliation and the onboarding hook.
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
-- Per-org custom roles. Admin UI in U11 calls this with the caller's
-- organization_id; the query never returns rows from other orgs.
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
-- Renames a role. Locked to is_builtin = FALSE so no built-in row can
-- be modified through this path; ADMIN and FIELD_TECH edits go
-- through the per-org built-in editor in U8.
UPDATE role
SET name = $1,
    description = $2
WHERE id = $3
  AND deleted_at IS NULL
  AND is_builtin = FALSE;

-- name: SoftDeleteCustomRole :exec
-- Delete is locked for every built-in. The domain layer in U8
-- surfaces BUILTIN_ROLE_IMMUTABLE on a delete attempt.
UPDATE role
SET deleted_at = CURRENT_TIMESTAMP
WHERE id = $1
  AND deleted_at IS NULL
  AND is_builtin = FALSE;
