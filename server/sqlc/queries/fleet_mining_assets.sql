-- Mining assets: filter and paginate miners/modules without parent aggregation.
-- Each authority scope uses org-wide/denylist or site-allowlist semantics.
-- name: ListFleetMiningAssets :one
WITH options AS (
 SELECT COALESCE((sqlc.arg('special_filters')::jsonb->>'miner_only')::boolean,FALSE) AS miner_only,
   COALESCE((sqlc.arg('special_filters')::jsonb->>'no_facility')::boolean,FALSE) AS no_facility,
   COALESCE((sqlc.arg('special_filters')::jsonb->>'no_group')::boolean,FALSE) AS no_group,
   COALESCE((sqlc.arg('special_filters')::jsonb->>'no_building')::boolean,FALSE) AS no_building,
   COALESCE((sqlc.arg('special_filters')::jsonb->>'no_rack')::boolean,FALSE) AS no_rack,
   COALESCE((sqlc.arg('special_filters')::jsonb->>'unassigned')::boolean,FALSE) AS unassigned,
   ARRAY(SELECT value::int FROM jsonb_array_elements_text(NULLIF(sqlc.arg('special_filters')::jsonb->'issues','null'::jsonb)))::int[] AS issues,
   ARRAY(SELECT value::bigint FROM jsonb_array_elements_text(NULLIF(sqlc.arg('special_filters')::jsonb->'building_ids','null'::jsonb)))::bigint[] AS building_ids,
   ARRAY(SELECT value::text FROM jsonb_array_elements_text(NULLIF(sqlc.arg('special_filters')::jsonb->'states','null'::jsonb)))::text[] AS states,
   ARRAY(SELECT value::text FROM jsonb_array_elements_text(NULLIF(sqlc.arg('special_filters')::jsonb->'models','null'::jsonb)))::text[] AS models,
   ARRAY(SELECT value::text FROM jsonb_array_elements_text(NULLIF(sqlc.arg('special_filters')::jsonb->'firmware','null'::jsonb)))::text[] AS firmware,
   ARRAY(SELECT value::text FROM jsonb_array_elements_text(NULLIF(sqlc.arg('special_filters')::jsonb->'pairing','null'::jsonb)))::text[] AS pairing,
   ARRAY(SELECT value::text FROM jsonb_array_elements_text(NULLIF(sqlc.arg('special_filters')::jsonb->'status','null'::jsonb)))::text[] AS status,
   ARRAY(SELECT value::bigint FROM jsonb_array_elements_text(NULLIF(sqlc.arg('special_filters')::jsonb->'groups','null'::jsonb)))::bigint[] AS groups,
   ARRAY(SELECT value::bigint FROM jsonb_array_elements_text(NULLIF(sqlc.arg('special_filters')::jsonb->'racks','null'::jsonb)))::bigint[] AS racks,
   ARRAY(SELECT value::bigint FROM jsonb_array_elements_text(NULLIF(sqlc.arg('special_filters')::jsonb->'sites','null'::jsonb)))::bigint[] AS sites,
   ARRAY(SELECT value::cidr FROM jsonb_array_elements_text(NULLIF(sqlc.arg('special_filters')::jsonb->'cidrs','null'::jsonb)))::cidr[] AS cidrs,
   NULLIF(sqlc.arg('special_filters')::jsonb->'zones','null'::jsonb) AS zones,
   NULLIF(sqlc.arg('special_filters')::jsonb->'ip_ranges','null'::jsonb) AS ip_ranges,
   NULLIF(sqlc.arg('special_filters')::jsonb->'ranges','null'::jsonb) AS ranges,
   COALESCE(sqlc.arg('special_filters')::jsonb->>'search','') AS search
), lookup_needs AS (
 SELECT sqlc.arg('sort_field')::int IN (2,3,4,5) OR cardinality(f.states)>0
     OR COALESCE(jsonb_array_length(sqlc.arg('ranges')::jsonb),0)>0
     OR COALESCE(jsonb_array_length(f.ranges),0)>0 AS asset_state_or_metrics,
   sqlc.arg('sort_field')::int=7 OR cardinality(f.issues)>0 OR cardinality(f.status)>0 AS open_issues,
   EXISTS (SELECT 1 FROM jsonb_to_recordset(f.ranges) AS v(field int)
           WHERE v.field IN (5,6)) AS voltage_or_current
 FROM options f
), asset_keys AS (
 -- Use the same miner eligibility and placement rules as fleet_mining_asset.
 SELECT 5::int AS kind, d.id, d.org_id, d.site_id,
   COALESCE(NULLIF(d.custom_name,''),concat_ws(' ',dd.manufacturer,dd.model))::text AS name,
   d.device_identifier::text AS device_identifier, COALESCE(d.serial_number,'')::text AS reported_id,
   1::int AS facility_kind, COALESCE(d.building_id,rack.building_id) AS facility_id,
   3::int AS group_kind, rack.device_set_id AS group_id,
   dd.model, dd.firmware_version, dd.ip_address, dd.ip_address_inet,
   d.mac_address, d.worker_name, dp.pairing_status::text AS pairing_status, rack.zone
 FROM device d
 JOIN discovered_device dd ON dd.id=d.discovered_device_id AND dd.org_id=d.org_id
 JOIN device_pairing dp ON dp.device_id=d.id
 LEFT JOIN (
   SELECT ms.device_id, ms.org_id, r.device_set_id, r.building_id, r.zone
   FROM device_set_membership ms
   JOIN device_set s ON s.id=ms.device_set_id AND s.deleted_at IS NULL
   JOIN device_set_rack r ON r.device_set_id=s.id
   WHERE ms.device_set_type='rack'
 ) rack ON rack.device_id=d.id AND rack.org_id=d.org_id
 WHERE d.org_id=sqlc.arg('org_id') AND 5=ANY(sqlc.arg('kinds')::int[])
   AND d.deleted_at IS NULL AND dd.deleted_at IS NULL AND dd.is_active AND dd.device_class<>2
   AND dp.pairing_status IN ('PAIRED','AUTHENTICATION_NEEDED','DEFAULT_PASSWORD')
 UNION ALL
 SELECT kind, id, org_id, site_id, name, device_identifier, reported_id, facility_kind, facility_id, group_kind, group_id,
   NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL
 FROM fleet_mining_asset
 WHERE org_id=sqlc.arg('org_id') AND kind=6 AND 6=ANY(sqlc.arg('kinds')::int[])
), facts AS (
 SELECT r.kind, r.id, r.org_id, r.site_id, r.name, r.facility_kind, r.facility_id, r.group_kind, r.group_id,
   details.state,
   CASE WHEN access.telemetry_allowed THEN details.hashrate_hs / 1e12 END AS hashrate_ths,
   CASE WHEN access.telemetry_allowed THEN details.power_w / 1e3 END AS power_kw,
   CASE WHEN access.telemetry_allowed THEN details.temperature_c END AS temperature_c,
   CASE WHEN access.telemetry_allowed THEN details.miner_efficiency_jth END AS efficiency_jth,
   access.telemetry_allowed, r.model, r.firmware_version, r.ip_address_inet, r.mac_address,
   CASE WHEN r.kind=6 THEN workers.summary ELSE r.worker_name END AS worker_name,
   workers.summary AS worker_name_summary,
   r.pairing_status, r.zone,
   CASE WHEN fn.last_seen_at < now()-INTERVAL '2 minutes' AND fn.enrollment_status='CONFIRMED' AND fn.deleted_at IS NULL THEN 'OFFLINE' ELSE ds.status::text END AS device_status,
   issues.issue_count, issues.issue_types, issues.actionable,
   dm.voltage_v, dm.current_a,
   concat_ws(' ',r.name,r.reported_id,r.device_identifier,r.mac_address,r.ip_address,CASE WHEN r.kind=6 THEN workers.summary ELSE r.worker_name END) AS search_text
 FROM asset_keys r
 CROSS JOIN lookup_needs needs
 CROSS JOIN LATERAL (
   SELECT NOT EXISTS (
     SELECT 1 FROM jsonb_to_recordset(sqlc.arg('authority')::jsonb) AS a(org_wide boolean, sites bigint[])
     WHERE NOT (CASE WHEN a.org_wide THEN r.site_id IS NULL OR NOT (r.site_id = ANY(a.sites))
                    ELSE COALESCE(r.site_id = ANY(a.sites), FALSE) END)
   ) AS telemetry_allowed
 ) access
 LEFT JOIN LATERAL (
   SELECT CASE
     WHEN count(*)=0 OR bool_and(h.worker_name_mode='none') THEN ''
     WHEN count(DISTINCT h.worker_name_mode)>1 THEN 'Mixed'
     WHEN bool_and(h.worker_name_mode='mac_address') THEN 'MAC address'
     WHEN count(DISTINCT h.worker_name)=1 THEN min(h.worker_name)
     ELSE 'Mixed'
   END AS summary
   FROM hashboard_installation i JOIN hashboard h ON h.id=i.hashboard_id AND h.org_id=r.org_id
   WHERE r.kind=6 AND i.module_id=r.id AND i.removed_at IS NULL
     AND i.site_id IS NOT DISTINCT FROM r.site_id
 ) workers ON r.kind=6
 LEFT JOIN device_status ds ON r.kind=5 AND ds.device_id=r.id
 LEFT JOIN fleet_node_device fnd ON r.kind=5 AND fnd.device_id=r.id AND fnd.org_id=r.org_id
 LEFT JOIN fleet_node_live fn ON fn.id=fnd.fleet_node_id AND fn.org_id=r.org_id
 LEFT JOIN LATERAL (
   SELECT a.state, a.observed_at, a.hashrate_hs, a.power_w, a.temperature_c, a.miner_efficiency_jth
   FROM fleet_mining_asset a
   WHERE needs.asset_state_or_metrics AND a.org_id=r.org_id AND a.kind=r.kind AND a.id=r.id
   LIMIT 1
 ) details ON TRUE
 LEFT JOIN LATERAL (
   SELECT count(DISTINCT e.component_type) FILTER (WHERE e.component_type IN (1,2,3,4))::bigint AS issue_count,
     array_agg(DISTINCT e.component_type) AS issue_types, bool_or(e.severity IN (1,2,3,4)) AS actionable
   FROM errors e
   WHERE needs.open_issues AND r.kind=5 AND access.telemetry_allowed
     AND e.org_id=r.org_id AND e.closed_at IS NULL AND e.device_id=r.id
 ) issues ON r.kind=5 AND access.telemetry_allowed
 LEFT JOIN LATERAL (
   SELECT voltage_v, current_a FROM device_metrics
   WHERE needs.voltage_or_current AND r.kind=5 AND device_identifier=r.device_identifier AND time=details.observed_at
   LIMIT 1
 ) dm ON TRUE
), filtered AS (
 SELECT r.kind, r.id, r.org_id, r.telemetry_allowed, r.worker_name_summary,
   COALESCE(CASE sqlc.arg('sort_field')::int WHEN 1 THEN r.name
     WHEN 8 THEN r.model WHEN 9 THEN r.firmware_version WHEN 10 THEN encode(inet_send(r.ip_address_inet),'hex')
     WHEN 11 THEN r.mac_address WHEN 12 THEN r.worker_name ELSE '' END, '')::text AS sort_text,
   CASE sqlc.arg('sort_field')::int WHEN 2 THEN r.hashrate_ths WHEN 3 THEN r.power_kw
     WHEN 4 THEN r.temperature_c WHEN 5 THEN r.efficiency_jth
     WHEN 6 THEN 1::double precision WHEN 7 THEN r.issue_count::double precision
     WHEN 8 THEN CASE WHEN r.model IS NOT NULL THEN 0 END
     WHEN 9 THEN CASE WHEN r.firmware_version IS NOT NULL THEN 0 END
     WHEN 10 THEN CASE WHEN r.ip_address_inet IS NOT NULL THEN 0 END
     WHEN 11 THEN CASE WHEN r.mac_address IS NOT NULL THEN 0 END
     WHEN 12 THEN CASE WHEN r.worker_name IS NOT NULL THEN 0 END
     ELSE 0::double precision END AS sort_number
 FROM facts r CROSS JOIN options f
 WHERE NOT EXISTS (
     SELECT 1 FROM jsonb_to_recordset(sqlc.arg('authority')::jsonb) AS a(org_wide boolean, sites bigint[], telemetry_only boolean)
     WHERE NOT (CASE WHEN a.org_wide THEN r.site_id IS NULL OR NOT (r.site_id = ANY(a.sites))
                    ELSE COALESCE(r.site_id = ANY(a.sites), FALSE) END)
   )
   AND ((cardinality(sqlc.arg('site_ids')::bigint[]) = 0 AND NOT sqlc.arg('include_unassigned')::boolean)
        OR r.site_id = ANY(sqlc.arg('site_ids')::bigint[]) OR (sqlc.arg('include_unassigned')::boolean AND r.site_id IS NULL))
   AND (sqlc.arg('search_query')::text = '' OR strpos(lower(r.search_text), lower(sqlc.arg('search_query'))) > 0)
   AND ((NOT f.no_facility AND NOT EXISTS (
     SELECT 1 FROM jsonb_to_recordset(sqlc.arg('ancestors')::jsonb) AS p(kind int, id bigint) WHERE p.kind IN (1,2)
   )) OR (f.no_facility AND r.kind=5 AND r.facility_id IS NULL AND (r.group_id IS NOT NULL OR f.no_group)) OR EXISTS (
     SELECT 1 FROM jsonb_to_recordset(sqlc.arg('ancestors')::jsonb) AS p(kind int, id bigint)
     WHERE r.facility_kind = p.kind AND r.facility_id = p.id
   ))
   AND ((NOT f.no_group AND NOT EXISTS (
     SELECT 1 FROM jsonb_to_recordset(sqlc.arg('ancestors')::jsonb) AS p(kind int, id bigint) WHERE p.kind IN (3,4)
   )) OR (f.no_group AND r.kind=5 AND r.group_id IS NULL) OR EXISTS (
     SELECT 1 FROM jsonb_to_recordset(sqlc.arg('ancestors')::jsonb) AS p(kind int, id bigint)
     WHERE r.group_kind = p.kind AND r.group_id = p.id
   ))
   AND (NOT f.miner_only OR r.kind=5)
   AND (COALESCE(cardinality(f.states),0)=0 OR r.state=ANY(f.states))
   AND (COALESCE(cardinality(f.issues),0)=0 OR r.issue_types && f.issues)
   AND (COALESCE(cardinality(f.models),0)=0 OR r.model=ANY(f.models))
   AND (COALESCE(cardinality(f.firmware),0)=0 OR r.firmware_version=ANY(f.firmware))
   AND (COALESCE(cardinality(f.pairing),0)=0 OR r.pairing_status=ANY(f.pairing))
   AND (COALESCE(f.search,'')='' OR strpos(lower(r.search_text),lower(f.search))>0)
   AND ((COALESCE(cardinality(f.sites),0)=0 AND NOT f.unassigned) OR r.site_id=ANY(f.sites) OR (f.unassigned AND r.site_id IS NULL))
   AND (COALESCE(cardinality(f.groups),0)=0 OR EXISTS (
     SELECT 1 FROM device_set_membership ms WHERE ms.org_id=r.org_id AND ms.device_id=r.id
       AND ms.device_set_type='group' AND ms.device_set_id=ANY(f.groups) AND r.kind=5))
   AND ((COALESCE(cardinality(f.racks),0)=0 AND NOT f.no_rack) OR
        (r.kind=5 AND (r.group_id=ANY(f.racks) OR (f.no_rack AND r.group_id IS NULL))))
   AND ((COALESCE(cardinality(f.building_ids),0)=0 AND NOT f.no_building
         AND NOT (f.no_rack AND COALESCE(cardinality(f.racks),0)=0)) OR
     (r.kind=5 AND (
       r.facility_id=ANY(f.building_ids)
       OR (f.no_building AND NOT (f.no_rack AND COALESCE(cardinality(f.racks),0)=0)
           AND r.group_id IS NOT NULL AND r.facility_id IS NULL)
       OR (f.no_rack AND (COALESCE(cardinality(f.racks),0)=0 OR f.no_building) AND r.kind=5 AND r.group_id IS NULL
           AND ((COALESCE(cardinality(f.building_ids),0)=0 AND NOT f.no_building) OR r.facility_id IS NULL))
     )))
   AND (COALESCE(jsonb_array_length(NULLIF(f.zones,'null'::jsonb)),0)=0 OR EXISTS (
     SELECT 1 FROM jsonb_to_recordset(f.zones) AS z(building_id bigint,zone text)
     WHERE r.kind=5 AND r.zone=z.zone AND (COALESCE(z.building_id,0)=0 OR r.facility_id=z.building_id)))
   AND ((COALESCE(cardinality(f.cidrs),0)=0 AND COALESCE(jsonb_array_length(NULLIF(f.ip_ranges,'null'::jsonb)),0)=0)
     OR r.ip_address_inet <<= ANY(f.cidrs) OR EXISTS (
       SELECT 1 FROM jsonb_to_recordset(NULLIF(f.ip_ranges,'null'::jsonb)) AS ip(start inet,"end" inet)
       WHERE r.ip_address_inet BETWEEN ip.start AND ip."end"))
   AND (COALESCE(cardinality(f.status),0)=0 OR (
     (r.device_status=ANY(f.status) AND (r.device_status IN ('OFFLINE','MAINTENANCE','INACTIVE','NEEDS_MINING_POOL')
       OR (r.device_status='ACTIVE' AND NOT COALESCE(r.actionable,FALSE)) OR 'ERROR'=ANY(f.status)))
     OR ('ERROR'=ANY(f.status) AND r.pairing_status='AUTHENTICATION_NEEDED' AND r.device_status IS DISTINCT FROM 'OFFLINE')
     OR ('ERROR'=ANY(f.status) AND r.actionable AND NOT (r.device_status IS NULL AND r.pairing_status IN ('PAIRED','DEFAULT_PASSWORD'))
       AND (r.device_status IS NULL OR r.device_status NOT IN ('OFFLINE','MAINTENANCE','INACTIVE','NEEDS_MINING_POOL')))
     OR ('OFFLINE'=ANY(f.status) AND r.device_status IS NULL AND r.pairing_status IN ('PAIRED','DEFAULT_PASSWORD'))
   ))
   AND NOT EXISTS (
     SELECT 1 FROM jsonb_to_recordset(f.ranges) AS v(field int, minimum float8, maximum float8, min_inclusive boolean, max_inclusive boolean)
     CROSS JOIN LATERAL (SELECT CASE v.field WHEN 1 THEN r.hashrate_ths WHEN 2 THEN r.efficiency_jth
       WHEN 3 THEN r.power_kw WHEN 4 THEN r.temperature_c WHEN 5 THEN r.voltage_v WHEN 6 THEN r.current_a END AS metric) m
     WHERE m.metric IS NULL OR r.device_status='OFFLINE'
       OR (v.minimum IS NOT NULL AND (m.metric<v.minimum OR (m.metric=v.minimum AND NOT v.min_inclusive)))
       OR (v.maximum IS NOT NULL AND (m.metric>v.maximum OR (m.metric=v.maximum AND NOT v.max_inclusive))))
   AND NOT EXISTS (
     SELECT 1 FROM jsonb_to_recordset(sqlc.arg('ranges')::jsonb) AS f(field int, minimum double precision, maximum double precision, min_inclusive boolean, max_inclusive boolean)
     CROSS JOIN LATERAL (
       SELECT CASE f.field WHEN 1 THEN r.hashrate_ths WHEN 2 THEN r.efficiency_jth
         WHEN 3 THEN r.power_kw WHEN 4 THEN r.temperature_c END AS metric
     ) v
     WHERE v.metric IS NULL
       OR (f.minimum IS NOT NULL AND (v.metric < f.minimum OR (v.metric = f.minimum AND NOT f.min_inclusive)))
       OR (f.maximum IS NOT NULL AND (v.metric > f.maximum OR (v.metric = f.maximum AND NOT f.max_inclusive)))
   )
), page AS (
 SELECT f.*, row_number() OVER (
   ORDER BY CASE WHEN NOT sqlc.arg('descending')::boolean THEN sort_number END ASC NULLS LAST,
            CASE WHEN sqlc.arg('descending')::boolean THEN sort_number END DESC NULLS LAST,
            CASE WHEN NOT sqlc.arg('descending')::boolean THEN sort_text END ASC,
            CASE WHEN sqlc.arg('descending')::boolean THEN sort_text END DESC, kind, id
 ) AS ordinal
 FROM filtered f
 WHERE NOT sqlc.arg('has_cursor')::boolean OR
   (NOT sqlc.arg('cursor_null')::boolean AND sort_number IS NULL) OR
   (sort_number IS NOT NULL AND NOT sqlc.arg('cursor_null')::boolean AND
     CASE WHEN sqlc.arg('descending')::boolean THEN (sort_number, sort_text) < (sqlc.arg('cursor_number')::double precision, sqlc.arg('cursor_text')::text)
          ELSE (sort_number, sort_text) > (sqlc.arg('cursor_number')::double precision, sqlc.arg('cursor_text')::text) END) OR
   (((sqlc.arg('cursor_null')::boolean AND sort_number IS NULL) OR
      (NOT sqlc.arg('cursor_null')::boolean AND sort_number = sqlc.arg('cursor_number')::double precision AND sort_text = sqlc.arg('cursor_text')::text))
     AND (kind, id) > (sqlc.arg('cursor_kind')::int, sqlc.arg('cursor_id')::bigint))
 ORDER BY ordinal
 LIMIT sqlc.arg('page_limit')::int
), counts AS (
 SELECT count(*)::bigint AS total_count, count(*) FILTER (WHERE kind=5)::bigint AS miner_count
 FROM filtered
)
SELECT COALESCE((SELECT jsonb_agg(jsonb_build_object(
 'kind',a.kind,'id',a.id,'site_id',a.site_id,'name',a.name,'device_identifier',a.device_identifier,
 'position',a.position,'reported_id',a.reported_id,'state',a.state,
 'facility_kind',a.facility_kind,'facility_id',a.facility_id,'group_kind',a.group_kind,'group_id',a.group_id,
 'telemetry_allowed',p.telemetry_allowed,'worker_name_summary',p.worker_name_summary,
 'sort_number',p.sort_number,'sort_text',p.sort_text,'asset_count',1,
 'observed_at',CASE WHEN p.telemetry_allowed THEN a.observed_at END,
 'hashrate_ths',CASE WHEN p.telemetry_allowed THEN a.hashrate_hs / 1e12 END,
 'power_kw',CASE WHEN p.telemetry_allowed THEN a.power_w / 1e3 END,
 'min_temperature_c',CASE WHEN p.telemetry_allowed THEN a.temperature_c END,
 'max_temperature_c',CASE WHEN p.telemetry_allowed THEN a.temperature_c END,
 'efficiency_jth',CASE WHEN p.telemetry_allowed THEN a.miner_efficiency_jth END,
 'hashrate_reporting_count',CASE WHEN p.telemetry_allowed AND a.hashrate_hs IS NOT NULL THEN 1 ELSE 0 END,
 'power_reporting_count',CASE WHEN p.telemetry_allowed AND a.power_w IS NOT NULL THEN 1 ELSE 0 END,
 'coverage',CASE WHEN p.telemetry_allowed THEN cm.coverage END,
 'module_issues',CASE WHEN p.telemetry_allowed AND module.observation_site_id IS NOT DISTINCT FROM a.site_id THEN module.issues END,
 'module_issues_last_seen_at',module.last_seen_at,
 'site_name',COALESCE(site.name,''), 'facility_name',COALESCE(parent_b.name,parent_d.custom_name,'Container '||parent_d.serial_number,''),
 'group_name',COALESCE(parent_s.label,'Tank '||parent_t.position,''),
 'hashing_count',CASE WHEN p.telemetry_allowed AND a.state='active' THEN 1 ELSE 0 END,
 'sleeping_count',CASE WHEN p.telemetry_allowed AND a.state IN ('inactive','maintenance') THEN 1 ELSE 0 END,
 'offline_count',CASE WHEN p.telemetry_allowed AND a.state='offline' THEN 1 ELSE 0 END,
 'broken_count',CASE WHEN p.telemetry_allowed AND a.state IN ('error','needs_mining_pool','updating','reboot_required') THEN 1 ELSE 0 END,
 'psu_issues',issues.psu_issues,'fan_issues',issues.fan_issues,'hashboard_issues',issues.hashboard_issues,'control_issues',issues.control_issues
 ) ORDER BY p.ordinal)
 FROM page p
 JOIN LATERAL (
   SELECT asset.* FROM fleet_mining_asset asset
   WHERE asset.org_id=p.org_id AND asset.kind=p.kind AND asset.id=p.id
   LIMIT 1
 ) a ON TRUE
 LEFT JOIN container_equipment_current_metrics cm ON p.kind=6 AND cm.kind=6 AND cm.id=p.id
 LEFT JOIN container_module module ON p.kind=6 AND module.id=p.id AND module.retired_at IS NULL
 LEFT JOIN site ON site.id=a.site_id AND site.org_id=p.org_id AND site.deleted_at IS NULL
 LEFT JOIN building parent_b ON a.facility_kind=1 AND parent_b.id=a.facility_id AND parent_b.org_id=p.org_id
 LEFT JOIN device parent_d ON a.facility_kind=2 AND parent_d.id=a.facility_id AND parent_d.org_id=p.org_id
 LEFT JOIN device_set parent_s ON a.group_kind=3 AND parent_s.id=a.group_id AND parent_s.org_id=p.org_id
 LEFT JOIN container_tank parent_t ON a.group_kind=4 AND parent_t.id=a.group_id
 LEFT JOIN LATERAL (
   SELECT count(DISTINCT e.device_id) FILTER (WHERE e.component_type=1)::int AS psu_issues,
     count(DISTINCT e.device_id) FILTER (WHERE e.component_type=2)::int AS hashboard_issues,
     count(DISTINCT e.device_id) FILTER (WHERE e.component_type=3)::int AS fan_issues,
     count(DISTINCT e.device_id) FILTER (WHERE e.component_type=4)::int AS control_issues
   FROM errors e WHERE e.org_id=p.org_id AND e.closed_at IS NULL AND e.device_id=p.id
 ) issues ON p.kind=5 AND p.telemetry_allowed
 ), '[]'::jsonb)::jsonb AS items, counts.total_count, counts.miner_count
FROM counts;
