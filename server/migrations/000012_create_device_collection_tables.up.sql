-- Collection type enum
CREATE TYPE collection_type AS ENUM ('group', 'rack');

-- Base collection table (both groups and racks)
CREATE TABLE device_collection (
    id BIGSERIAL PRIMARY KEY,
    org_id BIGINT NOT NULL,
    type collection_type NOT NULL,
    label TEXT NOT NULL,
    description TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMPTZ NULL,

    CONSTRAINT fk_device_collection_org FOREIGN KEY (org_id)
        REFERENCES organization(id) ON DELETE RESTRICT
);

-- Partial unique index: allows label reuse after soft delete
CREATE UNIQUE INDEX uk_device_collection_org_type_label
    ON device_collection(org_id, type, label)
    WHERE deleted_at IS NULL;

-- Indexes for collection lookups
-- Note: idx_device_collection_org_type covers org_id queries via leftmost prefix
CREATE INDEX idx_device_collection_org_type ON device_collection(org_id, type);
CREATE INDEX idx_device_collection_org_deleted ON device_collection(org_id, deleted_at);

-- Trigger for updated_at
CREATE TRIGGER update_device_collection_updated_at
    BEFORE UPDATE ON device_collection
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- Rack-specific extension (dimensions, zone)
CREATE TABLE device_collection_rack (
    collection_id BIGINT PRIMARY KEY REFERENCES device_collection(id) ON DELETE CASCADE,
    location TEXT,
    rows INT NOT NULL,
    columns INT NOT NULL,

    CONSTRAINT positive_dimensions CHECK (rows > 0 AND columns > 0)
);

-- All collection memberships (groups AND racks)
-- Note: collection_type is denormalized for efficient indexing; derived from collection on insert
CREATE TABLE device_collection_membership (
    id BIGSERIAL PRIMARY KEY,
    org_id BIGINT NOT NULL,
    collection_id BIGINT NOT NULL,
    collection_type collection_type NOT NULL,
    device_id BIGINT NOT NULL,
    device_identifier TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT fk_membership_org FOREIGN KEY (org_id)
        REFERENCES organization(id) ON DELETE RESTRICT,
    CONSTRAINT fk_membership_collection FOREIGN KEY (collection_id)
        REFERENCES device_collection(id) ON DELETE CASCADE,
    CONSTRAINT fk_membership_device FOREIGN KEY (device_id)
        REFERENCES device(id) ON DELETE CASCADE,
    CONSTRAINT uk_collection_device UNIQUE (collection_id, device_id)
);

-- Partial unique index: device can only be in ONE rack total
CREATE UNIQUE INDEX idx_one_rack_per_device ON device_collection_membership(device_id)
    WHERE collection_type = 'rack';

-- Indexes for membership lookups and filtering
CREATE INDEX idx_dcm_device_identifier ON device_collection_membership(device_identifier);
CREATE INDEX idx_dcm_org_collection ON device_collection_membership(org_id, collection_id);
CREATE INDEX idx_dcm_org_device ON device_collection_membership(org_id, device_id);
CREATE INDEX idx_dcm_org_type ON device_collection_membership(org_id, collection_type);

-- Rack slot positions (extension for rack members only)
-- Note: Application layer validates row < rack.rows and col < rack.columns
CREATE TABLE rack_slot (
    collection_id BIGINT NOT NULL,
    device_id BIGINT NOT NULL,
    row INT NOT NULL,
    col INT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    PRIMARY KEY (collection_id, device_id),
    CONSTRAINT fk_rack_slot_membership FOREIGN KEY (collection_id, device_id)
        REFERENCES device_collection_membership(collection_id, device_id) ON DELETE CASCADE,
    CONSTRAINT uk_rack_slot_position UNIQUE (collection_id, row, col),
    CONSTRAINT valid_position CHECK (row >= 0 AND col >= 0)
);
