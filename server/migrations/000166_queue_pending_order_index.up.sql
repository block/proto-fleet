CREATE INDEX CONCURRENTLY idx_queue_message_pending_created_id ON queue_message(created_at, id) WHERE status = 'PENDING';
