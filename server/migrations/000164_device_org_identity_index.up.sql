-- Build without blocking device writes; the next migration attaches it.
CREATE UNIQUE INDEX CONCURRENTLY uq_device_id_org_idx ON device(id, org_id);
