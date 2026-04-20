-- Normalize legacy Fleet pool usernames by removing stored worker suffixes.
-- Only rewrite active rows when doing so is unambiguous and will not collide
-- with another active pool under the existing (org_id, url, username) unique key.
WITH normalized_candidates AS (
    SELECT
        p.id,
        p.org_id,
        p.url,
        BTRIM(SPLIT_PART(BTRIM(p.username), '.', 1)) AS base_username
    FROM pool p
    WHERE p.deleted_at IS NULL
      AND POSITION('.' IN p.username) > 0
),
safe_candidates AS (
    SELECT n.id, n.base_username
    FROM normalized_candidates n
    WHERE n.base_username <> ''
      AND NOT EXISTS (
          SELECT 1
          FROM pool p2
          WHERE p2.deleted_at IS NULL
            AND p2.org_id = n.org_id
            AND p2.url = n.url
            AND p2.username = n.base_username
      )
      AND (
          SELECT COUNT(*)
          FROM normalized_candidates n2
          WHERE n2.org_id = n.org_id
            AND n2.url = n.url
            AND n2.base_username = n.base_username
      ) = 1
)
UPDATE pool p
SET username = s.base_username,
    updated_at = CURRENT_TIMESTAMP
FROM safe_candidates s
WHERE p.id = s.id;
