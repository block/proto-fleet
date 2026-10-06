-- Keeps activity-history pagination on unresolved rows after a large recovery batch. The key order matches
-- the organization filter and descending id keyset scan in ListNotificationHistory.
-- CONCURRENTLY avoids blocking alert ingestion and must remain the sole statement in this migration.
CREATE INDEX CONCURRENTLY idx_notification_history_org_unresolved_id
    ON notification_history (organization_id, id DESC)
    WHERE status <> 'resolved';
