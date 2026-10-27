-- name: CreatePendingEnrollment :one
INSERT INTO pending_enrollment (code_hash, org_id, created_by, status, expires_at, standby_for_node_id)
VALUES ($1, $2, $3, 'PENDING', $4, sqlc.narg('standby_for_node_id'))
RETURNING id, code_hash, org_id, created_by, fleet_node_id, status, expires_at, consumed_at, created_at, confirmed_by, standby_for_node_id;

-- name: GetPendingEnrollmentByCodeHash :one
SELECT id, code_hash, org_id, created_by, fleet_node_id, status, expires_at, consumed_at, created_at, confirmed_by, standby_for_node_id
FROM pending_enrollment
WHERE code_hash = $1;

-- name: GetPendingEnrollmentByFleetNode :one
-- Filter to the active status: a fleet_node_id can have terminal rows
-- (CONFIRMED/CANCELLED/EXPIRED) alongside the live AWAITING_CONFIRMATION,
-- so an unfiltered :one would return an arbitrary row.
SELECT id, code_hash, org_id, created_by, fleet_node_id, status, expires_at, consumed_at, created_at, confirmed_by, standby_for_node_id
FROM pending_enrollment
WHERE fleet_node_id = $1 AND org_id = $2 AND status = 'AWAITING_CONFIRMATION';

-- name: GetCancelledEnrollmentByFleetNode :one
-- Supports retrying teardown after authentication revocation has committed.
SELECT id, code_hash, org_id, created_by, fleet_node_id, status, expires_at, consumed_at, created_at, confirmed_by, standby_for_node_id
FROM pending_enrollment
WHERE id = $1 AND fleet_node_id = $2 AND org_id = $3 AND status = 'CANCELLED';

-- name: BindEnrollmentToFleetNode :execrows
UPDATE pending_enrollment
SET status = 'AWAITING_CONFIRMATION', fleet_node_id = $1
WHERE id = $2 AND status = 'PENDING';

-- name: ConfirmEnrollment :execrows
UPDATE pending_enrollment
SET status = 'CONFIRMED', consumed_at = $1, confirmed_by = sqlc.arg('confirmed_by')
WHERE id = $2 AND status = 'AWAITING_CONFIRMATION';

-- name: CancelPendingEnrollment :execrows
UPDATE pending_enrollment
SET status = 'CANCELLED', consumed_at = $1
WHERE id = $2 AND status = 'PENDING' AND org_id = $3;

-- name: CancelEnrollmentForFleetNode :execrows
UPDATE pending_enrollment
SET status = 'CANCELLED', consumed_at = $1
WHERE fleet_node_id = $2 AND org_id = $3
  AND status IN ('PENDING', 'AWAITING_CONFIRMATION');

-- name: SweepExpiredEnrollments :execrows
UPDATE pending_enrollment
SET status = 'EXPIRED'
WHERE expires_at < $1
  AND status IN ('PENDING', 'AWAITING_CONFIRMATION');

-- name: ListFleetNodesForOrganization :many
-- A fleet node can have multiple pending_enrollment rows over its lifetime
-- (terminal CONFIRMED/EXPIRED/CANCELLED + a new AWAITING_CONFIRMATION on
-- re-enrollment), so the join is filtered to the one status the listing
-- cares about. Without the filter the LEFT JOIN multiplies rows.
SELECT a.id, a.org_id, a.name, a.identity_pubkey,
       a.enrollment_status, h.last_seen_at, a.created_at, a.updated_at,
       COALESCE(pe.status, '')::text AS pending_enrollment_status,
       pe.id AS pending_enrollment_id,
       COALESCE(h.ip_addresses, '{}'::text[])::text[] AS ip_addresses,
       COALESCE(h.version, '')::text AS version,
       COALESCE(u.username, '')::text AS enrolled_by,
       pe.standby_for_node_id,
       p.active_node_id AS ha_active_node_id,
       p.standby_node_id AS ha_standby_node_id,
       COALESCE(p.standby_ready, false)::boolean AS ha_standby_ready,
       COALESCE(p.status_reason, '')::text AS ha_status_reason
FROM fleet_node a
LEFT JOIN pending_enrollment pe
  ON pe.fleet_node_id = a.id
 AND pe.status = 'AWAITING_CONFIRMATION'
LEFT JOIN fleet_node_ha_pair p
  ON p.org_id = a.org_id AND (p.active_node_id = a.id OR p.standby_node_id = a.id)
LEFT JOIN fleet_node_heartbeat h ON h.fleet_node_id = a.id AND h.org_id = a.org_id
LEFT JOIN LATERAL (
    SELECT confirmed_by FROM pending_enrollment
    WHERE fleet_node_id = a.id AND org_id = a.org_id AND status = 'CONFIRMED'
    ORDER BY consumed_at DESC, id DESC LIMIT 1
) completed ON TRUE
LEFT JOIN "user" u ON u.id = completed.confirmed_by
WHERE a.org_id = $1
  AND a.deleted_at IS NULL
ORDER BY a.created_at DESC;

-- name: GetFleetNodeByID :one
SELECT id, org_id, name, identity_pubkey, encryption_pubkey,
       enrollment_status, last_seen_at, created_at, updated_at
FROM fleet_node_live
WHERE id = $1 AND org_id = $2 AND deleted_at IS NULL;

-- name: LockFleetNodeByID :one
SELECT fn.id, fn.org_id, fn.name, fn.identity_pubkey, fn.encryption_pubkey,
       fn.enrollment_status, h.last_seen_at, fn.created_at, fn.updated_at
FROM fleet_node fn
LEFT JOIN fleet_node_heartbeat h ON h.fleet_node_id=fn.id AND h.org_id=fn.org_id
WHERE fn.id = $1 AND fn.org_id = $2 AND fn.deleted_at IS NULL
FOR NO KEY UPDATE OF fn;

-- name: LockFleetNodeIdentityByID :one
-- Keep the routing identity alive during a device wait without blocking
-- authentication revocation. Writers revalidate with LockFleetNodeByID after
-- acquiring the device lock before they mutate any binding children.
SELECT fn.id, fn.org_id, fn.name, fn.identity_pubkey, fn.encryption_pubkey,
       fn.enrollment_status, h.last_seen_at, fn.created_at, fn.updated_at
FROM fleet_node fn
LEFT JOIN fleet_node_heartbeat h ON h.fleet_node_id=fn.id AND h.org_id=fn.org_id
WHERE fn.id = $1 AND fn.org_id = $2 AND fn.deleted_at IS NULL
FOR KEY SHARE OF fn;

-- name: GetFleetNodeByIDUnscoped :one
SELECT id, org_id, name, identity_pubkey, encryption_pubkey,
       enrollment_status, last_seen_at, created_at, updated_at
FROM fleet_node_live
WHERE id = $1 AND deleted_at IS NULL;

-- name: CreateFleetNode :one
INSERT INTO fleet_node (org_id, name, identity_pubkey, encryption_pubkey, enrollment_status)
VALUES ($1, $2, $3, $4, 'PENDING')
RETURNING id, org_id, name, identity_pubkey, encryption_pubkey,
          enrollment_status, last_seen_at, created_at, updated_at;

-- name: SetFleetNodeEnrollmentStatus :execrows
UPDATE fleet_node
SET enrollment_status = $1
WHERE id = $2 AND org_id = $3 AND deleted_at IS NULL;

-- name: SoftDeleteFleetNode :execrows
UPDATE fleet_node
SET deleted_at = $1
WHERE id = $2 AND org_id = $3 AND deleted_at IS NULL;

-- name: SoftDeleteFleetNodesForExpiredEnrollments :execrows
UPDATE fleet_node a
SET deleted_at = $1
FROM pending_enrollment pe
WHERE a.id = pe.fleet_node_id
  AND a.deleted_at IS NULL
  AND pe.status IN ('PENDING', 'AWAITING_CONFIRMATION')
  AND pe.expires_at < $1;

-- name: UpdateFleetNodeLastSeenAt :execrows
INSERT INTO fleet_node_heartbeat (fleet_node_id,org_id,last_seen_at,ip_addresses,version)
SELECT fn.id,fn.org_id,sqlc.arg('last_seen_at')::timestamptz,
       COALESCE(sqlc.arg('ip_addresses')::text[], '{}'::text[]), sqlc.arg('version')::text
FROM fleet_node fn
WHERE fn.id=sqlc.arg('id') AND fn.org_id=sqlc.arg('org_id') AND fn.deleted_at IS NULL
ON CONFLICT (fleet_node_id) DO UPDATE
SET last_seen_at=GREATEST(fleet_node_heartbeat.last_seen_at,EXCLUDED.last_seen_at),
    ip_addresses=CASE WHEN EXCLUDED.last_seen_at >= fleet_node_heartbeat.last_seen_at THEN EXCLUDED.ip_addresses ELSE fleet_node_heartbeat.ip_addresses END,
    version=CASE WHEN EXCLUDED.last_seen_at >= fleet_node_heartbeat.last_seen_at THEN EXCLUDED.version ELSE fleet_node_heartbeat.version END;

-- name: LockFleetNodeDevicesForRevocation :many
-- Teardown follows dispatch's device-before-child order and retains routing
-- children until any admitted physical call has actually returned.
SELECT d.id
FROM device d
JOIN fleet_node_device fnd ON fnd.device_id=d.id AND fnd.org_id=d.org_id
WHERE fnd.fleet_node_id=$1 AND fnd.org_id=$2
ORDER BY d.id
FOR NO KEY UPDATE OF d;
