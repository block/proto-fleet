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

-- name: LockAlertMaintenanceWindowOrgForWrite :exec
-- Serializes the quota check with mutations across every server instance. The transaction that
-- takes this lock re-counts after its write and rolls back if the org would exceed its limit.
SELECT pg_advisory_xact_lock(
    hashtextextended('alert_maintenance_window:' || sqlc.arg('org_id')::bigint::text, 0)
);

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
-- Backs the transaction-scoped per-org write quota: only active or scheduled windows count, so
-- expired history can never block a write.
SELECT count(*) FROM alert_maintenance_window
WHERE org_id = sqlc.arg('org_id')
  AND ends_at > sqlc.arg('now');

-- name: PruneExpiredAlertMaintenanceWindows :execrows
-- Retention: reclaims the org's expired windows (ends_at <= now) that ended before the cutoff,
-- plus any beyond the newest keep_newest (see maxRetainedExpiredWindowsPerOrg for the why).
DELETE FROM alert_maintenance_window
WHERE alert_maintenance_window.org_id = sqlc.arg('org_id')
  AND alert_maintenance_window.ends_at <= sqlc.arg('now')
  AND (
    alert_maintenance_window.ends_at < sqlc.arg('before')
    OR alert_maintenance_window.id IN (
        SELECT newest.id FROM alert_maintenance_window AS newest
        WHERE newest.org_id = sqlc.arg('org_id')
          AND newest.ends_at <= sqlc.arg('now')
        ORDER BY newest.ends_at DESC, newest.id DESC
        OFFSET sqlc.arg('keep_newest')::bigint
    )
  );

-- name: DeleteAlertMaintenanceWindow :execrows
DELETE FROM alert_maintenance_window
WHERE id = sqlc.arg('id')
  AND org_id = sqlc.arg('org_id');
