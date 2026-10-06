-- Org alert destinations. Delivery moved out of Grafana into fleet-api, so the
-- Slack/webhook secret (URL, bearer) lives here encrypted with the service master key.
CREATE TABLE alert_channel (
    id                BIGSERIAL PRIMARY KEY,
    org_id            BIGINT NOT NULL,
    name              TEXT   NOT NULL,
    kind              TEXT   NOT NULL,           -- 'slack' | 'webhook'
    encrypted_config  TEXT   NOT NULL,           -- AES-GCM ciphertext of JSON {url, bearer}
    validation_state  TEXT   NOT NULL DEFAULT 'pending',
    validated_at      TIMESTAMPTZ NULL,
    validation_error  TEXT   NOT NULL DEFAULT '',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at        TIMESTAMPTZ NULL
);

-- Channel names are unique per org among live rows; a soft-deleted name can be reused.
CREATE UNIQUE INDEX uq_alert_channel_org_name
    ON alert_channel (org_id, name) WHERE deleted_at IS NULL;

CREATE INDEX idx_alert_channel_org ON alert_channel (org_id) WHERE deleted_at IS NULL;
