-- Scope legacy uniqueness checks to live rows so soft-deleted pools and users
-- do not reserve their old keys forever. This first migration builds the user
-- replacement index before 000117 swaps out the old full-table constraint.
--
-- CONCURRENTLY must be the sole statement and cannot run in a transaction.
-- golang-migrate v4's postgres driver runs each statement directly via
-- conn.ExecContext, no implicit transaction wraps the body, so
-- CREATE UNIQUE INDEX CONCURRENTLY is safe here. If a partial build
-- fails it leaves schema_migrations.dirty=true at version 116 and may
-- leave an INVALID uq_user_username_live index in pg_indexes. Recovery
-- is to DROP the INVALID index and `migrate force 115` before
-- re-deploying.
CREATE UNIQUE INDEX CONCURRENTLY uq_user_username_live
    ON "user" (username)
    WHERE deleted_at IS NULL;
