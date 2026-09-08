-- Firmware release channels and the rollouts that enforce them. Pair keys and
-- observed device identities are compared through release_channel_pair_key()
-- (migration 000148); firmware is identified by payload checksum.

-- --- Channels ---

-- name: CreateReleaseChannel :one
INSERT INTO release_channel (
    org_id, name, description, created_by,
    method, order_by, batch_size, pilot_size, wait_between_batches_seconds,
    review_after_each_batch, auto_continue, stabilization_seconds,
    max_hashrate_drop_percent, max_efficiency_increase_percent, max_temp_increase_c, max_new_errors,
    min_sample_coverage_percent, max_concurrent_offline, controller_timeout_seconds
)
VALUES (
    sqlc.arg('org_id'), sqlc.arg('name'), sqlc.arg('description'), sqlc.arg('created_by'),
    sqlc.arg('method'), sqlc.arg('order_by'), sqlc.arg('batch_size'), sqlc.arg('pilot_size'), sqlc.arg('wait_between_batches_seconds'),
    sqlc.arg('review_after_each_batch'), sqlc.arg('auto_continue'), sqlc.arg('stabilization_seconds'),
    sqlc.narg('max_hashrate_drop_percent')::double precision, sqlc.narg('max_efficiency_increase_percent')::double precision,
    sqlc.narg('max_temp_increase_c')::double precision, sqlc.narg('max_new_errors')::int,
    sqlc.narg('min_sample_coverage_percent')::double precision, sqlc.arg('max_concurrent_offline'), sqlc.arg('controller_timeout_seconds')
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
    min_sample_coverage_percent = sqlc.narg('min_sample_coverage_percent')::double precision,
    max_concurrent_offline = sqlc.arg('max_concurrent_offline'),
    controller_timeout_seconds = sqlc.arg('controller_timeout_seconds'),
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
-- Serializes scope writes per org. Call inside the transaction that checks
-- overlap and writes targets: the lock is transaction-scoped and a no-op
-- outside one.
SELECT pg_advisory_xact_lock(hashtextextended('release_channel_scope:' || (sqlc.arg('org_id')::bigint)::text, 0));

-- name: DeleteReleaseChannelTargets :exec
DELETE FROM release_channel_target WHERE channel_id = sqlc.arg('channel_id');

-- name: InsertReleaseChannelTargets :exec
-- Site, building, rack and group selectors; target_types and target_ids are
-- parallel arrays.
INSERT INTO release_channel_target (channel_id, target_type, target_id)
SELECT sqlc.arg('channel_id'),
       unnest(sqlc.arg('target_types')::text[]),
       unnest(sqlc.arg('target_ids')::bigint[])
ON CONFLICT DO NOTHING;

-- name: InsertReleaseChannelMinerTargets :exec
INSERT INTO release_channel_target (channel_id, target_type, device_identifier)
SELECT sqlc.arg('channel_id'), 'miner', unnest(sqlc.arg('device_identifiers')::text[])
ON CONFLICT DO NOTHING;

-- name: ListReleaseChannelTargets :many
-- Selectors of every channel in the org.
SELECT t.channel_id,
       t.target_type,
       COALESCE(t.target_id, 0)::bigint AS target_id,
       COALESCE(t.device_identifier, '')::text AS device_identifier
FROM release_channel_target t
JOIN release_channel c ON c.id = t.channel_id
WHERE c.org_id = sqlc.arg('org_id')
ORDER BY t.channel_id, t.target_type, t.target_id, t.device_identifier;

-- name: ListDeviceIDsByIdentifiers :many
-- Resolves an org's device identifiers to ids; unknown identifiers are dropped.
SELECT d.id, d.device_identifier
FROM device d
WHERE d.org_id = sqlc.arg('org_id')
  AND d.deleted_at IS NULL
  AND d.device_identifier = ANY(sqlc.arg('device_identifiers')::text[]);

-- --- Membership ---

-- name: ListReleaseChannelMembers :many
-- Every miner resolved into one of the org's channels, with its observed
-- identity, reported firmware and managed-deployment provenance.
SELECT m.channel_id,
       m.device_id,
       m.conflicted,
       d.device_identifier,
       COALESCE(dd.manufacturer, '')::text AS manufacturer,
       COALESCE(dd.model, '')::text AS model,
       COALESCE(dd.firmware_version, '')::text AS firmware_version,
       COALESCE(dep.firmware_checksum, '')::text AS last_deployed_firmware_checksum
FROM release_channel_member m
JOIN device d ON d.id = m.device_id
JOIN discovered_device dd ON dd.id = d.discovered_device_id
LEFT JOIN device_firmware_deployment dep ON dep.device_id = d.id
WHERE m.org_id = sqlc.arg('org_id')
ORDER BY d.device_identifier;

-- name: ListReleaseChannelMinersPage :many
-- One page of a channel's members, optionally filtered by observed
-- manufacturer and model (verbatim), ordered by identifier. The cursor is the
-- (device_identifier, device_id) of the last row of the previous page.
SELECT m.device_id,
       m.conflicted,
       d.device_identifier,
       COALESCE(dd.manufacturer, '')::text AS manufacturer,
       COALESCE(dd.model, '')::text AS model,
       COALESCE(dd.firmware_version, '')::text AS firmware_version,
       COALESCE(dep.firmware_checksum, '')::text AS last_deployed_firmware_checksum
FROM release_channel_member m
JOIN device d ON d.id = m.device_id
JOIN discovered_device dd ON dd.id = d.discovered_device_id
LEFT JOIN device_firmware_deployment dep ON dep.device_id = d.id
WHERE m.org_id = sqlc.arg('org_id')
  AND m.channel_id = sqlc.arg('channel_id')
  AND (sqlc.narg('manufacturer')::text IS NULL OR COALESCE(dd.manufacturer, '') = sqlc.narg('manufacturer'))
  AND (sqlc.narg('model')::text IS NULL OR COALESCE(dd.model, '') = sqlc.narg('model'))
  AND (
    sqlc.narg('after_identifier')::text IS NULL
    OR (d.device_identifier, d.id) > (sqlc.narg('after_identifier')::text, sqlc.narg('after_device_id')::bigint)
  )
ORDER BY d.device_identifier, d.id
LIMIT sqlc.arg('page_limit');

-- name: ListReleaseChannelModelGroupsPage :many
-- One page of a channel's manufacturer/model groups: every observed pair among
-- its members plus every assigned pair with no current members, each joined to
-- its assignment (by folded key) and active rollout. Ordered by observed
-- manufacturer then model; the cursor is the pair of the last row.
-- on_target_count follows the contract's definition: reported version and
-- provenance equal the assignment.
WITH members AS (
    SELECT m.device_id,
           COALESCE(dd.manufacturer, '')::text AS manufacturer,
           COALESCE(dd.model, '')::text AS model,
           COALESCE(dd.firmware_version, '')::text AS firmware_version,
           COALESCE(dep.firmware_checksum, '')::text AS deployed_checksum
    FROM release_channel_member m
    JOIN device d ON d.id = m.device_id
    JOIN discovered_device dd ON dd.id = d.discovered_device_id
    LEFT JOIN device_firmware_deployment dep ON dep.device_id = d.id
    WHERE m.org_id = sqlc.arg('org_id') AND m.channel_id = sqlc.arg('channel_id')
), groups AS (
    SELECT manufacturer,
           model,
           count(*)::int AS miner_count,
           (array_agg(DISTINCT firmware_version ORDER BY firmware_version))[1:10]::text[] AS reported_versions,
           count(DISTINCT firmware_version)::int AS reported_version_count
    FROM members
    GROUP BY manufacturer, model
    UNION ALL
    SELECT f.manufacturer, f.model, 0, ARRAY[]::text[], 0
    FROM release_channel_firmware f
    WHERE f.channel_id = sqlc.arg('channel_id')
      AND f.firmware_checksum <> ''
      AND NOT EXISTS (
          SELECT 1 FROM members mm
          WHERE release_channel_pair_key(mm.manufacturer) = release_channel_pair_key(f.manufacturer)
            AND release_channel_pair_key(mm.model) = release_channel_pair_key(f.model)
      )
)
SELECT g.manufacturer,
       g.model,
       g.miner_count,
       g.reported_versions,
       g.reported_version_count,
       COALESCE(f.firmware_checksum, '')::text AS firmware_checksum,
       COALESCE(f.firmware_version, '')::text AS firmware_version,
       COALESCE(f.firmware_target_manufacturer, '')::text AS firmware_target_manufacturer,
       COALESCE(f.firmware_target_model, '')::text AS firmware_target_model,
       COALESCE(f.assignment_generation, 0)::bigint AS assignment_generation,
       COALESCE(r.id, 0)::bigint AS active_rollout_id,
       (SELECT count(*) FROM members mm
         WHERE mm.manufacturer = g.manufacturer AND mm.model = g.model
           AND f.firmware_checksum IS NOT NULL AND f.firmware_checksum <> ''
           AND mm.firmware_version = f.firmware_version
           AND mm.deployed_checksum = f.firmware_checksum)::int AS on_target_count
FROM groups g
JOIN release_channel c ON c.id = sqlc.arg('channel_id') AND c.org_id = sqlc.arg('org_id')
LEFT JOIN release_channel_firmware f
       ON f.channel_id = c.id
      AND release_channel_pair_key(f.manufacturer) = release_channel_pair_key(g.manufacturer)
      AND release_channel_pair_key(f.model) = release_channel_pair_key(g.model)
LEFT JOIN firmware_rollout r
       ON r.channel_id = c.id AND r.status = 'active'
      AND release_channel_pair_key(r.manufacturer) = release_channel_pair_key(g.manufacturer)
      AND release_channel_pair_key(r.model) = release_channel_pair_key(g.model)
WHERE (
    sqlc.narg('after_manufacturer')::text IS NULL
    OR (g.manufacturer, g.model) > (sqlc.narg('after_manufacturer')::text, sqlc.narg('after_model')::text)
)
ORDER BY g.manufacturer, g.model
LIMIT sqlc.arg('page_limit');

-- name: ListReleaseChannelMembershipConflictsPage :many
-- One page of (miner, channel) relations for miners matched by several
-- channels, optionally for one channel, ordered by identifier then channel.
SELECT k.device_id,
       d.device_identifier,
       COALESCE(dd.manufacturer, '')::text AS manufacturer,
       COALESCE(dd.model, '')::text AS model,
       k.channel_id,
       c.name AS channel_name,
       k.specificity::int AS specificity,
       k.resolution
FROM release_channel_conflict k
JOIN release_channel c ON c.id = k.channel_id
JOIN device d ON d.id = k.device_id
JOIN discovered_device dd ON dd.id = d.discovered_device_id
WHERE k.org_id = sqlc.arg('org_id')
  AND (sqlc.narg('channel_id')::bigint IS NULL OR k.channel_id = sqlc.narg('channel_id'))
  AND (
    sqlc.narg('after_identifier')::text IS NULL
    OR (d.device_identifier, d.id, k.channel_id) > (sqlc.narg('after_identifier')::text, sqlc.narg('after_device_id')::bigint, sqlc.narg('after_channel_id')::bigint)
  )
ORDER BY d.device_identifier, d.id, k.channel_id
LIMIT sqlc.arg('page_limit');

-- name: ResolveReleaseChannelScope :many
-- Miners a candidate scope covers, each with the most specific other channel
-- (if any) whose selectors already match it. Used to preview a scope and to
-- reject overlapping saves; exclude_channel_id is the channel being edited.
WITH scoped AS (
    SELECT p.device_id
    FROM release_channel_placement p
    WHERE p.org_id = sqlc.arg('org_id')
      AND (
           p.device_identifier = ANY(sqlc.arg('device_identifiers')::text[])
        OR p.rack_id = ANY(sqlc.arg('rack_ids')::bigint[])
        OR p.building_id = ANY(sqlc.arg('building_ids')::bigint[])
        OR p.site_id = ANY(sqlc.arg('site_ids')::bigint[])
      )
    UNION
    SELECT gm.device_id
    FROM device_set gs
    JOIN device_set_membership gm ON gm.device_set_id = gs.id AND gm.device_set_type = 'group'
    JOIN device d ON d.id = gm.device_id AND d.deleted_at IS NULL
    WHERE gs.org_id = sqlc.arg('org_id')
      AND gs.type = 'group'
      AND gs.deleted_at IS NULL
      AND gs.id = ANY(sqlc.arg('group_ids')::bigint[])
)
SELECT s.device_id,
       d.device_identifier,
       COALESCE(dd.manufacturer, '')::text AS manufacturer,
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
    WHERE rm.org_id = sqlc.arg('org_id')
      AND rm.device_id = s.device_id
      AND rm.channel_id <> sqlc.arg('exclude_channel_id')
    ORDER BY rm.specificity, c.id
    LIMIT 1
) owner ON true
ORDER BY d.device_identifier;

-- --- Firmware assignments ---

-- name: UpsertReleaseChannelFirmware :one
-- Assigns an artifact to a pair and advances the pair's generation. The
-- stored key keeps the case it was first written with.
INSERT INTO release_channel_firmware (
    channel_id, manufacturer, model, firmware_checksum, firmware_version,
    firmware_target_manufacturer, firmware_target_model, assignment_generation, assigned_by
)
VALUES (
    sqlc.arg('channel_id'), sqlc.arg('manufacturer'), sqlc.arg('model'), sqlc.arg('firmware_checksum'), sqlc.arg('firmware_version'),
    sqlc.arg('firmware_target_manufacturer'), sqlc.arg('firmware_target_model'), 1, sqlc.arg('assigned_by')
)
ON CONFLICT (channel_id, release_channel_pair_key(manufacturer), release_channel_pair_key(model)) DO UPDATE
SET firmware_checksum = EXCLUDED.firmware_checksum,
    firmware_version = EXCLUDED.firmware_version,
    firmware_target_manufacturer = EXCLUDED.firmware_target_manufacturer,
    firmware_target_model = EXCLUDED.firmware_target_model,
    assignment_generation = release_channel_firmware.assignment_generation + 1,
    assigned_by = EXCLUDED.assigned_by,
    updated_at = now()
RETURNING *;

-- name: ClearReleaseChannelFirmware :one
-- Clears an assigned pair, advancing its generation; the row stays so the
-- generation survives. Returns nothing when the pair was not assigned.
UPDATE release_channel_firmware
SET firmware_checksum = '',
    firmware_version = '',
    firmware_target_manufacturer = '',
    firmware_target_model = '',
    assignment_generation = assignment_generation + 1,
    assigned_by = sqlc.arg('assigned_by'),
    updated_at = now()
WHERE channel_id = sqlc.arg('channel_id')
  AND release_channel_pair_key(manufacturer) = release_channel_pair_key(sqlc.arg('manufacturer')::text)
  AND release_channel_pair_key(model) = release_channel_pair_key(sqlc.arg('model')::text)
  AND firmware_checksum <> ''
RETURNING *;

-- name: GetReleaseChannelFirmware :one
SELECT * FROM release_channel_firmware
WHERE channel_id = sqlc.arg('channel_id')
  AND release_channel_pair_key(manufacturer) = release_channel_pair_key(sqlc.arg('manufacturer')::text)
  AND release_channel_pair_key(model) = release_channel_pair_key(sqlc.arg('model')::text);

-- name: ListReleaseChannelFirmware :many
-- Every pair row of an org's channels, assigned or cleared.
SELECT f.*
FROM release_channel_firmware f
JOIN release_channel c ON c.id = f.channel_id
WHERE c.org_id = sqlc.arg('org_id')
ORDER BY f.channel_id, f.manufacturer, f.model;

-- name: ListReleaseChannelMismatchedMembers :many
-- Members of one pair the enforcement loop should update: the reported
-- version or provenance differs from the assignment, or a FirmwareUpdate for a
-- file outside assigned_file_ids (the files carrying the assigned checksum) is
-- still pending or processing. Excludes miners already in rollout_id (0 for a
-- new rollout) and suppressed miners (firmware_rollout_suppressed_device).
-- Carries the latest efficiency sample for ordering.
SELECT d.id AS device_id,
       d.device_identifier,
       hm.efficiency_jh
FROM release_channel_member m
JOIN device d ON d.id = m.device_id
JOIN discovered_device dd ON dd.id = d.discovered_device_id
LEFT JOIN device_firmware_deployment dep ON dep.device_id = d.id
LEFT JOIN LATERAL (
    SELECT dm.efficiency_jh
    FROM device_metrics dm
    WHERE dm.device_identifier = d.device_identifier
      AND dm.time >= now() - INTERVAL '15 minutes'
    ORDER BY dm.time DESC
    LIMIT 1
) hm ON true
WHERE m.org_id = sqlc.arg('org_id')
  AND m.channel_id = sqlc.arg('channel_id')
  AND release_channel_pair_key(dd.manufacturer) = release_channel_pair_key(sqlc.arg('manufacturer')::text)
  AND release_channel_pair_key(dd.model) = release_channel_pair_key(sqlc.arg('model')::text)
  AND NOT (
      COALESCE(dd.firmware_version, '') = sqlc.arg('firmware_version')::text
      AND COALESCE(dep.firmware_checksum, '') = sqlc.arg('firmware_checksum')::text
      AND NOT EXISTS (
          SELECT 1 FROM queue_message qm
          WHERE qm.device_id = d.id
            AND qm.command_type = 'FirmwareUpdate'
            AND qm.status IN ('PENDING', 'PROCESSING')
            AND NOT (COALESCE(qm.payload->>'firmware_file_id', '') = ANY(COALESCE(sqlc.arg('assigned_file_ids')::text[], '{}')))
      )
  )
  AND NOT EXISTS (
      SELECT 1 FROM firmware_rollout_device rd
      WHERE rd.rollout_id = sqlc.arg('rollout_id') AND rd.device_id = d.id
  )
  AND NOT EXISTS (
      SELECT 1 FROM firmware_rollout_suppressed_device s
      WHERE s.channel_id = m.channel_id
        AND s.device_id = d.id
        AND s.manufacturer_key = release_channel_pair_key(sqlc.arg('manufacturer')::text)
        AND s.model_key = release_channel_pair_key(sqlc.arg('model')::text)
        AND s.assignment_generation = sqlc.arg('assignment_generation')::bigint
  )
ORDER BY d.device_identifier;

-- name: ListReleaseChannelSuppressedMembers :many
-- Members of one pair the enforcement loop currently suppresses;
-- RetryFailedRolloutDevices re-queues exactly this set.
SELECT d.id AS device_id,
       d.device_identifier
FROM release_channel_member m
JOIN device d ON d.id = m.device_id
JOIN discovered_device dd ON dd.id = d.discovered_device_id
JOIN firmware_rollout_suppressed_device s
  ON s.channel_id = m.channel_id
 AND s.device_id = m.device_id
 AND s.manufacturer_key = release_channel_pair_key(sqlc.arg('manufacturer')::text)
 AND s.model_key = release_channel_pair_key(sqlc.arg('model')::text)
 AND s.assignment_generation = sqlc.arg('assignment_generation')::bigint
WHERE m.org_id = sqlc.arg('org_id')
  AND m.channel_id = sqlc.arg('channel_id')
  AND release_channel_pair_key(dd.manufacturer) = release_channel_pair_key(sqlc.arg('manufacturer')::text)
  AND release_channel_pair_key(dd.model) = release_channel_pair_key(sqlc.arg('model')::text)
ORDER BY d.device_identifier;

-- name: ListReleaseChannelFirmwareNeedingRollout :many
-- Assigned pairs with no active rollout and at least one mismatched,
-- unsuppressed member: late joiners, re-entries and miners that drifted. Any
-- outstanding FirmwareUpdate counts as a mismatch here because the assigned
-- file set is only known per pair; ListReleaseChannelMismatchedMembers makes
-- the final call.
SELECT f.channel_id, f.manufacturer, f.model, f.firmware_checksum, f.firmware_version,
       f.firmware_target_manufacturer, f.firmware_target_model, f.assignment_generation,
       f.assigned_by, f.updated_at, c.org_id
FROM release_channel_firmware f
JOIN release_channel c ON c.id = f.channel_id
WHERE f.firmware_checksum <> ''
AND NOT EXISTS (
    SELECT 1 FROM firmware_rollout r
    WHERE r.channel_id = f.channel_id
      AND release_channel_pair_key(r.manufacturer) = release_channel_pair_key(f.manufacturer)
      AND release_channel_pair_key(r.model) = release_channel_pair_key(f.model)
      AND r.status = 'active'
)
AND EXISTS (
    SELECT 1
    FROM release_channel_member m
    JOIN device d ON d.id = m.device_id
    JOIN discovered_device dd ON dd.id = d.discovered_device_id
    LEFT JOIN device_firmware_deployment dep ON dep.device_id = d.id
    WHERE m.org_id = c.org_id
      AND m.channel_id = f.channel_id
      AND release_channel_pair_key(dd.manufacturer) = release_channel_pair_key(f.manufacturer)
      AND release_channel_pair_key(dd.model) = release_channel_pair_key(f.model)
      AND NOT (
          COALESCE(dd.firmware_version, '') = f.firmware_version
          AND COALESCE(dep.firmware_checksum, '') = f.firmware_checksum
          AND NOT EXISTS (
              SELECT 1 FROM queue_message qm
              WHERE qm.device_id = d.id
                AND qm.command_type = 'FirmwareUpdate'
                AND qm.status IN ('PENDING', 'PROCESSING')
          )
      )
      AND NOT EXISTS (
          SELECT 1 FROM firmware_rollout_suppressed_device s
          WHERE s.channel_id = f.channel_id
            AND s.device_id = d.id
            AND s.manufacturer_key = release_channel_pair_key(f.manufacturer)
            AND s.model_key = release_channel_pair_key(f.model)
            AND s.assignment_generation = f.assignment_generation
      )
)
ORDER BY f.channel_id, f.manufacturer, f.model;

-- --- Rollouts ---

-- name: CreateFirmwareRollout :one
INSERT INTO firmware_rollout (
    org_id, channel_id, manufacturer, model, firmware_checksum, firmware_version,
    previous_firmware_checksum, previous_firmware_version, assignment_generation,
    stage,
    method, order_by, batch_size, pilot_size, wait_between_batches_seconds,
    review_after_each_batch, auto_continue, stabilization_seconds,
    max_hashrate_drop_percent, max_efficiency_increase_percent, max_temp_increase_c, max_new_errors,
    min_sample_coverage_percent, max_concurrent_offline, controller_timeout_seconds, batch_count,
    started_by_type, started_by_id, started_by_name,
    last_action_by_type, last_action_by_id, last_action_by_name
)
VALUES (
    sqlc.arg('org_id'), sqlc.arg('channel_id'), sqlc.arg('manufacturer'), sqlc.arg('model'), sqlc.arg('firmware_checksum'), sqlc.arg('firmware_version'),
    sqlc.arg('previous_firmware_checksum'), sqlc.arg('previous_firmware_version'), sqlc.arg('assignment_generation'),
    sqlc.arg('stage'),
    sqlc.arg('method'), sqlc.arg('order_by'), sqlc.arg('batch_size'), sqlc.arg('pilot_size'), sqlc.arg('wait_between_batches_seconds'),
    sqlc.arg('review_after_each_batch'), sqlc.arg('auto_continue'), sqlc.arg('stabilization_seconds'),
    sqlc.narg('max_hashrate_drop_percent')::double precision, sqlc.narg('max_efficiency_increase_percent')::double precision,
    sqlc.narg('max_temp_increase_c')::double precision, sqlc.narg('max_new_errors')::int,
    sqlc.narg('min_sample_coverage_percent')::double precision, sqlc.arg('max_concurrent_offline'), sqlc.arg('controller_timeout_seconds'), sqlc.arg('batch_count'),
    sqlc.arg('actor_type'), sqlc.arg('actor_id'), sqlc.arg('actor_name'),
    sqlc.arg('actor_type'), sqlc.arg('actor_id'), sqlc.arg('actor_name')
)
RETURNING *;

-- name: GetFirmwareRollout :one
SELECT * FROM firmware_rollout
WHERE id = sqlc.arg('rollout_id') AND org_id = sqlc.arg('org_id');

-- name: GetFirmwareRolloutForUpdate :one
-- Locks the row for a mutation so the revision rule can be checked and the
-- change applied without a concurrent actor slipping in between.
SELECT * FROM firmware_rollout
WHERE id = sqlc.arg('rollout_id') AND org_id = sqlc.arg('org_id')
FOR UPDATE;

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
  AND (sqlc.narg('updated_after')::timestamptz IS NULL OR r.updated_at >= sqlc.narg('updated_after'))
  AND (
    sqlc.narg('before_created_at')::timestamptz IS NULL
    OR (r.created_at, r.id) < (sqlc.narg('before_created_at')::timestamptz, sqlc.narg('before_id')::bigint)
  )
ORDER BY r.created_at DESC, r.id DESC
LIMIT sqlc.arg('page_limit');

-- name: ListActiveFirmwareRollouts :many
-- Across all orgs; drives the enforcement loop. Carries the channel's live
-- offline budget, which governs every active rollout of the channel.
SELECT sqlc.embed(r), c.name AS channel_name, c.max_concurrent_offline AS channel_max_concurrent_offline
FROM firmware_rollout r
JOIN release_channel c ON c.id = r.channel_id
WHERE r.status = 'active'
ORDER BY r.id;

-- name: GetLatestFirmwareRolloutForPair :one
-- The most recent rollout of a pair within one assignment generation; a
-- reconciliation rollout inherits its lineage.
SELECT * FROM firmware_rollout
WHERE channel_id = sqlc.arg('channel_id')
  AND release_channel_pair_key(manufacturer) = release_channel_pair_key(sqlc.arg('manufacturer')::text)
  AND release_channel_pair_key(model) = release_channel_pair_key(sqlc.arg('model')::text)
  AND assignment_generation = sqlc.arg('assignment_generation')
ORDER BY created_at DESC, id DESC
LIMIT 1;

-- name: GetActiveFirmwareRolloutForPair :one
SELECT * FROM firmware_rollout
WHERE channel_id = sqlc.arg('channel_id')
  AND release_channel_pair_key(manufacturer) = release_channel_pair_key(sqlc.arg('manufacturer')::text)
  AND release_channel_pair_key(model) = release_channel_pair_key(sqlc.arg('model')::text)
  AND status = 'active';

-- name: CancelActiveFirmwareRollout :exec
-- Cancels the pair's active rollout because its assignment changed:
-- 'superseded', 'rolled_back' or 'cleared'.
UPDATE firmware_rollout
SET status = 'canceled',
    finished_at = now(),
    cancel_reason = sqlc.arg('cancel_reason'),
    last_action_by_type = sqlc.arg('actor_type'),
    last_action_by_id = sqlc.arg('actor_id'),
    last_action_by_name = sqlc.arg('actor_name')
WHERE channel_id = sqlc.arg('channel_id')
  AND release_channel_pair_key(manufacturer) = release_channel_pair_key(sqlc.arg('manufacturer')::text)
  AND release_channel_pair_key(model) = release_channel_pair_key(sqlc.arg('model')::text)
  AND status = 'active';

-- name: CancelFirmwareRollout :execrows
UPDATE firmware_rollout
SET status = 'canceled',
    finished_at = now(),
    cancel_reason = 'canceled_remaining',
    last_action_by_type = sqlc.arg('actor_type'),
    last_action_by_id = sqlc.arg('actor_id'),
    last_action_by_name = sqlc.arg('actor_name')
WHERE id = sqlc.arg('rollout_id') AND status = 'active';

-- name: FinishFirmwareRollout :execrows
-- Ends an active rollout as 'completed' or 'completed_with_failures'.
UPDATE firmware_rollout
SET status = sqlc.arg('status'), finished_at = now()
WHERE id = sqlc.arg('rollout_id') AND status = 'active';

-- name: AdvanceFirmwareRolloutStage :execrows
-- Stage transitions of an active rollout, attributed to an actor when one
-- drove them. Returns the affected row count so callers can detect a lost race.
UPDATE firmware_rollout
SET stage = sqlc.arg('stage'),
    current_batch = sqlc.arg('current_batch'),
    stage_changed_at = now(),
    last_action_by_type = COALESCE(sqlc.narg('actor_type')::text, last_action_by_type),
    last_action_by_id = COALESCE(sqlc.narg('actor_id')::bigint, last_action_by_id),
    last_action_by_name = COALESCE(sqlc.narg('actor_name')::text, last_action_by_name)
WHERE id = sqlc.arg('rollout_id')
  AND status = 'active'
  AND stage = sqlc.arg('from_stage');

-- name: PauseFirmwareRollout :execrows
UPDATE firmware_rollout
SET paused_at = now(),
    last_action_by_type = sqlc.arg('actor_type'),
    last_action_by_id = sqlc.arg('actor_id'),
    last_action_by_name = sqlc.arg('actor_name')
WHERE id = sqlc.arg('rollout_id') AND status = 'active' AND paused_at IS NULL;

-- name: ResumeFirmwareRollout :execrows
UPDATE firmware_rollout
SET paused_at = NULL,
    last_action_by_type = sqlc.arg('actor_type'),
    last_action_by_id = sqlc.arg('actor_id'),
    last_action_by_name = sqlc.arg('actor_name')
WHERE id = sqlc.arg('rollout_id') AND status = 'active' AND paused_at IS NOT NULL;

-- name: RecordFirmwareRolloutAction :exec
-- Attributes an action that changes only the rollout's devices (retry) to
-- its actor.
UPDATE firmware_rollout
SET last_action_by_type = sqlc.arg('actor_type'),
    last_action_by_id = sqlc.arg('actor_id'),
    last_action_by_name = sqlc.arg('actor_name')
WHERE id = sqlc.arg('rollout_id');

-- --- Rollout devices ---

-- name: ListFirmwareRolloutDevices :many
-- Every miner in a rollout with its bookkeeping, baseline, live health (device
-- status, latest telemetry within 15 minutes, open errors), provenance, the
-- files named by its pending or processing FirmwareUpdate commands, and
-- whether it is still a member of the channel for the rollout's pair. Live
-- health is evidence for the engine's next decision; the persisted columns
-- (verified_at, halted_at, excluded_at) carry the miner's phase.
SELECT rd.device_id,
       d.device_identifier,
       COALESCE(dd.firmware_version, '')::text AS firmware_version,
       COALESCE(dd.ip_address, '')::text AS ip_address,
       rd.batch_index,
       rd.position,
       rd.attempts,
       rd.first_sent_at,
       rd.last_sent_at,
       rd.verified_at,
       rd.halted_at,
       rd.halt_reason,
       rd.last_error,
       rd.skip_note,
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
       COALESCE(dep.firmware_checksum, '')::text AS last_deployed_firmware_checksum,
       COALESCE((
           SELECT array_agg(COALESCE(qm.payload->>'firmware_file_id', ''))
           FROM queue_message qm
           WHERE qm.device_id = d.id
             AND qm.command_type = 'FirmwareUpdate'
             AND qm.status IN ('PENDING', 'PROCESSING')
       ), '{}'::text[])::text[] AS pending_firmware_file_ids,
       EXISTS (
           SELECT 1 FROM release_channel_member m
           WHERE m.org_id = r.org_id AND m.device_id = d.id AND m.channel_id = r.channel_id
       )
       AND release_channel_pair_key(dd.manufacturer) = release_channel_pair_key(r.manufacturer)
       AND release_channel_pair_key(dd.model) = release_channel_pair_key(r.model) AS in_scope
FROM firmware_rollout_device rd
JOIN firmware_rollout r ON r.id = rd.rollout_id
JOIN device d ON d.id = rd.device_id
JOIN discovered_device dd ON dd.id = d.discovered_device_id
LEFT JOIN device_status ds ON ds.device_id = d.id
LEFT JOIN device_firmware_deployment dep ON dep.device_id = d.id
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
-- Adds a rollout's initial targets with their batch (NULL for the unbatched
-- rest), their order (position_offset + index in device_ids) and a baseline
-- of their health, so post-update evidence is compared with each miner's own
-- past. Miners already in the rollout are left as they are.
INSERT INTO firmware_rollout_device (
    rollout_id, device_id, batch_index, position,
    baseline_status, baseline_hash_rate_hs, baseline_power_w, baseline_efficiency_jh, baseline_temp_c,
    baseline_open_errors, baseline_at
)
SELECT sqlc.arg('rollout_id'),
       d.id,
       sqlc.narg('batch_index')::int,
       sqlc.arg('position_offset')::int + array_position(sqlc.arg('device_ids')::bigint[], d.id),
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
ON CONFLICT (rollout_id, device_id) DO NOTHING;

-- name: AppendFirmwareRolloutDevices :exec
-- Adds late joiners: unbatched, unordered (they sort last) and without a
-- baseline, so they are judged on version and being online only. Miners
-- already in the rollout are left as they are.
INSERT INTO firmware_rollout_device (rollout_id, device_id)
SELECT sqlc.arg('rollout_id'), d.id
FROM device d
WHERE d.id = ANY(sqlc.arg('device_ids')::bigint[])
ON CONFLICT (rollout_id, device_id) DO NOTHING;

-- name: ReincludeFirmwareRolloutDevices :exec
-- Re-includes miners that left the channel scope and came back. They keep
-- their batch, order and baseline but must verify again: their firmware may
-- have changed while they were out of scope.
UPDATE firmware_rollout_device
SET excluded_at = NULL,
    verified_at = NULL
WHERE rollout_id = sqlc.arg('rollout_id')
  AND device_id = ANY(sqlc.arg('device_ids')::bigint[])
  AND excluded_at IS NOT NULL;

-- name: MarkFirmwareRolloutDevicesSent :exec
UPDATE firmware_rollout_device
SET attempts = attempts + 1,
    first_sent_at = COALESCE(first_sent_at, now()),
    last_sent_at = now()
WHERE rollout_id = sqlc.arg('rollout_id')
  AND device_id = ANY(sqlc.arg('device_ids')::bigint[]);

-- name: MarkFirmwareRolloutDevicesVerified :exec
-- Latches convergence for miners that meet every criterion this tick, so the
-- phase change is a rollout change under the revision rule.
UPDATE firmware_rollout_device
SET verified_at = now()
WHERE rollout_id = sqlc.arg('rollout_id')
  AND device_id = ANY(sqlc.arg('device_ids')::bigint[])
  AND verified_at IS NULL;

-- name: UnverifyFirmwareRolloutDevices :exec
-- Reopens convergence for verified miners the enforcement loop sees drifting
-- from the assignment (reported version or provenance no longer match) while
-- the rollout runs, so they are updated again.
UPDATE firmware_rollout_device
SET verified_at = NULL
WHERE rollout_id = sqlc.arg('rollout_id')
  AND device_id = ANY(sqlc.arg('device_ids')::bigint[])
  AND verified_at IS NOT NULL;

-- name: HaltFirmwareRolloutDevices :exec
-- Stops retrying miners for this version: 'failed' (attempts exhausted),
-- 'canceled' (operator canceled the remaining updates) or 'skipped' (a caller
-- settled them without updating, with its note).
UPDATE firmware_rollout_device
SET halted_at = now(),
    halt_reason = sqlc.arg('halt_reason'),
    last_error = sqlc.arg('last_error'),
    skip_note = sqlc.arg('skip_note')
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
    skip_note = '',
    attempts = 0,
    first_sent_at = NULL,
    last_sent_at = NULL,
    verified_at = NULL
WHERE rollout_id = sqlc.arg('rollout_id')
  AND halted_at IS NOT NULL
RETURNING device_id;

-- name: ExcludeFirmwareRolloutDevices :exec
UPDATE firmware_rollout_device
SET excluded_at = now()
WHERE rollout_id = sqlc.arg('rollout_id')
  AND device_id = ANY(sqlc.arg('device_ids')::bigint[])
  AND excluded_at IS NULL;

-- name: RecordFirmwareDeployment :exec
-- Managed-deployment provenance: the miners listed reported the artifact a
-- rollout dispatched to them. The triggers on device_firmware_deployment
-- advance the rollout's revision.
INSERT INTO device_firmware_deployment (device_id, firmware_checksum, firmware_version, rollout_id, deployed_at)
SELECT ids.device_id, sqlc.arg('firmware_checksum'), sqlc.arg('firmware_version'), sqlc.arg('rollout_id'), now()
FROM (SELECT DISTINCT unnest(sqlc.arg('device_ids')::bigint[]) AS device_id) ids
ON CONFLICT (device_id) DO UPDATE
SET firmware_checksum = EXCLUDED.firmware_checksum,
    firmware_version = EXCLUDED.firmware_version,
    rollout_id = EXCLUDED.rollout_id,
    deployed_at = now();
