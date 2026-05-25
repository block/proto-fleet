-- Scope the curtailment idempotency / external-reference partial unique
-- indexes to non-terminal events only. Before this migration, a webhook
-- delivery that reused a long-completed event's idempotency_key (or
-- external_source+reference pair) returned that historical row from the
-- replay lookup with no new dispatch fired — the operator believed
-- curtailment was in flight, but the original event ended at some prior
-- time. Tightening the index frees the key once the event reaches a
-- terminal state, so subsequent calls treat the key as fresh.
--
-- Webhook retries during an event's in-flight lifetime still hit the
-- replay path (the partial index still covers pending/active/restoring
-- rows). Retries AFTER completion fire a fresh Start.
--
-- golang-migrate v4 postgres driver runs each statement directly with no
-- implicit transaction wrapping, so CONCURRENTLY works here. Each DROP/
-- CREATE pair has a brief window where the constraint is absent; the
-- companion one-non-terminal-per-org index continues to block duplicate
-- concurrent in-flight inserts during that window.
DROP INDEX CONCURRENTLY IF EXISTS uq_curtailment_event_idempotency;
CREATE UNIQUE INDEX CONCURRENTLY uq_curtailment_event_idempotency
    ON curtailment_event (org_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL
      AND state IN ('pending', 'active', 'restoring');

DROP INDEX CONCURRENTLY IF EXISTS uq_curtailment_event_external_ref;
CREATE UNIQUE INDEX CONCURRENTLY uq_curtailment_event_external_ref
    ON curtailment_event (org_id, external_source, external_reference)
    WHERE external_source IS NOT NULL
      AND external_reference IS NOT NULL
      AND state IN ('pending', 'active', 'restoring');
