ALTER TABLE curtailment_response_profile
    DROP CONSTRAINT IF EXISTS ck_curtailment_response_profile_scope_json_object;

ALTER TABLE curtailment_response_profile
    DROP COLUMN IF EXISTS scope_json;
