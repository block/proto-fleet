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

-- name: ListBuiltinRoles :many
-- Returns the three (eventually four) built-in roles keyed by
-- builtin_key. Used by U4 startup reconciliation.
SELECT *
FROM role
WHERE is_builtin = TRUE
  AND deleted_at IS NULL
ORDER BY builtin_key;

-- name: GetRoleByBuiltinKey :one
SELECT *
FROM role
WHERE builtin_key = $1
  AND deleted_at IS NULL;

-- name: UpsertBuiltinRole :one
-- Used only by U4 seed reconciliation. Created with is_builtin=TRUE
-- so subsequent custom-role mutation paths skip it via
-- builtin_key IS DISTINCT FROM 'SUPER_ADMIN'.
INSERT INTO role (name, description, is_builtin, builtin_key)
VALUES ($1, $2, TRUE, $3)
ON CONFLICT (builtin_key) DO UPDATE SET
    name = EXCLUDED.name,
    description = EXCLUDED.description,
    is_builtin = TRUE,
    deleted_at = NULL
RETURNING *;

-- name: ListCustomRoles :many
-- Custom roles are everything that isn't a built-in. Admin UI in U11
-- lists them per-org; this query is org-agnostic because custom roles
-- are global to the deployment in v1 (an org's admins can still pick
-- which to assign).
SELECT *
FROM role
WHERE is_builtin = FALSE
  AND deleted_at IS NULL
ORDER BY name;

-- name: CreateCustomRole :one
INSERT INTO role (name, description, is_builtin)
VALUES ($1, $2, FALSE)
RETURNING *;

-- name: UpdateCustomRoleName :exec
-- Renames a role. Rejects SUPER_ADMIN at the query level via the
-- builtin_key guard; ADMIN and FIELD_TECH are editable through the
-- same path as custom roles, per the U8 design.
UPDATE role
SET name = $1,
    description = $2
WHERE id = $3
  AND deleted_at IS NULL
  AND builtin_key IS DISTINCT FROM 'SUPER_ADMIN';

-- name: SoftDeleteCustomRole :exec
-- Delete is locked for every built-in (SUPER_ADMIN, ADMIN,
-- FIELD_TECH); the domain layer in U8 surfaces
-- BUILTIN_ROLE_NON_DELETABLE for ADMIN/FIELD_TECH and
-- BUILTIN_ROLE_IMMUTABLE for SUPER_ADMIN. This query is the
-- structural backstop: it refuses to soft-delete any is_builtin row.
UPDATE role
SET deleted_at = CURRENT_TIMESTAMP
WHERE id = $1
  AND deleted_at IS NULL
  AND is_builtin = FALSE;
