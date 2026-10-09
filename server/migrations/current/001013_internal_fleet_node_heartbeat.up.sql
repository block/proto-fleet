-- requires-shared: 1000

-- Heartbeats must not update the node row held by physical-dispatch leases.
CREATE TABLE fleet_node_heartbeat (
 fleet_node_id bigint CONSTRAINT pk_fleet_node_heartbeat PRIMARY KEY,
 org_id bigint NOT NULL,
 last_seen_at timestamptz NOT NULL,
 CONSTRAINT fk_fleet_node_heartbeat_node FOREIGN KEY (fleet_node_id,org_id)
  REFERENCES fleet_node(id,org_id) ON DELETE CASCADE
);
INSERT INTO fleet_node_heartbeat(fleet_node_id,org_id,last_seen_at)
 SELECT id,org_id,last_seen_at FROM fleet_node WHERE last_seen_at IS NOT NULL;

-- Keep the deployed baseline column as a migration snapshot. Active readers
-- and writers use this view/table; rollback copies current liveness back.
CREATE VIEW fleet_node_live AS
 SELECT fn.id,fn.org_id,fn.name,fn.identity_pubkey,fn.encryption_pubkey,
  fn.enrollment_status,h.last_seen_at,fn.created_at,fn.updated_at,fn.deleted_at
 FROM fleet_node fn LEFT JOIN fleet_node_heartbeat h
  ON h.fleet_node_id=fn.id AND h.org_id=fn.org_id;
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname='grafana_ha_ro') THEN
  GRANT SELECT (org_id,id,last_seen_at,enrollment_status,deleted_at)
   ON fleet_node_live TO grafana_ha_ro;
 END IF;
END $$;
