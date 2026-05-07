-- Multi-site support: stamp every history-bearing row with the writer's
-- `site_id` so per-site filters on multi-site dashboards use the
-- row-stamped value rather than the device's *current* site (which would
-- rewrite history on rename / reassign / delete). Pre-multi-site rows
-- stay NULL and surface in a "(no site)" bucket on the relevant pages.
-- See docs/plans/2026-05-05-multi-site-support-plan.md.

-- activity_log: composite FK against site(id, org_id) so a row stamped
-- with org A's organization_id can never point at org B's site, even if
-- a future writer skips service-layer validation. organization_id is
-- nullable here (system events); MATCH SIMPLE means rows with NULL
-- organization_id skip the FK check, matching today's behavior.
-- ON DELETE SET NULL (site_id) so site deletion only nulls site_id.
ALTER TABLE activity_log
    ADD COLUMN site_id BIGINT NULL,
    ADD CONSTRAINT fk_activity_log_site FOREIGN KEY (site_id, organization_id)
        REFERENCES site(id, org_id) ON DELETE SET NULL (site_id);
-- Index shape mirrors the existing `idx_activity_log_org_created`
-- (org, created_at DESC, id DESC) so site-filtered keyset pagination
-- uses the same `(created_at, id) <` cursor in `activity.sql` without
-- an extra sort.
CREATE INDEX idx_activity_log_org_site_created
    ON activity_log(organization_id, site_id, created_at DESC, id DESC);

-- command_on_device_log: denormalize `org_id` from `device` so the site
-- FK can be composite-keyed and Postgres rejects any future writer that
-- stamps `site_id` with a cross-tenant value. Same pattern as
-- `device_set_rack.org_id`. Backfill from device first, then promote to
-- NOT NULL and add the composite site FK with column-list SET NULL.
ALTER TABLE command_on_device_log ADD COLUMN org_id BIGINT NULL;

UPDATE command_on_device_log codl
SET org_id = d.org_id
FROM device d
WHERE codl.device_id = d.id;

ALTER TABLE command_on_device_log
    ALTER COLUMN org_id SET NOT NULL,
    ADD CONSTRAINT fk_command_on_device_log_organization FOREIGN KEY (org_id)
        REFERENCES organization(id) ON DELETE RESTRICT;

ALTER TABLE command_on_device_log
    ADD COLUMN site_id BIGINT NULL,
    ADD CONSTRAINT fk_command_on_device_log_site FOREIGN KEY (site_id, org_id)
        REFERENCES site(id, org_id) ON DELETE SET NULL (site_id);
CREATE INDEX idx_command_on_device_log_site
    ON command_on_device_log(site_id);

-- errors: composite FK same as activity_log. errors.org_id is NOT NULL
-- so the FK is enforced for every site-stamped row.
ALTER TABLE errors
    ADD COLUMN site_id BIGINT NULL,
    ADD CONSTRAINT fk_errors_site FOREIGN KEY (site_id, org_id)
        REFERENCES site(id, org_id) ON DELETE SET NULL (site_id);
CREATE INDEX idx_errors_org_site_last_seen
    ON errors(org_id, site_id, last_seen_at DESC);

-- miner_state_snapshots: TimescaleDB hypertable. No FK (matches the
-- existing `org_id BIGINT NOT NULL` precedent on this table — hypertables
-- intentionally avoid cross-table FKs to keep chunk maintenance cheap).
ALTER TABLE miner_state_snapshots
    ADD COLUMN site_id BIGINT NULL;
-- Partial index: pre-multi-site rows are NULL, and new rows stay NULL
-- until writers start stamping `site_id`. A full index on a hypertable
-- carries every chunk's NULL rows for no benefit; the partial index
-- covers only the queryable subset. Add a separate `IS NULL` index
-- only if the "(no site)" bucket needs efficient querying later.
CREATE INDEX idx_miner_state_snapshots_org_site_time
    ON miner_state_snapshots(org_id, site_id, time DESC)
    WHERE site_id IS NOT NULL;

-- device_metrics: TimescaleDB hypertable. No FK, same reasoning as
-- miner_state_snapshots; partial index for the same reason.
ALTER TABLE device_metrics
    ADD COLUMN site_id BIGINT NULL;
CREATE INDEX idx_device_metrics_site_time
    ON device_metrics(site_id, time DESC)
    WHERE site_id IS NOT NULL;
