-- Replace unique constraint with a functional index that excludes soft-deleted rows.
-- Active pools share a sentinel value (uniqueness enforced), deleted pools use their
-- deletion timestamp (always unique since pools must be deleted before recreation).
ALTER TABLE pool DROP INDEX uk_pool_org_url_username;

CREATE UNIQUE INDEX uk_pool_org_url_username
ON pool (org_id, url, username, ((COALESCE(deleted_at, TIMESTAMP('1970-01-01')))));
