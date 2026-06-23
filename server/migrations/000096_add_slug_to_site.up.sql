ALTER TABLE site
    ADD COLUMN slug VARCHAR(63);

UPDATE site
SET slug =
    COALESCE(
        NULLIF(trim(both '-' from regexp_replace(lower(name), '[^a-z0-9]+', '-', 'g')), ''),
        'site'
    ) || '-' || substr(md5(random()::text), 1, 4);

ALTER TABLE site
    ALTER COLUMN slug SET NOT NULL;

CREATE UNIQUE INDEX uk_site_org_slug
    ON site(org_id, slug)
    WHERE deleted_at IS NULL;
