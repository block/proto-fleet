-- name: CreateUserOrganization :exec
INSERT INTO user_organization (user_id, organization_id, role_id)
VALUES ($1, $2, $3);

-- name: GetOrganizationsForUser :many
SELECT o.*
FROM organization o
         JOIN user_organization uo ON o.id = uo.organization_id
WHERE uo.user_id = $1;

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
SELECT r.name
FROM role r
JOIN user_organization uo ON r.id = uo.role_id
WHERE uo.user_id = $1
  AND uo.organization_id = $2
  AND uo.deleted_at IS NULL;
