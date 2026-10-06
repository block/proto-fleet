-- Remove response profile scope_json after main introduced migration 000099.
ALTER TABLE curtailment_response_profile
    DROP CONSTRAINT IF EXISTS ck_curtailment_response_profile_scope_json_object;

UPDATE curtailment_response_profile
SET site_id = (scope_json->>'site_id')::BIGINT
WHERE site_id IS NULL
    AND scope_json ? 'site_id';

UPDATE curtailment_response_profile
SET site_id = (scope_json->'site_ids'->>0)::BIGINT
WHERE site_id IS NULL
    AND jsonb_typeof(scope_json->'site_ids') = 'array'
    AND jsonb_array_length(scope_json->'site_ids') = 1
    AND NOT (scope_json ? 'device_identifiers')
    AND NOT (scope_json ? 'device_set_ids');

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM curtailment_response_profile
        WHERE scope_json IS NOT NULL
            AND scope_json <> '{}'::jsonb
            AND NOT COALESCE((scope_json->>'whole_org')::BOOLEAN, FALSE)
            AND NOT (
                scope_json ? 'site_id'
                AND NOT (scope_json ? 'site_ids')
                AND NOT (scope_json ? 'device_identifiers')
                AND NOT (scope_json ? 'device_set_ids')
            )
            AND NOT (
                jsonb_typeof(scope_json->'site_ids') = 'array'
                AND jsonb_array_length(scope_json->'site_ids') = 1
                AND NOT (scope_json ? 'site_id')
                AND NOT (scope_json ? 'device_identifiers')
                AND NOT (scope_json ? 'device_set_ids')
            )
    ) THEN
        RAISE EXCEPTION 'cannot drop curtailment_response_profile.scope_json while non-legacy response profile scopes remain';
    END IF;
END $$;

ALTER TABLE curtailment_response_profile
    DROP COLUMN IF EXISTS scope_json;
