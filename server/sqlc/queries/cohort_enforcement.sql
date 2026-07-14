-- name: UpsertDeviceFirmwareState :exec
INSERT INTO device_firmware_state (
    org_id,
    device_identifier,
    firmware_version,
    observed_at
) VALUES (
    sqlc.arg('org_id'),
    sqlc.arg('device_identifier'),
    sqlc.arg('firmware_version'),
    sqlc.arg('observed_at')
)
ON CONFLICT (org_id, device_identifier)
DO UPDATE SET
    firmware_version = EXCLUDED.firmware_version,
    observed_at = EXCLUDED.observed_at,
    updated_at = CURRENT_TIMESTAMP;

-- name: ListFirmwareEnforcementCandidates :many
WITH super_admin AS (
    SELECT
        u.id AS id,
        u.user_id AS external_user_id,
        u.username AS username
    FROM user_organization_role uor
    JOIN role r
      ON r.id = uor.role_id
     AND r.organization_id = uor.organization_id
    JOIN "user" u
      ON u.id = uor.user_id
    WHERE uor.organization_id = sqlc.arg('org_id')
      AND uor.scope_type = 'org'
      AND uor.deleted_at IS NULL
      AND r.deleted_at IS NULL
      AND r.builtin_key = 'SUPER_ADMIN'
      AND u.deleted_at IS NULL
    ORDER BY u.id
    LIMIT 1
)
SELECT
    d.org_id,
    d.device_identifier,
    COALESCE(dd.manufacturer, '')::text AS manufacturer,
    COALESCE(dd.model, '')::text AS model,
    c.id AS cohort_id,
    c.owner_user_id,
    c.owner_username,
    COALESCE(c.owner_user_id, super_admin.id, 0)::bigint AS actor_user_id,
    COALESCE(owner_user.user_id, super_admin.external_user_id, 'cohort-reconciler')::text AS actor_external_user_id,
    COALESCE(c.owner_username, owner_user.username, super_admin.username, 'cohort-reconciler')::text AS actor_username,
    cft.firmware_file_id,
    dfs.firmware_version AS observed_firmware_version,
    dfs.observed_at AS firmware_observed_at,
    des.state AS enforcement_state,
    des.desired_firmware_file_id AS state_desired_firmware_file_id,
    des.desired_firmware_version AS state_desired_firmware_version,
    des.retry_count AS retry_count,
    des.last_batch_uuid AS last_batch_uuid,
    des.last_dispatched_at AS last_dispatched_at,
    des.confirmed_at AS confirmed_at,
    des.last_error AS last_error
FROM device d
JOIN discovered_device dd
  ON dd.id = d.discovered_device_id
 AND dd.org_id = d.org_id
 AND dd.deleted_at IS NULL
JOIN device_pairing dp
  ON dp.device_id = d.id
 AND dp.pairing_status = 'PAIRED'
LEFT JOIN cohort_membership cm
  ON cm.org_id = d.org_id
 AND cm.device_identifier = d.device_identifier
JOIN cohort default_c
  ON default_c.org_id = d.org_id
 AND default_c.is_default = TRUE
 AND default_c.state = 'active'
JOIN cohort c
  ON c.id = COALESCE(cm.cohort_id, default_c.id)
 AND c.org_id = d.org_id
 AND c.state = 'active'
JOIN cohort_firmware_target cft
  ON cft.cohort_id = c.id
 AND cft.org_id = c.org_id
 AND LOWER(BTRIM(cft.manufacturer)) = LOWER(BTRIM(COALESCE(dd.manufacturer, '')))
 AND LOWER(BTRIM(cft.model)) = LOWER(BTRIM(COALESCE(dd.model, '')))
LEFT JOIN device_firmware_state dfs
  ON dfs.org_id = d.org_id
 AND dfs.device_identifier = d.device_identifier
LEFT JOIN device_enforcement_state des
  ON des.org_id = d.org_id
 AND des.device_identifier = d.device_identifier
 AND des.dimension = 'firmware'
LEFT JOIN "user" owner_user
  ON owner_user.id = c.owner_user_id
 AND owner_user.deleted_at IS NULL
LEFT JOIN super_admin ON TRUE
WHERE d.org_id = sqlc.arg('org_id')
  AND d.deleted_at IS NULL
  AND cft.firmware_file_id IS NOT NULL
ORDER BY d.device_identifier;

-- name: ListOrgsWithFirmwareTargets :many
SELECT DISTINCT org_id
FROM cohort_firmware_target
WHERE firmware_file_id IS NOT NULL
ORDER BY org_id;

-- name: ClaimFirmwareDispatch :execrows
INSERT INTO device_enforcement_state (
    org_id,
    device_identifier,
    dimension,
    state,
    desired_firmware_file_id,
    desired_firmware_version,
    retry_count,
    last_error
) VALUES (
    sqlc.arg('org_id'),
    sqlc.arg('device_identifier'),
    'firmware',
    'dispatching',
    sqlc.arg('desired_firmware_file_id'),
    sqlc.arg('desired_firmware_version'),
    0,
    NULL
)
ON CONFLICT (org_id, device_identifier, dimension)
DO UPDATE SET
    state = 'dispatching',
    desired_firmware_file_id = EXCLUDED.desired_firmware_file_id,
    desired_firmware_version = EXCLUDED.desired_firmware_version,
    retry_count = CASE
        WHEN device_enforcement_state.desired_firmware_file_id IS DISTINCT FROM EXCLUDED.desired_firmware_file_id
          OR device_enforcement_state.desired_firmware_version IS DISTINCT FROM EXCLUDED.desired_firmware_version
        THEN 0
        ELSE device_enforcement_state.retry_count
    END,
    last_batch_uuid = CASE
        WHEN device_enforcement_state.desired_firmware_file_id IS DISTINCT FROM EXCLUDED.desired_firmware_file_id
          OR device_enforcement_state.desired_firmware_version IS DISTINCT FROM EXCLUDED.desired_firmware_version
        THEN NULL
        ELSE device_enforcement_state.last_batch_uuid
    END,
    last_dispatched_at = CASE
        WHEN device_enforcement_state.desired_firmware_file_id IS DISTINCT FROM EXCLUDED.desired_firmware_file_id
          OR device_enforcement_state.desired_firmware_version IS DISTINCT FROM EXCLUDED.desired_firmware_version
        THEN NULL
        ELSE device_enforcement_state.last_dispatched_at
    END,
    confirmed_at = CASE
        WHEN device_enforcement_state.desired_firmware_file_id IS DISTINCT FROM EXCLUDED.desired_firmware_file_id
          OR device_enforcement_state.desired_firmware_version IS DISTINCT FROM EXCLUDED.desired_firmware_version
        THEN NULL
        ELSE device_enforcement_state.confirmed_at
    END,
    observed_at = CASE
        WHEN device_enforcement_state.desired_firmware_file_id IS DISTINCT FROM EXCLUDED.desired_firmware_file_id
          OR device_enforcement_state.desired_firmware_version IS DISTINCT FROM EXCLUDED.desired_firmware_version
        THEN NULL
        ELSE device_enforcement_state.observed_at
    END,
    last_error = NULL,
    updated_at = CURRENT_TIMESTAMP
WHERE device_enforcement_state.state IN ('pending', 'drifted')
   OR device_enforcement_state.desired_firmware_file_id IS DISTINCT FROM EXCLUDED.desired_firmware_file_id
   OR device_enforcement_state.desired_firmware_version IS DISTINCT FROM EXCLUDED.desired_firmware_version
   OR (
      device_enforcement_state.state = 'dispatching'
      AND device_enforcement_state.updated_at < sqlc.arg('dispatching_before')
   );

-- name: MarkFirmwareDispatched :execrows
UPDATE device_enforcement_state
SET state = 'dispatched',
    desired_firmware_file_id = sqlc.arg('desired_firmware_file_id'),
    desired_firmware_version = sqlc.arg('desired_firmware_version'),
    last_batch_uuid = sqlc.arg('last_batch_uuid'),
    last_dispatched_at = sqlc.arg('last_dispatched_at'),
    last_error = NULL,
    updated_at = CURRENT_TIMESTAMP
WHERE org_id = sqlc.arg('org_id')
  AND device_identifier = sqlc.arg('device_identifier')
  AND dimension = 'firmware'
  AND state = 'dispatching'
  AND desired_firmware_file_id IS NOT DISTINCT FROM sqlc.arg('desired_firmware_file_id')
  AND desired_firmware_version IS NOT DISTINCT FROM sqlc.arg('desired_firmware_version');

-- name: MarkFirmwareConfirmed :execrows
UPDATE device_enforcement_state
SET state = 'confirmed',
    last_batch_uuid = CASE
        WHEN desired_firmware_file_id IS DISTINCT FROM sqlc.arg('desired_firmware_file_id')
          OR desired_firmware_version IS DISTINCT FROM sqlc.arg('desired_firmware_version')
        THEN NULL
        ELSE last_batch_uuid
    END,
    last_dispatched_at = CASE
        WHEN desired_firmware_file_id IS DISTINCT FROM sqlc.arg('desired_firmware_file_id')
          OR desired_firmware_version IS DISTINCT FROM sqlc.arg('desired_firmware_version')
        THEN NULL
        ELSE last_dispatched_at
    END,
    desired_firmware_file_id = sqlc.arg('desired_firmware_file_id'),
    desired_firmware_version = sqlc.arg('desired_firmware_version'),
    retry_count = 0,
    confirmed_at = sqlc.arg('confirmed_at'),
    observed_at = sqlc.arg('observed_at'),
    last_error = NULL,
    updated_at = CURRENT_TIMESTAMP
WHERE org_id = sqlc.arg('org_id')
  AND device_identifier = sqlc.arg('device_identifier')
  AND dimension = 'firmware';

-- name: MarkFirmwareDrifted :execrows
UPDATE device_enforcement_state
SET state = 'drifted',
    observed_at = sqlc.arg('observed_at'),
    updated_at = CURRENT_TIMESTAMP
WHERE org_id = sqlc.arg('org_id')
  AND device_identifier = sqlc.arg('device_identifier')
  AND dimension = 'firmware'
  AND state IN ('confirmed', 'dispatched');

-- name: MarkFirmwareDispatchFailure :execrows
UPDATE device_enforcement_state
SET state = CASE
        WHEN retry_count + 1 >= sqlc.arg('max_retries') THEN 'failed'
        ELSE sqlc.arg('retry_state')
    END,
    retry_count = retry_count + 1,
    last_error = sqlc.arg('last_error'),
    updated_at = CURRENT_TIMESTAMP
WHERE org_id = sqlc.arg('org_id')
  AND device_identifier = sqlc.arg('device_identifier')
  AND dimension = 'firmware'
  AND state IN ('dispatching', 'drifted', 'pending')
  AND desired_firmware_file_id IS NOT DISTINCT FROM sqlc.arg('desired_firmware_file_id')
  AND desired_firmware_version IS NOT DISTINCT FROM sqlc.arg('desired_firmware_version');

-- name: MarkFirmwareDispatchHeld :execrows
UPDATE device_enforcement_state
SET state = sqlc.arg('retry_state'),
    last_error = sqlc.arg('last_error'),
    updated_at = CURRENT_TIMESTAMP
WHERE org_id = sqlc.arg('org_id')
  AND device_identifier = sqlc.arg('device_identifier')
  AND dimension = 'firmware'
  AND state = 'dispatching'
  AND desired_firmware_file_id IS NOT DISTINCT FROM sqlc.arg('desired_firmware_file_id')
  AND desired_firmware_version IS NOT DISTINCT FROM sqlc.arg('desired_firmware_version');

-- name: ResetFirmwareEnforcementForDevices :execrows
DELETE FROM device_enforcement_state
WHERE org_id = sqlc.arg('org_id')
  AND dimension = 'firmware'
  AND device_identifier = ANY(sqlc.arg('device_identifiers')::text[]);

-- name: ResetFirmwareEnforcementForFirmwareFile :execrows
DELETE FROM device_enforcement_state
WHERE org_id = sqlc.arg('org_id')
  AND dimension = 'firmware'
  AND desired_firmware_file_id = sqlc.arg('firmware_file_id');

-- name: ResetFirmwareEnforcementForCohortMembers :execrows
DELETE FROM device_enforcement_state des
USING cohort_membership cm
WHERE des.org_id = sqlc.arg('org_id')
  AND des.dimension = 'firmware'
  AND des.device_identifier = cm.device_identifier
  AND cm.org_id = sqlc.arg('org_id')
  AND cm.cohort_id = sqlc.arg('cohort_id');

-- name: ResetFirmwareEnforcementForCohortTarget :execrows
WITH target_cohort AS (
    SELECT cohort.id, cohort.is_default
    FROM cohort
    WHERE cohort.id = sqlc.arg('cohort_id')
      AND cohort.org_id = sqlc.arg('org_id')
),
affected_devices AS (
    SELECT d.device_identifier
    FROM device d
    JOIN discovered_device dd
      ON dd.id = d.discovered_device_id
     AND dd.org_id = d.org_id
     AND dd.deleted_at IS NULL
    LEFT JOIN cohort_membership cm
      ON cm.org_id = d.org_id
     AND cm.device_identifier = d.device_identifier
    JOIN target_cohort tc
      ON (tc.is_default AND cm.cohort_id IS NULL)
      OR (NOT tc.is_default AND cm.cohort_id = tc.id)
    WHERE d.org_id = sqlc.arg('org_id')
      AND d.deleted_at IS NULL
      AND LOWER(BTRIM(COALESCE(dd.manufacturer, ''))) = LOWER(BTRIM(sqlc.arg('manufacturer')::text))
      AND LOWER(BTRIM(COALESCE(dd.model, ''))) = LOWER(BTRIM(sqlc.arg('model')::text))
)
DELETE FROM device_enforcement_state des
USING affected_devices affected
WHERE des.org_id = sqlc.arg('org_id')
  AND des.dimension = 'firmware'
  AND des.device_identifier = affected.device_identifier;

-- name: UpsertCohortReconcilerHeartbeat :exec
INSERT INTO cohort_reconciler_heartbeat (
    id,
    last_tick_at,
    last_tick_uuid,
    last_tick_duration_ms,
    active_device_count
) VALUES (
    1,
    sqlc.arg('last_tick_at'),
    sqlc.arg('last_tick_uuid'),
    sqlc.narg('last_tick_duration_ms'),
    sqlc.arg('active_device_count')
)
ON CONFLICT (id)
DO UPDATE SET
    last_tick_at = EXCLUDED.last_tick_at,
    last_tick_uuid = EXCLUDED.last_tick_uuid,
    last_tick_duration_ms = EXCLUDED.last_tick_duration_ms,
    active_device_count = EXCLUDED.active_device_count;

-- name: UpsertDeviceConfigState :exec
INSERT INTO device_config_state (
    org_id, device_identifier, dimension, observed_state_jsonb,
    observed_state_hash, observed_at
) VALUES (
    sqlc.arg('org_id'), sqlc.arg('device_identifier'), sqlc.arg('dimension'),
    sqlc.arg('observed_state_jsonb'), sqlc.arg('observed_state_hash'), sqlc.arg('observed_at')
)
ON CONFLICT (org_id, device_identifier, dimension)
DO UPDATE SET
    observed_state_jsonb = EXCLUDED.observed_state_jsonb,
    observed_state_hash = EXCLUDED.observed_state_hash,
    observed_at = EXCLUDED.observed_at,
    updated_at = CURRENT_TIMESTAMP;

-- name: ListOrgsWithDesiredConfig :many
SELECT DISTINCT org_id
FROM cohort
WHERE state = 'active'
  AND desired_config_jsonb IS NOT NULL
  AND desired_config_jsonb <> '{}'::jsonb
ORDER BY org_id;

-- name: ListConfigEnforcementCandidates :many
WITH super_admin AS (
    SELECT u.id, u.user_id AS external_user_id, u.username
    FROM user_organization_role uor
    JOIN role r ON r.id = uor.role_id AND r.organization_id = uor.organization_id
    JOIN "user" u ON u.id = uor.user_id
    WHERE uor.organization_id = sqlc.arg('org_id')
      AND uor.scope_type = 'org'
      AND uor.deleted_at IS NULL
      AND r.deleted_at IS NULL
      AND r.builtin_key = 'SUPER_ADMIN'
      AND u.deleted_at IS NULL
    ORDER BY u.id
    LIMIT 1
)
SELECT
    d.org_id,
    d.device_identifier,
    COALESCE(dd.driver_name, '')::text AS driver_name,
    COALESCE(dd.manufacturer, '')::text AS manufacturer,
    COALESCE(dd.model, '')::text AS model,
    COALESCE(d.worker_name, '')::text AS worker_name,
    c.id AS cohort_id,
    COALESCE(c.owner_user_id, super_admin.id, 0)::bigint AS actor_user_id,
    COALESCE(owner_user.user_id, super_admin.external_user_id, 'cohort-reconciler')::text AS actor_external_user_id,
    COALESCE(c.owner_username, owner_user.username, super_admin.username, 'cohort-reconciler')::text AS actor_username,
    c.desired_config_jsonb,
    dcs.observed_state_jsonb,
    dcs.observed_state_hash,
    dcs.observed_at AS config_observed_at,
    des.desired_state_hash,
    des.supported,
    des.state AS enforcement_state,
    des.retry_count,
    des.last_batch_uuid,
    des.last_dispatched_at,
    des.confirmed_at,
    des.last_error
FROM device d
JOIN discovered_device dd
  ON dd.id = d.discovered_device_id
 AND dd.org_id = d.org_id
 AND dd.deleted_at IS NULL
JOIN device_pairing dp
  ON dp.device_id = d.id
 AND dp.pairing_status = 'PAIRED'
LEFT JOIN cohort_membership cm
  ON cm.org_id = d.org_id
 AND cm.device_identifier = d.device_identifier
JOIN cohort default_c
  ON default_c.org_id = d.org_id
 AND default_c.is_default = TRUE
 AND default_c.state = 'active'
JOIN cohort c
  ON c.id = COALESCE(cm.cohort_id, default_c.id)
 AND c.org_id = d.org_id
 AND c.state = 'active'
LEFT JOIN device_config_state dcs
  ON dcs.org_id = d.org_id
 AND dcs.device_identifier = d.device_identifier
 AND dcs.dimension = sqlc.arg('dimension')
LEFT JOIN device_enforcement_state des
  ON des.org_id = d.org_id
 AND des.device_identifier = d.device_identifier
 AND des.dimension = sqlc.arg('dimension')
LEFT JOIN "user" owner_user
  ON owner_user.id = c.owner_user_id
 AND owner_user.deleted_at IS NULL
LEFT JOIN super_admin ON TRUE
WHERE d.org_id = sqlc.arg('org_id')
  AND d.deleted_at IS NULL
  AND c.desired_config_jsonb IS NOT NULL
  AND c.desired_config_jsonb <> '{}'::jsonb
ORDER BY d.device_identifier;

-- name: ClaimConfigDispatch :execrows
INSERT INTO device_enforcement_state (
    org_id, device_identifier, dimension, state, desired_state_hash,
    retry_count, last_error, supported
) VALUES (
    sqlc.arg('org_id'), sqlc.arg('device_identifier'), sqlc.arg('dimension'),
    'dispatching', sqlc.arg('desired_state_hash'), 0, NULL, TRUE
)
ON CONFLICT (org_id, device_identifier, dimension)
DO UPDATE SET
    state = 'dispatching',
    desired_state_hash = EXCLUDED.desired_state_hash,
    supported = TRUE,
    retry_count = CASE WHEN device_enforcement_state.desired_state_hash IS DISTINCT FROM EXCLUDED.desired_state_hash THEN 0 ELSE device_enforcement_state.retry_count END,
    last_batch_uuid = CASE WHEN device_enforcement_state.desired_state_hash IS DISTINCT FROM EXCLUDED.desired_state_hash THEN NULL ELSE device_enforcement_state.last_batch_uuid END,
    last_dispatched_at = CASE WHEN device_enforcement_state.desired_state_hash IS DISTINCT FROM EXCLUDED.desired_state_hash THEN NULL ELSE device_enforcement_state.last_dispatched_at END,
    confirmed_at = CASE WHEN device_enforcement_state.desired_state_hash IS DISTINCT FROM EXCLUDED.desired_state_hash THEN NULL ELSE device_enforcement_state.confirmed_at END,
    observed_at = CASE WHEN device_enforcement_state.desired_state_hash IS DISTINCT FROM EXCLUDED.desired_state_hash THEN NULL ELSE device_enforcement_state.observed_at END,
    last_error = NULL,
    updated_at = CURRENT_TIMESTAMP
WHERE device_enforcement_state.state IN ('pending', 'drifted', 'held')
   OR device_enforcement_state.desired_state_hash IS DISTINCT FROM EXCLUDED.desired_state_hash
   OR (device_enforcement_state.state = 'dispatching' AND device_enforcement_state.updated_at < sqlc.arg('dispatching_before'));

-- name: UpsertConfigSupport :exec
INSERT INTO device_enforcement_state (
    org_id, device_identifier, dimension, state, desired_state_hash, supported
) VALUES (
    sqlc.arg('org_id'), sqlc.arg('device_identifier'), sqlc.arg('dimension'),
    'pending', sqlc.arg('desired_state_hash'), sqlc.arg('supported')
)
ON CONFLICT (org_id, device_identifier, dimension)
DO UPDATE SET
    supported = EXCLUDED.supported,
    updated_at = CURRENT_TIMESTAMP;

-- name: MarkConfigDispatched :execrows
UPDATE device_enforcement_state
SET state = 'dispatched', last_batch_uuid = sqlc.arg('last_batch_uuid'),
    last_dispatched_at = sqlc.arg('last_dispatched_at'), last_error = NULL,
    updated_at = CURRENT_TIMESTAMP
WHERE org_id = sqlc.arg('org_id') AND device_identifier = sqlc.arg('device_identifier')
  AND dimension = sqlc.arg('dimension') AND state = 'dispatching'
  AND desired_state_hash = sqlc.arg('desired_state_hash');

-- name: MarkConfigConfirmed :execrows
UPDATE device_enforcement_state
SET state = 'confirmed', desired_state_hash = sqlc.arg('desired_state_hash'),
    retry_count = 0, confirmed_at = sqlc.arg('confirmed_at'),
    observed_at = sqlc.arg('observed_at'), last_error = NULL,
    updated_at = CURRENT_TIMESTAMP
WHERE org_id = sqlc.arg('org_id') AND device_identifier = sqlc.arg('device_identifier')
  AND dimension = sqlc.arg('dimension');

-- name: MarkConfigDrifted :execrows
UPDATE device_enforcement_state
SET state = 'drifted', observed_at = sqlc.arg('observed_at'), updated_at = CURRENT_TIMESTAMP
WHERE org_id = sqlc.arg('org_id') AND device_identifier = sqlc.arg('device_identifier')
  AND dimension = sqlc.arg('dimension') AND state IN ('confirmed', 'dispatched');

-- name: MarkConfigDispatchFailure :execrows
UPDATE device_enforcement_state
SET state = CASE WHEN retry_count + 1 >= sqlc.arg('max_retries') THEN 'failed' ELSE sqlc.arg('retry_state') END,
    retry_count = retry_count + 1, last_error = sqlc.arg('last_error'), updated_at = CURRENT_TIMESTAMP
WHERE org_id = sqlc.arg('org_id') AND device_identifier = sqlc.arg('device_identifier')
  AND dimension = sqlc.arg('dimension') AND state IN ('dispatching', 'drifted', 'pending', 'held')
  AND desired_state_hash = sqlc.arg('desired_state_hash');

-- name: MarkConfigDispatchHeld :execrows
UPDATE device_enforcement_state
SET state = 'held', last_error = sqlc.arg('last_error'),
    last_dispatched_at = sqlc.arg('last_dispatched_at'), updated_at = CURRENT_TIMESTAMP
WHERE org_id = sqlc.arg('org_id') AND device_identifier = sqlc.arg('device_identifier')
  AND dimension = sqlc.arg('dimension') AND state = 'dispatching'
  AND desired_state_hash = sqlc.arg('desired_state_hash');
