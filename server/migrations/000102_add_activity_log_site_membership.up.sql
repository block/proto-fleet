-- Per-event site membership for multi-device fleet activity events whose
-- touched device set spans more than one site (#538). This is the overflow
-- representation that backs activity_log.multi_site: the scalar
-- activity_log.site_id remains the fast path for the single-site case
-- (cardinality 1), and these rows carry the full set ONLY when the scope is
-- cardinality >= 2. The two are mutually exclusive by construction —
-- multi_site = true  <=>  site_id IS NULL AND >=1 membership row exists.
--
-- A row with site_id IS NULL records that the event also touched site-less
-- ("unassigned") devices, so a cross-site batch that includes unassigned
-- devices surfaces in BOTH its sites' feeds and the /unassigned bucket. The
-- composite site FK is MATCH SIMPLE, so the NULL-site row skips the FK.
--
-- Mirrors command_on_device_log's site denormalization: org_id is carried so
-- the site FK can be composite-keyed, and ON DELETE SET NULL keeps a deleted
-- site from leaving a dangling membership id (the array-column alternative
-- can't preserve this).
CREATE TABLE activity_log_site (
    activity_log_id BIGINT NOT NULL
        REFERENCES activity_log(id) ON DELETE CASCADE,
    org_id  BIGINT NOT NULL,
    site_id BIGINT NULL,
    CONSTRAINT fk_activity_log_site_membership_site
        FOREIGN KEY (site_id, org_id) REFERENCES site(id, org_id)
        ON DELETE SET NULL (site_id)
);

-- Unique on (activity_log_id, site_id) both enforces distinct membership and
-- serves the read query's correlated EXISTS lookups (site match + the
-- site_id IS NULL "touches unassigned" probe). NULLs compare distinct, but
-- the writer emits at most one NULL row per event, so no duplicate arises.
CREATE UNIQUE INDEX uq_activity_log_site ON activity_log_site (activity_log_id, site_id);
