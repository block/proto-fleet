-- CONCURRENTLY must be the sole statement and cannot run in a transaction (golang-migrate runs it directly).
CREATE INDEX CONCURRENTLY idx_notification_history_org_id
    ON notification_history (organization_id, id DESC);
