CREATE TABLE building (
    id                        BIGSERIAL PRIMARY KEY,
    org_id                    BIGINT NOT NULL,
    site_id                   BIGINT NULL,
    name                      VARCHAR(255) NOT NULL,
    description               TEXT,

    power_kw                  NUMERIC(10,3),
    overhead_kw               NUMERIC(10,3),
    aisles                    INT,
    physical_rack_count       INT,
    racks_per_aisle           INT,

    -- default_rack_order_index encodes the proto RackOrderIndex enum
    -- (BOTTOM_LEFT=1, TOP_LEFT=2, BOTTOM_RIGHT=3, TOP_RIGHT=4; 0 =
    -- unspecified) — same SMALLINT shape as device_set_rack.order_index.
    default_rack_rows         INT,
    default_rack_columns      INT,
    default_rack_order_index  SMALLINT NOT NULL DEFAULT 0,

    created_at                TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at                TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at                TIMESTAMPTZ NULL,

    CONSTRAINT fk_building_organization FOREIGN KEY (org_id)
        REFERENCES organization(id) ON DELETE RESTRICT,
    CONSTRAINT fk_building_site FOREIGN KEY (site_id, org_id)
        REFERENCES site(id, org_id) ON DELETE RESTRICT,
    CONSTRAINT uq_building_id_org_id UNIQUE (id, org_id),

    CONSTRAINT ck_building_default_rack_dims
        CHECK (
            (default_rack_rows IS NULL AND default_rack_columns IS NULL)
            OR (default_rack_rows IS NOT NULL AND default_rack_columns IS NOT NULL
                AND default_rack_rows > 0 AND default_rack_columns > 0)
        ),
    CONSTRAINT ck_building_default_rack_order_index
        CHECK (default_rack_order_index BETWEEN 0 AND 4),
    CONSTRAINT ck_building_aisles_nonneg
        CHECK (aisles IS NULL OR aisles >= 0),
    CONSTRAINT ck_building_physical_rack_count_nonneg
        CHECK (physical_rack_count IS NULL OR physical_rack_count >= 0),
    CONSTRAINT ck_building_racks_per_aisle_nonneg
        CHECK (racks_per_aisle IS NULL OR racks_per_aisle >= 0)
);

-- Name is unique within an assigned site. Unassigned buildings are not
-- name-unique because cascade-unassign on site delete must always
-- succeed — if the deleted site held "Aisle-1" and an unassigned
-- "Aisle-1" already existed, a strict constraint would abort the
-- transaction. The service layer surfaces collisions in the
-- unassigned bucket as a UX warning instead.
CREATE UNIQUE INDEX uk_building_site_name
    ON building(site_id, name)
    WHERE site_id IS NOT NULL AND deleted_at IS NULL;

CREATE INDEX idx_building_org_deleted
    ON building(org_id, deleted_at);
CREATE INDEX idx_building_site_deleted
    ON building(site_id, deleted_at);

CREATE TRIGGER update_building_updated_at
    BEFORE UPDATE ON building
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();
