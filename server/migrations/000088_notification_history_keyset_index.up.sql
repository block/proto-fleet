-- Index matching the ListNotifications keyset pattern (org_id, id DESC); existing indexes are on received_at and can't serve it.

CREATE INDEX idx_notification_history_org_id
    ON notification_history (organization_id, id DESC);
