-- Keying through device_id lets the rollup's COUNT(DISTINCT device_id) stream out of a GroupAggregate rather
-- than sort the match set first (100k firing rows: 266ms/101k buffers, now 15ms/1.8k). Bounds: 000136.
CREATE INDEX CONCURRENTLY idx_notification_active_org_rollup
    ON notification_active (organization_id, alert_name, rule_group, device_id)
    INCLUDE (starts_at, received_at)
    WHERE status = 'firing';
