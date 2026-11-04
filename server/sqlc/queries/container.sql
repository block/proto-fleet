-- name: GetContainerIdentifiersForCommands :many
-- Class metadata excludes known containers without live capability discovery.
SELECT d.device_identifier
FROM device d
JOIN discovered_device dd ON dd.id = d.discovered_device_id AND dd.org_id = d.org_id
WHERE d.org_id = sqlc.arg('org_id') AND d.deleted_at IS NULL
  AND dd.deleted_at IS NULL AND dd.device_class = 2
  AND d.device_identifier = ANY(sqlc.arg('device_identifiers')::text[]);

-- name: ListContainerPlacements :many
-- Membership lists need identity and topology counts, never telemetry.
SELECT d.device_identifier, d.site_id,
       COALESCE(NULLIF(d.custom_name, ''), 'Container ' || COALESCE(d.serial_number, ''))::text AS name,
       COALESCE(s.name, '')::text AS site_name,
       (SELECT count(*) FROM container_tank t WHERE t.device_id = d.id AND t.present)::bigint AS tank_count
FROM device d
JOIN discovered_device dd ON dd.id = d.discovered_device_id AND dd.org_id = d.org_id
JOIN device_pairing dp ON dp.device_id = d.id
LEFT JOIN site s ON s.id = d.site_id AND s.org_id = d.org_id AND s.deleted_at IS NULL
WHERE d.org_id = sqlc.arg('org_id') AND d.deleted_at IS NULL
  AND dd.deleted_at IS NULL AND dd.device_class = 2
ORDER BY d.id;

-- name: ListContainers :many
SELECT d.id, d.device_identifier, d.site_id, d.serial_number,
 ha.observed_at AS ha_observed_at, ha.degraded AS ha_degraded,
       COALESCE(d.custom_name, '')::text AS name,
       COALESCE(s.name, '')::text AS site_name,
       COALESCE(fans.readings, '[]'::jsonb)::jsonb AS fans,
       COALESCE(plant.metrics, 'null'::jsonb)::jsonb AS plant,
       COALESCE(to_jsonb(controls) || jsonb_build_object('controls', COALESCE(control_states.readings, '[]'::jsonb), 'setpoints', COALESCE(setpoints.readings, '[]'::jsonb)), 'null'::jsonb)::jsonb AS controls,
       ci.observed_at, COALESCE(health.tanks, '[]'::jsonb)::jsonb AS tank_health,
       COALESCE(dm.time, 'epoch'::timestamptz)::timestamptz AS metrics_at, dm.hashrate_hs AS hash_rate_hs, dm.power_w, dm.temperature_c AS temp_c, dm.efficiency_jh, dm.min_temperature_c, dm.max_temperature_c, dm.coverage
FROM device d
JOIN discovered_device dd ON dd.id = d.discovered_device_id AND dd.org_id = d.org_id
JOIN device_pairing dp ON dp.device_id = d.id
LEFT JOIN site s ON s.id = d.site_id AND s.org_id = d.org_id AND s.deleted_at IS NULL
LEFT JOIN container_inventory ci ON ci.device_id = d.id
LEFT JOIN container_ha_observation ha ON ha.device_id = d.id
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
               count(*) FILTER (WHERE a.state = 'active') AS hashing_count,
               count(*) FILTER (WHERE a.state IN ('error','needs_mining_pool','updating','reboot_required')) AS attention_count,
               count(*) FILTER (WHERE a.state = 'offline') AS offline_count,
               count(*) FILTER (WHERE a.state IN ('inactive','maintenance')) AS sleeping_count,
               0::bigint AS isolated_count,
               count(*) FILTER (WHERE a.state = 'unknown') AS unknown_count
        FROM container_tank t
        LEFT JOIN fleet_mining_asset a ON a.kind = 6 AND a.group_kind = 4 AND a.group_id = t.id
          AND a.facility_kind = 2 AND a.facility_id = d.id AND a.org_id = d.org_id
        WHERE t.device_id = d.id AND t.present
        GROUP BY t.id, t.position
    ) tank
) health ON TRUE
LEFT JOIN container_equipment_current_metrics dm ON dm.kind=2 AND dm.id=d.id
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
SELECT d.id, d.site_id, dd.is_active, dd.driver_name
FROM device d
JOIN discovered_device dd ON dd.id=d.discovered_device_id AND dd.org_id=d.org_id
JOIN device_pairing dp ON dp.device_id=d.id
WHERE d.device_identifier=sqlc.arg('device_identifier') AND d.org_id=sqlc.arg('org_id')
  AND d.deleted_at IS NULL AND dd.deleted_at IS NULL AND dd.device_class=2
  AND dp.pairing_status IN ('PAIRED','DEFAULT_PASSWORD')
FOR UPDATE OF d, dp;

-- name: HasPendingContainerCommand :one
SELECT EXISTS (
 SELECT 1 FROM queue_message
 WHERE device_id=sqlc.arg('device_id') AND command_type=ANY(sqlc.arg('command_types')::text[])
 AND status IN ('PENDING','PROCESSING')
)::boolean;

-- name: ContainerOwnsCommandBatch :one
SELECT EXISTS (
 SELECT 1 FROM command_batch_log b JOIN queue_message m ON m.command_batch_log_uuid=b.uuid
 WHERE b.uuid=sqlc.arg('batch_identifier') AND b.organization_id=sqlc.arg('org_id')
 AND b.type IN ('SetContainerControl', 'SetTankMining', 'SetModuleMining', 'ContainerMining') AND b.devices_count=1 AND m.device_id=sqlc.arg('device_id')
)::boolean;

-- name: ListTankCommandTargets :many
SELECT t.id AS tank_id, t.device_id, t.position, t.last_seen_at, ci.observed_at,
       d.device_identifier, d.site_id, COALESCE(NULLIF(d.custom_name, ''), NULLIF(d.serial_number, ''), d.device_identifier)::text AS container_name,
       dd.manufacturer, dd.model, dd.driver_name
FROM container_tank t
JOIN container_inventory ci ON ci.device_id=t.device_id
JOIN device d ON d.id=t.device_id
JOIN discovered_device dd ON dd.id=d.discovered_device_id AND dd.org_id=d.org_id
JOIN device_pairing dp ON dp.device_id=d.id
WHERE t.id=ANY(sqlc.arg('tank_ids')::bigint[]) AND t.present
  AND d.org_id=sqlc.arg('org_id') AND d.deleted_at IS NULL AND dd.deleted_at IS NULL
  AND dd.device_class=2 AND dp.pairing_status IN ('PAIRED','DEFAULT_PASSWORD')
ORDER BY d.id, t.id;

-- name: ListModuleCommandTargets :many
SELECT m.id AS module_id, t.device_id, t.position AS tank_position,
       m.position, m.reported_id, m.identity_confirmed,
       m.last_seen_at, ci.observed_at,
       d.device_identifier, d.site_id,
       COALESCE(NULLIF(d.custom_name, ''), NULLIF(d.serial_number, ''), d.device_identifier)::text AS container_name,
       dd.manufacturer, dd.model, dd.driver_name
FROM container_module m
JOIN container_tank t ON t.id=m.tank_id AND t.present
JOIN container_inventory ci ON ci.device_id=t.device_id
JOIN device d ON d.id=t.device_id
JOIN discovered_device dd ON dd.id=d.discovered_device_id AND dd.org_id=d.org_id
JOIN device_pairing dp ON dp.device_id=d.id
WHERE m.id=ANY(sqlc.arg('module_ids')::bigint[]) AND m.retired_at IS NULL
  AND d.org_id=sqlc.arg('org_id') AND d.deleted_at IS NULL AND dd.deleted_at IS NULL
  AND dd.device_class=2 AND dp.pairing_status IN ('PAIRED','DEFAULT_PASSWORD')
ORDER BY d.id, m.id;

-- name: GetContainerMiningTarget :one
SELECT d.id, d.site_id, COALESCE(f.epoch,0)::bigint AS write_epoch
FROM device d
JOIN discovered_device dd ON dd.id=d.discovered_device_id AND dd.org_id=d.org_id
JOIN device_pairing dp ON dp.device_id=d.id
LEFT JOIN device_write_fence f ON f.device_id=d.id AND f.organization_id=d.org_id
WHERE d.device_identifier=sqlc.arg('device_identifier') AND d.org_id=sqlc.arg('org_id')
  AND d.deleted_at IS NULL AND dd.deleted_at IS NULL AND dd.is_active
  AND dd.device_class=2 AND dd.driver_name='proto'
  AND dp.pairing_status IN ('PAIRED','DEFAULT_PASSWORD');
