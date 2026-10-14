-- Facilities: scope, filter, count, and paginate the two entity types together.
-- name: ListFleetFacilities :one
WITH parents AS (
  SELECT 1::int AS kind, b.id, b.org_id, b.site_id, b.name::text AS name, ''::text AS device_identifier,
         0::int AS position, ''::text AS reported_id, 0::int AS facility_kind, NULL::bigint AS facility_id
  FROM building b WHERE b.deleted_at IS NULL
  UNION ALL
  SELECT 2, d.id, d.org_id, d.site_id, COALESCE(NULLIF(d.custom_name, ''), 'Container ' || COALESCE(NULLIF(d.serial_number, ''), d.device_identifier))::text,
         d.device_identifier::text, 0, COALESCE(d.serial_number, '')::text, 0, NULL::bigint
  FROM device d JOIN discovered_device dd ON dd.id = d.discovered_device_id AND dd.org_id = d.org_id
  WHERE dd.device_class = 2 AND d.deleted_at IS NULL AND dd.deleted_at IS NULL
), aggregates AS (
  SELECT ma.org_id, ma.facility_kind, ma.facility_id, count(*)::bigint AS asset_count,
         count(ma.hashrate_hs)::bigint AS hashrate_reporting_count, count(ma.power_w)::bigint AS power_reporting_count,
         CASE WHEN count(ma.hashrate_hs) = count(*) THEN sum(ma.hashrate_hs) / 1e12 END::double precision AS hashrate_ths,
         CASE WHEN count(ma.power_w) = count(*) THEN sum(ma.power_w) / 1e3 END::double precision AS power_kw,
         CASE WHEN count(ma.temperature_c) = count(*) THEN min(ma.temperature_c) END::double precision AS min_temperature_c,
         CASE WHEN count(ma.temperature_c) = count(*) THEN max(ma.temperature_c) END::double precision AS max_temperature_c,
         min(ma.observed_at) AS observed_at
  FROM fleet_mining_asset ma
  WHERE ma.org_id = sqlc.arg('org_id') AND ma.facility_id IS NOT NULL
  GROUP BY ma.org_id, ma.facility_kind, ma.facility_id
), options AS (
 SELECT ARRAY(SELECT value::int FROM jsonb_array_elements_text(NULLIF(sqlc.arg('special_filters')::jsonb->'issues','null'::jsonb)))::int[] AS issues
), facts AS (
 SELECT r.kind AS kind, r.id AS id, r.org_id AS org_id, r.site_id AS site_id,
   r.name AS name, r.device_identifier AS device_identifier, r.position AS position, r.reported_id AS reported_id,
   r.facility_kind AS facility_kind, r.facility_id AS facility_id,
   CASE WHEN access.telemetry_allowed THEN CASE WHEN r.kind=2 THEN cm.hashrate_hs / 1e12 ELSE a.hashrate_ths END END AS hashrate_ths,
   CASE WHEN access.telemetry_allowed THEN CASE WHEN r.kind=2 THEN cm.power_w / 1000 ELSE a.power_kw END END AS power_kw,
   CASE WHEN access.telemetry_allowed THEN CASE WHEN r.kind=2 THEN cm.min_temperature_c ELSE a.min_temperature_c END END AS min_temperature_c,
   CASE WHEN access.telemetry_allowed THEN CASE WHEN r.kind=2 THEN cm.max_temperature_c ELSE a.max_temperature_c END END AS max_temperature_c,
   CASE WHEN access.telemetry_allowed THEN CASE WHEN r.kind=2 THEN cm.time ELSE a.observed_at END END AS observed_at,
   CASE WHEN access.telemetry_allowed THEN (CASE WHEN r.kind=2 THEN cm.efficiency_jh * 1e12 ELSE a.power_kw * 1000 / NULLIF(a.hashrate_ths, 0) END)::double precision END AS efficiency_jth,
   COALESCE(a.asset_count, 0)::bigint AS asset_count,
   CASE WHEN access.telemetry_allowed THEN COALESCE(CASE WHEN r.kind=2 THEN CASE WHEN cm.hashrate_hs IS NOT NULL THEN (cm.coverage#>>'{hashrate,reporting}')::bigint END ELSE a.hashrate_reporting_count END, 0) ELSE 0 END::bigint AS hashrate_reporting_count,
   CASE WHEN access.telemetry_allowed THEN COALESCE(CASE WHEN r.kind=2 THEN CASE WHEN cm.power_w IS NOT NULL THEN (cm.coverage#>>'{power,reporting}')::bigint END ELSE a.power_reporting_count END, 0) ELSE 0 END::bigint AS power_reporting_count,
   CASE WHEN access.telemetry_allowed THEN cm.coverage END AS coverage,
   access.telemetry_allowed AS telemetry_allowed,
   issues.issue_count, issues.issue_types,
   CASE WHEN access.telemetry_allowed THEN cm.hashrate_hs / 1e12 END AS reported_hashrate_ths,
   CASE WHEN access.telemetry_allowed THEN cm.power_w / 1e3 END AS reported_power_kw
 FROM parents r
 LEFT JOIN aggregates a ON a.org_id=r.org_id AND a.facility_kind=r.kind AND a.facility_id=r.id
 LEFT JOIN container_equipment_current_metrics cm ON cm.kind=r.kind AND cm.id=r.id AND r.kind=2
 CROSS JOIN LATERAL (
   SELECT NOT EXISTS (
     SELECT 1 FROM jsonb_to_recordset(sqlc.arg('authority')::jsonb) AS a(org_wide boolean, sites bigint[])
     WHERE NOT (CASE WHEN a.org_wide THEN r.site_id IS NULL OR NOT (r.site_id = ANY(a.sites))
                    ELSE COALESCE(r.site_id = ANY(a.sites), FALSE) END)
   ) AS telemetry_allowed
 ) access
 LEFT JOIN LATERAL (
   SELECT count(DISTINCT (e.device_id, e.component_type)) FILTER (WHERE e.component_type IN (1,2,3,4))::bigint AS issue_count,
          array_agg(DISTINCT e.component_type) AS issue_types
   FROM errors e
   WHERE e.org_id=r.org_id AND e.closed_at IS NULL AND EXISTS (
     SELECT 1 FROM fleet_mining_asset a WHERE a.kind=5 AND a.id=e.device_id AND a.org_id=r.org_id
       AND a.facility_kind=r.kind AND a.facility_id=r.id
   )
 ) issues ON r.kind=1 AND access.telemetry_allowed
 WHERE r.org_id=sqlc.arg('org_id') AND r.kind=ANY(sqlc.arg('kinds')::int[])
), filtered AS (
 SELECT r.*,
   COALESCE(CASE sqlc.arg('sort_field')::int WHEN 1 THEN r.name ELSE '' END, '')::text AS sort_text,
   CASE sqlc.arg('sort_field')::int WHEN 2 THEN r.hashrate_ths WHEN 3 THEN r.power_kw
     WHEN 4 THEN r.max_temperature_c WHEN 5 THEN r.efficiency_jth
     WHEN 6 THEN r.asset_count::double precision WHEN 7 THEN r.issue_count::double precision
     ELSE 0::double precision END AS sort_number
 FROM facts r CROSS JOIN options f
 WHERE TRUE
   AND NOT EXISTS (
     SELECT 1 FROM jsonb_to_recordset(sqlc.arg('authority')::jsonb) AS a(org_wide boolean, sites bigint[], telemetry_only boolean)
     WHERE NOT (r.kind=1 AND COALESCE(a.telemetry_only,FALSE)) AND NOT (CASE WHEN a.org_wide THEN r.site_id IS NULL OR NOT (r.site_id = ANY(a.sites))
                    ELSE COALESCE(r.site_id = ANY(a.sites), FALSE) END)
   )
   AND ((cardinality(sqlc.arg('site_ids')::bigint[]) = 0 AND NOT sqlc.arg('include_unassigned')::boolean)
        OR r.site_id = ANY(sqlc.arg('site_ids')::bigint[]) OR (sqlc.arg('include_unassigned')::boolean AND r.site_id IS NULL))
   AND (sqlc.arg('search_query')::text = '' OR strpos(lower(concat_ws(' ',r.name,r.reported_id,r.device_identifier)), lower(sqlc.arg('search_query'))) > 0)
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
 'building_details',to_jsonb(b), 'site_name',COALESCE(site.name,''),
 'rack_count',COALESCE(counts.rack_count,0),
 'hashing_count',health.hashing_count,'sleeping_count',health.sleeping_count,
 'offline_count',health.offline_count,'broken_count',health.broken_count,
 'psu_issues',issues.psu_issues,'fan_issues',issues.fan_issues,'hashboard_issues',issues.hashboard_issues,'control_issues',issues.control_issues
 ) ORDER BY p.ordinal)
 FROM page p
 LEFT JOIN building b ON p.kind=1 AND b.id=p.id AND b.org_id=p.org_id
 LEFT JOIN LATERAL (
   SELECT count(*)::bigint AS rack_count FROM device_set_rack dr JOIN device_set ds ON ds.id=dr.device_set_id
   WHERE p.kind=1 AND dr.building_id=p.id AND ds.org_id=p.org_id AND ds.deleted_at IS NULL
 ) counts ON TRUE
 LEFT JOIN site ON site.id=p.site_id AND site.org_id=p.org_id AND site.deleted_at IS NULL
 LEFT JOIN LATERAL (
   SELECT count(*) FILTER (WHERE a.state='hashing')::int AS hashing_count,
          count(*) FILTER (WHERE a.state='sleeping')::int AS sleeping_count,
          count(*) FILTER (WHERE a.state='offline')::int AS offline_count,
          count(*) FILTER (WHERE a.state IN ('attention','isolated'))::int AS broken_count
   FROM fleet_mining_asset a WHERE a.org_id=p.org_id AND a.facility_kind=p.kind AND a.facility_id=p.id
 ) health ON p.telemetry_allowed
 LEFT JOIN LATERAL (
   SELECT count(DISTINCT e.device_id) FILTER (WHERE e.component_type=1)::int AS psu_issues,
     count(DISTINCT e.device_id) FILTER (WHERE e.component_type=2)::int AS hashboard_issues,
     count(DISTINCT e.device_id) FILTER (WHERE e.component_type=3)::int AS fan_issues,
     count(DISTINCT e.device_id) FILTER (WHERE e.component_type=4)::int AS control_issues
   FROM errors e WHERE e.org_id=p.org_id AND e.closed_at IS NULL AND EXISTS (
     SELECT 1 FROM fleet_mining_asset a WHERE a.kind=5 AND a.id=e.device_id AND a.org_id=p.org_id
       AND a.facility_kind=p.kind AND a.facility_id=p.id)
 ) issues ON p.kind=1 AND p.telemetry_allowed
 ), '[]'::jsonb)::jsonb AS items,
       (SELECT count(*) FROM filtered)::bigint AS total_count;
