-- Swap pool(org_id, url, username) from a full-table unique constraint to the
-- live-row partial index built in 000118. The final index name matches the old
-- constraint name to keep unique-violation metadata stable for callers.
ALTER TABLE pool
    DROP CONSTRAINT uk_pool_org_url_username;

ALTER INDEX uk_pool_org_url_username_live
    RENAME TO uk_pool_org_url_username;
