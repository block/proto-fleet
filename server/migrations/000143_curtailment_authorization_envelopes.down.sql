ALTER TABLE curtailment_event
    DROP COLUMN authorization_envelope_jsonb;

ALTER TABLE curtailment_response_profile
    DROP COLUMN authorization_envelope_jsonb;
