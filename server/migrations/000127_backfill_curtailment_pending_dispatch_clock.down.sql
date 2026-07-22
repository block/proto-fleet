-- No-op: the pacing clock may have advanced after this backfill ran, so its
-- contribution cannot be removed safely without provenance. Migration 000126
-- drops the column when the schema change itself is rolled back.
SELECT 1;
