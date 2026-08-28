-- Only live events can be replayed by the older binary. Preserve provenance on
-- terminal history even when rolling the executable schema back.
UPDATE curtailment_event
SET decision_snapshot_jsonb = decision_snapshot_jsonb
    - 'response_profile_id'
    - 'response_profile_revision'
WHERE source_actor_type = 'automation'
  AND external_source = 'curtailment_automation'
  AND state IN ('pending', 'active', 'restoring');

DROP VIEW curtailment_response_profile_with_revision;

DROP TRIGGER bind_legacy_curtailment_automation_event_revision
    ON curtailment_event;
DROP FUNCTION bind_legacy_curtailment_automation_event_revision();

DROP TRIGGER sync_curtailment_automation_rule_profile_revision
    ON curtailment_automation_rule;
DROP FUNCTION sync_curtailment_automation_rule_profile_revision();

DROP TRIGGER sync_curtailment_response_profile_revision
    ON curtailment_response_profile;
DROP FUNCTION sync_curtailment_response_profile_revision();

DROP TRIGGER canonicalize_curtailment_response_profile_scope
    ON curtailment_response_profile;
DROP FUNCTION canonicalize_curtailment_response_profile_scope();

DROP TABLE curtailment_automation_rule_profile_revision;
DROP TABLE curtailment_response_profile_revision;
