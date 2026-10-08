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
INSERT INTO device_write_attempt(attempt_id,device_id,organization_id,epoch,queue_message_id)
VALUES ($1,$2,$3,$4,$5);

-- name: CheckDeviceWriteAttempt :one
SELECT EXISTS (
 SELECT 1 FROM device_write_attempt a JOIN device_write_fence f ON f.device_id=a.device_id AND f.organization_id=a.organization_id
 JOIN device d ON d.id=f.device_id AND d.org_id=f.organization_id
 JOIN queue_message m ON m.id=a.queue_message_id AND m.status='PROCESSING'
 WHERE a.attempt_id=$1 AND a.status='running' AND f.epoch=a.epoch AND f.state='host' AND d.deleted_at IS NULL
)::boolean AS valid;

-- name: FinishDeviceWriteAttempt :execrows
UPDATE device_write_attempt SET status=$1,finished_at=statement_timestamp()
WHERE attempt_id=$2 AND status='running';

-- name: QuarantineDeviceWriteFence :exec
UPDATE device_write_fence SET state='quarantined',updated_at=statement_timestamp()
WHERE device_id=$1 AND organization_id=$2 AND state<>'host';

-- name: ReserveDeviceWriteFence :one
UPDATE device_write_fence SET epoch=epoch+1,state='reserved',updated_at=statement_timestamp()
WHERE device_id=$1 AND organization_id=$2 AND state='host'
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
 AND NOT EXISTS (SELECT 1 FROM device_write_attempt a WHERE a.device_id=f.device_id AND a.status<>'complete')
)::boolean AS drained;

-- name: ReleaseDeviceWriteFenceToHost :execrows
UPDATE device_write_fence f SET state='host',updated_at=statement_timestamp()
WHERE f.device_id=$1 AND f.organization_id=$2 AND f.epoch=$3 AND f.state='reserved'
AND NOT EXISTS (SELECT 1 FROM device_write_attempt a WHERE a.device_id=f.device_id AND a.status<>'complete');

-- name: LockDeviceWriteFenceRecord :one
-- Completion must retain evidence even if the successful operation soft-deleted
-- the device. Starting a new operation still requires a live device.
SELECT * FROM device_write_fence WHERE device_id=$1 AND organization_id=$2 FOR UPDATE;
