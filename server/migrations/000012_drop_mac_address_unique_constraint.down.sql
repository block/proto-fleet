ALTER TABLE device
    ADD CONSTRAINT mac_address UNIQUE uq_device_mac_address (mac_address);