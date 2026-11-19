-- requires-shared: 1074
DO $$
BEGIN
 LOCK TABLE activation_pool_board_intent IN ACCESS EXCLUSIVE MODE;
 IF EXISTS (SELECT 1 FROM activation_pool_board_intent) THEN
  RAISE EXCEPTION 'Cannot roll back while activation board intent is pending.';
 END IF;
END;
$$;
CREATE OR REPLACE FUNCTION protect_activation_pool_handoff() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF EXISTS (SELECT 1 FROM mining_group_pool_handoff h
  WHERE h.organization_id=CASE WHEN TG_OP='INSERT' THEN NEW.organization_id ELSE OLD.organization_id END
   AND h.device_id=CASE WHEN TG_OP='INSERT' THEN NEW.device_id ELSE OLD.device_id END AND h.placement_pending) THEN
  RAISE EXCEPTION 'Pool changes are blocked until all hashboard locations are confirmed.';
 END IF;
 RETURN CASE WHEN TG_OP='DELETE' THEN OLD ELSE NEW END;
END;
$$;
CREATE OR REPLACE FUNCTION protect_activation_hashboard_owner() RETURNS trigger LANGUAGE plpgsql AS $$
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
DROP TABLE activation_pool_board_intent;
