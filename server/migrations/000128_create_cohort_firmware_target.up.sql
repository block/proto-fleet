CREATE TABLE cohort_firmware_target (
    cohort_id        BIGINT      NOT NULL,
    org_id           BIGINT      NOT NULL,
    manufacturer     TEXT        NOT NULL,
    model            TEXT        NOT NULL,
    firmware_file_id VARCHAR     NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT fk_cohort_firmware_target_cohort
        FOREIGN KEY (cohort_id) REFERENCES cohort(id) ON DELETE CASCADE,
    CONSTRAINT fk_cohort_firmware_target_org
        FOREIGN KEY (org_id) REFERENCES organization(id) ON DELETE RESTRICT,
    CONSTRAINT ck_cohort_firmware_target_manufacturer_nonempty
        CHECK (length(trim(manufacturer)) > 0),
    CONSTRAINT ck_cohort_firmware_target_model_nonempty
        CHECK (length(trim(model)) > 0),
    CONSTRAINT ck_cohort_firmware_target_firmware_file_nonempty
        CHECK (firmware_file_id IS NULL OR length(trim(firmware_file_id)) > 0)
);

CREATE UNIQUE INDEX uq_cohort_firmware_target_canonical_type
    ON cohort_firmware_target (cohort_id, LOWER(BTRIM(manufacturer)), LOWER(BTRIM(model)));

CREATE INDEX idx_cohort_firmware_target_org_type
    ON cohort_firmware_target (org_id, manufacturer, model);

CREATE TRIGGER update_cohort_firmware_target_updated_at
    BEFORE UPDATE ON cohort_firmware_target
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();
