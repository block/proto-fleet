DROP TABLE IF EXISTS fleet_metric_rollup_progress;
SELECT remove_retention_policy('fleet_metric_rollup_90s', if_exists => TRUE);
DROP TABLE IF EXISTS fleet_metric_rollup_90s;
