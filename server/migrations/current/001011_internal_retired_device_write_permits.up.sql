-- requires-shared: 1000

-- Retained physical identity prevents a new device row from erasing an old
-- running or uncertain operation. Keys contain routing/identity data, no secrets.
CREATE FUNCTION device_write_identity_keys(target_id bigint) RETURNS text[]
LANGUAGE sql STABLE AS $$
 SELECT ARRAY_REMOVE(ARRAY[
  CASE WHEN NULLIF(btrim(d.serial_number),'') IS NOT NULL THEN 'serial:' || lower(btrim(d.serial_number)) END,
  CASE WHEN NULLIF(regexp_replace(lower(d.mac_address),'[^0-9a-f]','','g'),'') IS NOT NULL THEN 'mac:' || regexp_replace(lower(d.mac_address),'[^0-9a-f]','','g') END,
  CASE WHEN NULLIF(dd.ip_address,'') IS NOT NULL THEN 'endpoint:' || COALESCE(fnd.fleet_node_id::text,'local') || ':' || lower(dd.ip_address) || ':' || dd.port END
 ],NULL)
 FROM device d JOIN discovered_device dd ON dd.id=d.discovered_device_id
 LEFT JOIN fleet_node_device fnd ON fnd.device_id=d.id AND fnd.org_id=d.org_id
 WHERE d.id=target_id
$$;
ALTER TABLE device_write_attempt ADD COLUMN identity_keys text[] NOT NULL DEFAULT '{}';
UPDATE device_write_attempt SET identity_keys=COALESCE(device_write_identity_keys(device_id),'{}');
CREATE TABLE device_deletion_unpair_permit (
 device_id bigint PRIMARY KEY,
 organization_id bigint NOT NULL,
 epoch bigint NOT NULL CHECK(epoch>0),
 binding_hash text NOT NULL,
 identity_keys text[] NOT NULL,
 attempt_id uuid REFERENCES device_write_attempt(attempt_id),
 created_at timestamptz NOT NULL DEFAULT statement_timestamp(),
 CONSTRAINT uq_device_deletion_unpair_permit_attempt_id UNIQUE (attempt_id),
 FOREIGN KEY(device_id,organization_id) REFERENCES device_write_fence(device_id,organization_id)
);
