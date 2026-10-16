-- name: ListHostedAccountMiningAssets :many
-- Include retained assignments even after retirement or policy changes.
WITH live_assets AS MATERIALIZED (
 -- Reuse the selected Fleet projection for leaves and readings. Inlining it
 -- twice can rebuild the entire projection for every selected leaf.
 SELECT f.* FROM fleet_mining_asset f WHERE f.org_id=sqlc.arg(organization_id)
 AND (jsonb_array_length(sqlc.arg(selected_assets)::jsonb)=0 OR EXISTS (SELECT 1 FROM jsonb_to_recordset(sqlc.arg(selected_assets)::jsonb) AS selected(kind integer,id bigint) WHERE selected.kind=f.kind AND selected.id=f.id))
), leaves AS (
 SELECT f.kind,f.id,f.org_id,CASE WHEN f.kind=5 THEN f.id ELSE f.facility_id END::bigint AS device_id
 FROM live_assets f
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
 CASE WHEN l.kind=5 THEN COALESCE(dd.model,'') ELSE '' END::text AS model,
 COALESCE(f.temperature_c,0)::float8 AS temperature_c, (f.temperature_c IS NOT NULL)::boolean AS has_temperature,
 COALESCE(f.miner_efficiency_jth,0)::float8 AS efficiency_j_th, (f.miner_efficiency_jth IS NOT NULL)::boolean AS has_efficiency,
 COALESCE(d.site_id,0)::bigint AS site_id,COALESCE(s.name,'')::text AS site_name,
 CASE WHEN l.kind=6 THEN l.device_id ELSE 0 END::bigint AS container_id,
 CASE WHEN l.kind=6 THEN COALESCE(NULLIF(d.custom_name,''),'Container '||d.device_identifier) ELSE '' END::text AS container_name,
 (f.id IS NOT NULL)::boolean AS available,
 COALESCE(f.hashrate_hs,0)::float8 AS hashrate_hs,COALESCE(f.power_w,0)::float8 AS power_w,
 (f.hashrate_hs IS NOT NULL)::boolean AS has_hashrate,(f.power_w IS NOT NULL)::boolean AS has_power,f.observed_at,
 COALESCE(cm.coverage,'null'::jsonb)::jsonb AS coverage,
 0::float8 AS sort_number, ''::text AS sort_text, true::boolean AS sort_null
FROM leaves l
LEFT JOIN hosted_account_asset a ON a.organization_id=l.org_id AND a.kind=l.kind AND a.asset_id=l.id
LEFT JOIN hosted_account c ON c.id=a.client_id
LEFT JOIN hosted_account sc ON sc.id=a.subclient_id
LEFT JOIN live_assets f ON f.org_id=l.org_id AND f.kind=l.kind AND f.id=l.id
JOIN device d ON d.id=l.device_id AND d.org_id=l.org_id
LEFT JOIN discovered_device dd ON dd.id=d.discovered_device_id AND dd.org_id=l.org_id
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
 CASE WHEN l.kind=5 THEN COALESCE(dd.model,'') ELSE '' END::text AS model,
 COALESCE(f.temperature_c,0)::float8 AS temperature_c, (f.temperature_c IS NOT NULL)::boolean AS has_temperature,
 COALESCE(f.miner_efficiency_jth,0)::float8 AS efficiency_j_th, (f.miner_efficiency_jth IS NOT NULL)::boolean AS has_efficiency,
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
LEFT JOIN discovered_device dd ON dd.id=d.discovered_device_id AND dd.org_id=l.org_id
LEFT JOIN site s ON s.id=d.site_id AND s.org_id=l.org_id
LEFT JOIN container_equipment_current_metrics cm ON l.kind=6 AND cm.kind=6 AND cm.id=l.id
WHERE sqlc.arg(is_root)::boolean OR a.client_id=sqlc.arg(account_internal_id)::bigint OR a.subclient_id=sqlc.arg(account_internal_id)::bigint

), filtered AS (
SELECT p.* FROM projection p
WHERE (NOT sqlc.arg(is_root)::boolean OR NOT EXISTS (
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
), sorted AS (
 SELECT p.*,
 CASE WHEN p.available AND (NOT sqlc.arg(policy_restricted)::boolean OR p.site_id=ANY(sqlc.arg(policy_sites)::bigint[])) THEN
   CASE sqlc.arg(sort_field)::int
     WHEN 1 THEN CASE WHEN p.name<>'' THEN 0::float8 END
     WHEN 5 THEN CASE WHEN p.model<>'' THEN 0::float8 END
     WHEN 6 THEN CASE WHEN p.has_hashrate AND p.hashrate_hs>=0 AND p.hashrate_hs<'Infinity'::float8 THEN p.hashrate_hs END
     WHEN 7 THEN CASE WHEN p.has_temperature AND p.temperature_c>'-Infinity'::float8 AND p.temperature_c<'Infinity'::float8 THEN p.temperature_c END
     WHEN 8 THEN CASE WHEN p.has_power AND p.power_w>=0 AND p.power_w<'Infinity'::float8 THEN p.power_w END
     WHEN 9 THEN CASE WHEN p.has_efficiency AND p.efficiency_j_th>=0 AND p.efficiency_j_th<'Infinity'::float8 THEN p.efficiency_j_th END
   END
 END::float8 AS sort_number,
 CASE WHEN p.available AND (NOT sqlc.arg(policy_restricted)::boolean OR p.site_id=ANY(sqlc.arg(policy_sites)::bigint[])) THEN
   CASE sqlc.arg(sort_field)::int WHEN 1 THEN lower(p.name) WHEN 5 THEN lower(p.model) ELSE '' END
 ELSE '' END::text AS sort_text
 FROM filtered p
)
SELECT p.kind,p.id,p.device_id,p.revision,p.client_id,p.subclient_id,p.owner_account_id,p.owner_account_name,p.name,p.state,p.model,p.temperature_c,p.has_temperature,p.efficiency_j_th,p.has_efficiency,p.site_id,p.site_name,p.container_id,p.container_name,p.available,p.hashrate_hs,p.power_w,p.has_hashrate,p.has_power,p.observed_at,p.coverage,
 COALESCE(p.sort_number,0)::float8 AS sort_number,p.sort_text,(p.sort_number IS NULL)::boolean AS sort_null
FROM sorted p
WHERE NOT sqlc.arg(has_cursor)::boolean OR
 (NOT sqlc.arg(cursor_null)::boolean AND p.sort_number IS NULL) OR
 (p.sort_number IS NOT NULL AND NOT sqlc.arg(cursor_null)::boolean AND
  CASE WHEN sqlc.arg(descending)::boolean THEN (p.sort_number,p.sort_text)<(sqlc.arg(cursor_number)::float8,sqlc.arg(cursor_text)::text)
  ELSE (p.sort_number,p.sort_text)>(sqlc.arg(cursor_number)::float8,sqlc.arg(cursor_text)::text) END) OR
 (((sqlc.arg(cursor_null)::boolean AND p.sort_number IS NULL) OR
   (NOT sqlc.arg(cursor_null)::boolean AND p.sort_number=sqlc.arg(cursor_number)::float8 AND p.sort_text=sqlc.arg(cursor_text)::text))
  AND (p.kind,p.id)>(sqlc.arg(after_kind)::int,sqlc.arg(after_id)::bigint))
ORDER BY CASE WHEN NOT sqlc.arg(descending)::boolean THEN p.sort_number END ASC NULLS LAST,
 CASE WHEN sqlc.arg(descending)::boolean THEN p.sort_number END DESC NULLS LAST,
 CASE WHEN NOT sqlc.arg(descending)::boolean THEN p.sort_text END ASC,
 CASE WHEN sqlc.arg(descending)::boolean THEN p.sort_text END DESC, p.kind,p.id
LIMIT sqlc.arg(page_limit)::int;
