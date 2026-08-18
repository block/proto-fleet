-- name: CaptureFirmwareRolloutEvidence :many
INSERT INTO firmware_rollout_evidence (
    rollout_id,
    member_id,
    org_id,
    phase,
    window_start,
    window_end,
    observed_at,
    avg_hashrate_hs,
    avg_power_w,
    avg_temperature_c,
    error_count,
    sample_count
)
SELECT member.rollout_id,
       member.id,
       member.org_id,
       sqlc.arg('phase'),
       sqlc.arg('window_start'),
       sqlc.arg('window_end'),
       CASE
           WHEN MAX(metrics.time) >= sqlc.arg('fresh_after')
               THEN MAX(metrics.time)
           ELSE NULL
       END,
       CASE
           WHEN MAX(metrics.time) >= sqlc.arg('fresh_after')
               THEN AVG(metrics.hash_rate_hs)::float8
           ELSE NULL
       END,
       CASE
           WHEN MAX(metrics.time) >= sqlc.arg('fresh_after')
               THEN AVG(metrics.power_w)::float8
           ELSE NULL
       END,
       CASE
           WHEN MAX(metrics.time) >= sqlc.arg('fresh_after')
               THEN AVG(metrics.temp_c)::float8
           ELSE NULL
       END,
       CASE
           WHEN MAX(metrics.time) >= sqlc.arg('fresh_after')
               THEN COUNT(DISTINCT errors.id)::bigint
           ELSE NULL
       END,
       CASE
           WHEN MAX(metrics.time) >= sqlc.arg('fresh_after')
               THEN COUNT(DISTINCT metrics.time)::bigint
           ELSE NULL
       END
FROM firmware_rollout_member member
JOIN device
  ON device.id = member.device_id
 AND device.org_id = member.org_id
LEFT JOIN device_metrics metrics
  ON metrics.device_identifier = device.device_identifier
 AND metrics.time >= sqlc.arg('window_start')
 AND metrics.time <= sqlc.arg('window_end')
LEFT JOIN errors
  ON errors.device_id = member.device_id
 AND errors.org_id = member.org_id
 AND errors.last_seen_at >= sqlc.arg('window_start')
 AND errors.first_seen_at <= sqlc.arg('window_end')
WHERE member.rollout_id = sqlc.arg('rollout_id')
  AND member.org_id = sqlc.arg('org_id')
GROUP BY member.rollout_id, member.id, member.org_id, member.position
ON CONFLICT (member_id, phase) DO UPDATE
SET window_start = EXCLUDED.window_start,
    window_end = EXCLUDED.window_end,
    observed_at = EXCLUDED.observed_at,
    avg_hashrate_hs = EXCLUDED.avg_hashrate_hs,
    avg_power_w = EXCLUDED.avg_power_w,
    avg_temperature_c = EXCLUDED.avg_temperature_c,
    error_count = EXCLUDED.error_count,
    sample_count = EXCLUDED.sample_count
RETURNING *;

-- name: ListFirmwareRolloutEvidence :many
SELECT *
FROM firmware_rollout_evidence
WHERE rollout_id = sqlc.arg('rollout_id')
  AND org_id = sqlc.arg('org_id')
ORDER BY member_id, phase;
