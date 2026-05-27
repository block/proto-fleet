CREATE UNIQUE INDEX CONCURRENTLY uq_curtailment_event_external_ref
    ON curtailment_event (org_id, external_source, external_reference)
    WHERE external_source IS NOT NULL
      AND external_reference IS NOT NULL
      AND state IN ('pending', 'active', 'restoring');
