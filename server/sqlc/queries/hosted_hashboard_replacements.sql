-- name: RecordHostedHashboardInventory :exec
-- Record the normalized map only after identity conflicts and unknown slots
-- have been resolved. Stale or replayed polls never reach this checkpoint.
UPDATE container_inventory
SET hashboard_inventory_order=nextval('hosted_hashboard_allocation_order'),
 hashboard_inventory_complete=sqlc.arg(complete)::boolean
WHERE device_id=sqlc.arg(device_id)::bigint AND observed_at=sqlc.arg(observed_at)::timestamptz;

-- name: ReconcileHostedHashboardReplacements :exec
-- Keep accepted inventory even when physical admission or mode publication is
-- busy. Match admission order: organization, fence, then mode state. Retain
-- those locks through transfer, or defer replacement when any lock is busy.
WITH locked_organization AS MATERIALIZED (
 SELECT id FROM organization WHERE id=sqlc.arg(organization_id)::bigint AND deleted_at IS NULL
 FOR NO KEY UPDATE SKIP LOCKED
), locked_fence AS MATERIALIZED (
 SELECT f.device_id,f.organization_id FROM device_write_fence f
 JOIN locked_organization o ON o.id=f.organization_id
 WHERE f.device_id=sqlc.arg(device_id)::bigint
 FOR UPDATE OF f SKIP LOCKED
), locked_state AS MATERIALIZED (
 SELECT s.device_id FROM container_mining_group_state s
 JOIN locked_fence f ON f.device_id=s.device_id AND f.organization_id=s.organization_id
 FOR UPDATE OF s SKIP LOCKED
)
SELECT CASE WHEN EXISTS (SELECT 1 FROM locked_state) THEN
 CASE WHEN ct1_hashboard_allocation_ready(sqlc.arg(organization_id)::bigint,sqlc.arg(device_id)::bigint)
  THEN reconcile_hosted_hashboard_replacements(sqlc.arg(organization_id)::bigint,sqlc.arg(device_id)::bigint,sqlc.arg(observed_at)::timestamptz)
  ELSE 0 END
 ELSE 0 END;
