DROP VIEW command_backlog_summary;
DROP INDEX idx_queue_message_monitoring_pending;
ALTER TABLE queue_message DROP COLUMN processing_started_at;
