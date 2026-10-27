-- Source time orders fans independently of inventory and control reads.
-- name: UpsertContainerFans :exec
INSERT INTO container_fan_observation (device_id, component_id, observed_at, speed_percent, power_w, direction, alarm_active)
SELECT d.id, x.component_id, x.observed_at, x.speed_percent, x.power_w, x.direction, x.alarm_active
FROM device d JOIN discovered_device dd ON dd.id = d.discovered_device_id
CROSS JOIN jsonb_to_recordset(sqlc.arg('fans')::jsonb) AS x(component_id text, observed_at timestamptz, speed_percent double precision, power_w double precision, direction text, alarm_active boolean)
WHERE d.device_identifier = sqlc.arg('device_identifier') AND d.deleted_at IS NULL AND dd.deleted_at IS NULL AND dd.device_class = 2
ON CONFLICT (device_id, component_id) DO UPDATE
SET observed_at = EXCLUDED.observed_at, speed_percent = EXCLUDED.speed_percent, power_w = EXCLUDED.power_w,
    direction = EXCLUDED.direction, alarm_active = EXCLUDED.alarm_active
WHERE container_fan_observation.observed_at < EXCLUDED.observed_at;

-- The parent upsert serializes replacement of its controls and setpoints.
-- name: AdvanceContainerControls :one
INSERT INTO container_controls_observation (device_id, observed_at, supported, main_circuit_breaker_supported, main_circuit_breaker_closed, alarm_reset_supported, alarm_mute_supported, dry_cooler_fan_reverse_supported)
SELECT d.id, x.observed_at, x.supported, x.main_circuit_breaker_supported, x.main_circuit_breaker_closed, x.alarm_reset_supported, x.alarm_mute_supported, x.dry_cooler_fan_reverse_supported
FROM device d JOIN discovered_device dd ON dd.id = d.discovered_device_id
CROSS JOIN jsonb_to_record(sqlc.arg('observation')::jsonb) AS x(observed_at timestamptz, supported boolean, main_circuit_breaker_supported boolean, main_circuit_breaker_closed boolean, alarm_reset_supported boolean, alarm_mute_supported boolean, dry_cooler_fan_reverse_supported boolean)
WHERE d.device_identifier = sqlc.arg('device_identifier') AND d.deleted_at IS NULL AND dd.deleted_at IS NULL AND dd.device_class = 2
ON CONFLICT (device_id) DO UPDATE
SET observed_at = EXCLUDED.observed_at, supported = EXCLUDED.supported,
    main_circuit_breaker_supported = EXCLUDED.main_circuit_breaker_supported, main_circuit_breaker_closed = EXCLUDED.main_circuit_breaker_closed,
    alarm_reset_supported = EXCLUDED.alarm_reset_supported, alarm_mute_supported = EXCLUDED.alarm_mute_supported,
    dry_cooler_fan_reverse_supported = EXCLUDED.dry_cooler_fan_reverse_supported
WHERE container_controls_observation.observed_at < EXCLUDED.observed_at
RETURNING device_id;

-- name: DeleteContainerControls :exec
DELETE FROM container_control_observation WHERE device_id = $1;
-- name: DeleteContainerSetpoints :exec
DELETE FROM container_setpoint_observation WHERE device_id = $1;
-- name: InsertContainerControls :exec
INSERT INTO container_control_observation (device_id, name, enabled)
SELECT sqlc.arg('device_id'), x.name, x.enabled
FROM jsonb_to_recordset(sqlc.arg('controls')::jsonb) AS x(name text, enabled boolean);
-- name: InsertContainerSetpoints :exec
INSERT INTO container_setpoint_observation (device_id, name, value, unit)
SELECT sqlc.arg('device_id'), x.name, x.value, x.unit
FROM jsonb_to_recordset(sqlc.arg('setpoints')::jsonb) AS x(name text, value double precision, unit text);

-- name: UpsertContainerHA :exec
INSERT INTO container_ha_observation (device_id, observed_at, degraded)
SELECT d.id, sqlc.arg(observed_at)::timestamptz, sqlc.arg(degraded)::boolean
FROM device d JOIN discovered_device dd ON dd.id=d.discovered_device_id AND dd.org_id=d.org_id
WHERE d.device_identifier=sqlc.arg(device_identifier) AND d.deleted_at IS NULL
AND dd.deleted_at IS NULL AND dd.device_class=2
ON CONFLICT (device_id) DO UPDATE SET observed_at=EXCLUDED.observed_at, degraded=EXCLUDED.degraded
WHERE container_ha_observation.observed_at < EXCLUDED.observed_at;
