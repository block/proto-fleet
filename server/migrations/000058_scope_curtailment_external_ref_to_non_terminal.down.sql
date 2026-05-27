-- Revert to the unscoped partial unique index. NOTE: if the up migration was
-- in effect long enough for two events to share an external reference, this
-- CREATE will fail until duplicates are resolved.
CREATE UNIQUE INDEX CONCURRENTLY uq_curtailment_event_external_ref
    ON curtailment_event (org_id, external_source, external_reference)
    WHERE external_source IS NOT NULL
      AND external_reference IS NOT NULL;
