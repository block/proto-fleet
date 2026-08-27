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

-- name: LockCurtailmentResponseProfileRevisionForExecution :one
SELECT id
FROM curtailment_response_profile
WHERE id = sqlc.arg('id')
  AND org_id = sqlc.arg('org_id')
  AND revision = sqlc.arg('expected_revision')
FOR SHARE;

-- name: LockCurtailmentResponseProfileAutomationMutation :exec
-- Serializes profile changes with automation create/update/enable. Both sides
-- re-read their compatibility conditions after acquiring this lock so a
-- concurrent pair cannot commit an invalid automation binding.
SELECT pg_advisory_xact_lock(
    hashtextextended(
        'curtailment_response_profile_automation:'
            || sqlc.arg('org_id')::bigint::text
            || ':'
            || sqlc.arg('profile_id')::bigint::text,
        0
    )
);

-- name: ListCurtailmentResponseProfileDeviceSitesByOrg :many
SELECT device_identifier, site_id
FROM device
WHERE org_id = sqlc.arg('org_id')
  AND device_identifier = ANY(sqlc.arg('device_identifiers')::text[])
  AND deleted_at IS NULL
ORDER BY device_identifier;

-- name: LockCurtailmentResponseProfileDeviceSitesByOrg :many
-- Topology and site writes still conflict with this lock, while command queue
-- inserts can take the foreign-key KEY SHARE lock without self-deadlocking.
SELECT device_identifier, site_id
FROM device
WHERE org_id = sqlc.arg('org_id')
  AND device_identifier = ANY(sqlc.arg('device_identifiers')::text[])
  AND deleted_at IS NULL
ORDER BY id
FOR NO KEY UPDATE;

-- name: ListResponseProfileInfrastructureDevicesByOrg :many
SELECT id, site_id, enabled
FROM infrastructure_device
WHERE org_id = sqlc.arg('org_id')
  AND id = ANY(sqlc.arg('infrastructure_device_ids')::bigint[])
  AND deleted_at IS NULL
ORDER BY id;

-- name: InsertCurtailmentResponseProfile :one
INSERT INTO curtailment_response_profile (
    org_id,
    profile_name,
    site_id,
    scope_json,
    authorization_envelope_jsonb,
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
    force_include_maintenance,
    post_event_cooldown_sec,
    force_include_all_paired_miners,
    facility_fan_device_ids,
    fan_off_delay_sec,
    fan_restore_delay_sec
) VALUES (
    sqlc.arg('org_id'),
    sqlc.arg('profile_name'),
    sqlc.narg('site_id'),
    sqlc.arg('scope_json'),
    sqlc.arg('authorization_envelope_jsonb'),
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
    sqlc.arg('force_include_maintenance'),
    sqlc.arg('post_event_cooldown_sec'),
    sqlc.arg('force_include_all_paired_miners'),
    sqlc.arg('facility_fan_device_ids'),
    sqlc.arg('fan_off_delay_sec'),
    sqlc.arg('fan_restore_delay_sec')
)
RETURNING *;

-- name: UpdateCurtailmentResponseProfile :one
UPDATE curtailment_response_profile
SET
    revision = CASE
        WHEN site_id IS DISTINCT FROM sqlc.narg('site_id')
          OR scope_json IS DISTINCT FROM sqlc.arg('scope_json')::jsonb
          OR authorization_envelope_jsonb IS DISTINCT FROM sqlc.arg('authorization_envelope_jsonb')::jsonb
          OR mode IS DISTINCT FROM sqlc.arg('mode')
          OR strategy IS DISTINCT FROM sqlc.arg('strategy')
          OR level IS DISTINCT FROM sqlc.arg('level')
          OR priority IS DISTINCT FROM sqlc.arg('priority')
          OR target_kw IS DISTINCT FROM sqlc.narg('target_kw')
          OR tolerance_kw IS DISTINCT FROM sqlc.narg('tolerance_kw')
          OR curtail_batch_size IS DISTINCT FROM sqlc.narg('curtail_batch_size')
          OR curtail_batch_interval_sec IS DISTINCT FROM sqlc.arg('curtail_batch_interval_sec')
          OR restore_batch_size IS DISTINCT FROM sqlc.arg('restore_batch_size')
          OR restore_batch_interval_sec IS DISTINCT FROM sqlc.arg('restore_batch_interval_sec')
          OR include_maintenance IS DISTINCT FROM sqlc.arg('include_maintenance')
          OR force_include_maintenance IS DISTINCT FROM sqlc.arg('force_include_maintenance')
          OR post_event_cooldown_sec IS DISTINCT FROM sqlc.arg('post_event_cooldown_sec')
          OR force_include_all_paired_miners IS DISTINCT FROM sqlc.arg('force_include_all_paired_miners')
          OR facility_fan_device_ids IS DISTINCT FROM sqlc.arg('facility_fan_device_ids')::bigint[]
          OR fan_off_delay_sec IS DISTINCT FROM sqlc.arg('fan_off_delay_sec')
          OR fan_restore_delay_sec IS DISTINCT FROM sqlc.arg('fan_restore_delay_sec')
        THEN gen_random_uuid()
        ELSE revision
    END,
    profile_name = sqlc.arg('profile_name'),
    site_id = sqlc.narg('site_id'),
    scope_json = sqlc.arg('scope_json'),
    authorization_envelope_jsonb = sqlc.arg('authorization_envelope_jsonb'),
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
    force_include_maintenance = sqlc.arg('force_include_maintenance'),
    post_event_cooldown_sec = sqlc.arg('post_event_cooldown_sec'),
    force_include_all_paired_miners = sqlc.arg('force_include_all_paired_miners'),
    facility_fan_device_ids = sqlc.arg('facility_fan_device_ids'),
    fan_off_delay_sec = sqlc.arg('fan_off_delay_sec'),
    fan_restore_delay_sec = sqlc.arg('fan_restore_delay_sec')
WHERE id = sqlc.arg('id')
  AND org_id = sqlc.arg('org_id')
  AND revision = sqlc.arg('expected_revision')
  AND site_id IS NOT DISTINCT FROM sqlc.narg('expected_site_id')
  AND scope_json = sqlc.arg('expected_scope_json')::jsonb
  AND facility_fan_device_ids = sqlc.arg('expected_facility_fan_device_ids')::bigint[]
  AND fan_off_delay_sec = sqlc.arg('expected_fan_off_delay_sec')
  AND fan_restore_delay_sec = sqlc.arg('expected_fan_restore_delay_sec')
RETURNING *;

-- name: DeleteCurtailmentResponseProfileByOrg :execrows
DELETE FROM curtailment_response_profile
WHERE id = sqlc.arg('id')
  AND org_id = sqlc.arg('org_id')
  AND site_id IS NOT DISTINCT FROM sqlc.narg('expected_site_id')
  AND scope_json = sqlc.arg('expected_scope_json')::jsonb
  AND authorization_envelope_jsonb = sqlc.arg('expected_authorization_envelope_json')::jsonb
  AND facility_fan_device_ids = sqlc.arg('expected_facility_fan_device_ids')::bigint[]
  AND fan_off_delay_sec = sqlc.arg('expected_fan_off_delay_sec')
  AND fan_restore_delay_sec = sqlc.arg('expected_fan_restore_delay_sec');
