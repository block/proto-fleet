-- Undo 000116 if the replacement user index was created but not swapped in by
-- 000117. CONCURRENTLY must be the sole statement and cannot run in a
-- transaction.
DROP INDEX CONCURRENTLY IF EXISTS uq_user_username_live;
