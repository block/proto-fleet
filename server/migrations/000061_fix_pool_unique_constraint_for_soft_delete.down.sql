-- Revert to simple unique constraint (may fail if deleted pools conflict with active ones)
ALTER TABLE pool DROP INDEX uk_pool_org_url_username;
ALTER TABLE pool ADD UNIQUE KEY uk_pool_org_url_username (org_id, url, username);
