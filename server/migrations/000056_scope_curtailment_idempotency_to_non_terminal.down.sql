-- Revert to the unscoped partial unique index. NOTE: if the up migration was
-- in effect long enough for two events to share an idempotency_key, this
-- CREATE will fail until duplicates are resolved.
CREATE UNIQUE INDEX CONCURRENTLY uq_curtailment_event_idempotency
    ON curtailment_event (org_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL;
