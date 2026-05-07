-- Multi-site support: stamp every history-bearing row with the writer's
-- `site_id` so per-site filters on multi-site dashboards use the
-- row-stamped value rather than the device's *current* site (which would
-- rewrite history on rename / reassign / delete). Pre-multi-site rows
-- stay NULL and surface in a "(no site)" bucket on the relevant pages.
-- See docs/plans/2026-05-05-multi-site-support-plan.md.

-- activity_log: regular table, FK with ON DELETE SET NULL.
ALTER TABLE activity_log
    ADD COLUMN site_id BIGINT NULL,
    ADD CONSTRAINT fk_activity_log_site FOREIGN KEY (site_id)
        REFERENCES site(id) ON DELETE SET NULL;
CREATE INDEX idx_activity_log_org_site_created
    ON activity_log(organization_id, site_id, created_at DESC);

-- command_on_device_log: regular table, FK with ON DELETE SET NULL.
ALTER TABLE command_on_device_log
    ADD COLUMN site_id BIGINT NULL,
    ADD CONSTRAINT fk_command_on_device_log_site FOREIGN KEY (site_id)
        REFERENCES site(id) ON DELETE SET NULL;
CREATE INDEX idx_command_on_device_log_site
    ON command_on_device_log(site_id);

-- errors: regular table, FK with ON DELETE SET NULL.
ALTER TABLE errors
    ADD COLUMN site_id BIGINT NULL,
    ADD CONSTRAINT fk_errors_site FOREIGN KEY (site_id)
        REFERENCES site(id) ON DELETE SET NULL;
CREATE INDEX idx_errors_org_site_last_seen
    ON errors(org_id, site_id, last_seen_at DESC);

-- miner_state_snapshots: TimescaleDB hypertable. No FK (matches the
-- existing `org_id BIGINT NOT NULL` precedent on this table — hypertables
-- intentionally avoid cross-table FKs to keep chunk maintenance cheap).
ALTER TABLE miner_state_snapshots
    ADD COLUMN site_id BIGINT NULL;
CREATE INDEX idx_miner_state_snapshots_org_site_time
    ON miner_state_snapshots(org_id, site_id, time DESC);

-- device_metrics: TimescaleDB hypertable. No FK, same reasoning as
-- miner_state_snapshots.
ALTER TABLE device_metrics
    ADD COLUMN site_id BIGINT NULL;
CREATE INDEX idx_device_metrics_site_time
    ON device_metrics(site_id, time DESC);
