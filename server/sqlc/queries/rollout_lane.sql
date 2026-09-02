-- Rollout lanes: containers of miners with per-model firmware enforcement.

-- name: CreateRolloutLane :one
INSERT INTO rollout_lane (org_id, name)
VALUES (sqlc.arg('org_id'), sqlc.arg('name'))
RETURNING *;

-- name: GetRolloutLane :one
SELECT * FROM rollout_lane
WHERE id = sqlc.arg('lane_id') AND org_id = sqlc.arg('org_id');

-- name: ListRolloutLanes :many
SELECT * FROM rollout_lane
WHERE org_id = sqlc.arg('org_id')
ORDER BY name;

-- name: DeleteRolloutLane :exec
DELETE FROM rollout_lane
WHERE id = sqlc.arg('lane_id') AND org_id = sqlc.arg('org_id');

-- Membership. The PK on rollout_lane_member.device_id makes adding a miner
-- that already belongs to another lane a move.

-- name: AddRolloutLaneMembers :exec
INSERT INTO rollout_lane_member (device_id, lane_id)
SELECT d.id, sqlc.arg('lane_id')
FROM device d
WHERE d.org_id = sqlc.arg('org_id')
  AND d.deleted_at IS NULL
  AND d.device_identifier = ANY(sqlc.arg('device_identifiers')::text[])
ON CONFLICT (device_id) DO UPDATE SET lane_id = EXCLUDED.lane_id, added_at = now();

-- name: RemoveRolloutLaneMembers :exec
DELETE FROM rollout_lane_member m
USING device d
WHERE m.device_id = d.id
  AND m.lane_id = sqlc.arg('lane_id')
  AND d.org_id = sqlc.arg('org_id')
  AND d.device_identifier = ANY(sqlc.arg('device_identifiers')::text[]);

-- name: ListRolloutLaneMembers :many
-- All lane members for an org with model and currently reported firmware.
SELECT m.lane_id,
       d.id AS device_id,
       d.device_identifier,
       COALESCE(dd.model, '') AS model,
       COALESCE(dd.firmware_version, '') AS firmware_version
FROM rollout_lane_member m
JOIN rollout_lane l ON l.id = m.lane_id
JOIN device d ON d.id = m.device_id AND d.deleted_at IS NULL
JOIN discovered_device dd ON dd.id = d.discovered_device_id
WHERE l.org_id = sqlc.arg('org_id')
ORDER BY d.device_identifier;

-- Per-model firmware assignments.

-- name: UpsertRolloutLaneFirmware :exec
INSERT INTO rollout_lane_firmware (lane_id, model, firmware_file_id, firmware_version, assigned_by)
VALUES (sqlc.arg('lane_id'), sqlc.arg('model'), sqlc.arg('firmware_file_id'), sqlc.arg('firmware_version'), sqlc.arg('assigned_by'))
ON CONFLICT (lane_id, model) DO UPDATE
SET firmware_file_id = EXCLUDED.firmware_file_id,
    firmware_version = EXCLUDED.firmware_version,
    assigned_by = EXCLUDED.assigned_by,
    updated_at = now();

-- name: DeleteRolloutLaneFirmware :exec
DELETE FROM rollout_lane_firmware
WHERE lane_id = sqlc.arg('lane_id') AND model = sqlc.arg('model');

-- name: ListRolloutLaneFirmware :many
-- All firmware assignments for an org's lanes.
SELECT f.*
FROM rollout_lane_firmware f
JOIN rollout_lane l ON l.id = f.lane_id
WHERE l.org_id = sqlc.arg('org_id');

-- Rollouts.

-- name: CreateFirmwareRollout :one
INSERT INTO firmware_rollout (
    org_id, lane_id, model, firmware_file_id, firmware_version, created_by,
    method, stage, batch_size, batch_count,
    auto_advance, max_hashrate_drop_percent, stabilization_seconds,
    previous_firmware_file_id, previous_firmware_version
)
VALUES (
    sqlc.arg('org_id'), sqlc.arg('lane_id'), sqlc.arg('model'), sqlc.arg('firmware_file_id'), sqlc.arg('firmware_version'), sqlc.arg('created_by'),
    sqlc.arg('method'), sqlc.arg('stage'), sqlc.arg('batch_size'), sqlc.arg('batch_count'),
    sqlc.arg('auto_advance'), sqlc.narg('max_hashrate_drop_percent')::double precision, sqlc.arg('stabilization_seconds'),
    sqlc.arg('previous_firmware_file_id'), sqlc.arg('previous_firmware_version')
)
RETURNING *;

-- name: GetFirmwareRollout :one
SELECT * FROM firmware_rollout
WHERE id = sqlc.arg('rollout_id') AND org_id = sqlc.arg('org_id');

-- name: CancelActiveFirmwareRollout :exec
-- Cancels the (lane, model) pair's active rollout, recording why
-- ('superseded' by a new assignment or 'cleared' with the assignment).
UPDATE firmware_rollout
SET status = 'canceled', finished_at = now(), cancel_reason = sqlc.arg('cancel_reason')
WHERE lane_id = sqlc.arg('lane_id') AND model = sqlc.arg('model') AND status = 'active';

-- name: AbortFirmwareRollout :execrows
UPDATE firmware_rollout
SET status = 'canceled', finished_at = now(), cancel_reason = 'aborted'
WHERE id = sqlc.arg('rollout_id') AND status = 'active';

-- name: CompleteFirmwareRollout :execrows
UPDATE firmware_rollout
SET status = 'completed', finished_at = now()
WHERE id = sqlc.arg('rollout_id') AND status = 'active';

-- name: AdvanceFirmwareRolloutStage :execrows
-- Stage transitions of an active rollout (batch -> awaiting_review -> batch
-- | rest). Returns the affected row count so callers can detect a lost race.
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

-- name: ListFirmwareRollouts :many
SELECT sqlc.embed(r), l.name AS lane_name
FROM firmware_rollout r
JOIN rollout_lane l ON l.id = r.lane_id
WHERE r.org_id = sqlc.arg('org_id')
  AND (sqlc.narg('lane_id')::bigint IS NULL OR r.lane_id = sqlc.narg('lane_id'))
ORDER BY r.created_at DESC, r.id DESC
LIMIT 100;

-- name: ListActiveFirmwareRollouts :many
-- Across all orgs; drives the enforcement loop.
SELECT sqlc.embed(r), l.name AS lane_name
FROM firmware_rollout r
JOIN rollout_lane l ON l.id = r.lane_id
WHERE r.status = 'active'
ORDER BY r.id;

-- name: ListFirmwareRolloutTargets :many
-- Current lane members of the rollout's model with reported firmware, the
-- rollout's per-device bookkeeping (command sent, batch, baseline health)
-- and live health: device status, the most recent hashrate sample (NULL when
-- no sample landed in the last 15 minutes) and open error count.
SELECT d.id AS device_id,
       d.device_identifier,
       COALESCE(dd.firmware_version, '') AS firmware_version,
       COALESCE(dd.ip_address, '')::text AS ip_address,
       rd.update_sent_at,
       rd.batch_index,
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
         WHERE e.device_id = d.id AND e.closed_at IS NULL AND e.severity IN (1, 2, 3, 4))::int AS open_errors
FROM rollout_lane_member m
JOIN device d ON d.id = m.device_id AND d.deleted_at IS NULL
JOIN discovered_device dd ON dd.id = d.discovered_device_id
LEFT JOIN firmware_rollout_device rd
       ON rd.rollout_id = sqlc.arg('rollout_id') AND rd.device_id = d.id
LEFT JOIN device_status ds ON ds.device_id = d.id
LEFT JOIN LATERAL (
    SELECT dm.hash_rate_hs, dm.power_w, dm.efficiency_jh, dm.temp_c
    FROM device_metrics dm
    WHERE dm.device_identifier = d.device_identifier
      AND dm.time >= now() - INTERVAL '15 minutes'
    ORDER BY dm.time DESC
    LIMIT 1
) hm ON true
WHERE m.lane_id = sqlc.arg('lane_id')
  AND COALESCE(dd.model, '') = sqlc.arg('model')::text
ORDER BY d.device_identifier;

-- name: SnapshotFirmwareRolloutDevices :exec
-- Records devices into a rollout at creation, before any command is sent:
-- their batch (NULL for the unbatched rest) and a baseline of their health
-- so post-update evidence can be compared against each miner's own past.
INSERT INTO firmware_rollout_device (
    rollout_id, device_id, batch_index,
    baseline_status, baseline_hash_rate_hs, baseline_power_w, baseline_efficiency_jh, baseline_temp_c,
    baseline_open_errors, baseline_at
)
SELECT sqlc.arg('rollout_id'),
       d.id,
       sqlc.narg('batch_index')::int,
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
WHERE d.device_identifier = ANY(sqlc.arg('device_identifiers')::text[])
ON CONFLICT (rollout_id, device_id) DO UPDATE SET batch_index = EXCLUDED.batch_index;

-- name: MarkFirmwareRolloutDevicesSent :exec
INSERT INTO firmware_rollout_device (rollout_id, device_id, update_sent_at)
SELECT sqlc.arg('rollout_id'), d.id, now()
FROM device d
WHERE d.device_identifier = ANY(sqlc.arg('device_identifiers')::text[])
ON CONFLICT (rollout_id, device_id) DO UPDATE SET update_sent_at = now();

-- name: ListRolloutLaneFirmwareNeedingRollout :many
-- Assignments with at least one mismatched member and no active rollout.
-- The enforcement loop starts a rollout for each, which covers both miners
-- added to a lane later and miners that drifted off the assigned version.
SELECT f.lane_id, f.model, f.firmware_file_id, f.firmware_version, f.assigned_by, l.org_id
FROM rollout_lane_firmware f
JOIN rollout_lane l ON l.id = f.lane_id
WHERE NOT EXISTS (
    SELECT 1 FROM firmware_rollout r
    WHERE r.lane_id = f.lane_id AND r.model = f.model AND r.status = 'active'
)
AND EXISTS (
    SELECT 1
    FROM rollout_lane_member m
    JOIN device d ON d.id = m.device_id AND d.deleted_at IS NULL
    JOIN discovered_device dd ON dd.id = d.discovered_device_id
    WHERE m.lane_id = f.lane_id
      AND COALESCE(dd.model, '') = f.model
      AND COALESCE(dd.firmware_version, '') <> f.firmware_version
);
