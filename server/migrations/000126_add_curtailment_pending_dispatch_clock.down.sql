ALTER TABLE curtailment_event
    DROP COLUMN IF EXISTS last_curtail_pending_dispatch_at;
