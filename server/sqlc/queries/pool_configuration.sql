-- name: ListPoolConfigurations :many
SELECT
    pc.id as pool_config_id,
    pc.name as pool_config_name,
    pc.description as pool_config_description,
    pcp.id as pool_config_pool_id,
    pcp.priority as pool_priority,
    p.id as pool_id,
    p.pool_name as pool_name,
    p.url as pool_url,
    p.username as pool_username,
    p.is_default as pool_is_default
FROM pool_configuration pc
         LEFT JOIN pool_configuration_pool pcp ON pc.id = pcp.pool_configuration_id
         LEFT JOIN pool p ON pcp.pool_id = p.id
WHERE pc.org_id = ?
  AND (p.deleted_at IS NULL OR p.id IS NULL)
ORDER BY pc.name ASC, pcp.priority ASC;

-- name: GetPoolConfiguration :many
SELECT
    pc.id as pool_config_id,
    pc.name as pool_config_name,
    pc.description as pool_config_description,
    pcp.id as pool_config_pool_id,
    pcp.priority as pool_priority,
    p.id as pool_id,
    p.pool_name as pool_name,
    p.url as pool_url,
    p.username as pool_username,
    p.is_default as pool_is_default
FROM pool_configuration pc
         LEFT JOIN pool_configuration_pool pcp ON pc.id = pcp.pool_configuration_id
         LEFT JOIN pool p ON pcp.pool_id = p.id
WHERE pc.org_id = ?
  AND pc.id = ?
  AND (p.deleted_at IS NULL OR p.id IS NULL)
ORDER BY pc.name ASC, pcp.priority ASC;

-- name: GetPoolConfigurationIDByOrg :one
SELECT id
FROM pool_configuration
WHERE org_id = ?
LIMIT 1;

-- name: DeletePoolConfiguration :exec
DELETE FROM pool_configuration
WHERE id = ? AND org_id = ?;

-- name: DeletePoolConfigurationPools :exec
DELETE FROM pool_configuration_pool
WHERE pool_configuration_id = ?;

-- name: AddPoolToConfiguration :exec
INSERT INTO pool_configuration_pool (pool_id, pool_configuration_id, priority, created_at, updated_at)
VALUES (?, ?, ?, NOW(), NOW());

-- TODO alter on https://linear.app/squareup/issue/DASH-568/refactor-pool-configuration-to-multiple-pools-per-org-approach
-- name: UpsertPoolConfiguration :exec
INSERT INTO pool_configuration (org_id, name, description, created_at, updated_at)
VALUES (?, ?, ?, NOW(), NOW())
ON DUPLICATE KEY UPDATE
    name = VALUES(name),
    description = VALUES(description),
    updated_at = NOW();
