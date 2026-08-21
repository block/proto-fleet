ALTER TABLE site
    ADD COLUMN slug VARCHAR(63);

WITH normalized AS (
    SELECT
        id,
        COALESCE(
            NULLIF(trim(both '-' from regexp_replace(lower(name), '[^a-z0-9]+', '-', 'g')), ''),
            'site'
        ) AS base
    FROM site
)
UPDATE site
SET slug =
    rtrim(left(normalized.base, 63 - length(site.id::text) - 1), '-') ||
    '-' ||
    site.id::text
FROM normalized
WHERE normalized.id = site.id;

ALTER TABLE site
    ALTER COLUMN slug SET NOT NULL;

CREATE UNIQUE INDEX uk_site_org_slug
    ON site(org_id, slug)
    WHERE deleted_at IS NULL;
