-- name: InsertNotificationMetricSamples :exec
-- Batch insert for the notification_metric_sample hypertable populated by
-- the in-process metrics provider on every flush.
INSERT INTO notification_metric_sample (
    time,
    metric,
    organization_id,
    device_id,
    device_group,
    driver,
    sensor_kind,
    kind,
    result,
    value
)
SELECT
    unnest(sqlc.arg('times')::timestamptz[]),
    unnest(sqlc.arg('metrics')::text[]),
    unnest(sqlc.arg('organization_ids')::text[]),
    unnest(sqlc.arg('device_ids')::text[]),
    unnest(sqlc.arg('device_groups')::text[]),
    unnest(sqlc.arg('drivers')::text[]),
    unnest(sqlc.arg('sensor_kinds')::text[]),
    unnest(sqlc.arg('kinds')::text[]),
    unnest(sqlc.arg('results')::text[]),
    unnest(sqlc.arg('values')::double precision[]);
