DROP INDEX IF EXISTS uq_activity_log_batch_completed;
DROP INDEX IF EXISTS idx_activity_log_batch_id;

ALTER TABLE activity_log
    DROP COLUMN IF EXISTS batch_id;
