-- requires-shared: 1074
CREATE OR REPLACE FUNCTION ct1_hashboard_allocation_ready(target_org bigint, controller bigint)
RETURNS boolean LANGUAGE sql VOLATILE AS $$
 SELECT EXISTS (
  SELECT 1 FROM container_mining_group_state s
  JOIN device_write_fence f ON f.device_id=s.device_id AND f.organization_id=s.organization_id
  WHERE s.organization_id=target_org AND s.device_id=controller
   AND f.state='host' AND s.status NOT IN ('pending','applying','blocked')
   AND s.observed_at>=f.updated_at
   AND s.observed_at>clock_timestamp()-interval '2 minutes' AND s.observed_at<=clock_timestamp()+interval '5 seconds'
   AND COALESCE((s.observed->>'revision')::bigint,0)>0
   AND s.observed @> '{"supported":true,"enabled":true,"suspended":false,"pending":false}'::jsonb
   AND NOT EXISTS (SELECT 1 FROM device_write_attempt a WHERE a.device_id=controller
    AND a.organization_id=target_org AND a.operation_kind='mutation' AND a.status IN ('running','unknown'))
 );
$$;

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
