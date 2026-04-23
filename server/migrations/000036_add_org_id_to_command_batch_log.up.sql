-- Dedicated organization_id on command_batch_log so GetCommandBatchDeviceResults
-- can filter directly on the batch's owning org. Historical rows that predate
-- this migration stay NULL and are intentionally invisible to the RPC
-- (closed-by-default; we never guess which org an old row belongs to).
--
-- CREATE INDEX without CONCURRENTLY holds ACCESS EXCLUSIVE briefly; run
-- during a low-traffic window on large tables.

ALTER TABLE command_batch_log
    ADD COLUMN organization_id BIGINT NULL;

CREATE INDEX idx_command_batch_log_organization_id
    ON command_batch_log(organization_id)
    WHERE organization_id IS NOT NULL;

ALTER TABLE command_batch_log
    ADD CONSTRAINT fk_command_batch_log_org
    FOREIGN KEY (organization_id)
    REFERENCES organization(id)
    ON DELETE RESTRICT;
