DROP INDEX IF EXISTS idx_device_metrics_site_time;
ALTER TABLE device_metrics DROP COLUMN IF EXISTS site_id;

DROP INDEX IF EXISTS idx_miner_state_snapshots_org_site_time;
ALTER TABLE miner_state_snapshots DROP COLUMN IF EXISTS site_id;

DROP INDEX IF EXISTS idx_errors_org_site_last_seen;
ALTER TABLE errors
    DROP CONSTRAINT IF EXISTS fk_errors_site,
    DROP COLUMN IF EXISTS site_id;

DROP INDEX IF EXISTS idx_command_on_device_log_site;
ALTER TABLE command_on_device_log
    DROP CONSTRAINT IF EXISTS fk_command_on_device_log_site,
    DROP COLUMN IF EXISTS site_id,
    DROP CONSTRAINT IF EXISTS fk_command_on_device_log_organization,
    DROP COLUMN IF EXISTS org_id;

DROP INDEX IF EXISTS idx_activity_log_org_site_created;
ALTER TABLE activity_log
    DROP CONSTRAINT IF EXISTS fk_activity_log_site,
    DROP COLUMN IF EXISTS site_id;
