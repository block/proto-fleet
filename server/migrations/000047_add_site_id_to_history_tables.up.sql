-- Stamp history rows with site_id at write time so per-site filters
-- read the row-stamped value, not the device's *current* site (which
-- would rewrite history on rename / reassign / delete). Pre-multi-site
-- rows stay NULL and surface in a "(no site)" bucket.

-- activity_log: organization_id is nullable (system events); MATCH
-- SIMPLE skips the FK on NULL org rows. The CHECK constraint closes
-- the loophole — a row stamped with a site_id MUST also carry an
-- organization_id so the composite FK is enforced.
ALTER TABLE activity_log
    ADD COLUMN site_id BIGINT NULL,
    ADD CONSTRAINT fk_activity_log_site FOREIGN KEY (site_id, organization_id)
        REFERENCES site(id, org_id) ON DELETE SET NULL (site_id),
    ADD CONSTRAINT ck_activity_log_site_requires_org
        CHECK (site_id IS NULL OR organization_id IS NOT NULL);
CREATE INDEX idx_activity_log_org_site_created
    ON activity_log(organization_id, site_id, created_at DESC, id DESC);

-- command_on_device_log has no org column today; denormalize from
-- device.org_id so the site FK can be composite-keyed. Pre-flight
-- check raises before SET NOT NULL if any historical orphan rows
-- exist (codl row whose device was hard-deleted before the
-- fk_command_on_device_log_device constraint took effect). A clean
-- abort with a clear message beats SET NOT NULL failing
-- mid-migration with the dirty flag set.
ALTER TABLE command_on_device_log ADD COLUMN org_id BIGINT NULL;

UPDATE command_on_device_log codl
SET org_id = d.org_id
FROM device d
WHERE codl.device_id = d.id;

DO $$
DECLARE
    orphan_count BIGINT;
BEGIN
    SELECT COUNT(*) INTO orphan_count
    FROM command_on_device_log
    WHERE org_id IS NULL;

    IF orphan_count > 0 THEN
        RAISE EXCEPTION
            'command_on_device_log has % rows with no matching device row; cannot SET NOT NULL on org_id. Resolve orphans before retrying.',
            orphan_count;
    END IF;
END
$$;

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

ALTER TABLE errors
    ADD COLUMN site_id BIGINT NULL,
    ADD CONSTRAINT fk_errors_site FOREIGN KEY (site_id, org_id)
        REFERENCES site(id, org_id) ON DELETE SET NULL (site_id);
CREATE INDEX idx_errors_org_site_last_seen
    ON errors(org_id, site_id, last_seen_at DESC);

-- Hypertables: no FK (matches the existing org_id precedent on these
-- tables). Partial index keeps NULL-only chunks out of the btree.
ALTER TABLE miner_state_snapshots
    ADD COLUMN site_id BIGINT NULL;
CREATE INDEX idx_miner_state_snapshots_org_site_time
    ON miner_state_snapshots(org_id, site_id, time DESC)
    WHERE site_id IS NOT NULL;

ALTER TABLE device_metrics
    ADD COLUMN site_id BIGINT NULL;
CREATE INDEX idx_device_metrics_site_time
    ON device_metrics(site_id, time DESC)
    WHERE site_id IS NOT NULL;
