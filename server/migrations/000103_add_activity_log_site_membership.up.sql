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
-- the site FK can be composite-keyed. The site FK is ON DELETE CASCADE (not
-- SET NULL): deleting a site removes only that site's membership row, so the
-- event stays attributed to its surviving sites and is never mistaken for one
-- that touched unassigned devices (a nulled-out site_id would alias the
-- "touched unassigned" representation and leak the event into /unassigned).
-- The event's all-sites visibility is unaffected — the activity_log row
-- persists regardless.
CREATE TABLE activity_log_site (
    activity_log_id BIGINT NOT NULL
        REFERENCES activity_log(id) ON DELETE CASCADE,
    org_id  BIGINT NOT NULL,
    site_id BIGINT NULL,
    CONSTRAINT fk_activity_log_site_membership_site
        FOREIGN KEY (site_id, org_id) REFERENCES site(id, org_id)
        ON DELETE CASCADE
);

-- Read-side lookup + distinctness: the correlated EXISTS probes filter by
-- (activity_log_id, site_id) and (activity_log_id, site_id IS NULL), and the
-- unique index also stops duplicate (activity_log_id, real-site) rows. NULLs
-- compare distinct in a UNIQUE index, so it does not bound the NULL-site rows;
-- the writer emits at most one per event, and ON DELETE CASCADE removes
-- referenced rows rather than nulling them, so no second NULL row can appear.
CREATE UNIQUE INDEX uq_activity_log_site ON activity_log_site (activity_log_id, site_id);

-- Supporting index for the site FK's referential-action lookup: deleting (or
-- re-keying) a site must find its membership rows by (site_id, org_id) to
-- cascade. Without a site_id-leading index that is a full table scan, making
-- site deletion slow/lock-heavy as the table grows. NULL-site rows are never
-- FK-referenced, so the partial predicate keeps the index small.
CREATE INDEX idx_activity_log_site_site
    ON activity_log_site (site_id, org_id) WHERE site_id IS NOT NULL;
