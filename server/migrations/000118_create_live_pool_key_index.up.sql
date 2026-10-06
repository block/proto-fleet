-- Build the live-row pool key replacement index before 000119 swaps out the
-- old full-table constraint.
--
-- CONCURRENTLY must be the sole statement and cannot run in a transaction.
-- golang-migrate v4's postgres driver runs each statement directly via
-- conn.ExecContext, no implicit transaction wraps the body, so
-- CREATE UNIQUE INDEX CONCURRENTLY is safe here. If a partial build
-- fails it leaves schema_migrations.dirty=true at version 118 and may
-- leave an INVALID uk_pool_org_url_username_live index in pg_indexes. Recovery
-- is to DROP the INVALID index and `migrate force 117` before
-- re-deploying.
CREATE UNIQUE INDEX CONCURRENTLY uk_pool_org_url_username_live
    ON pool (org_id, url, username)
    WHERE deleted_at IS NULL;
