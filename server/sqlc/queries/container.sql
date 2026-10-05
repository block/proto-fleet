-- name: ListContainers :many
SELECT d.id, d.device_identifier, d.site_id, d.serial_number,
       COALESCE(d.custom_name, '')::text AS name,
       COALESCE(s.name, '')::text AS site_name,
       COALESCE(fans.readings, '[]'::jsonb)::jsonb AS fans,
       COALESCE(plant.metrics, 'null'::jsonb)::jsonb AS plant,
       COALESCE(to_jsonb(controls) || jsonb_build_object('controls', COALESCE(control_states.readings, '[]'::jsonb), 'setpoints', COALESCE(setpoints.readings, '[]'::jsonb)), 'null'::jsonb)::jsonb AS controls,
       ci.observed_at, COALESCE(health.tanks, '[]'::jsonb)::jsonb AS tank_health,
       COALESCE(dm.time, 'epoch'::timestamptz)::timestamptz AS metrics_at, dm.hash_rate_hs, dm.power_w, dm.temp_c
FROM device d
JOIN discovered_device dd ON dd.id = d.discovered_device_id AND dd.org_id = d.org_id
JOIN device_pairing dp ON dp.device_id = d.id
LEFT JOIN site s ON s.id = d.site_id AND s.org_id = d.org_id AND s.deleted_at IS NULL
LEFT JOIN container_inventory ci ON ci.device_id = d.id
LEFT JOIN LATERAL (
 SELECT metrics FROM container_plant_metrics WHERE device_id = d.id
 ORDER BY time DESC LIMIT 1
) plant ON TRUE
LEFT JOIN LATERAL (
 SELECT jsonb_agg(to_jsonb(f) ORDER BY f.component_id) AS readings
 FROM container_fan_observation f WHERE f.device_id = d.id
) fans ON TRUE
LEFT JOIN container_controls_observation controls ON controls.device_id = d.id
LEFT JOIN LATERAL (
 SELECT jsonb_agg(to_jsonb(c) ORDER BY c.name) AS readings
 FROM container_control_observation c WHERE c.device_id = d.id
) control_states ON TRUE
LEFT JOIN LATERAL (
 SELECT jsonb_agg(to_jsonb(sp) ORDER BY sp.name) AS readings
 FROM container_setpoint_observation sp WHERE sp.device_id = d.id
) setpoints ON TRUE
-- Reuse the inventory state projection so cards agree with module lists about
-- freshness, retained partial topology, and retired/replaced hardware.
LEFT JOIN LATERAL (
    SELECT jsonb_agg(to_jsonb(tank) ORDER BY tank.position) AS tanks
    FROM (
        SELECT t.position,
               count(*) FILTER (WHERE a.state = 'hashing') AS hashing_count,
               count(*) FILTER (WHERE a.state = 'attention') AS attention_count,
               count(*) FILTER (WHERE a.state = 'offline') AS offline_count,
               count(*) FILTER (WHERE a.state = 'sleeping') AS sleeping_count,
               count(*) FILTER (WHERE a.state = 'isolated') AS isolated_count,
               count(*) FILTER (WHERE a.state = 'unknown') AS unknown_count
        FROM container_tank t
        LEFT JOIN fleet_mining_asset a ON a.kind = 6 AND a.group_kind = 4 AND a.group_id = t.id
          AND a.facility_kind = 2 AND a.facility_id = d.id AND a.org_id = d.org_id
        WHERE t.device_id = d.id AND t.present
        GROUP BY t.id, t.position
    ) tank
) health ON TRUE
LEFT JOIN LATERAL (
    SELECT time, hash_rate_hs, power_w, temp_c
    FROM device_metrics
    WHERE device_identifier = d.device_identifier
    ORDER BY time DESC LIMIT 1
) dm ON TRUE
WHERE d.org_id = sqlc.arg('org_id') AND d.deleted_at IS NULL
  AND dd.deleted_at IS NULL AND dd.device_class = 2
  AND (cardinality(COALESCE(sqlc.arg('device_identifiers')::text[], '{}'))=0 OR d.device_identifier=ANY(sqlc.arg('device_identifiers')::text[]))
  AND (
       (cardinality(COALESCE(sqlc.arg('site_ids')::bigint[], '{}')) = 0 AND NOT sqlc.arg('include_unassigned')::boolean)
       OR d.site_id = ANY(sqlc.arg('site_ids')::bigint[])
       OR (sqlc.arg('include_unassigned')::boolean AND d.site_id IS NULL)
  )
ORDER BY d.id;

-- name: InsertContainerPlantMetrics :exec
INSERT INTO container_plant_metrics (device_id, time, metrics)
SELECT d.id, sqlc.arg('time')::timestamptz, sqlc.arg('metrics')::jsonb
FROM device d JOIN discovered_device dd ON dd.id=d.discovered_device_id AND dd.org_id=d.org_id
WHERE d.device_identifier=sqlc.arg('device_identifier') AND d.deleted_at IS NULL AND dd.device_class=2
ON CONFLICT (device_id,time) DO NOTHING;

-- name: LockContainerCommandTarget :one
-- Lock current ownership/site through dispatch; child inventory IDs are never targets.
SELECT d.id, d.site_id
FROM device d
JOIN discovered_device dd ON dd.id=d.discovered_device_id AND dd.org_id=d.org_id
JOIN device_pairing dp ON dp.device_id=d.id
WHERE d.device_identifier=sqlc.arg('device_identifier') AND d.org_id=sqlc.arg('org_id')
  AND d.deleted_at IS NULL AND dd.deleted_at IS NULL AND dd.device_class=2
  AND dp.pairing_status IN ('PAIRED','DEFAULT_PASSWORD')
FOR UPDATE OF d, dp;

-- name: HasPendingContainerControl :one
SELECT EXISTS (
 SELECT 1 FROM queue_message
 WHERE device_id=sqlc.arg('device_id') AND command_type='SetContainerControl'
 AND status IN ('PENDING','PROCESSING')
)::boolean;

-- name: ContainerOwnsCommandBatch :one
SELECT EXISTS (
 SELECT 1 FROM command_batch_log b JOIN queue_message m ON m.command_batch_log_uuid=b.uuid
 WHERE b.uuid=sqlc.arg('batch_identifier') AND b.organization_id=sqlc.arg('org_id')
 AND b.type='SetContainerControl' AND b.devices_count=1 AND m.device_id=sqlc.arg('device_id')
)::boolean;
