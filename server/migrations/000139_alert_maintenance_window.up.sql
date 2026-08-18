-- Maintenance windows mute alert delivery for a bounded period: alerts from rule_uids (empty = every
-- rule) stop delivering to channel_ids (empty = every channel) while now() is inside the window.
-- Alert history still records the muted alerts. The id arrays carry no foreign keys on purpose: the
-- muted-target intent outlives rule/channel deletion, and a dangling id simply mutes nothing.
CREATE TABLE alert_maintenance_window (
    id          BIGSERIAL PRIMARY KEY,
    org_id      BIGINT NOT NULL,
    rule_uids   TEXT[] NOT NULL DEFAULT '{}',
    channel_ids BIGINT[] NOT NULL DEFAULT '{}',
    starts_at   TIMESTAMPTZ NOT NULL,
    ends_at     TIMESTAMPTZ NOT NULL,
    comment     TEXT NOT NULL DEFAULT '',
    created_by  TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_alert_maintenance_window_org ON alert_maintenance_window (org_id);
