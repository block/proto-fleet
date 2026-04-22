-- Add a dedicated organization_id column to command_batch_log so
-- GetCommandBatchDeviceResults (and any future org-scoped query) can filter
-- directly on the batch's owning organization, instead of joining through
-- user_organization.
--
-- The prior authorization check (JOIN user_organization ON user_id = created_by)
-- leaks across organizations when the creator belongs to multiple orgs: any
-- org the creator was a member of could see the batch's per-miner detail. A
-- dedicated column captured from session.OrganizationID at batch creation
-- closes that gap.
--
-- The column is left nullable here so the migration is safe to run without
-- coordinated code deployment. All new writers populate it from session
-- context. A follow-up migration can flip it to NOT NULL after a soak period.

ALTER TABLE command_batch_log
    ADD COLUMN organization_id BIGINT NULL;

-- Backfill existing rows from the creator's earliest user_organization
-- membership. Single-org creators (the common case) get the unambiguous
-- answer; multi-org creators get a deterministic pick rather than an
-- arbitrary one. Rows whose creator has no live membership stay NULL and
-- are invisible to the RPC, which is the correct closed-by-default posture.
UPDATE command_batch_log cbl
SET organization_id = (
    SELECT uo.organization_id
    FROM user_organization uo
    WHERE uo.user_id = cbl.created_by
      AND uo.deleted_at IS NULL
    ORDER BY uo.id
    LIMIT 1
)
WHERE cbl.organization_id IS NULL;

CREATE INDEX idx_command_batch_log_organization_id
    ON command_batch_log(organization_id)
    WHERE organization_id IS NOT NULL;

ALTER TABLE command_batch_log
    ADD CONSTRAINT fk_command_batch_log_org
    FOREIGN KEY (organization_id)
    REFERENCES organization(id)
    ON DELETE RESTRICT;
