-- Applications are stopped; ordinary index creation keeps reconciliation atomic.
-- GetMessagesToProcess selects the oldest pending messages globally. The
-- existing (device_id, status, created_at) index serves its per-device blocker
-- lookup, but cannot provide the outer query's created_at ordering.
CREATE INDEX idx_queue_message_pending_created
    ON queue_message (created_at)
    WHERE status = 'PENDING';
