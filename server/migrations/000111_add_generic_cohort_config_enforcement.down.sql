DROP INDEX IF EXISTS idx_device_enforcement_state_config_state;

DELETE FROM device_enforcement_state WHERE dimension <> 'firmware';

ALTER TABLE device_enforcement_state
    DROP CONSTRAINT ck_device_enforcement_state_desired_hash_nonempty,
    DROP CONSTRAINT ck_device_enforcement_state_state,
    DROP CONSTRAINT ck_device_enforcement_state_dimension,
    DROP COLUMN desired_state_hash,
    DROP COLUMN supported,
    ADD CONSTRAINT ck_device_enforcement_state_dimension
        CHECK (dimension IN ('firmware')),
    ADD CONSTRAINT ck_device_enforcement_state_state
        CHECK (state IN ('pending', 'dispatching', 'dispatched', 'confirmed', 'drifted', 'failed'));

DROP TRIGGER IF EXISTS update_device_config_state_updated_at ON device_config_state;
DROP INDEX IF EXISTS idx_device_config_state_observed;
DROP TABLE IF EXISTS device_config_state;
