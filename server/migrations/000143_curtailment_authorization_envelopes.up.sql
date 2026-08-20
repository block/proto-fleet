-- Intentionally no DEFAULT: rollout requires proving there are no pre-contract
-- profile or event rows before this migration is applied. Existing rows make
-- the migration fail before either table changes instead of acquiring an
-- unsafe synthetic scope.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM curtailment_response_profile LIMIT 1)
        OR EXISTS (SELECT 1 FROM curtailment_event LIMIT 1) THEN
        RAISE EXCEPTION 'curtailment authorization envelopes require empty response-profile and event tables';
    END IF;
END
$$;

ALTER TABLE curtailment_response_profile
    ADD COLUMN authorization_envelope_jsonb JSONB NOT NULL,
    ADD CONSTRAINT curtailment_response_profile_authorization_envelope_shape CHECK (
        jsonb_typeof(authorization_envelope_jsonb) = 'object'
        AND authorization_envelope_jsonb ?& ARRAY[
            'schema_version',
            'selected_resource_site_ids',
            'current_member_site_ids',
            'miner_scope_unbounded',
            'facility_fan_site_ids',
            'facility_fan_scope_unbounded'
        ]
        AND jsonb_typeof(authorization_envelope_jsonb->'schema_version') = 'number'
        AND authorization_envelope_jsonb->>'schema_version' = '1'
        AND jsonb_typeof(authorization_envelope_jsonb->'selected_resource_site_ids') = 'array'
        AND jsonb_typeof(authorization_envelope_jsonb->'current_member_site_ids') = 'array'
        AND jsonb_typeof(authorization_envelope_jsonb->'miner_scope_unbounded') = 'boolean'
        AND jsonb_typeof(authorization_envelope_jsonb->'facility_fan_site_ids') = 'array'
        AND jsonb_typeof(authorization_envelope_jsonb->'facility_fan_scope_unbounded') = 'boolean'
    );

ALTER TABLE curtailment_event
    ADD COLUMN authorization_envelope_jsonb JSONB NOT NULL,
    ADD CONSTRAINT curtailment_event_authorization_envelope_shape CHECK (
        jsonb_typeof(authorization_envelope_jsonb) = 'object'
        AND authorization_envelope_jsonb ?& ARRAY[
            'schema_version',
            'selected_resource_site_ids',
            'current_member_site_ids',
            'miner_scope_unbounded',
            'facility_fan_site_ids',
            'facility_fan_scope_unbounded'
        ]
        AND jsonb_typeof(authorization_envelope_jsonb->'schema_version') = 'number'
        AND authorization_envelope_jsonb->>'schema_version' = '1'
        AND jsonb_typeof(authorization_envelope_jsonb->'selected_resource_site_ids') = 'array'
        AND jsonb_typeof(authorization_envelope_jsonb->'current_member_site_ids') = 'array'
        AND jsonb_typeof(authorization_envelope_jsonb->'miner_scope_unbounded') = 'boolean'
        AND jsonb_typeof(authorization_envelope_jsonb->'facility_fan_site_ids') = 'array'
        AND jsonb_typeof(authorization_envelope_jsonb->'facility_fan_scope_unbounded') = 'boolean'
    );
