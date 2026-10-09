-- Row locks span final binding validation and the adapter call. The device
-- FOR UPDATE lock also blocks insertion of missing credentials/pairing/routes
-- through their device foreign keys. Existing children and discovery rows need
-- their own locks because updates/deletes do not recheck those foreign keys.
-- name: LockMinerBindingDevice :one
SELECT discovered_device_id FROM device WHERE id=$1 FOR UPDATE;

-- name: LockMinerBindingDiscovery :one
SELECT id FROM discovered_device WHERE id=$1 FOR SHARE;

-- name: LockMinerBindingPairing :many
SELECT id FROM device_pairing WHERE device_id=$1 ORDER BY id FOR SHARE;

-- name: LockMinerBindingCredentials :many
SELECT id FROM miner_credentials WHERE device_id=$1 ORDER BY id FOR SHARE;

-- name: LockMinerBindingRoutes :many
SELECT fnd.fleet_node_id FROM fleet_node_device fnd
JOIN fleet_node fn ON fn.id=fnd.fleet_node_id AND fn.org_id=fnd.org_id
WHERE fnd.device_id=$1 ORDER BY fnd.fleet_node_id FOR SHARE OF fnd,fn;
