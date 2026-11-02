-- name: RecordHostedHashboardInventory :exec
-- Record the normalized map only after identity conflicts and unknown slots
-- have been resolved. Stale or replayed polls never reach this checkpoint.
UPDATE container_inventory
SET hashboard_inventory_order=nextval('hosted_hashboard_allocation_order'),
 hashboard_inventory_complete=sqlc.arg(complete)::boolean
WHERE device_id=sqlc.arg(device_id)::bigint AND observed_at=sqlc.arg(observed_at)::timestamptz;

-- name: ReconcileHostedHashboardReplacements :exec
SELECT reconcile_hosted_hashboard_replacements(sqlc.arg(organization_id)::bigint,sqlc.arg(device_id)::bigint,sqlc.arg(observed_at)::timestamptz);
