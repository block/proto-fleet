-- requires-shared: 1074
ALTER TABLE mining_group_pool_handoff ADD COLUMN placement_pending BOOLEAN NOT NULL DEFAULT FALSE;
CREATE UNIQUE INDEX idx_mining_group_pool_handoff_pending_device
 ON mining_group_pool_handoff(device_id) WHERE placement_pending;

-- Retained container intent must not be edited or retired before every board
-- has received its assignment. The handoff clears its fence in the same transaction.
CREATE FUNCTION protect_activation_pool_handoff() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF EXISTS (SELECT 1 FROM mining_group_pool_handoff h
  WHERE h.organization_id=CASE WHEN TG_OP='INSERT' THEN NEW.organization_id ELSE OLD.organization_id END
   AND h.device_id=CASE WHEN TG_OP='INSERT' THEN NEW.device_id ELSE OLD.device_id END AND h.placement_pending) THEN
  RAISE EXCEPTION 'Pool changes are blocked until all hashboard locations are confirmed.';
 END IF;
 RETURN CASE WHEN TG_OP='DELETE' THEN OLD ELSE NEW END;
END;
$$;
CREATE TRIGGER protect_activation_pool_handoff BEFORE INSERT OR UPDATE OR DELETE ON pool_configuration_assignment
 FOR EACH ROW EXECUTE FUNCTION protect_activation_pool_handoff();

CREATE FUNCTION protect_activation_hashboard_owner() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 -- A controller move changes placement and its revision, not ownership.
 -- Identity changes still require the fence, even when account fields match.
 IF TG_OP='UPDATE' AND NEW.organization_id=OLD.organization_id
  AND NEW.kind=OLD.kind AND NEW.asset_id=OLD.asset_id
  AND NEW.hashboard_id IS NOT DISTINCT FROM OLD.hashboard_id
  AND NEW.client_id IS NOT DISTINCT FROM OLD.client_id
  AND NEW.subclient_id IS NOT DISTINCT FROM OLD.subclient_id THEN
  RETURN NEW;
 END IF;
 IF EXISTS (SELECT 1 FROM mining_group_pool_handoff h
  CROSS JOIN LATERAL jsonb_array_elements(h.captured->'Inventory') b
  WHERE h.placement_pending AND (
   (TG_OP<>'INSERT' AND OLD.kind=7 AND h.organization_id=OLD.organization_id AND (b->>'hashboard_id')::bigint=OLD.asset_id)
   OR (TG_OP<>'DELETE' AND NEW.kind=7 AND h.organization_id=NEW.organization_id AND (b->>'hashboard_id')::bigint=NEW.asset_id))) THEN
  RAISE EXCEPTION 'Hashboard ownership cannot change until the activation pool assignment transfer is complete.';
 END IF;
 RETURN CASE WHEN TG_OP='DELETE' THEN OLD ELSE NEW END;
END;
$$;
CREATE TRIGGER protect_activation_hashboard_owner BEFORE INSERT OR UPDATE OR DELETE ON hosted_account_asset
 FOR EACH ROW EXECUTE FUNCTION protect_activation_hashboard_owner();

CREATE OR REPLACE FUNCTION retire_reallocated_pool_assignments(organization bigint, controller bigint, asset_kind integer, asset bigint)
RETURNS void LANGUAGE SQL AS $$
 WITH retired AS (
  UPDATE pool_configuration_assignment a SET retired_at=CURRENT_TIMESTAMP,
   version=a.version+1,updated_at=CURRENT_TIMESTAMP
  WHERE a.organization_id=organization AND a.retired_at IS NULL
   -- A placement move must not discard retained activation intent. The board
   -- observation still advances; normal identity checks stop automatic handoff.
   AND NOT (a.target_kind='device' AND EXISTS (
    SELECT 1 FROM mining_group_pool_handoff h
    WHERE h.organization_id=a.organization_id AND h.device_id=a.device_id AND h.placement_pending))
   AND ((a.target_kind='device' AND a.device_id=controller) OR
    (a.target_kind='hashboard_installation' AND EXISTS (
     SELECT 1 FROM hashboard_installation i WHERE i.id=a.target_id
      AND ((asset_kind=6 AND i.module_id=asset) OR (asset_kind=7 AND i.hashboard_id=asset)))))
  RETURNING a.organization_id,a.target_kind,a.target_id
 )
 UPDATE pool_configuration_operation_target t SET state='superseded',updated_at=CURRENT_TIMESTAMP
 FROM retired a WHERE t.organization_id=a.organization_id AND t.target_kind=a.target_kind AND t.target_id=a.target_id
  AND t.state IN ('pending','applying','failed','different','applied_unverified');
$$;
