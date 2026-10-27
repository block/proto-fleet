-- name: CreateFleetNodeHAPair :exec
INSERT INTO fleet_node_ha_pair (org_id, active_node_id, standby_node_id, status_reason)
VALUES ($1, $2, $3, $4);

-- name: FleetNodeInHAPair :one
SELECT EXISTS (
    SELECT 1 FROM fleet_node_ha_pair
    WHERE org_id = sqlc.arg('org_id')
      AND (active_node_id = sqlc.arg('node_id') OR standby_node_id = sqlc.arg('node_id'))
);

-- name: IsFleetNodeHAStandby :one
SELECT EXISTS (
    SELECT 1 FROM fleet_node_ha_pair
    WHERE org_id = $1 AND standby_node_id = $2
);

-- name: ListFleetNodeHAPairs :many
SELECT p.id, p.org_id, p.active_node_id, p.standby_node_id, p.standby_ready, p.status_reason, p.pending_credential_key,
       s.encryption_pubkey AS standby_encryption_pubkey
FROM fleet_node_ha_pair p
JOIN fleet_node s ON s.id = p.standby_node_id AND s.org_id = p.org_id
ORDER BY p.id;

-- name: GetFleetNodeHAPairForNode :one
SELECT id, org_id, active_node_id, standby_node_id, standby_ready, pending_credential_key, status_reason, created_at, failed_over_at
FROM fleet_node_ha_pair
WHERE org_id = sqlc.arg('org_id')
  AND (active_node_id = sqlc.arg('node_id') OR standby_node_id = sqlc.arg('node_id'));

-- name: LockFleetNodeHAPair :one
SELECT id, org_id, active_node_id, standby_node_id, standby_ready, pending_credential_key, status_reason, created_at, failed_over_at
FROM fleet_node_ha_pair
WHERE id = $1 AND active_node_id = $2
FOR UPDATE;

-- name: SetLocalFleetNodeHAFailoverLockTimeout :exec
SELECT set_config('lock_timeout', sqlc.arg('timeout_milliseconds')::BIGINT::TEXT || 'ms', TRUE);

-- name: SetFleetNodeHAPairStatus :execrows
UPDATE fleet_node_ha_pair
SET status_reason = sqlc.arg('status_reason')
WHERE id = sqlc.arg('id') AND active_node_id = sqlc.arg('active_node_id')
  AND standby_node_id = sqlc.arg('standby_node_id')
  AND NOT standby_ready AND status_reason <> sqlc.arg('status_reason');

-- name: MarkFleetNodeHAStandbyReady :execrows
UPDATE fleet_node_ha_pair
SET standby_ready = true, status_reason = '', pending_credential_key = NULL
WHERE id = $1 AND active_node_id = $2 AND standby_node_id = $3;

-- name: MoveFleetNodeDevices :many
UPDATE fleet_node_device AS fnd
SET fleet_node_id = sqlc.arg('to_node_id')
FROM device d
WHERE fnd.fleet_node_id = sqlc.arg('from_node_id') AND fnd.org_id = sqlc.arg('org_id')
  AND d.id = fnd.device_id AND d.org_id = fnd.org_id
RETURNING d.device_identifier;

-- name: MoveDiscoveredDeviceAttribution :exec
UPDATE discovered_device
SET discovered_by_fleet_node_id = sqlc.arg('to_node_id')
WHERE discovered_by_fleet_node_id = sqlc.arg('from_node_id') AND org_id = sqlc.arg('org_id');

-- name: SwapFleetNodeHAPair :exec
UPDATE fleet_node_ha_pair
SET active_node_id = standby_node_id, standby_node_id = active_node_id, failed_over_at = now()
WHERE id = $1;

-- name: DeleteFleetNodeHAPairForNode :execrows
-- Pending installation may still execute after a server timeout. Keep the
-- surviving standby reserved until an acknowledgement reconciles its key.
-- A ready active pair must hand off before cleanup, even if the standby is
-- disconnected or readiness changed after the revocation decision.
DELETE FROM fleet_node_ha_pair AS p
WHERE p.org_id = sqlc.arg('org_id')
  AND (active_node_id = sqlc.arg('node_id') OR standby_node_id = sqlc.arg('node_id'))
  AND NOT (p.active_node_id = sqlc.arg('node_id') AND p.standby_ready)
  AND (p.pending_credential_key IS NULL OR p.standby_ready OR
       (p.standby_node_id = sqlc.arg('node_id') AND EXISTS (
           SELECT 1 FROM fleet_node n WHERE n.id = p.standby_node_id AND n.org_id = p.org_id
             AND n.enrollment_status = 'REVOKED'
       )));

-- name: PrepareFleetNodeHAKeyInstall :one
UPDATE fleet_node_ha_pair
SET pending_credential_key = COALESCE(pending_credential_key, sqlc.arg('pending_credential_key'))
WHERE id = sqlc.arg('id') AND active_node_id = sqlc.arg('active_node_id')
  AND standby_node_id = sqlc.arg('standby_node_id') AND NOT standby_ready
RETURNING pending_credential_key;
