-- Multi-site support: foundational `site` table. Schema only at this
-- point; no service consumes it yet. Power-contract fields are deferred
-- to a follow-up migration once the ISO / utility / rate-structure
-- modeling is locked in.
-- See docs/plans/2026-05-05-multi-site-support-plan.md.

CREATE TABLE site (
    id                BIGSERIAL PRIMARY KEY,
    org_id            BIGINT NOT NULL,
    name              VARCHAR(255) NOT NULL,
    description       TEXT,
    location_city     VARCHAR(255),
    location_state    VARCHAR(255),
    timezone          VARCHAR(64),
    power_capacity_mw NUMERIC(10,3),
    -- Newline-separated list of CIDRs/IPs for discovery scan; canonicalized
    -- and validated server-side at every write (see plan "Network config
    -- validation"). Stored verbatim here; the column is intentionally text
    -- rather than `inet[]` so partial-malformed saves can round-trip with
    -- per-line errors.
    network_config    TEXT,

    created_at        TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at        TIMESTAMPTZ NULL,

    CONSTRAINT fk_site_organization FOREIGN KEY (org_id)
        REFERENCES organization(id) ON DELETE RESTRICT
);

-- Site name is unique within an org for live rows; soft-deleted rows are
-- excluded so a name can be reused after deletion.
CREATE UNIQUE INDEX uk_site_org_name
    ON site(org_id, name)
    WHERE deleted_at IS NULL;

CREATE INDEX idx_site_org_deleted
    ON site(org_id, deleted_at);

CREATE TRIGGER update_site_updated_at
    BEFORE UPDATE ON site
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();
