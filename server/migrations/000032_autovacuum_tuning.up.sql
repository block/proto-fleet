-- Tighten autovacuum thresholds for tables with high dead-tuple churn.
-- device_status is upserted on every telemetry cycle; errors are frequently
-- opened and closed. The default 20% scale factor causes bloat to accumulate
-- between vacuum runs on both tables.

ALTER TABLE device_status SET (
    autovacuum_vacuum_scale_factor = 0.02,
    autovacuum_analyze_scale_factor = 0.01
);

ALTER TABLE errors SET (
    autovacuum_vacuum_scale_factor = 0.05,
    autovacuum_analyze_scale_factor = 0.02
);
