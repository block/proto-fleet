-- name: LockPoolEquipmentOrganization :one
-- Hardware updates may overlap, while assignment/account mutations wait.
SELECT id FROM organization WHERE id=$1 FOR SHARE;

-- name: GetPoolConfigurationRuntime :one
SELECT d.id,d.device_identifier,COALESCE(NULLIF(btrim(d.worker_name),''),NULLIF(btrim(d.mac_address),''),d.device_identifier)::text AS worker_name,
 dd.id AS discovery_id,dd.driver_name,COALESCE(dd.manufacturer,'')::text AS manufacturer,COALESCE(dd.model,'')::text AS model,
 dd.device_class,f.epoch,f.state,device_write_identity_keys(d.id)::text[] AS identity_keys
FROM device d JOIN discovered_device dd ON dd.id=d.discovered_device_id AND dd.org_id=d.org_id
JOIN device_write_fence f ON f.device_id=d.id AND f.organization_id=d.org_id
WHERE d.org_id=$1 AND d.id=$2 AND d.deleted_at IS NULL AND dd.deleted_at IS NULL;

-- name: ListPoolEquipmentIdentities :many
SELECT id,device_write_identity_keys(id)::text[] AS identity_keys FROM device
WHERE org_id=$1 AND id=ANY(sqlc.arg(device_ids)::bigint[]) AND deleted_at IS NULL ORDER BY id;
