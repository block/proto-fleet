-- name: CreateUserOrganization :exec
INSERT INTO user_organization (user_id, organization_id, role_id)
VALUES (?, ?, ?);

-- name: GetOrganizationsForUser :many
SELECT o.*
FROM organization o
         JOIN user_organization uo ON o.id = uo.organization_id
WHERE uo.user_id = ?;

-- name: GetUsersForOrganization :many
SELECT u.*
FROM user u
         JOIN user_organization uo ON u.id = uo.user_id
WHERE uo.organization_id = ?;

-- name: GetUserRoleInOrganization :one
SELECT r.*
FROM role r
         JOIN user_organization uo ON r.id = uo.role_id
WHERE uo.user_id = ?
  AND uo.organization_id = ?;

-- name: UpdateUserRole :exec
UPDATE user_organization
SET role_id = ?
WHERE user_id = ?
  AND organization_id = ?;

-- name: SoftDeleteUserFromOrganization :exec
UPDATE user_organization
SET deleted_at = CURRENT_TIMESTAMP(6)
WHERE user_id = ?
  AND organization_id = ?;
