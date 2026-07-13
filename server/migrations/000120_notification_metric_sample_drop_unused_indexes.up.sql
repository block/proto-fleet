-- Rules filter on metric + time only, so these two indexes see ~18 and 0
-- scans/day against ~24 GB/day of write amplification (stats in PR #739).

-- Bound the ACCESS EXCLUSIVE wait so a long query can't stall alerting at
-- startup; on timeout, `migrate force 119` + rerun recovers (drops idempotent).
SET LOCAL lock_timeout = '1min';

DROP INDEX IF EXISTS idx_notification_metric_sample_metric_org_device_time;
DROP INDEX IF EXISTS idx_notification_metric_sample_metric_org_result_time;
