-- The notification-history read path (ListNotifications) keyset-paginates
-- by id: WHERE organization_id = $1 AND id < $2 ORDER BY id DESC. The
-- existing indexes are on received_at, so that query can't use one and
-- Postgres filters/scans rows per page — and the dashboard fetches this
-- for every notification:read user. Add an index matching the keyset
-- access pattern so each page is an index range scan.

CREATE INDEX idx_notification_history_org_id
    ON notification_history (organization_id, id DESC);
