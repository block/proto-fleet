-- name: InsertNotificationHistory :exec
-- Persist one notification delivered by the Grafana alertmanager webhook receiver.
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
-- One page of an organization's notifications, newest first. Keyset
-- pagination: pass the previous page's last id as before_id, or NULL
-- for the first page. id order matches received_at order (rows are
-- insert-only with a now() default) and stays stable across pages.
-- The device join resolves the Grafana device_id label (the device
-- identifier UUID) to a display name and MAC for the UI; device_name
-- follows the same custom_name → manufacturer+model precedence as the
-- device list sort. Both come back '' when the device is unknown or
-- has been deleted since the alert fired.
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
