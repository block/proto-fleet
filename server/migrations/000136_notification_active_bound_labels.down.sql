-- Restores 000094's unguarded sync function and its rule_group-less fallback, rekeying fingerprintless rows back
-- onto it so their lifecycle state survives the rollback.
LOCK TABLE notification_history IN SHARE MODE;
LOCK TABLE notification_active IN SHARE MODE;

-- The old key can't tell two rule groups apart, so rows that split under 000136 have to collapse again. Keep the
-- one the trigger itself would have kept (newest event, then newest row) and drop the rest.
DELETE FROM notification_active na
WHERE na.fingerprint = ''
  AND EXISTS (
      SELECT 1
      FROM notification_active other
      WHERE other.organization_id = na.organization_id
        AND other.fingerprint = ''
        AND other.alert_name = na.alert_name
        AND other.device_id = na.device_id
        AND (other.event_at, other.history_id) > (na.event_at, na.history_id)
  );

UPDATE notification_active
SET alert_key = md5(alert_name || chr(31) || device_id)
WHERE fingerprint = '';

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
