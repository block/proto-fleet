-- Fence writers before checking operational state, and keep refusal and DDL
-- atomic even when the caller does not provide an outer transaction.
DO $$
BEGIN
 LOCK TABLE fleet_node_ha_pair, pending_enrollment IN ACCESS EXCLUSIVE MODE;
 IF EXISTS (SELECT 1 FROM fleet_node_ha_pair) THEN
  RAISE EXCEPTION 'remove Fleet Node HA pairs before rollback';
 END IF;
 IF EXISTS (SELECT 1 FROM pending_enrollment
            WHERE standby_for_node_id IS NOT NULL
              AND status IN ('PENDING', 'AWAITING_CONFIRMATION')
              AND expires_at > now()) THEN
  RAISE EXCEPTION 'cancel pending standby enrollments before rollback';
 END IF;
 DROP TABLE fleet_node_ha_pair;
 ALTER TABLE pending_enrollment DROP COLUMN standby_for_node_id;
END;
$$;
