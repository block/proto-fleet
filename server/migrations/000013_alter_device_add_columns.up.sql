ALTER TABLE device
    ADD COLUMN org_id BIGINT,
    ADD COLUMN model VARCHAR(255),
    ADD COLUMN manufacturer VARCHAR(255);
