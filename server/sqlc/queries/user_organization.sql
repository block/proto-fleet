-- name: CreateUserOrganization :exec
INSERT INTO user_organization (user_id, organization_id, role_id)
VALUES ($1, $2, $3);

-- name: GetOrganizationsForUser :many
SELECT o.*
FROM organization o
         JOIN user_organization uo ON o.id = uo.organization_id
WHERE uo.user_id = $1 AND uo.deleted_at IS NULL AND o.deleted_at IS NULL;

-- name: GetUsersForOrganization :many
SELECT u.*
FROM "user" u
         JOIN user_organization uo ON u.id = uo.user_id
WHERE uo.organization_id = $1;

-- name: GetUserRoleInOrganization :one
SELECT r.*
FROM role r
         JOIN user_organization uo ON r.id = uo.role_id
WHERE uo.user_id = $1
  AND uo.organization_id = $2;

-- name: UpdateUserRole :exec
UPDATE user_organization
SET role_id = $1
WHERE user_id = $2
  AND organization_id = $3;

-- name: SoftDeleteUserFromOrganization :exec
UPDATE user_organization
SET deleted_at = CURRENT_TIMESTAMP
WHERE user_id = $1
  AND organization_id = $2;

-- name: GetUserRoleName :one
SELECT COALESCE(r.name, '')::text AS name
FROM user_organization uo
JOIN "user" u ON u.id = uo.user_id AND u.deleted_at IS NULL
LEFT JOIN role r ON r.id = uo.role_id AND r.organization_id = uo.organization_id AND r.deleted_at IS NULL
WHERE uo.user_id = $1
  AND uo.organization_id = $2
  AND uo.deleted_at IS NULL;

-- name: GetUserRoleNameForUpdate :one
SELECT COALESCE(r.name, '')::text AS name
FROM user_organization uo
JOIN "user" u ON u.id = uo.user_id AND u.deleted_at IS NULL
LEFT JOIN role r ON r.id = uo.role_id AND r.organization_id = uo.organization_id AND r.deleted_at IS NULL
WHERE uo.user_id = $1
  AND uo.organization_id = $2
  AND uo.deleted_at IS NULL
FOR UPDATE OF uo;

-- name: GetActiveUserRoleNameForUpdate :one
-- Organization user management must not target customer-only affiliations.
-- Lock organization first to serialize account/role mutations, but allow the
-- key-share locks needed by concurrent affiliation/session foreign keys.
WITH organization_lock AS MATERIALIZED (
    SELECT org.id FROM organization org
    WHERE org.id = sqlc.arg(organization_id) AND org.deleted_at IS NULL
    FOR NO KEY UPDATE OF org
)
SELECT r.name
FROM organization_lock o
JOIN user_organization uo ON uo.organization_id = o.id
JOIN "user" u ON u.id = uo.user_id AND u.deleted_at IS NULL
JOIN role r ON r.id = uo.role_id AND r.organization_id = uo.organization_id
           AND r.deleted_at IS NULL AND r.name <> ''
WHERE uo.user_id = sqlc.arg(user_id) AND uo.deleted_at IS NULL
FOR NO KEY UPDATE OF uo, r;
