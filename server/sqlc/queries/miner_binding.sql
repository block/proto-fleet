-- Row locks span final binding validation and the adapter call. Device
-- Mutations use FOR NO KEY UPDATE; admitted observations use FOR SHARE so
-- reads can overlap. Both permit unrelated queue/log foreign-key inserts.
-- Binding child inserters take FOR NO KEY UPDATE (or stronger) on the parent
-- to protect missing rows against both modes. Existing children and discovery
-- rows also retain their own locks.
-- name: LockMinerBindingDevice :one
SELECT discovered_device_id FROM device WHERE id=$1 FOR NO KEY UPDATE;

-- name: LockMinerBindingDeviceForAllocation :one
-- Allocation holds organization admission. Do not retain that lock while a
-- controller observation or mutation holds the device binding for I/O.
SELECT discovered_device_id FROM device WHERE id=$1 FOR NO KEY UPDATE NOWAIT;

-- name: LockMinerBindingObservationDevice :one
SELECT discovered_device_id FROM device WHERE id=$1 FOR SHARE;

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
