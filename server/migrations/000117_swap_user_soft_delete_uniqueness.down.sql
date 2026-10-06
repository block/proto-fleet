-- Restore the original full-table user.username uniqueness constraint.
--
-- This can fail after the up migration has admitted reused usernames behind
-- soft-deleted rows. Resolve those duplicate historical rows before migrating
-- down.
ALTER TABLE "user"
    ADD CONSTRAINT uq_user_username_full UNIQUE (username);

DROP INDEX uq_user_username;

ALTER TABLE "user"
    RENAME CONSTRAINT uq_user_username_full TO uq_user_username;
