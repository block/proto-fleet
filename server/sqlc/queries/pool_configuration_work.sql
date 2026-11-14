-- name: LinkPoolConfigurationAttempt :execrows
UPDATE pool_configuration_operation_target SET physical_attempt_id=sqlc.arg(attempt_id)
WHERE organization_id=sqlc.arg(organization_id) AND operation_id=sqlc.arg(operation_id)
 AND target_kind=sqlc.arg(target_kind) AND target_id=sqlc.arg(target_id)
 AND state='applying' AND attempts=sqlc.arg(attempt) AND physical_attempt_id IS NULL;

-- name: RecoverSettledPoolConfigurationWork :exec
-- Never redispatch work. Running admissions need explicit offline recovery,
-- regardless of their age or process owner. A settled attempt proves drain,
-- not successful settings application. Allow a minute for normal bookkeeping
-- after completion; this delay is not evidence of physical safety.
UPDATE pool_configuration_operation_target t
SET state='failed',result_code=CASE WHEN w.status='unknown' THEN 'write_uncertain' ELSE 'interrupted' END,
 updated_at=clock_timestamp()
FROM device_write_attempt w
WHERE t.physical_attempt_id=w.attempt_id AND EXISTS(SELECT 1 FROM pool_configuration_operation o WHERE o.operation_id=t.operation_id AND o.kind='apply') AND (
 (t.state='applying' AND (w.status IN ('resolved','unknown') OR (w.status='complete' AND w.finished_at<clock_timestamp()-interval '1 minute')))
 OR (t.state='failed' AND t.result_code='write_uncertain' AND
     (w.status='resolved' OR (w.status='complete' AND w.finished_at<clock_timestamp()-interval '1 minute')))
);

-- name: ListPendingPoolConfigurationWork :many
-- Waiting boards must not fill the batch and starve other devices. Keep them
-- pending until their controller workflow settles; admission still rechecks
-- the journal under lock in case it becomes busy after this selection.
SELECT t.organization_id,t.operation_id,t.target_kind,t.target_id,a.device_id
FROM pool_configuration_operation_target t
JOIN pool_configuration_assignment a ON a.organization_id=t.organization_id AND a.target_kind=t.target_kind AND a.target_id=t.target_id
JOIN pool_configuration_operation o ON o.operation_id=t.operation_id AND o.organization_id=t.organization_id
WHERE t.state='pending' AND a.retired_at IS NULL AND (a.operation_id=t.operation_id OR o.kind IN ('clear','unassign')) AND a.version=t.assignment_version
 AND (t.target_kind<>'hashboard_installation' OR NOT EXISTS (
  SELECT 1 FROM container_mining_group_state s
  WHERE s.organization_id=t.organization_id AND s.device_id=a.device_id
   AND s.status IN ('pending','applying','blocked')
 ))
ORDER BY t.updated_at,t.operation_id,t.target_kind,t.target_id LIMIT 100;

-- name: GetPoolConfigurationWork :one
SELECT t.*,a.device_id,o.configuration_id,o.revision_id,o.actor_id,o.kind AS operation_kind,o.lifecycle_id,c.account_id,
 COALESCE(account.account_id,'')::text AS external_account_id,COALESCE(u.username,'')::text AS username,COALESCE(u.user_id,'')::text AS user_id,
 r.slots
FROM pool_configuration_operation_target t
JOIN pool_configuration_operation o ON o.operation_id=t.operation_id AND o.organization_id=t.organization_id
JOIN pool_configuration c ON c.configuration_id=o.configuration_id AND c.organization_id=o.organization_id
JOIN pool_configuration_revision r ON r.revision_id=o.revision_id AND r.configuration_id=o.configuration_id AND r.organization_id=o.organization_id
JOIN pool_configuration_assignment a ON a.organization_id=t.organization_id AND a.target_kind=t.target_kind AND a.target_id=t.target_id
LEFT JOIN hosted_account account ON account.id=c.account_id AND account.organization_id=c.organization_id
LEFT JOIN "user" u ON u.id=o.actor_id
WHERE t.organization_id=$1 AND t.operation_id=$2 AND t.target_kind=$3 AND t.target_id=$4
 AND (a.operation_id=t.operation_id OR o.kind IN ('clear','unassign')) AND a.version=t.assignment_version AND a.retired_at IS NULL;

-- name: ListPoolConfigurationOperations :many
SELECT o.operation_id,o.created_at,count(t.target_id)::bigint AS target_count,
 count(t.target_id) FILTER (WHERE t.state IN ('pending','applying'))::bigint AS pending_count,
 count(t.target_id) FILTER (WHERE t.state IN ('failed','different','uncertain'))::bigint AS failed_count
FROM pool_configuration_operation o
JOIN pool_configuration c ON c.organization_id=o.organization_id AND c.configuration_id=o.configuration_id
JOIN pool_configuration_operation_target t ON t.organization_id=o.organization_id AND t.operation_id=o.operation_id
WHERE o.organization_id=sqlc.arg(organization_id) AND o.configuration_id=sqlc.arg(configuration_id)
 AND c.account_id IS NOT DISTINCT FROM sqlc.narg(account_id)::bigint
 AND (sqlc.arg(before_id)::text='' OR (o.created_at,o.operation_id)<(
  SELECT cursor.created_at,cursor.operation_id FROM pool_configuration_operation cursor
  WHERE cursor.organization_id=o.organization_id AND cursor.configuration_id=o.configuration_id AND cursor.operation_id=sqlc.arg(before_id)
 ))
GROUP BY o.operation_id,o.created_at ORDER BY o.created_at DESC,o.operation_id DESC LIMIT 26;
