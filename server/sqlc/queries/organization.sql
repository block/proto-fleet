-- name: CreateOrganization :one
INSERT INTO organization (org_id, name, miner_auth_private_key)
VALUES ($1, $2, $3)
RETURNING id;

-- name: GetOrganizationByID :one
SELECT *
FROM organization
WHERE id = $1
  AND deleted_at IS NULL;

-- name: GetOrganizationByOrgID :one
SELECT *
FROM organization
WHERE org_id = $1
  AND deleted_at IS NULL;

-- name: GetOrganizationByName :one
SELECT *
FROM organization
WHERE name = $1
  AND deleted_at IS NULL;

-- name: ListOrganizations :many
SELECT *
FROM organization
ORDER BY name;

-- name: UpdateOrganization :exec
UPDATE organization
SET name = $1
WHERE id = $2;

-- name: SoftDeleteOrganization :exec
UPDATE organization
SET deleted_at = CURRENT_TIMESTAMP
WHERE id = $1;

-- name: UndeleteOrganization :exec
UPDATE organization
SET deleted_at = NULL
WHERE id = $1;

-- name: DeleteOrganization :exec
DELETE
FROM organization
WHERE id = $1;

-- name: GetOrganizationPrivateKey :one
SELECT miner_auth_private_key
FROM organization
where id = $1;
