-- Scope legacy uniqueness checks to live rows so soft-deleted pools and users
-- do not reserve their old keys forever.
--
-- Build replacement indexes before dropping the old constraints so the schema
-- never has a uniqueness gap. The final index names match the old constraint
-- names to keep PostgreSQL unique-violation metadata stable for callers.
CREATE UNIQUE INDEX uq_user_username_live
    ON "user" (username)
    WHERE deleted_at IS NULL;

ALTER TABLE "user"
    DROP CONSTRAINT uq_user_username;

ALTER INDEX uq_user_username_live
    RENAME TO uq_user_username;

CREATE UNIQUE INDEX uk_pool_org_url_username_live
    ON pool (org_id, url, username)
    WHERE deleted_at IS NULL;

ALTER TABLE pool
    DROP CONSTRAINT uk_pool_org_url_username;

ALTER INDEX uk_pool_org_url_username_live
    RENAME TO uk_pool_org_url_username;
