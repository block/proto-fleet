ALTER TABLE curtailment_automation_rule
    DROP COLUMN response_profile_revision;

ALTER TABLE curtailment_response_profile
    DROP COLUMN revision;
