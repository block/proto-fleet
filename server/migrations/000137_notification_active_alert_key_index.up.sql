-- Seeks one rule's firing rows in the drill-in's page order; the drill-in always names both alert_name and
-- rule_group, so alert_key trails them. Bounds: 000136. CONCURRENTLY: a blocking build would stall alert ingest.
CREATE INDEX CONCURRENTLY idx_notification_active_org_alert_key
    ON notification_active (organization_id, alert_name, rule_group, alert_key)
    WHERE status = 'firing';
