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

-- name: AdminResetUserPassword :exec
UPDATE "user"
SET
    password_hash = $1,
    requires_password_change = TRUE,
    updated_at = NOW(),
    password_updated_at = NOW()
WHERE
    id = $2
    AND deleted_at IS NULL;
