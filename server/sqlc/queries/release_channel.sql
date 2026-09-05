-- Firmware release channels and the rollouts that enforce them.

-- --- Channels ---

-- name: CreateReleaseChannel :one
INSERT INTO release_channel (
    org_id, name, description, created_by,
    method, order_by, batch_size, pilot_size, wait_between_batches_seconds,
    review_after_each_batch, auto_continue, stabilization_seconds,
    max_hashrate_drop_percent, max_efficiency_increase_percent, max_temp_increase_c, max_new_errors,
    max_concurrent_offline
)
VALUES (
    sqlc.arg('org_id'), sqlc.arg('name'), sqlc.arg('description'), sqlc.arg('created_by'),
    sqlc.arg('method'), sqlc.arg('order_by'), sqlc.arg('batch_size'), sqlc.arg('pilot_size'), sqlc.arg('wait_between_batches_seconds'),
    sqlc.arg('review_after_each_batch'), sqlc.arg('auto_continue'), sqlc.arg('stabilization_seconds'),
    sqlc.narg('max_hashrate_drop_percent')::double precision, sqlc.narg('max_efficiency_increase_percent')::double precision,
    sqlc.narg('max_temp_increase_c')::double precision, sqlc.narg('max_new_errors')::int,
    sqlc.arg('max_concurrent_offline')
)
RETURNING *;

-- name: UpdateReleaseChannel :one
UPDATE release_channel
SET name = sqlc.arg('name'),
    description = sqlc.arg('description'),
    method = sqlc.arg('method'),
    order_by = sqlc.arg('order_by'),
    batch_size = sqlc.arg('batch_size'),
    pilot_size = sqlc.arg('pilot_size'),
    wait_between_batches_seconds = sqlc.arg('wait_between_batches_seconds'),
    review_after_each_batch = sqlc.arg('review_after_each_batch'),
    auto_continue = sqlc.arg('auto_continue'),
    stabilization_seconds = sqlc.arg('stabilization_seconds'),
    max_hashrate_drop_percent = sqlc.narg('max_hashrate_drop_percent')::double precision,
    max_efficiency_increase_percent = sqlc.narg('max_efficiency_increase_percent')::double precision,
    max_temp_increase_c = sqlc.narg('max_temp_increase_c')::double precision,
    max_new_errors = sqlc.narg('max_new_errors')::int,
    max_concurrent_offline = sqlc.arg('max_concurrent_offline'),
    updated_at = now()
WHERE id = sqlc.arg('channel_id') AND org_id = sqlc.arg('org_id')
RETURNING *;

-- name: GetReleaseChannel :one
SELECT * FROM release_channel
WHERE id = sqlc.arg('channel_id') AND org_id = sqlc.arg('org_id');

-- name: ListReleaseChannels :many
SELECT * FROM release_channel
WHERE org_id = sqlc.arg('org_id')
ORDER BY name;

-- name: DeleteReleaseChannel :execrows
DELETE FROM release_channel
WHERE id = sqlc.arg('channel_id') AND org_id = sqlc.arg('org_id');

-- name: LockReleaseChannelScopes :exec
-- Serializes scope writes per org so the overlap check and the target
-- insert happen in one critical section.
SELECT pg_advisory_xact_lock(hashtextextended('release_channel_scope:' || (sqlc.arg('org_id')::bigint)::text, 0));

-- name: DeleteReleaseChannelTargets :exec
DELETE FROM release_channel_target WHERE channel_id = sqlc.arg('channel_id');

-- name: InsertReleaseChannelTargets :exec
-- target_types and target_ids are parallel arrays.
INSERT INTO release_channel_target (channel_id, target_type, target_id)
SELECT sqlc.arg('channel_id'),
       unnest(sqlc.arg('target_types')::text[]),
       unnest(sqlc.arg('target_ids')::bigint[])
ON CONFLICT DO NOTHING;

-- name: ListReleaseChannelTargets :many
-- Targets of every channel in the org; miner targets carry their identifier.
SELECT t.channel_id,
       t.target_type,
       t.target_id,
       COALESCE(d.device_identifier, '')::text AS device_identifier
FROM release_channel_target t
JOIN release_channel c ON c.id = t.channel_id
LEFT JOIN device d ON t.target_type = 'miner' AND d.id = t.target_id
WHERE c.org_id = sqlc.arg('org_id')
ORDER BY t.channel_id, t.target_type, t.target_id;

-- name: ListDeviceIDsByIdentifiers :many
-- Resolves an org's device identifiers to ids; unknown identifiers are dropped.
SELECT d.id, d.device_identifier
FROM device d
WHERE d.org_id = sqlc.arg('org_id')
  AND d.deleted_at IS NULL
  AND d.device_identifier = ANY(sqlc.arg('device_identifiers')::text[]);

-- --- Membership ---

-- name: ListReleaseChannelMembers :many
-- Every miner resolved into one of the org's channels, with its model and
-- reported firmware.
SELECT m.channel_id,
       m.device_id,
       m.conflicted,
       d.device_identifier,
       COALESCE(dd.model, '')::text AS model,
       COALESCE(dd.firmware_version, '')::text AS firmware_version
FROM release_channel_member m
JOIN device d ON d.id = m.device_id
JOIN discovered_device dd ON dd.id = d.discovered_device_id
WHERE m.org_id = sqlc.arg('org_id')
ORDER BY d.device_identifier;

-- name: ListReleaseChannelMinersPage :many
-- One page of a channel's members, optionally one model, ordered by
-- identifier. The cursor is the (device_identifier, device_id) of the last
-- row of the previous page.
SELECT m.device_id,
       m.conflicted,
       d.device_identifier,
       COALESCE(dd.model, '')::text AS model,
       COALESCE(dd.firmware_version, '')::text AS firmware_version
FROM release_channel_member m
JOIN release_channel c ON c.id = m.channel_id
JOIN device d ON d.id = m.device_id
JOIN discovered_device dd ON dd.id = d.discovered_device_id
WHERE m.channel_id = sqlc.arg('channel_id')
  AND c.org_id = sqlc.arg('org_id')
  AND (sqlc.narg('model')::text IS NULL OR dd.model = sqlc.narg('model'))
  AND (
    sqlc.narg('after_identifier')::text IS NULL
    OR (d.device_identifier, d.id) > (sqlc.narg('after_identifier')::text, sqlc.narg('after_device_id')::bigint)
  )
ORDER BY d.device_identifier, d.id
LIMIT sqlc.arg('page_limit');

-- name: ResolveReleaseChannelScope :many
-- Miners a candidate scope covers, each with the other channel (if any)
-- whose selectors already match it. Used to preview a scope and to reject
-- overlapping saves; exclude_channel_id is the channel being edited.
WITH scoped AS (
    SELECT d.id AS device_id
    FROM device d
    WHERE d.org_id = sqlc.arg('org_id')
      AND d.deleted_at IS NULL
      AND d.device_identifier = ANY(sqlc.arg('device_identifiers')::text[])
    UNION
    SELECT d.id
    FROM fleet_device_placement p
    JOIN device d ON d.device_identifier = p.device_id AND d.org_id = p.org_id
    WHERE p.org_id = sqlc.arg('org_id')
      AND (
           p.group_id = ANY(sqlc.arg('group_ids')::bigint[])
        OR p.rack_id = ANY(sqlc.arg('rack_ids')::bigint[])
        OR p.building_id = ANY(sqlc.arg('building_ids')::bigint[])
        OR p.site_id = ANY(sqlc.arg('site_ids')::bigint[])
      )
)
SELECT s.device_id,
       d.device_identifier,
       COALESCE(dd.model, '')::text AS model,
       COALESCE(dd.firmware_version, '')::text AS firmware_version,
       COALESCE(owner.channel_id, 0)::bigint AS owner_channel_id,
       COALESCE(owner.name, '')::text AS owner_channel_name
FROM scoped s
JOIN device d ON d.id = s.device_id
JOIN discovered_device dd ON dd.id = d.discovered_device_id
LEFT JOIN LATERAL (
    SELECT c.id AS channel_id, c.name
    FROM release_channel_match rm
    JOIN release_channel c ON c.id = rm.channel_id
    WHERE rm.device_id = s.device_id
      AND rm.channel_id <> sqlc.arg('exclude_channel_id')
    ORDER BY c.id
    LIMIT 1
) owner ON true
ORDER BY d.device_identifier;

-- --- Firmware assignments ---

-- name: UpsertReleaseChannelFirmware :one
INSERT INTO release_channel_firmware (channel_id, model, firmware_file_id, firmware_version, assigned_by)
VALUES (sqlc.arg('channel_id'), sqlc.arg('model'), sqlc.arg('firmware_file_id'), sqlc.arg('firmware_version'), sqlc.arg('assigned_by'))
ON CONFLICT (channel_id, model) DO UPDATE
SET firmware_file_id = EXCLUDED.firmware_file_id,
    firmware_version = EXCLUDED.firmware_version,
    assigned_by = EXCLUDED.assigned_by,
    updated_at = now()
RETURNING *;

-- name: DeleteReleaseChannelFirmware :exec
DELETE FROM release_channel_firmware
WHERE channel_id = sqlc.arg('channel_id') AND model = sqlc.arg('model');

-- name: ListReleaseChannelFirmware :many
-- All firmware assignments for an org's channels.
SELECT f.*
FROM release_channel_firmware f
JOIN release_channel c ON c.id = f.channel_id
WHERE c.org_id = sqlc.arg('org_id');

-- name: ListReleaseChannelMismatchedMembers :many
-- Channel members of one model not running the given version, that are not
-- already part of rollout_id (0 for a new rollout) and were not halted
-- (failed / canceled) for this version since the assignment was last made.
-- Carries the latest efficiency sample for ordering.
SELECT d.id AS device_id,
       d.device_identifier,
       hm.efficiency_jh
FROM release_channel_member m
JOIN device d ON d.id = m.device_id
JOIN discovered_device dd ON dd.id = d.discovered_device_id
LEFT JOIN LATERAL (
    SELECT dm.efficiency_jh
    FROM device_metrics dm
    WHERE dm.device_identifier = d.device_identifier
      AND dm.time >= now() - INTERVAL '15 minutes'
    ORDER BY dm.time DESC
    LIMIT 1
) hm ON true
WHERE m.channel_id = sqlc.arg('channel_id')
  AND COALESCE(dd.model, '') = sqlc.arg('model')::text
  AND COALESCE(dd.firmware_version, '') <> sqlc.arg('firmware_version')::text
  AND NOT EXISTS (
      SELECT 1 FROM firmware_rollout_device rd
      WHERE rd.rollout_id = sqlc.arg('rollout_id') AND rd.device_id = d.id
  )
  AND NOT EXISTS (
      SELECT 1
      FROM firmware_rollout_device rd
      JOIN firmware_rollout r ON r.id = rd.rollout_id
      WHERE rd.device_id = d.id
        AND rd.halted_at IS NOT NULL
        AND r.channel_id = sqlc.arg('channel_id')
        AND r.model = sqlc.arg('model')::text
        AND r.firmware_version = sqlc.arg('firmware_version')::text
        AND r.created_at >= sqlc.arg('assigned_at')::timestamptz
  )
ORDER BY d.device_identifier;

-- name: ListReleaseChannelFirmwareNeedingRollout :many
-- Assignments with no active rollout and at least one member that is not
-- on the assigned version and was not halted for it: late joiners and
-- miners that drifted off the version.
SELECT f.channel_id, f.model, f.firmware_file_id, f.firmware_version, f.assigned_by, f.updated_at, c.org_id
FROM release_channel_firmware f
JOIN release_channel c ON c.id = f.channel_id
WHERE NOT EXISTS (
    SELECT 1 FROM firmware_rollout r
    WHERE r.channel_id = f.channel_id AND r.model = f.model AND r.status = 'active'
)
AND EXISTS (
    SELECT 1
    FROM release_channel_member m
    JOIN device d ON d.id = m.device_id
    JOIN discovered_device dd ON dd.id = d.discovered_device_id
    WHERE m.channel_id = f.channel_id
      AND COALESCE(dd.model, '') = f.model
      AND COALESCE(dd.firmware_version, '') <> f.firmware_version
      AND NOT EXISTS (
          SELECT 1
          FROM firmware_rollout_device rd
          JOIN firmware_rollout r ON r.id = rd.rollout_id
          WHERE rd.device_id = d.id
            AND rd.halted_at IS NOT NULL
            AND r.channel_id = f.channel_id
            AND r.model = f.model
            AND r.firmware_version = f.firmware_version
            AND r.created_at >= f.updated_at
      )
)
ORDER BY f.channel_id, f.model;

-- --- Rollouts ---

-- name: CreateFirmwareRollout :one
INSERT INTO firmware_rollout (
    org_id, channel_id, model, firmware_file_id, firmware_version,
    previous_firmware_file_id, previous_firmware_version,
    stage, created_by,
    method, order_by, batch_size, pilot_size, wait_between_batches_seconds,
    review_after_each_batch, auto_continue, stabilization_seconds,
    max_hashrate_drop_percent, max_efficiency_increase_percent, max_temp_increase_c, max_new_errors,
    max_concurrent_offline, batch_count
)
VALUES (
    sqlc.arg('org_id'), sqlc.arg('channel_id'), sqlc.arg('model'), sqlc.arg('firmware_file_id'), sqlc.arg('firmware_version'),
    sqlc.arg('previous_firmware_file_id'), sqlc.arg('previous_firmware_version'),
    sqlc.arg('stage'), sqlc.arg('created_by'),
    sqlc.arg('method'), sqlc.arg('order_by'), sqlc.arg('batch_size'), sqlc.arg('pilot_size'), sqlc.arg('wait_between_batches_seconds'),
    sqlc.arg('review_after_each_batch'), sqlc.arg('auto_continue'), sqlc.arg('stabilization_seconds'),
    sqlc.narg('max_hashrate_drop_percent')::double precision, sqlc.narg('max_efficiency_increase_percent')::double precision,
    sqlc.narg('max_temp_increase_c')::double precision, sqlc.narg('max_new_errors')::int,
    sqlc.arg('max_concurrent_offline'), sqlc.arg('batch_count')
)
RETURNING *;

-- name: GetFirmwareRollout :one
SELECT * FROM firmware_rollout
WHERE id = sqlc.arg('rollout_id') AND org_id = sqlc.arg('org_id');

-- name: GetFirmwareRolloutWithChannel :one
SELECT sqlc.embed(r), c.name AS channel_name
FROM firmware_rollout r
JOIN release_channel c ON c.id = r.channel_id
WHERE r.id = sqlc.arg('rollout_id') AND r.org_id = sqlc.arg('org_id');

-- name: ListFirmwareRollouts :many
-- Newest first. The cursor is the (created_at, id) of the last row of the
-- previous page; rows strictly older than it are returned.
SELECT sqlc.embed(r), c.name AS channel_name
FROM firmware_rollout r
JOIN release_channel c ON c.id = r.channel_id
WHERE r.org_id = sqlc.arg('org_id')
  AND (sqlc.narg('channel_id')::bigint IS NULL OR r.channel_id = sqlc.narg('channel_id'))
  AND (sqlc.narg('status')::text IS NULL OR r.status = sqlc.narg('status'))
  AND (
    sqlc.narg('before_created_at')::timestamptz IS NULL
    OR (r.created_at, r.id) < (sqlc.narg('before_created_at')::timestamptz, sqlc.narg('before_id')::bigint)
  )
ORDER BY r.created_at DESC, r.id DESC
LIMIT sqlc.arg('page_limit');

-- name: ListActiveFirmwareRollouts :many
-- Across all orgs; drives the enforcement loop.
SELECT sqlc.embed(r), c.name AS channel_name
FROM firmware_rollout r
JOIN release_channel c ON c.id = r.channel_id
WHERE r.status = 'active'
ORDER BY r.id;

-- name: CancelActiveFirmwareRollout :exec
-- Cancels the (channel, model) pair's active rollout because its assignment
-- changed: 'superseded', 'rolled_back' or 'cleared'.
UPDATE firmware_rollout
SET status = 'canceled', finished_at = now(), cancel_reason = sqlc.arg('cancel_reason')
WHERE channel_id = sqlc.arg('channel_id') AND model = sqlc.arg('model') AND status = 'active';

-- name: CancelFirmwareRollout :execrows
UPDATE firmware_rollout
SET status = 'canceled', finished_at = now(), cancel_reason = 'canceled_remaining'
WHERE id = sqlc.arg('rollout_id') AND status = 'active';

-- name: FinishFirmwareRollout :execrows
-- Ends an active rollout as 'completed' or 'completed_with_failures'.
UPDATE firmware_rollout
SET status = sqlc.arg('status'), finished_at = now()
WHERE id = sqlc.arg('rollout_id') AND status = 'active';

-- name: AdvanceFirmwareRolloutStage :execrows
-- Stage transitions of an active rollout. Returns the affected row count so
-- callers can detect a lost race.
UPDATE firmware_rollout
SET stage = sqlc.arg('stage'),
    current_batch = sqlc.arg('current_batch'),
    stage_changed_at = now()
WHERE id = sqlc.arg('rollout_id')
  AND status = 'active'
  AND stage = sqlc.arg('from_stage');

-- name: PauseFirmwareRollout :execrows
UPDATE firmware_rollout
SET paused_at = now()
WHERE id = sqlc.arg('rollout_id') AND status = 'active' AND paused_at IS NULL;

-- name: ResumeFirmwareRollout :execrows
UPDATE firmware_rollout
SET paused_at = NULL
WHERE id = sqlc.arg('rollout_id') AND status = 'active' AND paused_at IS NOT NULL;

-- --- Rollout devices ---

-- name: ListFirmwareRolloutDevices :many
-- Every miner in a rollout with its bookkeeping, baseline, live health (device
-- status, latest telemetry within 15 minutes, open errors) and whether it is
-- still a member of the channel for the rollout's model.
SELECT rd.device_id,
       d.device_identifier,
       COALESCE(dd.firmware_version, '')::text AS firmware_version,
       COALESCE(dd.ip_address, '')::text AS ip_address,
       rd.batch_index,
       rd.position,
       rd.attempts,
       rd.first_sent_at,
       rd.last_sent_at,
       rd.halted_at,
       rd.halt_reason,
       rd.last_error,
       rd.excluded_at,
       rd.baseline_status,
       rd.baseline_hash_rate_hs,
       rd.baseline_power_w,
       rd.baseline_efficiency_jh,
       rd.baseline_temp_c,
       rd.baseline_open_errors,
       COALESCE(ds.status::text, '')::text AS status,
       hm.hash_rate_hs,
       hm.power_w,
       hm.efficiency_jh,
       hm.temp_c,
       (SELECT count(*) FROM errors e
         WHERE e.device_id = d.id AND e.closed_at IS NULL AND e.severity IN (1, 2, 3, 4))::int AS open_errors,
       EXISTS (
           SELECT 1 FROM release_channel_member m
           WHERE m.device_id = d.id AND m.channel_id = r.channel_id
       ) AND COALESCE(dd.model, '') = r.model AS in_scope
FROM firmware_rollout_device rd
JOIN firmware_rollout r ON r.id = rd.rollout_id
JOIN device d ON d.id = rd.device_id
JOIN discovered_device dd ON dd.id = d.discovered_device_id
LEFT JOIN device_status ds ON ds.device_id = d.id
LEFT JOIN LATERAL (
    SELECT dm.hash_rate_hs, dm.power_w, dm.efficiency_jh, dm.temp_c
    FROM device_metrics dm
    WHERE dm.device_identifier = d.device_identifier
      AND dm.time >= now() - INTERVAL '15 minutes'
    ORDER BY dm.time DESC
    LIMIT 1
) hm ON true
WHERE rd.rollout_id = sqlc.arg('rollout_id')
ORDER BY rd.position NULLS LAST, d.device_identifier;

-- name: SnapshotFirmwareRolloutDevices :exec
-- Adds miners to a rollout with their batch (NULL for the unbatched rest /
-- late joiners), their order (position_offset + index in device_ids; NULL
-- offset for late joiners) and a baseline of their health, so post-update
-- evidence can be compared against each miner's own past. A miner that
-- re-enters the scope after being excluded is re-included and keeps its
-- original batch and order.
INSERT INTO firmware_rollout_device (
    rollout_id, device_id, batch_index, position,
    baseline_status, baseline_hash_rate_hs, baseline_power_w, baseline_efficiency_jh, baseline_temp_c,
    baseline_open_errors, baseline_at
)
SELECT sqlc.arg('rollout_id'),
       d.id,
       sqlc.narg('batch_index')::int,
       sqlc.narg('position_offset')::int + array_position(sqlc.arg('device_ids')::bigint[], d.id),
       ds.status::text,
       hm.hash_rate_hs,
       hm.power_w,
       hm.efficiency_jh,
       hm.temp_c,
       (SELECT count(*) FROM errors e
         WHERE e.device_id = d.id AND e.closed_at IS NULL AND e.severity IN (1, 2, 3, 4))::int,
       now()
FROM device d
LEFT JOIN device_status ds ON ds.device_id = d.id
LEFT JOIN LATERAL (
    SELECT dm.hash_rate_hs, dm.power_w, dm.efficiency_jh, dm.temp_c
    FROM device_metrics dm
    WHERE dm.device_identifier = d.device_identifier
      AND dm.time >= now() - INTERVAL '15 minutes'
    ORDER BY dm.time DESC
    LIMIT 1
) hm ON true
WHERE d.id = ANY(sqlc.arg('device_ids')::bigint[])
ON CONFLICT (rollout_id, device_id) DO UPDATE
SET batch_index = COALESCE(firmware_rollout_device.batch_index, EXCLUDED.batch_index),
    position = COALESCE(firmware_rollout_device.position, EXCLUDED.position),
    excluded_at = NULL;

-- name: MarkFirmwareRolloutDevicesSent :exec
UPDATE firmware_rollout_device
SET attempts = attempts + 1,
    first_sent_at = COALESCE(first_sent_at, now()),
    last_sent_at = now()
WHERE rollout_id = sqlc.arg('rollout_id')
  AND device_id = ANY(sqlc.arg('device_ids')::bigint[]);

-- name: HaltFirmwareRolloutDevices :exec
-- Stops retrying miners for this version: 'failed' (attempts exhausted) or
-- 'canceled' (operator canceled the remaining updates).
UPDATE firmware_rollout_device
SET halted_at = now(),
    halt_reason = sqlc.arg('halt_reason'),
    last_error = sqlc.arg('last_error')
WHERE rollout_id = sqlc.arg('rollout_id')
  AND device_id = ANY(sqlc.arg('device_ids')::bigint[])
  AND halted_at IS NULL;

-- name: RequeueFirmwareRolloutDevices :many
-- Re-queues every halted miner of an active rollout from scratch and
-- returns them.
UPDATE firmware_rollout_device
SET halted_at = NULL,
    halt_reason = '',
    last_error = '',
    attempts = 0,
    first_sent_at = NULL,
    last_sent_at = NULL
WHERE rollout_id = sqlc.arg('rollout_id')
  AND halted_at IS NOT NULL
RETURNING device_id;

-- name: ReleaseFirmwareRolloutDeviceHalts :many
-- Lets drift correction pick the halted miners of a finished rollout up
-- again, keeping the rollout's own record of why they stopped. Returns them.
UPDATE firmware_rollout_device
SET halted_at = NULL
WHERE rollout_id = sqlc.arg('rollout_id')
  AND halted_at IS NOT NULL
RETURNING device_id;

-- name: ExcludeFirmwareRolloutDevices :exec
UPDATE firmware_rollout_device
SET excluded_at = now()
WHERE rollout_id = sqlc.arg('rollout_id')
  AND device_id = ANY(sqlc.arg('device_ids')::bigint[])
  AND excluded_at IS NULL;
