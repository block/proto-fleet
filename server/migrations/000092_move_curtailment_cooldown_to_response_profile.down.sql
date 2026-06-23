ALTER TABLE curtailment_org_config
    ADD COLUMN post_event_cooldown_sec INT NOT NULL DEFAULT 600;

ALTER TABLE curtailment_org_config
    ADD CONSTRAINT ck_curtailment_org_config_cooldown_nonneg
        CHECK (post_event_cooldown_sec >= 0);

DROP INDEX IF EXISTS idx_curtailment_target_terminal_by_device;

DROP INDEX IF EXISTS idx_curtailment_target_terminal_by_event;

DROP INDEX IF EXISTS idx_curtailment_event_org_recent_end;

ALTER TABLE curtailment_response_profile
    DROP CONSTRAINT IF EXISTS ck_curtailment_response_profile_post_event_cooldown;

ALTER TABLE curtailment_response_profile
    DROP COLUMN post_event_cooldown_sec;
