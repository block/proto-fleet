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

-- name: ListFleetNodeHAMembers :many
SELECT m.*,fn.name,fn.encryption_pubkey,fn.enrollment_status,
c.instance_id,c.control_session_id,c.credential_key_fingerprint,c.seen_at,c.ha_capable
FROM fleet_node_ha_member m JOIN fleet_node fn ON fn.id=m.node_id AND fn.org_id=m.org_id
LEFT JOIN fleet_node_ha_connection c ON c.node_id=m.node_id AND c.org_id=m.org_id
WHERE m.owner_node_id=$1 AND m.org_id=$2 AND m.revoked_at IS NULL
ORDER BY m.joined_at,m.node_id;

-- name: AcquireOrRenewFleetNodeHALease :one
-- Only acknowledgement of current authority renews it. An expired connection
-- tuple must reconnect before becoming a candidate again.
UPDATE fleet_node_ha_group g SET
 active_node_id=sqlc.arg('node_id'), instance_id=sqlc.arg('instance_id'),control_session_id=sqlc.arg('control_session_id'),
 lease_epoch=CASE WHEN g.active_node_id=sqlc.arg('node_id') AND g.instance_id=sqlc.arg('instance_id') AND g.control_session_id=sqlc.arg('control_session_id') AND g.lease_expires_at>clock_timestamp() THEN g.lease_epoch ELSE g.lease_epoch+1 END,
 acknowledged=CASE WHEN g.active_node_id=sqlc.arg('node_id') AND g.instance_id=sqlc.arg('instance_id') AND g.control_session_id=sqlc.arg('control_session_id') AND g.lease_expires_at>clock_timestamp() THEN (g.acknowledged OR sqlc.arg('acknowledged_epoch')=g.lease_epoch) ELSE false END,
 lease_expires_at=CASE WHEN g.active_node_id=sqlc.arg('node_id') AND g.instance_id=sqlc.arg('instance_id') AND g.control_session_id=sqlc.arg('control_session_id') AND g.lease_expires_at>clock_timestamp() AND sqlc.arg('acknowledged_epoch')<>g.lease_epoch THEN g.lease_expires_at ELSE clock_timestamp()+interval '30 seconds' END
WHERE g.owner_node_id=sqlc.arg('owner_node_id') AND g.org_id=sqlc.arg('org_id') AND g.enabled
 AND EXISTS (SELECT 1 FROM fleet_node_ha_member m JOIN fleet_node fn ON fn.id=m.node_id AND fn.org_id=m.org_id JOIN fleet_node_ha_connection c ON c.node_id=m.node_id AND c.org_id=m.org_id
 WHERE m.node_id=sqlc.arg('node_id') AND m.owner_node_id=g.owner_node_id AND m.ready AND m.revoked_at IS NULL AND fn.enrollment_status='CONFIRMED' AND fn.deleted_at IS NULL AND c.instance_id=sqlc.arg('instance_id') AND c.control_session_id=sqlc.arg('control_session_id') AND c.ha_capable AND c.credential_key_fingerprint=g.credential_key_fingerprint)
 AND ((g.active_node_id=sqlc.arg('node_id') AND g.instance_id=sqlc.arg('instance_id') AND g.control_session_id=sqlc.arg('control_session_id') AND g.lease_expires_at>clock_timestamp())
 OR (g.lease_epoch=0 AND sqlc.arg('node_id')=g.owner_node_id AND g.lease_expires_at>clock_timestamp())
 OR (g.lease_expires_at<=clock_timestamp() AND sqlc.arg('node_id')=(SELECT m.node_id FROM fleet_node_ha_member m JOIN fleet_node fn ON fn.id=m.node_id AND fn.org_id=m.org_id JOIN fleet_node_ha_connection c ON c.node_id=m.node_id AND c.org_id=m.org_id WHERE m.owner_node_id=g.owner_node_id AND m.org_id=g.org_id AND m.ready AND m.revoked_at IS NULL AND fn.enrollment_status='CONFIRMED' AND fn.deleted_at IS NULL AND c.ha_capable AND c.credential_key_fingerprint=g.credential_key_fingerprint AND c.seen_at>clock_timestamp()-interval '10 seconds' AND NOT (m.node_id=g.active_node_id AND c.instance_id=g.instance_id AND c.control_session_id=g.control_session_id) ORDER BY m.joined_at,m.node_id LIMIT 1)))
RETURNING g.*;

-- name: RevokeFleetNodeHAMember :exec
UPDATE fleet_node_ha_member SET revoked_at=clock_timestamp(),ready=false,status_reason='revoked' WHERE node_id=$1 AND org_id=$2;

-- name: ExpireFleetNodeHALease :exec
-- Deny routing immediately, but retain the old process expiry before promotion.
UPDATE fleet_node_ha_group SET acknowledged=false WHERE active_node_id=$1 AND org_id=$2;

-- name: LockFleetNodeHAGroup :one
SELECT * FROM fleet_node_ha_group WHERE owner_node_id=$1 AND org_id=$2 FOR UPDATE;

-- name: ConfirmFleetNodeHAMemberFingerprint :execrows
UPDATE fleet_node_ha_member m SET ready=true,status_reason=''
FROM fleet_node_ha_group g, fleet_node_ha_connection c, fleet_node fn
WHERE m.node_id=sqlc.arg('node_id') AND m.org_id=sqlc.arg('org_id') AND m.revoked_at IS NULL AND NOT m.ready
 AND c.node_id=m.node_id AND c.org_id=m.org_id AND c.control_session_id=sqlc.arg('control_session_id')
 AND g.owner_node_id=m.owner_node_id AND g.org_id=m.org_id AND c.credential_key_fingerprint=g.credential_key_fingerprint
 AND fn.id=m.node_id AND fn.org_id=m.org_id AND fn.enrollment_status='CONFIRMED' AND fn.deleted_at IS NULL;

-- name: SetFleetNodeHAMemberFailure :exec
UPDATE fleet_node_ha_member SET status_reason=sqlc.arg('status_reason') WHERE node_id=sqlc.arg('node_id') AND org_id=sqlc.arg('org_id') AND revoked_at IS NULL AND NOT ready;

-- name: EnableFleetNodeHAGroup :execrows
-- Reserve the first grant for the original source while it reconnects. Epoch 0
-- and the empty incarnation/session never authorize commands or reports.
UPDATE fleet_node_ha_group g SET enabled=true,active_node_id=g.owner_node_id,
 lease_expires_at=clock_timestamp()+interval '30 seconds'
WHERE g.owner_node_id=$1 AND g.org_id=$2 AND NOT g.enabled
 AND EXISTS (SELECT 1 FROM fleet_node_ha_member m JOIN fleet_node fn ON fn.id=m.node_id AND fn.org_id=m.org_id
 WHERE m.owner_node_id=g.owner_node_id AND m.org_id=g.org_id AND m.node_id<>g.owner_node_id
 AND m.ready AND m.revoked_at IS NULL AND fn.enrollment_status='CONFIRMED' AND fn.deleted_at IS NULL);

-- name: GetFleetNodeHARoute :one
SELECT fn.id AS owner_node_id, fn.org_id, fn.enrollment_status,
 COALESCE(m.owner_node_id,fn.id)::bigint AS member_owner_node_id,
 COALESCE(g.enabled,false)::boolean AS enabled,
 CASE WHEN g.enabled THEN g.active_node_id ELSE fn.id END::bigint AS physical_node_id,
 COALESCE(g.lease_epoch,0)::bigint AS lease_epoch,
 COALESCE(g.instance_id,'')::text AS instance_id,
 COALESCE(g.control_session_id,'')::text AS control_session_id,
 COALESCE(g.lease_expires_at,'1970-01-01 00:00:00+00'::timestamptz)::timestamptz AS lease_expires_at,
 COALESCE(g.acknowledged,false)::boolean AS acknowledged,
 COALESCE(physical.encryption_pubkey,fn.encryption_pubkey)::bytea AS encryption_pubkey,
 COALESCE(physical.enrollment_status,'')::text AS physical_enrollment_status
FROM fleet_node fn
LEFT JOIN fleet_node_ha_member m ON m.node_id=fn.id AND m.org_id=fn.org_id
LEFT JOIN fleet_node_ha_group g ON g.owner_node_id=fn.id AND g.org_id=fn.org_id
LEFT JOIN fleet_node physical ON physical.id=CASE WHEN g.enabled THEN g.active_node_id ELSE fn.id END AND physical.org_id=fn.org_id AND physical.deleted_at IS NULL
WHERE fn.id=$1 AND (fn.org_id=sqlc.arg('org_id') OR sqlc.arg('org_id')=0) AND fn.deleted_at IS NULL;

-- name: ListFleetNodeHAState :many
SELECT fn.id AS node_id, fn.enrollment_status,
 COALESCE(m.owner_node_id,0)::bigint AS owner_node_id,
 COALESCE(m.ready,false)::boolean AS ready,
 COALESCE(m.status_reason,'')::text AS status_reason,m.revoked_at,
 COALESCE(g.enabled,false)::boolean AS enabled,
 COALESCE(g.active_node_id,0)::bigint AS active_node_id,
 COALESCE(g.lease_epoch,0)::bigint AS lease_epoch,
 COALESCE(g.instance_id,'')::text AS instance_id,
 COALESCE(g.control_session_id,'')::text AS control_session_id,
 COALESCE(g.lease_expires_at,'1970-01-01 00:00:00+00'::timestamptz)::timestamptz AS lease_expires_at,
 COALESCE(g.acknowledged,false)::boolean AS acknowledged,
 COALESCE(g.credential_key_fingerprint,''::bytea)::bytea AS credential_key_fingerprint
FROM fleet_node fn
LEFT JOIN fleet_node_ha_member m ON m.node_id=fn.id AND m.org_id=fn.org_id
LEFT JOIN fleet_node_ha_group g ON g.owner_node_id=m.owner_node_id AND g.org_id=m.org_id
WHERE fn.org_id=$1 AND fn.deleted_at IS NULL;
