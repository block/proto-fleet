UPDATE fleet_node fn SET last_seen_at=h.last_seen_at
 FROM fleet_node_heartbeat h WHERE h.fleet_node_id=fn.id AND h.org_id=fn.org_id;
DROP VIEW fleet_node_live;
DROP TABLE fleet_node_heartbeat;
