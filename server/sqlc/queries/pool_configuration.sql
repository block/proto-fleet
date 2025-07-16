-- name: GetPoolConfiguration :one
SELECT *
FROM pool_configuration
WHERE id = ?;

-- name: ListPoolConfigurations :many
SELECT *
FROM pool_configuration
WHERE org_id = ?
  AND deleted_at IS NULL
ORDER BY name ASC;

-- name: CreatePoolConfiguration :execresult
INSERT INTO pool_configuration (org_id, name, description, created_at, updated_at)
VALUES (?, ?, ?, NOW(), NOW());

-- name: DeletePoolConfiguration :exec
DELETE FROM pool_configuration
WHERE id = ?;

-- name: AddPoolToConfiguration :execresult
INSERT INTO pool_configuration_pool (pool_id, pool_configuration_id, priority, created_at, updated_at)
VALUES (?, ?, ?, NOW(), NOW());

-- name: RemovePoolFromConfiguration :exec
DELETE FROM pool_configuration_pool
WHERE id = ?;

-- name: GetPoolConfigurationPoolWithPriority :one
SELECT
    pcp.id as pool_config_pool_id,
    pcp.priority as pool_priority,
    p.id as pool_id,
    p.pool_name as pool_name,
    p.url as pool_url,
    p.username as pool_username,
    p.is_default as pool_is_default
FROM pool_configuration_pool pcp
         JOIN pool p ON pcp.pool_id = p.id
WHERE pcp.id = ?;

-- name: GetPoolConfigurationsWithPools :many
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
