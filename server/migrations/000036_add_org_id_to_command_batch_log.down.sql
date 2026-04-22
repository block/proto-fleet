ALTER TABLE command_batch_log
    DROP CONSTRAINT IF EXISTS fk_command_batch_log_org;

DROP INDEX IF EXISTS idx_command_batch_log_organization_id;

ALTER TABLE command_batch_log
    DROP COLUMN IF EXISTS organization_id;
