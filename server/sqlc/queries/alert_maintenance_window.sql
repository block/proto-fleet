-- name: InsertAlertMaintenanceWindow :one
INSERT INTO alert_maintenance_window (org_id, rule_uids, channel_ids, starts_at, ends_at, comment, created_by)
VALUES (
    sqlc.arg('org_id'),
    sqlc.arg('rule_uids')::text[],
    sqlc.arg('channel_ids')::bigint[],
    sqlc.arg('starts_at'),
    sqlc.arg('ends_at'),
    sqlc.arg('comment'),
    sqlc.arg('created_by')
)
RETURNING *;

-- name: UpdateAlertMaintenanceWindow :one
-- created_by/created_at are write-once: an update keeps the original creator for the audit trail.
UPDATE alert_maintenance_window
SET rule_uids   = sqlc.arg('rule_uids')::text[],
    channel_ids = sqlc.arg('channel_ids')::bigint[],
    starts_at   = sqlc.arg('starts_at'),
    ends_at     = sqlc.arg('ends_at'),
    comment     = sqlc.arg('comment'),
    updated_at  = now()
WHERE id = sqlc.arg('id')
  AND org_id = sqlc.arg('org_id')
RETURNING *;

-- name: ListAlertMaintenanceWindows :many
SELECT * FROM alert_maintenance_window
WHERE org_id = sqlc.arg('org_id')
ORDER BY starts_at DESC, id DESC;

-- name: ListActiveAlertMaintenanceWindows :many
-- The delivery-path read: only windows covering sqlc.arg('now'), so the expired tail never loads.
SELECT * FROM alert_maintenance_window
WHERE org_id = sqlc.arg('org_id')
  AND starts_at <= sqlc.arg('now')
  AND ends_at > sqlc.arg('now');

-- name: CountUnexpiredAlertMaintenanceWindows :one
-- Backs the per-org write quota: only windows still active or scheduled count against it, so
-- expired history can never block a write. excluding_id skips the row an update rewrites
-- (0 on insert, which no BIGSERIAL id equals).
SELECT count(*) FROM alert_maintenance_window
WHERE org_id = sqlc.arg('org_id')
  AND ends_at > sqlc.arg('now')
  AND id <> sqlc.arg('excluding_id');

-- name: DeleteExpiredAlertMaintenanceWindows :execrows
-- Retention: reclaims windows that ended before the cutoff so the org's list stays bounded.
DELETE FROM alert_maintenance_window
WHERE org_id = sqlc.arg('org_id')
  AND ends_at < sqlc.arg('before');

-- name: DeleteAlertMaintenanceWindow :execrows
DELETE FROM alert_maintenance_window
WHERE id = sqlc.arg('id')
  AND org_id = sqlc.arg('org_id');
