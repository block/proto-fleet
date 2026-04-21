CREATE INDEX idx_queue_message_reaper
    ON queue_message (updated_at)
    WHERE status = 'PROCESSING';
