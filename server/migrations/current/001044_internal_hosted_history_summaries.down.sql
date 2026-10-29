-- requires-shared: 1000
DROP TRIGGER record_hosted_account_history ON device_metrics;
DROP TRIGGER record_hosted_account_history ON miner_state_snapshots;
DROP TRIGGER record_hosted_account_history ON container_module_metrics;
DROP TRIGGER record_hosted_account_history ON container_module_diagnostic_observation;
DROP FUNCTION record_hosted_account_history_sample();
DROP FUNCTION hosted_history_metric_coverage(jsonb);
DROP FUNCTION hosted_module_uptime_state(text);
DROP FUNCTION hosted_account_history_watermarks(timestamptz);
DROP FUNCTION hosted_history_authorized_intervals(jsonb,bigint,boolean,bigint[],jsonb);
DROP MATERIALIZED VIEW hosted_account_history_daily;
DROP MATERIALIZED VIEW hosted_account_history_hourly;
DROP TABLE hosted_account_history_sample;
DROP FUNCTION hosted_history_metric_code(text);
DROP INDEX idx_hosted_account_asset_interval_capture_miner;
DROP INDEX idx_hosted_account_asset_interval_capture_module;
DROP TABLE hosted_account_history_summary_epoch;
