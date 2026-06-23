-- Latest-state-per-alert derived from notification_history, maintained on ingest so the dashboard
-- active card reads O(active alerts) (via the partial index below) instead of a DISTINCT ON over the
-- org's whole history. Resolved alerts are kept as tombstones rather than deleted so a later-arriving
-- stale event (an out-of-order firing retry, a delayed resolve) can be ordered against the recorded
-- lifecycle time instead of silently re-creating an active row.
CREATE TABLE notification_active (
    organization_id BIGINT       NOT NULL,
    alert_key       TEXT         NOT NULL,
    history_id      BIGINT       NOT NULL,
    received_at     TIMESTAMPTZ  NOT NULL,
    status          TEXT         NOT NULL,
    -- Alertmanager lifecycle time used to order events: starts_at for firing, ends_at for resolved
    -- (received_at as a fallback when the source timestamp is absent).
    event_at        TIMESTAMPTZ  NOT NULL,
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

-- Read path: an org's currently-firing alerts, most recent first. Partial on status so resolved
-- tombstones don't bloat the scan the dashboard poll runs.
CREATE INDEX idx_notification_active_org_recent
    ON notification_active (organization_id, received_at DESC, history_id DESC)
    WHERE status = 'firing';

-- Keep notification_active in sync as alert events are appended to notification_history: every event
-- upserts the row for its alert key, recording the new status. The event_at guard applies a change
-- only when the incoming event is more recent by Alertmanager lifecycle time (history_id breaks ties),
-- so an out-of-order firing retry can't reopen an already-resolved alert and a stale resolve can't clear
-- a newer firing. The alert key falls back to alert_name + device_id so fingerprintless alerts don't
-- collapse across devices, then is hashed (md5, for bounded keying not security) so a webhook-controlled
-- label can't push the primary key past the btree index tuple limit.
CREATE OR REPLACE FUNCTION notification_active_sync()
RETURNS TRIGGER AS $$
DECLARE
    key TEXT;
    ev  TIMESTAMPTZ;
BEGIN
    -- Unscoped (NULL org) alerts never surface in the per-org active card; skip them.
    IF NEW.organization_id IS NULL THEN
        RETURN NEW;
    END IF;
    key := md5(COALESCE(NULLIF(NEW.fingerprint, ''), NEW.alert_name || chr(31) || NEW.device_id));
    ev := CASE
              WHEN NEW.status = 'firing' THEN COALESCE(NEW.starts_at, NEW.received_at)
              ELSE COALESCE(NEW.ends_at, NEW.received_at)
          END;
    INSERT INTO notification_active (
        organization_id, alert_key, history_id, received_at, status, event_at, alert_name,
        severity, rule_group, fingerprint, device_id, template, summary, starts_at, ends_at
    ) VALUES (
        NEW.organization_id, key, NEW.id, NEW.received_at, NEW.status, ev, NEW.alert_name,
        NEW.severity, NEW.rule_group, NEW.fingerprint, NEW.device_id, NEW.template, NEW.summary,
        NEW.starts_at, NEW.ends_at
    )
    ON CONFLICT (organization_id, alert_key) DO UPDATE SET
        history_id  = EXCLUDED.history_id,
        received_at = EXCLUDED.received_at,
        status      = EXCLUDED.status,
        event_at    = EXCLUDED.event_at,
        alert_name  = EXCLUDED.alert_name,
        severity    = EXCLUDED.severity,
        rule_group  = EXCLUDED.rule_group,
        fingerprint = EXCLUDED.fingerprint,
        device_id   = EXCLUDED.device_id,
        template    = EXCLUDED.template,
        summary     = EXCLUDED.summary,
        starts_at   = EXCLUDED.starts_at,
        ends_at     = EXCLUDED.ends_at
    WHERE notification_active.event_at < EXCLUDED.event_at
       OR (notification_active.event_at = EXCLUDED.event_at
           AND notification_active.history_id < EXCLUDED.history_id);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER notification_history_active_sync
    AFTER INSERT ON notification_history
    FOR EACH ROW
    EXECUTE FUNCTION notification_active_sync();

-- Backfill latest state per alert key from existing history. Seed resolved tombstones too (not just
-- firing rows) so the event_at guard above has something to compare a later stale event against. Latest
-- is chosen by lifecycle event_at (id DESC as a tie-breaker), matching the trigger's ordering. The
-- trigger above is already live, so a concurrent ingest during backfill may have written a row first;
-- ON CONFLICT (with the same recency guard) keeps this idempotent instead of failing the migration.
INSERT INTO notification_active (
    organization_id, alert_key, history_id, received_at, status, event_at, alert_name,
    severity, rule_group, fingerprint, device_id, template, summary, starts_at, ends_at
)
SELECT
    latest.organization_id,
    latest.alert_key,
    latest.id,
    latest.received_at,
    latest.status,
    latest.event_at,
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
        md5(COALESCE(NULLIF(fingerprint, ''), alert_name || chr(31) || device_id)) AS alert_key,
        id, received_at, status,
        CASE
            WHEN status = 'firing' THEN COALESCE(starts_at, received_at)
            ELSE COALESCE(ends_at, received_at)
        END AS event_at,
        alert_name, severity, rule_group, fingerprint, device_id, template, summary, starts_at, ends_at
    FROM notification_history
    WHERE organization_id IS NOT NULL
    ORDER BY
        organization_id,
        COALESCE(NULLIF(fingerprint, ''), alert_name || chr(31) || device_id),
        CASE
            WHEN status = 'firing' THEN COALESCE(starts_at, received_at)
            ELSE COALESCE(ends_at, received_at)
        END DESC,
        id DESC
) latest
ON CONFLICT (organization_id, alert_key) DO UPDATE SET
    history_id  = EXCLUDED.history_id,
    received_at = EXCLUDED.received_at,
    status      = EXCLUDED.status,
    event_at    = EXCLUDED.event_at,
    alert_name  = EXCLUDED.alert_name,
    severity    = EXCLUDED.severity,
    rule_group  = EXCLUDED.rule_group,
    fingerprint = EXCLUDED.fingerprint,
    device_id   = EXCLUDED.device_id,
    template    = EXCLUDED.template,
    summary     = EXCLUDED.summary,
    starts_at   = EXCLUDED.starts_at,
    ends_at     = EXCLUDED.ends_at
WHERE notification_active.event_at < EXCLUDED.event_at
   OR (notification_active.event_at = EXCLUDED.event_at
       AND notification_active.history_id < EXCLUDED.history_id);
