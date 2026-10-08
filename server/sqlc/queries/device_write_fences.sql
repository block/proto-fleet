-- name: EnsureDeviceWriteFences :exec
INSERT INTO device_write_fence(device_id,organization_id)
SELECT d.id,d.org_id FROM device d
WHERE d.id=ANY(sqlc.arg(device_ids)::bigint[]) AND d.deleted_at IS NULL
ORDER BY d.id ON CONFLICT(device_id) DO NOTHING;

-- name: LockDeviceWriteFences :many
SELECT f.* FROM device_write_fence f JOIN device d ON d.id=f.device_id AND d.org_id=f.organization_id
WHERE f.device_id=ANY(sqlc.arg(device_ids)::bigint[]) AND d.deleted_at IS NULL
ORDER BY f.device_id FOR UPDATE OF f;

-- name: LockQueuedDeviceWrite :one
SELECT m.id,m.device_id,m.command_batch_log_uuid,m.command_type,m.write_epoch,d.org_id
FROM queue_message m JOIN device d ON d.id=m.device_id
WHERE m.id=$1 AND m.status='PROCESSING' AND d.deleted_at IS NULL
FOR UPDATE OF m;

-- name: CreateDeviceWriteAttempt :exec
INSERT INTO device_write_attempt(attempt_id,device_id,organization_id,epoch,queue_message_id,owner_instance_id,operation_kind,identity_keys,identity_snapshot)
SELECT sqlc.arg(attempt_id),d.id,d.org_id,sqlc.arg(epoch),sqlc.arg(queue_message_id),sqlc.arg(owner_instance_id),sqlc.arg(operation_kind),sqlc.arg(identity_keys)::text[],
 jsonb_build_object('device_identifier',d.device_identifier,'serial_number',d.serial_number,'mac_address',d.mac_address,
 'ip_address',dd.ip_address,'port',dd.port,'url_scheme',dd.url_scheme)
FROM device d JOIN discovered_device dd ON dd.id=d.discovered_device_id
WHERE d.id=sqlc.arg(device_id) AND d.org_id=sqlc.arg(organization_id);

-- name: CheckDeviceWriteAttempt :one
SELECT EXISTS (
 SELECT 1 FROM device_write_attempt a JOIN device_write_fence f ON f.device_id=a.device_id AND f.organization_id=a.organization_id
 JOIN device d ON d.id=f.device_id AND d.org_id=f.organization_id
 LEFT JOIN queue_message m ON m.id=a.queue_message_id
 WHERE a.attempt_id=$1 AND a.status='running' AND f.epoch=a.epoch AND f.state='host'
 AND ((d.deleted_at IS NULL AND a.identity_keys=device_write_identity_keys(d.id)) OR (d.deleted_at IS NOT NULL
  AND EXISTS (SELECT 1 FROM device_deletion_unpair_permit p WHERE p.attempt_id=a.attempt_id AND p.device_id=a.device_id AND p.epoch=a.epoch)
  AND NOT EXISTS (SELECT 1 FROM device successor WHERE successor.deleted_at IS NULL AND device_write_identity_keys(successor.id) && a.identity_keys)))
 AND (a.queue_message_id IS NULL OR m.status='PROCESSING')
)::boolean AS valid;

-- name: FinishDeviceWriteAttempt :execrows
UPDATE device_write_attempt SET status=$1,finished_at=statement_timestamp()
WHERE attempt_id=$2 AND status='running';

-- name: QuarantineDeviceWriteFence :exec
UPDATE device_write_fence SET quarantine_origin=CASE WHEN state='quarantined' THEN quarantine_origin ELSE state END,state='quarantined',updated_at=statement_timestamp()
WHERE device_id=$1 AND organization_id=$2 AND state<>'host';

-- name: ReserveDeviceWriteFence :one
UPDATE device_write_fence f SET epoch=epoch+1,state='reserved',updated_at=statement_timestamp()
WHERE f.device_id=$1 AND f.organization_id=$2 AND f.state='host'
AND NOT EXISTS (SELECT 1 FROM device_write_attempt a WHERE a.device_id=f.device_id AND a.status='unknown')
RETURNING epoch;

-- name: CancelPendingDeviceWrites :exec
-- Canceled queue rows cannot reach the worker or reaper. Record their results
-- atomically without firmware-completion effects: these writes never dispatched.
WITH canceled AS (
 UPDATE queue_message m SET status='FAILED',error_info='device command canceled by configuration handover',updated_at=statement_timestamp()
 WHERE m.device_id=$1 AND m.status='PENDING'
 RETURNING m.command_batch_log_uuid,m.device_id,m.error_info,m.updated_at
), results AS (
 SELECT DISTINCT c.command_batch_log_uuid,c.device_id,c.error_info,c.updated_at FROM canceled c
)
INSERT INTO command_on_device_log (
 command_batch_log_id,device_id,status,updated_at,error_info,org_id,site_id,
 custom_name,manufacturer,model,ip_address,mac_address
)
SELECT b.id,r.device_id,'FAILED'::device_command_status_enum,r.updated_at,r.error_info,d.org_id,d.site_id,
 d.custom_name,dd.manufacturer,dd.model,dd.ip_address,d.mac_address
FROM results r
JOIN command_batch_log b ON b.uuid=r.command_batch_log_uuid
JOIN device d ON d.id=r.device_id
JOIN discovered_device dd ON dd.id=d.discovered_device_id
ON CONFLICT (command_batch_log_id,device_id) DO UPDATE SET
 status=EXCLUDED.status,updated_at=EXCLUDED.updated_at,error_info=EXCLUDED.error_info;

-- name: IsDeviceWriteFenceDrained :one
SELECT EXISTS (
 SELECT 1 FROM device_write_fence f WHERE f.device_id=$1 AND f.organization_id=$2 AND f.epoch=$3
 AND NOT EXISTS (SELECT 1 FROM device_write_attempt a WHERE a.device_id=f.device_id AND a.status NOT IN ('complete','resolved'))
)::boolean AS drained;

-- name: ReleaseDeviceWriteFenceToHost :execrows
UPDATE device_write_fence f SET state='host',quarantine_origin=NULL,updated_at=statement_timestamp()
WHERE f.device_id=$1 AND f.organization_id=$2 AND f.epoch=$3 AND (f.state='reserved' OR (f.state='quarantined' AND f.quarantine_origin='reserved'))
AND NOT EXISTS (SELECT 1 FROM device_write_attempt a WHERE a.device_id=f.device_id AND a.status NOT IN ('complete','resolved'));

-- name: LockDeviceWriteFenceRecord :one
-- Completion must retain evidence even if the successful operation soft-deleted
-- the device. Starting a new operation still requires a live device.
SELECT * FROM device_write_fence WHERE device_id=$1 AND organization_id=$2 FOR UPDATE;


-- name: ListUnresolvedDeviceWriteAttempts :many
SELECT a.* FROM device_write_attempt a
WHERE a.organization_id=$1 AND a.device_id=$2 AND a.status IN ('running','unknown')
ORDER BY a.created_at,a.attempt_id;

-- name: LockDeviceWriteAttemptForRecovery :one
SELECT * FROM device_write_attempt WHERE attempt_id=$1 FOR UPDATE;

-- name: RecordDeviceWriteRecovery :exec
INSERT INTO device_write_recovery(recovery_id,attempt_id,fence_epoch,prior_status,operator_name,evidence,evidence_sha256)
VALUES($1,$2,$3,$4,$5,$6,$7);

-- name: ResolveDeviceWriteAttempt :execrows
UPDATE device_write_attempt SET status='resolved',finished_at=statement_timestamp()
WHERE attempt_id=$1 AND status IN ('running','unknown');

-- name: AdvanceRecoveredDeviceWriteFence :one
-- Invalidates stale handles and queue messages, without granting new custody.
UPDATE device_write_fence SET epoch=epoch+1,updated_at=statement_timestamp()
WHERE device_id=$1 AND organization_id=$2 AND epoch=$3
RETURNING *;

-- name: GetDeviceWriteIdentityKeys :one
SELECT COALESCE(device_write_identity_keys($1),'{}'::text[])::text[] AS identity_keys;

-- name: LockDeviceWriteIdentities :exec
-- Bound all identity admission paths to 256 transaction locks, including fleet
-- deletion. The two-int DWID namespace is separate from bigint advisory keys.
-- Collisions only serialize admission; physical identity comparisons stay exact.
SELECT pg_advisory_xact_lock(1146571076, stripe) FROM (
 SELECT DISTINCT (hashtextextended(identity,0) & 255)::integer AS stripe
 FROM unnest(sqlc.arg(identity_keys)::text[]) AS identity ORDER BY stripe
) AS ordered_stripes;

-- name: IsRetiredDeviceWriteDrained :one
SELECT EXISTS (SELECT 1 WHERE NOT EXISTS (
 SELECT 1 FROM device_write_attempt a JOIN device d ON d.id=a.device_id
 WHERE d.deleted_at IS NOT NULL AND a.status NOT IN ('complete','resolved')
 AND a.identity_keys && sqlc.arg(identity_keys)::text[]
))::boolean AS drained;

-- name: GetDeviceWriteDeletionTargets :many
SELECT d.id,d.device_identifier,COALESCE(device_write_identity_keys(d.id),'{}'::text[])::text[] AS identity_keys FROM device d
WHERE d.org_id=$1 AND d.device_identifier=ANY(sqlc.arg(device_identifiers)::text[]) AND d.deleted_at IS NULL
ORDER BY d.id;

-- name: AdvanceDeletedDeviceWriteFences :many
UPDATE device_write_fence SET epoch=epoch+1,updated_at=statement_timestamp()
WHERE device_id=ANY(sqlc.arg(device_ids)::bigint[]) AND organization_id=$1 AND state='host'
RETURNING device_id,epoch;

-- name: CancelPendingDeviceWritesForDeletion :exec
UPDATE queue_message SET status='FAILED',error_info='device command canceled by configuration handover',updated_at=statement_timestamp()
WHERE device_id=ANY(sqlc.arg(device_ids)::bigint[]) AND status='PENDING';

-- name: CreateDeletionUnpairPermits :exec
INSERT INTO device_deletion_unpair_permit(device_id,organization_id,epoch,binding_hash,identity_keys)
SELECT p.device_id,p.organization_id,p.epoch,p.binding_hash,p.identity_keys
FROM jsonb_to_recordset(sqlc.arg(permits)::jsonb) AS p(device_id bigint,organization_id bigint,epoch bigint,binding_hash text,identity_keys text[]);

-- name: CreateDeletionUnpairPermit :exec
INSERT INTO device_deletion_unpair_permit(device_id,organization_id,epoch,binding_hash,identity_keys)
VALUES($1,$2,$3,$4,$5);

-- name: LockDeletionUnpairPermit :one
SELECT p.* FROM device_deletion_unpair_permit p JOIN device d ON d.id=p.device_id AND d.org_id=p.organization_id
WHERE d.id=$1 AND d.org_id=$2 AND d.deleted_at IS NOT NULL AND p.attempt_id IS NULL
FOR UPDATE OF p;

-- name: IsDeletionUnpairIdentityCurrent :one
SELECT EXISTS (SELECT 1 WHERE NOT EXISTS (
 SELECT 1 FROM device d WHERE d.deleted_at IS NULL
 AND device_write_identity_keys(d.id) && sqlc.arg(identity_keys)::text[]
))::boolean AS current;

-- name: ClaimDeletionUnpairPermit :execrows
UPDATE device_deletion_unpair_permit SET attempt_id=$1 WHERE device_id=$2 AND attempt_id IS NULL;

-- name: GetDeletionUnpairPermit :one
SELECT p.* FROM device_deletion_unpair_permit p JOIN device d ON d.id=p.device_id AND d.org_id=p.organization_id
WHERE d.id=$1 AND d.org_id=$2 AND d.deleted_at IS NOT NULL AND p.attempt_id IS NULL;
