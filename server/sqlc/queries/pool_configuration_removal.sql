-- Removal-specific preflight is stored in the shared immutable review target.
-- The operation and target records own execution, progress and recovery.
-- name: GetPoolRemoval :one
SELECT r.review_id::uuid AS removal_id,r.organization_id,c.account_id,r.actor_id,
 (r.targets->0->'Change'->'Target'->>'Kind')::text AS target_kind,
 (r.targets->0->'Change'->'Target'->>'ID')::bigint AS target_id,
 (r.targets->0->'Change'->'Target'->>'DeviceID')::bigint AS device_id,
 r.configuration_id,(r.targets->0->>'RevisionID')::varchar(36) AS revision_id,
 (r.targets->0->'Change'->>'ExpectedVersion')::bigint AS assignment_version,
 (r.targets->0->>'Binding')::text AS reviewed_binding,
 (r.targets->0->>'VerificationState')::text AS verification_state,
 (r.targets->0->>'ClearSupported')::boolean AS clear_supported,
 COALESCE(o.kind='unassign',false)::boolean AS keep_device_settings,
 COALESCE(t.state,'reviewed')::text AS state,COALESCE(t.result_code,'')::text AS result_code,
 t.physical_attempt_id,r.created_at,COALESCE(t.updated_at,r.created_at)::timestamptz AS updated_at,
 COALESCE(w.status,'')::text AS attempt_status
FROM pool_configuration_review r
JOIN pool_configuration c ON c.configuration_id=r.configuration_id AND c.organization_id=r.organization_id
LEFT JOIN pool_configuration_operation o ON o.operation_id=r.operation_id AND o.organization_id=r.organization_id
LEFT JOIN pool_configuration_operation_target t ON t.operation_id=o.operation_id AND t.organization_id=o.organization_id
LEFT JOIN device_write_attempt w ON w.attempt_id=t.physical_attempt_id
WHERE r.kind='remove' AND r.review_id=sqlc.arg(removal_id)::uuid::text AND r.organization_id=sqlc.arg(organization_id)::bigint AND c.account_id IS NOT DISTINCT FROM sqlc.narg(account_id)::bigint AND r.actor_id=sqlc.arg(actor_id)::bigint
UNION ALL
SELECT * FROM pool_lifecycle_removal WHERE removal_id=sqlc.arg(removal_id)::uuid AND organization_id=sqlc.arg(organization_id)::bigint
 AND account_id IS NOT DISTINCT FROM sqlc.narg(account_id)::bigint AND actor_id=sqlc.arg(actor_id)::bigint;

-- name: EnqueuePoolRemoval :execrows
WITH operation AS (
 INSERT INTO pool_configuration_operation(operation_id,organization_id,configuration_id,revision_id,actor_id,kind)
 SELECT r.review_id,r.organization_id,r.configuration_id,(r.targets->0->>'RevisionID')::varchar(36),r.actor_id,
 CASE WHEN sqlc.arg(keep_device_settings)::boolean THEN 'unassign' ELSE 'clear' END
 FROM pool_configuration_review r WHERE r.review_id=sqlc.arg(review_id) AND r.kind='remove'
  AND r.saved_version IS NULL AND r.expires_at>clock_timestamp()
 RETURNING *
), target AS (
 INSERT INTO pool_configuration_operation_target(organization_id,operation_id,target_kind,target_id,assignment_version,reviewed_binding)
 SELECT o.organization_id,o.operation_id,r.targets->0->'Change'->'Target'->>'Kind',
  (r.targets->0->'Change'->'Target'->>'ID')::bigint,(r.targets->0->'Change'->>'ExpectedVersion')::bigint,r.targets->0->>'Binding'
 FROM operation o JOIN pool_configuration_review r ON r.review_id=o.operation_id
 RETURNING operation_id
)
UPDATE pool_configuration_review r SET operation_id=o.operation_id,saved_revision_id=o.revision_id,saved_version=r.configuration_version
FROM operation o WHERE r.review_id=o.operation_id AND EXISTS(SELECT 1 FROM target);

-- name: AcceptPoolRemoval :one
WITH lifecycle AS (UPDATE pool_lifecycle_transition SET state='applying',physical_attempt_id=sqlc.narg(physical_attempt_id)::uuid,updated_at=clock_timestamp() WHERE transition_id=sqlc.arg(removal_id)::uuid AND state='ready' AND configuration_id IS NULL RETURNING 1),
manual AS (
UPDATE pool_configuration_operation_target SET state='applying',physical_attempt_id=sqlc.narg(physical_attempt_id)::uuid,attempts=attempts+1,updated_at=clock_timestamp()
WHERE operation_id=sqlc.arg(removal_id)::uuid::text AND state='pending' RETURNING 1)
SELECT ((SELECT count(*) FROM lifecycle)+(SELECT count(*) FROM manual))::bigint AS changed;

-- name: FinishPoolRemoval :one
WITH lifecycle AS (UPDATE pool_lifecycle_transition SET state=sqlc.arg(state)::text,result_code=sqlc.arg(result_code)::text,updated_at=clock_timestamp() WHERE transition_id=sqlc.arg(removal_id)::uuid AND state IN ('ready','applying','uncertain') AND configuration_id IS NULL RETURNING 1),
manual AS (
UPDATE pool_configuration_operation_target SET state=sqlc.arg(state)::text,result_code=sqlc.arg(result_code)::text,updated_at=clock_timestamp()
WHERE operation_id=sqlc.arg(removal_id)::uuid::text AND state IN ('pending','applying','uncertain') RETURNING 1)
SELECT ((SELECT count(*) FROM lifecycle)+(SELECT count(*) FROM manual))::bigint AS changed;

-- name: ListActivePoolRemovals :many
SELECT o.operation_id::uuid AS removal_id,t.physical_attempt_id FROM pool_configuration_operation o JOIN pool_configuration_operation_target t USING(operation_id) JOIN pool_configuration_assignment a ON a.organization_id=t.organization_id AND a.target_kind=t.target_kind AND a.target_id=t.target_id WHERE o.kind IN ('clear','unassign') AND o.organization_id=$1 AND a.device_id=$2 AND t.state IN ('pending','applying','uncertain')
UNION ALL
SELECT transition_id AS removal_id,physical_attempt_id FROM pool_lifecycle_transition WHERE organization_id=$1 AND device_id=$2 AND configuration_id IS NULL AND target_id>0 AND state IN ('ready','applying','uncertain');

-- name: ResolveRecoveredPoolRemoval :exec
WITH lifecycle AS (
 UPDATE pool_lifecycle_transition SET state='failed',result_code='interrupted',updated_at=clock_timestamp()
 WHERE organization_id=$1 AND device_id=$2 AND physical_attempt_id=$3 AND configuration_id IS NULL AND state IN ('ready','applying','uncertain') RETURNING 1
)
UPDATE pool_configuration_operation_target t SET state='failed',result_code='interrupted',updated_at=clock_timestamp()
FROM pool_configuration_operation o,pool_configuration_assignment a
WHERE o.operation_id=t.operation_id AND o.organization_id=t.organization_id AND o.kind IN ('clear','unassign')
 AND a.organization_id=t.organization_id AND a.target_kind=t.target_kind AND a.target_id=t.target_id
 AND t.organization_id=$1 AND a.device_id=$2 AND t.physical_attempt_id=$3 AND t.state IN ('pending','applying','uncertain');

-- name: ListDeviceWriteRecoveryCandidates :many
SELECT w.* FROM device_write_attempt w
WHERE w.organization_id=$1 AND w.device_id=$2 AND (w.status IN ('running','unknown') OR
 (w.status='complete' AND w.operation_kind='mutation' AND w.command_type='UpdateMiningPools' AND (EXISTS(
 SELECT 1 FROM pool_configuration_operation_target t JOIN pool_configuration_operation o ON o.operation_id=t.operation_id
 WHERE t.organization_id=w.organization_id AND t.physical_attempt_id=w.attempt_id
 AND t.target_kind='device' AND o.kind='clear' AND t.state IN ('applying','uncertain')) OR EXISTS(SELECT 1 FROM pool_lifecycle_transition l WHERE l.physical_attempt_id=w.attempt_id AND l.configuration_id IS NULL AND l.target_kind='device' AND l.state IN ('applying','uncertain')))))
ORDER BY w.created_at,w.attempt_id;

-- name: GetUnfinishedCompletedPoolRemoval :one
SELECT o.operation_id::uuid AS removal_id FROM pool_configuration_operation o JOIN pool_configuration_operation_target t USING(operation_id) JOIN device_write_attempt w ON w.attempt_id=t.physical_attempt_id WHERE o.organization_id=$1 AND w.device_id=$2 AND t.physical_attempt_id=$3 AND t.target_kind='device' AND o.kind='clear' AND t.state IN ('applying','uncertain') AND w.status='complete'
UNION ALL
SELECT l.transition_id FROM pool_lifecycle_transition l JOIN device_write_attempt w ON w.attempt_id=l.physical_attempt_id WHERE l.organization_id=$1 AND l.device_id=$2 AND l.physical_attempt_id=$3 AND l.target_kind='device' AND l.configuration_id IS NULL AND l.state IN ('applying','uncertain') AND w.status='complete';

-- name: ResolveCompletedPoolRemoval :one
WITH lifecycle AS (
 UPDATE pool_lifecycle_transition SET state='failed',result_code='interrupted',updated_at=clock_timestamp()
 WHERE transition_id=sqlc.arg(removal_id)::uuid AND organization_id=sqlc.arg(organization_id)::bigint AND device_id=sqlc.arg(device_id)::bigint
 AND physical_attempt_id=sqlc.narg(physical_attempt_id)::uuid AND target_kind='device' AND configuration_id IS NULL AND state IN ('applying','uncertain') RETURNING 1
), manual AS (
UPDATE pool_configuration_operation_target t SET state='failed',result_code='interrupted',updated_at=clock_timestamp()
FROM pool_configuration_operation o,pool_configuration_assignment a
WHERE o.operation_id=t.operation_id AND o.organization_id=t.organization_id AND o.kind='clear'
 AND a.organization_id=t.organization_id AND a.target_kind=t.target_kind AND a.target_id=t.target_id
 AND t.operation_id=sqlc.arg(removal_id)::uuid::text AND t.organization_id=sqlc.arg(organization_id)::bigint AND a.device_id=sqlc.arg(device_id)::bigint AND t.physical_attempt_id=sqlc.narg(physical_attempt_id)::uuid
 AND t.target_kind='device' AND t.state IN ('applying','uncertain') RETURNING 1)
SELECT ((SELECT count(*) FROM lifecycle)+(SELECT count(*) FROM manual))::bigint AS changed;

-- name: GetBoardPoolRemovalAttempt :one
SELECT o.operation_id::uuid AS removal_id,o.actor_id,COALESCE(a.account_id,'')::text AS external_account_id FROM pool_configuration_operation o JOIN pool_configuration_operation_target t USING(operation_id) JOIN pool_configuration c ON c.configuration_id=o.configuration_id LEFT JOIN hosted_account a ON a.id=c.account_id AND a.organization_id=o.organization_id WHERE o.organization_id=$1 AND t.physical_attempt_id=$2 AND t.target_kind='hashboard_installation' AND o.kind='clear' AND t.state IN ('applying','uncertain')
UNION ALL
SELECT l.transition_id AS removal_id,0::bigint AS actor_id,COALESCE(a.account_id,'')::text AS external_account_id FROM pool_lifecycle_transition l LEFT JOIN hosted_account a ON a.id=l.owner_account_id AND a.organization_id=l.organization_id WHERE l.organization_id=$1 AND l.physical_attempt_id=$2 AND l.target_kind='hashboard_installation' AND l.configuration_id IS NULL AND l.state IN ('applying','uncertain');

-- name: ListClearedBoardPoolIntents :many
SELECT o.operation_id::uuid AS removal_id,c.account_id,t.target_id,t.assignment_version,t.reviewed_binding,t.state,t.physical_attempt_id,
 COALESCE(i.metadata->>'macAddress','')::text AS mac_address
FROM pool_configuration_operation o
JOIN pool_configuration_operation_target t ON t.operation_id=o.operation_id AND t.organization_id=o.organization_id
JOIN pool_configuration c ON c.configuration_id=o.configuration_id AND c.organization_id=o.organization_id
JOIN pool_configuration_assignment a ON a.organization_id=t.organization_id AND a.target_kind=t.target_kind AND a.target_id=t.target_id
JOIN hashboard_installation i ON i.id=t.target_id AND i.removed_at IS NULL
WHERE o.organization_id=$1 AND a.device_id=$2 AND t.target_kind='hashboard_installation' AND o.kind='clear'
 AND ((t.state IN ('pending','applying','uncertain') AND a.version=t.assignment_version AND a.retired_at IS NULL)
 OR (t.state='completed' AND a.version=t.assignment_version+1 AND a.retired_at IS NOT NULL))
UNION ALL
SELECT l.transition_id,NULLIF(l.owner_account_id,0)::bigint,l.target_id,l.allocation_revision,l.reviewed_binding,l.state,l.physical_attempt_id,
 COALESCE(i.metadata->>'macAddress','')::text AS mac_address
FROM pool_lifecycle_transition l JOIN hashboard_installation i ON i.id=l.target_id AND i.removed_at IS NULL AND i.device_id=l.device_id
LEFT JOIN hosted_account_asset owner ON owner.organization_id=l.organization_id AND owner.kind=l.asset_kind AND owner.asset_id=l.asset_id
WHERE l.organization_id=$1 AND l.device_id=$2 AND l.target_kind='hashboard_installation' AND l.configuration_id IS NULL
 AND l.state IN ('ready','applying','uncertain','completed')
 AND COALESCE(owner.revision,0)=l.allocation_revision AND COALESCE(owner.subclient_id,owner.client_id,0)=l.owner_account_id
 AND NOT EXISTS(SELECT 1 FROM pool_configuration_assignment a WHERE a.organization_id=l.organization_id AND a.target_kind=l.target_kind AND a.target_id=l.target_id AND (a.retired_at IS NULL OR a.updated_at>l.created_at))
ORDER BY target_id;

-- name: RejectPendingPoolRemoval :execrows
UPDATE pool_configuration_operation_target SET state='failed',result_code='rejected',updated_at=clock_timestamp() WHERE operation_id=$1 AND state='pending';

-- name: ListRetiredCompletedPoolRemovals :many
-- Current equipment authority and binding locks are required. A completed
-- physical attempt may be observed and superseded, never replayed. Unknown
-- attempts retain their physical fence even after an ownership change.
SELECT o.operation_id::uuid AS removal_id
FROM pool_configuration_operation o
JOIN pool_configuration_operation_target t USING (operation_id)
JOIN device_write_attempt w ON w.attempt_id=t.physical_attempt_id
JOIN pool_configuration_assignment a ON a.organization_id=t.organization_id
 AND a.target_kind=t.target_kind AND a.target_id=t.target_id
WHERE o.organization_id=$1 AND t.organization_id=o.organization_id
 AND o.kind='clear' AND t.target_kind='device' AND t.target_id=$2
 AND a.device_id=$2 AND w.device_id=$2 AND w.organization_id=o.organization_id
 AND w.status='complete' AND w.operation_kind='mutation' AND w.command_type='UpdateMiningPools'
 AND t.state IN ('applying','uncertain')
 AND (a.retired_at IS NOT NULL OR a.version<>t.assignment_version OR a.configuration_id<>o.configuration_id)
UNION ALL
SELECT l.transition_id FROM pool_lifecycle_transition l
JOIN device_write_attempt w ON w.attempt_id=l.physical_attempt_id
LEFT JOIN hosted_account_asset owner ON owner.organization_id=l.organization_id AND owner.kind=l.asset_kind AND owner.asset_id=l.asset_id
WHERE l.organization_id=$1 AND l.device_id=$2 AND l.target_kind='device' AND l.configuration_id IS NULL
 AND l.state IN ('applying','uncertain') AND w.status='complete'
 AND (COALESCE(owner.subclient_id,owner.client_id,0)<>l.owner_account_id OR COALESCE(owner.revision,0)<>l.allocation_revision
  OR EXISTS(SELECT 1 FROM pool_lifecycle_transition newer WHERE newer.organization_id=l.organization_id AND newer.device_id=l.device_id
   AND newer.reason='ownership' AND newer.created_at>w.created_at));
