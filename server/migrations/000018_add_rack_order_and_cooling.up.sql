ALTER TABLE device_collection_rack
    ADD COLUMN order_index SMALLINT NOT NULL DEFAULT 0,
    ADD COLUMN cooling_type SMALLINT NOT NULL DEFAULT 0;
