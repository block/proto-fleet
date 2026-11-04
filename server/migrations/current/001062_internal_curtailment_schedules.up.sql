-- requires-shared: 1061

-- Schedule rules have no MQTT source; keep them visible to every rule read.
DROP VIEW curtailment_automation_rule_binding;
CREATE VIEW curtailment_automation_rule_binding WITH (security_invoker = true) AS
SELECT r.*, rule_revision.response_profile_revision,
       COALESCE(src.source_name, '') AS mqtt_source_name,
       profile.profile_name AS response_profile_name,
       profile.site_id AS response_profile_site_id,
       profile.scope_json AS response_profile_scope_json
FROM curtailment_automation_rule r
JOIN curtailment_automation_rule_profile_revision rule_revision
    ON rule_revision.automation_rule_id = r.id
LEFT JOIN curtailment_mqtt_source_config src
    ON src.id = r.mqtt_source_id
    AND src.organization_id = r.org_id
JOIN curtailment_response_profile profile
    ON profile.id = r.response_profile_id
    AND profile.org_id = r.org_id;
