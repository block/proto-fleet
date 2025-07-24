ALTER TABLE queue_message
DROP FOREIGN KEY queue_message_ibfk_1,
DROP INDEX idx_command_batch_log_id,
DROP COLUMN command_batch_log_id,
ADD COLUMN command_batch_log_uuid VARCHAR(36) NOT NULL,
ADD INDEX idx_command_batch_log_uuid (command_batch_log_uuid);
