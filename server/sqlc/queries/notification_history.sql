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

-- name: BulkInsertNotificationHistory :execrows
-- Multi-row insert via jsonb_to_recordset (per-row AFTER-INSERT trigger still fires); :execrows lets the caller verify the row count.
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
)
SELECT
    r.alert_name,
    r.status,
    r.severity,
    r.rule_group,
    r.fingerprint,
    r.organization_id,
    r.device_id,
    r.template,
    r.summary,
    r.starts_at,
    r.ends_at,
    COALESCE(r.labels, '{}'::jsonb),
    COALESCE(r.annotations, '{}'::jsonb)
FROM jsonb_to_recordset(sqlc.arg('rows_jsonb')::JSONB) AS r(
    alert_name      TEXT,
    status          TEXT,
    severity        TEXT,
    rule_group      TEXT,
    fingerprint     TEXT,
    organization_id BIGINT,
    device_id       TEXT,
    template        TEXT,
    summary         TEXT,
    starts_at       TIMESTAMPTZ,
    ends_at         TIMESTAMPTZ,
    labels          JSONB,
    annotations     JSONB
);

-- name: ListNotificationHistory :many
-- Resolution rows remain stored so the notification_active trigger can close firing alerts,
-- but the activity feed records only the alert firing event.
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
  AND nh.status <> 'resolved'
  AND (sqlc.narg('before_id')::bigint IS NULL OR nh.id < sqlc.narg('before_id'))
ORDER BY nh.id DESC
LIMIT sqlc.arg('page_limit');

-- name: ListActiveNotifications :many
-- Current firing alerts (one row per alert instance), served from the incrementally-maintained
-- notification_active table, which also retains resolved tombstones; device name/MAC are joined live
-- so they reflect current device records. Ordered to match idx_notification_active_org_recent, so the freshness
-- window is a range scan that stops at page_limit rather than a sort over the whole set.
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
  AND na.received_at >= sqlc.arg('active_since') -- drop alerts not re-asserted within the freshness window
ORDER BY na.received_at DESC, na.history_id DESC
LIMIT sqlc.arg('page_limit');

-- name: ListActiveNotificationsByAlert :many
-- One rule's firing instances, one per affected miner: the drill-in behind a rollup row. Keyset on alert_key,
-- not history_id: a re-assert rewrites history_id, lifting an unread row above the cursor and losing it.
SELECT
    na.alert_key,
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
  AND na.received_at >= sqlc.arg('active_since') -- drop alerts not re-asserted within the freshness window
  AND na.alert_name = sqlc.arg('alert_name')
  AND na.rule_group = sqlc.arg('rule_group')
  AND (sqlc.narg('after_key')::text IS NULL OR na.alert_key > sqlc.narg('after_key'))
ORDER BY na.alert_key
LIMIT sqlc.arg('page_limit');

-- name: ListActiveNotificationGroups :many
-- Firing alerts rolled up per rule, worst blast radius first. (alert_name, rule_group) is rule identity: Grafana
-- keeps titles unique per folder and a rule_group label maps to one folder, so a title repeats only across labels.
-- Counts and identity aggregate here; the one piece of per-instance detail is picked off in the lateral below.
WITH groups AS (
    SELECT
        na.alert_name,
        na.rule_group,
        COUNT(*)::bigint AS alert_count,
        -- FILTER, not COUNT(DISTINCT NULLIF(...)): equivalent, but only the bare column matches
        -- idx_notification_active_org_rollup's ordering, so this streams out of the GroupAggregate unsorted.
        (COUNT(DISTINCT na.device_id) FILTER (WHERE na.device_id <> ''))::bigint AS device_count,
        MIN(COALESCE(na.starts_at, na.received_at))::timestamptz AS first_started_at,
        MAX(na.received_at) AS last_received_at
    FROM notification_active na
    WHERE na.organization_id = sqlc.arg('organization_id')
      AND na.status = 'firing'
      AND na.received_at >= sqlc.arg('active_since') -- drop alerts not re-asserted within the freshness window
    GROUP BY na.alert_name, na.rule_group
    ORDER BY device_count DESC, alert_count DESC, MAX(na.received_at) DESC, na.alert_name
    LIMIT sqlc.arg('page_limit')
)
SELECT
    g.alert_name,
    g.rule_group,
    g.alert_count,
    g.device_count,
    g.first_started_at,
    -- A group with miners is described by its drill-in, so it reports no free text here and the CASE drops what
    -- the lateral found. device_id = '' is an index prefix, so a group with no device-less rows costs one descent.
    -- The summary is a header-polled one-liner, so it is bounded here rather than shipping a whole TEXT column;
    -- the template rides along because the caller decides from it whether the summary may name a miner. Neither
    -- is length-checked on insert (000136 bounds only the indexed columns), and a truncated template fails that
    -- decision closed: it matches no known template, so the summary is withheld.
    (CASE WHEN g.device_count = 0 THEN LEFT(COALESCE(s.summary, ''), 500) ELSE '' END)::text AS summary,
    (CASE WHEN g.device_count = 0 THEN LEFT(COALESCE(s.template, ''), 64) ELSE '' END)::text AS template
FROM groups g
LEFT JOIN LATERAL (
    SELECT na.summary, na.template
    FROM notification_active na
    WHERE na.organization_id = sqlc.arg('organization_id')
      AND na.status = 'firing'
      AND na.received_at >= sqlc.arg('active_since')
      AND na.alert_name = g.alert_name
      AND na.rule_group = g.rule_group
      AND na.device_id = ''
    ORDER BY na.received_at DESC, na.history_id DESC
    LIMIT 1
) s ON TRUE
-- Repeated: the CTE's ordering is not guaranteed to survive the join.
ORDER BY g.device_count DESC, g.alert_count DESC, g.last_received_at DESC, g.alert_name;
