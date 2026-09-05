-- name: GetUserByUsername :one
SELECT * FROM "user" WHERE username = $1 AND deleted_at IS NULL;

-- name: GetUserById :one
SELECT * FROM "user" WHERE id = $1 AND deleted_at IS NULL;

-- name: GetUserByIdForUpdate :one
SELECT * FROM "user" WHERE id = $1 AND deleted_at IS NULL FOR UPDATE;

-- name: GetUserByExternalId :one
SELECT * FROM "user" WHERE user_id = $1 AND deleted_at IS NULL;

-- name: CreateUser :one
INSERT INTO
    "user" (
        user_id,
        username,
        password_hash,
        requires_password_change,
        created_at
    )
VALUES ($1, $2, $3, $4, $5)
RETURNING id;

-- name: UpdateUserPassword :exec
UPDATE "user"
SET
    password_hash = $1,
    updated_at = NOW(),
    password_updated_at = NOW()
WHERE
    id = $2;

-- name: UpdateUserUsername :exec
UPDATE "user" SET username = $1, updated_at = NOW() WHERE id = $2;

-- name: HasUser :one
SELECT COUNT(*) > 0 FROM "user";

-- name: PasswordUpdatedAt :one
SELECT password_updated_at FROM "user" WHERE id = $1;

-- name: CountActiveRepairTicketsAssignedToUser :one
SELECT COUNT(*)
FROM repair_ticket
WHERE org_id = sqlc.arg('organization_id')
  AND assignee_user_id = sqlc.arg('user_id')::bigint
  AND status <> 5
  AND deleted_at IS NULL;

-- name: SoftDeleteUser :exec
UPDATE "user"
SET
    deleted_at = NOW(),
    updated_at = NOW()
WHERE
    id = $1
    AND deleted_at IS NULL;

-- name: UpdateLastLogin :exec
UPDATE "user"
SET
    last_login_at = NOW(),
    updated_at = NOW()
WHERE
    id = $1;

-- name: ListUsersForOrganization :many
SELECT u.id, u.user_id, u.username, u.created_at, u.updated_at, u.deleted_at, u.password_updated_at, u.last_login_at, u.requires_password_change, r.name as role_name
FROM
    "user" u
    JOIN user_organization uo ON u.id = uo.user_id
    JOIN role r ON uo.role_id = r.id
WHERE
    uo.organization_id = $1
    AND u.deleted_at IS NULL
    AND uo.deleted_at IS NULL
ORDER BY u.created_at DESC;

-- name: UpdateUserPasswordAndFlag :exec
UPDATE "user"
SET
    password_hash = $1,
    requires_password_change = FALSE,
    updated_at = NOW(),
    password_updated_at = NOW()
WHERE
    id = $2;

-- name: AdminResetUserPassword :execrows
UPDATE "user"
SET
    password_hash = $1,
    requires_password_change = TRUE,
    updated_at = NOW(),
    password_updated_at = NOW()
WHERE
    id = $2
    AND deleted_at IS NULL;

-- name: LockActiveSuperAdminUsers :many
-- Break-glass resets intentionally target the sole live org-scope
-- SUPER_ADMIN. Lock the complete identity/assignment chain so concurrent
-- resets serialize on the same rows. The live membership join matches what
-- role resolution requires at sign-in; without it a reset could succeed for
-- an account that still cannot log in.
SELECT
    u.id,
    u.user_id AS external_user_id,
    u.username,
    uor.organization_id
FROM user_organization_role AS uor
JOIN role AS r
    ON r.id = uor.role_id
    AND r.organization_id = uor.organization_id
JOIN "user" AS u ON u.id = uor.user_id
JOIN user_organization AS uo
    ON uo.user_id = uor.user_id
    AND uo.organization_id = uor.organization_id
    AND uo.deleted_at IS NULL
WHERE uor.scope_type = 'org'
    AND uor.scope_id IS NULL
    AND uor.deleted_at IS NULL
    AND r.deleted_at IS NULL
    AND r.builtin_key = 'SUPER_ADMIN'
    AND u.deleted_at IS NULL
ORDER BY u.id
FOR UPDATE OF uor, r, u, uo;
