DROP TRIGGER request_curtailment_rig_config_on_building_delete ON building;
DROP TRIGGER request_curtailment_rig_config_on_device_set_delete ON device_set;
DROP TRIGGER request_curtailment_rig_config_on_rack_placement_delete ON device_set_rack;
DROP TRIGGER request_curtailment_rig_config_on_rack_placement ON device_set_rack;
DROP FUNCTION request_curtailment_rig_config_on_placement_change();
DROP TRIGGER request_curtailment_rig_config_on_device_placement ON device;
DROP FUNCTION request_curtailment_rig_config_on_device_placement_change();
DROP TRIGGER request_curtailment_rig_config_on_membership_delete ON device_set_membership;
DROP TRIGGER request_curtailment_rig_config_on_membership_update ON device_set_membership;
DROP TRIGGER request_curtailment_rig_config_on_membership_insert ON device_set_membership;
DROP FUNCTION request_curtailment_rig_config_on_membership_change();
DROP FUNCTION request_curtailment_rig_config_for_devices(bigint, bigint[]);
DROP TRIGGER guard_curtailment_mqtt_source_disable ON curtailment_mqtt_source_config;
DROP FUNCTION guard_curtailment_mqtt_source_disable();
DROP TRIGGER request_curtailment_rig_config_on_rule_change ON curtailment_automation_rule;
DROP FUNCTION request_curtailment_rig_config_on_rule_change();
CREATE OR REPLACE FUNCTION request_curtailment_rig_config_reconciliation(p_org_id bigint, p_requested_by bigint, p_full boolean)
RETURNS bigint LANGUAGE sql VOLATILE SECURITY INVOKER AS $$
 INSERT INTO curtailment_rig_config_reconciliation AS reconciliation (organization_id, requested_by, full_reconcile_generation)
 VALUES (p_org_id, p_requested_by, CASE WHEN p_full THEN 1 ELSE 0 END)
 ON CONFLICT (organization_id) DO UPDATE SET requested_by = EXCLUDED.requested_by,
 desired_generation = reconciliation.desired_generation + 1,
 full_reconcile_generation = CASE WHEN p_full THEN reconciliation.desired_generation + 1 ELSE reconciliation.full_reconcile_generation END,
 retry_at = CURRENT_TIMESTAMP, last_error = NULL
 RETURNING reconciliation.desired_generation;
$$;
