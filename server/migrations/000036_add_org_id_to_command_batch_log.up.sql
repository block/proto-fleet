-- Dedicated organization_id on command_batch_log so GetCommandBatchDeviceResults
-- can filter directly on the batch's owning org instead of joining through
-- user_organization (which leaks across orgs for multi-org creators).
--
-- The column is left nullable so the migration is safe to run without a
-- coordinated code deployment. New writers populate it from session context;
-- a follow-up migration can flip it to NOT NULL after a soak period.
--
-- OPERATIONAL NOTE: the backfill UPDATE and the non-CONCURRENTLY CREATE INDEX
-- each hold locks proportional to command_batch_log's size. Run during a
-- low-traffic window for large tables.

ALTER TABLE command_batch_log
    ADD COLUMN organization_id BIGINT NULL;

-- Backfill only when the creator's org is unambiguous (exactly one live
-- user_organization row). Guessing for multi-org creators could silently
-- mis-attribute history to the wrong tenant, so those rows stay NULL and
-- are invisible to GetBatchHeaderForOrg (closed-by-default). Operators can
-- repair ambiguous rows manually once the correct org is known.
UPDATE command_batch_log cbl
SET organization_id = (
    SELECT uo.organization_id
    FROM user_organization uo
    WHERE uo.user_id = cbl.created_by
      AND uo.deleted_at IS NULL
    LIMIT 1
)
WHERE cbl.organization_id IS NULL
  AND (
    SELECT COUNT(*)
    FROM user_organization uo
    WHERE uo.user_id = cbl.created_by
      AND uo.deleted_at IS NULL
  ) = 1;

CREATE INDEX idx_command_batch_log_organization_id
    ON command_batch_log(organization_id)
    WHERE organization_id IS NOT NULL;

ALTER TABLE command_batch_log
    ADD CONSTRAINT fk_command_batch_log_org
    FOREIGN KEY (organization_id)
    REFERENCES organization(id)
    ON DELETE RESTRICT;
