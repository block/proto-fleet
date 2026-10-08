-- requires-shared: 1000

-- The current runner commits each private migration with its checkpoint.
-- Build this index in that transaction so a failed upgrade rolls back fully.
CREATE INDEX idx_queue_message_pending_created_id ON queue_message(created_at, id) WHERE status = 'PENDING';
