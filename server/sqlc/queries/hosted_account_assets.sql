-- name: ListHostedAccountMiningAssets :many
-- Include retained assignments even after retirement or policy changes.
WITH leaves AS (
 SELECT f.kind,f.id,f.org_id,CASE WHEN f.kind=5 THEN f.id ELSE f.facility_id END::bigint AS device_id
 FROM fleet_mining_asset f WHERE f.org_id=sqlc.arg(organization_id)
 AND (jsonb_array_length(sqlc.arg(selected_assets)::jsonb)=0 OR EXISTS (SELECT 1 FROM jsonb_to_recordset(sqlc.arg(selected_assets)::jsonb) AS selected(kind integer,id bigint) WHERE selected.kind=f.kind AND selected.id=f.id))
 UNION
 SELECT a.kind,a.asset_id,a.organization_id,a.device_id FROM hosted_account_asset a
 WHERE a.organization_id=sqlc.arg(organization_id) AND a.client_id IS NOT NULL
 AND (jsonb_array_length(sqlc.arg(selected_assets)::jsonb)=0 OR EXISTS (SELECT 1 FROM jsonb_to_recordset(sqlc.arg(selected_assets)::jsonb) AS selected(kind integer,id bigint) WHERE selected.kind=a.kind AND selected.id=a.asset_id))
)
SELECT l.kind,l.id,l.device_id,COALESCE(a.revision,0)::bigint AS revision,
 COALESCE(a.client_id,0)::bigint AS client_id,COALESCE(a.subclient_id,0)::bigint AS subclient_id,
 COALESCE(sc.account_id,c.account_id,'')::text AS owner_account_id,
 COALESCE(sc.name,c.name,'')::text AS owner_account_name,
 COALESCE(f.name,'')::text AS name,COALESCE(f.state,'unknown')::text AS state,
 COALESCE(d.site_id,0)::bigint AS site_id,COALESCE(s.name,'')::text AS site_name,
 CASE WHEN l.kind=6 THEN l.device_id ELSE 0 END::bigint AS container_id,
 CASE WHEN l.kind=6 THEN COALESCE(NULLIF(d.custom_name,''),'Container '||d.device_identifier) ELSE '' END::text AS container_name,
 (f.id IS NOT NULL)::boolean AS available,
 COALESCE(f.hashrate_hs,0)::float8 AS hashrate_hs,COALESCE(f.power_w,0)::float8 AS power_w,
 (f.hashrate_hs IS NOT NULL)::boolean AS has_hashrate,(f.power_w IS NOT NULL)::boolean AS has_power,f.observed_at,
 COALESCE(cm.coverage,'null'::jsonb)::jsonb AS coverage
FROM leaves l
LEFT JOIN hosted_account_asset a ON a.organization_id=l.org_id AND a.kind=l.kind AND a.asset_id=l.id
LEFT JOIN hosted_account c ON c.id=a.client_id
LEFT JOIN hosted_account sc ON sc.id=a.subclient_id
LEFT JOIN fleet_mining_asset f ON f.org_id=l.org_id AND f.kind=l.kind AND f.id=l.id
JOIN device d ON d.id=l.device_id AND d.org_id=l.org_id
LEFT JOIN site s ON s.id=d.site_id AND s.org_id=l.org_id
LEFT JOIN container_equipment_current_metrics cm ON l.kind=6 AND cm.kind=6 AND cm.id=l.id
WHERE sqlc.arg(is_root)::boolean OR a.client_id=sqlc.arg(account_internal_id)::bigint OR a.subclient_id=sqlc.arg(account_internal_id)::bigint
ORDER BY l.kind,l.id;

-- name: ListHostedAccountAssetChildren :many
SELECT account_id,name,suspended,id FROM hosted_account WHERE organization_id=$1 AND parent_id=$2 ORDER BY id;

-- name: GetHostedAccountAssetDevice :one
SELECT d.id FROM device d WHERE d.org_id=sqlc.arg(organization_id) AND (
 (sqlc.arg(kind)::int=5 AND d.id=sqlc.arg(asset_id)) OR
 (sqlc.arg(kind)::int=6 AND d.id=(SELECT t.device_id FROM container_module m JOIN container_tank t ON t.id=m.tank_id WHERE m.id=sqlc.arg(asset_id)))
);

-- name: LockHostedAccountAssetInventory :exec
SELECT device_id FROM container_inventory WHERE device_id=ANY(sqlc.arg(device_ids)::bigint[]) ORDER BY device_id FOR UPDATE;

-- name: SetHostedAccountAsset :one
INSERT INTO hosted_account_asset(organization_id,kind,asset_id,device_id,miner_id,module_id,client_id,subclient_id)
VALUES(sqlc.arg(organization_id),sqlc.arg(kind),sqlc.arg(asset_id),sqlc.arg(device_id),sqlc.narg(miner_id),sqlc.narg(module_id),sqlc.narg(client_id),sqlc.narg(subclient_id))
ON CONFLICT (organization_id,kind,asset_id) DO UPDATE SET client_id=EXCLUDED.client_id,subclient_id=EXCLUDED.subclient_id,revision=hosted_account_asset.revision+1,updated_at=CURRENT_TIMESTAMP
RETURNING revision;

-- name: ListHostedAccountMiningAssetPage :many
WITH leaves AS (
 SELECT f.kind,f.id,f.org_id,CASE WHEN f.kind=5 THEN f.id ELSE f.facility_id END::bigint AS device_id
 FROM fleet_mining_asset f WHERE f.org_id=sqlc.arg(organization_id)
 UNION
 SELECT a.kind,a.asset_id,a.organization_id,a.device_id FROM hosted_account_asset a
 WHERE a.organization_id=sqlc.arg(organization_id) AND a.client_id IS NOT NULL
), projection AS (
SELECT l.kind,l.id,l.device_id,COALESCE(a.revision,0)::bigint AS revision,
 COALESCE(a.client_id,0)::bigint AS client_id,COALESCE(a.subclient_id,0)::bigint AS subclient_id,
 COALESCE(sc.account_id,c.account_id,'')::text AS owner_account_id,
 COALESCE(sc.name,c.name,'')::text AS owner_account_name,
 COALESCE(f.name,'')::text AS name,COALESCE(f.state,'unknown')::text AS state,
 COALESCE(d.site_id,0)::bigint AS site_id,COALESCE(s.name,'')::text AS site_name,
 CASE WHEN l.kind=6 THEN l.device_id ELSE 0 END::bigint AS container_id,
 CASE WHEN l.kind=6 THEN COALESCE(NULLIF(d.custom_name,''),'Container '||d.device_identifier) ELSE '' END::text AS container_name,
 (f.id IS NOT NULL)::boolean AS available,
 COALESCE(f.hashrate_hs,0)::float8 AS hashrate_hs,COALESCE(f.power_w,0)::float8 AS power_w,
 (f.hashrate_hs IS NOT NULL)::boolean AS has_hashrate,(f.power_w IS NOT NULL)::boolean AS has_power,f.observed_at,
 COALESCE(cm.coverage,'null'::jsonb)::jsonb AS coverage
FROM leaves l
LEFT JOIN hosted_account_asset a ON a.organization_id=l.org_id AND a.kind=l.kind AND a.asset_id=l.id
LEFT JOIN hosted_account c ON c.id=a.client_id
LEFT JOIN hosted_account sc ON sc.id=a.subclient_id
LEFT JOIN fleet_mining_asset f ON f.org_id=l.org_id AND f.kind=l.kind AND f.id=l.id
JOIN device d ON d.id=l.device_id AND d.org_id=l.org_id
LEFT JOIN site s ON s.id=d.site_id AND s.org_id=l.org_id
LEFT JOIN container_equipment_current_metrics cm ON l.kind=6 AND cm.kind=6 AND cm.id=l.id
WHERE sqlc.arg(is_root)::boolean OR a.client_id=sqlc.arg(account_internal_id)::bigint OR a.subclient_id=sqlc.arg(account_internal_id)::bigint

)
SELECT p.* FROM projection p
WHERE (p.kind,p.id)>(sqlc.arg(after_kind)::int,sqlc.arg(after_id)::bigint)
 AND (NOT sqlc.arg(is_root)::boolean OR NOT EXISTS (
  SELECT 1 FROM jsonb_to_recordset(sqlc.arg(host_authority)::jsonb) AS a(org_wide boolean,sites bigint[])
  WHERE NOT (CASE WHEN a.org_wide THEN p.site_id=0 OR NOT(p.site_id=ANY(a.sites)) ELSE p.site_id=ANY(a.sites) END)
 ))
 AND (NOT sqlc.arg(has_owner_filter)::boolean OR CASE
  WHEN sqlc.arg(is_root)::boolean THEN p.client_id=sqlc.arg(owner_internal_id)::bigint
  ELSE p.subclient_id=sqlc.arg(owner_internal_id)::bigint END)
 AND (sqlc.arg(container_filter)::bigint=0 OR
  (p.available AND (NOT sqlc.arg(policy_restricted)::boolean OR p.site_id=ANY(sqlc.arg(policy_sites)::bigint[])) AND p.container_id=sqlc.arg(container_filter)::bigint))
 AND (sqlc.arg(search)::text='' OR position(lower(sqlc.arg(search)::text) IN lower(CASE
  WHEN p.available AND (NOT sqlc.arg(policy_restricted)::boolean OR p.site_id=ANY(sqlc.arg(policy_sites)::bigint[])) THEN p.name ELSE '' END))>0)
ORDER BY p.kind,p.id LIMIT sqlc.arg(page_limit)::int;
