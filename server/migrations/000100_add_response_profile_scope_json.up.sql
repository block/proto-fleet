-- Add response profile scope_json after main introduced migration 000099.
ALTER TABLE curtailment_response_profile
    ADD COLUMN scope_json JSONB NOT NULL DEFAULT '{}'::jsonb;

UPDATE curtailment_response_profile
SET scope_json = CASE
    WHEN site_id IS NULL THEN '{}'::jsonb
    ELSE jsonb_build_object('site_ids', jsonb_build_array(site_id))
END;

ALTER TABLE curtailment_response_profile
    ADD CONSTRAINT ck_curtailment_response_profile_scope_json_object
    CHECK (jsonb_typeof(scope_json) = 'object');
