-- Build the live-row pool key replacement index before 000119 swaps out the
-- old full-table constraint.
--
-- CONCURRENTLY must be the sole statement and cannot run in a transaction.
CREATE UNIQUE INDEX CONCURRENTLY uk_pool_org_url_username_live
    ON pool (org_id, url, username)
    WHERE deleted_at IS NULL;
