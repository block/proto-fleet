-- Per-rule delivery routing: no row = default (all org channels), 'custom' = only the policy's channels, 'none' = in-app history only.
CREATE TABLE alert_route_policy (
    id         BIGSERIAL PRIMARY KEY,
    org_id     BIGINT NOT NULL,
    rule_uid   TEXT   NOT NULL,
    mode       TEXT   NOT NULL,               -- 'custom' | 'none'
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_alert_route_policy_org_rule UNIQUE (org_id, rule_uid)
);

-- Channels a 'custom' policy delivers to; alert_channel rows are soft-deleted, so reads must filter on the channel's deleted_at.
CREATE TABLE alert_route_channel (
    policy_id  BIGINT NOT NULL REFERENCES alert_route_policy(id) ON DELETE CASCADE,
    channel_id BIGINT NOT NULL REFERENCES alert_channel(id) ON DELETE CASCADE,
    PRIMARY KEY (policy_id, channel_id)
);

CREATE INDEX idx_alert_route_channel_channel ON alert_route_channel (channel_id);
