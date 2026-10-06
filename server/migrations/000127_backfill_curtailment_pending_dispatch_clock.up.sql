-- Backfill only events whose live curtail dispatcher uses interval pacing.
-- The event-id lookup avoids aggregating target history for terminal,
-- restoring, or unpaced events.
WITH eligible_live_event_dispatches AS MATERIALIZED (
    SELECT
        ce.id AS curtailment_event_id,
        (
            SELECT MAX(COALESCE(ct.curtail_dispatched_at, ct.last_dispatched_at))
            FROM curtailment_target AS ct
            WHERE ct.curtailment_event_id = ce.id
              AND ct.desired_state = 'curtailed'
        ) AS dispatched_at
    FROM curtailment_event AS ce
    WHERE ce.state IN ('pending', 'active')
      AND ce.curtail_batch_size IS NOT NULL
      AND ce.curtail_batch_interval_sec > 0
      AND ce.last_curtail_pending_dispatch_at IS NULL
)
UPDATE curtailment_event AS ce
SET last_curtail_pending_dispatch_at = eligible.dispatched_at
FROM eligible_live_event_dispatches AS eligible
WHERE ce.id = eligible.curtailment_event_id
  AND eligible.dispatched_at IS NOT NULL
  AND ce.last_curtail_pending_dispatch_at IS NULL;
