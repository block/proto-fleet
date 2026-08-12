-- Seeks a group's newest firing instance for the rollup; that lookup always knows the rule group, so rule_group
-- precedes the history_id ordering. Bounds: 000136. CONCURRENTLY: a blocking build would stall alert ingest.
CREATE INDEX CONCURRENTLY idx_notification_active_org_alert
    ON notification_active (organization_id, alert_name, rule_group, history_id DESC)
    WHERE status = 'firing';
