-- name: ListPendingPoolLifecycleTransitions :many
-- Prepare one policy per controller and let accepted work dispatch first.
SELECT * FROM (
 SELECT DISTINCT ON (l.device_id) l.* FROM pool_lifecycle_transition l
 LEFT JOIN device_write_attempt w ON w.attempt_id=l.physical_attempt_id
 WHERE (l.state IN ('pending','ready') OR (l.state IN ('applying','uncertain') AND l.configuration_id IS NULL AND l.target_kind='device' AND w.status='complete'))
  AND l.next_attempt_at<=clock_timestamp()
  AND NOT EXISTS(SELECT 1 FROM pool_configuration_assignment a JOIN pool_configuration_operation_target t
   ON t.organization_id=a.organization_id AND t.operation_id=a.operation_id AND t.target_kind=a.target_kind AND t.target_id=a.target_id AND t.assignment_version=a.version
   WHERE a.organization_id=l.organization_id AND a.device_id=l.device_id AND a.retired_at IS NULL AND t.state IN ('pending','applying'))
 ORDER BY l.device_id,l.next_attempt_at,l.created_at
) pending ORDER BY next_attempt_at,created_at LIMIT 100;

-- name: GetPoolLifecycleTransition :one
SELECT t.*,COALESCE(a.account_id,'')::text AS external_account_id,COALESCE(a.name,'')::text AS account_name
FROM pool_lifecycle_transition t LEFT JOIN hosted_account a ON a.organization_id=t.organization_id AND a.id=t.owner_account_id
WHERE t.transition_id=$1;

-- name: DeferPoolLifecycleTransition :exec
UPDATE pool_lifecycle_transition SET next_attempt_at=clock_timestamp()+interval '30 seconds',result_code=$2,updated_at=clock_timestamp()
WHERE transition_id=$1 AND state IN ('pending','ready','applying','uncertain');

-- name: FinishPoolLifecycleTransition :execrows
UPDATE pool_lifecycle_transition SET state=$2,result_code=$3,updated_at=clock_timestamp() WHERE transition_id=$1 AND state<>'superseded';

-- name: PreparePoolLifecycleTransition :execrows
UPDATE pool_lifecycle_transition SET target_kind=$2,target_id=$3,reviewed_binding=$4,state='ready',updated_at=clock_timestamp()
WHERE transition_id=$1 AND state='pending';

-- name: LinkPoolLifecycleOperation :exec
UPDATE pool_lifecycle_transition SET operation_id=$2,state='applying',updated_at=clock_timestamp() WHERE transition_id=$1 AND state='ready';

-- name: CreatePoolLifecycleBoardTransition :exec
INSERT INTO pool_lifecycle_transition(organization_id,device_id,asset_kind,asset_id,owner_account_id,allocation_revision,reason,configuration_id,revision_id,created_at)
SELECT p.organization_id,p.device_id,7,sqlc.arg(asset_id)::bigint,sqlc.arg(owner_account_id)::bigint,sqlc.arg(allocation_revision)::bigint,'pairing',
 p.pairing_defaults->(sqlc.arg(owner_account_id)::bigint::text)->>'configuration_id',p.pairing_defaults->(sqlc.arg(owner_account_id)::bigint::text)->>'revision_id',p.created_at
FROM pool_lifecycle_transition p WHERE p.transition_id=sqlc.arg(parent_id)::uuid
 AND NOT EXISTS(SELECT 1 FROM pool_lifecycle_transition newer WHERE newer.organization_id=p.organization_id AND newer.device_id=p.device_id
  AND newer.asset_kind=7 AND newer.asset_id=sqlc.arg(asset_id)::bigint AND newer.reason='ownership' AND newer.created_at>=p.created_at);

-- name: GetPoolLifecycleAllocation :one
SELECT COALESCE(a.subclient_id,a.client_id,0)::bigint AS owner_account_id,COALESCE(a.revision,0)::bigint AS revision
FROM (SELECT 1) seed LEFT JOIN hosted_account_asset a ON a.organization_id=$1 AND a.kind=$2 AND a.asset_id=$3 AND a.device_id=$4;

-- name: CreateAutomaticPoolConfigurationOperation :one
INSERT INTO pool_configuration_operation(operation_id,organization_id,configuration_id,revision_id,actor_id,lifecycle_id)
VALUES($1,$2,$3,$4,0,$5) RETURNING *;

-- name: SupersedeContainerPoolLifecycleTransitions :exec
UPDATE pool_lifecycle_transition SET state='superseded',updated_at=clock_timestamp()
WHERE organization_id=$1 AND device_id=$2 AND owner_account_id=$3 AND transition_id<>$4
 AND state='pending';

-- name: ReconcilePoolLifecycleOperations :exec
UPDATE pool_lifecycle_transition l SET state=CASE WHEN t.state='verified' THEN 'verified' ELSE 'failed' END,
 result_code=t.result_code,updated_at=clock_timestamp()
FROM pool_configuration_operation_target t WHERE l.operation_id=t.operation_id AND l.state='applying'
 AND t.state NOT IN ('pending','applying');

-- name: ListVisiblePoolLifecycleTransitions :many
SELECT l.*,COALESCE(c.name,'')::text AS configuration_name,
 COALESCE(NULLIF(l.target_kind,''),CASE WHEN l.asset_kind=7 THEN 'hashboard_installation' ELSE 'device' END)::text AS display_kind,
 COALESCE(NULLIF(l.target_id,0),CASE WHEN l.asset_kind=7 THEN i.id ELSE l.device_id END)::bigint AS display_id
FROM pool_lifecycle_transition l
LEFT JOIN pool_configuration c ON c.organization_id=l.organization_id AND c.configuration_id=l.configuration_id
LEFT JOIN hashboard_installation i ON l.asset_kind=7 AND i.hashboard_id=l.asset_id AND i.device_id=l.device_id AND i.removed_at IS NULL
LEFT JOIN hosted_account_asset owner ON owner.organization_id=l.organization_id AND owner.kind=l.asset_kind AND owner.asset_id=l.asset_id
WHERE l.organization_id=sqlc.arg(organization_id) AND l.owner_account_id=sqlc.arg(owner_account_id)::bigint
 AND (l.device_id=ANY(sqlc.arg(device_ids)::bigint[]) OR i.id=ANY(sqlc.arg(installation_ids)::bigint[]))
 AND l.state<>'superseded' AND (l.asset_kind<>0 OR l.state='pending')
 AND COALESCE(owner.subclient_id,owner.client_id,0)=l.owner_account_id AND COALESCE(owner.revision,0)=l.allocation_revision
 AND NOT EXISTS(SELECT 1 FROM pool_configuration_assignment a WHERE a.organization_id=l.organization_id
  AND a.device_id=l.device_id AND a.target_kind=COALESCE(NULLIF(l.target_kind,''),CASE WHEN l.asset_kind=7 THEN 'hashboard_installation' ELSE 'device' END)
  AND a.target_id=COALESCE(NULLIF(l.target_id,0),CASE WHEN l.asset_kind=7 THEN i.id ELSE l.device_id END)
  AND a.operation_id IS DISTINCT FROM l.operation_id AND a.updated_at>l.created_at)
ORDER BY l.created_at DESC,l.transition_id DESC;

-- name: PoolConfigurationHasPendingLifecycle :one
SELECT (EXISTS(SELECT 1 FROM pool_lifecycle_transition l WHERE l.organization_id=$1 AND l.configuration_id=$2 AND l.state IN ('pending','ready','applying'))
 OR EXISTS(SELECT 1 FROM pool_lifecycle_transition p,jsonb_each(p.pairing_defaults) entry
 WHERE p.organization_id=$1 AND p.asset_kind=0 AND p.state='pending' AND entry.value->>'configuration_id'=$2))::boolean AS pending;

-- name: PoolLifecycleHasNewerAssignment :one
SELECT EXISTS(SELECT 1 FROM pool_configuration_assignment a JOIN pool_lifecycle_transition l ON l.transition_id=$1
 WHERE a.organization_id=l.organization_id AND a.device_id=l.device_id
 AND a.operation_id IS DISTINCT FROM l.operation_id AND a.updated_at>l.created_at
 AND ((a.target_kind='device') OR (a.target_kind='hashboard_installation' AND EXISTS(SELECT 1 FROM hashboard_installation i WHERE i.id=a.target_id AND i.hashboard_id=l.asset_id AND l.asset_kind=7))));

-- name: RetryUndispatchedPoolLifecycleRemoval :exec
-- Retry only proven no-write attempts. A dispatched or unknown clear is never replayed.
UPDATE pool_lifecycle_transition l SET state='ready',physical_attempt_id=NULL,next_attempt_at=clock_timestamp()+interval '30 seconds',updated_at=clock_timestamp()
FROM device_write_attempt w WHERE l.transition_id=$1 AND l.physical_attempt_id=w.attempt_id
 AND l.state='failed' AND l.configuration_id IS NULL AND w.status='complete' AND w.write_outcome='{}'::jsonb;
