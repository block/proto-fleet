-- Multi-site support: `building` table (replaces today's `device_set_rack.zone`
-- string as a first-class entity). `site_id` is nullable so a building may
-- exist without an assigned site (zone-promoted buildings on upgrade,
-- placeholder buildings created ahead of site assignment).
-- See docs/plans/2026-05-05-multi-site-support-plan.md.

CREATE TABLE building (
    id                        BIGSERIAL PRIMARY KEY,
    org_id                    BIGINT NOT NULL,
    site_id                   BIGINT NULL,
    name                      VARCHAR(255) NOT NULL,
    description               TEXT,

    -- Capacity / layout
    power_kw                  NUMERIC(10,3),
    overhead_kw               NUMERIC(10,3),
    aisles                    INT,
    physical_rack_count       INT,
    racks_per_aisle           INT,

    -- Defaults applied when a new rack is added to the building. Pre-existing
    -- racks may not match these defaults; that's allowed. Mirrors
    -- `device_set_rack.rows` / `.columns` / `.order_index` shape so the
    -- existing rack create/edit code paths can adopt them as defaults
    -- without a type translation. `default_rack_order_index` carries the
    -- same SMALLINT encoding as `device_set_rack.order_index` (proto
    -- RackOrderIndex enum: BOTTOM_LEFT=1, TOP_LEFT=2, BOTTOM_RIGHT=3,
    -- TOP_RIGHT=4; 0 = unspecified).
    default_rack_rows         INT,
    default_rack_columns      INT,
    default_rack_order_index  SMALLINT NOT NULL DEFAULT 0,

    created_at                TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at                TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at                TIMESTAMPTZ NULL,

    CONSTRAINT fk_building_organization FOREIGN KEY (org_id)
        REFERENCES organization(id) ON DELETE RESTRICT,
    CONSTRAINT fk_building_site FOREIGN KEY (site_id)
        REFERENCES site(id) ON DELETE RESTRICT,

    CONSTRAINT ck_building_default_rack_dims
        CHECK (
            (default_rack_rows IS NULL AND default_rack_columns IS NULL)
            OR (default_rack_rows IS NOT NULL AND default_rack_columns IS NOT NULL
                AND default_rack_rows > 0 AND default_rack_columns > 0)
        ),
    CONSTRAINT ck_building_aisles_nonneg
        CHECK (aisles IS NULL OR aisles >= 0),
    CONSTRAINT ck_building_physical_rack_count_nonneg
        CHECK (physical_rack_count IS NULL OR physical_rack_count >= 0),
    CONSTRAINT ck_building_racks_per_aisle_nonneg
        CHECK (racks_per_aisle IS NULL OR racks_per_aisle >= 0)
);

-- Name is unique within site when site is set; unique within org when
-- unassigned. Two partial unique indexes match the two states.
CREATE UNIQUE INDEX uk_building_site_name
    ON building(site_id, name)
    WHERE site_id IS NOT NULL AND deleted_at IS NULL;

CREATE UNIQUE INDEX uk_building_org_name_unassigned
    ON building(org_id, name)
    WHERE site_id IS NULL AND deleted_at IS NULL;

CREATE INDEX idx_building_org_deleted
    ON building(org_id, deleted_at);
CREATE INDEX idx_building_site_deleted
    ON building(site_id, deleted_at);

CREATE TRIGGER update_building_updated_at
    BEFORE UPDATE ON building
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();
