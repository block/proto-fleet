ALTER TABLE curtailment_response_profile
    ADD COLUMN post_event_cooldown_sec INT NOT NULL DEFAULT 0;

ALTER TABLE curtailment_response_profile
    ADD CONSTRAINT ck_curtailment_response_profile_post_event_cooldown
        CHECK (post_event_cooldown_sec >= 0 AND post_event_cooldown_sec <= 86400);

CREATE INDEX idx_curtailment_event_org_recent_end
    ON curtailment_event (org_id, ended_at DESC, id)
    WHERE ended_at IS NOT NULL;

CREATE INDEX idx_curtailment_target_terminal_by_event
    ON curtailment_target (curtailment_event_id, device_identifier)
    WHERE state IN ('resolved', 'restore_failed');

CREATE INDEX idx_curtailment_target_terminal_by_device
    ON curtailment_target (device_identifier, curtailment_event_id)
    WHERE state IN ('resolved', 'restore_failed');

ALTER TABLE curtailment_org_config
    DROP CONSTRAINT IF EXISTS ck_curtailment_org_config_cooldown_nonneg;

ALTER TABLE curtailment_org_config
    DROP COLUMN post_event_cooldown_sec;
