-- name: SetEnrollmentStandbyOwner :execrows
UPDATE pending_enrollment SET standby_owner_node_id=sqlc.arg('owner_node_id')
WHERE id=sqlc.arg('enrollment_id') AND org_id=sqlc.arg('org_id') AND status='PENDING' AND standby_owner_node_id IS NULL;

-- name: UpsertFleetNodeHAConnection :exec
INSERT INTO fleet_node_ha_connection(node_id,org_id,ha_capable,instance_id,control_session_id,credential_key_fingerprint)
VALUES($1,$2,$3,$4,$5,$6)
ON CONFLICT(node_id) DO UPDATE SET ha_capable=EXCLUDED.ha_capable,instance_id=EXCLUDED.instance_id,
control_session_id=EXCLUDED.control_session_id,credential_key_fingerprint=EXCLUDED.credential_key_fingerprint,seen_at=clock_timestamp();

-- name: TouchFleetNodeHAConnection :execrows
UPDATE fleet_node_ha_connection SET seen_at=clock_timestamp()
WHERE node_id=$1 AND org_id=$2 AND instance_id=$3 AND control_session_id=$4;

-- name: DeleteFleetNodeHAConnection :exec
DELETE FROM fleet_node_ha_connection WHERE node_id=$1 AND org_id=$2 AND control_session_id=$3;

-- name: GetFleetNodeHAMember :one
SELECT * FROM fleet_node_ha_member WHERE node_id=$1 AND org_id=$2;

-- name: GetFleetNodeHAGroup :one
SELECT * FROM fleet_node_ha_group WHERE owner_node_id=$1 AND (org_id=sqlc.arg('org_id') OR sqlc.arg('org_id')=0);

-- name: CreateFleetNodeHAGroup :exec
INSERT INTO fleet_node_ha_group(owner_node_id,org_id,credential_key_fingerprint) VALUES($1,$2,$3)
ON CONFLICT(owner_node_id) DO NOTHING;

-- name: CreateFleetNodeHAMember :exec
INSERT INTO fleet_node_ha_member(node_id,org_id,owner_node_id,ready,status_reason)
VALUES($1,$2,$3,$4,$5) ON CONFLICT(node_id) DO NOTHING;

-- name: SetFleetNodeHAMemberReady :execrows
UPDATE fleet_node_ha_member SET ready=true,status_reason=''
WHERE node_id=$1 AND org_id=$2 AND revoked_at IS NULL;

-- name: GetFleetNodeHAConnection :one
SELECT * FROM fleet_node_ha_connection WHERE node_id=$1 AND org_id=$2;

-- name: ListFleetNodeHAMembers :many
SELECT m.*,fn.name,fn.encryption_pubkey,fn.enrollment_status,
c.instance_id,c.control_session_id,c.credential_key_fingerprint,c.seen_at,c.ha_capable
FROM fleet_node_ha_member m JOIN fleet_node fn ON fn.id=m.node_id AND fn.org_id=m.org_id
LEFT JOIN fleet_node_ha_connection c ON c.node_id=m.node_id AND c.org_id=m.org_id
WHERE m.owner_node_id=$1 AND m.org_id=$2 AND m.revoked_at IS NULL
ORDER BY m.joined_at,m.node_id;

-- name: AcquireOrRenewFleetNodeHALease :one
UPDATE fleet_node_ha_group g SET
 active_node_id=sqlc.arg('node_id'), instance_id=sqlc.arg('instance_id'),control_session_id=sqlc.arg('control_session_id'),
 lease_epoch=CASE WHEN g.active_node_id=sqlc.arg('node_id') AND g.instance_id=sqlc.arg('instance_id') AND g.control_session_id=sqlc.arg('control_session_id') AND g.lease_expires_at>clock_timestamp() THEN g.lease_epoch ELSE g.lease_epoch+1 END,
 acknowledged=CASE WHEN g.active_node_id=sqlc.arg('node_id') AND g.instance_id=sqlc.arg('instance_id') AND g.control_session_id=sqlc.arg('control_session_id') AND g.lease_expires_at>clock_timestamp() THEN (g.acknowledged OR sqlc.arg('acknowledged_epoch')=g.lease_epoch) ELSE false END,
 lease_expires_at=clock_timestamp()+interval '30 seconds'
WHERE g.owner_node_id=sqlc.arg('owner_node_id') AND g.org_id=sqlc.arg('org_id')
 AND EXISTS (SELECT 1 FROM fleet_node_ha_member m JOIN fleet_node fn ON fn.id=m.node_id AND fn.org_id=m.org_id JOIN fleet_node_ha_connection c ON c.node_id=m.node_id AND c.org_id=m.org_id
 WHERE m.node_id=sqlc.arg('node_id') AND m.owner_node_id=g.owner_node_id AND m.ready AND m.revoked_at IS NULL AND fn.enrollment_status='CONFIRMED' AND fn.deleted_at IS NULL AND c.instance_id=sqlc.arg('instance_id') AND c.control_session_id=sqlc.arg('control_session_id') AND c.ha_capable AND c.credential_key_fingerprint=g.credential_key_fingerprint)
 AND ((g.active_node_id=sqlc.arg('node_id') AND g.instance_id=sqlc.arg('instance_id') AND g.control_session_id=sqlc.arg('control_session_id') AND g.lease_expires_at>clock_timestamp())
 OR (g.lease_expires_at<=clock_timestamp() AND sqlc.arg('node_id')=(SELECT m.node_id FROM fleet_node_ha_member m JOIN fleet_node fn ON fn.id=m.node_id AND fn.org_id=m.org_id JOIN fleet_node_ha_connection c ON c.node_id=m.node_id AND c.org_id=m.org_id WHERE m.owner_node_id=g.owner_node_id AND m.org_id=g.org_id AND m.ready AND m.revoked_at IS NULL AND fn.enrollment_status='CONFIRMED' AND fn.deleted_at IS NULL AND c.ha_capable AND c.credential_key_fingerprint=g.credential_key_fingerprint AND c.seen_at>clock_timestamp()-interval '10 seconds' ORDER BY m.joined_at,m.node_id LIMIT 1)))
RETURNING g.*;

-- name: RevokeFleetNodeHAMember :exec
UPDATE fleet_node_ha_member SET revoked_at=clock_timestamp(),ready=false,status_reason='revoked' WHERE node_id=$1 AND org_id=$2;

-- name: ExpireFleetNodeHALease :exec
-- Deny routing immediately, but retain the old process expiry before promotion.
UPDATE fleet_node_ha_group SET acknowledged=false WHERE active_node_id=$1 AND org_id=$2;

-- name: GetFleetNodeHATransfer :one
SELECT * FROM fleet_node_ha_transfer WHERE recipient_node_id=$1;

-- name: CreateFleetNodeHATransfer :exec
INSERT INTO fleet_node_ha_transfer(recipient_node_id,operation_id,owner_node_id,source_node_id,expires_at)
VALUES($1,$2,$3,$4,clock_timestamp()+interval '5 minutes')
ON CONFLICT(recipient_node_id) DO UPDATE SET operation_id=EXCLUDED.operation_id,source_node_id=EXCLUDED.source_node_id,
expires_at=EXCLUDED.expires_at,attempts=0,encrypted_key=''::bytea,next_attempt_at=clock_timestamp()
WHERE NOT fleet_node_ha_transfer.failed AND (fleet_node_ha_transfer.expires_at<=clock_timestamp()
 OR (octet_length(fleet_node_ha_transfer.encrypted_key)=0 AND fleet_node_ha_transfer.source_node_id<>EXCLUDED.source_node_id));

-- name: ClaimFleetNodeHATransfer :one
UPDATE fleet_node_ha_transfer SET attempts=attempts+1,
next_attempt_at=clock_timestamp()+interval '1 second'*LEAST(30,power(2,LEAST(attempts,5)))
WHERE recipient_node_id=$1 AND expires_at>clock_timestamp() AND next_attempt_at<=clock_timestamp() AND NOT failed RETURNING *;

-- name: SetFleetNodeHATransferCiphertext :execrows
UPDATE fleet_node_ha_transfer SET encrypted_key=$1 WHERE recipient_node_id=$2 AND operation_id=$3 AND expires_at>clock_timestamp();

-- name: SetFleetNodeHAConnectionFingerprint :execrows
UPDATE fleet_node_ha_connection SET credential_key_fingerprint=$1 WHERE node_id=$2 AND org_id=$3 AND control_session_id=$4;

-- name: LockFleetNodeHAGroup :one
SELECT * FROM fleet_node_ha_group WHERE owner_node_id=$1 AND org_id=$2 FOR UPDATE;

-- name: CompleteFleetNodeHATransfer :execrows
UPDATE fleet_node_ha_member m SET ready=true,status_reason=''
FROM fleet_node_ha_transfer t, fleet_node_ha_connection c, fleet_node_ha_group g, fleet_node fn
WHERE m.node_id=sqlc.arg('node_id') AND m.org_id=sqlc.arg('org_id') AND m.revoked_at IS NULL AND NOT m.ready
 AND t.recipient_node_id=m.node_id AND t.operation_id=sqlc.arg('operation_id') AND t.expires_at>clock_timestamp()
 AND c.node_id=m.node_id AND c.org_id=m.org_id AND c.control_session_id=sqlc.arg('control_session_id')
 AND g.owner_node_id=m.owner_node_id AND g.org_id=m.org_id AND c.credential_key_fingerprint=g.credential_key_fingerprint
 AND fn.id=m.node_id AND fn.org_id=m.org_id AND fn.enrollment_status='CONFIRMED' AND fn.deleted_at IS NULL;

-- name: ResetFleetNodeHAMemberOnFingerprintMismatch :exec
UPDATE fleet_node_ha_member m SET ready=false,status_reason=CASE WHEN EXISTS (SELECT 1 FROM fleet_node_ha_transfer t WHERE t.recipient_node_id=m.node_id AND t.failed) THEN m.status_reason ELSE 'preparing credential key' END
FROM fleet_node_ha_group g, fleet_node_ha_connection c
WHERE m.node_id=$1 AND m.org_id=$2 AND m.revoked_at IS NULL
 AND g.owner_node_id=m.owner_node_id AND g.org_id=m.org_id
 AND c.node_id=m.node_id AND c.org_id=m.org_id
 AND c.credential_key_fingerprint<>g.credential_key_fingerprint;

-- name: FailFleetNodeHATransfer :exec
UPDATE fleet_node_ha_transfer SET failed=true WHERE recipient_node_id=$1 AND operation_id=$2;

-- name: SetFleetNodeHAMemberFailure :exec
UPDATE fleet_node_ha_member SET status_reason=sqlc.arg('status_reason') WHERE node_id=sqlc.arg('node_id') AND org_id=sqlc.arg('org_id') AND revoked_at IS NULL;
