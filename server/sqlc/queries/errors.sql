-- name: GetOpenErrorByDedupKey :one
-- Finds an open error (closed_at IS NULL) matching the deduplication key.
-- Used to determine if an upsert should update an existing error or insert a new one.
SELECT * FROM errors
WHERE org_id = ?
  AND device_id = ?
  AND miner_error = ?
  AND component_id <=> ?
  AND component_type <=> ?
  AND closed_at IS NULL
LIMIT 1;

-- name: InsertError :execresult
-- Inserts a new error record with all fields.
INSERT INTO errors (
    error_id, org_id, device_id, miner_error, severity, summary, impact,
    cause_summary, recommended_action, first_seen_at, last_seen_at,
    component_id, component_type, vendor_code, firmware, extra, closed_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: UpdateOpenError :exec
-- Updates mutable fields on an existing open error.
-- Only updates if closed_at IS NULL to prevent updating closed errors.
-- Can also close the error by setting closed_at.
UPDATE errors SET
    last_seen_at = ?,
    severity = ?,
    summary = ?,
    impact = ?,
    cause_summary = ?,
    recommended_action = ?,
    vendor_code = ?,
    firmware = ?,
    extra = ?,
    closed_at = ?
WHERE id = ? AND closed_at IS NULL;

-- name: GetErrorByID :one
-- Fetches an error by internal ID, scoped to organization.
SELECT * FROM errors WHERE id = ? AND org_id = ?;

-- name: GetErrorByErrorID :one
-- Fetches an error by external ULID with device_identifier, scoped to organization.
SELECT
    e.*,
    d.device_identifier
FROM errors e
JOIN device d ON e.device_id = d.id AND e.org_id = d.org_id
WHERE e.error_id = ? AND e.org_id = ?;

-- name: GetDeviceIDByIdentifier :one
-- Resolves device_identifier to internal device_id.
SELECT id FROM device WHERE device_identifier = ? AND org_id = ?;

-- ============================================================================
-- Query Errors (AND Filter Logic)
-- ============================================================================

-- name: QueryErrors :many
-- Queries errors with AND filter logic where all provided filter criteria must match.
-- Time range and include_closed are always applied as base filters.
-- Uses cursor-based pagination with (severity, last_seen_at, error_id) ordering.
-- TODO(DASH-1048): Add CASE statement to support OR logic via use_or_logic parameter.
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
JOIN device d ON e.device_id = d.id
JOIN discovered_device dd ON d.discovered_device_id = dd.id
WHERE e.org_id = sqlc.arg('org_id')
    -- Base filters (always AND)
    AND (sqlc.narg('time_from') IS NULL OR e.last_seen_at >= sqlc.narg('time_from'))
    AND (sqlc.narg('time_to') IS NULL OR e.last_seen_at <= sqlc.narg('time_to'))
    AND (sqlc.arg('include_closed') = TRUE OR e.closed_at IS NULL)
    -- Filter criteria (AND logic): all provided filters must match
    AND (sqlc.narg('device_filter') IS NULL OR d.device_identifier IN (sqlc.slice('device_identifiers')))
    AND (sqlc.narg('device_type_filter') IS NULL OR dd.model IN (sqlc.slice('device_types')))
    AND (sqlc.narg('severity_filter') IS NULL OR e.severity IN (sqlc.slice('severities')))
    AND (sqlc.narg('miner_error_filter') IS NULL OR e.miner_error IN (sqlc.slice('miner_errors')))
    AND (sqlc.narg('component_type_filter') IS NULL OR e.component_type IN (sqlc.slice('component_types')))
    AND (sqlc.narg('component_id_filter') IS NULL OR e.component_id IN (sqlc.slice('component_ids')))
    -- Cursor pagination: skip rows before cursor position
    AND (
        sqlc.narg('cursor_severity') IS NULL
        OR e.severity > sqlc.narg('cursor_severity')
        OR (e.severity = sqlc.narg('cursor_severity') AND e.last_seen_at < sqlc.narg('cursor_last_seen'))
        OR (e.severity = sqlc.narg('cursor_severity') AND e.last_seen_at = sqlc.narg('cursor_last_seen') AND e.error_id < sqlc.narg('cursor_error_id'))
    )
ORDER BY e.severity ASC, e.last_seen_at DESC, e.error_id DESC
LIMIT ?;

-- name: CountErrors :one
-- Counts errors with AND filter logic (same logic as QueryErrors without pagination).
-- TODO(DASH-1048): Add CASE statement to support OR logic via use_or_logic parameter.
SELECT COUNT(*) as total
FROM errors e
JOIN device d ON e.device_id = d.id
JOIN discovered_device dd ON d.discovered_device_id = dd.id
WHERE e.org_id = sqlc.arg('org_id')
    AND (sqlc.narg('time_from') IS NULL OR e.last_seen_at >= sqlc.narg('time_from'))
    AND (sqlc.narg('time_to') IS NULL OR e.last_seen_at <= sqlc.narg('time_to'))
    AND (sqlc.arg('include_closed') = TRUE OR e.closed_at IS NULL)
    -- Filter criteria (AND logic): all provided filters must match
    AND (sqlc.narg('device_filter') IS NULL OR d.device_identifier IN (sqlc.slice('device_identifiers')))
    AND (sqlc.narg('device_type_filter') IS NULL OR dd.model IN (sqlc.slice('device_types')))
    AND (sqlc.narg('severity_filter') IS NULL OR e.severity IN (sqlc.slice('severities')))
    AND (sqlc.narg('miner_error_filter') IS NULL OR e.miner_error IN (sqlc.slice('miner_errors')))
    AND (sqlc.narg('component_type_filter') IS NULL OR e.component_type IN (sqlc.slice('component_types')))
    AND (sqlc.narg('component_id_filter') IS NULL OR e.component_id IN (sqlc.slice('component_ids')));

-- ============================================================================
-- Device-Based Pagination Queries
-- ============================================================================

-- name: QueryDeviceIDsWithErrors :many
-- Gets distinct device IDs that have errors, sorted by worst severity then device_id.
-- Uses cursor-based pagination on device_id for ResultViewDevice pagination.
-- Returns both device_id (for keyset pagination) and device_identifier (for re-filtering).
-- TODO(DASH-1048): Add CASE statement to support OR logic via use_or_logic parameter.
SELECT
    e.device_id,
    d.device_identifier,
    MIN(e.severity) as worst_severity
FROM errors e
JOIN device d ON e.device_id = d.id
JOIN discovered_device dd ON d.discovered_device_id = dd.id
WHERE e.org_id = sqlc.arg('org_id')
    AND (sqlc.narg('time_from') IS NULL OR e.last_seen_at >= sqlc.narg('time_from'))
    AND (sqlc.narg('time_to') IS NULL OR e.last_seen_at <= sqlc.narg('time_to'))
    AND (sqlc.arg('include_closed') = TRUE OR e.closed_at IS NULL)
    -- Filter criteria (AND logic): all provided filters must match
    AND (sqlc.narg('device_filter') IS NULL OR d.device_identifier IN (sqlc.slice('device_identifiers')))
    AND (sqlc.narg('device_type_filter') IS NULL OR dd.model IN (sqlc.slice('device_types')))
    AND (sqlc.narg('severity_filter') IS NULL OR e.severity IN (sqlc.slice('severities')))
    AND (sqlc.narg('miner_error_filter') IS NULL OR e.miner_error IN (sqlc.slice('miner_errors')))
    AND (sqlc.narg('component_type_filter') IS NULL OR e.component_type IN (sqlc.slice('component_types')))
    AND (sqlc.narg('component_id_filter') IS NULL OR e.component_id IN (sqlc.slice('component_ids')))
    -- Device cursor: keyset pagination using (worst_severity, device_id) compound key
    -- Must skip rows where (worst_severity, device_id) <= cursor position in sort order
GROUP BY e.device_id, d.device_identifier
HAVING (
    sqlc.narg('cursor_severity') IS NULL
    OR MIN(e.severity) > sqlc.narg('cursor_severity')
    OR (MIN(e.severity) = sqlc.narg('cursor_severity') AND e.device_id > sqlc.narg('cursor_device_id'))
)
ORDER BY worst_severity ASC, e.device_id ASC
LIMIT ?;

-- name: CountDevicesWithErrors :one
-- Counts distinct devices that have errors matching filter criteria.
-- TODO(DASH-1048): Add CASE statement to support OR logic via use_or_logic parameter.
SELECT COUNT(DISTINCT e.device_id) as total
FROM errors e
JOIN device d ON e.device_id = d.id
JOIN discovered_device dd ON d.discovered_device_id = dd.id
WHERE e.org_id = sqlc.arg('org_id')
    AND (sqlc.narg('time_from') IS NULL OR e.last_seen_at >= sqlc.narg('time_from'))
    AND (sqlc.narg('time_to') IS NULL OR e.last_seen_at <= sqlc.narg('time_to'))
    AND (sqlc.arg('include_closed') = TRUE OR e.closed_at IS NULL)
    -- Filter criteria (AND logic): all provided filters must match
    AND (sqlc.narg('device_filter') IS NULL OR d.device_identifier IN (sqlc.slice('device_identifiers')))
    AND (sqlc.narg('device_type_filter') IS NULL OR dd.model IN (sqlc.slice('device_types')))
    AND (sqlc.narg('severity_filter') IS NULL OR e.severity IN (sqlc.slice('severities')))
    AND (sqlc.narg('miner_error_filter') IS NULL OR e.miner_error IN (sqlc.slice('miner_errors')))
    AND (sqlc.narg('component_type_filter') IS NULL OR e.component_type IN (sqlc.slice('component_types')))
    AND (sqlc.narg('component_id_filter') IS NULL OR e.component_id IN (sqlc.slice('component_ids')));

-- ============================================================================
-- Component-Based Pagination Queries
-- ============================================================================

-- name: QueryComponentKeysWithErrors :many
-- Gets distinct (device_id, component_type, component_id) tuples that have errors, sorted by worst severity.
-- Uses cursor-based pagination on (device_id, component_type, component_id) for ResultViewComponent pagination.
-- Returns device_identifier (for re-filtering) alongside device_id (for keyset pagination).
-- TODO(DASH-1048): Add CASE statement to support OR logic via use_or_logic parameter.
SELECT
    e.device_id,
    d.device_identifier,
    e.component_type,
    e.component_id,
    MIN(e.severity) as worst_severity
FROM errors e
JOIN device d ON e.device_id = d.id
JOIN discovered_device dd ON d.discovered_device_id = dd.id
WHERE e.org_id = sqlc.arg('org_id')
    AND (sqlc.narg('time_from') IS NULL OR e.last_seen_at >= sqlc.narg('time_from'))
    AND (sqlc.narg('time_to') IS NULL OR e.last_seen_at <= sqlc.narg('time_to'))
    AND (sqlc.arg('include_closed') = TRUE OR e.closed_at IS NULL)
    -- Filter criteria (AND logic): all provided filters must match
    AND (sqlc.narg('device_filter') IS NULL OR d.device_identifier IN (sqlc.slice('device_identifiers')))
    AND (sqlc.narg('device_type_filter') IS NULL OR dd.model IN (sqlc.slice('device_types')))
    AND (sqlc.narg('severity_filter') IS NULL OR e.severity IN (sqlc.slice('severities')))
    AND (sqlc.narg('miner_error_filter') IS NULL OR e.miner_error IN (sqlc.slice('miner_errors')))
    AND (sqlc.narg('component_type_filter') IS NULL OR e.component_type IN (sqlc.slice('component_types')))
    AND (sqlc.narg('component_id_filter') IS NULL OR e.component_id IN (sqlc.slice('component_ids')))
    -- Component cursor: keyset pagination using (worst_severity, device_id, component_type, component_id) compound key
    -- Must skip rows where (worst_severity, device_id, component_type, component_id) <= cursor position in sort order
GROUP BY e.device_id, d.device_identifier, e.component_type, e.component_id
HAVING (
    sqlc.narg('cursor_severity') IS NULL
    OR MIN(e.severity) > sqlc.narg('cursor_severity')
    OR (MIN(e.severity) = sqlc.narg('cursor_severity') AND e.device_id > sqlc.narg('cursor_device_id'))
    OR (MIN(e.severity) = sqlc.narg('cursor_severity') AND e.device_id = sqlc.narg('cursor_device_id') AND e.component_type > sqlc.narg('cursor_component_type'))
    OR (MIN(e.severity) = sqlc.narg('cursor_severity') AND e.device_id = sqlc.narg('cursor_device_id') AND e.component_type = sqlc.narg('cursor_component_type') AND (
        e.component_id > sqlc.narg('cursor_component_id')
        OR (sqlc.narg('cursor_component_id') IS NULL AND e.component_id IS NOT NULL)
    ))
)
ORDER BY worst_severity ASC, e.device_id ASC, e.component_type ASC, e.component_id ASC
LIMIT ?;

-- name: CountComponentsWithErrors :one
-- Counts distinct (device_id, component_type, component_id) tuples that have errors matching filter criteria.
-- TODO(DASH-1048): Add CASE statement to support OR logic via use_or_logic parameter.
SELECT COUNT(*) as total FROM (
    SELECT DISTINCT e.device_id, e.component_type, e.component_id
    FROM errors e
    JOIN device d ON e.device_id = d.id
    JOIN discovered_device dd ON d.discovered_device_id = dd.id
    WHERE e.org_id = sqlc.arg('org_id')
        AND (sqlc.narg('time_from') IS NULL OR e.last_seen_at >= sqlc.narg('time_from'))
        AND (sqlc.narg('time_to') IS NULL OR e.last_seen_at <= sqlc.narg('time_to'))
        AND (sqlc.arg('include_closed') = TRUE OR e.closed_at IS NULL)
        -- Filter criteria (AND logic): all provided filters must match
        AND (sqlc.narg('device_filter') IS NULL OR d.device_identifier IN (sqlc.slice('device_identifiers')))
        AND (sqlc.narg('device_type_filter') IS NULL OR dd.model IN (sqlc.slice('device_types')))
        AND (sqlc.narg('severity_filter') IS NULL OR e.severity IN (sqlc.slice('severities')))
        AND (sqlc.narg('miner_error_filter') IS NULL OR e.miner_error IN (sqlc.slice('miner_errors')))
        AND (sqlc.narg('component_type_filter') IS NULL OR e.component_type IN (sqlc.slice('component_types')))
        AND (sqlc.narg('component_id_filter') IS NULL OR e.component_id IN (sqlc.slice('component_ids')))
) as component_count;

-- ============================================================================
-- Error Lifecycle Management
-- ============================================================================

-- name: CloseStaleErrors :execresult
-- Closes stale errors only when device was successfully polled after the staleness cutoff time.
-- This ensures we have confirmed the error is absent from a recent poll.
UPDATE errors
SET closed_at = CURRENT_TIMESTAMP(6)
WHERE closed_at IS NULL
  AND last_seen_at < sqlc.arg('cutoff_time')
  AND EXISTS (
    SELECT 1
    FROM device_status ds
    WHERE ds.device_id = errors.device_id
      AND ds.status_timestamp >= sqlc.arg('status_cutoff_time')
  );
