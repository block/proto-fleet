-- name: CreatePendingEnrollment :one
INSERT INTO pending_enrollment (code_hash, org_id, created_by, status, expires_at)
VALUES ($1, $2, $3, 'PENDING', $4)
RETURNING id, code_hash, org_id, created_by, fleet_node_id, status, expires_at, consumed_at, created_at;

-- name: GetPendingEnrollmentByCodeHash :one
SELECT id, code_hash, org_id, created_by, fleet_node_id, status, expires_at, consumed_at, created_at
FROM pending_enrollment
WHERE code_hash = $1;

-- name: GetPendingEnrollmentByFleetNode :one
-- Filter to the active status: a fleet_node_id can have terminal rows
-- (CONFIRMED/CANCELLED/EXPIRED) alongside the live AWAITING_CONFIRMATION,
-- so an unfiltered :one would return an arbitrary row.
SELECT id, code_hash, org_id, created_by, fleet_node_id, status, expires_at, consumed_at, created_at
FROM pending_enrollment
WHERE fleet_node_id = $1 AND org_id = $2 AND status = 'AWAITING_CONFIRMATION';

-- name: BindEnrollmentToFleetNode :execrows
UPDATE pending_enrollment
SET status = 'AWAITING_CONFIRMATION', fleet_node_id = $1
WHERE id = $2 AND status = 'PENDING';

-- name: ConfirmEnrollment :execrows
UPDATE pending_enrollment
SET status = 'CONFIRMED', consumed_at = $1
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
       a.enrollment_status, a.last_seen_at, a.created_at, a.updated_at,
       COALESCE(pe.status, '')::text AS pending_enrollment_status,
       pe.id AS pending_enrollment_id
FROM fleet_node a
LEFT JOIN pending_enrollment pe
  ON pe.fleet_node_id = a.id
 AND pe.status = 'AWAITING_CONFIRMATION'
WHERE a.org_id = $1
  AND a.deleted_at IS NULL
ORDER BY a.created_at DESC;

-- name: GetFleetNodeByID :one
SELECT id, org_id, name, identity_pubkey, encryption_pubkey,
       enrollment_status, last_seen_at, created_at, updated_at
FROM fleet_node
WHERE id = $1 AND org_id = $2 AND deleted_at IS NULL;

-- name: LockFleetNodeByID :one
SELECT id, org_id, name, identity_pubkey, encryption_pubkey,
       enrollment_status, last_seen_at, created_at, updated_at
FROM fleet_node
WHERE id = $1 AND org_id = $2 AND deleted_at IS NULL
FOR UPDATE;

-- name: GetFleetNodeByIDUnscoped :one
SELECT id, org_id, name, identity_pubkey, encryption_pubkey,
       enrollment_status, last_seen_at, created_at, updated_at
FROM fleet_node
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
UPDATE fleet_node
SET last_seen_at = $1
WHERE id = $2 AND org_id = $3 AND deleted_at IS NULL;
