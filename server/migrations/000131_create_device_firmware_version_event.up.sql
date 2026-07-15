CREATE TABLE device_firmware_version_event (
    id                BIGSERIAL   PRIMARY KEY,
    org_id            BIGINT      NOT NULL,
    device_identifier VARCHAR     NOT NULL,
    firmware_version  TEXT        NOT NULL,
    observed_at       TIMESTAMPTZ NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT fk_device_firmware_version_event_org
        FOREIGN KEY (org_id) REFERENCES organization(id) ON DELETE RESTRICT,
    CONSTRAINT ck_device_firmware_version_event_identifier_nonempty
        CHECK (length(trim(device_identifier)) > 0),
    CONSTRAINT ck_device_firmware_version_event_version_nonempty
        CHECK (length(trim(firmware_version)) > 0)
);

CREATE INDEX idx_device_firmware_version_event_device_observed
    ON device_firmware_version_event (org_id, device_identifier, observed_at DESC, id DESC);

-- Existing current-state rows provide the earliest trustworthy baseline. No
-- pre-feature version history can be reconstructed from discovered devices.
INSERT INTO device_firmware_version_event (
    org_id,
    device_identifier,
    firmware_version,
    observed_at,
    created_at
)
SELECT
    org_id,
    device_identifier,
    firmware_version,
    observed_at,
    created_at
FROM device_firmware_state;

CREATE FUNCTION record_device_firmware_version_event()
RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'INSERT' OR OLD.firmware_version IS DISTINCT FROM NEW.firmware_version THEN
        INSERT INTO device_firmware_version_event (
            org_id,
            device_identifier,
            firmware_version,
            observed_at
        ) VALUES (
            NEW.org_id,
            NEW.device_identifier,
            NEW.firmware_version,
            NEW.observed_at
        );
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER record_device_firmware_version_event
    AFTER INSERT OR UPDATE OF firmware_version ON device_firmware_state
    FOR EACH ROW
    EXECUTE FUNCTION record_device_firmware_version_event();
