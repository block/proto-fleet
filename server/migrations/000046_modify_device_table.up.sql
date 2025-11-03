ALTER TABLE device
    ADD COLUMN discovered_device_id BIGINT NULL AFTER org_id;

-- Since we inserted with matching IDs in migration 043, use device.id to find discovered_device.id
UPDATE device d
INNER JOIN discovered_device dd ON dd.id = d.id
SET d.discovered_device_id = dd.id;

ALTER TABLE device
    ADD CONSTRAINT fk_device_discovered_device 
        FOREIGN KEY (discovered_device_id) REFERENCES discovered_device(id);

ALTER TABLE device
    DROP FOREIGN KEY fk_device_discovered_device;

ALTER TABLE device 
    MODIFY COLUMN discovered_device_id BIGINT NOT NULL;

ALTER TABLE device
    ADD CONSTRAINT fk_device_discovered_device
        FOREIGN KEY (discovered_device_id) REFERENCES discovered_device(id);

CREATE INDEX idx_device_discovered_device_id ON device(discovered_device_id);

DROP INDEX idx_device_type ON device;

ALTER TABLE device 
    DROP COLUMN model,
    DROP COLUMN manufacturer,
    DROP COLUMN type,
    DROP COLUMN first_discovered,
    DROP COLUMN last_seen,
    DROP COLUMN is_active;
