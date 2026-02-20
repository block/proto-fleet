-- name: GetOpenErrorByDedupKey :one
-- Finds an open error (closed_at IS NULL) matching the deduplication key.
-- Used to determine if an upsert should update an existing error or insert a new one.
-- PostgreSQL uses IS NOT DISTINCT FROM for NULL-safe comparison (MySQL uses <=>)
SELECT * FROM errors
WHERE org_id = $1
  AND device_id = $2
  AND miner_error = $3
  AND component_id IS NOT DISTINCT FROM $4
  AND component_type IS NOT DISTINCT FROM $5
  AND closed_at IS NULL
LIMIT 1;

-- name: InsertError :one
-- Inserts a new error record with all fields.
INSERT INTO errors (
    error_id, org_id, device_id, miner_error, severity, summary, impact,
    cause_summary, recommended_action, first_seen_at, last_seen_at,
    component_id, component_type, vendor_code, firmware, extra, closed_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
RETURNING id;

-- name: UpdateOpenError :exec
-- Updates mutable fields on an existing open error.
-- Only updates if closed_at IS NULL to prevent updating closed errors.
-- Can also close the error by setting closed_at.
UPDATE errors SET
    last_seen_at = $1,
    severity = $2,
    summary = $3,
    impact = $4,
    cause_summary = $5,
    recommended_action = $6,
    vendor_code = $7,
    firmware = $8,
    extra = $9,
    closed_at = $10
WHERE id = $11 AND closed_at IS NULL;

-- name: GetErrorByID :one
-- Fetches an error by internal ID, scoped to organization.
SELECT * FROM errors WHERE id = $1 AND org_id = $2;

-- name: GetErrorByErrorID :one
-- Fetches an error by external ULID with device_identifier, scoped to organization.
SELECT
    e.*,
    d.device_identifier
FROM errors e
JOIN device d ON e.device_id = d.id AND e.org_id = d.org_id AND d.deleted_at IS NULL
WHERE e.error_id = $1 AND e.org_id = $2;

-- name: GetDeviceIDByIdentifier :one
-- Resolves device_identifier to internal device_id.
SELECT id FROM device WHERE device_identifier = $1 AND org_id = $2 AND deleted_at IS NULL;

-- ============================================================================
-- Query Errors (AND Filter Logic)
-- ============================================================================

-- name: QueryErrors :many
-- Queries errors with AND filter logic where all provided filter criteria must match.
-- Time range and include_closed are always applied as base filters.
-- Uses cursor-based pagination with (severity, last_seen_at, error_id) ordering.
SELECT
    e.id,
    e.error_id,
    e.org_id,
    e.miner_error,
    e.severity,
    e.summary,
    e.impact,
    e.cause_summary,
    e.recommended_action,
    e.first_seen_at,
    e.last_seen_at,
    e.closed_at,
    e.device_id,
    e.component_id,
    e.component_type,
    e.vendor_code,
    e.firmware,
    e.extra,
    e.created_at,
    e.updated_at,
    d.device_identifier,
    dd.model as device_type
FROM errors e
JOIN device d ON e.device_id = d.id AND d.deleted_at IS NULL
JOIN discovered_device dd ON d.discovered_device_id = dd.id
WHERE e.org_id = sqlc.arg('org_id')
    -- Base filters (always AND)
    AND (sqlc.narg('time_from')::timestamptz IS NULL OR e.last_seen_at >= sqlc.narg('time_from')::timestamptz)
    AND (sqlc.narg('time_to')::timestamptz IS NULL OR e.last_seen_at <= sqlc.narg('time_to')::timestamptz)
    AND (sqlc.arg('include_closed')::boolean = TRUE OR e.closed_at IS NULL)
    -- Filter criteria (AND logic): all provided filters must match
    AND (sqlc.narg('device_filter')::text IS NULL OR d.device_identifier = ANY(sqlc.arg('device_identifiers')::text[]))
    AND (sqlc.narg('device_type_filter')::text IS NULL OR dd.model = ANY(sqlc.arg('device_types')::text[]))
    AND (sqlc.narg('severity_filter')::text IS NULL OR e.severity = ANY(sqlc.arg('severities')::int[]))
    AND (sqlc.narg('miner_error_filter')::text IS NULL OR e.miner_error = ANY(sqlc.arg('miner_errors')::int[]))
    AND (sqlc.narg('component_type_filter')::text IS NULL OR e.component_type = ANY(sqlc.arg('component_types')::int[]))
    AND (sqlc.narg('component_id_filter')::text IS NULL OR e.component_id = ANY(sqlc.arg('component_ids')::text[]))
    -- Cursor pagination: skip rows before cursor position
    AND (
        sqlc.narg('cursor_severity')::int IS NULL
        OR e.severity > sqlc.narg('cursor_severity')::int
        OR (e.severity = sqlc.narg('cursor_severity')::int AND e.last_seen_at < sqlc.narg('cursor_last_seen')::timestamptz)
        OR (e.severity = sqlc.narg('cursor_severity')::int AND e.last_seen_at = sqlc.narg('cursor_last_seen')::timestamptz AND e.error_id < sqlc.narg('cursor_error_id')::text)
    )
ORDER BY e.severity ASC, e.last_seen_at DESC, e.error_id DESC
LIMIT $1;

-- name: CountErrors :one
-- Counts errors with AND filter logic (same logic as QueryErrors without pagination).
SELECT COUNT(*) as total
FROM errors e
JOIN device d ON e.device_id = d.id AND d.deleted_at IS NULL
JOIN discovered_device dd ON d.discovered_device_id = dd.id
WHERE e.org_id = sqlc.arg('org_id')
    AND (sqlc.narg('time_from')::timestamptz IS NULL OR e.last_seen_at >= sqlc.narg('time_from')::timestamptz)
    AND (sqlc.narg('time_to')::timestamptz IS NULL OR e.last_seen_at <= sqlc.narg('time_to')::timestamptz)
    AND (sqlc.arg('include_closed')::boolean = TRUE OR e.closed_at IS NULL)
    -- Filter criteria (AND logic): all provided filters must match
    AND (sqlc.narg('device_filter')::text IS NULL OR d.device_identifier = ANY(sqlc.arg('device_identifiers')::text[]))
    AND (sqlc.narg('device_type_filter')::text IS NULL OR dd.model = ANY(sqlc.arg('device_types')::text[]))
    AND (sqlc.narg('severity_filter')::text IS NULL OR e.severity = ANY(sqlc.arg('severities')::int[]))
    AND (sqlc.narg('miner_error_filter')::text IS NULL OR e.miner_error = ANY(sqlc.arg('miner_errors')::int[]))
    AND (sqlc.narg('component_type_filter')::text IS NULL OR e.component_type = ANY(sqlc.arg('component_types')::int[]))
    AND (sqlc.narg('component_id_filter')::text IS NULL OR e.component_id = ANY(sqlc.arg('component_ids')::text[]));

-- ============================================================================
-- Device-Based Pagination Queries
-- ============================================================================

-- name: QueryDeviceIDsWithErrors :many
-- Gets distinct device IDs that have errors, sorted by worst severity then device_id.
-- Uses cursor-based pagination on device_id for ResultViewDevice pagination.
-- Returns both device_id (for keyset pagination) and device_identifier (for re-filtering).
SELECT
    e.device_id,
    d.device_identifier,
    MIN(e.severity) as worst_severity
FROM errors e
JOIN device d ON e.device_id = d.id AND d.deleted_at IS NULL
JOIN discovered_device dd ON d.discovered_device_id = dd.id
WHERE e.org_id = sqlc.arg('org_id')
    AND (sqlc.narg('time_from')::timestamptz IS NULL OR e.last_seen_at >= sqlc.narg('time_from')::timestamptz)
    AND (sqlc.narg('time_to')::timestamptz IS NULL OR e.last_seen_at <= sqlc.narg('time_to')::timestamptz)
    AND (sqlc.arg('include_closed')::boolean = TRUE OR e.closed_at IS NULL)
    -- Filter criteria (AND logic): all provided filters must match
    AND (sqlc.narg('device_filter')::text IS NULL OR d.device_identifier = ANY(sqlc.arg('device_identifiers')::text[]))
    AND (sqlc.narg('device_type_filter')::text IS NULL OR dd.model = ANY(sqlc.arg('device_types')::text[]))
    AND (sqlc.narg('severity_filter')::text IS NULL OR e.severity = ANY(sqlc.arg('severities')::int[]))
    AND (sqlc.narg('miner_error_filter')::text IS NULL OR e.miner_error = ANY(sqlc.arg('miner_errors')::int[]))
    AND (sqlc.narg('component_type_filter')::text IS NULL OR e.component_type = ANY(sqlc.arg('component_types')::int[]))
    AND (sqlc.narg('component_id_filter')::text IS NULL OR e.component_id = ANY(sqlc.arg('component_ids')::text[]))
    -- Device cursor: keyset pagination using (worst_severity, device_id) compound key
    -- Must skip rows where (worst_severity, device_id) <= cursor position in sort order
GROUP BY e.device_id, d.device_identifier
HAVING (
    sqlc.narg('cursor_severity')::int IS NULL
    OR MIN(e.severity) > sqlc.narg('cursor_severity')::int
    OR (MIN(e.severity) = sqlc.narg('cursor_severity')::int AND e.device_id > sqlc.narg('cursor_device_id')::bigint)
)
ORDER BY worst_severity ASC, e.device_id ASC
LIMIT $1;

-- name: CountDevicesWithErrors :one
-- Counts distinct devices that have errors matching filter criteria.
SELECT COUNT(DISTINCT e.device_id) as total
FROM errors e
JOIN device d ON e.device_id = d.id AND d.deleted_at IS NULL
JOIN discovered_device dd ON d.discovered_device_id = dd.id
WHERE e.org_id = sqlc.arg('org_id')
    AND (sqlc.narg('time_from')::timestamptz IS NULL OR e.last_seen_at >= sqlc.narg('time_from')::timestamptz)
    AND (sqlc.narg('time_to')::timestamptz IS NULL OR e.last_seen_at <= sqlc.narg('time_to')::timestamptz)
    AND (sqlc.arg('include_closed')::boolean = TRUE OR e.closed_at IS NULL)
    -- Filter criteria (AND logic): all provided filters must match
    AND (sqlc.narg('device_filter')::text IS NULL OR d.device_identifier = ANY(sqlc.arg('device_identifiers')::text[]))
    AND (sqlc.narg('device_type_filter')::text IS NULL OR dd.model = ANY(sqlc.arg('device_types')::text[]))
    AND (sqlc.narg('severity_filter')::text IS NULL OR e.severity = ANY(sqlc.arg('severities')::int[]))
    AND (sqlc.narg('miner_error_filter')::text IS NULL OR e.miner_error = ANY(sqlc.arg('miner_errors')::int[]))
    AND (sqlc.narg('component_type_filter')::text IS NULL OR e.component_type = ANY(sqlc.arg('component_types')::int[]))
    AND (sqlc.narg('component_id_filter')::text IS NULL OR e.component_id = ANY(sqlc.arg('component_ids')::text[]));

-- ============================================================================
-- Component-Based Pagination Queries
-- ============================================================================

-- name: QueryComponentKeysWithErrors :many
-- Gets distinct (device_id, component_type, component_id) tuples that have errors, sorted by worst severity.
-- Uses cursor-based pagination on (device_id, component_type, component_id) for ResultViewComponent pagination.
-- Returns device_identifier (for re-filtering) alongside device_id (for keyset pagination).
SELECT
    e.device_id,
    d.device_identifier,
    e.component_type,
    e.component_id,
    MIN(e.severity) as worst_severity
FROM errors e
JOIN device d ON e.device_id = d.id AND d.deleted_at IS NULL
JOIN discovered_device dd ON d.discovered_device_id = dd.id
WHERE e.org_id = sqlc.arg('org_id')
    AND (sqlc.narg('time_from')::timestamptz IS NULL OR e.last_seen_at >= sqlc.narg('time_from')::timestamptz)
    AND (sqlc.narg('time_to')::timestamptz IS NULL OR e.last_seen_at <= sqlc.narg('time_to')::timestamptz)
    AND (sqlc.arg('include_closed')::boolean = TRUE OR e.closed_at IS NULL)
    -- Filter criteria (AND logic): all provided filters must match
    AND (sqlc.narg('device_filter')::text IS NULL OR d.device_identifier = ANY(sqlc.arg('device_identifiers')::text[]))
    AND (sqlc.narg('device_type_filter')::text IS NULL OR dd.model = ANY(sqlc.arg('device_types')::text[]))
    AND (sqlc.narg('severity_filter')::text IS NULL OR e.severity = ANY(sqlc.arg('severities')::int[]))
    AND (sqlc.narg('miner_error_filter')::text IS NULL OR e.miner_error = ANY(sqlc.arg('miner_errors')::int[]))
    AND (sqlc.narg('component_type_filter')::text IS NULL OR e.component_type = ANY(sqlc.arg('component_types')::int[]))
    AND (sqlc.narg('component_id_filter')::text IS NULL OR e.component_id = ANY(sqlc.arg('component_ids')::text[]))
    -- Component cursor: keyset pagination using (worst_severity, device_id, component_type, component_id) compound key
    -- Must skip rows where (worst_severity, device_id, component_type, component_id) <= cursor position in sort order
GROUP BY e.device_id, d.device_identifier, e.component_type, e.component_id
HAVING (
    sqlc.narg('cursor_severity')::int IS NULL
    OR MIN(e.severity) > sqlc.narg('cursor_severity')::int
    OR (MIN(e.severity) = sqlc.narg('cursor_severity')::int AND e.device_id > sqlc.narg('cursor_device_id')::bigint)
    OR (MIN(e.severity) = sqlc.narg('cursor_severity')::int AND e.device_id = sqlc.narg('cursor_device_id')::bigint AND e.component_type > sqlc.narg('cursor_component_type')::int)
    OR (MIN(e.severity) = sqlc.narg('cursor_severity')::int AND e.device_id = sqlc.narg('cursor_device_id')::bigint AND e.component_type = sqlc.narg('cursor_component_type')::int AND (
        e.component_id > sqlc.narg('cursor_component_id')::text
        OR (sqlc.narg('cursor_component_id')::text IS NULL AND e.component_id IS NOT NULL)
    ))
)
ORDER BY worst_severity ASC, e.device_id ASC, e.component_type ASC, e.component_id ASC
LIMIT $1;

-- name: CountComponentsWithErrors :one
-- Counts distinct (device_id, component_type, component_id) tuples that have errors matching filter criteria.
SELECT COUNT(*) as total FROM (
    SELECT DISTINCT e.device_id, e.component_type, e.component_id
    FROM errors e
    JOIN device d ON e.device_id = d.id AND d.deleted_at IS NULL
    JOIN discovered_device dd ON d.discovered_device_id = dd.id
    WHERE e.org_id = sqlc.arg('org_id')
        AND (sqlc.narg('time_from')::timestamptz IS NULL OR e.last_seen_at >= sqlc.narg('time_from')::timestamptz)
        AND (sqlc.narg('time_to')::timestamptz IS NULL OR e.last_seen_at <= sqlc.narg('time_to')::timestamptz)
        AND (sqlc.arg('include_closed')::boolean = TRUE OR e.closed_at IS NULL)
        -- Filter criteria (AND logic): all provided filters must match
        AND (sqlc.narg('device_filter')::text IS NULL OR d.device_identifier = ANY(sqlc.arg('device_identifiers')::text[]))
        AND (sqlc.narg('device_type_filter')::text IS NULL OR dd.model = ANY(sqlc.arg('device_types')::text[]))
        AND (sqlc.narg('severity_filter')::text IS NULL OR e.severity = ANY(sqlc.arg('severities')::int[]))
        AND (sqlc.narg('miner_error_filter')::text IS NULL OR e.miner_error = ANY(sqlc.arg('miner_errors')::int[]))
        AND (sqlc.narg('component_type_filter')::text IS NULL OR e.component_type = ANY(sqlc.arg('component_types')::int[]))
        AND (sqlc.narg('component_id_filter')::text IS NULL OR e.component_id = ANY(sqlc.arg('component_ids')::text[]))
) as component_count;

-- ============================================================================
-- Error Lifecycle Management
-- ============================================================================

-- name: CloseStaleErrors :execresult
-- Closes stale errors only when device was successfully polled after the staleness cutoff time.
-- This ensures we have confirmed the error is absent from a recent poll.
UPDATE errors
SET closed_at = CURRENT_TIMESTAMP
WHERE closed_at IS NULL
  AND last_seen_at < sqlc.arg('cutoff_time')
  AND EXISTS (
    SELECT 1
    FROM device_status ds
    WHERE ds.device_id = errors.device_id
      AND ds.status_timestamp >= sqlc.arg('status_cutoff_time')
  );
