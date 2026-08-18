CREATE UNIQUE INDEX idx_one_channel_per_device
    ON device_set_membership(device_id)
    WHERE device_set_type = 'channel';

ALTER TABLE device_set_membership
    ADD CONSTRAINT fk_device_set_membership_device_set_org
    FOREIGN KEY (device_set_id, org_id)
    REFERENCES device_set(id, org_id)
    ON DELETE CASCADE;

CREATE TABLE firmware_release_set (
    id BIGSERIAL PRIMARY KEY,
    org_id BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT fk_firmware_release_set_org
        FOREIGN KEY (org_id) REFERENCES organization(id) ON DELETE RESTRICT,
    CONSTRAINT uq_firmware_release_set_id_org UNIQUE (id, org_id)
);

CREATE INDEX idx_firmware_release_set_org
    ON firmware_release_set(org_id, created_at DESC, id DESC);

CREATE TABLE firmware_release_target (
    id BIGSERIAL PRIMARY KEY,
    release_set_id BIGINT NOT NULL,
    org_id BIGINT NOT NULL,
    firmware_file_id TEXT NOT NULL,
    target_manufacturer TEXT NOT NULL,
    target_model TEXT NOT NULL,
    firmware_version TEXT NOT NULL,
    sha256 TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT fk_firmware_release_target_set_org
        FOREIGN KEY (release_set_id, org_id)
        REFERENCES firmware_release_set(id, org_id)
        ON DELETE CASCADE,
    CONSTRAINT ck_firmware_release_target_file_id
        CHECK (btrim(firmware_file_id) <> ''),
    CONSTRAINT ck_firmware_release_target_manufacturer
        CHECK (btrim(target_manufacturer) <> ''),
    CONSTRAINT ck_firmware_release_target_model
        CHECK (btrim(target_model) <> ''),
    CONSTRAINT ck_firmware_release_target_version
        CHECK (btrim(firmware_version) <> ''),
    CONSTRAINT ck_firmware_release_target_sha256
        CHECK (sha256 ~ '^[0-9a-f]{64}$')
);

CREATE UNIQUE INDEX uq_firmware_release_target_model
    ON firmware_release_target(
        release_set_id,
        lower(target_manufacturer),
        lower(target_model)
    );

CREATE INDEX idx_firmware_release_target_file
    ON firmware_release_target(firmware_file_id);

CREATE TABLE device_set_channel (
    device_set_id BIGINT PRIMARY KEY,
    org_id BIGINT NOT NULL,
    release_set_id BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT fk_device_set_channel_device_set_org
        FOREIGN KEY (device_set_id, org_id)
        REFERENCES device_set(id, org_id)
        ON DELETE CASCADE,
    CONSTRAINT fk_device_set_channel_release_set_org
        FOREIGN KEY (release_set_id, org_id)
        REFERENCES firmware_release_set(id, org_id)
        ON DELETE RESTRICT
);

CREATE INDEX idx_device_set_channel_release_set
    ON device_set_channel(org_id, release_set_id);

CREATE TRIGGER update_device_set_channel_updated_at
    BEFORE UPDATE ON device_set_channel
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

CREATE FUNCTION reject_firmware_release_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'firmware release sets and targets are immutable'
        USING ERRCODE = '55000';
END;
$$;

CREATE TRIGGER firmware_release_set_immutable
    BEFORE UPDATE OR DELETE ON firmware_release_set
    FOR EACH ROW
    EXECUTE FUNCTION reject_firmware_release_mutation();

CREATE TRIGGER firmware_release_target_immutable
    BEFORE UPDATE OR DELETE ON firmware_release_target
    FOR EACH ROW
    EXECUTE FUNCTION reject_firmware_release_mutation();
