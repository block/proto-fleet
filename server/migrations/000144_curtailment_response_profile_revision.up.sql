ALTER TABLE curtailment_response_profile
    ADD COLUMN revision UUID NOT NULL DEFAULT gen_random_uuid();

ALTER TABLE curtailment_automation_rule
    ADD COLUMN response_profile_revision UUID;

UPDATE curtailment_automation_rule AS rule
SET response_profile_revision = profile.revision
FROM curtailment_response_profile AS profile
WHERE profile.id = rule.response_profile_id
  AND profile.org_id = rule.org_id;

ALTER TABLE curtailment_automation_rule
    ALTER COLUMN response_profile_revision SET NOT NULL;
