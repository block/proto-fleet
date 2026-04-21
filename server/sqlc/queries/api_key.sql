-- name: CreateApiKey :exec
INSERT INTO api_key (key_id, name, prefix, key_hash, user_id, organization_id, created_at, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: GetApiKeyByHash :one
SELECT ak.*, u.username AS created_by_username
FROM api_key ak
JOIN "user" u ON ak.user_id = u.id
WHERE ak.key_hash = $1
  AND ak.revoked_at IS NULL
  AND u.deleted_at IS NULL;

-- name: ListApiKeysByOrganization :many
SELECT ak.id, ak.key_id, ak.name, ak.prefix, ak.user_id, ak.organization_id,
       ak.created_at, ak.expires_at, ak.revoked_at, ak.last_used_at,
       u.username AS created_by_username
FROM api_key ak
JOIN "user" u ON ak.user_id = u.id
WHERE ak.organization_id = $1
  AND ak.revoked_at IS NULL
  AND u.deleted_at IS NULL
ORDER BY ak.created_at DESC;

-- name: RevokeApiKey :execrows
UPDATE api_key
SET revoked_at = $1
WHERE key_id = $2 AND organization_id = $3 AND revoked_at IS NULL;

-- name: UpdateApiKeyLastUsed :exec
UPDATE api_key
SET last_used_at = $1
WHERE id = $2;
