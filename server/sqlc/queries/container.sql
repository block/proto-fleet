-- name: UpsertContainerSnapshot :exec
INSERT INTO container_snapshot (device_id, observed_at, snapshot)
SELECT d.id, sqlc.arg('observed_at')::timestamptz, sqlc.arg('snapshot')::jsonb
FROM device d
JOIN discovered_device dd ON dd.id = d.discovered_device_id AND dd.org_id = d.org_id
WHERE d.device_identifier = sqlc.arg('device_identifier')
  AND d.deleted_at IS NULL AND dd.device_class = 2
ON CONFLICT (device_id) DO UPDATE
SET observed_at = EXCLUDED.observed_at, snapshot = EXCLUDED.snapshot
WHERE container_snapshot.observed_at < EXCLUDED.observed_at;

-- name: ListContainers :many
SELECT d.device_identifier, d.site_id, d.serial_number,
       dd.ip_address, dd.port, dd.url_scheme, dd.is_active,
       COALESCE((dd.is_active AND dp.pairing_status IN ('PAIRED', 'DEFAULT_PASSWORD')
        AND dd.driver_name = 'proto'
        AND NOT EXISTS (SELECT 1 FROM fleet_node_device fnd WHERE fnd.device_id=d.id AND fnd.org_id=d.org_id)), FALSE)::boolean AS embedded_web_view_available,
       COALESCE(d.custom_name, '')::text AS name,
       COALESCE(s.name, '')::text AS site_name,
       dp.pairing_status,
       cs.observed_at, cs.snapshot,
       COALESCE(dm.time, 'epoch'::timestamptz)::timestamptz AS metrics_at, dm.hash_rate_hs, dm.power_w, dm.temp_c
FROM device d
JOIN discovered_device dd ON dd.id = d.discovered_device_id AND dd.org_id = d.org_id
JOIN device_pairing dp ON dp.device_id = d.id
LEFT JOIN site s ON s.id = d.site_id AND s.org_id = d.org_id AND s.deleted_at IS NULL
LEFT JOIN container_snapshot cs ON cs.device_id = d.id
LEFT JOIN LATERAL (
    SELECT time, hash_rate_hs, power_w, temp_c
    FROM device_metrics
    WHERE device_identifier = d.device_identifier
    ORDER BY time DESC LIMIT 1
) dm ON TRUE
WHERE d.org_id = sqlc.arg('org_id') AND d.deleted_at IS NULL
  AND dd.deleted_at IS NULL AND dd.device_class = 2
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
