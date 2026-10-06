-- Restore the original full-table pool key uniqueness constraint.
--
-- This can fail after the up migration has admitted reused pool keys behind
-- soft-deleted rows. Resolve those duplicate historical rows before migrating
-- down.
ALTER TABLE pool
    ADD CONSTRAINT uk_pool_org_url_username_full UNIQUE (org_id, url, username);

DROP INDEX uk_pool_org_url_username;

ALTER TABLE pool
    RENAME CONSTRAINT uk_pool_org_url_username_full TO uk_pool_org_url_username;
