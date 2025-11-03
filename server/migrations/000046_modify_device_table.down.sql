-- Restore columns to device table
ALTER TABLE device 
    ADD COLUMN model VARCHAR(255) NULL,
    ADD COLUMN manufacturer VARCHAR(255) NULL,
    ADD COLUMN type VARCHAR(50) NOT NULL DEFAULT 'Unknown',
    ADD COLUMN first_discovered TIMESTAMP(6) DEFAULT CURRENT_TIMESTAMP(6),
    ADD COLUMN last_seen TIMESTAMP(6) DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    ADD COLUMN is_active BOOLEAN DEFAULT TRUE;

CREATE INDEX idx_device_type ON device(type);

-- Restore data from discovered_device
UPDATE device d
INNER JOIN discovered_device dd ON d.discovered_device_id = dd.id
SET 
    d.model = dd.model,
    d.manufacturer = dd.manufacturer,
    d.type = dd.type,
    d.first_discovered = dd.first_discovered,
    d.last_seen = dd.last_seen,
    d.is_active = dd.is_active;

DROP INDEX idx_device_discovered_device_id ON device;

ALTER TABLE device
    DROP FOREIGN KEY fk_device_discovered_device,
    DROP COLUMN discovered_device_id;
