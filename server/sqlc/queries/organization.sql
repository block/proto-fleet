-- name: CreateOrganization :execresult
INSERT INTO organization (org_id, name, miner_auth_private_key)
VALUES (?, ?, ?);

-- name: GetOrganizationByID :one
SELECT *
FROM organization
WHERE id = ?
  AND deleted_at IS NULL;

-- name: GetOrganizationByOrgID :one
SELECT *
FROM organization
WHERE org_id = ?
  AND deleted_at IS NULL;

-- name: GetOrganizationByName :one
SELECT *
FROM organization
WHERE name = ?
  AND deleted_at IS NULL;

-- name: ListOrganizations :many
SELECT *
FROM organization
ORDER BY name;

-- name: UpdateOrganization :exec
UPDATE organization
SET name        = ?
WHERE id = ?;

-- name: SoftDeleteOrganization :exec
UPDATE organization
SET deleted_at = CURRENT_TIMESTAMP(6)
WHERE id = ?;

-- name: UndeleteOrganization :exec
UPDATE organization
SET deleted_at = NULL
WHERE id = ?;

-- name: GetOrganizationPrivateKey :one
SELECT miner_auth_private_key
FROM organization
where id = ?;
