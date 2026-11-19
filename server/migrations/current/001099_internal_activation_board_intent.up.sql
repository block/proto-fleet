-- requires-shared: 1074
CREATE TABLE activation_pool_board_intent (
 organization_id bigint NOT NULL,
 device_id bigint NOT NULL,
 hashboard_id bigint NOT NULL REFERENCES hashboard(id),
 operation_id uuid NOT NULL,
 snapshot_id varchar(36) NOT NULL,
 mac_address text NOT NULL,
 serial text NOT NULL,
 account_id bigint NOT NULL,
 ownership_revision bigint NOT NULL,
 PRIMARY KEY (organization_id,hashboard_id),
 FOREIGN KEY (organization_id,operation_id) REFERENCES mining_group_pool_handoff(organization_id,operation_id),
 FOREIGN KEY (device_id,organization_id) REFERENCES device(id,org_id),
 FOREIGN KEY (organization_id,snapshot_id) REFERENCES pool_configuration_group_snapshot(organization_id,snapshot_id)
);
CREATE INDEX idx_activation_pool_board_intent_device ON activation_pool_board_intent(device_id);

CREATE OR REPLACE FUNCTION protect_activation_pool_handoff() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE org bigint; controller bigint; kind text; target bigint;
BEGIN
 org := CASE WHEN TG_OP='INSERT' THEN NEW.organization_id ELSE OLD.organization_id END;
 controller := CASE WHEN TG_OP='INSERT' THEN NEW.device_id ELSE OLD.device_id END;
 kind := CASE WHEN TG_OP='INSERT' THEN NEW.target_kind ELSE OLD.target_kind END;
 target := CASE WHEN TG_OP='INSERT' THEN NEW.target_id ELSE OLD.target_id END;
 IF EXISTS (SELECT 1 FROM mining_group_pool_handoff h WHERE h.organization_id=org AND h.device_id=controller
   AND h.placement_pending AND (kind='device' OR NOT COALESCE((h.captured->>'PartialTransfer')::boolean,false)))
 OR EXISTS (SELECT 1 FROM activation_pool_board_intent p JOIN hashboard_installation i ON i.hashboard_id=p.hashboard_id
   WHERE p.organization_id=org AND kind='hashboard_installation' AND i.id=target) THEN
  RAISE EXCEPTION 'Pool assignment is waiting for hashboard placement recovery.';
 END IF;
 RETURN CASE WHEN TG_OP='DELETE' THEN OLD ELSE NEW END;
END;
$$;

-- Partial transfers no longer reserve ownership for the entire inventory.
-- Recovery checks the pending board's saved owner and allocation generation.
CREATE OR REPLACE FUNCTION protect_activation_hashboard_owner() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='UPDATE' AND NEW.organization_id=OLD.organization_id
  AND NEW.kind=OLD.kind AND NEW.asset_id=OLD.asset_id
  AND NEW.hashboard_id IS NOT DISTINCT FROM OLD.hashboard_id
  AND NEW.client_id IS NOT DISTINCT FROM OLD.client_id
  AND NEW.subclient_id IS NOT DISTINCT FROM OLD.subclient_id THEN RETURN NEW; END IF;
 IF EXISTS (SELECT 1 FROM mining_group_pool_handoff h
  CROSS JOIN LATERAL jsonb_array_elements(h.captured->'Inventory') b
  WHERE h.placement_pending AND NOT COALESCE((h.captured->>'PartialTransfer')::boolean,false) AND (
   (TG_OP<>'INSERT' AND OLD.kind=7 AND h.organization_id=OLD.organization_id AND (b->>'hashboard_id')::bigint=OLD.asset_id)
   OR (TG_OP<>'DELETE' AND NEW.kind=7 AND h.organization_id=NEW.organization_id AND (b->>'hashboard_id')::bigint=NEW.asset_id))) THEN
  RAISE EXCEPTION 'Hashboard ownership cannot change until the activation pool assignment transfer is complete.';
 END IF;
 RETURN CASE WHEN TG_OP='DELETE' THEN OLD ELSE NEW END;
END;
$$;
