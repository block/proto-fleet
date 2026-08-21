-- Multi-device fleet writers (rename/unpair miners, group add/remove
-- devices, device building-unassign) operate on a device SET that can
-- span multiple sites, so there is no single scalar site_id to stamp.
-- Before this column such events landed with site_id IS NULL and a
-- non-org-level category, which the Activity read filter (see #534)
-- treats as part of the /unassigned bucket — leaking a cross-site fleet
-- operation into a site-less view.
--
-- multi_site marks an event whose touched device set has no single site
-- scope (spans >1 site, or mixes sited + site-less devices). The read
-- query excludes these from the unassigned bucket so they surface only
-- in the all-sites feed. A single-site batch instead stamps that site_id
-- (multi_site stays false) and a fully site-less batch keeps site_id NULL
-- with multi_site false so it correctly remains in the unassigned bucket.
--
-- Invariant: a multi_site event has no single site, so site_id MUST be
-- NULL. The CHECK closes off a contradictory (site_id set, multi_site
-- true) row.
ALTER TABLE activity_log
    ADD COLUMN multi_site BOOLEAN NOT NULL DEFAULT FALSE,
    ADD CONSTRAINT ck_activity_log_multi_site_requires_null_site
        CHECK (NOT multi_site OR site_id IS NULL);
