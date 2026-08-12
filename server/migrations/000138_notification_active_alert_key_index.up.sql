-- Seeks one alert's firing rows in the drill-in's page order; alert_key precedes rule_group so the order still
-- holds when the caller omits rule_group. On the label bounds and CONCURRENTLY, see 000136 and 000137.
CREATE INDEX CONCURRENTLY idx_notification_active_org_alert_key
    ON notification_active (organization_id, alert_name, alert_key, rule_group)
    WHERE status = 'firing';
