-- Bounded current-firing state derived from notification_history, maintained on ingest so the
-- dashboard active card reads O(active alerts) instead of a DISTINCT ON over the org's whole history.
CREATE TABLE notification_active (
    organization_id BIGINT       NOT NULL,
    alert_key       TEXT         NOT NULL,
    history_id      BIGINT       NOT NULL,
    received_at     TIMESTAMPTZ  NOT NULL,
    alert_name      TEXT         NOT NULL,
    severity        TEXT         NOT NULL DEFAULT '',
    rule_group      TEXT         NOT NULL DEFAULT '',
    fingerprint     TEXT         NOT NULL DEFAULT '',
    device_id       TEXT         NOT NULL DEFAULT '',
    template        TEXT         NOT NULL DEFAULT '',
    summary         TEXT         NOT NULL DEFAULT '',
    starts_at       TIMESTAMPTZ,
    ends_at         TIMESTAMPTZ,
    PRIMARY KEY (organization_id, alert_key)
);

-- Read path: an org's active alerts, most recent first.
CREATE INDEX idx_notification_active_org_recent
    ON notification_active (organization_id, received_at DESC, history_id DESC);

-- Keep notification_active in sync as alert events are appended to notification_history: a firing
-- event upserts the row, a resolved (or any non-firing) event clears it. The history_id guards apply
-- a change only when it is newer than the recorded one, tolerating out-of-order delivery. The alert
-- key falls back to alert_name + device_id so fingerprintless alerts don't collapse across devices.
CREATE OR REPLACE FUNCTION notification_active_sync()
RETURNS TRIGGER AS $$
DECLARE
    key TEXT;
BEGIN
    -- Unscoped (NULL org) alerts never surface in the per-org active card; skip them.
    IF NEW.organization_id IS NULL THEN
        RETURN NEW;
    END IF;
    key := COALESCE(NULLIF(NEW.fingerprint, ''), NEW.alert_name || chr(31) || NEW.device_id);
    IF NEW.status = 'firing' THEN
        INSERT INTO notification_active (
            organization_id, alert_key, history_id, received_at, alert_name,
            severity, rule_group, fingerprint, device_id, template, summary, starts_at, ends_at
        ) VALUES (
            NEW.organization_id, key, NEW.id, NEW.received_at, NEW.alert_name,
            NEW.severity, NEW.rule_group, NEW.fingerprint, NEW.device_id, NEW.template, NEW.summary,
            NEW.starts_at, NEW.ends_at
        )
        ON CONFLICT (organization_id, alert_key) DO UPDATE SET
            history_id  = EXCLUDED.history_id,
            received_at = EXCLUDED.received_at,
            alert_name  = EXCLUDED.alert_name,
            severity    = EXCLUDED.severity,
            rule_group  = EXCLUDED.rule_group,
            fingerprint = EXCLUDED.fingerprint,
            device_id   = EXCLUDED.device_id,
            template    = EXCLUDED.template,
            summary     = EXCLUDED.summary,
            starts_at   = EXCLUDED.starts_at,
            ends_at     = EXCLUDED.ends_at
        WHERE notification_active.history_id < EXCLUDED.history_id;
    ELSE
        DELETE FROM notification_active
        WHERE organization_id = NEW.organization_id
          AND alert_key = key
          AND history_id < NEW.id;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER notification_history_active_sync
    AFTER INSERT ON notification_history
    FOR EACH ROW
    EXECUTE FUNCTION notification_active_sync();

-- Backfill current active state from existing history: the latest firing row per alert key.
INSERT INTO notification_active (
    organization_id, alert_key, history_id, received_at, alert_name,
    severity, rule_group, fingerprint, device_id, template, summary, starts_at, ends_at
)
SELECT
    latest.organization_id,
    latest.alert_key,
    latest.id,
    latest.received_at,
    latest.alert_name,
    latest.severity,
    latest.rule_group,
    latest.fingerprint,
    latest.device_id,
    latest.template,
    latest.summary,
    latest.starts_at,
    latest.ends_at
FROM (
    SELECT DISTINCT ON (organization_id, COALESCE(NULLIF(fingerprint, ''), alert_name || chr(31) || device_id))
        organization_id,
        COALESCE(NULLIF(fingerprint, ''), alert_name || chr(31) || device_id) AS alert_key,
        id, received_at, alert_name, status, severity, rule_group, fingerprint, device_id, template,
        summary, starts_at, ends_at
    FROM notification_history
    WHERE organization_id IS NOT NULL
    ORDER BY organization_id, COALESCE(NULLIF(fingerprint, ''), alert_name || chr(31) || device_id), id DESC
) latest
WHERE latest.status = 'firing';
