UPDATE curtailment_event
SET decision_snapshot_jsonb = decision_snapshot_jsonb
    - 'response_profile_id'
    - 'response_profile_revision'
WHERE source_actor_type = 'automation'
  AND external_source = 'curtailment_automation';

ALTER TABLE curtailment_automation_rule
    DROP COLUMN response_profile_revision;

ALTER TABLE curtailment_response_profile
    DROP COLUMN revision;
