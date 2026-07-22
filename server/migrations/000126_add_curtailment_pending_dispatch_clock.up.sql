ALTER TABLE curtailment_event
    ADD COLUMN last_curtail_pending_dispatch_at TIMESTAMPTZ NULL;

UPDATE curtailment_event AS ce
SET last_curtail_pending_dispatch_at = latest.dispatched_at
FROM (
    SELECT
        curtailment_event_id,
        MAX(COALESCE(curtail_dispatched_at, last_dispatched_at)) AS dispatched_at
    FROM curtailment_target
    WHERE COALESCE(curtail_dispatched_at, last_dispatched_at) IS NOT NULL
      AND desired_state = 'curtailed'
    GROUP BY curtailment_event_id
) AS latest
WHERE ce.id = latest.curtailment_event_id
  AND ce.state IN ('pending', 'active')
  AND ce.curtail_batch_size IS NOT NULL
  AND ce.curtail_batch_interval_sec > 0;
