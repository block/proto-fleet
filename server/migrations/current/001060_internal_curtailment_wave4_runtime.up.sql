-- requires-shared: 1059
CREATE OR REPLACE FUNCTION request_curtailment_rig_config_reconciliation(p_org_id bigint, p_requested_by bigint, p_full boolean)
RETURNS bigint LANGUAGE plpgsql VOLATILE SECURITY INVOKER AS $$
DECLARE result bigint;
BEGIN
 INSERT INTO curtailment_rig_config_reconciliation AS reconciliation (organization_id, requested_by, full_reconcile_generation)
 VALUES (p_org_id, p_requested_by, CASE WHEN p_full THEN 1 ELSE 0 END)
 ON CONFLICT (organization_id) DO UPDATE SET requested_by = EXCLUDED.requested_by,
 desired_generation = reconciliation.desired_generation + 1,
 full_reconcile_generation = CASE WHEN p_full THEN reconciliation.desired_generation + 1 ELSE reconciliation.full_reconcile_generation END,
 retry_at = CURRENT_TIMESTAMP, last_error = NULL, delivery_attempts = 0
 RETURNING reconciliation.desired_generation INTO result;
 IF p_full THEN
  DELETE FROM curtailment_rig_config_delivery_attempt WHERE organization_id = p_org_id;
 END IF;
 RETURN result;
END;
$$;

CREATE FUNCTION request_curtailment_rig_config_on_rule_change() RETURNS trigger
LANGUAGE plpgsql VOLATILE SECURITY INVOKER AS $$
DECLARE affected_org bigint; affected_source bigint; actor bigint;
BEGIN
 IF TG_OP = 'DELETE' THEN affected_org := OLD.org_id; affected_source := OLD.mqtt_source_id;
 ELSE affected_org := NEW.org_id; affected_source := NEW.mqtt_source_id; END IF;
 SELECT service_user_id INTO actor FROM curtailment_mqtt_source_config WHERE id = affected_source AND organization_id = affected_org;
 IF actor IS NOT NULL THEN PERFORM request_curtailment_rig_config_reconciliation(affected_org, actor, TRUE); END IF;
 RETURN NULL;
END;
$$;
CREATE TRIGGER request_curtailment_rig_config_on_rule_change AFTER INSERT OR UPDATE OF enabled, mqtt_source_id, response_profile_id OR DELETE ON curtailment_automation_rule
FOR EACH ROW EXECUTE FUNCTION request_curtailment_rig_config_on_rule_change();

-- Rig fallback providers follow rule scope, which depends on device placement
-- and rack/group membership. Targets are filtered for eligibility when read.
CREATE FUNCTION request_curtailment_rig_config_for_devices(p_org_id bigint, p_device_ids bigint[])
RETURNS void LANGUAGE plpgsql VOLATILE SECURITY INVOKER AS $$
DECLARE actor bigint; generation bigint;
BEGIN
 IF COALESCE(cardinality(p_device_ids), 0) > 0 THEN
  SELECT src.service_user_id INTO actor
  FROM curtailment_mqtt_source_config src
  WHERE src.organization_id = p_org_id AND src.enabled AND src.service_user_id IS NOT NULL
    AND EXISTS (SELECT 1 FROM curtailment_automation_rule rule WHERE rule.org_id = p_org_id AND rule.mqtt_source_id = src.id AND rule.enabled)
  ORDER BY src.id LIMIT 1;
 END IF;
 -- Without an enabled rule no rig has a provider; rule changes already
 -- requested the full reconciliation that cleared them.
 IF actor IS NULL THEN RETURN; END IF;
 generation := request_curtailment_rig_config_reconciliation(p_org_id, actor, FALSE);
 DELETE FROM curtailment_rig_config_delivery_attempt WHERE organization_id = p_org_id AND device_id = ANY(p_device_ids);
 INSERT INTO curtailment_rig_config_target (organization_id, device_id, requested_generation)
 SELECT p_org_id, d.id, generation FROM device d WHERE d.org_id = p_org_id AND d.id = ANY(p_device_ids) ORDER BY d.id
 ON CONFLICT (organization_id, device_id) DO UPDATE SET requested_generation = EXCLUDED.requested_generation;
END;
$$;

CREATE FUNCTION request_curtailment_rig_config_on_membership_change() RETURNS trigger
LANGUAGE plpgsql VOLATILE SECURITY INVOKER AS $$
DECLARE affected record;
BEGIN
 IF TG_OP = 'INSERT' THEN
  FOR affected IN SELECT org_id, array_agg(DISTINCT device_id) AS device_ids FROM new_rows GROUP BY org_id LOOP
   PERFORM request_curtailment_rig_config_for_devices(affected.org_id, affected.device_ids);
  END LOOP;
 ELSIF TG_OP = 'DELETE' THEN
  FOR affected IN SELECT org_id, array_agg(DISTINCT device_id) AS device_ids FROM old_rows GROUP BY org_id LOOP
   PERFORM request_curtailment_rig_config_for_devices(affected.org_id, affected.device_ids);
  END LOOP;
 ELSE
  FOR affected IN
   SELECT org_id, array_agg(DISTINCT device_id) AS device_ids
   FROM (SELECT org_id, device_id FROM old_rows UNION SELECT org_id, device_id FROM new_rows) changed
   GROUP BY org_id
  LOOP
   PERFORM request_curtailment_rig_config_for_devices(affected.org_id, affected.device_ids);
  END LOOP;
 END IF;
 RETURN NULL;
END;
$$;
CREATE TRIGGER request_curtailment_rig_config_on_membership_insert AFTER INSERT ON device_set_membership
REFERENCING NEW TABLE AS new_rows FOR EACH STATEMENT EXECUTE FUNCTION request_curtailment_rig_config_on_membership_change();
CREATE TRIGGER request_curtailment_rig_config_on_membership_update AFTER UPDATE ON device_set_membership
REFERENCING OLD TABLE AS old_rows NEW TABLE AS new_rows FOR EACH STATEMENT EXECUTE FUNCTION request_curtailment_rig_config_on_membership_change();
CREATE TRIGGER request_curtailment_rig_config_on_membership_delete AFTER DELETE ON device_set_membership
REFERENCING OLD TABLE AS old_rows FOR EACH STATEMENT EXECUTE FUNCTION request_curtailment_rig_config_on_membership_change();

-- Bulk site/building reassignments update many devices in one statement, so
-- device placement requests delivery once per organization per statement.
-- Transition tables cannot be combined with UPDATE OF, so the join filters
-- unrelated device updates.
CREATE FUNCTION request_curtailment_rig_config_on_device_placement_change() RETURNS trigger
LANGUAGE plpgsql VOLATILE SECURITY INVOKER AS $$
DECLARE affected record;
BEGIN
 FOR affected IN
  SELECT new_rows.org_id, array_agg(DISTINCT new_rows.id) AS device_ids
  FROM new_rows JOIN old_rows ON old_rows.id = new_rows.id
  WHERE old_rows.site_id IS DISTINCT FROM new_rows.site_id
     OR old_rows.building_id IS DISTINCT FROM new_rows.building_id
  GROUP BY new_rows.org_id
 LOOP
  PERFORM request_curtailment_rig_config_for_devices(affected.org_id, affected.device_ids);
 END LOOP;
 RETURN NULL;
END;
$$;
CREATE TRIGGER request_curtailment_rig_config_on_device_placement AFTER UPDATE ON device
REFERENCING OLD TABLE AS old_rows NEW TABLE AS new_rows FOR EACH STATEMENT
EXECUTE FUNCTION request_curtailment_rig_config_on_device_placement_change();

CREATE FUNCTION request_curtailment_rig_config_on_placement_change() RETURNS trigger
LANGUAGE plpgsql VOLATILE SECURITY INVOKER AS $$
BEGIN
 IF TG_TABLE_NAME = 'device_set_rack' THEN
  PERFORM request_curtailment_rig_config_for_devices(OLD.org_id, ARRAY(
   SELECT membership.device_id FROM device_set_membership membership
   WHERE membership.org_id = OLD.org_id AND membership.device_set_id = OLD.device_set_id));
 ELSIF TG_TABLE_NAME = 'device_set' THEN
  PERFORM request_curtailment_rig_config_for_devices(OLD.org_id, ARRAY(
   SELECT membership.device_id FROM device_set_membership membership
   WHERE membership.org_id = OLD.org_id AND membership.device_set_id = OLD.id));
 ELSIF TG_TABLE_NAME = 'building' THEN
  PERFORM request_curtailment_rig_config_for_devices(OLD.org_id, ARRAY(
   SELECT d.id FROM device d WHERE d.org_id = OLD.org_id AND d.building_id = OLD.id
   UNION
   SELECT membership.device_id FROM device_set_membership membership
   JOIN device_set_rack placement ON placement.device_set_id = membership.device_set_id AND placement.org_id = membership.org_id
   WHERE membership.org_id = OLD.org_id AND placement.building_id = OLD.id));
 END IF;
 RETURN NULL;
END;
$$;
CREATE TRIGGER request_curtailment_rig_config_on_rack_placement AFTER UPDATE OF building_id ON device_set_rack
FOR EACH ROW WHEN (OLD.building_id IS DISTINCT FROM NEW.building_id)
EXECUTE FUNCTION request_curtailment_rig_config_on_placement_change();
CREATE TRIGGER request_curtailment_rig_config_on_rack_placement_delete AFTER DELETE ON device_set_rack
FOR EACH ROW EXECUTE FUNCTION request_curtailment_rig_config_on_placement_change();
CREATE TRIGGER request_curtailment_rig_config_on_device_set_delete AFTER UPDATE OF deleted_at ON device_set
FOR EACH ROW WHEN (OLD.deleted_at IS DISTINCT FROM NEW.deleted_at)
EXECUTE FUNCTION request_curtailment_rig_config_on_placement_change();
CREATE TRIGGER request_curtailment_rig_config_on_building_delete AFTER UPDATE OF deleted_at ON building
FOR EACH ROW WHEN (OLD.deleted_at IS DISTINCT FROM NEW.deleted_at)
EXECUTE FUNCTION request_curtailment_rig_config_on_placement_change();

CREATE FUNCTION guard_curtailment_mqtt_source_disable() RETURNS trigger
LANGUAGE plpgsql VOLATILE SECURITY INVOKER AS $$
BEGIN
 IF OLD.enabled AND NOT NEW.enabled AND EXISTS (
  SELECT 1 FROM curtailment_automation_rule rule
  WHERE rule.org_id = OLD.organization_id AND rule.mqtt_source_id = OLD.id
  AND curtailment_automation_rule_has_live_event(rule.id, rule.org_id)
 ) THEN
  RAISE EXCEPTION 'source has active curtailment events' USING ERRCODE = '23514', CONSTRAINT = 'ck_curtailment_mqtt_source_no_active_disable';
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER guard_curtailment_mqtt_source_disable BEFORE UPDATE OF enabled ON curtailment_mqtt_source_config
FOR EACH ROW EXECUTE FUNCTION guard_curtailment_mqtt_source_disable();

-- Rigs keep the organization-wide fallback config delivered before this schema
-- until delivery is requested, and existing rules need a rebind before they
-- grant providers again. Rebuild every organization that may hold one.
SELECT request_curtailment_rig_config_reconciliation(candidate.organization_id, candidate.requested_by, TRUE)
FROM (
 SELECT reconciliation.organization_id, reconciliation.requested_by
 FROM curtailment_rig_config_reconciliation reconciliation
 UNION ALL
 SELECT source.organization_id, source.service_user_id
 FROM (
  SELECT DISTINCT ON (src.organization_id) src.organization_id, src.service_user_id
  FROM curtailment_mqtt_source_config src
  WHERE src.service_user_id IS NOT NULL
    AND NOT EXISTS (SELECT 1 FROM curtailment_rig_config_reconciliation existing WHERE existing.organization_id = src.organization_id)
  ORDER BY src.organization_id, src.id
 ) source
) candidate;
