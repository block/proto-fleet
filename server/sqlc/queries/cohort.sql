-- name: CreateCohort :one
INSERT INTO cohort (
    org_id,
    label,
    is_default,
    owner_user_id,
    owner_username,
    expires_at,
    desired_config_jsonb,
    state,
    purpose,
    source_actor_type,
    source_actor_id,
    idempotency_key
) VALUES (
    sqlc.arg('org_id'),
    sqlc.arg('label'),
    FALSE,
    sqlc.narg('owner_user_id'),
    sqlc.narg('owner_username'),
    sqlc.narg('expires_at'),
    sqlc.narg('desired_config_jsonb')::jsonb,
    'active',
    sqlc.arg('purpose'),
    sqlc.arg('source_actor_type'),
    sqlc.narg('source_actor_id'),
    sqlc.narg('idempotency_key')
)
RETURNING *;

-- name: CreateDefaultCohort :exec
-- Seeds the single is_default cohort for a freshly created org. Values mirror
-- the per-org default seeded for pre-existing orgs in migration 000094; the
-- uq_cohort_one_default_per_org partial index enforces one default per org.
INSERT INTO cohort (
    org_id,
    label,
    is_default,
    state,
    purpose,
    source_actor_type
) VALUES (
    sqlc.arg('org_id'),
    'Default',
    TRUE,
    'active',
    'Default cohort',
    'scheduler'
);

-- name: GetCohort :one
SELECT
    c.*,
    CASE
        WHEN c.is_default THEN (
            SELECT COUNT(*)::bigint
            FROM device d_default
            LEFT JOIN cohort_membership cm_default
                ON cm_default.org_id = d_default.org_id
               AND cm_default.device_identifier = d_default.device_identifier
            WHERE d_default.org_id = c.org_id
              AND d_default.deleted_at IS NULL
              AND cm_default.cohort_id IS NULL
        )
        ELSE COALESCE(m.explicit_member_count, 0)::bigint
    END AS explicit_member_count
FROM cohort c
LEFT JOIN (
    SELECT cohort_id, COUNT(*) AS explicit_member_count
    FROM cohort_membership
    GROUP BY cohort_id
) m ON m.cohort_id = c.id
WHERE c.id = sqlc.arg('id')
  AND c.org_id = sqlc.arg('org_id');

-- name: ListCohorts :many
SELECT
    c.*,
    CASE
        WHEN c.is_default THEN (
            SELECT COUNT(*)::bigint
            FROM device d_default
            LEFT JOIN cohort_membership cm_default
                ON cm_default.org_id = d_default.org_id
               AND cm_default.device_identifier = d_default.device_identifier
            WHERE d_default.org_id = c.org_id
              AND d_default.deleted_at IS NULL
              AND cm_default.cohort_id IS NULL
        )
        ELSE COALESCE(m.explicit_member_count, 0)::bigint
    END AS explicit_member_count
FROM cohort c
LEFT JOIN (
    SELECT cohort_id, COUNT(*) AS explicit_member_count
    FROM cohort_membership
    GROUP BY cohort_id
) m ON m.cohort_id = c.id
WHERE c.org_id = sqlc.arg('org_id')
  AND (sqlc.arg('include_released')::boolean OR c.state = 'active')
  AND (
    NOT sqlc.arg('search_filter_set')::boolean
    OR c.label ILIKE '%' || sqlc.arg('search')::text || '%'
    OR c.purpose ILIKE '%' || sqlc.arg('search')::text || '%'
    OR COALESCE(c.owner_username, '') ILIKE '%' || sqlc.arg('search')::text || '%'
  )
  AND (
    NOT sqlc.arg('cursor_set')::boolean
    OR c.is_default < sqlc.arg('cursor_is_default')::boolean
    OR (
      c.is_default = sqlc.arg('cursor_is_default')::boolean
      AND (
        c.updated_at < sqlc.narg('cursor_updated_at')::timestamptz
        OR (c.updated_at = sqlc.narg('cursor_updated_at')::timestamptz AND c.id < sqlc.narg('cursor_id')::bigint)
      )
    )
  )
ORDER BY c.is_default DESC, c.updated_at DESC, c.id DESC
LIMIT sqlc.arg('limit_count')::int;

-- name: CountCohorts :one
SELECT COUNT(*)::bigint
FROM cohort c
WHERE c.org_id = sqlc.arg('org_id')
  AND (sqlc.arg('include_released')::boolean OR c.state = 'active')
  AND (
    NOT sqlc.arg('search_filter_set')::boolean
    OR c.label ILIKE '%' || sqlc.arg('search')::text || '%'
    OR c.purpose ILIKE '%' || sqlc.arg('search')::text || '%'
    OR COALESCE(c.owner_username, '') ILIKE '%' || sqlc.arg('search')::text || '%'
  );
-- name: ListCohortsByOwner :many
SELECT
    c.*,
    CASE
        WHEN c.is_default THEN (
            SELECT COUNT(*)::bigint
            FROM device d_default
            LEFT JOIN cohort_membership cm_default
                ON cm_default.org_id = d_default.org_id
               AND cm_default.device_identifier = d_default.device_identifier
            WHERE d_default.org_id = c.org_id
              AND d_default.deleted_at IS NULL
              AND cm_default.cohort_id IS NULL
        )
        ELSE COALESCE(m.explicit_member_count, 0)::bigint
    END AS explicit_member_count
FROM cohort c
LEFT JOIN (
    SELECT cohort_id, COUNT(*) AS explicit_member_count
    FROM cohort_membership
    GROUP BY cohort_id
) m ON m.cohort_id = c.id
WHERE c.org_id = sqlc.arg('org_id')
  AND c.owner_user_id = sqlc.arg('owner_user_id')
  AND (sqlc.arg('include_released')::boolean OR c.state = 'active')
  AND (
    NOT sqlc.arg('search_filter_set')::boolean
    OR c.label ILIKE '%' || sqlc.arg('search')::text || '%'
    OR c.purpose ILIKE '%' || sqlc.arg('search')::text || '%'
    OR COALESCE(c.owner_username, '') ILIKE '%' || sqlc.arg('search')::text || '%'
  )
  AND (
    NOT sqlc.arg('cursor_set')::boolean
    OR c.updated_at < sqlc.narg('cursor_updated_at')::timestamptz
    OR (c.updated_at = sqlc.narg('cursor_updated_at')::timestamptz AND c.id < sqlc.narg('cursor_id')::bigint)
  )
ORDER BY c.updated_at DESC, c.id DESC
LIMIT sqlc.arg('limit_count')::int;

-- name: CountCohortsByOwner :one
SELECT COUNT(*)::bigint
FROM cohort c
WHERE c.org_id = sqlc.arg('org_id')
  AND c.owner_user_id = sqlc.arg('owner_user_id')
  AND (sqlc.arg('include_released')::boolean OR c.state = 'active')
  AND (
    NOT sqlc.arg('search_filter_set')::boolean
    OR c.label ILIKE '%' || sqlc.arg('search')::text || '%'
    OR c.purpose ILIKE '%' || sqlc.arg('search')::text || '%'
    OR COALESCE(c.owner_username, '') ILIKE '%' || sqlc.arg('search')::text || '%'
  );

-- name: UpdateCohort :one
UPDATE cohort
SET label = COALESCE(sqlc.narg('label'), label),
    purpose = COALESCE(sqlc.narg('purpose'), purpose),
    expires_at = CASE
        WHEN sqlc.arg('clear_expires_at')::boolean THEN NULL
        WHEN sqlc.narg('expires_at')::timestamptz IS NOT NULL THEN sqlc.narg('expires_at')::timestamptz
        ELSE expires_at
    END,
    desired_config_jsonb = CASE
        WHEN sqlc.arg('clear_desired_config')::boolean THEN NULL
        WHEN sqlc.arg('desired_config_jsonb_set')::boolean THEN sqlc.narg('desired_config_jsonb')::jsonb
        ELSE desired_config_jsonb
    END,
    updated_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg('id')
  AND org_id = sqlc.arg('org_id')
  AND is_default = FALSE
  AND state = 'active'
RETURNING *;

-- name: UpdateDefaultCohortConfig :one
UPDATE cohort
SET desired_config_jsonb = CASE
        WHEN sqlc.arg('clear_desired_config')::boolean THEN NULL
        ELSE sqlc.narg('desired_config_jsonb')::jsonb
    END,
    updated_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg('id')
  AND org_id = sqlc.arg('org_id')
  AND is_default = TRUE
  AND state = 'active'
RETURNING *;

-- name: ListCohortFirmwareTargets :many
SELECT *
FROM cohort_firmware_target
WHERE cohort_id = sqlc.arg('cohort_id')
  AND org_id = sqlc.arg('org_id')
ORDER BY manufacturer, model;

-- name: UpsertCohortFirmwareTarget :one
INSERT INTO cohort_firmware_target (
    cohort_id,
    org_id,
    manufacturer,
    model,
    firmware_file_id
) VALUES (
    sqlc.arg('cohort_id'),
    sqlc.arg('org_id'),
    sqlc.arg('manufacturer'),
    sqlc.arg('model'),
    sqlc.narg('firmware_file_id')
)
ON CONFLICT (cohort_id, (LOWER(BTRIM(manufacturer))), (LOWER(BTRIM(model))))
DO UPDATE SET
    firmware_file_id = EXCLUDED.firmware_file_id,
    manufacturer = EXCLUDED.manufacturer,
    model = EXCLUDED.model,
    updated_at = CURRENT_TIMESTAMP
RETURNING *;

-- name: DeleteCohortFirmwareTarget :execrows
DELETE FROM cohort_firmware_target
WHERE cohort_id = sqlc.arg('cohort_id')
  AND org_id = sqlc.arg('org_id')
  AND LOWER(BTRIM(manufacturer)) = LOWER(BTRIM(sqlc.arg('manufacturer')::text))
  AND LOWER(BTRIM(model)) = LOWER(BTRIM(sqlc.arg('model')::text));

-- name: ClearCohortFirmwareTargetFileReferences :execrows
DELETE FROM cohort_firmware_target
WHERE org_id = sqlc.arg('org_id')
  AND firmware_file_id = sqlc.arg('firmware_file_id');

-- name: ReleaseCohort :one
UPDATE cohort
SET state = 'released',
    updated_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg('id')
  AND org_id = sqlc.arg('org_id')
  AND is_default = FALSE
RETURNING *;

-- name: ListExpiredActiveCohorts :many
SELECT
    c.*,
    CASE
        WHEN c.is_default THEN (
            SELECT COUNT(*)::bigint
            FROM device d_default
            LEFT JOIN cohort_membership cm_default
                ON cm_default.org_id = d_default.org_id
               AND cm_default.device_identifier = d_default.device_identifier
            WHERE d_default.org_id = c.org_id
              AND d_default.deleted_at IS NULL
              AND cm_default.cohort_id IS NULL
        )
        ELSE COALESCE(m.explicit_member_count, 0)::bigint
    END AS explicit_member_count
FROM cohort c
LEFT JOIN (
    SELECT cohort_id, COUNT(*) AS explicit_member_count
    FROM cohort_membership
    GROUP BY cohort_id
) m ON m.cohort_id = c.id
WHERE c.state = 'active'
  AND c.is_default = FALSE
  AND c.expires_at IS NOT NULL
  AND c.expires_at <= CURRENT_TIMESTAMP
ORDER BY c.expires_at, c.id;

-- name: InsertCohortMembership :exec
INSERT INTO cohort_membership (
    cohort_id,
    org_id,
    device_identifier
) VALUES (
    sqlc.arg('cohort_id'),
    sqlc.arg('org_id'),
    sqlc.arg('device_identifier')
);

-- name: BulkInsertCohortMemberships :execrows
INSERT INTO cohort_membership (
    cohort_id,
    org_id,
    device_identifier
)
SELECT
    sqlc.arg('cohort_id'),
    sqlc.arg('org_id'),
    payload.device_identifier
FROM jsonb_to_recordset(sqlc.arg('members_jsonb')::jsonb)
    AS payload(device_identifier text);

-- name: DeleteCohortMembershipsByCohort :execrows
DELETE FROM cohort_membership
WHERE cohort_id = sqlc.arg('cohort_id')
  AND org_id = sqlc.arg('org_id');

-- name: DeleteCohortMemberships :execrows
DELETE FROM cohort_membership
WHERE cohort_id = sqlc.arg('cohort_id')
  AND org_id = sqlc.arg('org_id')
  AND device_identifier = ANY(sqlc.arg('device_identifiers')::text[]);

-- name: CountCohortMemberships :one
SELECT COUNT(*)::bigint
FROM cohort_membership
WHERE cohort_id = sqlc.arg('cohort_id')
  AND org_id = sqlc.arg('org_id')
  AND device_identifier = ANY(sqlc.arg('device_identifiers')::text[]);

-- name: DeleteCohortMembershipsByDevice :execrows
DELETE FROM cohort_membership
WHERE org_id = sqlc.arg('org_id')
  AND device_identifier = ANY(sqlc.arg('device_identifiers')::text[]);

-- name: ListCohortMembers :many
SELECT
    cm.*,
    COALESCE(
        NULLIF(d.custom_name, ''),
        NULLIF(TRIM(CONCAT_WS(' ', NULLIF(dd.manufacturer, ''), NULLIF(dd.model, ''))), ''),
        ''
    )::text AS display_name,
    COALESCE(d.worker_name, '') AS worker_name,
    COALESCE(dd.manufacturer, '') AS manufacturer,
    COALESCE(dd.model, '') AS model,
    COALESCE(dd.ip_address, '') AS ip_address,
    COALESCE(dd.firmware_version, '') AS firmware_version,
    COALESCE(d.serial_number, '') AS serial_number
FROM cohort_membership cm
LEFT JOIN device d
    ON d.org_id = cm.org_id
   AND d.device_identifier = cm.device_identifier
   AND d.deleted_at IS NULL
LEFT JOIN discovered_device dd
    ON dd.id = d.discovered_device_id
   AND dd.org_id = d.org_id
   AND dd.deleted_at IS NULL
WHERE cm.cohort_id = sqlc.arg('cohort_id')
  AND cm.org_id = sqlc.arg('org_id')
ORDER BY cm.added_at, cm.device_identifier;

-- name: ListCohortFirmwareStatuses :many
WITH requested_cohorts AS (
    SELECT id, org_id, is_default
    FROM cohort c
    WHERE c.org_id = sqlc.arg('org_id')
      AND c.id = ANY(sqlc.arg('cohort_ids')::bigint[])
),
effective_devices AS (
    SELECT
        rc.id AS cohort_id,
        d.org_id AS org_id,
        d.id AS device_id,
        d.device_identifier AS device_identifier,
        COALESCE(dd.manufacturer, '')::text AS manufacturer,
        COALESCE(dd.model, '')::text AS model,
        COALESCE(dd.firmware_version, '')::text AS discovered_firmware_version,
        COALESCE(ds.status::text, '')::text AS device_status
    FROM requested_cohorts rc
    JOIN device d
        ON d.org_id = rc.org_id
       AND d.deleted_at IS NULL
    JOIN discovered_device dd
        ON dd.id = d.discovered_device_id
       AND dd.org_id = d.org_id
       AND dd.deleted_at IS NULL
    LEFT JOIN cohort_membership cm
        ON cm.org_id = d.org_id
       AND cm.device_identifier = d.device_identifier
    LEFT JOIN device_status ds
        ON ds.device_id = d.id
    WHERE (
        rc.is_default = TRUE
        AND cm.cohort_id IS NULL
    ) OR (
        rc.is_default = FALSE
        AND cm.cohort_id = rc.id
    )
)
SELECT
    ed.cohort_id,
    ed.device_identifier,
    cft.firmware_file_id AS target_firmware_file_id,
    COALESCE(dfs.firmware_version, ed.discovered_firmware_version, '')::text AS current_firmware_version,
    dfs.observed_at AS firmware_observed_at,
    des.state AS enforcement_state,
    des.desired_firmware_file_id AS state_desired_firmware_file_id,
    des.desired_firmware_version AS state_desired_firmware_version,
    des.retry_count AS retry_count,
    des.last_error AS last_error,
    des.last_dispatched_at AS last_dispatched_at,
    des.confirmed_at AS confirmed_at,
    ed.device_status
FROM effective_devices ed
LEFT JOIN cohort_firmware_target cft
    ON cft.cohort_id = ed.cohort_id
   AND cft.org_id = ed.org_id
   AND LOWER(BTRIM(cft.manufacturer)) = LOWER(BTRIM(ed.manufacturer))
   AND LOWER(BTRIM(cft.model)) = LOWER(BTRIM(ed.model))
LEFT JOIN device_firmware_state dfs
    ON dfs.org_id = ed.org_id
   AND dfs.device_identifier = ed.device_identifier
LEFT JOIN device_enforcement_state des
    ON des.org_id = ed.org_id
   AND des.device_identifier = ed.device_identifier
   AND des.dimension = 'firmware'
   AND cft.firmware_file_id IS NOT NULL
   AND des.desired_firmware_file_id IS NOT DISTINCT FROM cft.firmware_file_id
ORDER BY ed.cohort_id, ed.device_identifier;

-- name: ListCohortFirmwareStatusesForDevices :many
WITH effective_devices AS (
    SELECT
        c.id AS cohort_id,
        d.org_id AS org_id,
        d.id AS device_id,
        d.device_identifier AS device_identifier,
        COALESCE(dd.manufacturer, '')::text AS manufacturer,
        COALESCE(dd.model, '')::text AS model,
        COALESCE(dd.firmware_version, '')::text AS discovered_firmware_version,
        COALESCE(ds.status::text, '')::text AS device_status
    FROM device d
    JOIN discovered_device dd
        ON dd.id = d.discovered_device_id
       AND dd.org_id = d.org_id
       AND dd.deleted_at IS NULL
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
    LEFT JOIN device_status ds
        ON ds.device_id = d.id
    WHERE d.org_id = sqlc.arg('org_id')
      AND d.deleted_at IS NULL
      AND d.device_identifier = ANY(sqlc.arg('device_identifiers')::text[])
)
SELECT
    ed.cohort_id,
    ed.device_identifier,
    cft.firmware_file_id AS target_firmware_file_id,
    COALESCE(dfs.firmware_version, ed.discovered_firmware_version, '')::text AS current_firmware_version,
    dfs.observed_at AS firmware_observed_at,
    des.state AS enforcement_state,
    des.desired_firmware_file_id AS state_desired_firmware_file_id,
    des.desired_firmware_version AS state_desired_firmware_version,
    des.retry_count AS retry_count,
    des.last_error AS last_error,
    des.last_dispatched_at AS last_dispatched_at,
    des.confirmed_at AS confirmed_at,
    ed.device_status
FROM effective_devices ed
LEFT JOIN cohort_firmware_target cft
    ON cft.cohort_id = ed.cohort_id
   AND cft.org_id = ed.org_id
   AND LOWER(BTRIM(cft.manufacturer)) = LOWER(BTRIM(ed.manufacturer))
   AND LOWER(BTRIM(cft.model)) = LOWER(BTRIM(ed.model))
LEFT JOIN device_firmware_state dfs
    ON dfs.org_id = ed.org_id
   AND dfs.device_identifier = ed.device_identifier
LEFT JOIN device_enforcement_state des
    ON des.org_id = ed.org_id
   AND des.device_identifier = ed.device_identifier
   AND des.dimension = 'firmware'
   AND cft.firmware_file_id IS NOT NULL
   AND des.desired_firmware_file_id IS NOT DISTINCT FROM cft.firmware_file_id
ORDER BY ed.device_identifier;

-- name: ListCohortConfigStatusesForDevices :many
WITH effective_devices AS (
    SELECT
        c.id AS cohort_id,
        c.desired_config_jsonb,
        d.org_id,
        d.device_identifier
    FROM device d
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
    WHERE d.org_id = sqlc.arg('org_id')
      AND d.deleted_at IS NULL
      AND d.device_identifier = ANY(sqlc.arg('device_identifiers')::text[])
)
SELECT
    ed.cohort_id,
    ed.device_identifier,
    'pools'::text AS dimension,
    COALESCE(des.supported, TRUE)::boolean AS supported,
    des.state AS enforcement_state,
    des.retry_count,
    des.last_error,
    des.last_dispatched_at,
    des.confirmed_at,
    dcs.observed_at
FROM effective_devices ed
LEFT JOIN device_enforcement_state des
    ON des.org_id = ed.org_id
   AND des.device_identifier = ed.device_identifier
   AND des.dimension = 'pools'
LEFT JOIN device_config_state dcs
    ON dcs.org_id = ed.org_id
   AND dcs.device_identifier = ed.device_identifier
   AND dcs.dimension = 'pools'
WHERE ed.desired_config_jsonb ? 'pools'
ORDER BY ed.device_identifier;

-- name: ListCohortConfigStatuses :many
WITH effective_devices AS (
    SELECT
        c.id AS cohort_id,
        c.desired_config_jsonb,
        d.org_id,
        d.device_identifier
    FROM device d
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
    WHERE d.org_id = sqlc.arg('org_id')
      AND d.deleted_at IS NULL
      AND c.id = ANY(sqlc.arg('cohort_ids')::bigint[])
)
SELECT
    ed.cohort_id,
    ed.device_identifier,
    'pools'::text AS dimension,
    COALESCE(des.supported, TRUE)::boolean AS supported,
    des.state AS enforcement_state,
    des.retry_count,
    des.last_error,
    des.last_dispatched_at,
    des.confirmed_at,
    dcs.observed_at
FROM effective_devices ed
LEFT JOIN device_enforcement_state des
    ON des.org_id = ed.org_id
   AND des.device_identifier = ed.device_identifier
   AND des.dimension = 'pools'
LEFT JOIN device_config_state dcs
    ON dcs.org_id = ed.org_id
   AND dcs.device_identifier = ed.device_identifier
   AND dcs.dimension = 'pools'
WHERE ed.desired_config_jsonb ? 'pools'
ORDER BY ed.cohort_id, ed.device_identifier;

-- name: ListDeviceIdentifiersForCohortMembership :many
SELECT device_identifier
FROM device
WHERE org_id = sqlc.arg('org_id')
  AND device_identifier = ANY(sqlc.arg('device_identifiers')::text[])
  AND deleted_at IS NULL
ORDER BY device_identifier;

-- name: ListCohortDeviceOwnership :many
SELECT
    cm.device_identifier,
    cm.cohort_id,
    c.owner_user_id,
    c.owner_username
FROM cohort_membership cm
JOIN cohort c ON c.id = cm.cohort_id
WHERE cm.org_id = sqlc.arg('org_id')
  AND cm.device_identifier = ANY(sqlc.arg('device_identifiers')::text[])
  AND c.state = 'active'
  AND c.is_default = FALSE
ORDER BY cm.device_identifier;

-- name: ListActiveOwnedCohortMemberships :many
SELECT
    cm.device_identifier,
    cm.cohort_id,
    c.owner_user_id,
    c.owner_username
FROM cohort_membership cm
JOIN cohort c ON c.id = cm.cohort_id
WHERE cm.org_id = sqlc.arg('org_id')
  AND cm.device_identifier = ANY(sqlc.arg('device_identifiers')::text[])
  AND c.state = 'active'
  AND c.is_default = FALSE
  AND c.owner_user_id IS NOT NULL
ORDER BY cm.device_identifier;

-- name: ResolveEffectiveCohortForDevice :one
SELECT
    c.*,
    CASE
        WHEN c.is_default THEN (
            SELECT COUNT(*)::bigint
            FROM device d_default
            LEFT JOIN cohort_membership cm_default
                ON cm_default.org_id = d_default.org_id
               AND cm_default.device_identifier = d_default.device_identifier
            WHERE d_default.org_id = c.org_id
              AND d_default.deleted_at IS NULL
              AND cm_default.cohort_id IS NULL
        )
        ELSE COALESCE(m.explicit_member_count, 0)::bigint
    END AS explicit_member_count
FROM device d
LEFT JOIN cohort_membership cm
    ON cm.org_id = d.org_id
   AND cm.device_identifier = d.device_identifier
JOIN cohort default_c
    ON default_c.org_id = d.org_id
   AND default_c.is_default = TRUE
JOIN cohort c
    ON c.id = COALESCE(cm.cohort_id, default_c.id)
LEFT JOIN (
    SELECT cohort_id, COUNT(*) AS explicit_member_count
    FROM cohort_membership
    GROUP BY cohort_id
) m ON m.cohort_id = c.id
WHERE d.org_id = sqlc.arg('org_id')
  AND d.device_identifier = sqlc.arg('device_identifier')
  AND d.deleted_at IS NULL;

-- name: ListDefaultCohortDevices :many
SELECT d.device_identifier
FROM device d
JOIN discovered_device dd
    ON dd.id = d.discovered_device_id
   AND dd.org_id = d.org_id
   AND dd.deleted_at IS NULL
LEFT JOIN cohort_membership cm
    ON cm.org_id = d.org_id
   AND cm.device_identifier = d.device_identifier
WHERE d.org_id = sqlc.arg('org_id')
  AND d.deleted_at IS NULL
  AND cm.cohort_id IS NULL
  AND (
    NOT sqlc.arg('product_filter_set')::boolean
    OR LOWER(BTRIM(dd.manufacturer)) = LOWER(BTRIM(sqlc.narg('product')::text))
  )
  AND (
    NOT sqlc.arg('model_filter_set')::boolean
    OR LOWER(BTRIM(dd.model)) = LOWER(BTRIM(sqlc.narg('model')::text))
  )
ORDER BY d.device_identifier
LIMIT sqlc.arg('limit_count')::int;

-- name: ListCohortDevices :many
WITH cohort_devices AS (
    SELECT
        d.device_identifier AS device_identifier,
        COALESCE(
            NULLIF(d.custom_name, ''),
            NULLIF(TRIM(CONCAT_WS(' ', NULLIF(dd.manufacturer, ''), NULLIF(dd.model, ''))), ''),
            d.device_identifier
        )::text AS display_name,
        COALESCE(d.worker_name, '') AS worker_name,
        COALESCE(dd.manufacturer, '') AS manufacturer,
        COALESCE(dd.model, '') AS model,
        COALESCE(dd.ip_address, '') AS ip_address,
        COALESCE(dd.firmware_version, '') AS firmware_version,
        COALESCE(d.serial_number, '') AS serial_number,
        c.*,
        CASE
            WHEN c.is_default THEN (
                SELECT COUNT(*)::bigint
                FROM device d_default
                LEFT JOIN cohort_membership cm_default
                    ON cm_default.org_id = d_default.org_id
                   AND cm_default.device_identifier = d_default.device_identifier
                WHERE d_default.org_id = c.org_id
                  AND d_default.deleted_at IS NULL
                  AND cm_default.cohort_id IS NULL
            )
            ELSE COALESCE(m.explicit_member_count, 0)::bigint
        END AS explicit_member_count
    FROM device d
    JOIN discovered_device dd
        ON dd.id = d.discovered_device_id
       AND dd.org_id = d.org_id
       AND dd.deleted_at IS NULL
    LEFT JOIN cohort_membership cm
        ON cm.org_id = d.org_id
       AND cm.device_identifier = d.device_identifier
    JOIN cohort default_c
        ON default_c.org_id = d.org_id
       AND default_c.is_default = TRUE
    JOIN cohort c
        ON c.id = COALESCE(cm.cohort_id, default_c.id)
    LEFT JOIN (
        SELECT cohort_id, COUNT(*) AS explicit_member_count
        FROM cohort_membership
        GROUP BY cohort_id
    ) m ON m.cohort_id = c.id
    WHERE d.org_id = sqlc.arg('org_id')
      AND d.deleted_at IS NULL
)
SELECT *
FROM cohort_devices
WHERE (
    cardinality(sqlc.arg('assignments')::text[]) = 0
    OR ('available' = ANY(sqlc.arg('assignments')::text[]) AND is_default = TRUE)
    OR ('reserved' = ANY(sqlc.arg('assignments')::text[]) AND is_default = FALSE)
  )
  AND (
    cardinality(sqlc.arg('cohort_ids')::bigint[]) = 0
    OR id = ANY(sqlc.arg('cohort_ids')::bigint[])
  )
  AND (
    cardinality(sqlc.arg('owner_user_ids')::bigint[]) = 0
    OR owner_user_id = ANY(sqlc.arg('owner_user_ids')::bigint[])
    OR (sqlc.arg('include_unowned')::boolean AND owner_user_id IS NULL)
  )
  AND (
    cardinality(sqlc.arg('manufacturers')::text[]) = 0
    OR LOWER(BTRIM(manufacturer)) = ANY(
        SELECT LOWER(BTRIM(value))
        FROM unnest(sqlc.arg('manufacturers')::text[]) AS value
    )
  )
  AND (
    cardinality(sqlc.arg('models')::text[]) = 0
    OR LOWER(BTRIM(model)) = ANY(
        SELECT LOWER(BTRIM(value))
        FROM unnest(sqlc.arg('models')::text[]) AS value
    )
  )
  AND (
    NOT sqlc.arg('search_filter_set')::boolean
    OR display_name ILIKE '%' || sqlc.arg('search')::text || '%'
    OR worker_name ILIKE '%' || sqlc.arg('search')::text || '%'
    OR manufacturer ILIKE '%' || sqlc.arg('search')::text || '%'
    OR model ILIKE '%' || sqlc.arg('search')::text || '%'
    OR ip_address ILIKE '%' || sqlc.arg('search')::text || '%'
    OR serial_number ILIKE '%' || sqlc.arg('search')::text || '%'
    OR device_identifier ILIKE '%' || sqlc.arg('search')::text || '%'
    OR label ILIKE '%' || sqlc.arg('search')::text || '%'
    OR COALESCE(owner_username, '') ILIKE '%' || sqlc.arg('search')::text || '%'
  )
  AND (
    NOT sqlc.arg('cursor_set')::boolean
    OR display_name > sqlc.arg('cursor_display_name')::text
    OR (display_name = sqlc.arg('cursor_display_name')::text AND device_identifier > sqlc.arg('cursor_device_identifier')::text)
  )
ORDER BY display_name ASC, device_identifier ASC
LIMIT sqlc.arg('limit_count')::int;

-- name: CountCohortDevices :one
WITH cohort_devices AS (
    SELECT
        d.device_identifier AS device_identifier,
        COALESCE(
            NULLIF(d.custom_name, ''),
            NULLIF(TRIM(CONCAT_WS(' ', NULLIF(dd.manufacturer, ''), NULLIF(dd.model, ''))), ''),
            d.device_identifier
        )::text AS display_name,
        COALESCE(d.worker_name, '') AS worker_name,
        COALESCE(dd.manufacturer, '') AS manufacturer,
        COALESCE(dd.model, '') AS model,
        COALESCE(dd.ip_address, '') AS ip_address,
        COALESCE(d.serial_number, '') AS serial_number,
        c.*
    FROM device d
    JOIN discovered_device dd
        ON dd.id = d.discovered_device_id
       AND dd.org_id = d.org_id
       AND dd.deleted_at IS NULL
    LEFT JOIN cohort_membership cm
        ON cm.org_id = d.org_id
       AND cm.device_identifier = d.device_identifier
    JOIN cohort default_c
        ON default_c.org_id = d.org_id
       AND default_c.is_default = TRUE
    JOIN cohort c
        ON c.id = COALESCE(cm.cohort_id, default_c.id)
    WHERE d.org_id = sqlc.arg('org_id')
      AND d.deleted_at IS NULL
)
SELECT
    COUNT(*)::bigint AS total_count,
    COUNT(*) FILTER (WHERE is_default = TRUE)::bigint AS available_count,
    COUNT(*) FILTER (WHERE is_default = FALSE)::bigint AS reserved_count
FROM cohort_devices
WHERE (
    cardinality(sqlc.arg('assignments')::text[]) = 0
    OR ('available' = ANY(sqlc.arg('assignments')::text[]) AND is_default = TRUE)
    OR ('reserved' = ANY(sqlc.arg('assignments')::text[]) AND is_default = FALSE)
  )
  AND (
    cardinality(sqlc.arg('cohort_ids')::bigint[]) = 0
    OR id = ANY(sqlc.arg('cohort_ids')::bigint[])
  )
  AND (
    cardinality(sqlc.arg('owner_user_ids')::bigint[]) = 0
    OR owner_user_id = ANY(sqlc.arg('owner_user_ids')::bigint[])
    OR (sqlc.arg('include_unowned')::boolean AND owner_user_id IS NULL)
  )
  AND (
    cardinality(sqlc.arg('manufacturers')::text[]) = 0
    OR LOWER(BTRIM(manufacturer)) = ANY(
        SELECT LOWER(BTRIM(value))
        FROM unnest(sqlc.arg('manufacturers')::text[]) AS value
    )
  )
  AND (
    cardinality(sqlc.arg('models')::text[]) = 0
    OR LOWER(BTRIM(model)) = ANY(
        SELECT LOWER(BTRIM(value))
        FROM unnest(sqlc.arg('models')::text[]) AS value
    )
  )
  AND (
    NOT sqlc.arg('search_filter_set')::boolean
    OR display_name ILIKE '%' || sqlc.arg('search')::text || '%'
    OR worker_name ILIKE '%' || sqlc.arg('search')::text || '%'
    OR manufacturer ILIKE '%' || sqlc.arg('search')::text || '%'
    OR model ILIKE '%' || sqlc.arg('search')::text || '%'
    OR ip_address ILIKE '%' || sqlc.arg('search')::text || '%'
    OR serial_number ILIKE '%' || sqlc.arg('search')::text || '%'
    OR device_identifier ILIKE '%' || sqlc.arg('search')::text || '%'
    OR label ILIKE '%' || sqlc.arg('search')::text || '%'
    OR COALESCE(owner_username, '') ILIKE '%' || sqlc.arg('search')::text || '%'
  );
