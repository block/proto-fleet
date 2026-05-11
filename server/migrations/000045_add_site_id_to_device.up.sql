-- ON DELETE SET NULL (site_id): PG15+ column list — site deletion only
-- nulls site_id, leaving the NOT NULL org_id intact.
ALTER TABLE device
    ADD COLUMN site_id BIGINT NULL,
    ADD CONSTRAINT fk_device_site FOREIGN KEY (site_id, org_id)
        REFERENCES site(id, org_id) ON DELETE SET NULL (site_id);

CREATE INDEX idx_device_org_site ON device(org_id, site_id);
