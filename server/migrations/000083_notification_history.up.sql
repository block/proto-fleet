-- Notification history populated by the Grafana alertmanager webhook receiver.

CREATE TABLE notification_history (
    id              BIGSERIAL    PRIMARY KEY,
    received_at     TIMESTAMPTZ  NOT NULL DEFAULT now(),
    alert_name      TEXT         NOT NULL,
    status          TEXT         NOT NULL,
    severity        TEXT         NOT NULL DEFAULT '',
    rule_group      TEXT         NOT NULL DEFAULT '',
    fingerprint     TEXT         NOT NULL DEFAULT '',
    organization_id BIGINT,
    device_id       TEXT         NOT NULL DEFAULT '',
    template        TEXT         NOT NULL DEFAULT '',
    summary         TEXT         NOT NULL DEFAULT '',
    starts_at       TIMESTAMPTZ,
    ends_at         TIMESTAMPTZ,
    labels          JSONB        NOT NULL DEFAULT '{}'::jsonb,
    annotations     JSONB        NOT NULL DEFAULT '{}'::jsonb
);

CREATE INDEX idx_notification_history_org_received
    ON notification_history (organization_id, received_at DESC);

CREATE INDEX idx_notification_history_received
    ON notification_history (received_at DESC);

CREATE INDEX idx_notification_history_fingerprint
    ON notification_history (fingerprint)
    WHERE fingerprint <> '';
