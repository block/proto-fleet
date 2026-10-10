-- Row locks span final binding validation and the adapter call. Device
-- FOR NO KEY UPDATE permits unrelated queue/log foreign-key inserts. Binding
-- child writers explicitly acquire a conflicting parent lock before insertion;
-- existing children and discovery rows also retain their own locks.
-- name: LockMinerBindingDevice :one
SELECT discovered_device_id FROM device WHERE id=$1 FOR NO KEY UPDATE;

-- name: LockMinerBindingDiscovery :one
SELECT id FROM discovered_device WHERE id=$1 FOR SHARE;

-- name: LockMinerBindingPairing :many
SELECT id FROM device_pairing WHERE device_id=$1 ORDER BY id FOR SHARE;

-- name: LockMinerBindingCredentials :many
SELECT id FROM miner_credentials WHERE device_id=$1 ORDER BY id FOR SHARE;

-- name: LockMinerBindingRoutes :many
SELECT fnd.fleet_node_id FROM fleet_node_device fnd
JOIN fleet_node fn ON fn.id=fnd.fleet_node_id AND fn.org_id=fnd.org_id
WHERE fnd.device_id=$1 ORDER BY fnd.fleet_node_id FOR SHARE OF fnd FOR KEY SHARE OF fn;
