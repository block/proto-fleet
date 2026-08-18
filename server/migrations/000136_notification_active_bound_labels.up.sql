-- 000137-000138 index alert_name, rule_group and device_id as raw text, so an oversized value would push an entry
-- past the btree tuple limit and fail the insert. 190 is Grafana's rule-title cap, 255 device_identifier's (000084).

-- SHARE drains in-flight webhook writes, so none commits through the old trigger after the DELETE's snapshot and
-- leaves a row for 000137's index build to fail on. history first: a writer already holds it before the trigger.
LOCK TABLE notification_history IN SHARE MODE;
LOCK TABLE notification_active IN SHARE MODE;

-- Rows carrying chr(31) go too: the trigger below skips them, so one left behind could never be resolved and
-- would sit firing until the freshness window dropped it.
DELETE FROM notification_active
WHERE LENGTH(alert_name) > 190
   OR LENGTH(rule_group) > 190
   OR LENGTH(device_id) > 255
   OR strpos(alert_name, chr(31)) > 0
   OR strpos(rule_group, chr(31)) > 0
   OR strpos(device_id, chr(31)) > 0;

-- The fallback identity gains rule_group: 000094 keyed on name and device alone, so two same-named rules firing
-- on one device overwrote each other. Rekey the existing fingerprintless rows rather than dropping them: a
-- deleted resolved row is a lost tombstone, and a delayed firing retry for that episode would reopen the alert
-- with no prior state to lose to. No key can collide, because the purge above leaves no chr(31) in any part, so
-- the old preimage holds exactly one separator and the new one exactly two.
UPDATE notification_active
SET alert_key = md5(alert_name || chr(31) || rule_group || chr(31) || device_id)
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
    -- Only a forged post breaches these. Skip rather than truncate: the rollup groups on these columns, so a
    -- prefix would merge two rules, and a chr(31) in one part hashes onto the identity of another split.
    IF LENGTH(NEW.alert_name) > 190 OR LENGTH(NEW.rule_group) > 190 OR LENGTH(NEW.device_id) > 255
       OR strpos(NEW.alert_name, chr(31)) > 0
       OR strpos(NEW.rule_group, chr(31)) > 0
       OR strpos(NEW.device_id, chr(31)) > 0 THEN
        RETURN NEW;
    END IF;
    key := md5(COALESCE(
        NULLIF(NEW.fingerprint, ''),
        NEW.alert_name || chr(31) || NEW.rule_group || chr(31) || NEW.device_id
    ));
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
