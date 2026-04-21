-- Proto Fleet PostgreSQL Retention Policies
-- Automated data lifecycle management for telemetry data

-- =====================================================
-- Retention policy for raw device metrics
-- Keep raw telemetry data for 30 days
-- =====================================================
SELECT add_retention_policy('device_metrics', INTERVAL '30 days');

-- =====================================================
-- Retention policy for hourly aggregates
-- Keep hourly aggregates for 3 months (90 days)
-- =====================================================
SELECT add_retention_policy('device_metrics_hourly', INTERVAL '3 months');

-- =====================================================
-- Retention policy for daily aggregates
-- Keep daily aggregates for 3 years
-- =====================================================
SELECT add_retention_policy('device_metrics_daily', INTERVAL '3 years');
