-- requires-shared: 1000

-- Reported serial/MAC identities are trusted only within their organization.
-- Endpoints additionally belong to the authenticated Fleet Node routing scope.
CREATE FUNCTION device_write_identity_keys_values(org_id bigint, serial text, mac text, routing_scope text, ip text, port text) RETURNS text[]
LANGUAGE sql IMMUTABLE AS $$
 SELECT ARRAY_REMOVE(ARRAY[
  CASE WHEN NULLIF(btrim(serial),'') IS NOT NULL THEN 'org:' || org_id || ':serial:' || lower(btrim(serial)) END,
  CASE WHEN length(regexp_replace(lower(mac),'[^0-9a-f]','','g'))=12 THEN 'org:' || org_id || ':mac:' || regexp_replace(lower(mac),'[^0-9a-f]','','g') END,
  CASE WHEN NULLIF(routing_scope,'') IS NOT NULL AND NULLIF(ip,'') IS NOT NULL AND NULLIF(port,'') IS NOT NULL THEN 'org:' || org_id || ':endpoint:' || routing_scope || ':' || lower(ip) || ':' || port END
 ],NULL)
$$;
CREATE FUNCTION device_write_identity_keys(target_id bigint) RETURNS text[]
LANGUAGE sql STABLE AS $$
 SELECT device_write_identity_keys_values(d.org_id,d.serial_number,d.mac_address,COALESCE(fnd.fleet_node_id::text,'local'),dd.ip_address,dd.port)
 FROM device d JOIN discovered_device dd ON dd.id=d.discovered_device_id
 LEFT JOIN fleet_node_device fnd ON fnd.device_id=d.id AND fnd.org_id=d.org_id
 WHERE d.id=target_id
$$;

-- Hosted ships as one feature. Earlier schema checkpoints are migration steps,
-- not supported runtimes that can create attempts before this final schema.
LOCK TABLE device_write_attempt IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM device_write_attempt) THEN
  RAISE EXCEPTION 'device write history exists; intermediate hosted schema upgrades are unsupported';
 END IF;
END $$;
ALTER TABLE device_write_attempt ADD COLUMN identity_keys text[] NOT NULL DEFAULT '{}';
CREATE INDEX idx_device_write_attempt_outstanding_identity_keys
 ON device_write_attempt USING gin(identity_keys) WHERE status NOT IN ('complete','resolved');
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
