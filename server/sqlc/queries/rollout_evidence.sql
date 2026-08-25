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
WHERE firmware_rollout_evidence.status = 'open'
  AND (
      firmware_rollout_evidence.observed_at,
      firmware_rollout_evidence.avg_hashrate_hs,
      firmware_rollout_evidence.avg_power_w,
      firmware_rollout_evidence.avg_temperature_c,
      firmware_rollout_evidence.error_count,
      firmware_rollout_evidence.sample_count
  ) IS DISTINCT FROM (
      EXCLUDED.observed_at,
      EXCLUDED.avg_hashrate_hs,
      EXCLUDED.avg_power_w,
      EXCLUDED.avg_temperature_c,
      EXCLUDED.error_count,
      EXCLUDED.sample_count
  )
RETURNING *;

-- name: ListFirmwareRolloutEvidence :many
SELECT *
FROM firmware_rollout_evidence
WHERE rollout_id = sqlc.arg('rollout_id')
  AND org_id = sqlc.arg('org_id')
ORDER BY member_id, phase;

-- name: ListFirmwareRolloutEvidenceCandidates :many
WITH candidate_rows AS (
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
           auto_control.resulting_revision AS auto_control_resulting_revision,
           (
               SELECT COUNT(*)::bigint
               FROM firmware_rollout_member member
               WHERE member.rollout_id = batch.rollout_id
                 AND member.batch_id = batch.id
                 AND member.org_id = batch.org_id
           ) AS member_count
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
)
SELECT batch_id,
       rollout_id,
       org_id,
       completed_at,
       policy_enabled,
       max_drop_basis_points,
       healthy_duration_seconds,
       rollout_state,
       rollout_revision,
       rollout_created_by_user_id,
       is_current_review_batch,
       has_pending_batch,
       evidence_status,
       healthy_since,
       last_policy_bucket_boundary,
       latest_policy_bucket_hashrate_hs,
       latest_policy_bucket_delta_basis_points,
       evaluated_at,
       evidence_error_message,
       auto_control_status,
       auto_control_expected_revision,
       auto_control_resulting_revision,
       member_count
FROM candidate_rows
ORDER BY evaluated_at ASC NULLS FIRST, completed_at, batch_id
LIMIT sqlc.arg('limit_count');

-- name: EnsureFirmwareRolloutEvidenceAccumulators :execrows
INSERT INTO firmware_rollout_evidence_accumulator (
    rollout_id,
    batch_id,
    member_id,
    org_id,
    processed_through
)
SELECT member.rollout_id,
       member.batch_id,
       member.id,
       member.org_id,
       sqlc.arg('window_start')
FROM firmware_rollout_member member
WHERE member.rollout_id = sqlc.arg('rollout_id')
  AND member.batch_id = sqlc.arg('batch_id')
  AND member.org_id = sqlc.arg('org_id')
ON CONFLICT (member_id) DO NOTHING;

-- name: AdvanceFirmwareRolloutEvidenceAccumulators :execrows
WITH locked AS (
    SELECT accumulator.member_id,
           accumulator.processed_through
    FROM firmware_rollout_evidence_accumulator accumulator
    WHERE accumulator.rollout_id = sqlc.arg('rollout_id')
      AND accumulator.batch_id = sqlc.arg('batch_id')
      AND accumulator.org_id = sqlc.arg('org_id')
      AND accumulator.processed_through < sqlc.arg('stable_cutoff')
    ORDER BY accumulator.member_id
    FOR UPDATE
),
deltas AS (
    SELECT locked.member_id,
           MAX(metrics.time) FILTER (WHERE metrics.hash_rate_hs IS NOT NULL) AS observed_at,
           COALESCE(SUM(metrics.hash_rate_hs), 0)::float8 AS hashrate_sum,
           COALESCE(SUM(metrics.power_w), 0)::float8 AS power_sum,
           COUNT(metrics.power_w)::bigint AS power_sample_count,
           COALESCE(SUM(metrics.temp_c), 0)::float8 AS temperature_sum,
           COUNT(metrics.temp_c)::bigint AS temperature_sample_count,
           COUNT(metrics.hash_rate_hs)::bigint AS sample_count
    FROM locked
    JOIN firmware_rollout_member member
      ON member.id = locked.member_id
     AND member.rollout_id = sqlc.arg('rollout_id')
     AND member.batch_id = sqlc.arg('batch_id')
     AND member.org_id = sqlc.arg('org_id')
    JOIN device
      ON device.id = member.device_id
     AND device.org_id = member.org_id
    LEFT JOIN device_metrics metrics
      ON metrics.device_identifier = device.device_identifier
     AND metrics.time >= locked.processed_through
     AND metrics.time < sqlc.arg('stable_cutoff')
    GROUP BY locked.member_id
)
UPDATE firmware_rollout_evidence_accumulator accumulator
SET processed_through = sqlc.arg('stable_cutoff'),
    observed_at = GREATEST(accumulator.observed_at, deltas.observed_at),
    hashrate_sum = accumulator.hashrate_sum + deltas.hashrate_sum,
    power_sum = accumulator.power_sum + deltas.power_sum,
    power_sample_count = accumulator.power_sample_count + deltas.power_sample_count,
    temperature_sum = accumulator.temperature_sum + deltas.temperature_sum,
    temperature_sample_count =
        accumulator.temperature_sample_count + deltas.temperature_sample_count,
    sample_count = accumulator.sample_count + deltas.sample_count
FROM deltas
WHERE accumulator.member_id = deltas.member_id;

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
SELECT accumulator.rollout_id,
       accumulator.member_id,
       accumulator.org_id,
       'post',
       sqlc.arg('window_start'),
       sqlc.arg('window_end'),
       GREATEST(
           accumulator.observed_at,
           MAX(metrics.time) FILTER (WHERE metrics.hash_rate_hs IS NOT NULL)
       ),
       CASE
           WHEN accumulator.sample_count + COUNT(metrics.hash_rate_hs) > 0
               THEN (
                   accumulator.hashrate_sum + COALESCE(SUM(metrics.hash_rate_hs), 0)
               ) / (accumulator.sample_count + COUNT(metrics.hash_rate_hs))
           ELSE NULL
       END,
       CASE
           WHEN accumulator.power_sample_count + COUNT(metrics.power_w) > 0
               THEN (
                   accumulator.power_sum + COALESCE(SUM(metrics.power_w), 0)
               ) / (accumulator.power_sample_count + COUNT(metrics.power_w))
           ELSE NULL
       END,
       CASE
           WHEN accumulator.temperature_sample_count + COUNT(metrics.temp_c) > 0
               THEN (
                   accumulator.temperature_sum + COALESCE(SUM(metrics.temp_c), 0)
               ) / (accumulator.temperature_sample_count + COUNT(metrics.temp_c))
           ELSE NULL
       END,
       CASE
           WHEN accumulator.sample_count + COUNT(metrics.hash_rate_hs) > 0 THEN (
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
           WHEN accumulator.sample_count + COUNT(metrics.hash_rate_hs) > 0
               THEN accumulator.sample_count + COUNT(metrics.hash_rate_hs)
           ELSE NULL
       END
FROM firmware_rollout_evidence_accumulator accumulator
JOIN firmware_rollout_member member
  ON member.id = accumulator.member_id
 AND member.rollout_id = accumulator.rollout_id
 AND member.batch_id = accumulator.batch_id
 AND member.org_id = accumulator.org_id
JOIN device
  ON device.id = member.device_id
 AND device.org_id = member.org_id
LEFT JOIN device_metrics metrics
  ON metrics.device_identifier = device.device_identifier
 AND metrics.time >= accumulator.processed_through
 AND metrics.time <= sqlc.arg('window_end')
WHERE accumulator.rollout_id = sqlc.arg('rollout_id')
  AND accumulator.batch_id = sqlc.arg('batch_id')
  AND accumulator.org_id = sqlc.arg('org_id')
GROUP BY accumulator.rollout_id,
         accumulator.member_id,
         accumulator.org_id,
         accumulator.processed_through,
         accumulator.observed_at,
         accumulator.hashrate_sum,
         accumulator.power_sum,
         accumulator.power_sample_count,
         accumulator.temperature_sum,
         accumulator.temperature_sample_count,
         accumulator.sample_count,
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
    sample_count = EXCLUDED.sample_count
WHERE firmware_rollout_evidence.status = 'open'
  AND (
      firmware_rollout_evidence.observed_at,
      firmware_rollout_evidence.avg_hashrate_hs,
      firmware_rollout_evidence.avg_power_w,
      firmware_rollout_evidence.avg_temperature_c,
      firmware_rollout_evidence.error_count,
      firmware_rollout_evidence.sample_count
  ) IS DISTINCT FROM (
      EXCLUDED.observed_at,
      EXCLUDED.avg_hashrate_hs,
      EXCLUDED.avg_power_w,
      EXCLUDED.avg_temperature_c,
      EXCLUDED.error_count,
      EXCLUDED.sample_count
  );

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

-- name: GetFirmwareRolloutBatchHashrateEvidenceSummary :one
WITH member_evidence AS (
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
)
SELECT COUNT(*)::bigint AS total_count,
       COUNT(*) FILTER (
           WHERE baseline_hashrate_hs > 0
             AND baseline_hashrate_hs NOT IN ('NaN'::float8, 'Infinity'::float8, '-Infinity'::float8)
             AND post_hashrate_hs >= 0
             AND post_hashrate_hs NOT IN ('NaN'::float8, 'Infinity'::float8, '-Infinity'::float8)
       )::bigint AS paired_count,
       COALESCE(BOOL_AND(
           baseline_hashrate_hs > 0
           AND baseline_hashrate_hs NOT IN ('NaN'::float8, 'Infinity'::float8, '-Infinity'::float8)
       ), FALSE)::boolean AS baseline_available,
       COALESCE(BOOL_AND(
           post_hashrate_hs >= 0
           AND post_hashrate_hs NOT IN ('NaN'::float8, 'Infinity'::float8, '-Infinity'::float8)
       ), FALSE)::boolean AS post_available,
       COALESCE(AVG(baseline_hashrate_hs) FILTER (
           WHERE baseline_hashrate_hs > 0
             AND baseline_hashrate_hs NOT IN ('NaN'::float8, 'Infinity'::float8, '-Infinity'::float8)
             AND post_hashrate_hs >= 0
             AND post_hashrate_hs NOT IN ('NaN'::float8, 'Infinity'::float8, '-Infinity'::float8)
       ), 0)::float8 AS baseline_average,
       COALESCE(AVG(post_hashrate_hs) FILTER (
           WHERE baseline_hashrate_hs > 0
             AND baseline_hashrate_hs NOT IN ('NaN'::float8, 'Infinity'::float8, '-Infinity'::float8)
             AND post_hashrate_hs >= 0
             AND post_hashrate_hs NOT IN ('NaN'::float8, 'Infinity'::float8, '-Infinity'::float8)
       ), 0)::float8 AS post_average,
       COALESCE(MIN(post_observed_at), 'epoch'::timestamptz)::timestamptz
           AS oldest_post_observed_at
FROM member_evidence;

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
SELECT member_buckets.bucket_index,
       COUNT(*)::bigint AS member_count,
       AVG(baseline.avg_hashrate_hs)::float8 AS baseline_average,
       AVG(member_buckets.avg_hashrate_hs)::float8 AS current_average,
       MIN(member_buckets.observed_at)::timestamptz AS oldest_observed_at
FROM member_buckets
JOIN complete_buckets
  ON complete_buckets.bucket_index = member_buckets.bucket_index
JOIN firmware_rollout_evidence baseline
  ON baseline.member_id = member_buckets.member_id
 AND baseline.rollout_id = sqlc.arg('rollout_id')
 AND baseline.org_id = sqlc.arg('org_id')
 AND baseline.phase = 'baseline'
 AND baseline.avg_hashrate_hs > 0
GROUP BY member_buckets.bucket_index
HAVING COUNT(*) = (SELECT COUNT(*) FROM frozen_members)
ORDER BY member_buckets.bucket_index;

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

-- name: CompleteFirmwareRolloutEvidenceRows :execrows
UPDATE firmware_rollout_evidence evidence
SET status = 'completed'
FROM firmware_rollout_member member
WHERE member.id = evidence.member_id
  AND member.rollout_id = evidence.rollout_id
  AND member.org_id = evidence.org_id
  AND member.batch_id = sqlc.arg('batch_id')
  AND evidence.rollout_id = sqlc.arg('rollout_id')
  AND evidence.org_id = sqlc.arg('org_id')
  AND evidence.status = 'open';

-- name: CancelFirmwareRolloutEvidence :one
WITH cancelled_batches AS (
    UPDATE firmware_rollout_batch batch
    SET evidence_status = 'cancelled',
        evidence_cancellation_reason = sqlc.arg('cancellation_reason'),
        evidence_cancelled_at = sqlc.arg('cancelled_at'),
        evidence_error_message = sqlc.arg('cancellation_reason'),
        healthy_since = NULL,
        evaluated_at = sqlc.arg('cancelled_at'),
        post_window_finalized = TRUE,
        post_window_finalized_at = sqlc.arg('cancelled_at')
    WHERE batch.rollout_id = sqlc.arg('rollout_id')
      AND batch.org_id = sqlc.arg('org_id')
      AND NOT batch.post_window_finalized
    RETURNING batch.id
),
cancelled_rows AS (
    UPDATE firmware_rollout_evidence evidence
    SET status = 'cancelled',
        cancellation_reason = sqlc.arg('cancellation_reason'),
        cancelled_at = sqlc.arg('cancelled_at')
    FROM firmware_rollout_member member
    WHERE member.id = evidence.member_id
      AND member.rollout_id = evidence.rollout_id
      AND member.org_id = evidence.org_id
      AND evidence.rollout_id = sqlc.arg('rollout_id')
      AND evidence.org_id = sqlc.arg('org_id')
      AND evidence.status = 'open'
    RETURNING evidence.id
),
disabled_controls AS (
    UPDATE firmware_rollout_control control
    SET status = 'failed',
        error_message = sqlc.arg('cancellation_reason')
    WHERE control.rollout_id = sqlc.arg('rollout_id')
      AND control.org_id = sqlc.arg('org_id')
      AND control.operation = 'continue'
      AND control.idempotency_key LIKE 'rollout-evidence-auto-continue-batch-%'
      AND control.status = 'started'
    RETURNING control.id
)
SELECT COUNT(*)::bigint
FROM cancelled_batches;

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
