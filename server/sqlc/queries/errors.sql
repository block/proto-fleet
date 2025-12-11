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
