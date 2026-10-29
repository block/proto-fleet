-- Keep the current pending phase's age separate from the lifetime safety cap.
ALTER TABLE curtailment_event ADD COLUMN pending_started_at TIMESTAMPTZ;

-- Older rows do not retain restore-reversal timestamps. Creation is the best
-- available bound for their current phase; future reversals stamp it explicitly.
-- The backfill is not an event change, so it must not advance updated_at.
ALTER TABLE curtailment_event DISABLE TRIGGER update_curtailment_event_updated_at;
UPDATE curtailment_event SET pending_started_at = created_at;
ALTER TABLE curtailment_event ENABLE TRIGGER update_curtailment_event_updated_at;

ALTER TABLE curtailment_event
    ALTER COLUMN pending_started_at SET DEFAULT CURRENT_TIMESTAMP,
    ALTER COLUMN pending_started_at SET NOT NULL;
