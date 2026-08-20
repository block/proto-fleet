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

-- name: ListFirmwareRolloutEvidenceCandidates :many
SELECT batch.id AS batch_id,
       batch.rollout_id,
       batch.org_id,
       batch.completed_at,
       (rollout.hashrate_policy_max_drop_basis_points IS NOT NULL)::boolean AS policy_enabled,
       COALESCE(rollout.hashrate_policy_max_drop_basis_points, 0)::int AS max_drop_basis_points,
       COALESCE(rollout.hashrate_policy_healthy_duration_seconds, 0)::int AS healthy_duration_seconds,
       rollout.state AS rollout_state,
       rollout.revision AS rollout_revision,
       rollout.created_by_user_id AS rollout_created_by_user_id,
       NOT EXISTS (
           SELECT 1
           FROM firmware_rollout_batch later_completed
           WHERE later_completed.rollout_id = batch.rollout_id
             AND later_completed.org_id = batch.org_id
             AND later_completed.state = 'completed'
             AND later_completed.position > batch.position
       ) AS is_current_review_batch,
       EXISTS (
           SELECT 1
           FROM firmware_rollout_batch pending
           WHERE pending.rollout_id = batch.rollout_id
             AND pending.org_id = batch.org_id
             AND pending.state = 'pending'
             AND pending.position > batch.position
       ) AS has_pending_batch,
       batch.evidence_status,
       batch.healthy_since,
       batch.last_policy_bucket_boundary,
       batch.latest_policy_bucket_hashrate_hs,
       batch.latest_policy_bucket_delta_basis_points,
       batch.evaluated_at,
       batch.evidence_error_message,
       auto_control.status AS auto_control_status,
       auto_control.expected_revision AS auto_control_expected_revision,
       auto_control.resulting_revision AS auto_control_resulting_revision
FROM firmware_rollout_batch batch
JOIN firmware_rollout rollout
  ON rollout.id = batch.rollout_id
 AND rollout.org_id = batch.org_id
LEFT JOIN firmware_rollout_control auto_control
  ON auto_control.rollout_id = batch.rollout_id
 AND auto_control.org_id = batch.org_id
 AND auto_control.operation = 'continue'
 AND auto_control.idempotency_key =
     CONCAT('rollout-evidence-auto-continue-batch-', batch.id)
WHERE batch.state = 'completed'
  AND batch.completed_at IS NOT NULL
  AND NOT batch.post_window_finalized
  AND batch.evidence_status <> 'finalized'
  AND rollout.state IN (
      'running',
      'paused',
      'review',
      'completed',
      'completed_with_failures'
  )
ORDER BY batch.evaluated_at ASC NULLS FIRST, batch.completed_at, batch.id
LIMIT sqlc.arg('limit_count');

-- name: CaptureFirmwareRolloutBatchPostEvidence :execrows
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
       'post',
       sqlc.arg('window_start'),
       sqlc.arg('window_end'),
       MAX(metrics.time) FILTER (WHERE metrics.hash_rate_hs IS NOT NULL),
       AVG(metrics.hash_rate_hs)::float8,
       CASE
           WHEN COUNT(metrics.hash_rate_hs) > 0
               THEN AVG(metrics.power_w)::float8
           ELSE NULL
       END,
       CASE
           WHEN COUNT(metrics.hash_rate_hs) > 0
               THEN AVG(metrics.temp_c)::float8
           ELSE NULL
       END,
       CASE
           WHEN COUNT(metrics.hash_rate_hs) > 0 THEN (
               SELECT COUNT(*)::bigint
               FROM errors error_row
               WHERE error_row.device_id = member.device_id
                 AND error_row.org_id = member.org_id
                 AND error_row.last_seen_at >= sqlc.arg('window_start')
                 AND error_row.first_seen_at <= sqlc.arg('window_end')
           )
           ELSE NULL
       END,
       CASE
           WHEN COUNT(metrics.hash_rate_hs) > 0
               THEN COUNT(metrics.hash_rate_hs)::bigint
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
WHERE member.rollout_id = sqlc.arg('rollout_id')
  AND member.batch_id = sqlc.arg('batch_id')
  AND member.org_id = sqlc.arg('org_id')
GROUP BY member.rollout_id,
         member.id,
         member.org_id,
         member.device_id,
         member.position
ON CONFLICT (member_id, phase) DO UPDATE
SET window_start = EXCLUDED.window_start,
    window_end = EXCLUDED.window_end,
    observed_at = EXCLUDED.observed_at,
    avg_hashrate_hs = EXCLUDED.avg_hashrate_hs,
    avg_power_w = EXCLUDED.avg_power_w,
    avg_temperature_c = EXCLUDED.avg_temperature_c,
    error_count = EXCLUDED.error_count,
    sample_count = EXCLUDED.sample_count;

-- name: ListFirmwareRolloutBatchHashrateEvidence :many
SELECT member.id AS member_id,
       baseline.avg_hashrate_hs AS baseline_hashrate_hs,
       post.avg_hashrate_hs AS post_hashrate_hs,
       post.observed_at AS post_observed_at
FROM firmware_rollout_member member
LEFT JOIN firmware_rollout_evidence baseline
  ON baseline.member_id = member.id
 AND baseline.rollout_id = member.rollout_id
 AND baseline.org_id = member.org_id
 AND baseline.phase = 'baseline'
LEFT JOIN firmware_rollout_evidence post
  ON post.member_id = member.id
 AND post.rollout_id = member.rollout_id
 AND post.org_id = member.org_id
 AND post.phase = 'post'
WHERE member.rollout_id = sqlc.arg('rollout_id')
  AND member.batch_id = sqlc.arg('batch_id')
  AND member.org_id = sqlc.arg('org_id')
ORDER BY member.position, member.id;

-- name: ListCompleteFirmwareRolloutPolicyBuckets :many
WITH frozen_members AS (
    SELECT member.id AS member_id,
           device.device_identifier
    FROM firmware_rollout_member member
    JOIN device
      ON device.id = member.device_id
     AND device.org_id = member.org_id
    WHERE member.rollout_id = sqlc.arg('rollout_id')
      AND member.batch_id = sqlc.arg('batch_id')
      AND member.org_id = sqlc.arg('org_id')
),
member_buckets AS (
    SELECT frozen.member_id,
           FLOOR(
               EXTRACT(EPOCH FROM (metrics.time - sqlc.arg('window_start'))) / 10
           )::bigint AS bucket_index,
           AVG(metrics.hash_rate_hs)::float8 AS avg_hashrate_hs,
           MAX(metrics.time)::timestamptz AS observed_at
    FROM frozen_members frozen
    JOIN device_metrics metrics
      ON metrics.device_identifier = frozen.device_identifier
     AND metrics.time >= GREATEST(
         sqlc.arg('window_start')::timestamptz,
         sqlc.arg('bucket_after')::timestamptz
     )
     AND metrics.time < sqlc.arg('bucket_cutoff')
     AND metrics.hash_rate_hs IS NOT NULL
    GROUP BY frozen.member_id, bucket_index
),
complete_buckets AS (
    SELECT member_buckets.bucket_index
    FROM member_buckets
    GROUP BY member_buckets.bucket_index
    HAVING COUNT(*) = (SELECT COUNT(*) FROM frozen_members)
       AND sqlc.arg('window_start')::timestamptz
           + (member_buckets.bucket_index + 1) * INTERVAL '10 seconds'
           > sqlc.arg('bucket_after')::timestamptz
)
SELECT member_buckets.member_id,
       member_buckets.avg_hashrate_hs,
       member_buckets.observed_at,
       member_buckets.bucket_index
FROM member_buckets
JOIN complete_buckets
  ON complete_buckets.bucket_index = member_buckets.bucket_index
ORDER BY member_buckets.bucket_index, member_buckets.member_id;

-- name: UpdateFirmwareRolloutBatchEvidenceSummary :execrows
UPDATE firmware_rollout_batch
SET evidence_status = CASE
        WHEN evidence_status = 'automation_error'
            THEN evidence_status
        ELSE sqlc.arg('evidence_status')::text
    END,
    evidence_total_count = sqlc.arg('evidence_total_count'),
    evidence_paired_count = sqlc.arg('evidence_paired_count'),
    cumulative_baseline_hashrate_hs = sqlc.narg('cumulative_baseline_hashrate_hs'),
    cumulative_current_hashrate_hs = sqlc.narg('cumulative_current_hashrate_hs'),
    cumulative_delta_basis_points = sqlc.narg('cumulative_delta_basis_points'),
    latest_policy_bucket_hashrate_hs = COALESCE(
        sqlc.narg('latest_policy_bucket_hashrate_hs'),
        latest_policy_bucket_hashrate_hs
    ),
    latest_policy_bucket_delta_basis_points = COALESCE(
        sqlc.narg('latest_policy_bucket_delta_basis_points'),
        latest_policy_bucket_delta_basis_points
    ),
    healthy_since = CASE
        WHEN sqlc.arg('evidence_status')::text = 'stale' THEN NULL
        ELSE sqlc.narg('healthy_since')::timestamptz
    END,
    last_policy_bucket_boundary = COALESCE(
        sqlc.narg('last_policy_bucket_boundary'),
        last_policy_bucket_boundary
    ),
    evaluated_at = sqlc.arg('evaluated_at'),
    evidence_error_message = sqlc.narg('evidence_error_message'),
    post_window_finalized = sqlc.arg('post_window_finalized'),
    post_window_finalized_at = sqlc.narg('post_window_finalized_at')
WHERE id = sqlc.arg('batch_id')
  AND rollout_id = sqlc.arg('rollout_id')
  AND org_id = sqlc.arg('org_id')
  AND NOT post_window_finalized
  AND evaluated_at IS NOT DISTINCT FROM
      sqlc.narg('expected_evaluated_at')::timestamptz
  AND last_policy_bucket_boundary IS NOT DISTINCT FROM
      sqlc.narg('expected_last_policy_bucket_boundary')::timestamptz;

-- name: MarkFirmwareRolloutBatchAutomationError :execrows
UPDATE firmware_rollout_batch
SET evidence_status = 'automation_error',
    evidence_error_message = sqlc.arg('evidence_error_message'),
    healthy_since = NULL,
    evaluated_at = sqlc.arg('evaluated_at')
WHERE id = sqlc.arg('batch_id')
  AND rollout_id = sqlc.arg('rollout_id')
  AND org_id = sqlc.arg('org_id')
  AND evidence_status <> 'automation_error';
