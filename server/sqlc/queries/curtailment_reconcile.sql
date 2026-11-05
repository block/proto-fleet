-- name: GetCurtailmentDispatchRejection :one
SELECT dispatch_rejection_reason
FROM curtailment_event
WHERE id = sqlc.arg('event_id') AND org_id = sqlc.arg('org_id');

-- name: RejectCurtailmentDispatch :execrows
-- Latch before attempting restoration, so failover cannot resume dispatch.
UPDATE curtailment_event
SET dispatch_rejection_reason = COALESCE(dispatch_rejection_reason, sqlc.arg('reason')::TEXT)
WHERE id = sqlc.arg('event_id') AND org_id = sqlc.arg('org_id')
  AND state IN ('pending', 'active');

-- name: BulkObserveCurtailmentTargets :many
-- One parent lock preserves Stop/confirmation ordering for the whole page.
WITH locked_event AS MATERIALIZED (
    SELECT id FROM curtailment_event
    WHERE id = sqlc.arg('event_id') AND org_id = sqlc.arg('org_id')
      AND state = sqlc.arg('expected_event_state')::TEXT
      AND state IN ('pending', 'active')
    FOR UPDATE
)
UPDATE curtailment_target t
SET state = o.state, curtail_state = o.state,
    observed_power_w = COALESCE(o.observed_power_w, t.observed_power_w),
    observed_at = o.observed_at,
    confirmed_at = COALESCE(o.confirmed_at, t.confirmed_at)
FROM locked_event e
JOIN jsonb_to_recordset(sqlc.arg('observations')::JSONB) AS o(
    device_identifier TEXT, state TEXT, observed_power_w NUMERIC, expected_batch_uuid TEXT,
    observed_at TIMESTAMPTZ, confirmed_at TIMESTAMPTZ
) ON TRUE
WHERE t.curtailment_event_id = e.id
  AND t.device_identifier = o.device_identifier
  AND t.desired_state = 'curtailed' AND t.state = 'confirmed'
  AND (o.expected_batch_uuid IS NULL OR t.curtail_batch_uuid = o.expected_batch_uuid)
RETURNING t.device_identifier;

-- name: FailCurtailmentPendingDeadlineTargets :execrows
-- Keep ambiguous in-flight intents nonterminal until BeginRestore resets them:
-- enqueueing may have succeeded before its dispatched timestamp was persisted.
-- When confirmed miners keep the event curtailed, in-flight intents are left
-- untouched so the active phase can still confirm them.
WITH locked_event AS MATERIALIZED (
 SELECT id FROM curtailment_event
 WHERE id = sqlc.arg('event_id') AND org_id = sqlc.arg('org_id') AND state = 'pending'
 FOR UPDATE
)
UPDATE curtailment_target t
SET curtail_state = 'restore_failed',
    curtail_completed_at = CURRENT_TIMESTAMP,
    curtail_last_error = sqlc.arg('reason'),
    last_error = sqlc.arg('reason'),
    state = CASE WHEN t.state IN ('dispatching','dispatched') THEN t.state ELSE 'restore_failed' END
FROM locked_event e
WHERE t.curtailment_event_id = e.id AND t.desired_state = 'curtailed'
 AND (t.state IN ('pending','unavailable','drifted')
      OR (sqlc.arg('include_in_flight')::BOOLEAN AND t.state IN ('dispatching','dispatched')));
