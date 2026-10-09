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

-- Old snapshots did not capture Fleet Node routing scope. Today's association
-- cannot establish the route used by an old operation. Outstanding attempts
-- require a captured stable identity, or an explicitly captured endpoint scope.
DO $$ BEGIN
 IF EXISTS (
  SELECT 1 FROM device_write_attempt a WHERE a.status NOT IN ('complete','resolved')
  AND cardinality(device_write_identity_keys_values(a.organization_id,
   a.identity_snapshot->>'serial_number',a.identity_snapshot->>'mac_address',
   a.identity_snapshot->>'routing_scope',a.identity_snapshot->>'ip_address',a.identity_snapshot->>'port'))=0
 ) THEN
  RAISE EXCEPTION 'outstanding device write lacks captured identity; resolve with audited operator recovery before migration 1011';
 END IF;
END $$;
ALTER TABLE device_write_attempt ADD COLUMN identity_keys text[] NOT NULL DEFAULT '{}';
-- Present snapshot values (including blank values) are authoritative. Only
-- completed/resolved history may fall back to current values for missing fields.
UPDATE device_write_attempt a SET identity_keys=device_write_identity_keys_values(a.organization_id,
 CASE WHEN a.status NOT IN ('complete','resolved') OR a.identity_snapshot ? 'serial_number' THEN a.identity_snapshot->>'serial_number' ELSE d.serial_number END,
 CASE WHEN a.status NOT IN ('complete','resolved') OR a.identity_snapshot ? 'mac_address' THEN a.identity_snapshot->>'mac_address' ELSE d.mac_address END,
 a.identity_snapshot->>'routing_scope',a.identity_snapshot->>'ip_address',a.identity_snapshot->>'port')
FROM device d WHERE d.id=a.device_id AND d.org_id=a.organization_id;
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
