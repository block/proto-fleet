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

-- name: GetReleaseChannelForUpdate :one
-- Serialize assignment changes before reading any pair, including pairs with
-- no assignment row yet. Lock the channel before its rollouts. NO KEY UPDATE
-- permits foreign-key checks by concurrent rollout/target inserts.
SELECT * FROM release_channel
WHERE id = sqlc.arg('channel_id') AND org_id = sqlc.arg('org_id')
FOR NO KEY UPDATE;

-- name: ListReleaseChannels :many
SELECT * FROM release_channel
WHERE org_id = sqlc.arg('org_id')
ORDER BY name;

-- name: GetReleaseChannelDeletionState :one
-- Channel deletion must not erase active rollout controls or command history
-- while an already-dispatched update remains pending. A recovered target may
-- have released its reservation while the command still runs, so consult the
-- durable dispatch identity as well as reservations.
SELECT EXISTS (
    SELECT 1 FROM firmware_rollout r
    WHERE r.channel_id = sqlc.arg('channel_id') AND r.status = 'active'
)::boolean AS active_rollouts,
EXISTS (
    SELECT 1 FROM queue_message qm
    WHERE qm.command_type = 'FirmwareUpdate'
      AND qm.status IN ('PENDING', 'PROCESSING')
      AND (
          EXISTS (
              SELECT 1 FROM firmware_rollout_device rd
              JOIN firmware_rollout r ON r.id = rd.rollout_id
              WHERE r.channel_id = sqlc.arg('channel_id')
                AND rd.device_id = qm.device_id
                AND rd.last_dispatched_batch_uuid = qm.command_batch_log_uuid
          )
          OR EXISTS (
              SELECT 1 FROM firmware_rollout_reservation reservation
              WHERE reservation.channel_id = sqlc.arg('channel_id')
                AND reservation.device_id = qm.device_id
                AND reservation.batch_uuid = qm.command_batch_log_uuid
          )
      )
)::boolean AS pending_commands;

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
-- Resolves an org's device identifiers to the ids of current devices;
-- unknown identifiers are dropped.
SELECT p.device_id AS id, p.device_identifier
FROM release_channel_placement p
WHERE p.org_id = sqlc.arg('org_id')
  AND p.device_identifier = ANY(sqlc.arg('device_identifiers')::text[]);

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
-- Miners a candidate scope covers, one row per distinct other channel whose
-- selectors already match each miner, or one row with no owner for a miner
-- without conflicts. Callers count distinct miners for scope/model totals and
-- aggregate every conflicting channel. Used to preview a scope and reject
-- overlapping saves; exclude_channel_id is the channel being edited.
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
    SELECT p.device_id
    FROM device_set gs
    JOIN device_set_membership gm ON gm.device_set_id = gs.id AND gm.device_set_type = 'group'
    JOIN release_channel_placement p ON p.device_id = gm.device_id
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
    SELECT DISTINCT c.id AS channel_id, c.name
    FROM release_channel_match rm
    JOIN release_channel c ON c.id = rm.channel_id
    WHERE rm.org_id = sqlc.arg('org_id')
      AND rm.device_id = s.device_id
      AND rm.channel_id <> sqlc.arg('exclude_channel_id')
) owner ON true
ORDER BY d.device_identifier, d.id, owner.channel_id;

-- --- Firmware assignments ---

-- name: UpsertReleaseChannelFirmware :one
-- Assigns an artifact to a pair and advances the pair's generation. The
-- stored key keeps the case it was first written with. assigned_by is the
-- owning user for enforcement commands, separate from the rollout's audit
-- actor. Reconciliation and retries retain this assignment's owner.
INSERT INTO release_channel_firmware (
    channel_id, manufacturer, model, firmware_checksum, firmware_version,
    firmware_target_manufacturer, firmware_target_model, assignment_generation, assigned_by
)
VALUES (
    sqlc.arg('channel_id'), sqlc.arg('manufacturer'), sqlc.arg('model'), sqlc.arg('firmware_checksum'), sqlc.arg('firmware_version'),
    sqlc.arg('firmware_target_manufacturer'), sqlc.arg('firmware_target_model'), 1, sqlc.arg('assigned_by')
)
ON CONFLICT (channel_id, release_channel_pair_key(manufacturer), release_channel_pair_key(model)) DO UPDATE
SET previous_firmware_checksum = release_channel_firmware.firmware_checksum,
    previous_firmware_version = release_channel_firmware.firmware_version,
    firmware_checksum = EXCLUDED.firmware_checksum,
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
    previous_firmware_checksum = '',
    previous_firmware_version = '',
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
-- version or provenance differs from the assignment, or a FirmwareUpdate for
-- another checksum is still pending or processing. Commands without a checksum
-- fall back to assigned_file_ids (the files carrying the assigned checksum).
-- Excludes miners already in rollout_id (0 for a
-- new rollout) and suppressed miners (firmware_rollout_suppressed_device).
-- Carries the latest efficiency sample within 15 minutes of this statement
-- for ordering; time spent earlier in the transaction does not extend freshness.
-- Samples before the paired device's creation cannot determine its order.
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
      AND d.deleted_at IS NULL
      AND dm.time >= d.created_at
      AND dm.time >= statement_timestamp() - INTERVAL '15 minutes'
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
            AND CASE WHEN COALESCE(qm.payload->>'firmware_checksum', '') <> ''
                THEN qm.payload->>'firmware_checksum' <> sqlc.arg('firmware_checksum')::text
                ELSE NOT (COALESCE(qm.payload->>'firmware_file_id', '') = ANY(COALESCE(sqlc.arg('assigned_file_ids')::text[], '{}')))
            END
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

-- name: CountReleaseChannelFirmwarePreviewMembers :one
-- Both preview counts use one snapshot of the mismatch rule above. A pending
-- foreign command makes a member mismatched even when it reports the assigned
-- version and provenance. Suppression excludes dispatch targets, but does not
-- change whether a member matches the assignment.
WITH members AS (
    SELECT COALESCE(dd.firmware_version, '') = sqlc.arg('firmware_version')::text
           AND COALESCE(dep.firmware_checksum, '') = sqlc.arg('firmware_checksum')::text
           AND NOT EXISTS (
               SELECT 1 FROM queue_message qm
               WHERE qm.device_id = d.id
                 AND qm.command_type = 'FirmwareUpdate'
                 AND qm.status IN ('PENDING', 'PROCESSING')
                 AND CASE WHEN COALESCE(qm.payload->>'firmware_checksum', '') <> ''
                     THEN qm.payload->>'firmware_checksum' <> sqlc.arg('firmware_checksum')::text
                     ELSE NOT (COALESCE(qm.payload->>'firmware_file_id', '') = ANY(COALESCE(sqlc.arg('assigned_file_ids')::text[], '{}')))
                 END
           ) AS on_target,
           EXISTS (
               SELECT 1 FROM firmware_rollout_suppressed_device s
               WHERE s.channel_id = m.channel_id
                 AND s.device_id = d.id
                 AND s.manufacturer_key = release_channel_pair_key(sqlc.arg('manufacturer')::text)
                 AND s.model_key = release_channel_pair_key(sqlc.arg('model')::text)
                 AND s.assignment_generation = sqlc.arg('assignment_generation')::bigint
           ) AS suppressed
    FROM release_channel_member m
    JOIN device d ON d.id = m.device_id
    JOIN discovered_device dd ON dd.id = d.discovered_device_id
    LEFT JOIN device_firmware_deployment dep ON dep.device_id = d.id
    WHERE m.org_id = sqlc.arg('org_id')
      AND m.channel_id = sqlc.arg('channel_id')
      AND release_channel_pair_key(dd.manufacturer) = release_channel_pair_key(sqlc.arg('manufacturer')::text)
      AND release_channel_pair_key(dd.model) = release_channel_pair_key(sqlc.arg('model')::text)
)
SELECT count(*) FILTER (WHERE NOT on_target AND NOT suppressed)::int AS target_count,
       count(*) FILTER (WHERE on_target)::int AS on_target_count
FROM members;

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
-- Creation follows assignment/scope locks; start the initial stage at this
-- write rather than at the beginning of a transaction that may have waited.
INSERT INTO firmware_rollout (
    org_id, channel_id, manufacturer, model, firmware_checksum, firmware_version,
    previous_firmware_checksum, previous_firmware_version, assignment_generation,
    stage, stage_changed_at, behavior_snapshot, batch_count,
    started_by_type, started_by_id, started_by_name,
    last_action_by_type, last_action_by_id, last_action_by_name
)
VALUES (
    sqlc.arg('org_id'), sqlc.arg('channel_id'), sqlc.arg('manufacturer'), sqlc.arg('model'), sqlc.arg('firmware_checksum'), sqlc.arg('firmware_version'),
    sqlc.arg('previous_firmware_checksum'), sqlc.arg('previous_firmware_version'), sqlc.arg('assignment_generation'),
    sqlc.arg('stage'), clock_timestamp(), sqlc.arg('behavior_snapshot'), sqlc.arg('batch_count'),
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

-- name: GetFirmwareRolloutPollWatermark :one
-- Capture before the first page's read. Every transaction still invisible to
-- that page has an ID at or above this bound, even if it commits out of order.
SELECT pg_snapshot_xmin(pg_current_snapshot())::text::bigint AS poll_xmin;

-- name: ListFirmwareRollouts :many
-- Newest first. The cursor is the (created_at, id) of the last row of the
-- previous page; rows strictly older than it are returned. Incremental polls
-- include the previous cycle's xmin and all later transaction IDs, allowing
-- replay while retaining late commits. updated_after is only a date filter.
SELECT sqlc.embed(r), c.name AS channel_name
FROM firmware_rollout r
JOIN release_channel c ON c.id = r.channel_id
WHERE r.org_id = sqlc.arg('org_id')
  AND (sqlc.narg('channel_id')::bigint IS NULL OR r.channel_id = sqlc.narg('channel_id'))
  AND (sqlc.narg('status')::text IS NULL OR r.status = sqlc.narg('status'))
  AND (sqlc.narg('updated_after')::timestamptz IS NULL OR r.updated_at >= sqlc.narg('updated_after'))
  AND (sqlc.narg('after_revision_txid')::bigint IS NULL OR r.revision_txid >= sqlc.narg('after_revision_txid'))
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
-- reconciliation rollout inherits its lineage. Sequence allocation orders
-- history independently of wall-clock corrections.
SELECT * FROM firmware_rollout
WHERE channel_id = sqlc.arg('channel_id')
  AND release_channel_pair_key(manufacturer) = release_channel_pair_key(sqlc.arg('manufacturer')::text)
  AND release_channel_pair_key(model) = release_channel_pair_key(sqlc.arg('model')::text)
  AND assignment_generation = sqlc.arg('assignment_generation')
ORDER BY id DESC
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
-- Finish after acquiring the header lock, never before creation or the last
-- stage transition or pause even if the wall clock has moved back. Terminal
-- rollouts no longer carry an active pause.
WITH locked_rollout AS MATERIALIZED (
    SELECT candidate.id, candidate.created_at, candidate.stage_changed_at, candidate.paused_at
    FROM firmware_rollout AS candidate
    WHERE candidate.channel_id = sqlc.arg('channel_id')
      AND release_channel_pair_key(candidate.manufacturer) = release_channel_pair_key(sqlc.arg('manufacturer')::text)
      AND release_channel_pair_key(candidate.model) = release_channel_pair_key(sqlc.arg('model')::text)
      AND candidate.status = 'active'
    FOR UPDATE
)
UPDATE firmware_rollout AS r
SET status = 'canceled',
    finished_at = GREATEST(locked_rollout.created_at, locked_rollout.stage_changed_at, locked_rollout.paused_at, clock_timestamp()),
    paused_at = NULL,
    cancel_reason = sqlc.arg('cancel_reason'),
    last_action_by_type = sqlc.arg('actor_type'),
    last_action_by_id = sqlc.arg('actor_id'),
    last_action_by_name = sqlc.arg('actor_name')
FROM locked_rollout
WHERE r.id = locked_rollout.id AND r.status = 'active';

-- name: CancelFirmwareRollout :execrows
-- Record the first terminal time after the header lock, bounded by the
-- rollout's preceding lifecycle events, and clear any active pause.
WITH locked_rollout AS MATERIALIZED (
    SELECT candidate.id, candidate.created_at, candidate.stage_changed_at, candidate.paused_at
    FROM firmware_rollout AS candidate
    WHERE candidate.id = sqlc.arg('rollout_id') AND candidate.status = 'active'
    FOR UPDATE
)
UPDATE firmware_rollout AS r
SET status = 'canceled',
    finished_at = GREATEST(locked_rollout.created_at, locked_rollout.stage_changed_at, locked_rollout.paused_at, clock_timestamp()),
    paused_at = NULL,
    cancel_reason = 'canceled_remaining',
    last_action_by_type = sqlc.arg('actor_type'),
    last_action_by_id = sqlc.arg('actor_id'),
    last_action_by_name = sqlc.arg('actor_name')
FROM locked_rollout
WHERE r.id = locked_rollout.id AND r.status = 'active';

-- name: FinishFirmwareRollout :execrows
-- Ends an active, unpaused rollout as 'completed' or 'completed_with_failures'.
-- Recheck the pause under the row lock so stale settled targets cannot complete
-- work after the operator pauses it. Resuming permits a later tick to finish.
-- Record the first terminal time after the header lock, bounded by the
-- rollout's preceding lifecycle events.
WITH locked_rollout AS MATERIALIZED (
    SELECT candidate.id, candidate.created_at, candidate.stage_changed_at, candidate.paused_at
    FROM firmware_rollout AS candidate
    WHERE candidate.id = sqlc.arg('rollout_id') AND candidate.status = 'active'
      AND candidate.paused_at IS NULL
    FOR UPDATE
)
UPDATE firmware_rollout AS r
SET status = sqlc.arg('status'),
    finished_at = GREATEST(locked_rollout.created_at, locked_rollout.stage_changed_at, locked_rollout.paused_at, clock_timestamp()),
    paused_at = NULL
FROM locked_rollout
WHERE r.id = locked_rollout.id AND r.status = 'active' AND r.paused_at IS NULL;

-- name: AdvanceFirmwareRolloutStage :one
-- Stage transitions of an active rollout, attributed to an actor when one
-- drove them. Reject paused rows and stale timer observations so an enforcement
-- tick loaded before a pause/resume cannot advance on its old elapsed time.
-- Return the persisted stage clock for subsequent decisions in the same tick.
-- Acquire the row before sampling the stage clock: an UPDATE expression can
-- otherwise be evaluated before a row-lock wait. Never move the stage time back.
WITH locked_rollout AS MATERIALIZED (
    SELECT candidate.id, candidate.stage_changed_at
    FROM firmware_rollout AS candidate
    WHERE candidate.id = sqlc.arg('rollout_id')
      AND candidate.status = 'active'
      AND candidate.stage = sqlc.arg('from_stage')
      AND candidate.paused_at IS NULL
      AND candidate.stage_changed_at = sqlc.arg('expected_stage_changed_at')
      AND candidate.stage_paused_microseconds = sqlc.arg('expected_stage_paused_microseconds')
    FOR UPDATE
)
UPDATE firmware_rollout AS r
SET stage = sqlc.arg('stage'),
    current_batch = sqlc.arg('current_batch'),
    stage_changed_at = GREATEST(locked_rollout.stage_changed_at, clock_timestamp()),
    stage_paused_microseconds = 0,
    last_action_by_type = COALESCE(sqlc.narg('actor_type')::text, r.last_action_by_type),
    last_action_by_id = COALESCE(sqlc.narg('actor_id')::bigint, r.last_action_by_id),
    last_action_by_name = COALESCE(sqlc.narg('actor_name')::text, r.last_action_by_name)
FROM locked_rollout
WHERE r.id = locked_rollout.id
  AND r.status = 'active'
  AND r.stage = sqlc.arg('from_stage')
RETURNING r.stage_changed_at;

-- name: PauseFirmwareRollout :execrows
-- Timestamp the pause after the header lock, never before the creation or
-- stage transition the caller observed. Repeated pauses keep the first event.
WITH locked_rollout AS MATERIALIZED (
    SELECT candidate.id, candidate.created_at, candidate.stage_changed_at
    FROM firmware_rollout AS candidate
    WHERE candidate.id = sqlc.arg('rollout_id')
      AND candidate.status = 'active'
      AND candidate.paused_at IS NULL
    FOR UPDATE
)
UPDATE firmware_rollout AS r
SET paused_at = GREATEST(locked_rollout.created_at, locked_rollout.stage_changed_at, clock_timestamp()),
    last_action_by_type = sqlc.arg('actor_type'),
    last_action_by_id = sqlc.arg('actor_id'),
    last_action_by_name = sqlc.arg('actor_name')
FROM locked_rollout
WHERE r.id = locked_rollout.id AND r.status = 'active' AND r.paused_at IS NULL;

-- name: ResumeFirmwareRollout :execrows
-- Charge the pause exactly once, after acquiring the header lock. A backward
-- clock correction contributes zero rather than subtracting an earlier pause.
-- Keep lifecycle and device evidence timestamps unchanged: only stage timers
-- exclude the pause; commands already sent continue while paused.
WITH locked_rollout AS MATERIALIZED (
    SELECT candidate.id, candidate.paused_at, candidate.stage_paused_microseconds
    FROM firmware_rollout AS candidate
    WHERE candidate.id = sqlc.arg('rollout_id')
      AND candidate.status = 'active'
      AND candidate.paused_at IS NOT NULL
    FOR UPDATE
)
UPDATE firmware_rollout AS r
SET paused_at = NULL,
    stage_paused_microseconds = locked_rollout.stage_paused_microseconds
        + (EXTRACT(EPOCH FROM GREATEST(clock_timestamp() - locked_rollout.paused_at, INTERVAL '0')) * 1000000)::bigint,
    last_action_by_type = sqlc.arg('actor_type'),
    last_action_by_id = sqlc.arg('actor_id'),
    last_action_by_name = sqlc.arg('actor_name')
FROM locked_rollout
WHERE r.id = locked_rollout.id AND r.status = 'active' AND r.paused_at IS NOT NULL;

-- name: RecordFirmwareRolloutAction :exec
-- Attributes an action that changes only the rollout's devices (retry) to
-- its actor.
UPDATE firmware_rollout
SET last_action_by_type = sqlc.arg('actor_type'),
    last_action_by_id = sqlc.arg('actor_id'),
    last_action_by_name = sqlc.arg('actor_name')
WHERE id = sqlc.arg('rollout_id');

-- --- Rollout devices ---

-- name: ObserveFirmwareRolloutReservationsOffline :exec
-- Once an outstanding command's target has been seen offline, its reservation
-- becomes an offline slot. Returning online releases that slot even if command
-- completion arrives later. This observation survives exclusion, cancellation,
-- retries and completed rollout history.
UPDATE firmware_rollout_reservation reservation
SET observed_offline = true
FROM device d
LEFT JOIN device_status ds ON ds.device_id = d.id
WHERE (sqlc.narg('channel_id')::bigint IS NULL OR reservation.channel_id = sqlc.narg('channel_id'))
  AND reservation.device_id = d.id
  AND d.deleted_at IS NULL
  AND NOT reservation.observed_offline
  AND COALESCE(ds.status::text, '') IN ('', 'OFFLINE', 'UNKNOWN', 'UPDATING');

-- name: DeleteReleasedFirmwareRolloutReservations :exec
-- Retain an offline dispatched target until recovery, including after command
-- completion or departure from the channel. A live command can also release
-- after an observed offline/online cycle. Once released, later unrelated
-- outages of departed historical targets do not reserve capacity again.
DELETE FROM firmware_rollout_reservation reservation
USING device d
WHERE d.id = reservation.device_id
  AND (
      d.deleted_at IS NOT NULL
      OR (NOT EXISTS (
          SELECT 1 FROM queue_message qm
          WHERE qm.device_id = reservation.device_id
            AND qm.command_batch_log_uuid = reservation.batch_uuid
            AND qm.command_type = 'FirmwareUpdate'
            AND qm.status IN ('PENDING', 'PROCESSING')
      ) AND EXISTS (
          SELECT 1 FROM device_status ds
          WHERE ds.device_id = d.id
            AND ds.status::text NOT IN ('OFFLINE', 'UNKNOWN', 'UPDATING')
      ))
      OR (reservation.observed_offline AND EXISTS (
          SELECT 1 FROM device_status ds
          WHERE ds.device_id = d.id
            AND ds.status::text NOT IN ('OFFLINE', 'UNKNOWN', 'UPDATING')
      ))
  );

-- name: ListFirmwareRolloutOfflineSlots :many
-- Offline current members that have been targeted hold capacity. Departed
-- targets count only while a durable dispatch reservation remains unresolved:
-- a pending command before an offline cycle, or an offline miner not yet
-- observed recovered. Completed historical targets cannot reacquire capacity
-- after leaving. UNION counts each device only once; fleet deletion releases it.
SELECT rd.device_id
FROM firmware_rollout_device rd
JOIN firmware_rollout r ON r.id = rd.rollout_id
JOIN device d ON d.id = rd.device_id AND d.deleted_at IS NULL
JOIN release_channel_member member ON member.channel_id = r.channel_id AND member.device_id = d.id
LEFT JOIN device_status ds ON ds.device_id = d.id
WHERE r.channel_id = sqlc.arg('channel_id')
  AND COALESCE(ds.status::text, '') IN ('', 'OFFLINE', 'UNKNOWN', 'UPDATING')
UNION
SELECT reservation.device_id
FROM firmware_rollout_reservation reservation
JOIN device d ON d.id = reservation.device_id AND d.deleted_at IS NULL
LEFT JOIN device_status ds ON ds.device_id = d.id
WHERE reservation.channel_id = sqlc.arg('channel_id')
  AND (
      COALESCE(ds.status::text, '') IN ('', 'OFFLINE', 'UNKNOWN', 'UPDATING')
      OR (NOT reservation.observed_offline AND EXISTS (
          SELECT 1 FROM queue_message qm
          WHERE qm.device_id = reservation.device_id
            AND qm.command_batch_log_uuid = reservation.batch_uuid
            AND qm.command_type = 'FirmwareUpdate'
            AND qm.status IN ('PENDING', 'PROCESSING')
      ))
  );

-- name: ListFirmwareRolloutDevices :many
-- Every miner in a rollout with its bookkeeping, baseline, live health (device
-- status, latest telemetry within 15 minutes of this statement and at or after
-- verification once DONE (so baseline samples cannot pass post-update gates), open errors
-- and errors opened since its baseline), provenance, the checksums of pending or processing
-- FirmwareUpdate commands (or file IDs for legacy commands without a checksum),
-- and channel membership separately from eligibility for the rollout's pair.
-- Existing targets remain tied to their paired device when firmware changes its
-- reported manufacturer/model: the engine still verifies the update's outcome,
-- but in_scope must be true before dispatching another compatible update.
-- The prior deployed version distinguishes a version change from replacing an
-- artifact with another that reports the same version; the latter requires the
-- latest dispatch's successful command result before recording new provenance.
-- Command completion serializes through the provenance row and retains its
-- exact latest batch identity. That command must also have succeeded for the
-- same immutable checksum: timestamps and historical file IDs cannot prove
-- current artifact identity, and missing command history fails closed.
-- Live health is evidence for the engine's
-- next decision; the persisted columns (verified_at, halted_at, excluded_at)
-- carry the miner's phase. A miner whose discovery row was soft-deleted reads
-- with empty identity and is out of scope.
-- Identifiers can be reused after deletion. Live telemetry belongs only to a
-- non-deleted device and must be sampled at or after that device was created;
-- retained targets keep their saved baselines without reading a replacement.
SELECT rd.device_id,
       d.device_identifier,
       COALESCE(dd.firmware_version, '')::text AS firmware_version,
       COALESCE(dd.ip_address, '')::text AS ip_address,
       rd.batch_index,
       rd.position,
       rd.attempts,
       rd.first_sent_at,
       rd.last_sent_at,
       rd.last_dispatched_at,
       rd.last_dispatched_batch_uuid,
       (EXISTS (
           SELECT 1
           FROM command_batch_log batch
           JOIN command_on_device_log result ON result.command_batch_log_id = batch.id
           WHERE batch.uuid = rd.last_dispatched_batch_uuid
             AND batch.organization_id = r.org_id
             AND batch.type = 'FirmwareUpdate'
             AND batch.payload->>'firmware_checksum' = r.firmware_checksum
             AND result.org_id = r.org_id
             AND result.device_id = rd.device_id
             AND result.status = 'SUCCESS'
             AND EXISTS (
                 SELECT 1
                 FROM command_batch_log current_batch
                 JOIN command_on_device_log current_result ON current_result.command_batch_log_id = current_batch.id
                 WHERE current_batch.uuid = dep.last_command_batch_uuid
                   AND (current_batch.organization_id = r.org_id OR current_batch.organization_id IS NULL)
                   AND current_batch.type = 'FirmwareUpdate'
                   AND current_batch.payload->>'firmware_checksum' = r.firmware_checksum
                   AND current_result.device_id = rd.device_id
                   AND current_result.org_id = r.org_id
                   AND current_result.status = 'SUCCESS'
             )
       ))::boolean AS last_dispatch_succeeded,
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
       rd.baseline_at,
       COALESCE(ds.status::text, '')::text AS status,
       hm.hash_rate_hs,
       hm.power_w,
       hm.efficiency_jh,
       hm.temp_c,
       (SELECT count(*) FROM errors e
         WHERE e.device_id = d.id AND e.closed_at IS NULL AND e.severity IN (1, 2, 3, 4))::int AS open_errors,
       (SELECT count(*) FROM errors e
         WHERE e.device_id = d.id AND e.first_seen_at > rd.baseline_at AND e.severity IN (1, 2, 3, 4))::int AS errors_since_baseline,
       COALESCE(dep.firmware_checksum, '')::text AS last_deployed_firmware_checksum,
       COALESCE(dep.firmware_version, '')::text AS last_deployed_firmware_version,
       dep.deployed_at AS last_deployed_at,
       COALESCE((
           SELECT array_agg(qm.payload->>'firmware_checksum')
           FROM queue_message qm
           WHERE qm.device_id = d.id
             AND qm.command_type = 'FirmwareUpdate'
             AND qm.status IN ('PENDING', 'PROCESSING')
             AND COALESCE(qm.payload->>'firmware_checksum', '') <> ''
       ), '{}'::text[])::text[] AS pending_firmware_checksums,
       COALESCE((
           SELECT array_agg(COALESCE(qm.payload->>'firmware_file_id', ''))
           FROM queue_message qm
           WHERE qm.device_id = d.id
             AND qm.command_type = 'FirmwareUpdate'
             AND qm.status IN ('PENDING', 'PROCESSING')
             AND COALESCE(qm.payload->>'firmware_checksum', '') = ''
       ), '{}'::text[])::text[] AS pending_legacy_firmware_file_ids,
       (member.device_id IS NOT NULL)::boolean AS is_channel_member,
       member.device_id IS NOT NULL
       AND release_channel_pair_key(dd.manufacturer) = release_channel_pair_key(r.manufacturer)
       AND release_channel_pair_key(dd.model) = release_channel_pair_key(r.model) AS in_scope
FROM firmware_rollout_device rd
JOIN firmware_rollout r ON r.id = rd.rollout_id
JOIN device d ON d.id = rd.device_id
LEFT JOIN release_channel_member member ON member.org_id = r.org_id
    AND member.device_id = d.id AND member.channel_id = r.channel_id
LEFT JOIN discovered_device dd ON dd.id = d.discovered_device_id AND dd.deleted_at IS NULL
LEFT JOIN device_status ds ON ds.device_id = d.id
LEFT JOIN device_firmware_deployment dep ON dep.device_id = d.id
LEFT JOIN LATERAL (
    SELECT dm.hash_rate_hs, dm.power_w, dm.efficiency_jh, dm.temp_c
    FROM device_metrics dm
    WHERE dm.device_identifier = d.device_identifier
      AND d.deleted_at IS NULL
      AND dm.time >= d.created_at
      AND dm.time >= statement_timestamp() - INTERVAL '15 minutes'
      AND (rd.verified_at IS NULL OR dm.time >= rd.verified_at)
    ORDER BY dm.time DESC
    LIMIT 1
) hm ON true
WHERE rd.rollout_id = sqlc.arg('rollout_id')
ORDER BY rd.position NULLS LAST, d.device_identifier;

-- name: SnapshotFirmwareRolloutDevices :exec
-- Adds a rollout's initial targets with their batch (NULL for the unbatched
-- rest), their order (position_offset + index in device_ids) and a baseline
-- of their health, so post-update evidence is compared with each miner's own
-- past. baseline_at is the statement's time, the instant the baseline reads
-- see, so an error is in the baseline or opened after it, never both. Miners
-- already in the rollout are left as they are. The telemetry cutoff uses the
-- same statement clock as baseline_at, excluding samples stale at capture.
-- Only samples from this non-deleted device's lifetime qualify, so re-pairing
-- a reused identifier cannot inherit the previous device's health baseline.
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
       statement_timestamp()
FROM device d
LEFT JOIN device_status ds ON ds.device_id = d.id
LEFT JOIN LATERAL (
    SELECT dm.hash_rate_hs, dm.power_w, dm.efficiency_jh, dm.temp_c
    FROM device_metrics dm
    WHERE dm.device_identifier = d.device_identifier
      AND d.deleted_at IS NULL
      AND dm.time >= d.created_at
      AND dm.time >= statement_timestamp() - INTERVAL '15 minutes'
    ORDER BY dm.time DESC
    LIMIT 1
) hm ON true
WHERE d.id = ANY(sqlc.arg('device_ids')::bigint[])
ON CONFLICT (rollout_id, device_id) DO NOTHING;

-- name: AppendFirmwareRolloutDevices :exec
-- Adds late joiners: unbatched, unordered (they sort last) and without a
-- baseline; the engine applies the contract's late-joiner convergence criteria. Miners
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
-- Every attempted target advances retry pacing, including preflight skips.
-- Only targets actually dispatched to may authorize a later provenance write.
-- Lock targets before sampling one monotonic clock per attempt. A timestamp
-- evaluated before a target-lock wait would shorten the next retry interval.
WITH reservations AS (
    INSERT INTO firmware_rollout_reservation (channel_id, device_id, batch_uuid)
    SELECT r.channel_id, device_id, sqlc.arg('batch_uuid')::text
    FROM firmware_rollout r
    CROSS JOIN unnest(sqlc.arg('dispatched_device_ids')::bigint[]) AS dispatched(device_id)
    WHERE r.id = sqlc.arg('rollout_id')
      AND sqlc.arg('batch_uuid')::text <> ''
    ON CONFLICT (channel_id, device_id, batch_uuid) DO NOTHING
), locked_targets AS MATERIALIZED (
    SELECT target.rollout_id, target.device_id, target.last_sent_at, target.last_dispatched_at
    FROM firmware_rollout_device AS target
    WHERE target.rollout_id = sqlc.arg('rollout_id')
      AND target.device_id = ANY(sqlc.arg('device_ids')::bigint[])
    ORDER BY target.device_id
    FOR UPDATE
), attempt_times AS MATERIALIZED (
    SELECT rollout_id, device_id,
           GREATEST(last_sent_at, last_dispatched_at, clock_timestamp()) AS sent_at
    FROM locked_targets
)
UPDATE firmware_rollout_device AS target
SET attempts = target.attempts + 1,
    first_sent_at = COALESCE(target.first_sent_at, attempt_times.sent_at),
    last_sent_at = attempt_times.sent_at,
    last_dispatched_at = CASE
        WHEN target.device_id = ANY(sqlc.arg('dispatched_device_ids')::bigint[]) THEN attempt_times.sent_at
        ELSE target.last_dispatched_at
    END,
    last_dispatched_batch_uuid = CASE
        WHEN target.device_id = ANY(sqlc.arg('dispatched_device_ids')::bigint[]) THEN NULLIF(sqlc.arg('batch_uuid')::text, '')
        ELSE target.last_dispatched_batch_uuid
    END
FROM attempt_times
WHERE target.rollout_id = attempt_times.rollout_id
  AND target.device_id = attempt_times.device_id;

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
-- Re-queues the pair's suppressed miners (device_ids: what
-- ListReleaseChannelSuppressedMembers returns for the rollout's pair and
-- generation) into an active rollout and returns them. Miners the rollout
-- already holds are reset in place; miners whose halt lives in an earlier
-- rollout of the generation are added as unbatched late joiners, so the
-- earlier rollout's history stands and this one becomes the most recent to
-- hold them. Excluded rows are left alone: re-inclusion brings such a miner
-- back still halted, for the next retry.
WITH reset AS (
    UPDATE firmware_rollout_device held
    SET halted_at = NULL,
        halt_reason = '',
        last_error = '',
        skip_note = '',
        attempts = 0,
        first_sent_at = NULL,
        last_sent_at = NULL,
        last_dispatched_at = NULL,
        last_dispatched_batch_uuid = NULL,
        verified_at = NULL
    WHERE held.rollout_id = sqlc.arg('rollout_id')::bigint
      AND held.device_id = ANY(sqlc.arg('device_ids')::bigint[])
      AND held.halted_at IS NOT NULL
      AND held.excluded_at IS NULL
    RETURNING held.device_id
), added AS (
    INSERT INTO firmware_rollout_device (
        rollout_id, device_id, baseline_status, baseline_hash_rate_hs,
        baseline_power_w, baseline_efficiency_jh, baseline_temp_c,
        baseline_open_errors, baseline_at
    )
    SELECT current_rollout.id, ids.device_id, prior.baseline_status, prior.baseline_hash_rate_hs,
           prior.baseline_power_w, prior.baseline_efficiency_jh, prior.baseline_temp_c,
           prior.baseline_open_errors, prior.baseline_at
    FROM unnest(sqlc.arg('device_ids')::bigint[]) AS ids(device_id)
    JOIN firmware_rollout current_rollout ON current_rollout.id = sqlc.arg('rollout_id')::bigint
    LEFT JOIN LATERAL (
        SELECT previous.*
        FROM firmware_rollout_device previous
        JOIN firmware_rollout previous_rollout ON previous_rollout.id = previous.rollout_id
        WHERE previous.device_id = ids.device_id
          AND previous_rollout.id < current_rollout.id
          AND previous_rollout.channel_id = current_rollout.channel_id
          AND release_channel_pair_key(previous_rollout.manufacturer) = release_channel_pair_key(current_rollout.manufacturer)
          AND release_channel_pair_key(previous_rollout.model) = release_channel_pair_key(current_rollout.model)
          AND previous_rollout.assignment_generation = current_rollout.assignment_generation
        ORDER BY previous_rollout.id DESC
        LIMIT 1
    ) prior ON prior.halted_at IS NOT NULL
    WHERE NOT EXISTS (
        SELECT 1 FROM firmware_rollout_device existing
        WHERE existing.rollout_id = sqlc.arg('rollout_id')::bigint AND existing.device_id = ids.device_id
    )
    RETURNING firmware_rollout_device.device_id
)
SELECT reset.device_id FROM reset
UNION ALL
SELECT added.device_id FROM added;

-- name: PreserveFirmwareRolloutRetryBaselines :exec
-- Retrying a failed deployment preserves its original health expectations.
-- A failed update can itself stop hashing; recapturing that degraded state
-- would incorrectly lower the requirements for the retry to succeed.
WITH originals AS (
    SELECT target.device_id, prior.baseline_status, prior.baseline_hash_rate_hs,
           prior.baseline_power_w, prior.baseline_efficiency_jh, prior.baseline_temp_c,
           prior.baseline_open_errors, prior.baseline_at
    FROM firmware_rollout_device target
    JOIN firmware_rollout current_rollout ON current_rollout.id = target.rollout_id
    JOIN LATERAL (
        SELECT previous.*
        FROM firmware_rollout_device previous
        JOIN firmware_rollout previous_rollout ON previous_rollout.id = previous.rollout_id
        WHERE previous.device_id = target.device_id
          AND previous_rollout.id < current_rollout.id
          AND previous_rollout.channel_id = current_rollout.channel_id
          AND release_channel_pair_key(previous_rollout.manufacturer) = release_channel_pair_key(current_rollout.manufacturer)
          AND release_channel_pair_key(previous_rollout.model) = release_channel_pair_key(current_rollout.model)
          AND previous_rollout.assignment_generation = current_rollout.assignment_generation
        ORDER BY previous_rollout.id DESC
        LIMIT 1
    ) prior ON prior.halted_at IS NOT NULL
    WHERE target.rollout_id = sqlc.arg('rollout_id')
)
UPDATE firmware_rollout_device target
SET baseline_status = originals.baseline_status,
    baseline_hash_rate_hs = originals.baseline_hash_rate_hs,
    baseline_power_w = originals.baseline_power_w,
    baseline_efficiency_jh = originals.baseline_efficiency_jh,
    baseline_temp_c = originals.baseline_temp_c,
    baseline_open_errors = originals.baseline_open_errors,
    baseline_at = originals.baseline_at
FROM originals
WHERE target.rollout_id = sqlc.arg('rollout_id') AND target.device_id = originals.device_id;

-- name: ExcludeFirmwareRolloutDevices :exec
UPDATE firmware_rollout_device
SET excluded_at = now()
WHERE rollout_id = sqlc.arg('rollout_id')
  AND device_id = ANY(sqlc.arg('device_ids')::bigint[])
  AND excluded_at IS NULL;

-- name: RecordFirmwareDeployment :exec
-- Managed-deployment provenance: the miners listed reported the artifact a
-- rollout dispatched to them. Each aligned observation must still match the
-- current provenance; losing that race is a no-op and the caller reloads it.
-- Observed absence only permits insertion, never replacement of a concurrent
-- insert. Existing rows advance their timestamp strictly, including writes in
-- one transaction, so an older observation cannot match a later write. Rollout
-- IDs do not order deployments: an older rollout can make a corrective send.
-- The triggers on device_firmware_deployment advance the rollout's revision.
WITH observations AS (
    SELECT DISTINCT ids.device_id, ids.deployment_present, ids.deployed_at, ids.firmware_checksum
    FROM (
        SELECT unnest(sqlc.arg('device_ids')::bigint[]) AS device_id,
               unnest(sqlc.arg('expected_deployment_present')::boolean[]) AS deployment_present,
               unnest(sqlc.arg('expected_deployed_ats')::timestamptz[]) AS deployed_at,
               unnest(sqlc.arg('expected_firmware_checksums')::text[]) AS firmware_checksum
    ) ids
), updated AS (
    UPDATE device_firmware_deployment dep
    SET firmware_checksum = sqlc.arg('firmware_checksum'),
        firmware_version = sqlc.arg('firmware_version'),
        rollout_id = sqlc.arg('rollout_id'),
        deployed_at = GREATEST(clock_timestamp(), dep.deployed_at + INTERVAL '1 microsecond')
    FROM observations observed
    WHERE observed.deployment_present
      AND dep.device_id = observed.device_id
      AND dep.deployed_at = observed.deployed_at
      AND dep.firmware_checksum = observed.firmware_checksum
    RETURNING dep.device_id
)
INSERT INTO device_firmware_deployment (device_id, firmware_checksum, firmware_version, rollout_id, deployed_at)
SELECT observed.device_id, sqlc.arg('firmware_checksum'), sqlc.arg('firmware_version'), sqlc.arg('rollout_id'), clock_timestamp()
FROM observations observed
WHERE NOT observed.deployment_present
ON CONFLICT (device_id) DO NOTHING;
