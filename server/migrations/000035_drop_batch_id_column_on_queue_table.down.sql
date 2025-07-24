ALTER TABLE queue_message
    ADD COLUMN command_batch_log_id BIGINT NOT NULL,
    ADD INDEX idx_command_batch_log_id (command_batch_log_id),
    ADD CONSTRAINT queue_message_ibfk_1 FOREIGN KEY (command_batch_log_id) REFERENCES command_batch_log(id),
    DROP INDEX idx_command_batch_log_uuid,
    DROP COLUMN command_batch_log_uuid;

-- manual mapping would be required here for the command_batch_log_id to work properly
