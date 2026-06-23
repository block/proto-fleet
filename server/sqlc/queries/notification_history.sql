-- name: InsertNotificationHistory :exec
INSERT INTO notification_history (
    alert_name,
    status,
    severity,
    rule_group,
    fingerprint,
    organization_id,
    device_id,
    template,
    summary,
    starts_at,
    ends_at,
    labels,
    annotations
) VALUES (
    sqlc.arg('alert_name'),
    sqlc.arg('status'),
    sqlc.arg('severity'),
    sqlc.arg('rule_group'),
    sqlc.arg('fingerprint'),
    sqlc.narg('organization_id'),
    sqlc.arg('device_id'),
    sqlc.arg('template'),
    sqlc.arg('summary'),
    sqlc.narg('starts_at'),
    sqlc.narg('ends_at'),
    sqlc.arg('labels'),
    sqlc.arg('annotations')
);

-- name: ListNotificationHistory :many
SELECT
    nh.id,
    nh.received_at,
    nh.alert_name,
    nh.status,
    nh.severity,
    nh.rule_group,
    nh.fingerprint,
    nh.organization_id,
    nh.device_id,
    COALESCE(
        TRIM(COALESCE(
            NULLIF(d.custom_name, ''),
            COALESCE(dd.manufacturer, '') || ' ' || COALESCE(dd.model, '')
        )),
        ''
    )::text AS device_name,
    COALESCE(d.mac_address, '') AS device_mac,
    nh.template,
    nh.summary,
    nh.starts_at,
    nh.ends_at
FROM notification_history nh
LEFT JOIN device d
    ON d.device_identifier = nh.device_id
    AND d.org_id = nh.organization_id
    AND d.deleted_at IS NULL
LEFT JOIN discovered_device dd ON dd.id = d.discovered_device_id
WHERE nh.organization_id = sqlc.arg('organization_id')
  AND (sqlc.narg('before_id')::bigint IS NULL OR nh.id < sqlc.narg('before_id'))
ORDER BY nh.id DESC
LIMIT sqlc.arg('page_limit');

-- name: ListActiveNotifications :many
-- Current firing alerts (one row per alert instance), served from the incrementally-maintained
-- notification_active table, which also retains resolved tombstones; device name/MAC are joined live
-- so they reflect current device records.
SELECT
    na.history_id,
    na.received_at,
    na.alert_name,
    na.severity,
    na.rule_group,
    na.fingerprint,
    na.organization_id,
    na.device_id,
    COALESCE(
        TRIM(COALESCE(
            NULLIF(d.custom_name, ''),
            COALESCE(dd.manufacturer, '') || ' ' || COALESCE(dd.model, '')
        )),
        ''
    )::text AS device_name,
    COALESCE(d.mac_address, '') AS device_mac,
    na.template,
    na.summary,
    na.starts_at,
    na.ends_at
FROM notification_active na
LEFT JOIN device d
    ON d.device_identifier = na.device_id
    AND d.org_id = na.organization_id
    AND d.deleted_at IS NULL
LEFT JOIN discovered_device dd ON dd.id = d.discovered_device_id
WHERE na.organization_id = sqlc.arg('organization_id')
  AND na.status = 'firing'
ORDER BY na.received_at DESC, na.history_id DESC
LIMIT sqlc.arg('page_limit');
