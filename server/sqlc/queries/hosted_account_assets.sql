-- name: ListHostedAccountMiningAssets :many
-- Include retained assignments even after retirement or policy changes.
WITH allocations AS MATERIALIZED (
 SELECT a.* FROM hosted_account_asset a
 WHERE a.organization_id=sqlc.arg(organization_id)
 AND (sqlc.arg(is_root)::boolean OR a.client_id=sqlc.arg(account_internal_id)::bigint OR a.subclient_id=sqlc.arg(account_internal_id)::bigint)
 AND (jsonb_array_length(sqlc.arg(selected_assets)::jsonb)=0 OR EXISTS (SELECT 1 FROM jsonb_to_recordset(sqlc.arg(selected_assets)::jsonb) AS selected(kind integer,id bigint) WHERE selected.kind=a.kind AND selected.id=a.asset_id))
), live_assets AS MATERIALIZED (
 -- Unselected root reports need the full fleet. Exact root selections and
 -- customer reports enter the Fleet view by typed ID, before telemetry joins.
 SELECT f.* FROM fleet_mining_asset f
 WHERE sqlc.arg(is_root)::boolean AND f.org_id=sqlc.arg(organization_id)
 AND jsonb_array_length(sqlc.arg(selected_assets)::jsonb)=0
 UNION ALL
 SELECT f.* FROM (
  SELECT DISTINCT (value->>'kind')::integer AS kind,(value->>'id')::bigint AS id
  FROM jsonb_array_elements(sqlc.arg(selected_assets)::jsonb)
 ) selected
 CROSS JOIN LATERAL (
  SELECT f.* FROM fleet_mining_asset f
  WHERE f.org_id=sqlc.arg(organization_id) AND f.kind=selected.kind AND f.id=selected.id
  -- Prevent pull-up from moving selection filtering after telemetry joins.
  OFFSET 0
 ) f
 WHERE sqlc.arg(is_root)::boolean
 UNION ALL
 SELECT f.* FROM allocations a
 CROSS JOIN LATERAL (
  SELECT f.* FROM fleet_mining_asset f
  WHERE f.org_id=sqlc.arg(organization_id) AND f.kind=a.kind AND f.id=a.asset_id
  -- Prevent pull-up from moving allocation filtering after telemetry joins.
  OFFSET 0
 ) f
 WHERE NOT sqlc.arg(is_root)::boolean
), leaves AS (
 SELECT f.kind,f.id,f.org_id,CASE WHEN f.kind=5 THEN f.id ELSE f.facility_id END::bigint AS device_id
 FROM live_assets f
 UNION
 SELECT a.kind,a.asset_id,a.organization_id,a.device_id FROM allocations a
 WHERE a.client_id IS NOT NULL
)
SELECT l.kind,l.id,l.device_id,COALESCE(a.revision,0)::bigint AS revision,
 COALESCE(a.client_id,0)::bigint AS client_id,COALESCE(a.subclient_id,0)::bigint AS subclient_id,
 COALESCE(sc.account_id,c.account_id,'')::text AS owner_account_id,
 COALESCE(sc.name,c.name,'')::text AS owner_account_name,
 COALESCE(f.name,'')::text AS name,CASE
  WHEN g.state_allowed AND (sqlc.arg(is_root)::boolean OR l.kind=6 OR f.state='offline') THEN COALESCE(f.state,'unknown')
  WHEN source.auth_attention_time IS NOT NULL THEN 'error'
  WHEN g.state_allowed AND status.status IN ('MAINTENANCE','INACTIVE') THEN lower(status.status::text)
  WHEN source.error_attention_time IS NOT NULL THEN 'error'
  WHEN g.state_allowed THEN COALESCE(lower(status.status::text),'unknown')
  ELSE 'unknown' END::text AS state,
 CASE WHEN l.kind=5 THEN COALESCE(dd.model,'') ELSE '' END::text AS model,
 COALESCE(CASE WHEN g.temperature_allowed THEN f.temperature_c END,0)::float8 AS temperature_c, (g.temperature_allowed AND f.temperature_c IS NOT NULL)::boolean AS has_temperature,
 COALESCE(CASE WHEN g.hash_allowed AND g.power_allowed THEN f.miner_efficiency_jth END,0)::float8 AS efficiency_j_th, COALESCE(g.hash_allowed AND g.power_allowed AND f.miner_efficiency_jth IS NOT NULL
  AND f.hashrate_hs>=0 AND f.hashrate_hs<'Infinity'::float8 AND f.power_w>=0 AND f.power_w<'Infinity'::float8
  AND (l.kind=5 OR (
   (cm.coverage#>>'{hashrate,topology_complete}')::boolean AND (cm.coverage#>>'{power,topology_complete}')::boolean
   AND (cm.coverage#>>'{hashrate,expected}')::int>0 AND (cm.coverage#>>'{power,expected}')::int>0
   AND (cm.coverage#>>'{hashrate,reporting}')::int=(cm.coverage#>>'{hashrate,expected}')::int
   AND (cm.coverage#>>'{power,reporting}')::int=(cm.coverage#>>'{power,expected}')::int
  )),false)::boolean AS has_efficiency,
 COALESCE(d.site_id,0)::bigint AS site_id,COALESCE(s.name,'')::text AS site_name,
 CASE WHEN l.kind=6 THEN l.device_id ELSE 0 END::bigint AS container_id,
 CASE WHEN l.kind=6 THEN COALESCE(NULLIF(d.custom_name,''),'Container '||d.device_identifier) ELSE '' END::text AS container_name,
 (f.id IS NOT NULL)::boolean AS available,
 COALESCE(CASE WHEN g.hash_allowed THEN f.hashrate_hs END,0)::float8 AS hashrate_hs,COALESCE(CASE WHEN g.power_allowed THEN f.power_w END,0)::float8 AS power_w,
 (g.hash_allowed AND f.hashrate_hs IS NOT NULL)::boolean AS has_hashrate,(g.power_allowed AND f.power_w IS NOT NULL)::boolean AS has_power,
 COALESCE(CASE WHEN sqlc.arg(is_root)::boolean THEN f.observed_at ELSE GREATEST(CASE WHEN g.hash_allowed THEN source.hash_new END,CASE WHEN g.power_allowed THEN source.power_new END,CASE WHEN g.temperature_allowed THEN source.temperature_new END,CASE WHEN g.state_allowed THEN source.state_time END,source.auth_attention_time,source.error_attention_time) END,'0001-01-01T00:00:00Z'::timestamptz)::timestamptz AS observed_at,
 CASE WHEN sqlc.arg(is_root)::boolean THEN COALESCE(cm.coverage,'null'::jsonb) ELSE jsonb_strip_nulls(jsonb_build_object('hashrate',CASE WHEN g.hash_allowed THEN cm.coverage->'hashrate' END,'power',CASE WHEN g.power_allowed THEN cm.coverage->'power' END,'temperature',CASE WHEN g.temperature_allowed THEN cm.coverage->'temperature' END)) END::jsonb AS coverage,
 0::float8 AS sort_number, ''::text AS sort_text, true::boolean AS sort_null
FROM leaves l
LEFT JOIN allocations a ON a.organization_id=l.org_id AND a.kind=l.kind AND a.asset_id=l.id
LEFT JOIN hosted_account c ON c.id=a.client_id
LEFT JOIN hosted_account sc ON sc.id=a.subclient_id
LEFT JOIN live_assets f ON f.org_id=l.org_id AND f.kind=l.kind AND f.id=l.id
JOIN device d ON d.id=l.device_id AND d.org_id=l.org_id
LEFT JOIN discovered_device dd ON dd.id=d.discovered_device_id AND dd.org_id=l.org_id
LEFT JOIN site s ON s.id=d.site_id AND s.org_id=l.org_id
LEFT JOIN container_equipment_current_metrics cm ON l.kind=6 AND cm.kind=6 AND cm.id=l.id
LEFT JOIN device_metrics sample ON l.kind=5 AND sample.device_identifier=d.device_identifier AND sample.time=f.observed_at
LEFT JOIN device_status status ON l.kind=5 AND status.device_id=l.id
LEFT JOIN device_pairing pairing ON l.kind=5 AND pairing.device_id=l.id
LEFT JOIN container_module module ON l.kind=6 AND module.id=l.id
LEFT JOIN container_module_latest_metrics latest ON l.kind=6 AND latest.module_id=l.id
LEFT JOIN container_module_metrics module_sample ON l.kind=6 AND module_sample.module_id=l.id AND module_sample.time=latest.time AND module_sample.collected_at=latest.collected_at
CROSS JOIN LATERAL (
 SELECT CASE WHEN l.kind=5 THEN f.observed_at ELSE (cm.coverage#>>'{hashrate,oldest_at}')::timestamptz END AS hash_old,
 CASE WHEN l.kind=5 THEN f.observed_at ELSE (cm.coverage#>>'{hashrate,newest_at}')::timestamptz END AS hash_new,
 CASE WHEN l.kind=5 THEN f.observed_at ELSE (cm.coverage#>>'{power,oldest_at}')::timestamptz END AS power_old,
 CASE WHEN l.kind=5 THEN f.observed_at ELSE (cm.coverage#>>'{power,newest_at}')::timestamptz END AS power_new,
 CASE WHEN l.kind=5 THEN f.observed_at ELSE (cm.coverage#>>'{temperature,oldest_at}')::timestamptz END AS temperature_old,
 CASE WHEN l.kind=5 THEN f.observed_at ELSE (cm.coverage#>>'{temperature,newest_at}')::timestamptz END AS temperature_new,
 CASE WHEN l.kind=5 THEN status.status_timestamp ELSE module.state_observed_at END AS state_time,
 CASE WHEN l.kind=5 AND pairing.pairing_status='AUTHENTICATION_NEEDED'
  AND hosted_account_source_authorized(l.org_id,l.kind,l.id,a.client_id,a.subclient_id,d.site_id,d.device_identifier,pairing.updated_at,pairing.updated_at)
  THEN pairing.updated_at END AS auth_attention_time,
 -- A retained error must fit wholly within this owner's allocation/site.
 (SELECT max(e.last_seen_at) FROM errors e
  WHERE l.kind=5 AND e.device_id=l.id AND e.org_id=l.org_id
   AND e.closed_at IS NULL AND e.severity IN (1,2,3,4)
   AND e.site_id IS NOT DISTINCT FROM d.site_id
   AND hosted_account_source_authorized(l.org_id,l.kind,l.id,a.client_id,a.subclient_id,e.site_id,d.device_identifier,e.first_seen_at,e.last_seen_at)) AS error_attention_time,
 CASE WHEN l.kind=5 THEN sample.site_id ELSE module_sample.site_id END AS sample_site,
 CASE WHEN l.kind=5 THEN sample.time IS NOT NULL ELSE module_sample.module_id IS NOT NULL END AS sample_present
) source
CROSS JOIN LATERAL (
 SELECT sqlc.arg(is_root)::boolean OR (source.sample_present AND source.sample_site IS NOT DISTINCT FROM d.site_id AND hosted_account_source_authorized(l.org_id,l.kind,l.id,a.client_id,a.subclient_id,source.sample_site,d.device_identifier,source.hash_old,source.hash_new)) AS hash_allowed,
 sqlc.arg(is_root)::boolean OR (source.sample_present AND source.sample_site IS NOT DISTINCT FROM d.site_id AND hosted_account_source_authorized(l.org_id,l.kind,l.id,a.client_id,a.subclient_id,source.sample_site,d.device_identifier,source.power_old,source.power_new)) AS power_allowed,
 sqlc.arg(is_root)::boolean OR (source.sample_present AND source.sample_site IS NOT DISTINCT FROM d.site_id AND hosted_account_source_authorized(l.org_id,l.kind,l.id,a.client_id,a.subclient_id,source.sample_site,d.device_identifier,source.temperature_old,source.temperature_new)) AS temperature_allowed,
 sqlc.arg(is_root)::boolean OR hosted_account_source_authorized(l.org_id,l.kind,l.id,a.client_id,a.subclient_id,d.site_id,d.device_identifier,source.state_time,source.state_time) AS state_allowed
) g
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
-- Scope like ListHostedAccountMiningAssets: customer pages enter the Fleet view
-- by allocated ID so a small estate never evaluates the host fleet's telemetry.
WITH allocations AS MATERIALIZED (
 SELECT a.* FROM hosted_account_asset a
 WHERE a.organization_id=sqlc.arg(organization_id)
 AND (sqlc.arg(is_root)::boolean OR a.client_id=sqlc.arg(account_internal_id)::bigint OR a.subclient_id=sqlc.arg(account_internal_id)::bigint)
), live_assets AS MATERIALIZED (
 SELECT f.* FROM fleet_mining_asset f
 WHERE sqlc.arg(is_root)::boolean AND f.org_id=sqlc.arg(organization_id)
 UNION ALL
 SELECT f.* FROM allocations a
 CROSS JOIN LATERAL (
  SELECT f.* FROM fleet_mining_asset f
  WHERE f.org_id=sqlc.arg(organization_id) AND f.kind=a.kind AND f.id=a.asset_id
  -- Prevent pull-up from moving allocation filtering after telemetry joins.
  OFFSET 0
 ) f
 WHERE NOT sqlc.arg(is_root)::boolean
), leaves AS (
 SELECT f.kind,f.id,f.org_id,CASE WHEN f.kind=5 THEN f.id ELSE f.facility_id END::bigint AS device_id
 FROM live_assets f
 UNION
 SELECT a.kind,a.asset_id,a.organization_id,a.device_id FROM allocations a
 WHERE a.client_id IS NOT NULL
), projection AS (
SELECT l.kind,l.id,l.device_id,COALESCE(a.revision,0)::bigint AS revision,
 COALESCE(a.client_id,0)::bigint AS client_id,COALESCE(a.subclient_id,0)::bigint AS subclient_id,
 COALESCE(sc.account_id,c.account_id,'')::text AS owner_account_id,
 COALESCE(sc.name,c.name,'')::text AS owner_account_name,
 COALESCE(f.name,'')::text AS name,CASE
  WHEN g.state_allowed AND (sqlc.arg(is_root)::boolean OR l.kind=6 OR f.state='offline') THEN COALESCE(f.state,'unknown')
  WHEN source.auth_attention_time IS NOT NULL THEN 'error'
  WHEN g.state_allowed AND status.status IN ('MAINTENANCE','INACTIVE') THEN lower(status.status::text)
  WHEN source.error_attention_time IS NOT NULL THEN 'error'
  WHEN g.state_allowed THEN COALESCE(lower(status.status::text),'unknown')
  ELSE 'unknown' END::text AS state,
 CASE WHEN l.kind=5 THEN COALESCE(dd.model,'') ELSE '' END::text AS model,
 COALESCE(CASE WHEN g.temperature_allowed THEN f.temperature_c END,0)::float8 AS temperature_c, (g.temperature_allowed AND f.temperature_c IS NOT NULL)::boolean AS has_temperature,
 COALESCE(CASE WHEN g.hash_allowed AND g.power_allowed THEN f.miner_efficiency_jth END,0)::float8 AS efficiency_j_th, COALESCE(g.hash_allowed AND g.power_allowed AND f.miner_efficiency_jth IS NOT NULL
  AND f.hashrate_hs>=0 AND f.hashrate_hs<'Infinity'::float8 AND f.power_w>=0 AND f.power_w<'Infinity'::float8
  AND (l.kind=5 OR (
   (cm.coverage#>>'{hashrate,topology_complete}')::boolean AND (cm.coverage#>>'{power,topology_complete}')::boolean
   AND (cm.coverage#>>'{hashrate,expected}')::int>0 AND (cm.coverage#>>'{power,expected}')::int>0
   AND (cm.coverage#>>'{hashrate,reporting}')::int=(cm.coverage#>>'{hashrate,expected}')::int
   AND (cm.coverage#>>'{power,reporting}')::int=(cm.coverage#>>'{power,expected}')::int
  )),false)::boolean AS has_efficiency,
 COALESCE(d.site_id,0)::bigint AS site_id,COALESCE(s.name,'')::text AS site_name,
 CASE WHEN l.kind=6 THEN l.device_id ELSE 0 END::bigint AS container_id,
 CASE WHEN l.kind=6 THEN COALESCE(NULLIF(d.custom_name,''),'Container '||d.device_identifier) ELSE '' END::text AS container_name,
 (f.id IS NOT NULL)::boolean AS available,
 COALESCE(CASE WHEN g.hash_allowed THEN f.hashrate_hs END,0)::float8 AS hashrate_hs,COALESCE(CASE WHEN g.power_allowed THEN f.power_w END,0)::float8 AS power_w,
 (g.hash_allowed AND f.hashrate_hs IS NOT NULL)::boolean AS has_hashrate,(g.power_allowed AND f.power_w IS NOT NULL)::boolean AS has_power,
 COALESCE(CASE WHEN sqlc.arg(is_root)::boolean THEN f.observed_at ELSE GREATEST(CASE WHEN g.hash_allowed THEN source.hash_new END,CASE WHEN g.power_allowed THEN source.power_new END,CASE WHEN g.temperature_allowed THEN source.temperature_new END,CASE WHEN g.state_allowed THEN source.state_time END,source.auth_attention_time,source.error_attention_time) END,'0001-01-01T00:00:00Z'::timestamptz)::timestamptz AS observed_at,
 CASE WHEN sqlc.arg(is_root)::boolean THEN COALESCE(cm.coverage,'null'::jsonb) ELSE jsonb_strip_nulls(jsonb_build_object('hashrate',CASE WHEN g.hash_allowed THEN cm.coverage->'hashrate' END,'power',CASE WHEN g.power_allowed THEN cm.coverage->'power' END,'temperature',CASE WHEN g.temperature_allowed THEN cm.coverage->'temperature' END)) END::jsonb AS coverage
FROM leaves l
LEFT JOIN allocations a ON a.organization_id=l.org_id AND a.kind=l.kind AND a.asset_id=l.id
LEFT JOIN hosted_account c ON c.id=a.client_id
LEFT JOIN hosted_account sc ON sc.id=a.subclient_id
LEFT JOIN live_assets f ON f.org_id=l.org_id AND f.kind=l.kind AND f.id=l.id
JOIN device d ON d.id=l.device_id AND d.org_id=l.org_id
LEFT JOIN discovered_device dd ON dd.id=d.discovered_device_id AND dd.org_id=l.org_id
LEFT JOIN site s ON s.id=d.site_id AND s.org_id=l.org_id
LEFT JOIN container_equipment_current_metrics cm ON l.kind=6 AND cm.kind=6 AND cm.id=l.id
LEFT JOIN device_metrics sample ON l.kind=5 AND sample.device_identifier=d.device_identifier AND sample.time=f.observed_at
LEFT JOIN device_status status ON l.kind=5 AND status.device_id=l.id
LEFT JOIN device_pairing pairing ON l.kind=5 AND pairing.device_id=l.id
LEFT JOIN container_module module ON l.kind=6 AND module.id=l.id
LEFT JOIN container_module_latest_metrics latest ON l.kind=6 AND latest.module_id=l.id
LEFT JOIN container_module_metrics module_sample ON l.kind=6 AND module_sample.module_id=l.id AND module_sample.time=latest.time AND module_sample.collected_at=latest.collected_at
CROSS JOIN LATERAL (
 SELECT CASE WHEN l.kind=5 THEN f.observed_at ELSE (cm.coverage#>>'{hashrate,oldest_at}')::timestamptz END AS hash_old,
 CASE WHEN l.kind=5 THEN f.observed_at ELSE (cm.coverage#>>'{hashrate,newest_at}')::timestamptz END AS hash_new,
 CASE WHEN l.kind=5 THEN f.observed_at ELSE (cm.coverage#>>'{power,oldest_at}')::timestamptz END AS power_old,
 CASE WHEN l.kind=5 THEN f.observed_at ELSE (cm.coverage#>>'{power,newest_at}')::timestamptz END AS power_new,
 CASE WHEN l.kind=5 THEN f.observed_at ELSE (cm.coverage#>>'{temperature,oldest_at}')::timestamptz END AS temperature_old,
 CASE WHEN l.kind=5 THEN f.observed_at ELSE (cm.coverage#>>'{temperature,newest_at}')::timestamptz END AS temperature_new,
 CASE WHEN l.kind=5 THEN status.status_timestamp ELSE module.state_observed_at END AS state_time,
 CASE WHEN l.kind=5 AND pairing.pairing_status='AUTHENTICATION_NEEDED'
  AND hosted_account_source_authorized(l.org_id,l.kind,l.id,a.client_id,a.subclient_id,d.site_id,d.device_identifier,pairing.updated_at,pairing.updated_at)
  THEN pairing.updated_at END AS auth_attention_time,
 -- A retained error must fit wholly within this owner's allocation/site.
 (SELECT max(e.last_seen_at) FROM errors e
  WHERE l.kind=5 AND e.device_id=l.id AND e.org_id=l.org_id
   AND e.closed_at IS NULL AND e.severity IN (1,2,3,4)
   AND e.site_id IS NOT DISTINCT FROM d.site_id
   AND hosted_account_source_authorized(l.org_id,l.kind,l.id,a.client_id,a.subclient_id,e.site_id,d.device_identifier,e.first_seen_at,e.last_seen_at)) AS error_attention_time,
 CASE WHEN l.kind=5 THEN sample.site_id ELSE module_sample.site_id END AS sample_site,
 CASE WHEN l.kind=5 THEN sample.time IS NOT NULL ELSE module_sample.module_id IS NOT NULL END AS sample_present
) source
CROSS JOIN LATERAL (
 SELECT sqlc.arg(is_root)::boolean OR (source.sample_present AND source.sample_site IS NOT DISTINCT FROM d.site_id AND hosted_account_source_authorized(l.org_id,l.kind,l.id,a.client_id,a.subclient_id,source.sample_site,d.device_identifier,source.hash_old,source.hash_new)) AS hash_allowed,
 sqlc.arg(is_root)::boolean OR (source.sample_present AND source.sample_site IS NOT DISTINCT FROM d.site_id AND hosted_account_source_authorized(l.org_id,l.kind,l.id,a.client_id,a.subclient_id,source.sample_site,d.device_identifier,source.power_old,source.power_new)) AS power_allowed,
 sqlc.arg(is_root)::boolean OR (source.sample_present AND source.sample_site IS NOT DISTINCT FROM d.site_id AND hosted_account_source_authorized(l.org_id,l.kind,l.id,a.client_id,a.subclient_id,source.sample_site,d.device_identifier,source.temperature_old,source.temperature_new)) AS temperature_allowed,
 sqlc.arg(is_root)::boolean OR hosted_account_source_authorized(l.org_id,l.kind,l.id,a.client_id,a.subclient_id,d.site_id,d.device_identifier,source.state_time,source.state_time) AS state_allowed
) g
WHERE sqlc.arg(is_root)::boolean OR a.client_id=sqlc.arg(account_internal_id)::bigint OR a.subclient_id=sqlc.arg(account_internal_id)::bigint

), filtered AS (
SELECT p.* FROM projection p
-- Host operators carry host read authority at every depth; customers pass none.
WHERE (NOT EXISTS (
  SELECT 1 FROM jsonb_to_recordset(sqlc.arg(host_authority)::jsonb) AS a(org_wide boolean,sites bigint[])
  WHERE NOT (CASE WHEN a.org_wide THEN p.site_id=0 OR NOT(p.site_id=ANY(a.sites)) ELSE p.site_id=ANY(a.sites) END)
 ))
 AND (NOT sqlc.arg(has_owner_filter)::boolean OR CASE
  WHEN sqlc.arg(is_root)::boolean THEN p.client_id=sqlc.arg(owner_internal_id)::bigint
  ELSE p.subclient_id=sqlc.arg(owner_internal_id)::bigint END)
 AND (sqlc.arg(container_filter)::bigint=0 OR
  (p.available AND (NOT sqlc.arg(policy_restricted)::boolean OR p.site_id=ANY(sqlc.arg(policy_sites)::bigint[])) AND p.container_id=sqlc.arg(container_filter)::bigint))
 AND (sqlc.arg(model_filter)::text='' OR
  (p.available AND (NOT sqlc.arg(policy_restricted)::boolean OR p.site_id=ANY(sqlc.arg(policy_sites)::bigint[])) AND p.model=sqlc.arg(model_filter)::text))
 AND (sqlc.arg(site_filter)::bigint=0 OR
  (p.available AND (NOT sqlc.arg(policy_restricted)::boolean OR p.site_id=ANY(sqlc.arg(policy_sites)::bigint[])) AND p.site_id=sqlc.arg(site_filter)::bigint))
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

-- name: ListHostedAccountAssetFilterOptions :many
-- Only physical metadata is projected, so telemetry values are not fetched.
-- Use the same live inventory, allocation and physical-policy boundaries as the page.
SELECT DISTINCT CASE WHEN f.kind=5 THEN COALESCE(dd.model,'') ELSE '' END::text AS model,
 COALESCE(d.site_id,0)::bigint AS site_id,COALESCE(s.name,'')::text AS site_name,
 CASE WHEN f.kind=6 THEN d.id ELSE 0 END::bigint AS container_id,
 CASE WHEN f.kind=6 THEN COALESCE(NULLIF(d.custom_name,''),'Container '||d.device_identifier) ELSE '' END::text AS container_name
FROM fleet_mining_asset f
JOIN device d ON d.id=CASE WHEN f.kind=5 THEN f.id ELSE f.facility_id END AND d.org_id=f.org_id
LEFT JOIN discovered_device dd ON dd.id=d.discovered_device_id AND dd.org_id=f.org_id
LEFT JOIN site s ON s.id=d.site_id AND s.org_id=f.org_id
WHERE f.org_id=sqlc.arg(organization_id)
 AND (sqlc.arg(is_root)::boolean OR EXISTS (
  SELECT 1 FROM hosted_account_asset a WHERE a.organization_id=f.org_id AND a.kind=f.kind AND a.asset_id=f.id
   AND (a.client_id=sqlc.arg(account_internal_id)::bigint OR a.subclient_id=sqlc.arg(account_internal_id)::bigint)
 ))
 AND (NOT sqlc.arg(policy_restricted)::boolean OR COALESCE(d.site_id,0)=ANY(sqlc.arg(policy_sites)::bigint[]))
 AND NOT EXISTS (
  SELECT 1 FROM jsonb_to_recordset(sqlc.arg(host_authority)::jsonb) AS a(org_wide boolean,sites bigint[])
  WHERE NOT (CASE WHEN a.org_wide THEN COALESCE(d.site_id,0)=0 OR NOT(COALESCE(d.site_id,0)=ANY(a.sites)) ELSE COALESCE(d.site_id,0)=ANY(a.sites) END)
 );
