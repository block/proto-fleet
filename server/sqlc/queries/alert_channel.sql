-- name: InsertAlertChannel :one
INSERT INTO alert_channel (
    org_id,
    name,
    kind,
    encrypted_config,
    validation_state
) VALUES (
    sqlc.arg('org_id'),
    sqlc.arg('name'),
    sqlc.arg('kind'),
    sqlc.arg('encrypted_config'),
    sqlc.arg('validation_state')
)
RETURNING *;

-- name: UpdateAlertChannel :one
UPDATE alert_channel
SET name = sqlc.arg('name'),
    kind = sqlc.arg('kind'),
    encrypted_config = sqlc.arg('encrypted_config'),
    validation_state = sqlc.arg('validation_state'),
    validated_at = sqlc.narg('validated_at'),
    validation_error = sqlc.arg('validation_error'),
    updated_at = now()
WHERE id = sqlc.arg('id')
  AND org_id = sqlc.arg('org_id')
  AND deleted_at IS NULL
RETURNING *;

-- name: GetAlertChannel :one
SELECT * FROM alert_channel
WHERE id = sqlc.arg('id')
  AND org_id = sqlc.arg('org_id')
  AND deleted_at IS NULL;

-- name: GetAlertChannelByName :one
SELECT * FROM alert_channel
WHERE org_id = sqlc.arg('org_id')
  AND name = sqlc.arg('name')
  AND deleted_at IS NULL;

-- name: ListAlertChannels :many
SELECT * FROM alert_channel
WHERE org_id = sqlc.arg('org_id')
  AND deleted_at IS NULL
ORDER BY created_at, id;

-- name: SoftDeleteAlertChannel :execrows
-- Clear the encrypted secret on delete: a soft-deleted channel never delivers again, so there's
-- no reason to retain its webhook URL / bearer.
UPDATE alert_channel
SET deleted_at = now(),
    encrypted_config = ''
WHERE id = sqlc.arg('id')
  AND org_id = sqlc.arg('org_id')
  AND deleted_at IS NULL;

-- name: GetDeviceIdentities :many
-- Friendly device name + MAC for a set of device_identifiers in one org, resolved the
-- same way notification_history hydrates them (custom_name, else manufacturer + model).
SELECT
    d.device_identifier,
    TRIM(COALESCE(
        NULLIF(d.custom_name, ''),
        COALESCE(dd.manufacturer, '') || ' ' || COALESCE(dd.model, '')
    ))::text AS device_name,
    COALESCE(d.mac_address, '') AS device_mac
FROM device d
LEFT JOIN discovered_device dd ON dd.id = d.discovered_device_id
WHERE d.org_id = sqlc.arg('org_id')
  AND d.device_identifier = ANY(sqlc.arg('device_ids')::text[])
  AND d.deleted_at IS NULL;
