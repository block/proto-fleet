-- name: ListCurtailmentResponseProfilesByOrg :many
SELECT *
FROM curtailment_response_profile
WHERE org_id = sqlc.arg('org_id')
ORDER BY profile_name, id;

-- name: GetCurtailmentResponseProfileByOrg :one
SELECT *
FROM curtailment_response_profile
WHERE id = sqlc.arg('id')
  AND org_id = sqlc.arg('org_id');

-- name: InsertCurtailmentResponseProfile :one
INSERT INTO curtailment_response_profile (
    org_id,
    profile_name,
    site_id,
    mode,
    strategy,
    level,
    priority,
    target_kw,
    tolerance_kw,
    curtail_batch_size,
    curtail_batch_interval_sec,
    restore_batch_size,
    restore_batch_interval_sec,
    include_maintenance,
    force_include_maintenance
) VALUES (
    sqlc.arg('org_id'),
    sqlc.arg('profile_name'),
    sqlc.narg('site_id'),
    sqlc.arg('mode'),
    sqlc.arg('strategy'),
    sqlc.arg('level'),
    sqlc.arg('priority'),
    sqlc.narg('target_kw'),
    sqlc.narg('tolerance_kw'),
    sqlc.narg('curtail_batch_size'),
    sqlc.arg('curtail_batch_interval_sec'),
    sqlc.arg('restore_batch_size'),
    sqlc.arg('restore_batch_interval_sec'),
    sqlc.arg('include_maintenance'),
    sqlc.arg('force_include_maintenance')
)
RETURNING *;

-- name: UpdateCurtailmentResponseProfile :one
UPDATE curtailment_response_profile
SET
    profile_name = sqlc.arg('profile_name'),
    site_id = sqlc.narg('site_id'),
    mode = sqlc.arg('mode'),
    strategy = sqlc.arg('strategy'),
    level = sqlc.arg('level'),
    priority = sqlc.arg('priority'),
    target_kw = sqlc.narg('target_kw'),
    tolerance_kw = sqlc.narg('tolerance_kw'),
    curtail_batch_size = sqlc.narg('curtail_batch_size'),
    curtail_batch_interval_sec = sqlc.arg('curtail_batch_interval_sec'),
    restore_batch_size = sqlc.arg('restore_batch_size'),
    restore_batch_interval_sec = sqlc.arg('restore_batch_interval_sec'),
    include_maintenance = sqlc.arg('include_maintenance'),
    force_include_maintenance = sqlc.arg('force_include_maintenance')
WHERE id = sqlc.arg('id')
  AND org_id = sqlc.arg('org_id')
  AND site_id IS NOT DISTINCT FROM sqlc.narg('expected_site_id')
RETURNING *;

-- name: DeleteCurtailmentResponseProfileByOrg :execrows
DELETE FROM curtailment_response_profile
WHERE id = sqlc.arg('id')
  AND org_id = sqlc.arg('org_id')
  AND site_id IS NOT DISTINCT FROM sqlc.narg('expected_site_id');
