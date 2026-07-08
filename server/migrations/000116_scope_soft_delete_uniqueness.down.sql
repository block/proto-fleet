-- Restore the original full-table uniqueness constraints.
--
-- This can fail after the up migration has admitted reused keys behind
-- soft-deleted rows. Resolve those duplicate historical rows before migrating
-- down.
ALTER TABLE "user"
    ADD CONSTRAINT uq_user_username_full UNIQUE (username);

DROP INDEX uq_user_username;

ALTER TABLE "user"
    RENAME CONSTRAINT uq_user_username_full TO uq_user_username;

ALTER TABLE pool
    ADD CONSTRAINT uk_pool_org_url_username_full UNIQUE (org_id, url, username);

DROP INDEX uk_pool_org_url_username;

ALTER TABLE pool
    RENAME CONSTRAINT uk_pool_org_url_username_full TO uk_pool_org_url_username;
