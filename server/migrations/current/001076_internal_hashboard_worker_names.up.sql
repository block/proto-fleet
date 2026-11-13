-- requires-shared: 1074
-- A NULL name uses the plugin default; an empty name explicitly omits the suffix.
ALTER TABLE hashboard
    ADD COLUMN worker_name TEXT,
    ADD COLUMN worker_name_mode TEXT NOT NULL DEFAULT 'none'
        CONSTRAINT hashboard_worker_name_mode_check CHECK (worker_name_mode IN ('none', 'mac_address', 'custom'));

-- Activation can inherit a controller's input while the physical board retains
-- a different saved choice. Pool edits preserve this input until an explicit
-- worker-name confirmation replaces it.
ALTER TABLE pool_configuration_assignment ADD COLUMN inherited_worker_name TEXT;

-- Before this migration, {miner_name} used the controller's trimmed saved name,
-- MAC, or identifier, including for individually assigned hashboards. Preserve
-- that input for existing active assignments; new/unassigned boards stay blank.
-- The explicit trim set matches Go strings.TrimSpace, including Unicode spaces.
-- Inherited controller MACs are custom names, not each board's MAC strategy.
UPDATE hashboard h
SET worker_name = COALESCE(NULLIF(btrim(d.worker_name, U&'\0009\000A\000B\000C\000D\0020\0085\00A0\1680\2000\2001\2002\2003\2004\2005\2006\2007\2008\2009\200A\2028\2029\202F\205F\3000'), ''), NULLIF(btrim(d.mac_address, U&'\0009\000A\000B\000C\000D\0020\0085\00A0\1680\2000\2001\2002\2003\2004\2005\2006\2007\2008\2009\200A\2028\2029\202F\205F\3000'), ''), d.device_identifier),
    worker_name_mode = 'custom'
FROM hashboard_installation i
JOIN container_module m ON m.id = i.module_id AND m.retired_at IS NULL
JOIN container_tank tank ON tank.id = m.tank_id AND tank.present AND tank.device_id = i.device_id
JOIN device d ON d.id = i.device_id AND d.deleted_at IS NULL
JOIN discovered_device dd ON dd.id = d.discovered_device_id AND dd.org_id = d.org_id
WHERE h.id = i.hashboard_id AND h.org_id = d.org_id AND h.worker_name IS NULL
    AND i.removed_at IS NULL AND i.site_id IS NOT DISTINCT FROM d.site_id
    AND dd.device_class = 2 AND dd.deleted_at IS NULL
    AND EXISTS (
        SELECT 1 FROM pool_configuration_assignment a
        JOIN pool_configuration_revision r ON r.organization_id = a.organization_id
            AND r.configuration_id = a.configuration_id AND r.revision_id = a.revision_id
        WHERE a.organization_id = d.org_id AND a.device_id = d.id AND a.retired_at IS NULL
            AND ((a.target_kind = 'hashboard_installation' AND a.target_id = i.id)
                OR (a.target_kind = 'device' AND a.target_id = d.id))
            AND EXISTS (
                SELECT 1 FROM jsonb_array_elements(r.slots) slot
                WHERE strpos(slot->>'worker_template', '{miner_name}') > 0
            )
    );

-- Existing miner and whole-container assignments need an explicit name before
-- empty metadata gains the new no-suffix meaning. Keep the old spelling/case exactly;
-- normalizing a MAC here would change an already configured pool username.
UPDATE device d
SET worker_name = COALESCE(NULLIF(btrim(d.worker_name, U&'\0009\000A\000B\000C\000D\0020\0085\00A0\1680\2000\2001\2002\2003\2004\2005\2006\2007\2008\2009\200A\2028\2029\202F\205F\3000'), ''), NULLIF(btrim(d.mac_address, U&'\0009\000A\000B\000C\000D\0020\0085\00A0\1680\2000\2001\2002\2003\2004\2005\2006\2007\2008\2009\200A\2028\2029\202F\205F\3000'), ''), d.device_identifier)
FROM discovered_device dd
WHERE dd.id = d.discovered_device_id AND dd.org_id = d.org_id
    AND dd.device_class IN (1, 2) AND dd.deleted_at IS NULL AND d.deleted_at IS NULL
    AND EXISTS (
        SELECT 1 FROM pool_configuration_assignment a
        JOIN pool_configuration_revision r ON r.organization_id = a.organization_id
            AND r.configuration_id = a.configuration_id AND r.revision_id = a.revision_id
        WHERE a.organization_id = d.org_id AND a.device_id = d.id
            AND a.target_kind = 'device' AND a.target_id = d.id AND a.retired_at IS NULL
            AND EXISTS (
                SELECT 1 FROM jsonb_array_elements(r.slots) slot
                WHERE strpos(slot->>'worker_template', '{miner_name}') > 0
            )
    );
