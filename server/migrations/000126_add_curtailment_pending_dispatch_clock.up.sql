ALTER TABLE curtailment_event
    ADD COLUMN last_curtail_pending_dispatch_at TIMESTAMPTZ NULL;
