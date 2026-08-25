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
INSERT INTO firmware_rollout (org_id, lane_id, model, firmware_file_id, firmware_version, created_by)
VALUES (sqlc.arg('org_id'), sqlc.arg('lane_id'), sqlc.arg('model'), sqlc.arg('firmware_file_id'), sqlc.arg('firmware_version'), sqlc.arg('created_by'))
RETURNING *;

-- name: CancelActiveFirmwareRollout :exec
UPDATE firmware_rollout
SET status = 'canceled', finished_at = now()
WHERE lane_id = sqlc.arg('lane_id') AND model = sqlc.arg('model') AND status = 'active';

-- name: CompleteFirmwareRollout :exec
UPDATE firmware_rollout
SET status = 'completed', finished_at = now()
WHERE id = sqlc.arg('rollout_id') AND status = 'active';

-- name: ListFirmwareRollouts :many
SELECT r.*, l.name AS lane_name
FROM firmware_rollout r
JOIN rollout_lane l ON l.id = r.lane_id
WHERE r.org_id = sqlc.arg('org_id')
  AND (sqlc.narg('lane_id')::bigint IS NULL OR r.lane_id = sqlc.narg('lane_id'))
ORDER BY r.created_at DESC, r.id DESC
LIMIT 100;

-- name: ListActiveFirmwareRollouts :many
-- Across all orgs; drives the enforcement loop.
SELECT r.*, l.name AS lane_name
FROM firmware_rollout r
JOIN rollout_lane l ON l.id = r.lane_id
WHERE r.status = 'active'
ORDER BY r.id;

-- name: ListFirmwareRolloutTargets :many
-- Current lane members of the rollout's model with reported firmware and
-- whether an update command was already sent by this rollout.
SELECT d.id AS device_id,
       d.device_identifier,
       COALESCE(dd.firmware_version, '') AS firmware_version,
       rd.update_sent_at
FROM rollout_lane_member m
JOIN device d ON d.id = m.device_id AND d.deleted_at IS NULL
JOIN discovered_device dd ON dd.id = d.discovered_device_id
LEFT JOIN firmware_rollout_device rd
       ON rd.rollout_id = sqlc.arg('rollout_id') AND rd.device_id = d.id
WHERE m.lane_id = sqlc.arg('lane_id')
  AND COALESCE(dd.model, '') = sqlc.arg('model')::text
ORDER BY d.device_identifier;

-- name: MarkFirmwareRolloutDevicesSent :exec
INSERT INTO firmware_rollout_device (rollout_id, device_id)
SELECT sqlc.arg('rollout_id'), d.id
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
