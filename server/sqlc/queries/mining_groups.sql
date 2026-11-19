-- name: EnsureMiningGroupState :exec
INSERT INTO container_mining_group_state(device_id,organization_id) VALUES($1,$2) ON CONFLICT(device_id) DO NOTHING;

-- name: GetMiningGroupState :one
SELECT * FROM container_mining_group_state WHERE device_id=$1 AND organization_id=$2;

-- name: LockMiningGroupState :one
SELECT * FROM container_mining_group_state WHERE device_id=$1 AND organization_id=$2 FOR UPDATE;

-- name: SaveMiningGroupState :exec
UPDATE container_mining_group_state SET observed=$3,observed_at=$4,bindings=$5,operation_id=$6,actor_id=$7,operation_kind=$8,status=$9,message=$10,step=$11,step_key=$12,controller_operation_id=$13,write_epoch=$14,attempt_id=$15,revision=$16,retry_count=$17,next_reconcile_at=$18,activation_resume_mining=$19,activation_stop_requested=$20,transfer_failure_count=$21,updated_at=CURRENT_TIMESTAMP
WHERE device_id=$1 AND organization_id=$2;

-- name: ListMiningGroupControllers :many
SELECT s.device_id,s.organization_id,d.device_identifier,s.attempt_id,s.retry_count,false::boolean AS readiness
FROM container_mining_group_state s JOIN device d ON d.id=s.device_id
WHERE d.deleted_at IS NULL AND s.status IN ('pending','applying','blocked') AND s.next_reconcile_at<=CURRENT_TIMESTAMP
ORDER BY s.next_reconcile_at,s.device_id LIMIT 1000;

-- name: DiscoverMiningGroupControllers :exec
-- Discover paired container inventory once per minute, including controllers
-- whose mining groups were enabled outside Fleet and have no allocations.
INSERT INTO container_mining_group_state(device_id,organization_id)
SELECT d.id,d.org_id
FROM discovered_device dd JOIN device d ON d.discovered_device_id=dd.id
JOIN device_pairing dp ON dp.device_id=d.id AND dp.pairing_status IN ('PAIRED','DEFAULT_PASSWORD')
WHERE dd.deleted_at IS NULL AND dd.device_class=2 AND d.deleted_at IS NULL
ON CONFLICT(device_id) DO NOTHING;

-- name: ListMiningGroupReadinessControllers :many
-- Frequent due-work checks only scan tracked controllers and allocated boards.
-- Interleave organizations so a large fleet cannot hide another organization's
-- due controllers behind the page limit. Within each organization, oldest wins.
WITH controllers AS MATERIALIZED (
 SELECT organization_id,device_id FROM container_mining_group_state
 UNION
 SELECT organization_id,device_id FROM hosted_account_asset
 WHERE kind=7 AND client_id IS NOT NULL AND device_id IS NOT NULL
), due AS (
 SELECT d.id AS device_id,d.org_id AS organization_id,d.device_identifier,s.attempt_id,COALESCE(s.retry_count,0)::integer AS retry_count,true::boolean AS readiness,
  ROW_NUMBER() OVER (PARTITION BY d.org_id ORDER BY COALESCE(s.next_reconcile_at,'-infinity'::timestamptz),d.id) AS organization_rank,
  COALESCE(s.next_reconcile_at,'-infinity'::timestamptz) AS due_at
 FROM controllers a JOIN device d ON d.id=a.device_id AND d.org_id=a.organization_id
 JOIN discovered_device dd ON dd.id=d.discovered_device_id AND dd.deleted_at IS NULL AND dd.device_class=2
 JOIN device_pairing dp ON dp.device_id=d.id AND dp.pairing_status IN ('PAIRED','DEFAULT_PASSWORD')
 LEFT JOIN container_mining_group_state s ON s.device_id=d.id AND s.organization_id=d.org_id
 WHERE d.deleted_at IS NULL AND COALESCE(s.next_reconcile_at,'-infinity'::timestamptz)<=CURRENT_TIMESTAMP
  AND COALESCE(s.status,'idle') NOT IN ('pending','applying','blocked')
  AND (s.observed_at IS NULL OR s.observed_at<=CURRENT_TIMESTAMP-INTERVAL '1 minute' OR s.observed_at>CURRENT_TIMESTAMP+INTERVAL '5 seconds')
)
SELECT device_id,organization_id,device_identifier,attempt_id,retry_count,readiness
FROM due ORDER BY organization_rank,due_at,organization_id,device_id LIMIT 1000;

-- name: ScheduleMiningGroupReconciliation :exec
-- A successful RPC can still leave a blocked operation (including FAILED
-- transfers). Only a successful reconciliation outcome resets the backoff.
-- Guard the workflow identity so delayed bookkeeping cannot affect a new request.
UPDATE container_mining_group_state
SET retry_count=CASE WHEN sqlc.arg(failed)::boolean OR status='blocked' THEN LEAST(retry_count+1,7) ELSE 0 END,
 next_reconcile_at=clock_timestamp() +
   (CASE WHEN sqlc.arg(failed)::boolean OR status='blocked' THEN sqlc.arg(failure_delay_ms)::bigint
    WHEN status NOT IN ('pending','applying','blocked') THEN 60000 ELSE 0 END) * INTERVAL '1 millisecond'
WHERE device_id=sqlc.arg(device_id) AND organization_id=sqlc.arg(organization_id)
 AND attempt_id IS NOT DISTINCT FROM sqlc.narg(attempt_id)::uuid;

-- name: GetMiningGroupBoards :many
SELECT h.id AS hashboard_id,i.id AS installation_id,i.module_id,COALESCE(i.metadata->>'macAddress','')::text AS mac_address,
 t.device_id,d.device_identifier,d.site_id,m.reported_id AS module_name,i.position
FROM hashboard h JOIN hashboard_installation i ON i.hashboard_id=h.id AND i.removed_at IS NULL
JOIN container_module m ON m.id=i.module_id JOIN container_tank t ON t.id=m.tank_id
JOIN device d ON d.id=t.device_id JOIN hashboard_slot s ON s.module_id=m.id AND s.position=i.position AND s.hashboard_id=h.id
WHERE h.org_id=sqlc.arg(organization_id) AND d.org_id=sqlc.arg(organization_id)
 AND h.id=ANY(sqlc.arg(hashboard_ids)::bigint[]) AND d.deleted_at IS NULL AND m.retired_at IS NULL AND t.present
 AND s.occupancy='occupied' AND s.unavailable_reason='' AND s.observed_at>now()-INTERVAL '2 minutes' AND s.observed_at<=now()+INTERVAL '5 seconds'
 AND i.metadata->>'macAddress'<>'' AND i.site_id IS NOT DISTINCT FROM d.site_id
ORDER BY h.id;

-- name: ListHashboardPoolIntents :many
SELECT * FROM hashboard_pool_intent WHERE organization_id=$1 AND device_id=$2 ORDER BY hashboard_id;

-- name: SetHashboardPoolIntent :exec
INSERT INTO hashboard_pool_intent(organization_id,hashboard_id,device_id,installation_id,mac_address,account_id,pool_revision_id,operation_id)
VALUES($1,$2,$3,$4,$5,$6,$7,$8)
ON CONFLICT(organization_id,hashboard_id) DO UPDATE SET device_id=EXCLUDED.device_id,installation_id=EXCLUDED.installation_id,mac_address=EXCLUDED.mac_address,account_id=EXCLUDED.account_id,pool_revision_id=EXCLUDED.pool_revision_id,operation_id=EXCLUDED.operation_id,updated_at=CURRENT_TIMESTAMP;

-- name: GetHostPoolAccount :one
SELECT * FROM hosted_account WHERE organization_id=$1 AND depth=0 AND NOT suspended;

-- name: DeleteUnappliedHashboardPoolIntents :exec
-- Only after confirmed terminal transfer failures and a fresh observation with
-- no pending controller work. Retain the assignments verified on the controller;
-- abandoned desires must not be retried by a subsequent unrelated request.
DELETE FROM hashboard_pool_intent
WHERE organization_id=sqlc.arg(organization_id) AND device_id=sqlc.arg(device_id)
 AND NOT (mac_address=ANY(sqlc.arg(applied_macs)::text[]));

-- name: GetHostMiningGroupPoolRevisionForReconciliation :one
-- Archiving prevents new assignments but preserves revisions used by accepted
-- workflows and existing controller bindings. Account restrictions still apply.
SELECT r.* FROM hosted_account_pool_revision r JOIN hosted_account a ON a.id=r.account_id AND a.organization_id=r.organization_id
JOIN hosted_account_pool p ON p.pool_id=r.pool_id AND p.organization_id=r.organization_id AND p.account_id=r.account_id
WHERE r.organization_id=$1 AND r.revision_id=$2 AND a.depth=0 AND NOT a.suspended;

-- name: GetMiningGroupWorkflowAttempt :one
SELECT a.* FROM device_write_attempt a JOIN container_mining_group_state s ON s.attempt_id=a.attempt_id AND s.device_id=a.device_id AND s.organization_id=a.organization_id
WHERE a.attempt_id=$1 AND a.device_id=$2 AND a.organization_id=$3 AND a.status='running' AND s.status IN ('pending','applying','blocked');

-- name: GetMiningGroupRequest :one
SELECT * FROM mining_group_request WHERE organization_id=$1 AND request_id=$2;

-- name: RecordMiningGroupRequest :exec
INSERT INTO mining_group_request(organization_id,request_id,device_id,fingerprint) VALUES($1,$2,$3,$4);

-- name: GetMiningGroupRequestActivity :one
-- Outcomes retain the actor and site captured at admission, even after the
-- operator is renamed/deleted or access and device placement change.
SELECT * FROM activity_log
WHERE event_id=sqlc.arg(event_id) AND organization_id=sqlc.arg(organization_id)::bigint;

-- name: ListHostMiningGroupPoolRevisions :many
SELECT r.* FROM hosted_account_pool_revision r JOIN hosted_account a ON a.id=r.account_id AND a.organization_id=r.organization_id
JOIN hosted_account_pool p ON p.pool_id=r.pool_id AND p.account_id=r.account_id AND p.organization_id=r.organization_id
WHERE r.organization_id=$1 AND a.depth=0 AND NOT a.suspended AND NOT p.archived ORDER BY r.created_at DESC LIMIT 1000;

-- name: GetMiningGroupTarget :one
SELECT d.id,d.site_id,f.epoch FROM device d JOIN device_write_fence f ON f.device_id=d.id AND f.organization_id=d.org_id
WHERE d.org_id=$1 AND d.device_identifier=$2 AND d.deleted_at IS NULL;

-- name: PruneDetachedHashboardPoolIntents :exec
-- Called only while admitting new intent or settling an interrupted workflow
-- after all controller work is known to be terminal.
-- A new board/installation never inherits the previous occupant's intent.
DELETE FROM hashboard_pool_intent p WHERE p.organization_id=$1 AND p.device_id=$2
AND NOT EXISTS (
 SELECT 1 FROM hashboard_installation i JOIN container_module m ON m.id=i.module_id
 JOIN container_tank t ON t.id=m.tank_id
 WHERE i.id=p.installation_id AND i.hashboard_id=p.hashboard_id AND i.removed_at IS NULL
 AND m.retired_at IS NULL AND t.device_id=p.device_id AND i.metadata->>'macAddress'=p.mac_address
);

-- name: HasDetachedHashboardPoolIntents :one
-- Stale telemetry alone is not evidence of removal. Match the exact saved
-- installation, controller and MAC without imposing a freshness requirement.
SELECT EXISTS (
 SELECT 1 FROM hashboard_pool_intent p WHERE p.organization_id=$1 AND p.device_id=$2
 AND NOT EXISTS (
  SELECT 1 FROM hashboard_installation i JOIN container_module m ON m.id=i.module_id
  JOIN container_tank t ON t.id=m.tank_id
  WHERE i.id=p.installation_id AND i.hashboard_id=p.hashboard_id AND i.removed_at IS NULL
  AND m.retired_at IS NULL AND t.device_id=p.device_id AND i.metadata->>'macAddress'=p.mac_address
 )
);
