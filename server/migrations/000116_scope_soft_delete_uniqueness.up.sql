-- Scope legacy uniqueness checks to live rows so soft-deleted pools and users
-- do not reserve their old keys forever. This first migration builds the user
-- replacement index before 000117 swaps out the old full-table constraint.
--
-- CONCURRENTLY must be the sole statement and cannot run in a transaction.
CREATE UNIQUE INDEX CONCURRENTLY uq_user_username_live
    ON "user" (username)
    WHERE deleted_at IS NULL;
