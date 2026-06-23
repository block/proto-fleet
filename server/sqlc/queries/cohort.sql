-- name: CreateCohort :one
INSERT INTO cohort (
    org_id,
    label,
    is_default,
    owner_user_id,
    owner_username,
    expires_at,
    desired_firmware_file_id,
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
    sqlc.narg('desired_firmware_file_id'),
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
    COALESCE(m.explicit_member_count, 0)::bigint AS explicit_member_count
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
    COALESCE(m.explicit_member_count, 0)::bigint AS explicit_member_count
FROM cohort c
LEFT JOIN (
    SELECT cohort_id, COUNT(*) AS explicit_member_count
    FROM cohort_membership
    GROUP BY cohort_id
) m ON m.cohort_id = c.id
WHERE c.org_id = sqlc.arg('org_id')
  AND (sqlc.arg('include_released')::boolean OR c.state = 'active')
ORDER BY c.is_default DESC, c.updated_at DESC, c.id DESC;

-- name: ListCohortsByOwner :many
SELECT
    c.*,
    COALESCE(m.explicit_member_count, 0)::bigint AS explicit_member_count
FROM cohort c
LEFT JOIN (
    SELECT cohort_id, COUNT(*) AS explicit_member_count
    FROM cohort_membership
    GROUP BY cohort_id
) m ON m.cohort_id = c.id
WHERE c.org_id = sqlc.arg('org_id')
  AND c.owner_user_id = sqlc.arg('owner_user_id')
  AND (sqlc.arg('include_released')::boolean OR c.state = 'active')
ORDER BY c.updated_at DESC, c.id DESC;

-- name: ReleaseCohort :one
UPDATE cohort
SET state = 'released',
    updated_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg('id')
  AND org_id = sqlc.arg('org_id')
  AND is_default = FALSE
RETURNING *;

-- name: InsertCohortMembership :exec
INSERT INTO cohort_membership (
    cohort_id,
    org_id,
    device_identifier,
    site_id
) VALUES (
    sqlc.arg('cohort_id'),
    sqlc.arg('org_id'),
    sqlc.arg('device_identifier'),
    sqlc.narg('site_id')
);

-- name: BulkInsertCohortMemberships :execrows
INSERT INTO cohort_membership (
    cohort_id,
    org_id,
    device_identifier,
    site_id
)
SELECT
    sqlc.arg('cohort_id'),
    sqlc.arg('org_id'),
    payload.device_identifier,
    payload.site_id
FROM jsonb_to_recordset(sqlc.arg('members_jsonb')::jsonb)
    AS payload(device_identifier text, site_id bigint);

-- name: DeleteCohortMembershipsByCohort :execrows
DELETE FROM cohort_membership
WHERE cohort_id = sqlc.arg('cohort_id')
  AND org_id = sqlc.arg('org_id');

-- name: DeleteCohortMemberships :execrows
DELETE FROM cohort_membership
WHERE cohort_id = sqlc.arg('cohort_id')
  AND org_id = sqlc.arg('org_id')
  AND device_identifier = ANY(sqlc.arg('device_identifiers')::text[]);

-- name: ListCohortMembers :many
SELECT *
FROM cohort_membership
WHERE cohort_id = sqlc.arg('cohort_id')
  AND org_id = sqlc.arg('org_id')
ORDER BY added_at, device_identifier;

-- name: ResolveEffectiveCohortForDevice :one
SELECT
    c.*,
    COALESCE(m.explicit_member_count, 0)::bigint AS explicit_member_count
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
SELECT d.device_identifier, d.site_id
FROM device d
LEFT JOIN cohort_membership cm
    ON cm.org_id = d.org_id
   AND cm.device_identifier = d.device_identifier
WHERE d.org_id = sqlc.arg('org_id')
  AND d.deleted_at IS NULL
  AND cm.cohort_id IS NULL
ORDER BY d.device_identifier;
