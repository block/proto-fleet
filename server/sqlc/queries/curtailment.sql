-- name: ListCurtailmentPreviewDevices :many
WITH scoped_devices AS (
    SELECT DISTINCT
        d.id AS device_id,
        d.device_identifier,
        COALESCE(dd.manufacturer, '') AS manufacturer,
        COALESCE(dd.model, '') AS model,
        COALESCE(dd.firmware_version, '') AS firmware_version,
        dd.driver_name,
        dp.pairing_status::text AS pairing_status,
        COALESCE(ds.status::text, '')::text AS device_status
    FROM device d
    JOIN discovered_device dd ON dd.id = d.discovered_device_id
    JOIN device_pairing dp ON dp.device_id = d.id
    LEFT JOIN device_status ds ON ds.device_id = d.id
    WHERE d.org_id = sqlc.arg('org_id')
      AND d.deleted_at IS NULL
      AND dd.deleted_at IS NULL
      AND (
          sqlc.arg('scope_type')::text = 'whole_org'
          OR (
              sqlc.arg('scope_type')::text = 'device_sets'
              AND EXISTS (
                  SELECT 1
                  FROM device_set_membership dsm
                  JOIN device_set device_set_scope ON device_set_scope.id = dsm.device_set_id
                  WHERE dsm.org_id = d.org_id
                    AND device_set_scope.org_id = d.org_id
                    AND device_set_scope.deleted_at IS NULL
                    AND dsm.device_id = d.id
                    AND dsm.device_set_id = ANY(sqlc.arg('device_set_ids')::bigint[])
              )
          )
          OR (
              sqlc.arg('scope_type')::text = 'device_list'
              AND d.device_identifier = ANY(sqlc.arg('device_identifiers')::text[])
          )
      )
)
SELECT
    sd.device_id,
    sd.device_identifier,
    sd.manufacturer,
    sd.model,
    sd.firmware_version,
    sd.driver_name,
    sd.pairing_status,
    sd.device_status,
    (latest_metrics.time IS NOT NULL)::boolean AS has_latest_metric,
    COALESCE(latest_metrics.time, 'epoch'::timestamptz) AS latest_metric_at,
    (latest_metrics.power_w IS NOT NULL)::boolean AS has_current_power_w,
    COALESCE(latest_metrics.power_w, 0)::float8 AS current_power_w,
    (recent_metrics.avg_power_w IS NOT NULL)::boolean AS has_recent_power_w,
    COALESCE(recent_metrics.avg_power_w, 0)::float8 AS recent_power_w,
    (recent_metrics.avg_hash_rate_hs IS NOT NULL)::boolean AS has_recent_hash_rate_hs,
    COALESCE(recent_metrics.avg_hash_rate_hs, 0)::float8 AS recent_hash_rate_hs,
    (latest_efficiency.avg_efficiency IS NOT NULL)::boolean AS has_efficiency_jh,
    COALESCE(latest_efficiency.avg_efficiency, 0)::float8 AS efficiency_jh,
    EXISTS (
        SELECT 1
        FROM curtailment_target ct
        JOIN curtailment_event ce ON ce.id = ct.curtailment_event_id
        WHERE ce.org_id = sqlc.arg('org_id')
          AND ct.device_identifier = sd.device_identifier
          AND ce.state IN ('pending', 'active', 'restoring')
          AND ct.state NOT IN ('resolved', 'restore_failed', 'released')
    ) AS in_active_curtailment,
    EXISTS (
        SELECT 1
        FROM curtailment_target ct
        JOIN curtailment_event ce ON ce.id = ct.curtailment_event_id
        WHERE ce.org_id = sqlc.arg('org_id')
          AND ct.device_identifier = sd.device_identifier
          AND ct.state IN ('resolved', 'restore_failed')
          AND COALESCE(ct.released_at, ce.ended_at, ct.confirmed_at, ct.added_at) >= sqlc.arg('cooldown_since')::timestamptz
    ) AS in_cooldown
FROM scoped_devices sd
LEFT JOIN LATERAL (
    SELECT dm.time, dm.power_w
    FROM device_metrics dm
    WHERE dm.device_identifier = sd.device_identifier
      AND dm.time >= NOW() - INTERVAL '15 minutes'
    ORDER BY dm.time DESC
    LIMIT 1
) latest_metrics ON TRUE
LEFT JOIN LATERAL (
    SELECT
        AVG(dm.power_w) AS avg_power_w,
        AVG(dm.hash_rate_hs) AS avg_hash_rate_hs
    FROM device_metrics dm
    WHERE dm.device_identifier = sd.device_identifier
      AND dm.time >= NOW() - INTERVAL '5 minutes'
) recent_metrics ON TRUE
LEFT JOIN LATERAL (
    SELECT dmh.avg_efficiency
    FROM device_metrics_hourly dmh
    WHERE dmh.device_identifier = sd.device_identifier
      AND dmh.bucket < date_trunc('hour', NOW())
    ORDER BY dmh.bucket DESC
    LIMIT 1
) latest_efficiency ON TRUE
ORDER BY sd.device_identifier;
