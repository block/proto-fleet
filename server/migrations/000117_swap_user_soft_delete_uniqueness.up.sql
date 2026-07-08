-- Swap user.username from a full-table unique constraint to the live-row
-- partial index built in 000116. The final index name matches the old
-- constraint name to keep unique-violation metadata stable for callers.
ALTER TABLE "user"
    DROP CONSTRAINT uq_user_username;

ALTER INDEX uq_user_username_live
    RENAME TO uq_user_username;
