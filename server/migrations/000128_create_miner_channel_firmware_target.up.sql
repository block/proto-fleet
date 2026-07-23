CREATE TABLE miner_channel_firmware_target (
    miner_channel_id        BIGINT      NOT NULL,
    org_id           BIGINT      NOT NULL,
    manufacturer     TEXT        NOT NULL,
    model            TEXT        NOT NULL,
    firmware_file_id VARCHAR     NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT fk_miner_channel_firmware_target_miner_channel
        FOREIGN KEY (miner_channel_id) REFERENCES miner_channel(id) ON DELETE CASCADE,
    CONSTRAINT fk_miner_channel_firmware_target_org
        FOREIGN KEY (org_id) REFERENCES organization(id) ON DELETE RESTRICT,
    CONSTRAINT ck_miner_channel_firmware_target_manufacturer_nonempty
        CHECK (length(trim(manufacturer)) > 0),
    CONSTRAINT ck_miner_channel_firmware_target_model_nonempty
        CHECK (length(trim(model)) > 0),
    CONSTRAINT ck_miner_channel_firmware_target_firmware_file_nonempty
        CHECK (firmware_file_id IS NULL OR length(trim(firmware_file_id)) > 0)
);

CREATE UNIQUE INDEX uq_miner_channel_firmware_target_canonical_type
    ON miner_channel_firmware_target (miner_channel_id, LOWER(BTRIM(manufacturer)), LOWER(BTRIM(model)));

CREATE INDEX idx_miner_channel_firmware_target_org_type
    ON miner_channel_firmware_target (org_id, manufacturer, model);

CREATE TRIGGER update_miner_channel_firmware_target_updated_at
    BEFORE UPDATE ON miner_channel_firmware_target
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();
