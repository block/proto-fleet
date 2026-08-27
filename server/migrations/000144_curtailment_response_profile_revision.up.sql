DO $migration$
BEGIN
    IF EXISTS (SELECT 1 FROM curtailment_response_profile)
        OR EXISTS (SELECT 1 FROM curtailment_automation_rule)
        OR EXISTS (SELECT 1 FROM curtailment_event)
    THEN
        RAISE EXCEPTION
            'response profile revision rollout requires zero pre-contract profiles, automation rules, and events';
    END IF;
END
$migration$;

ALTER TABLE curtailment_response_profile
    ADD COLUMN revision UUID NOT NULL DEFAULT gen_random_uuid();

ALTER TABLE curtailment_automation_rule
    ADD COLUMN response_profile_revision UUID NOT NULL;
