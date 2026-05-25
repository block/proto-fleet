-- Revert to the unscoped partial unique indexes. NOTE: if the up
-- migration was in effect long enough for two events to share an
-- idempotency_key (one terminal, one non-terminal) — which the up
-- migration explicitly allows — this CREATE will fail until the
-- duplicates are resolved. Production rollback is forward-only in
-- practice; this DOWN exists for local dev cycling.
DROP INDEX CONCURRENTLY IF EXISTS uq_curtailment_event_idempotency;
CREATE UNIQUE INDEX CONCURRENTLY uq_curtailment_event_idempotency
    ON curtailment_event (org_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL;

DROP INDEX CONCURRENTLY IF EXISTS uq_curtailment_event_external_ref;
CREATE UNIQUE INDEX CONCURRENTLY uq_curtailment_event_external_ref
    ON curtailment_event (org_id, external_source, external_reference)
    WHERE external_source IS NOT NULL
      AND external_reference IS NOT NULL;
