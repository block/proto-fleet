-- Keep schema expansion separate from 000127's data backfill so the
-- ACCESS EXCLUSIVE lock is released before target rows are scanned.
ALTER TABLE curtailment_event
    ADD COLUMN last_curtail_pending_dispatch_at TIMESTAMPTZ NULL;
