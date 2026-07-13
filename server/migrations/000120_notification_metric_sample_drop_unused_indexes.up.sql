-- Drop the two near-unused indexes on notification_metric_sample.
--
-- Production pg_stat_user_indexes (5000 miners, per daily chunk):
--   notification_metric_sample_time_idx            ~4.4 GB  ~81k scans
--   idx_..._metric_time                            ~12 GB   ~23k scans
--   idx_..._metric_org_device_time                 ~22 GB      ~18 scans
--   idx_..._metric_org_result_time (partial)       ~1.8 GB       0 scans
--
-- The provisioned alert rules filter on metric + time and only GROUP BY
-- organization_id/device_id, so the planner picks (metric, time DESC) and
-- the wide composite index goes almost entirely unused while costing more
-- write amplification than every other index combined. The partial result
-- index has no reader at all. The rare per-device lookups the composite
-- served still resolve through (metric, time DESC) at acceptable cost, and
-- chunks older than the compression horizon use the compressed
-- segmentby (metric, organization_id) layout instead.
-- Bound the ACCESS EXCLUSIVE wait (parent + every chunk): without it a
-- long-running Explore query blocks the drop while every rule query and
-- INSERT queues behind the pending lock — a fleet-wide alerting stall at
-- startup. The whole file runs as one implicit transaction, so SET LOCAL
-- scopes to it. On timeout golang-migrate leaves the version dirty; the
-- drops are idempotent, so `migrate force 119` + rerun recovers cleanly.
SET LOCAL lock_timeout = '1min';

DROP INDEX IF EXISTS idx_notification_metric_sample_metric_org_device_time;
DROP INDEX IF EXISTS idx_notification_metric_sample_metric_org_result_time;
