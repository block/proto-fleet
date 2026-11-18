ALTER TABLE mining_group_pool_handoff ADD COLUMN placement_pending BOOLEAN NOT NULL DEFAULT FALSE;
CREATE UNIQUE INDEX ux_mining_group_pool_handoff_pending_device
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
