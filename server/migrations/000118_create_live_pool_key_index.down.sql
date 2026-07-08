-- Undo 000118 if the replacement pool key index was created but not swapped in
-- by 000119. CONCURRENTLY must be the sole statement and cannot run in a
-- transaction.
DROP INDEX CONCURRENTLY IF EXISTS uk_pool_org_url_username_live;
