-- name: CreatePendingEnrollment :one
INSERT INTO pending_enrollment (code_hash, org_id, created_by, status, expires_at)
VALUES ($1, $2, $3, 'PENDING', $4)
RETURNING id, code_hash, org_id, created_by, agent_id, status, expires_at, consumed_at, created_at;

-- name: GetPendingEnrollmentByCodeHash :one
SELECT id, code_hash, org_id, created_by, agent_id, status, expires_at, consumed_at, created_at
FROM pending_enrollment
WHERE code_hash = $1;

-- name: GetPendingEnrollmentByAgent :one
SELECT id, code_hash, org_id, created_by, agent_id, status, expires_at, consumed_at, created_at
FROM pending_enrollment
WHERE agent_id = $1 AND org_id = $2;

-- name: BindEnrollmentToAgent :execrows
UPDATE pending_enrollment
SET status = 'AWAITING_CONFIRMATION', agent_id = $1
WHERE id = $2 AND status = 'PENDING';

-- name: ConfirmEnrollment :execrows
UPDATE pending_enrollment
SET status = 'CONFIRMED', consumed_at = $1
WHERE id = $2 AND status = 'AWAITING_CONFIRMATION';

-- name: CancelPendingEnrollment :execrows
UPDATE pending_enrollment
SET status = 'CANCELLED', consumed_at = $1
WHERE id = $2 AND status = 'PENDING' AND org_id = $3;

-- name: CancelEnrollmentForAgent :execrows
UPDATE pending_enrollment
SET status = 'CANCELLED', consumed_at = $1
WHERE agent_id = $2 AND org_id = $3
  AND status IN ('PENDING', 'AWAITING_CONFIRMATION');

-- name: SweepExpiredEnrollments :execrows
UPDATE pending_enrollment
SET status = 'EXPIRED'
WHERE expires_at < $1
  AND status IN ('PENDING', 'AWAITING_CONFIRMATION');

-- name: ListAgentsForOrganization :many
SELECT a.id, a.org_id, a.name, a.identity_pubkey, a.miner_signing_pubkey,
       a.enrollment_status, a.last_seen_at, a.created_at, a.updated_at
FROM agent a
WHERE a.org_id = $1
  AND a.deleted_at IS NULL
ORDER BY a.created_at DESC;

-- name: GetAgentByID :one
SELECT id, org_id, name, identity_pubkey, miner_signing_pubkey,
       enrollment_status, last_seen_at, created_at, updated_at
FROM agent
WHERE id = $1 AND org_id = $2 AND deleted_at IS NULL;

-- name: GetAgentByIDUnscoped :one
SELECT id, org_id, name, identity_pubkey, miner_signing_pubkey,
       enrollment_status, last_seen_at, created_at, updated_at
FROM agent
WHERE id = $1 AND deleted_at IS NULL;

-- name: CreateAgent :one
INSERT INTO agent (org_id, name, identity_pubkey, miner_signing_pubkey, enrollment_status)
VALUES ($1, $2, $3, $4, 'PENDING')
RETURNING id, org_id, name, identity_pubkey, miner_signing_pubkey,
          enrollment_status, last_seen_at, created_at, updated_at;

-- name: SetAgentEnrollmentStatus :execrows
UPDATE agent
SET enrollment_status = $1
WHERE id = $2 AND org_id = $3 AND deleted_at IS NULL;

-- name: SoftDeleteAgent :execrows
UPDATE agent
SET deleted_at = $1
WHERE id = $2 AND org_id = $3 AND deleted_at IS NULL;

-- name: SoftDeleteAgentsForExpiredEnrollments :execrows
UPDATE agent a
SET deleted_at = $1
FROM pending_enrollment pe
WHERE a.id = pe.agent_id
  AND a.deleted_at IS NULL
  AND pe.status IN ('PENDING', 'AWAITING_CONFIRMATION')
  AND pe.expires_at < $1;
