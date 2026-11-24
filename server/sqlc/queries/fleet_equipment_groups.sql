-- Equipment groups: scope, filter, count, and paginate the two entity types together.
-- name: ListFleetEquipmentGroups :one
WITH parents AS (
  SELECT 3::int AS kind, s.id, s.org_id, r.site_id, s.label::text AS name, ''::text AS device_identifier,
         0::int AS position, ''::text AS reported_id, 1::int AS facility_kind, r.building_id AS facility_id, r.zone
  FROM device_set s JOIN device_set_rack r ON r.device_set_id = s.id WHERE s.deleted_at IS NULL
  UNION ALL
  SELECT 4, t.id, d.org_id, d.site_id,
         (COALESCE(NULLIF(d.custom_name, ''), 'Container ' || COALESCE(NULLIF(d.serial_number, ''), d.device_identifier)) || ' T' || t.position)::text,
         d.device_identifier::text, t.position, '', 2, d.id, NULL::text
  FROM container_tank t JOIN device d ON d.id = t.device_id
  JOIN discovered_device dd ON dd.id = d.discovered_device_id
  WHERE t.present AND d.deleted_at IS NULL AND dd.deleted_at IS NULL
), aggregates AS (
  SELECT ma.org_id, ma.group_kind, ma.group_id, count(*)::bigint AS asset_count,
         count(ma.temperature_c)::bigint AS temperature_reporting_count,
         count(*) FILTER (WHERE ma.hashrate_hs IS NOT NULL AND ma.power_w IS NOT NULL)::bigint AS efficiency_reporting_count,
         count(ma.hashrate_hs)::bigint AS hashrate_reporting_count, count(ma.power_w)::bigint AS power_reporting_count,
         CASE WHEN ma.group_kind = 4 OR count(ma.hashrate_hs) = count(*) THEN sum(ma.hashrate_hs) / 1e12 END::double precision AS hashrate_ths,
         CASE WHEN ma.group_kind = 4 OR count(ma.power_w) = count(*) THEN sum(ma.power_w) / 1e3 END::double precision AS power_kw,
         CASE WHEN ma.group_kind = 4 OR count(ma.temperature_c) = count(*) THEN min(ma.temperature_c) END::double precision AS min_temperature_c,
         CASE WHEN ma.group_kind = 4 OR count(ma.temperature_c) = count(*) THEN max(ma.temperature_c) END::double precision AS max_temperature_c,
         (sum(ma.power_w) FILTER (WHERE ma.hashrate_hs IS NOT NULL) * 1e12 /
          NULLIF(sum(ma.hashrate_hs) FILTER (WHERE ma.power_w IS NOT NULL), 0))::double precision AS paired_efficiency_jth,
         min(ma.observed_at) AS observed_at
  FROM fleet_mining_asset ma
  WHERE ma.org_id = sqlc.arg('org_id') AND ma.group_id IS NOT NULL
  GROUP BY ma.org_id, ma.group_kind, ma.group_id
), options AS (
 SELECT ARRAY(SELECT value::int FROM jsonb_array_elements_text(NULLIF(sqlc.arg('special_filters')::jsonb->'issues','null'::jsonb)))::int[] AS issues,
   ARRAY(SELECT value::bigint FROM jsonb_array_elements_text(NULLIF(sqlc.arg('special_filters')::jsonb->'building_ids','null'::jsonb)))::bigint[] AS building_ids,
   COALESCE((sqlc.arg('special_filters')::jsonb->>'no_facility')::boolean,FALSE) AS no_facility,
   COALESCE((sqlc.arg('special_filters')::jsonb->>'no_group')::boolean,FALSE) AS no_group,
   COALESCE((sqlc.arg('special_filters')::jsonb->>'no_building')::boolean,FALSE) AS no_building,
   NULLIF(sqlc.arg('special_filters')::jsonb->'zones','null'::jsonb) AS zones
), facts AS (
 SELECT r.kind AS kind, r.id AS id, r.org_id AS org_id, r.site_id AS site_id,
   r.name AS name, r.device_identifier AS device_identifier, r.position AS position, r.reported_id AS reported_id,
   r.facility_kind AS facility_kind, r.facility_id AS facility_id, r.zone AS zone,
   CASE WHEN access.telemetry_allowed THEN CASE WHEN r.kind=4 THEN cm.hashrate_hs / 1e12 ELSE a.hashrate_ths END END AS hashrate_ths,
   CASE WHEN access.telemetry_allowed THEN CASE WHEN r.kind=4 THEN cm.power_w / 1000 ELSE a.power_kw END END AS power_kw,
   CASE WHEN access.telemetry_allowed THEN CASE WHEN r.kind=4 THEN cm.min_temperature_c ELSE a.min_temperature_c END END AS min_temperature_c,
   CASE WHEN access.telemetry_allowed THEN CASE WHEN r.kind=4 THEN cm.max_temperature_c ELSE a.max_temperature_c END END AS max_temperature_c,
   CASE WHEN access.telemetry_allowed THEN CASE WHEN r.kind=4 THEN cm.time ELSE a.observed_at END END AS observed_at,
   CASE WHEN access.telemetry_allowed THEN (CASE WHEN r.kind = 4 THEN cm.efficiency_jh * 1e12 ELSE a.power_kw * 1000 / NULLIF(a.hashrate_ths, 0) END)::double precision END AS efficiency_jth,
   COALESCE(a.asset_count, 0)::bigint AS asset_count,
   CASE WHEN access.telemetry_allowed THEN COALESCE(CASE WHEN r.kind=4 THEN CASE WHEN cm.hashrate_hs IS NOT NULL THEN (cm.coverage#>>'{hashrate,reporting}')::bigint END ELSE a.hashrate_reporting_count END, 0) ELSE 0 END::bigint AS hashrate_reporting_count,
   CASE WHEN access.telemetry_allowed THEN COALESCE(CASE WHEN r.kind=4 THEN CASE WHEN cm.power_w IS NOT NULL THEN (cm.coverage#>>'{power,reporting}')::bigint END ELSE a.power_reporting_count END, 0) ELSE 0 END::bigint AS power_reporting_count,
   CASE WHEN access.telemetry_allowed THEN COALESCE(CASE WHEN r.kind=4 THEN CASE WHEN cm.temperature_c IS NOT NULL THEN (cm.coverage#>>'{temperature,reporting}')::bigint END ELSE a.temperature_reporting_count END, 0) ELSE 0 END::bigint AS temperature_reporting_count,
   CASE WHEN access.telemetry_allowed THEN COALESCE(CASE WHEN r.kind=4 THEN CASE WHEN cm.efficiency_jh IS NOT NULL THEN (cm.coverage#>>'{hashrate,reporting}')::bigint END ELSE a.efficiency_reporting_count END, 0) ELSE 0 END::bigint AS efficiency_reporting_count,
   CASE WHEN access.telemetry_allowed THEN cm.coverage END AS coverage,
   access.telemetry_allowed AS telemetry_allowed,
   issues.issue_count, issues.issue_types
 FROM parents r
 LEFT JOIN aggregates a ON a.org_id=r.org_id AND a.group_kind=r.kind AND a.group_id=r.id
 LEFT JOIN container_equipment_current_metrics cm ON cm.kind=r.kind AND cm.id=r.id AND r.kind=4
 CROSS JOIN LATERAL (
   SELECT NOT EXISTS (
     SELECT 1 FROM jsonb_to_recordset(sqlc.arg('authority')::jsonb) AS a(org_wide boolean, sites bigint[], rack_only boolean)
     WHERE (NOT COALESCE(a.rack_only, FALSE) OR r.kind = 3)
       AND NOT (CASE WHEN a.org_wide THEN r.site_id IS NULL OR NOT (r.site_id = ANY(a.sites))
                    ELSE COALESCE(r.site_id = ANY(a.sites), FALSE) END)
   ) AS telemetry_allowed
 ) access
 LEFT JOIN LATERAL (
   SELECT count(DISTINCT (e.device_id, e.component_type)) FILTER (WHERE e.component_type IN (1,2,3,4))::bigint AS issue_count,
          array_agg(DISTINCT e.component_type) AS issue_types
   FROM errors e
   WHERE e.org_id=r.org_id AND e.closed_at IS NULL AND EXISTS (
     SELECT 1 FROM fleet_mining_asset a WHERE a.kind=5 AND a.id=e.device_id AND a.org_id=r.org_id
       AND a.group_kind=r.kind AND a.group_id=r.id
   )
 ) issues ON r.kind=3 AND access.telemetry_allowed
 WHERE r.org_id=sqlc.arg('org_id') AND r.kind=ANY(sqlc.arg('kinds')::int[])
), filtered AS (
 SELECT r.*,
   COALESCE(CASE sqlc.arg('sort_field')::int WHEN 1 THEN r.name WHEN 13 THEN r.zone ELSE '' END, '')::text AS sort_text,
   CASE sqlc.arg('sort_field')::int WHEN 2 THEN r.hashrate_ths WHEN 3 THEN r.power_kw
     WHEN 4 THEN r.max_temperature_c WHEN 5 THEN r.efficiency_jth
     WHEN 6 THEN r.asset_count::double precision WHEN 7 THEN r.issue_count::double precision
     WHEN 13 THEN CASE WHEN r.zone IS NOT NULL THEN 0 END
     ELSE 0::double precision END AS sort_number
 FROM facts r CROSS JOIN options f
 WHERE TRUE
   AND NOT EXISTS (
     SELECT 1 FROM jsonb_to_recordset(sqlc.arg('authority')::jsonb) AS a(org_wide boolean, sites bigint[], rack_only boolean)
     WHERE (NOT COALESCE(a.rack_only, FALSE) OR r.kind = 3)
       AND NOT (CASE WHEN a.org_wide THEN r.site_id IS NULL OR NOT (r.site_id = ANY(a.sites))
                    ELSE COALESCE(r.site_id = ANY(a.sites), FALSE) END)
   )
   AND ((cardinality(sqlc.arg('site_ids')::bigint[]) = 0 AND NOT sqlc.arg('include_unassigned')::boolean)
        OR r.site_id = ANY(sqlc.arg('site_ids')::bigint[]) OR (sqlc.arg('include_unassigned')::boolean AND r.site_id IS NULL))
   AND (sqlc.arg('search_query')::text = '' OR strpos(lower(concat_ws(' ',r.name,r.reported_id,r.device_identifier)), lower(sqlc.arg('search_query'))) > 0)
   AND ((NOT f.no_facility AND jsonb_array_length(sqlc.arg('ancestors')::jsonb) = 0)
     OR (f.no_facility AND r.kind=3 AND r.facility_id IS NULL) OR EXISTS (
     SELECT 1 FROM jsonb_to_recordset(sqlc.arg('ancestors')::jsonb) AS p(kind int, id bigint)
     WHERE r.facility_kind=p.kind AND r.facility_id=p.id
   ))
   AND ((COALESCE(cardinality(f.building_ids),0)=0 AND NOT f.no_building) OR
        (r.kind=3 AND (r.facility_id=ANY(f.building_ids) OR (f.no_building AND r.facility_id IS NULL))))
   AND (COALESCE(jsonb_array_length(f.zones),0)=0 OR EXISTS (
     SELECT 1 FROM jsonb_to_recordset(f.zones) AS z(building_id bigint, zone text)
     WHERE r.kind=3 AND r.zone=z.zone AND (COALESCE(z.building_id,0)=0 OR r.facility_id=z.building_id)))
   AND (COALESCE(cardinality(f.issues),0)=0 OR r.issue_types && f.issues)
   AND NOT EXISTS (
     SELECT 1 FROM jsonb_to_recordset(sqlc.arg('ranges')::jsonb) AS f(field int, minimum double precision, maximum double precision, min_inclusive boolean, max_inclusive boolean)
     CROSS JOIN LATERAL (
       SELECT CASE f.field WHEN 1 THEN r.hashrate_ths WHEN 2 THEN r.efficiency_jth WHEN 3 THEN r.power_kw WHEN 4 THEN r.min_temperature_c END AS low,
              CASE f.field WHEN 1 THEN r.hashrate_ths WHEN 2 THEN r.efficiency_jth WHEN 3 THEN r.power_kw WHEN 4 THEN r.max_temperature_c END AS high
     ) v
     WHERE v.low IS NULL OR v.high IS NULL
       OR (f.minimum IS NOT NULL AND (v.low < f.minimum OR (v.low = f.minimum AND NOT f.min_inclusive)))
       OR (f.maximum IS NOT NULL AND (v.high > f.maximum OR (v.high = f.maximum AND NOT f.max_inclusive)))
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
)
SELECT COALESCE((SELECT jsonb_agg(to_jsonb(p) || jsonb_build_object(
 'rack_details',CASE WHEN s.id IS NOT NULL THEN to_jsonb(s) || jsonb_build_object('type',2,'device_count',p.asset_count,'rack_info',to_jsonb(rack)) END,
 'site_name',COALESCE(site.name,''), 'facility_name',COALESCE(parent_b.name,parent_d.custom_name,'Container '||parent_d.serial_number,''),
 'hashing_count',health.hashing_count,'sleeping_count',health.sleeping_count,
 'offline_count',health.offline_count,'broken_count',health.broken_count,
 'psu_issues',issues.psu_issues,'fan_issues',issues.fan_issues,'hashboard_issues',issues.hashboard_issues,'control_issues',issues.control_issues
 ) ORDER BY p.ordinal)
 FROM page p
 LEFT JOIN device_set s ON p.kind=3 AND s.id=p.id AND s.org_id=p.org_id
 LEFT JOIN device_set_rack rack ON rack.device_set_id=s.id
 LEFT JOIN building parent_b ON p.facility_kind=1 AND parent_b.id=p.facility_id AND parent_b.org_id=p.org_id
 LEFT JOIN device parent_d ON p.facility_kind=2 AND parent_d.id=p.facility_id AND parent_d.org_id=p.org_id
 LEFT JOIN site ON site.id=p.site_id AND site.org_id=p.org_id AND site.deleted_at IS NULL
 LEFT JOIN LATERAL (
   SELECT count(*) FILTER (WHERE a.state='active')::int AS hashing_count,
          count(*) FILTER (WHERE a.state IN ('inactive','maintenance'))::int AS sleeping_count,
          count(*) FILTER (WHERE a.state='offline')::int AS offline_count,
          count(*) FILTER (WHERE a.state IN ('error','needs_mining_pool','updating','reboot_required'))::int AS broken_count
   FROM fleet_mining_asset a WHERE a.org_id=p.org_id AND a.group_kind=p.kind AND a.group_id=p.id
 ) health ON p.telemetry_allowed
 LEFT JOIN LATERAL (
   SELECT count(DISTINCT e.device_id) FILTER (WHERE e.component_type=1)::int AS psu_issues,
     count(DISTINCT e.device_id) FILTER (WHERE e.component_type=2)::int AS hashboard_issues,
     count(DISTINCT e.device_id) FILTER (WHERE e.component_type=3)::int AS fan_issues,
     count(DISTINCT e.device_id) FILTER (WHERE e.component_type=4)::int AS control_issues
   FROM errors e WHERE e.org_id=p.org_id AND e.closed_at IS NULL AND EXISTS (
     SELECT 1 FROM fleet_mining_asset a WHERE a.kind=5 AND a.id=e.device_id AND a.org_id=p.org_id
       AND a.group_kind=p.kind AND a.group_id=p.id)
 ) issues ON p.kind=3 AND p.telemetry_allowed
 ), '[]'::jsonb)::jsonb AS items,
       (SELECT count(*) FROM filtered)::bigint AS total_count;
