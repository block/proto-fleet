-- name: ListActiveMiningGroupDeactivations :many
SELECT device_id FROM container_mining_group_state
 WHERE device_id=ANY($1::bigint[]) AND operation_kind='deactivate'
  AND status IN ('pending','applying','blocked');

-- name: InvalidateMiningGroupObservation :exec
-- Keep the last display snapshot, but a failed refresh cannot authorize a new
-- allocation merely because the previous successful read was recent.
-- Cleanup runs after binding locks unwind; never invalidate a newer observation.
UPDATE container_mining_group_state SET observed_at=NULL
WHERE organization_id=sqlc.arg(organization_id) AND device_id=sqlc.arg(device_id)
 AND observed_at IS NOT DISTINCT FROM sqlc.narg(observed_at)::timestamptz;

-- name: GetMiningGroupDeactivationBlocker :one
-- The caller holds organization admission and the device fence. Keep queued
-- policy separate from physical attempts: recalling the final board does not
-- mean its asynchronous credential cleanup has finished.
SELECT CASE
 WHEN EXISTS (
  SELECT 1 FROM hosted_account_asset a WHERE a.organization_id=sqlc.arg(organization_id)
   AND a.kind=7 AND a.client_id IS NOT NULL AND (a.device_id=sqlc.arg(device_id) OR EXISTS (
    SELECT 1 FROM hashboard_installation i WHERE i.hashboard_id=a.asset_id
     AND i.device_id=sqlc.arg(device_id) AND i.removed_at IS NULL))
 ) THEN 'Recall all allocated hashboards before disabling mining groups.'
 WHEN EXISTS (
  SELECT 1 FROM pool_lifecycle_transition l LEFT JOIN device_write_attempt w ON w.attempt_id=l.physical_attempt_id
  WHERE l.organization_id=sqlc.arg(organization_id) AND l.device_id=sqlc.arg(device_id)
   AND (l.state IN ('pending','ready','applying','uncertain')
    OR (l.state='deferred' AND NOT sqlc.arg(ignore_deferred)::boolean) OR
    pool_lifecycle_clear_retryable(l,w))
 ) OR EXISTS (
  SELECT 1 FROM pool_configuration_operation_target t
  JOIN pool_configuration_assignment a ON a.organization_id=t.organization_id
   AND a.target_kind=t.target_kind AND a.target_id=t.target_id
  WHERE t.organization_id=sqlc.arg(organization_id) AND a.device_id=sqlc.arg(device_id)
   AND (t.state IN ('pending','applying','uncertain') OR (t.state='failed' AND t.result_code='write_uncertain'))
 ) THEN 'Wait for pending pool changes and ownership cleanup to finish before disabling mining groups.'
 ELSE '' END::text AS reason;

-- name: CT1HashboardAllocationReady :one
-- Manual assignment and automatic replacement share the same mode rule.
SELECT ct1_hashboard_allocation_ready(sqlc.arg(organization_id)::bigint,sqlc.arg(device_id)::bigint)::boolean AS ready;

-- name: GetCT1HashboardAllocationBlocker :one
-- Explain a refused allocation under the same fence and journal locks. This
-- diagnostic never authorizes allocation or treats stale mode as current.
SELECT CASE
 WHEN s.status IN ('pending','applying','blocked')
  THEN 'Wait for pending mining-group changes or recovery to finish before allocating hashboards.'
 WHEN s.observed_at IS NULL OR s.observed_at<f.updated_at
  OR s.observed_at<=clock_timestamp()-INTERVAL '2 minutes' OR s.observed_at>clock_timestamp()+INTERVAL '5 seconds'
  THEN 'Refresh the container''s mining-group state before allocating hashboards.'
 WHEN s.observed @> '{"supported":false}'::jsonb
  THEN 'This controller does not support mining groups for hashboard allocation.'
 WHEN s.observed @> '{"enabled":false}'::jsonb
  THEN 'Enable mining groups before allocating hashboards.'
 ELSE 'Wait for pending mining-group or device changes and finish any required recovery before allocating hashboards.'
 END::text AS reason
FROM (SELECT 1) seed
LEFT JOIN container_mining_group_state s ON s.organization_id=sqlc.arg(organization_id) AND s.device_id=sqlc.arg(device_id)
LEFT JOIN device_write_fence f ON f.organization_id=sqlc.arg(organization_id) AND f.device_id=sqlc.arg(device_id);
