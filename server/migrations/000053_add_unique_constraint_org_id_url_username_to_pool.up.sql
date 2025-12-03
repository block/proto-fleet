-- Delete duplicate pools, keeping only the most recent one for each (org_id, url, username) combination
DELETE p1 FROM pool p1
INNER JOIN pool p2 ON
    p1.org_id = p2.org_id
    AND p1.url = p2.url
    AND p1.username = p2.username
    AND p1.id < p2.id;

-- Now add the unique constraint
ALTER TABLE pool
ADD UNIQUE KEY uk_pool_org_url_username (org_id, url, username);
