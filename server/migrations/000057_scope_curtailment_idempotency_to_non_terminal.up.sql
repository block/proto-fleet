CREATE UNIQUE INDEX CONCURRENTLY uq_curtailment_event_idempotency
    ON curtailment_event (org_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL
      AND state IN ('pending', 'active', 'restoring');
