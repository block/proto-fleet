-- Serializes reconciliation for each controller. A delayed response cannot
-- change placement, retire inventory, or overwrite a newer status observation.
-- name: AdvanceContainerInventory :one
INSERT INTO container_inventory (device_id, observed_at)
SELECT d.id, sqlc.arg('observed_at')::timestamptz
FROM device d JOIN discovered_device dd ON dd.id = d.discovered_device_id
WHERE d.device_identifier = sqlc.arg('device_identifier') AND d.deleted_at IS NULL
  AND dd.deleted_at IS NULL AND dd.device_class = 2
ON CONFLICT (device_id) DO UPDATE SET observed_at = EXCLUDED.observed_at
WHERE container_inventory.observed_at < EXCLUDED.observed_at
RETURNING device_id;

-- name: ReconcileContainerTanks :exec
INSERT INTO container_tank (device_id, position, first_seen_at, last_seen_at)
SELECT sqlc.arg('device_id'), x.position, sqlc.arg('observed_at'), sqlc.arg('observed_at')
FROM jsonb_to_recordset(sqlc.arg('tanks')::jsonb) AS x(position int)
ON CONFLICT (device_id, position) DO UPDATE
SET present = TRUE, last_seen_at = EXCLUDED.last_seen_at;

-- A full map may retire missing slots. A known identity change also retires
-- the old hardware so its history is never attached to the replacement.
-- name: RetireContainerModules :exec
UPDATE container_module m SET retired_at = sqlc.arg('observed_at')
FROM container_tank t
WHERE m.tank_id = t.id AND t.device_id = sqlc.arg('device_id') AND m.retired_at IS NULL
AND (
  (sqlc.arg('complete')::boolean AND NOT EXISTS (
    SELECT 1 FROM jsonb_to_recordset(sqlc.arg('modules')::jsonb) AS x(tank_position int, position int, reported_id text)
    WHERE x.tank_position = t.position AND x.position = m.position
  )) OR EXISTS (
    SELECT 1 FROM jsonb_to_recordset(sqlc.arg('modules')::jsonb) AS x(tank_position int, position int, reported_id text)
    WHERE x.tank_position = t.position AND x.position = m.position
      AND m.reported_id <> '' AND COALESCE(x.reported_id, '') <> '' AND m.reported_id <> x.reported_id
  )
);

-- name: MarkAbsentContainerTanks :exec
UPDATE container_tank t SET present = FALSE
WHERE t.device_id = sqlc.arg('device_id') AND NOT EXISTS (
  SELECT 1 FROM jsonb_to_recordset(sqlc.arg('tanks')::jsonb) AS x(position int) WHERE x.position = t.position
);

-- name: ReconcileContainerModules :exec
INSERT INTO container_module (tank_id, position, reported_id, first_seen_at, last_seen_at, state, state_observed_at, issues, issues_observed_at, issues_source_times)
SELECT t.id, x.position, COALESCE(x.reported_id, ''), sqlc.arg('observed_at'), sqlc.arg('observed_at'), x.state, x.observed_at, x.issues, x.issues_observed_at, COALESCE(x.issues->'source_times', '{}'::jsonb)
FROM jsonb_to_recordset(sqlc.arg('modules')::jsonb) AS x(tank_position int, position int, reported_id text, state text, observed_at timestamptz, issues jsonb, issues_observed_at timestamptz)
JOIN container_tank t ON t.device_id = sqlc.arg('device_id') AND t.position = x.tank_position
ON CONFLICT (tank_id, position) WHERE retired_at IS NULL DO UPDATE
SET reported_id = CASE WHEN EXCLUDED.reported_id = '' THEN container_module.reported_id ELSE EXCLUDED.reported_id END,
    last_seen_at = EXCLUDED.last_seen_at,
    state = CASE WHEN EXCLUDED.state_observed_at IS NULL OR container_module.state_observed_at IS NULL OR EXCLUDED.state_observed_at >= container_module.state_observed_at THEN EXCLUDED.state ELSE container_module.state END,
    state_observed_at = GREATEST(container_module.state_observed_at, EXCLUDED.state_observed_at),
    -- Rejected payloads must not consume source timestamps needed by a corrected report.
    (issues, issues_observed_at, issues_source_times) = (
      SELECT CASE
        -- Missing evidence immediately withdraws confirmation; keep its watermark.
        WHEN EXCLUDED.issues_observed_at IS NULL THEN NULL
        WHEN evidence.accepted THEN EXCLUDED.issues
        WHEN NOT COALESCE((EXCLUDED.issues->>'complete')::boolean, FALSE)
          AND EXCLUDED.issues IS DISTINCT FROM container_module.issues THEN NULL
        ELSE container_module.issues END,
      CASE WHEN evidence.accepted
        THEN GREATEST(container_module.issues_observed_at, EXCLUDED.issues_observed_at)
        ELSE container_module.issues_observed_at END,
      CASE WHEN evidence.accepted THEN COALESCE((
        SELECT jsonb_object_agg(COALESCE(previous.key, incoming.key),
          GREATEST(previous.value::timestamptz, incoming.value::timestamptz))
        FROM jsonb_each_text(container_module.issues_source_times) AS previous(key, value)
        FULL JOIN jsonb_each_text(EXCLUDED.issues_source_times) AS incoming(key, value) USING (key)
      ), '{}'::jsonb) ELSE container_module.issues_source_times END
      FROM (SELECT EXCLUDED.issues_observed_at IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM jsonb_each_text(EXCLUDED.issues_source_times) incoming
        JOIN jsonb_each_text(container_module.issues_source_times) previous USING (key)
        WHERE incoming.value::timestamptz < previous.value::timestamptz
      ) AND (
        container_module.issues_observed_at IS NULL OR
        EXCLUDED.issues_observed_at > container_module.issues_observed_at OR
        EXISTS (
          SELECT 1 FROM jsonb_each_text(EXCLUDED.issues_source_times) incoming
          LEFT JOIN jsonb_each_text(container_module.issues_source_times) previous USING (key)
          WHERE previous.value IS NULL OR incoming.value::timestamptz > previous.value::timestamptz
        )
      ) AS accepted) evidence
    );


-- Samples resolve only to current matching hardware. Late samples for replaced
-- hardware cannot contaminate its replacement. Retried observations are no-ops.
-- Collection time disambiguates aggregates sharing an oldest-source timestamp.
-- A reconciled poll may explicitly clear unavailable metrics without identity
-- evidence. Never use that exception for measurements or delayed inventory.
-- name: InsertContainerModuleMetrics :exec
WITH inserted AS (
  INSERT INTO container_module_metrics (module_id, time, collected_at, hashrate_hs, power_w, temperature_c, efficiency_jh, coverage,site_id,topology_version_id)
  SELECT m.id, x.time, sqlc.arg('collected_at')::timestamptz, x.hashrate_hs, x.power_w, x.temperature_c, x.efficiency_jh, x.coverage,d.site_id,o.version_id
  FROM jsonb_to_recordset(sqlc.arg('samples')::jsonb) AS x(tank_position int, position int, reported_id text, time timestamptz, hashrate_hs double precision, power_w double precision, temperature_c double precision, efficiency_jh double precision, coverage jsonb)
  JOIN device d ON d.device_identifier = sqlc.arg('device_identifier') AND d.deleted_at IS NULL
  JOIN container_tank t ON t.device_id = d.id AND t.position = x.tank_position AND t.present
  JOIN container_module m ON m.tank_id = t.id AND m.position = x.position AND m.retired_at IS NULL
    AND (m.reported_id = COALESCE(x.reported_id, '') OR (
      sqlc.arg('inventory_accepted')::boolean AND COALESCE(x.reported_id, '') = ''
      AND m.last_seen_at = sqlc.arg('collected_at')::timestamptz
      AND x.hashrate_hs IS NULL AND x.power_w IS NULL AND x.temperature_c IS NULL
    ))
  LEFT JOIN container_topology_observation o ON o.device_id=d.id AND o.collected_at=sqlc.arg('collected_at')::timestamptz
  ON CONFLICT (module_id, time, collected_at) DO NOTHING
  RETURNING *
)
INSERT INTO container_module_latest_metrics (module_id, time, collected_at, hashrate_hs, power_w, temperature_c, efficiency_jh, coverage, source_times)
SELECT module_id, time, collected_at, hashrate_hs, power_w, temperature_c, efficiency_jh, coverage, COALESCE(NULLIF(coverage->'source_times', 'null'::jsonb),'{}'::jsonb) FROM inserted
ON CONFLICT (module_id) DO UPDATE
SET time = EXCLUDED.time, collected_at = EXCLUDED.collected_at, hashrate_hs = EXCLUDED.hashrate_hs,
    power_w = EXCLUDED.power_w, temperature_c = EXCLUDED.temperature_c, efficiency_jh=EXCLUDED.efficiency_jh, coverage=EXCLUDED.coverage,
 source_times=(SELECT COALESCE(jsonb_object_agg(COALESCE(p.key,n.key),GREATEST(p.value::timestamptz,n.value::timestamptz)),'{}'::jsonb)
 FROM jsonb_each_text(container_module_latest_metrics.source_times) p FULL JOIN jsonb_each_text(EXCLUDED.source_times) n USING(key))
WHERE (container_module_latest_metrics.time, container_module_latest_metrics.collected_at) < (EXCLUDED.time, EXCLUDED.collected_at);
