-- requires-shared: 1000

-- Keep state outside the view: the restore guard explicitly locks st.
CREATE VIEW curtailment_automation_rule_binding WITH (security_invoker = true) AS
SELECT r.*, rule_revision.response_profile_revision,
       src.source_name AS mqtt_source_name,
       profile.profile_name AS response_profile_name,
       profile.site_id AS response_profile_site_id,
       profile.scope_json AS response_profile_scope_json
FROM curtailment_automation_rule r
JOIN curtailment_automation_rule_profile_revision rule_revision
    ON rule_revision.automation_rule_id = r.id
JOIN curtailment_mqtt_source_config src
    ON src.id = r.mqtt_source_id
    AND src.organization_id = r.org_id
JOIN curtailment_response_profile profile
    ON profile.id = r.response_profile_id
    AND profile.org_id = r.org_id;

-- STABLE keeps the live-event test on the calling statement's snapshot.
CREATE FUNCTION curtailment_automation_rule_has_live_event(p_rule_id bigint, p_org_id bigint)
RETURNS boolean LANGUAGE sql STABLE SECURITY INVOKER AS $$
    SELECT EXISTS (
        SELECT 1 FROM curtailment_event e
        WHERE e.state IN ('pending', 'active', 'restoring')
          AND (
              EXISTS (
                  SELECT 1 FROM curtailment_automation_rule_state st
                  WHERE st.rule_id = p_rule_id AND st.active_event_uuid = e.event_uuid
              )
              OR (e.org_id = p_org_id
                  AND e.external_source = 'curtailment_automation'
                  AND e.external_reference = p_rule_id::text)
          )
    );
$$;

-- Each caller consumes the returned generation in the same statement. The
-- upsert keeps the existing organization lock order and full/targeted semantics.
CREATE FUNCTION request_curtailment_rig_config_reconciliation(p_org_id bigint, p_requested_by bigint, p_full boolean)
RETURNS bigint
LANGUAGE sql VOLATILE SECURITY INVOKER AS $$
    INSERT INTO curtailment_rig_config_reconciliation AS reconciliation (
        organization_id, requested_by, full_reconcile_generation
    ) VALUES (p_org_id, p_requested_by, CASE WHEN p_full THEN 1 ELSE 0 END)
    ON CONFLICT (organization_id) DO UPDATE
    SET requested_by = EXCLUDED.requested_by,
        desired_generation = reconciliation.desired_generation + 1,
        full_reconcile_generation = CASE WHEN p_full
            THEN reconciliation.desired_generation + 1
            ELSE reconciliation.full_reconcile_generation END,
        retry_at = CURRENT_TIMESTAMP,
        last_error = NULL
    RETURNING reconciliation.desired_generation;
$$;
